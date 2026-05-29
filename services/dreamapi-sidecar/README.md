# dreamapi-sidecar

FastAPI front for AI image + video generation. Lives inside the monorepo
because it only exists to serve Dream-Waver's `/api/v1/design/*` surface —
no independent product.

Bridges **two** upstreams:

- **df-ability gateway** (`http://38.98.112.79`) — Google Gemini image
  generation (NanoBanana) + Seedance 1.5 Pro image-to-video. This is the
  primary, currently-wired path for the `/design` canvas.
- **DreamAPI** ([api.newportai.com](https://api.newportai.com/)) — Flux
  text2image + the classic edit ops (remove_bg / enhance / outpaint /
  colorize / image2image). Optional; only lights up when a key is set.

## Run

```bash
# from the repo root — sources .env so DF_ABILITY_* + DREAMAPI_API_KEY
# are inherited:
make sidecar

# or manually:
cd services/dreamapi-sidecar
pip install -r requirements.txt
DF_ABILITY_ACCESS_KEY=… DF_ABILITY_SECRET_KEY=… \
  python3 -m uvicorn main:app --host 127.0.0.1 --port 8091 --reload
```

## API surface

### df-ability — NanoBanana + Seedance (primary)

| Method | Path                   | Description                                      |
| ------ | ---------------------- | ------------------------------------------------ |
| POST   | `/generate/nano_banana`| Google Gemini image gen. Body `{prompt, model?, image_size?, aspect_ratio?, images?}`. `images[]` accepts http(s) URLs **or** `data:image/…;base64,…` data URLs (forwarded as native Gemini `inlineData`). → `{url, width, height, task_id}` |
| POST   | `/video/seedance_i2v`  | Seedance 1.5 Pro image-to-video. Body `{image_url, prompt, resolution?, ratio?, duration?, seed?}` → `{video_url, task_id}` |

### DreamAPI — Flux + edits (optional, needs `DREAMAPI_API_KEY`)

| Method | Path                  | Description                                      |
| ------ | --------------------- | ------------------------------------------------ |
| GET    | `/healthz`            | Liveness check                                   |
| POST   | `/generate/image`     | Flux text2image — `{prompt, width?, height?, seed?}` → `{url, width, height, task_id}` |
| POST   | `/generate/variants`  | Same prompt, N variants — `{prompt, count?, width?, height?}` → `{variants: [{url, width, height}], task_id}` |
| POST   | `/edit/remove_bg`     | Cut background → PNG with alpha. Body `{image_url}` |
| POST   | `/edit/enhance`       | Super-resolution + sharpen. Body `{image_url}` |
| POST   | `/edit/colorize`      | Colourise a B&W photo (requires a human face). Body `{image_url}` |
| POST   | `/edit/outpaint`      | Extend borders. Body `{image_url, left?, right?, top?, bottom?}` (at least one > 0) |
| POST   | `/edit/image2image`   | Transform via prompt. Body `{image_url, prompt, width?, height?}` |

### SSE flow (in-canvas progress, DreamAPI Flux)

| Method | Path                                       | Description                              |
| ------ | ------------------------------------------ | ---------------------------------------- |
| POST   | `/generate/image/submit`                   | Submit text2image; returns `{task_id}` immediately |
| GET    | `/generate/image/{task_id}/events`         | SSE stream: `progress` ticks then terminal `done`/`error` |

The SSE polling lives inside the stream handler so a browser
disconnect immediately stops upstream polling — no zombie tasks.

> Note: the canvas chat now drives NanoBanana (synchronous, with a
> client-side "Generating · Ns" placeholder) rather than the Flux SSE
> path. The SSE endpoints stay wired for the Flux fallback.

## Auth

Two independent credential sets, each gated per-upstream:

- **df-ability**: `DF_ABILITY_ACCESS_KEY` + `DF_ABILITY_SECRET_KEY`
  (optionally `DF_ABILITY_BASE_URL`, default `http://38.98.112.79`).
  Missing → the `/generate/nano_banana` + `/video/seedance_i2v` routes
  return a clean 503 with a setup hint.
- **DreamAPI**: `DREAMAPI_API_KEY` env var, with
  `~/.dreamapi/credentials.json` (the dreamapi-skill credentials file)
  as a fallback. Missing → the Flux/edit routes return 503. Get a key
  at <https://api.newportai.com/>.

The Go orchestrator points at this service via `DREAMAPI_SIDECAR_URL`
(default `http://localhost:8091`); when unset, every `/api/v1/design/*`
route 503s with that hint.

## Roadmap

- [x] `POST /edit/remove_bg`
- [x] `POST /edit/enhance`
- [x] `POST /edit/colorize`
- [x] `POST /generate/variants` — N variants of one prompt
- [x] `POST /edit/outpaint` — extend image borders (useful for aspect-ratio changes)
- [x] `POST /edit/image2image` — transform image via prompt
- [x] `POST /generate/image/submit` + `GET /generate/image/{task_id}/events` (SSE)
- [x] `POST /generate/nano_banana` — Gemini image gen with URL **or** base64 refs
- [x] `POST /video/seedance_i2v` — Seedance 1.5 Pro image-to-video
- [ ] `POST /edit/inpaint` — fill a masked region from a prompt (needs canvas mask UI)
- [ ] SSE variants for the long df-ability ops (NanoBanana, Seedance)
- [ ] Cost metering — accept upstream user/credit context and return
      per-task cost so the orchestrator can debit before responding
