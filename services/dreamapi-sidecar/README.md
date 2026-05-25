# dreamapi-sidecar

FastAPI front for [DreamAPI](https://api.newportai.com/) image
generation. Lives inside the monorepo because it only exists to serve
Dream-Waver's `/api/v1/design/*` surface — no independent product.

## Run

```bash
cd services/dreamapi-sidecar
pip install -r requirements.txt
DREAMAPI_API_KEY=sk-...  uvicorn main:app --port 8091 --reload
```

## API surface

| Method | Path                | Description                                      |
| ------ | ------------------- | ------------------------------------------------ |
| GET    | `/healthz`          | Liveness check                                   |
| POST   | `/generate/image`   | Flux text2image — body `{prompt, width?, height?, seed?}`; returns `{url, width, height, task_id}`. Synchronous (~30-60 s). |

`POST /generate/image` is intentionally synchronous for MVP — the
canvas just needs pixels on screen and a single straight-line call is
the smallest viable surface. SSE-based progress streaming is the
roadmap, tracked in a `TODO(progress)` at the bottom of `main.py`.

## Auth

DreamAPI key picked up from `DREAMAPI_API_KEY` env var, with
`~/.dreamapi/credentials.json` (the dreamapi-skill credentials file)
as a fallback. Missing key → clean 503 with a setup hint.

Get a key at <https://api.newportai.com/>.

## Roadmap

- [ ] `POST /generate/image/submit` + `GET /generate/image/{task_id}/events` (SSE) for in-place progress
- [ ] `POST /edit/remove_bg` (wraps DreamAPI matting)
- [ ] `POST /edit/enhance` (wraps DreamAPI upscaler)
- [ ] `POST /generate/image_variants` — N variants of one prompt
- [ ] Cost metering — accept upstream user/credit context and return
      per-task cost so the orchestrator can debit before responding
