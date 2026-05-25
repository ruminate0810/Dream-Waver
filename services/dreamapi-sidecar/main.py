"""DreamAPI sidecar FastAPI app.

Single-purpose: turn natural-language prompts into image URLs via
DreamAPI's Flux text2image endpoint. The Go orchestrator's design
skill calls this service; the browser never sees this surface
directly.

Synchronous on purpose for MVP — Flux completes in ~30-60s and a
single blocking HTTP request is the smallest possible API surface
the canvas needs to put an image on screen. SSE-based progress
streaming is on the roadmap once the canvas grows beyond "drop
image" into "show generation progress in place" (track in TODO at
the bottom of this file).

Run locally:

    cd services/dreamapi-sidecar
    pip install -r requirements.txt
    DREAMAPI_API_KEY=sk-...  uvicorn main:app --port 8091 --reload
"""
from __future__ import annotations

import os
from typing import Optional

from fastapi import Depends, FastAPI, HTTPException, status
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field

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


# TODO(progress):
#   Add POST /generate/image/submit (returns task_id) + GET
#   /generate/image/{task_id}/events (SSE polling under the hood) so
#   the canvas can show a placeholder shape with a live progress
#   indicator rather than a 60-second loading spinner. Same shape as
#   the Opendream SSE-tailing-state-file pattern; not blocking the
#   MVP because the synchronous version puts pixels on screen.
