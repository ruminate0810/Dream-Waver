"""DreamAPI sidecar FastAPI app.

Wraps DreamAPI image generation + edit endpoints behind a uniform
HTTP + SSE surface. The Go orchestrator's design skill calls this
service; the browser never sees this surface directly.

Synchronous endpoints (block until DreamAPI completes; 30-60 s):

    POST /generate/image       Flux text2image
    POST /generate/variants    Flux text2image with num>1
    POST /edit/remove_bg       remove background (alpha mask)
    POST /edit/enhance         super-resolution + sharpen
    POST /edit/outpaint        extend image borders
    POST /edit/image2image     transform existing image via prompt

SSE flow — for the canvas's in-place progress experience:

    POST /generate/image/submit              returns {task_id}
    GET  /generate/image/{task_id}/events    streams progress|done|error

The SSE flow polls DreamAPI inside the stream handler so the polling
lifetime tracks the SSE connection — browser disconnect immediately
stops upstream polling, no zombie tasks.

Run locally:

    cd services/dreamapi-sidecar
    pip install -r requirements.txt
    DREAMAPI_API_KEY=sk-...  uvicorn main:app --port 8091 --reload
"""
from __future__ import annotations

import asyncio
import json
import time
import uuid
from dataclasses import dataclass, field
from typing import AsyncIterator, Optional

from fastapi import Depends, FastAPI, HTTPException, Request, status
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field, HttpUrl

from dreamapi_client import (
    DEFAULT_POLL_INTERVAL_S,
    STATUS_FAILED,
    STATUS_SUCCESS,
    DreamAPIClient,
    DreamAPIError,
)
from df_ability_client import (
    DFAbilityClient,
    DFAbilityError,
)


# ─── App + dependencies ────────────────────────────────────────────


app = FastAPI(
    title="dreamapi-sidecar",
    description=(
        "Thin FastAPI front for DreamAPI image generation, behind "
        "the Dream-Waver Go orchestrator. Not safe to expose directly "
        "— no auth, no rate-limiting; trusts the upstream caller."
    ),
    version="0.1.0",
)

# Permissive CORS so a local dev browser CAN hit the sidecar directly
# for debugging, but in production all traffic comes from the Go
# orchestrator over the cluster network where CORS is moot.
app.add_middleware(
    CORSMiddleware,
    allow_origin_regex=r"http://(localhost|127\.0\.0\.1):\d+",
    allow_methods=["*"],
    allow_headers=["*"],
    allow_credentials=True,
)


def get_client() -> DreamAPIClient:
    """One client per request. Cheap to construct (just stashes the
    api key), and per-request creation avoids stale-credentials bugs
    if the env var changes during a long-running uvicorn process."""
    try:
        return DreamAPIClient()
    except DreamAPIError as exc:
        # Convert auth failures into a clean 503 so the Go bridge can
        # mirror it to the browser with a setup hint instead of a 500.
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail=str(exc),
        ) from exc


def get_df_client() -> DFAbilityClient:
    """Same per-request construction pattern as get_client, but for the
    df-ability-server internal API (NanoBanana + Seedance i2v). 503s
    when the access/secret env vars aren't set so the Go bridge can
    show a setup hint to the user."""
    try:
        return DFAbilityClient()
    except DFAbilityError as exc:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail=str(exc),
        ) from exc


# ─── Health ────────────────────────────────────────────────────────


@app.get("/healthz", include_in_schema=False)
def healthz() -> dict[str, str]:
    return {"status": "ok"}


# ─── POST /generate/image (Flux text2image) ───────────────────────


class GenerateImageRequest(BaseModel):
    """Body for `POST /generate/image`.

    The shape mirrors DreamAPI's flux_text2image input with a couple of
    sane defaults so the canvas doesn't have to think about dimensions
    for the common case. width/height must be multiples of 16 per
    DreamAPI's contract.
    """

    prompt: str = Field(..., min_length=1, description="Text description of the image")
    width: int = Field(1024, ge=256, le=1600, multiple_of=16)
    height: int = Field(1024, ge=256, le=1600, multiple_of=16)
    seed: Optional[int] = Field(
        None,
        description=(
            "Optional deterministic seed. When the canvas asks for "
            "'4 variants of this image' we'll pass distinct seeds so "
            "results are reproducible."
        ),
    )


class GenerateImageResponse(BaseModel):
    url: str
    width: int
    height: int
    task_id: str
    # Cost is left for the orchestrator to compute from a price table —
    # DreamAPI doesn't return a unit price in the task payload.


@app.post(
    "/generate/image",
    response_model=GenerateImageResponse,
    responses={
        503: {"description": "DreamAPI key not configured (see DREAMAPI_API_KEY)"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def generate_image(
    body: GenerateImageRequest,
    client: DreamAPIClient = Depends(get_client),
) -> GenerateImageResponse:
    """Submit a Flux text2image task and block until it completes.

    Typical latency 30-60s; we hold the connection open the whole time
    because the synchronous shape keeps the Go bridge / browser code
    one straight line. Once we want a progress UI in the canvas,
    swap this for `POST /generate/image` (returns task_id) + `GET
    /generate/image/{task_id}/events` (SSE).
    """
    api_body = {
        "prompt": body.prompt,
        "width": body.width,
        "height": body.height,
    }
    if body.seed is not None:
        api_body["seed"] = body.seed

    try:
        data = client.run("/api/async/flux_text2image", api_body)
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    url = out.get("output_url")
    if not url:
        # Polled-OK but no image — shouldn't happen for text2image, but
        # if DreamAPI ever changes its schema we want a precise error
        # instead of a silent empty string downstream.
        raise HTTPException(
            status_code=502,
            detail="DreamAPI returned no image URL despite success status",
        )

    return GenerateImageResponse(
        url=url,
        width=body.width,
        height=body.height,
        task_id=out.get("task_id", ""),
    )


# ─── POST /generate/variants (Flux text2image, N variants) ────────


class GenerateVariantsRequest(BaseModel):
    """Body for `POST /generate/variants`.

    DreamAPI's Flux text2image accepts `num` (1-10) and returns that
    many images in one poll. We cap at 6 for the canvas — beyond that
    the 2x3 grid stops being scannable and per-image cost is the same
    as N single calls anyway.
    """

    prompt: str = Field(..., min_length=1)
    count: int = Field(4, ge=2, le=6, description="Number of variants")
    width: int = Field(1024, ge=256, le=1600, multiple_of=16)
    height: int = Field(1024, ge=256, le=1600, multiple_of=16)


class Variant(BaseModel):
    url: str
    width: int
    height: int


class GenerateVariantsResponse(BaseModel):
    variants: list[Variant]
    task_id: str


@app.post(
    "/generate/variants",
    response_model=GenerateVariantsResponse,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def generate_variants(
    body: GenerateVariantsRequest,
    client: DreamAPIClient = Depends(get_client),
) -> GenerateVariantsResponse:
    """Generate N variants of one prompt in a single task.

    DreamAPI guarantees distinct seeds across `num` results, so the
    canvas can drop them in a 2xN grid and the user reads variation at
    a glance without manually re-rolling. Latency is similar to a
    single image — the provider parallelises internally.
    """
    api_body = {
        "prompt": body.prompt,
        "width": body.width,
        "height": body.height,
        "num": body.count,
    }
    try:
        data = client.run("/api/async/flux_text2image", api_body)
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    urls = out.get("images") or []
    if not urls:
        raise HTTPException(
            status_code=502,
            detail="DreamAPI returned no images despite success status",
        )
    return GenerateVariantsResponse(
        variants=[
            Variant(url=u, width=body.width, height=body.height) for u in urls
        ],
        task_id=out.get("task_id", ""),
    )


# ─── POST /edit/remove_bg (remove_background) ─────────────────────


class EditImageRequest(BaseModel):
    """Body shared by every endpoint that operates on an existing image
    URL — remove_bg, enhance, colorize, etc. Keeps the wire surface
    uniform so the bridge can serialise without per-endpoint shaping."""

    image_url: HttpUrl = Field(..., description="URL of the source image")


class EditImageResponse(BaseModel):
    url: str
    # width/height are nullable because DreamAPI's edit endpoints don't
    # always echo dimensions in the response. The canvas falls back to
    # natural image size after load when these are missing.
    width: Optional[int] = None
    height: Optional[int] = None
    task_id: str


@app.post(
    "/edit/remove_bg",
    response_model=EditImageResponse,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def edit_remove_bg(
    body: EditImageRequest,
    client: DreamAPIClient = Depends(get_client),
) -> EditImageResponse:
    """Cut the background out of an image. Output is a PNG with alpha.

    Note the DreamAPI quirk: this endpoint takes `url`, not `imageUrl`
    — different from /edit/enhance below. The bridge's uniform wire
    surface hides the inconsistency from callers.
    """
    try:
        data = client.run(
            "/api/async/remove_background",
            {"url": str(body.image_url)},
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    url = out.get("output_url")
    if not url:
        raise HTTPException(status_code=502, detail="remove_bg returned no image")
    return EditImageResponse(url=url, task_id=out.get("task_id", ""))


# ─── POST /edit/enhance (super-resolution) ────────────────────────


@app.post(
    "/edit/enhance",
    response_model=EditImageResponse,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def edit_enhance(
    body: EditImageRequest,
    client: DreamAPIClient = Depends(get_client),
) -> EditImageResponse:
    """Upscale + sharpen an image. Typical output is 2-4× input
    resolution; DreamAPI picks the scale factor internally.

    DreamAPI quirk: this endpoint takes `imageUrl` (camelCase), not
    `url` like remove_bg. Documented at the call site to flag future
    contract drift.
    """
    try:
        data = client.run(
            "/api/async/enhance",
            {"imageUrl": str(body.image_url)},
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    url = out.get("output_url")
    if not url:
        raise HTTPException(status_code=502, detail="enhance returned no image")
    return EditImageResponse(url=url, task_id=out.get("task_id", ""))


# ─── POST /edit/outpaint (extend image borders) ───────────────────


class OutpaintRequest(BaseModel):
    """Body for `POST /edit/outpaint`. left/right/top/bottom are pixels
    to extend in each direction; 0 means "don't extend this side".
    At least one must be > 0 or the call is a no-op."""

    image_url: HttpUrl
    left: int = Field(0, ge=0, le=2000)
    right: int = Field(0, ge=0, le=2000)
    top: int = Field(0, ge=0, le=2000)
    bottom: int = Field(0, ge=0, le=2000)


@app.post(
    "/edit/outpaint",
    response_model=EditImageResponse,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def edit_outpaint(
    body: OutpaintRequest,
    client: DreamAPIClient = Depends(get_client),
) -> EditImageResponse:
    """Extend an image's borders. DreamAPI infills the new area using
    surrounding context — the typical use is "change this image's
    aspect ratio" (e.g. extend 200 px left + 200 px right to turn a
    1:1 into a 16:9)."""
    if body.left + body.right + body.top + body.bottom == 0:
        raise HTTPException(
            status_code=422,
            detail="at least one of left/right/top/bottom must be > 0",
        )
    try:
        data = client.run(
            "/api/async/outpainting",
            {
                "url": str(body.image_url),
                "outPaintSize": {
                    "left": body.left,
                    "right": body.right,
                    "top": body.top,
                    "bottom": body.bottom,
                },
            },
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    url = out.get("output_url")
    if not url:
        raise HTTPException(status_code=502, detail="outpaint returned no image")
    return EditImageResponse(url=url, task_id=out.get("task_id", ""))


# ─── POST /edit/image2image (transform an image via prompt) ───────


class Image2ImageRequest(BaseModel):
    """Body for `POST /edit/image2image` — Flux image-to-image. The
    output dimensions can differ from input (the model generates fresh
    pixels at the requested size guided by the source). Width/height
    follow the same constraints as text2image."""

    image_url: HttpUrl
    prompt: str = Field(..., min_length=1)
    width: int = Field(1024, ge=256, le=1600, multiple_of=16)
    height: int = Field(1024, ge=256, le=1600, multiple_of=16)


@app.post(
    "/edit/image2image",
    response_model=GenerateImageResponse,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def edit_image2image(
    body: Image2ImageRequest,
    client: DreamAPIClient = Depends(get_client),
) -> GenerateImageResponse:
    """Transform an existing image guided by a text prompt. Returns one
    image at the requested dimensions; for N variants, the canvas
    would call this N times with distinct seeds (same as text2image,
    we just don't expose a `num` parameter to keep the wire small)."""
    try:
        data = client.run(
            "/api/async/flux_image2image",
            {
                "imageUrl": str(body.image_url),
                "prompt": body.prompt,
                "width": body.width,
                "height": body.height,
            },
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    url = out.get("output_url")
    if not url:
        raise HTTPException(status_code=502, detail="image2image returned no image")
    return GenerateImageResponse(
        url=url,
        width=body.width,
        height=body.height,
        task_id=out.get("task_id", ""),
    )


# ─── POST /edit/colorize (B&W → color) ────────────────────────────


@app.post(
    "/edit/colorize",
    response_model=EditImageResponse,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI upstream error"},
        504: {"description": "DreamAPI task did not finish in time"},
    },
)
def edit_colorize(
    body: EditImageRequest,
    client: DreamAPIClient = Depends(get_client),
) -> EditImageResponse:
    """Add realistic colours to a B&W photo. DreamAPI requires the image
    to contain at least one human face; the upstream surface returns a
    400 with a clear message when it doesn't, which the bridge mirrors
    back as a 4xx so the canvas can show the hint inline."""
    try:
        data = client.run(
            "/api/async/colorize",
            {"url": str(body.image_url)},
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    out = client.extract_output(data)
    url = out.get("output_url")
    if not url:
        raise HTTPException(status_code=502, detail="colorize returned no image")
    return EditImageResponse(url=url, task_id=out.get("task_id", ""))


# ─── POST /generate/nano_banana (Google Gemini image) ─────────────


class NanoBananaRequest(BaseModel):
    """Body for `POST /generate/nano_banana`.

    Wraps the df-ability-server `df-ability-google-gemini` endpoint —
    Google Gemini image generation. The biggest differentiator vs Flux
    is reference-image support: the canvas can pass up to 4 existing
    image URLs (typically previously-generated AI images on the canvas)
    so the model riffs on them instead of starting from a blank latent.

    Model variants:
      - "nano-banana-2"   → gemini-3.1-flash-image-preview (cheap, fast)
      - "nano-banana-pro" → gemini-3-pro-image-preview     (higher fidelity)

    image_size / aspect_ratio are accepted for forward-compat but are
    NOT forwarded to the upstream — the documented contract is `model`
    + `contents[].parts[]` only. The canvas keeps sending them so we
    can wire them up once the gateway supports `generationConfig`.
    """

    prompt: str = Field(..., min_length=1)
    model: str = Field("nano-banana-2", description="nano-banana-2 | nano-banana-pro")
    image_size: Optional[str] = Field(
        None,
        description="Reserved — gateway doesn't yet accept generationConfig",
    )
    aspect_ratio: Optional[str] = Field(
        None,
        description="Reserved — gateway doesn't yet accept generationConfig",
    )
    # 4 references max — past that the prompt steers more than refs and
    # response payloads balloon. df-ability accepts more in theory.
    # Accepts either an http(s) URL (gateway fetches it) OR a base64
    # data URL `data:image/<type>;base64,<payload>` (forwarded as a
    # proper Gemini-native inlineData with mimeType). The dual-mode
    # is what lets user-uploaded files participate as references
    # without us hosting them anywhere.
    images: Optional[list[str]] = Field(
        None,
        max_length=4,
        description="Up to 4 reference images — http(s) URL or data:image/...;base64,... data URL",
    )


_NANO_BANANA_MODELS = {
    "nano-banana-2": "gemini-3.1-flash-image-preview",
    "nano-banana-pro": "gemini-3-pro-image-preview",
}

# data URL parser: matches `data:image/<subtype>;base64,<payload>` and
# captures the subtype + payload. Anything else (e.g. `data:image/<x>;`
# without base64 encoding) we treat as malformed and skip with a log.
import re as _re  # local-only import alias to avoid colliding with re elsewhere
_DATA_URL_RE = _re.compile(r"^data:(image/[A-Za-z0-9.+-]+);base64,(.+)$", _re.DOTALL)


def _build_ref_part(img: str) -> Optional[dict[str, Any]]:
    """Convert one reference (URL or data URL) into a Gemini parts entry.

    Returns None for malformed inputs (caller skips silently — better
    UX than 422'ing the whole generation when one ref is bad).
    """
    if not isinstance(img, str) or not img:
        return None
    s = img.strip()
    if s.startswith("data:"):
        m = _DATA_URL_RE.match(s)
        if m is None:
            return None
        mime_type, payload = m.group(1), m.group(2)
        # Gateway upstream Gemini requires mimeType when data is base64.
        return {"inlineData": {"mimeType": mime_type, "data": payload}}
    if s.startswith("http://") or s.startswith("https://"):
        # Gateway-specific: it accepts a URL string in inlineData.data
        # and fetches the image itself before forwarding to Gemini.
        return {"inlineData": {"data": s}}
    return None

# df-ability paths for the google-gemini ability. Per the doc the
# google-gemini endpoint is under /df-ability-server/...; seedance is
# at /task/... — kept distinct so the gateway routes correctly.
_NANO_BANANA_ABILITY = "df-ability-google-gemini"
_NANO_BANANA_SUBMIT = "/df-ability-server/task/v1/submit"
_NANO_BANANA_STATUS = "/df-ability-server/task/v1/status"


@app.post(
    "/generate/nano_banana",
    response_model=GenerateImageResponse,
    responses={
        503: {"description": "DF_ABILITY_* env vars not configured"},
        502: {"description": "df-ability upstream error"},
        504: {"description": "df-ability task did not finish in time"},
    },
)
def generate_nano_banana(
    body: NanoBananaRequest,
    client: DFAbilityClient = Depends(get_df_client),
) -> GenerateImageResponse:
    """Generate via Google Gemini through the df-ability-server gateway.

    Returns width/height of 0 — the gateway returns just a `result` URL
    without echoed dimensions. The canvas measures the loaded <img>.
    """
    upstream_model = _NANO_BANANA_MODELS.get(body.model)
    if upstream_model is None:
        raise HTTPException(
            status_code=422,
            detail=f"unknown model {body.model!r}; expected one of {list(_NANO_BANANA_MODELS)}",
        )

    # Build the Gemini-native body. Two transport modes per reference:
    #
    #   http(s)://...               → {inlineData: {data: <url>}}
    #                                  Non-standard vs native Gemini, but
    #                                  the gateway documents URLs in
    #                                  inlineData.data and accepts them.
    #
    #   data:image/<type>;base64,X  → {inlineData: {mimeType, data: X}}
    #                                  Proper native Gemini shape. The
    #                                  gateway proxies straight to upstream;
    #                                  this lets user-uploaded files
    #                                  participate as references without
    #                                  any hosting on our side.
    #
    # Important: the mimeType field is REQUIRED for base64 — sending
    # base64 without it causes a 500 from upstream Gemini (verified
    # empirically by curl test before this code landed).
    parts: list[dict[str, Any]] = [{"text": body.prompt}]
    if body.images:
        for img in body.images:
            ref_part = _build_ref_part(img)
            if ref_part is not None:
                parts.append(ref_part)

    upstream_body = {
        "model": upstream_model,
        "contents": [{"parts": parts}],
    }

    try:
        data = client.run(
            ability=_NANO_BANANA_ABILITY,
            submit_path=_NANO_BANANA_SUBMIT,
            status_path_base=_NANO_BANANA_STATUS,
            body=upstream_body,
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DFAbilityError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    url = data.get("result")
    if not url or not isinstance(url, str):
        raise HTTPException(status_code=502, detail="nano_banana returned no image URL")
    return GenerateImageResponse(
        url=url,
        width=0,
        height=0,
        task_id=str(data.get("task_id") or ""),
    )


# ─── POST /video/seedance_i2v (image-to-video) ────────────────────


class SeedanceI2VRequest(BaseModel):
    """Body for `POST /video/seedance_i2v`.

    Submits an image-to-video task to df-ability `seedance-image-to-video`
    (doubao-seedance-1-5-pro). Output is an MP4 URL; the canvas drops
    it into a video shape (TLDraw natively supports video assets).
    """

    image_url: HttpUrl = Field(..., description="Reference image URL")
    prompt: str = Field(..., min_length=1, max_length=1500)
    # Gateway accepts 480p / 720p / 1080p. 720p is the doc default —
    # we mirror that. 1080p is 3-4× the cost of 720p.
    resolution: str = Field("720p", pattern=r"^(480p|720p|1080p)$")
    # adaptive lets the model match the source's ratio — usually what
    # the canvas wants. Other options: 16:9 / 9:16 / 1:1 / 4:3.
    ratio: str = Field("adaptive", description="adaptive / 16:9 / 9:16 / 1:1 / 4:3")
    # 4-12 s is the documented sweet spot; 5s is the default.
    duration: int = Field(5, ge=4, le=12)
    seed: Optional[int] = Field(None, description="-1 = random")


class SeedanceI2VResponse(BaseModel):
    """The realised MP4 URL. We don't echo back the chosen resolution —
    the canvas needs only the URL; reuse the request payload if you
    want to surface "720p / 5s" in the UI."""

    video_url: str
    task_id: str


# df-ability paths for seedance. The seedance doc shows `/task/v1/submit`
# (no /df-ability-server prefix) but the live gateway 404s on that path;
# in practice the prefix is required, same as the google-gemini ability.
# Confirmed empirically — keep this in sync if the gateway ever fixes
# its routing.
_SEEDANCE_ABILITY = "df-ability-seedance-image-to-video"
_SEEDANCE_SUBMIT = "/df-ability-server/task/v1/submit"
_SEEDANCE_STATUS = "/df-ability-server/task/v1/status"
_SEEDANCE_MODEL = "doubao-seedance-1-5-pro-251215"


@app.post(
    "/video/seedance_i2v",
    response_model=SeedanceI2VResponse,
    responses={
        503: {"description": "DF_ABILITY_* env vars not configured"},
        502: {"description": "df-ability upstream error"},
        504: {"description": "df-ability task did not finish in time"},
    },
)
def video_seedance_i2v(
    body: SeedanceI2VRequest,
    client: DFAbilityClient = Depends(get_df_client),
) -> SeedanceI2VResponse:
    """Submit a Seedance image-to-video task and block until the MP4
    is ready. Typical wall-clock 60-120s for 720p/5s; we hold the
    connection open the whole time (matches the synchronous shape of
    every other sidecar endpoint)."""
    upstream_body: dict[str, Any] = {
        "model": _SEEDANCE_MODEL,
        "prompt": body.prompt,
        "image_url": str(body.image_url),
        "resolution": body.resolution,
        "ratio": body.ratio,
        "duration": body.duration,
    }
    if body.seed is not None:
        upstream_body["seed"] = body.seed

    try:
        # Seedance i2v takes longer than image gen; bump the per-poll
        # interval to 6s and keep the 10-min cap (matches DFAbility default).
        data = client.run(
            ability=_SEEDANCE_ABILITY,
            submit_path=_SEEDANCE_SUBMIT,
            status_path_base=_SEEDANCE_STATUS,
            body=upstream_body,
            interval=6.0,
        )
    except TimeoutError as exc:
        raise HTTPException(status_code=504, detail=str(exc)) from exc
    except DFAbilityError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    video_url = data.get("result")
    if not video_url or not isinstance(video_url, str):
        raise HTTPException(status_code=502, detail="seedance_i2v returned no video URL")
    return SeedanceI2VResponse(
        video_url=video_url,
        task_id=str(data.get("task_id") or ""),
    )


# ─── SSE: submit + events for in-canvas progress ──────────────────


@dataclass
class _PendingTask:
    """In-memory record of a submit'd task awaiting SSE consumer.

    Process-local; restarting the sidecar drops in-flight tasks. That
    matches DreamAPI's own semantics — task IDs are only meaningful
    within a session anyway, and the canvas will time out and offer a
    retry if it can't find the task.
    """

    sidecar_task_id: str
    dreamapi_task_id: str
    width: int
    height: int
    created_at: float = field(default_factory=time.time)


# Module-level registry. Cap with a slow GC sweep on submit to avoid
# unbounded growth from canvases that submit-then-never-stream.
_PENDING: dict[str, _PendingTask] = {}
_PENDING_TTL_S = 600.0  # 10 min — DreamAPI itself caps tasks well before this


def _gc_pending() -> None:
    cutoff = time.time() - _PENDING_TTL_S
    stale = [k for k, v in _PENDING.items() if v.created_at < cutoff]
    for k in stale:
        _PENDING.pop(k, None)


class SubmitGenerateResponse(BaseModel):
    task_id: str


@app.post(
    "/generate/image/submit",
    response_model=SubmitGenerateResponse,
    status_code=status.HTTP_202_ACCEPTED,
    responses={
        503: {"description": "DreamAPI key not configured"},
        502: {"description": "DreamAPI rejected the submit"},
    },
)
def submit_generate_image(
    body: GenerateImageRequest,
    client: DreamAPIClient = Depends(get_client),
) -> SubmitGenerateResponse:
    """Kick off a Flux text2image task and return a sidecar-local task
    id. The caller then opens the SSE endpoint with that id to receive
    progress + final URL.

    We don't return the upstream DreamAPI task id directly — wrapping
    keeps the wire surface stable when we later swap providers, and
    means the canvas can't accidentally poll DreamAPI without going
    through us (which would skip future billing hooks)."""
    _gc_pending()
    api_body = {
        "prompt": body.prompt,
        "width": body.width,
        "height": body.height,
    }
    if body.seed is not None:
        api_body["seed"] = body.seed
    try:
        dreamapi_task_id = client.submit("/api/async/flux_text2image", api_body)
    except DreamAPIError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    sidecar_task_id = uuid.uuid4().hex[:12]
    _PENDING[sidecar_task_id] = _PendingTask(
        sidecar_task_id=sidecar_task_id,
        dreamapi_task_id=dreamapi_task_id,
        width=body.width,
        height=body.height,
    )
    return SubmitGenerateResponse(task_id=sidecar_task_id)


@app.get("/generate/image/{task_id}/events")
async def stream_generate_image_events(
    task_id: str,
    request: Request,
    client: DreamAPIClient = Depends(get_client),
) -> StreamingResponse:
    """SSE stream emitting `progress` events on each poll tick, then a
    single terminal `done` or `error` event.

    Polling lives inside the stream handler so a browser disconnect
    immediately stops upstream polling (no zombie tasks). The interval
    matches DreamAPI's recommended cadence to avoid rate-limit pressure.
    """
    task = _PENDING.get(task_id)
    if task is None:
        raise HTTPException(status_code=404, detail="unknown task_id")

    async def event_source() -> AsyncIterator[str]:
        start = time.time()
        # First emit so the browser knows the connection is live and the
        # task is queued. Useful UX cue while DreamAPI hasn't yet picked
        # up the work.
        yield _sse("progress", json.dumps({
            "elapsed_s": 0,
            "status": "queued",
        }))
        while True:
            await asyncio.sleep(DEFAULT_POLL_INTERVAL_S)
            if await request.is_disconnected():
                # Drop the task — the next stream lookup will 404 which
                # the canvas treats as "I should resubmit". Cleaner than
                # holding state for a tab that's gone.
                _PENDING.pop(task_id, None)
                return
            try:
                data = client.query(task.dreamapi_task_id)
            except DreamAPIError as exc:
                yield _sse("error", json.dumps({"message": str(exc)}))
                _PENDING.pop(task_id, None)
                return
            except Exception as exc:  # noqa: BLE001 — log+surface
                yield _sse("error", json.dumps({"message": str(exc)}))
                _PENDING.pop(task_id, None)
                return

            t = data.get("task", {})
            upstream_status = t.get("status")
            elapsed = int(time.time() - start)

            if upstream_status == STATUS_SUCCESS:
                out = client.extract_output(data)
                url = out.get("output_url")
                if not url:
                    yield _sse("error", json.dumps({
                        "message": "DreamAPI returned no image despite success status",
                    }))
                else:
                    yield _sse("done", json.dumps({
                        "url": url,
                        "width": task.width,
                        "height": task.height,
                        "task_id": out.get("task_id", ""),
                    }))
                _PENDING.pop(task_id, None)
                return

            if upstream_status == STATUS_FAILED:
                reason = t.get("reason", "task failed")
                yield _sse("error", json.dumps({"message": reason}))
                _PENDING.pop(task_id, None)
                return

            # Still queued/running — emit a heartbeat so the browser
            # can render an "Xs elapsed" hint on the placeholder shape.
            yield _sse("progress", json.dumps({
                "elapsed_s": elapsed,
                "status": _status_label(upstream_status),
            }))

    return StreamingResponse(
        event_source(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


def _sse(event: str, data: str) -> str:
    return f"event: {event}\ndata: {data}\n\n"


def _status_label(raw: object) -> str:
    """DreamAPI returns integer status codes; map to short labels the
    canvas can show without translating itself."""
    if raw is None:
        return "queued"
    if raw == 0:
        return "queued"
    if raw == 1:
        return "running"
    if raw == 2:
        return "finalising"
    return f"status-{raw}"
