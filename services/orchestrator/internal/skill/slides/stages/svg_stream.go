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
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/themetokens"
)

// AuthorSVGStream is the streaming variant of AuthorSVG (Sprint PM). It
// pipes the planner LLM's streamed deltas into a JSON decoder and invokes
// onSlide for each slide AS SOON as its <svg> lands — so the caller can
// render + emit that slide immediately while the LLM is still writing the
// rest. The deck fills in one page at a time in the live preview instead
// of the user staring at a 3-5 minute black box.
//
// Fallback contract (mirrors ContentStream): on ANY streaming failure
// (truncation, malformed delta, decoder error) it falls back to the
// non-streaming AuthorSVG — same prompt, same result shape. Callers must
// handle both: drain onSlide on the happy path, and iterate result.Slides
// if the streamed count ends up short of the result.
func AuthorSVGStream(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	theme string,
	onSlide func(*ContentSlide),
) (*ContentResult, llm.Usage, error) {
	res, usage, err := streamingAuthorSVG(ctx, router, outline, theme, onSlide)
	if err == nil {
		return res, usage, nil
	}
	slog.Warn("AuthorSVGStream: streaming path failed, falling back to non-streaming AuthorSVG",
		"err", err.Error())
	return AuthorSVG(ctx, router, outline, theme)
}

// svgUserMessage builds the user turn shared by AuthorSVG and the stream:
// the deck title + each outline slide's type/headline/points/note.
func svgUserMessage(outline *OutlineResult, theme string) string {
	var user strings.Builder
	fmt.Fprintf(&user, "Deck title: %s\nTheme: %s\n\nSlides:\n", outline.Title, theme)
	for _, s := range outline.Slides {
		fmt.Fprintf(&user, "\n%d. [%s] %s\n", s.Index, s.Type, s.Headline)
		if len(s.KeyPoints) > 0 && string(s.KeyPoints) != "null" {
			fmt.Fprintf(&user, "   points: %s\n", string(s.KeyPoints))
		}
		if strings.TrimSpace(s.SpeakerNotes) != "" {
			fmt.Fprintf(&user, "   note: %s\n", s.SpeakerNotes)
		}
	}
	return user.String()
}

func streamingAuthorSVG(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	theme string,
	onSlide func(*ContentSlide),
) (*ContentResult, llm.Usage, error) {
	tok := themetokens.Get(theme)
	sys := renderSVGPrompt(tok)
	user := svgUserMessage(outline, theme)
	client := router.For("planner")

	pr, pw := io.Pipe()

	var (
		mu        sync.Mutex
		slides    []ContentSlide
		decodeErr error
		fullBuf   strings.Builder
	)

	decoderDone := make(chan struct{})
	go func() {
		defer close(decoderDone)
		// Close the reader when the decoder finishes so trailing tokens
		// the LLM emits after the `]` don't block the pipe writer (the
		// ContentStream hang lesson — see content_stream.go).
		defer pr.Close()
		decodeErr = runSVGJSONDecoder(pr, theme, func(s ContentSlide) {
			mu.Lock()
			s.Index = len(slides) + 1
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
		_, _ = pw.Write([]byte(delta))
	}

	resp, streamErr := client.AskToolStream(ctx, llm.AskToolRequest{
		Model:             router.ModelFor("planner"),
		SystemPrompt:      sys,
		Messages:          []schema.Message{schema.NewUser(user)},
		MaxTokens:         28000,
		EnablePromptCache: true,
	}, onChunk)

	if streamErr != nil {
		_ = pw.CloseWithError(streamErr)
	} else {
		_ = pw.Close()
	}
	<-decoderDone

	if streamErr != nil {
		return nil, llm.Usage{}, fmt.Errorf("svg author stream: %w", streamErr)
	}

	mu.Lock()
	got := slides
	mu.Unlock()

	// Last-ditch full-buffer parse when the streaming decoder failed or
	// got nothing (e.g. the LLM wrapped the JSON in ```fences).
	if decodeErr != nil || len(got) == 0 {
		var full struct {
			Slides []struct {
				SVG string `json:"svg"`
			} `json:"slides"`
		}
		raw := stripFences(fullBuf.String())
		if len(raw) > 0 && json.Unmarshal(raw, &full) == nil && len(full.Slides) > 0 {
			var res []ContentSlide
			for i, s := range full.Slides {
				svg := strings.TrimSpace(s.SVG)
				if svg == "" {
					continue
				}
				cs := ContentSlide{Index: i + 1, Template: theme, Layout: schema.LayoutSVG, Data: schema.SlideData{SVG: svg}}
				res = append(res, cs)
				// Emit only the slides streaming never delivered.
				if onSlide != nil && i >= len(got) {
					onSlide(&cs)
				}
			}
			if len(res) > 0 {
				if resp != nil {
					return &ContentResult{Slides: res}, resp.Usage, nil
				}
				return &ContentResult{Slides: res}, llm.Usage{}, nil
			}
		}
		if decodeErr != nil {
			return nil, llm.Usage{}, fmt.Errorf("svg author stream decode: %w", decodeErr)
		}
		return nil, llm.Usage{}, errors.New("svg author stream: 0 slides parsed")
	}

	if resp != nil {
		return &ContentResult{Slides: got}, resp.Usage, nil
	}
	return &ContentResult{Slides: got}, llm.Usage{}, nil
}

// runSVGJSONDecoder walks { "slides": [ {"svg":"…"}, … ] } and calls
// onSlide for each complete object as it streams in. Mirrors
// runJSONDecoder but decodes the SVG slide shape.
func runSVGJSONDecoder(r io.Reader, theme string, onSlide func(ContentSlide)) error {
	dec := json.NewDecoder(r)
	depth := 0
	foundSlides := false
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				if !foundSlides {
					return errors.New("stream ended before reaching `slides` array")
				}
				return nil
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
					for dec.More() {
						var obj struct {
							SVG string `json:"svg"`
						}
						if err := dec.Decode(&obj); err != nil {
							return fmt.Errorf("decode svg slide: %w", err)
						}
						svg := strings.TrimSpace(obj.SVG)
						if svg == "" {
							continue
						}
						onSlide(ContentSlide{
							Template: theme,
							Layout:   schema.LayoutSVG,
							Data:     schema.SlideData{SVG: svg},
						})
					}
					return nil
				}
				depth++
			case ']':
				depth--
			}
		case string:
			if depth == 1 && v == "slides" {
				foundSlides = true
			}
		}
	}
}
