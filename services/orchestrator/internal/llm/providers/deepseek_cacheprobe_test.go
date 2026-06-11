package providers

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	openai "github.com/sashabaranov/go-openai"
)

// TestDeepSeekCacheProbe settles ONE question before we build cache-warming:
// does DeepSeek's automatic prompt cache actually hit on THIS deployment?
//
// The mode=svg author bills author_in≈90K/deck because cache_read is observed
// as 0 — but with concurrency-5 the cache may simply never warm (all 5 first
// calls miss in parallel). This probe removes that confound: two IDENTICAL
// calls, strictly sequential, with a large stable system prompt. Call 1 writes
// the cache; call 2 must read it IF caching works at all here.
//
//	cache_read>0 on call 2  → warming is viable → implement it.
//	cache_read==0 on call 2 → the proxy/deployment doesn't forward caching →
//	                          warming is futile, fall back to other levers.
//
// Skips unless DEEPSEEK_API_KEY is set (real network call, ~2× a few seconds).
// Run: DEEPSEEK_API_KEY=… go test ./internal/llm/providers -run CacheProbe -v
func TestDeepSeekCacheProbe(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY unset — skipping live cache probe")
	}
	d := NewDeepSeek(key, os.Getenv("DEEPSEEK_BASE_URL"), "deepseek-v4-flash")

	// ~8K-token stable prefix — well over DeepSeek's 64-token cache floor and
	// in the same ballpark as the 34KB SVGMaster author prompt. Identical bytes
	// across both calls so call 2 is a pure cache-read candidate.
	sys := strings.Repeat(
		"You are a meticulous SVG slide designer working on a 1920x1080 canvas. "+
			"Follow the layout rules exactly, keep every text element inside its card, "+
			"reserve the accent colour for a single focal point, and never split a word "+
			"across two lines. Emit only valid SVG. ", 500)

	req := llm.AskToolRequest{
		Model:             "deepseek-v4-flash",
		SystemPrompt:      sys,
		Messages:          []schema.Message{schema.NewUser("Reply with the single word: OK")},
		MaxTokens:         200,
		EnablePromptCache: true,
	}

	for i := 1; i <= 2; i++ {
		resp, err := d.AskTool(context.Background(), req)
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		// Our mapped value (known-suspect: extractCacheHit reads the wrong key).
		t.Logf("call %d MAPPED: input=%d output=%d cache_read=%d",
			i, resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadTokens)
		// Ground truth straight from DeepSeek's raw usage object.
		if raw, ok := resp.Raw.(openai.ChatCompletionResponse); ok {
			cached := 0
			if raw.Usage.PromptTokensDetails != nil {
				cached = raw.Usage.PromptTokensDetails.CachedTokens
			}
			usageJSON, _ := json.Marshal(raw.Usage)
			t.Logf("call %d RAW: prompt_tokens_details.cached_tokens=%d  full_usage=%s",
				i, cached, string(usageJSON))
		}
	}
	t.Log("VERDICT: if call 2 RAW cached_tokens>0 → DeepSeek caches when warm → warming viable (and our extractCacheHit is just reading the wrong field)")
}
