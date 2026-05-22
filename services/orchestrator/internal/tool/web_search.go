package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// TavilySearch wraps the Tavily Search API and exposes it as a tool. Tavily
// returns LLM-friendly snippets without the typical scraping/parsing chore.
type TavilySearch struct {
	APIKey string
	client *http.Client
}

func NewTavilySearch(apiKey string) *TavilySearch {
	return &TavilySearch{APIKey: apiKey, client: &http.Client{Timeout: 20 * time.Second}}
}

func (TavilySearch) Name() string { return "web_search" }

func (TavilySearch) Description() string {
	return "Search the public web for up-to-date information. Returns a list of titles, URLs, and snippets that the agent can quote with citations."
}

func (TavilySearch) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query":       {"type": "string", "description": "Search query"},
			"max_results": {"type": "integer", "default": 5, "minimum": 1, "maximum": 10},
			"depth":       {"type": "string", "enum": ["basic", "advanced"], "default": "basic"}
		},
		"required": ["query"]
	}`)
}

type tavilyReq struct {
	APIKey     string `json:"api_key"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	SearchDepth string `json:"search_depth,omitempty"`
}

type tavilyResp struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (t TavilySearch) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Depth      string `json:"depth"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	if p.Query == "" {
		return schema.ToolResult{Error: "query is required"}, nil
	}
	if p.MaxResults == 0 {
		p.MaxResults = 5
	}
	if t.APIKey == "" {
		return schema.ToolResult{Error: "TAVILY_API_KEY not configured"}, nil
	}

	body, _ := json.Marshal(tavilyReq{
		APIKey:      t.APIKey,
		Query:       p.Query,
		MaxResults:  p.MaxResults,
		SearchDepth: p.Depth,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return schema.ToolResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return schema.ToolResult{Error: fmt.Sprintf("tavily %d: %s", resp.StatusCode, buf.String())}, nil
	}

	var out tavilyResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return schema.ToolResult{Error: "decode: " + err.Error()}, nil
	}

	var b bytes.Buffer
	for i, r := range out.Results {
		fmt.Fprintf(&b, "[%d] %s\n%s\n%s\n\n", i+1, r.Title, r.URL, r.Content)
	}
	return schema.ToolResult{Output: b.String()}, nil
}
