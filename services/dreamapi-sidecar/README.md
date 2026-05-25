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

| Method | Path                  | Description                                      |
| ------ | --------------------- | ------------------------------------------------ |
| GET    | `/healthz`            | Liveness check                                   |
| POST   | `/generate/image`     | Flux text2image — body `{prompt, width?, height?, seed?}`; returns `{url, width, height, task_id}` |
| POST   | `/generate/variants`  | Same prompt, N variants — body `{prompt, count?, width?, height?}`; returns `{variants: [{url, width, height}], task_id}` |
| POST   | `/edit/remove_bg`     | Cut background — body `{image_url}`; returns `{url, width?, height?, task_id}` (output is PNG with alpha) |
| POST   | `/edit/enhance`       | Super-resolution + sharpen — body `{image_url}`; returns same shape as remove_bg |

Every endpoint is synchronous (~30-60 s typical). SSE-based progress
streaming is the roadmap, tracked in a `TODO(progress)` at the bottom
of `main.py`.

## Auth

DreamAPI key picked up from `DREAMAPI_API_KEY` env var, with
`~/.dreamapi/credentials.json` (the dreamapi-skill credentials file)
as a fallback. Missing key → clean 503 with a setup hint.

Get a key at <https://api.newportai.com/>.

## Roadmap

- [x] `POST /edit/remove_bg`
- [x] `POST /edit/enhance`
- [x] `POST /generate/variants` — N variants of one prompt
- [ ] `POST /edit/inpaint` — fill a masked region from a prompt
- [ ] `POST /edit/outpaint` — extend image borders (useful for aspect-ratio changes)
- [ ] `POST /generate/image/submit` + `GET /generate/image/{task_id}/events` (SSE) for in-place progress
- [ ] Cost metering — accept upstream user/credit context and return
      per-task cost so the orchestrator can debit before responding
