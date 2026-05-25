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
