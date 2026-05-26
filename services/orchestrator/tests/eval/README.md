# Slides / video / design regression eval

Sprint X2c seeded this harness so future LLM model upgrades (DeepSeek
v4.1, switching to Claude, etc.) don't quietly degrade output quality.

## Layout

```
tests/eval/
  diff.go              Structural-diff comparator + Expectation type
  diff_test.go         Unit tests for the comparator (CI gates these)
  golden/
    <slug>.spec.json       Input — the same shape POST /api/v1/slides accepts
    <slug>.expected.json   Expectation — see Expectation struct in diff.go
  eval_live_test.go    (TODO Phase 4 follow-up) drives slides.Pipeline.Run
                       against every golden spec when EVAL_DEEPSEEK_API_KEY is set
```

## What's intentionally NOT compared

- **Slide text content** — LLM output varies; pinning exact strings
  would break on every model nudge.
- **Image URLs** — Nano-banana / Unsplash return different URLs each
  call; nothing useful to compare.
- **PPTX bytes** — chromedp screenshot pixels move on browser updates.

## What IS compared

- **slide_count ±tolerance** — catches "planner started returning
  3-slide decks for 8-slide requests".
- **layout distribution** — catches "everything became bullets" (the
  Sprint G / L regression mode).
- **required layouts present** — catches "planner dropped the metric
  / closing layout entirely".
- **title non-empty** — catches "planner returned blank headlines".

## Running

```bash
# Unit tests on the diff harness itself (always runs, CI-gated):
cd tests/eval && go test ./...

# Live eval against actual LLM (opt-in, costs DeepSeek tokens):
EVAL_DEEPSEEK_API_KEY=sk-... go test -run TestEvalLive ./tests/eval/
```

## Adding a fixture

1. Drop a `<slug>.spec.json` matching `slides.Input` shape.
2. Drop a `<slug>.expected.json` matching `Expectation` in diff.go.
3. Run the live test against it to confirm the expectation passes.
4. Commit both files. CI's structural diff will catch future drift.
