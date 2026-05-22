package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// DeepSeek talks to the DeepSeek API (api.deepseek.com), which is wire-compatible
// with the OpenAI Chat Completions schema. We reuse `sashabaranov/go-openai`
// with a custom BaseURL — no separate SDK needed.
//
// Set DEEPSEEK_BASE_URL to override (e.g. proxy through Cloudflare). Default
// `https://api.deepseek.com/v1`.
type DeepSeek struct {
	client       *openai.Client
	defaultModel string
}

func NewDeepSeek(apiKey, baseURL, defaultModel string) *DeepSeek {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	cfg.BaseURL = baseURL
	if defaultModel == "" {
		defaultModel = "deepseek-chat"
	}
	return &DeepSeek{client: openai.NewClientWithConfig(cfg), defaultModel: defaultModel}
}

func (d *DeepSeek) Name() string { return "deepseek" }

func (d *DeepSeek) AskTool(ctx context.Context, req llm.AskToolRequest) (*llm.AskToolResponse, error) {
	model := req.Model
	if model == "" {
		model = d.defaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case schema.RoleSystem:
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: m.Content,
			})
		case schema.RoleUser:
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: m.Content,
			})
		case schema.RoleAssistant:
			am := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: m.Content,
			}
			for _, tc := range m.ToolCalls {
				am.ToolCalls = append(am.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: string(tc.Args),
					},
				})
			}
			msgs = append(msgs, am)
		case schema.RoleTool:
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
				Name:       m.Name,
			})
		}
	}

	var tools []openai.Tool
	for _, t := range req.Tools {
		var params map[string]any
		_ = json.Unmarshal(t.Parameters, &params)
		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}

	chatReq := openai.ChatCompletionRequest{
		Model:     model,
		Messages:  msgs,
		MaxTokens: maxTokens,
		Tools:     tools,
	}
	if req.Temperature > 0 {
		chatReq.Temperature = req.Temperature
	}
	switch req.ToolChoice {
	case "required":
		chatReq.ToolChoice = "required"
	case "auto", "":
		// default behaviour; omit field
	default:
		// Pin to a specific tool by name.
		chatReq.ToolChoice = map[string]any{
			"type":     "function",
			"function": map[string]string{"name": req.ToolChoice},
		}
	}

	resp, err := d.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek: empty choices")
	}
	choice := resp.Choices[0]

	// Defensive: DeepSeek occasionally returns choice.Message.Content == ""
	// AND ToolCalls == nil with a 200 status. The OpenAI SDK presents this
	// as a successful response, but downstream parsers (e.g. stages.Outline
	// → json.Unmarshal) then fail with "unexpected end of JSON input".
	//
	// Treat as a transient error so the caller can retry. Include
	// finish_reason to help debug — common values:
	//   - "length"        → MaxTokens hit before content produced
	//   - "content_filter" → safety filter ate the response
	//   - "stop" / ""     → genuinely empty model output (transient)
	if strings.TrimSpace(choice.Message.Content) == "" && len(choice.Message.ToolCalls) == 0 {
		return nil, fmt.Errorf(
			"deepseek: empty content (finish_reason=%q, prompt_tokens=%d, completion_tokens=%d) — usually transient",
			choice.FinishReason,
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
		)
	}

	out := &llm.AskToolResponse{
		Content: choice.Message.Content,
		Usage: llm.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			// DeepSeek exposes cache stats via `prompt_cache_hit_tokens`
			// when the prompt-caching feature triggers. We surface it
			// through the same Usage.CacheReadTokens field so billing math
			// works the same way as Anthropic.
			CacheReadTokens: extractCacheHit(resp.Usage),
		},
		Raw: resp,
	}
	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, schema.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		})
	}
	return out, nil
}

// extractCacheHit reads the non-standard `prompt_cache_hit_tokens` field
// DeepSeek adds to the usage object. The OpenAI Go SDK doesn't model it, so
// we round-trip through JSON to read it without breaking when DeepSeek
// removes / renames it later.
func extractCacheHit(u openai.Usage) int {
	b, err := json.Marshal(u)
	if err != nil {
		return 0
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return 0
	}
	if v, ok := raw["prompt_cache_hit_tokens"].(float64); ok {
		return int(v)
	}
	return 0
}
