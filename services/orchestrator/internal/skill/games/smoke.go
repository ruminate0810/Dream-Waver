// Smoke-test the generated HTML in a real headless browser to catch
// runtime errors the static validator can't see (e.g. ReferenceErrors,
// TypeErrors thrown from requestAnimationFrame callbacks, unhandled
// promise rejections, console.error spam from broken event handlers).
//
// The whole thing runs in ≤ 5 s with hard timeouts so a failure here
// never blocks a turn for long. If chromedp can't launch (no chrome
// installed in the deployment image, etc.) the caller gets an error
// and should treat smoke as "skipped" rather than "failed" — we do
// NOT want to penalise the model for an infra issue.
//
// We capture errors via chromedp's CDP event listener rather than by
// mutating the HTML, so the artifact the user gets is exactly the
// artifact the model produced. No injection.

package games

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// SmokeTest renders the HTML, lets it run for ~2.5 s, and returns any
// runtime exceptions / console.error messages observed. Returns
// (nil, nil) when the HTML booted cleanly; (errs, nil) when there are
// issues to surface to the LLM; (nil, err) when smoke itself failed
// (no chrome available, timeout, etc.) — caller should fall through
// rather than blame the model.
func SmokeTest(ctx context.Context, html string) ([]string, error) {
	if strings.TrimSpace(html) == "" {
		return nil, fmt.Errorf("empty html")
	}

	// Hard cap regardless of caller's ctx so a wedged browser never
	// drags the whole generation turn past its deadline.
	smokeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		// Audio output is the most common reason chrome hangs in CI;
		// silencing it doesn't change error detection because Web
		// Audio errors still surface as JS exceptions.
		chromedp.Flag("mute-audio", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(smokeCtx, allocOpts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	var (
		mu       sync.Mutex
		captured []string
	)
	push := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return
		}
		// First line only — stack traces from JS engines are noisy
		// and the first line is enough for the model to act on.
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, msg)
	}

	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails == nil {
				return
			}
			d := e.ExceptionDetails
			msg := d.Text
			if d.Exception != nil && d.Exception.Description != "" {
				msg = d.Exception.Description
			}
			push("Uncaught: " + msg)
		case *runtime.EventConsoleAPICalled:
			if e.Type != "error" {
				return
			}
			parts := make([]string, 0, len(e.Args))
			for _, a := range e.Args {
				if a == nil {
					continue
				}
				if a.Description != "" {
					parts = append(parts, a.Description)
				} else if len(a.Value) > 0 {
					parts = append(parts, strings.Trim(string(a.Value), `"`))
				}
			}
			if len(parts) > 0 {
				push("console.error: " + strings.Join(parts, " "))
			}
		}
	})

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))

	// runtime.Enable() turns on the CDP runtime domain so EventException
	// + EventConsoleAPICalled actually fire. Without it the listener
	// would silently never receive anything.
	if err := chromedp.Run(browserCtx,
		runtime.Enable(),
		chromedp.Navigate(dataURL),
		// 2.5 s is long enough for the start screen to render and the
		// first few requestAnimationFrame ticks to fire, but short
		// enough that a clean game never holds the request open.
		chromedp.Sleep(2500*time.Millisecond),
	); err != nil {
		return nil, fmt.Errorf("smoke: %w", err)
	}

	mu.Lock()
	defer mu.Unlock()
	return dedupClip(captured, 5), nil
}

// dedupClip removes duplicate error lines (browsers love to repeat the
// same exception every RAF tick) and clips to the first n unique
// messages so the retry hint stays bounded.
func dedupClip(items []string, n int) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, n)
	for _, s := range items {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) >= n {
			break
		}
	}
	return out
}
