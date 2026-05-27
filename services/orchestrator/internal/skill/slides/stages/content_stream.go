package stages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
)

// ContentStream is the Sprint AD streaming variant of Content().
// Instead of waiting for the worker LLM to write all slides' JSON
// before parsing, it pipes the streamed delta into a json.Decoder and
// emits each completed ContentSlide via onSlide AS SOON as it lands.
//
// The caller (write_content tool) uses onSlide to:
//   - push the slide into SessionState.Deck immediately
//   - emit slides.content event (frontend bumps progress + mounts iframe)
//   - spawn a goroutine to chromedp-render that slide in parallel
//     with the LLM still writing later slides
//
// End result: while the LLM is writing slide 8, slides 1-7 are already
// rendering in chromedp goroutines. Phase 3 wall-time drops 30-50%
// on a 10-slide deck.
//
// Fallback contract: if the streaming JSON parser fails for any reason
// (truncation, malformed delta, decoder error), this function falls
// back to the non-streaming Content() — same retry / cap semantics,
// no per-slide callback fires from the fallback path. Callers MUST
// handle both cases: drain `onSlide` calls during the happy path, and
// iterate `result.Slides` themselves if `onSlide` was never invoked
// (signalled by the streamed counter mismatching result count).
//
// The return shape mirrors Content() — same ContentResult + Usage so
// the tool layer can switch to ContentStream with minimal blast
// radius.
func ContentStream(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	onSlide func(*ContentSlide),
) (*ContentResult, llm.Usage, error) {
	res, usage, err := streamingContent(ctx, router, outline, onSlide)
	if err == nil {
		return res, usage, nil
	}
	// Fall back to non-streaming Content. Logged at WARN because the
	// streaming path SHOULD succeed; if it doesn't, we'd rather know
	// (cost: lose the streaming wall-time win for this one deck).
	slog.Warn("ContentStream: streaming path failed, falling back to non-streaming Content",
		"err", err.Error())
	return Content(ctx, router, outline)
}

// streamingContent is the actual streaming implementation. Returns an
// error on ANY failure so the caller can fall back to non-streaming.
func streamingContent(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	onSlide func(*ContentSlide),
) (*ContentResult, llm.Usage, error) {
	outlineJSON, _ := json.Marshal(outline)
	user := fmt.Sprintf("Outline (JSON):\n%s\n\nProduce the final slide content.", string(outlineJSON))

	client := router.For("worker")

	// io.Pipe bridges the LLM's streaming deltas (writer side) to the
	// json.Decoder (reader side, running in a goroutine).
	pr, pw := io.Pipe()

	var (
		mu        sync.Mutex
		slides    []ContentSlide
		decodeErr error
		fullBuf   strings.Builder // also accumulate full text so we can
		//                         do a last-ditch full-parse if decoder fails.
	)

	decoderDone := make(chan struct{})
	go func() {
		defer close(decoderDone)
		// CRITICAL: close pr when decoder finishes. Otherwise, after
		// the decoder sees the `]` and returns, the pipe writer side
		// (fed by onChunk) keeps trying to Write for any trailing
		// tokens the LLM emits (whitespace, the outer `}`, sometimes
		// a markdown coda). With no reader, writes BLOCK forever →
		// AskToolStream blocks on onChunk → write_content tool blocks
		// → the entire agent loop hangs even though slides streamed
		// in fine. Closing pr here makes subsequent writes return
		// ErrClosedPipe immediately (and onChunk silently swallows
		// the error, which is the right call — the data made it).
		defer pr.Close()
		decodeErr = runJSONDecoder(pr, func(s ContentSlide) {
			mu.Lock()
			slides = append(slides, s)
			mu.Unlock()
			if onSlide != nil {
				onSlide(&s)
			}
		})
	}()

	onChunk := func(delta string) {
		if delta == "" {
			return
		}
		fullBuf.WriteString(delta)
		// Write to pipe; ignore the error — if the decoder has already
		// finished (success or failure) the pipe is closed and writes
		// short-circuit. The decoder's err is the authoritative signal.
		_, _ = pw.Write([]byte(delta))
	}

	resp, streamErr := client.AskToolStream(ctx, llm.AskToolRequest{
		Model:             router.ModelFor("worker"),
		SystemPrompt:      prompts.Content,
		Messages:          []schema.Message{schema.NewUser(user)},
		MaxTokens:         10000,
		EnablePromptCache: true,
	}, onChunk)

	// Signal EOF to decoder. CloseWithError(nil) = EOF, which makes
	// decoder.More() return false at the right place.
	if streamErr != nil {
		_ = pw.CloseWithError(streamErr)
	} else {
		_ = pw.Close()
	}
	<-decoderDone

	if streamErr != nil {
		return nil, llm.Usage{}, fmt.Errorf("content stream: %w", streamErr)
	}

	// Even if the decoder errored mid-stream, try a final full-buffer
	// parse — useful when the LLM wrapped output in ```json fences
	// (which our streaming decoder doesn't strip).
	mu.Lock()
	got := slides
	mu.Unlock()
	if decodeErr != nil || len(got) == 0 {
		var full ContentResult
		raw := stripFences(fullBuf.String())
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &full); err == nil && len(full.Slides) > 0 {
				// Full-parse succeeded. Emit per-slide for the slides
				// we never delivered via streaming.
				if onSlide != nil {
					for i := len(got); i < len(full.Slides); i++ {
						s := full.Slides[i]
						onSlide(&s)
					}
				}
				if resp != nil {
					return &full, resp.Usage, nil
				}
				return &full, llm.Usage{}, nil
			}
		}
		if decodeErr != nil {
			return nil, llm.Usage{}, fmt.Errorf("content stream decode: %w", decodeErr)
		}
		return nil, llm.Usage{}, errors.New("content stream: 0 slides parsed and full buffer was empty")
	}

	if resp != nil {
		return &ContentResult{Slides: got}, resp.Usage, nil
	}
	return &ContentResult{Slides: got}, llm.Usage{}, nil
}

// runJSONDecoder walks the streaming JSON shape:
//
//	{ "slides": [ <slide>, <slide>, ... ] }
//
// Calls onSlide for each complete <slide> object as it arrives. The
// reader is an io.Pipe being fed from the LLM stream — Decode() blocks
// until enough bytes have arrived to parse the next object.
//
// The function ignores any other top-level fields the LLM may emit
// (it just walks to the `slides` array). Anything outside the array
// is discarded.
func runJSONDecoder(r io.Reader, onSlide func(ContentSlide)) error {
	dec := json.NewDecoder(r)

	// Walk forward looking for "slides" key followed by an array open.
	depth := 0
	foundSlides := false
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				if !foundSlides {
					return errors.New("stream ended before reaching `slides` array")
				}
				return nil // clean EOF after we've drained items
			}
			return err
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				depth++
			case '}':
				depth--
			case '[':
				if foundSlides {
					// Enter the slides array. Loop while there's another
					// item, decode it.
					for dec.More() {
						var s ContentSlide
						if err := dec.Decode(&s); err != nil {
							return fmt.Errorf("decode slide: %w", err)
						}
						onSlide(s)
					}
					// Past the closing ']'. After this, we don't care
					// about whatever else the LLM emits.
					return nil
				}
				depth++
			case ']':
				depth--
			}
		case string:
			// Top-level "slides" key — next token after this should be '['.
			if depth == 1 && v == "slides" {
				foundSlides = true
			}
		}
		_ = depth
	}
}
