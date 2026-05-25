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

### Synchronous (block until task completes; 30-60 s typical)

| Method | Path                  | Description                                      |
| ------ | --------------------- | ------------------------------------------------ |
| GET    | `/healthz`            | Liveness check                                   |
| POST   | `/generate/image`     | Flux text2image — `{prompt, width?, height?, seed?}` → `{url, width, height, task_id}` |
| POST   | `/generate/variants`  | Same prompt, N variants — `{prompt, count?, width?, height?}` → `{variants: [{url, width, height}], task_id}` |
| POST   | `/edit/remove_bg`     | Cut background → PNG with alpha. Body `{image_url}` |
| POST   | `/edit/enhance`       | Super-resolution + sharpen. Body `{image_url}` |
| POST   | `/edit/outpaint`      | Extend borders. Body `{image_url, left?, right?, top?, bottom?}` (at least one > 0) |
| POST   | `/edit/image2image`   | Transform via prompt. Body `{image_url, prompt, width?, height?}` |

### SSE flow (in-canvas progress)

| Method | Path                                       | Description                              |
| ------ | ------------------------------------------ | ---------------------------------------- |
| POST   | `/generate/image/submit`                   | Submit text2image; returns `{task_id}` immediately |
| GET    | `/generate/image/{task_id}/events`         | SSE stream: `progress` ticks then terminal `done`/`error` |

The SSE polling lives inside the stream handler so a browser
disconnect immediately stops upstream polling — no zombie tasks.

## Auth

DreamAPI key picked up from `DREAMAPI_API_KEY` env var, with
`~/.dreamapi/credentials.json` (the dreamapi-skill credentials file)
as a fallback. Missing key → clean 503 with a setup hint.

Get a key at <https://api.newportai.com/>.

## Roadmap

- [x] `POST /edit/remove_bg`
- [x] `POST /edit/enhance`
- [x] `POST /generate/variants` — N variants of one prompt
- [x] `POST /edit/outpaint` — extend image borders (useful for aspect-ratio changes)
- [x] `POST /edit/image2image` — transform image via prompt
- [x] `POST /generate/image/submit` + `GET /generate/image/{task_id}/events` (SSE) for in-place progress
- [ ] `POST /edit/inpaint` — fill a masked region from a prompt (needs canvas mask UI)
- [ ] SSE variants for the other long ops (variants, outpaint, image2image)
- [ ] Cost metering — accept upstream user/credit context and return
      per-task cost so the orchestrator can debit before responding
