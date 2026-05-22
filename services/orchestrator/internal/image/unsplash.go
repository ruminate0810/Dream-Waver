// Package image provides photo-search backends the slides pipeline calls
// when a slide has an ImageQuery. Unsplash is the primary backend (Gamma's
// default); the interface accepts any source so we can add Pexels/Pixabay
// later without touching call sites.
package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Result is the minimum the pipeline needs from a search backend.
type Result struct {
	URL    string // direct image URL to embed (typically "regular" size, ~1080w)
	Credit string // attribution text — required by Unsplash TOS
}

// Searcher abstracts an image source. NoopSearcher is the safe default
// when no API key is configured.
type Searcher interface {
	Search(ctx context.Context, query string) (*Result, error)
}

// NoopSearcher returns (nil, nil) on every search. Used when there's no
// API key — templates simply render without a hero image.
type NoopSearcher struct{}

func (NoopSearcher) Search(context.Context, string) (*Result, error) { return nil, nil }

// Unsplash searches via api.unsplash.com/search/photos. Free dev tier =
// 50 requests/hour, plenty for early users since we cache results per
// keyword inside one job.
type Unsplash struct {
	AccessKey string
	client    *http.Client
}

func NewUnsplash(accessKey string) *Unsplash {
	return &Unsplash{
		AccessKey: accessKey,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

type unsplashResp struct {
	Results []struct {
		URLs struct {
			Regular string `json:"regular"`
		} `json:"urls"`
		User struct {
			Name  string `json:"name"`
			Links struct {
				HTML string `json:"html"`
			} `json:"links"`
		} `json:"user"`
		AltDescription string `json:"alt_description"`
	} `json:"results"`
}

// Search returns the first matching photo (Unsplash ranks by relevance).
// Returns (nil, nil) — not an error — when the query yields no results
// so the pipeline can render the slide without a hero image.
func (u *Unsplash) Search(ctx context.Context, query string) (*Result, error) {
	if u.AccessKey == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("per_page", "1")
	q.Set("orientation", "landscape")
	q.Set("content_filter", "high")

	endpoint := "https://api.unsplash.com/search/photos?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Client-ID "+u.AccessKey)
	req.Header.Set("Accept-Version", "v1")

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unsplash: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		// Rate-limit; degrade gracefully.
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unsplash %d: %s", resp.StatusCode, string(body))
	}

	var out unsplashResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("unsplash decode: %w", err)
	}
	if len(out.Results) == 0 {
		return nil, nil
	}
	r := out.Results[0]
	credit := fmt.Sprintf("Photo by %s on Unsplash", r.User.Name)
	return &Result{URL: r.URLs.Regular, Credit: credit}, nil
}
