"""DreamAPI sidecar FastAPI app.

Wraps DreamAPI image generation + edit endpoints behind a uniform
synchronous HTTP surface. The Go orchestrator's design skill calls
this service; the browser never sees this surface directly.

Surface (every endpoint is synchronous submit+poll; 30-60 s typical):

    POST /generate/image       Flux text2image
    POST /generate/variants    Flux text2image with num>1 — same prompt,
                               distinct seeds, returns N images
    POST /edit/remove_bg       remove background (alpha mask)
    POST /edit/enhance         super-resolution + sharpen

The synchronous shape keeps the bridge + browser code one straight
line; SSE-based progress streaming is on the roadmap (track in TODO
at the bottom of this file).

Run locally:

    cd services/dreamapi-sidecar
    pip install -r requirements.txt
    DREAMAPI_API_KEY=sk-...  uvicorn main:app --port 8091 --reload
"""
from __future__ import annotations

from typing import Optional

from fastapi import Depends, FastAPI, HTTPException, status
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field, HttpUrl

from dreamapi_client import DreamAPIClient, DreamAPIError


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


# TODO(progress):
#   Add POST /generate/image/submit (returns task_id) + GET
#   /generate/image/{task_id}/events (SSE polling under the hood) so
#   the canvas can show a placeholder shape with a live progress
#   indicator rather than a 60-second loading spinner. Same shape as
#   the Opendream SSE-tailing-state-file pattern; not blocking the
#   MVP because the synchronous version puts pixels on screen.
#
# TODO(more-edits):
#   inpainting / outpainting / colorize / swap_face all wrap the same
#   submit+poll pattern. Add them when the canvas grows specific
#   actions for them — outpainting in particular is a natural fit for
#   "change aspect ratio of this image" once that becomes a UX.
