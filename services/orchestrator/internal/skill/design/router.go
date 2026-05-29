package design

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Smart chat routing for /design — given a free-text user message and
// optional selection context, classify into one of the six design
// tools and return structured args. Implemented as a single-shot LLM
// call using the provider's native tool-calling API rather than the
// full ReAct ToolCallAgent loop — we just need one classification
// decision, not a multi-step plan.
//
// Why bother (vs always-generate): users now say things like "make it
// darker" or "animate this" — the canvas has the context (a selected
// image) that turns those into Reimagine / Animate calls. Without a
// router the chat does literal-generate, which 90% of the time is
// wrong when a selection exists.
//
// Fallback: any error / low-confidence outcome → DesignIntent{Tool:
// "generate"} with the original prompt verbatim. Calling code can
// trust this never panics — worst case is a generate.

// DesignIntent is what the router returns. Tool is the action name
// the frontend should dispatch to (matching existing endpoint
// surfaces). Args carries tool-specific parameters the model filled
// in. Confidence is a self-reported 0..1 score that lets the
// frontend decide whether to surface the routing decision (e.g.
// "Routed to: Reimagine" chip) or hide it (low confidence — just
// run it silently with generate fallback).
type DesignIntent struct {
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args"`
	Confidence float64        `json:"confidence"`
	// Rationale is a short human-readable explanation surfaced to the
	// user via a tooltip on the "Routed to" chip. Helps users learn
	// what kind of phrasing triggers which tool.
	Rationale string `json:"rationale"`
}

// RouteContext is what the API handler hands the router — the user's
// message plus whatever canvas context exists. SelectedURL == "" means
// no AI image is currently selected. When unset, intents that need a
// selection (reimagine / animate / quick_edit) are dropped from the
// tool catalogue so the model can't pick something the frontend can't
// then execute.
type RouteContext struct {
	Message     string
	SelectedURL string
	// Recent prompts — the last 0-3 prompts the user has typed in the
	// chat. Helps the model disambiguate "another one" / "different
	// style" type messages. Order: newest last.
	RecentPrompts []string
	// ActiveSkillID is the chat's currently-selected design task scaffold
	// (e.g. "poster-event", "character-bust"). Empty when the user is
	// in free-form mode. Surfaced verbatim to the planner LLM so it
	// can pick a more specific intent (e.g. user typing "darker" with
	// poster-event active → reimagine, not generate).
	ActiveSkillID string
	// SkillHint is the skill's `routerHint` string — passed by the
	// frontend so the backend doesn't need to know the full skill
	// catalogue. 1-2 sentences describing what the skill expects.
	SkillHint string
}

// ToolGenerate is the default fallback — text-to-image NanoBanana.
const (
	ToolGenerate    = "generate"
	ToolVariants    = "variants"
	ToolReimagine   = "reimagine"
	ToolAnimate     = "animate"
	ToolQuickEdit   = "quick_edit"
	ToolExtendCanvas = "extend_canvas"
)

// RouteMessage runs the classification. ctx should already have a
// reasonable deadline (the API handler caps at 2-3 s); on timeout we
// silently fall back to the generate intent so the user's request
// always proceeds.
func RouteMessage(
	ctx context.Context,
	router llm.Router,
	rc RouteContext,
) DesignIntent {
	fallback := DesignIntent{
		Tool:       ToolGenerate,
		Args:       map[string]any{"prompt": rc.Message},
		Confidence: 1.0,
		Rationale:  "default to generate when no router or no signal",
	}
	if router == nil {
		return fallback
	}
	if strings.TrimSpace(rc.Message) == "" {
		return fallback
	}

	// Cap the routing call at 5s — most provider chat completions
	// finish in <2s for this short prompt. Beyond that, fall through.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req := llm.AskToolRequest{
		// Planner tier — cheap + fast, doesn't need to be smart.
		Model:        router.ModelFor("planner"),
		SystemPrompt: routerSystemPrompt(rc),
		Messages: []schema.Message{
			schema.NewUser(routerUserMessage(rc)),
		},
		Tools:       designRouterTools(rc),
		ToolChoice:  "auto",
		MaxTokens:   400,
		Temperature: 0.1, // near-deterministic
	}

	client := router.For("planner")
	resp, err := client.AskTool(ctx, req)
	if err != nil {
		slog.WarnContext(ctx, "design router LLM call failed", "err", err)
		return fallback
	}
	if len(resp.ToolCalls) == 0 {
		// Model declined to call a tool — probably wanted to chat.
		// Fall back to generate with the message as the prompt.
		return fallback
	}

	intent, err := decodeRouterToolCall(resp.ToolCalls[0], rc)
	if err != nil {
		slog.WarnContext(ctx, "design router tool decode failed", "err", err)
		return fallback
	}
	return intent
}

// designRouterTools returns the tool catalog the model picks from.
// Tools that require a selection are filtered out when there isn't
// one — model would otherwise be tempted to pick "reimagine" with
// no canvas image to operate on, leading to a useless decision.
func designRouterTools(rc RouteContext) []schema.ToolSchema {
	hasSelection := strings.TrimSpace(rc.SelectedURL) != ""

	mustParams := func(p map[string]any) json.RawMessage {
		raw, err := json.Marshal(p)
		if err != nil {
			// Static schemas — marshaling can't fail in practice.
			panic(err)
		}
		return raw
	}

	tools := []schema.ToolSchema{
		{
			Name:        ToolGenerate,
			Description: "Create a new image from scratch using the prompt. The default when the user describes a fresh subject (e.g. 'a sunset over mountains', 'a logo for a coffee shop').",
			Parameters: mustParams(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Refined prompt to send to the image model. May elaborate the user's terse request.",
					},
				},
				"required": []string{"prompt"},
			}),
		},
	}

	if hasSelection {
		tools = append(tools, schema.ToolSchema{
			Name:        ToolReimagine,
			Description: "Transform the currently-selected image based on the user's instruction. Use when the message references 'this', 'this image', 'darker', 'nighttime version', etc.",
			Parameters: mustParams(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "How to transform the selected image (e.g. 'make it nighttime with neon lighting').",
					},
				},
				"required": []string{"prompt"},
			}),
		})
		tools = append(tools, schema.ToolSchema{
			Name:        ToolVariants,
			Description: "Generate 4 variations of the currently-selected image. Use when the user asks for 'variants', 'more like this', 'different versions'.",
			Parameters: mustParams(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Anchoring prompt for the variants. Often the original prompt; can elaborate.",
					},
				},
				"required": []string{"prompt"},
			}),
		})
		tools = append(tools, schema.ToolSchema{
			Name:        ToolAnimate,
			Description: "Generate a short video (3-12s MP4) from the currently-selected image. Use when the user says 'animate', 'make it move', 'turn into a video'.",
			Parameters: mustParams(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Motion description (e.g. 'slow camera push-in, gentle sway').",
					},
					"resolution": map[string]any{
						"type":        "string",
						"enum":        []string{"480p", "720p", "1080p"},
						"description": "Output resolution. Default 720p; pick 480p for speed, 1080p only when quality is critical.",
					},
					"duration": map[string]any{
						"type":        "integer",
						"description": "Seconds (4-8). Default 5.",
					},
				},
				"required": []string{"prompt"},
			}),
		})
		tools = append(tools, schema.ToolSchema{
			Name:        ToolQuickEdit,
			Description: "Apply a one-word style transform to the currently-selected image (no detailed prompt needed). Use when the user names a style: 'minimalize', 'cartoonize', 'photoreal', 'watercolor', 'darken', 'brighten', 'vintage', 'add neon'.",
			Parameters: mustParams(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"preset": map[string]any{
						"type":        "string",
						"enum":        []string{"minimalize", "cartoonize", "photoreal", "watercolor", "darken", "brighten", "vintage", "add_neon"},
						"description": "Which style preset to apply.",
					},
				},
				"required": []string{"preset"},
			}),
		})
	}
	return tools
}

func routerSystemPrompt(rc RouteContext) string {
	hasSelection := strings.TrimSpace(rc.SelectedURL) != ""
	hasSkill := strings.TrimSpace(rc.SkillHint) != ""
	sb := strings.Builder{}
	sb.WriteString(`You are the intent router for a design canvas. Given a user's chat message, pick the single best tool to execute and fill in its arguments.

Rules:
- If the user describes a NEW subject from scratch ("a logo of a fox", "a sunset over mountains"), use generate.
- If the user references the SELECTED image ("this", "this image", "darker", "different style") and a selection exists, prefer reimagine.
- If they say "variants" / "more like this" / "different versions", use variants.
- If they say "animate" / "make it move" / "video", use animate.
- If the user uses a named style ("minimalize", "cartoonize", "watercolor", etc.), use quick_edit with the matching preset.
- When unsure, default to generate.

Always call exactly one tool. Refine the user's prompt only when it's terse — preserve their voice.
`)
	if hasSelection {
		sb.WriteString("\nA canvas image is currently SELECTED. Tools that operate on the selection are available.\n")
	} else {
		sb.WriteString("\nNo canvas image is currently selected. Selection-only tools are disabled — pick generate or skip.\n")
	}
	if hasSkill {
		// Skill context substantially narrows the action space. When the
		// user has picked, say, "Event Poster" as their active task,
		// "darker" almost certainly means reimagine the existing poster
		// to a moodier palette — not a generic darken preset on some
		// other shape. Surfacing the skill hint to the model fixes that.
		sb.WriteString("\nThe user has picked an ACTIVE DESIGN TASK. Context:\n")
		sb.WriteString("  ")
		sb.WriteString(rc.SkillHint)
		sb.WriteString("\n\nHonor the task context when classifying — terse messages should be interpreted in light of what the user is trying to build.\n")
	}
	return sb.String()
}

func routerUserMessage(rc RouteContext) string {
	sb := strings.Builder{}
	if len(rc.RecentPrompts) > 0 {
		sb.WriteString("Recent prompts (oldest → newest):\n")
		for _, p := range rc.RecentPrompts {
			sb.WriteString("  - ")
			sb.WriteString(p)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("User just typed:\n")
	sb.WriteString(rc.Message)
	return sb.String()
}

// decodeRouterToolCall converts the LLM's tool call into a DesignIntent.
// Validates the tool name + args; rejects unknown / malformed.
func decodeRouterToolCall(call schema.ToolCall, rc RouteContext) (DesignIntent, error) {
	args := map[string]any{}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			return DesignIntent{}, fmt.Errorf("decode args: %w", err)
		}
	}

	switch call.Name {
	case ToolGenerate, ToolReimagine, ToolVariants:
		if _, ok := args["prompt"].(string); !ok {
			return DesignIntent{}, errors.New("missing or non-string prompt")
		}
	case ToolAnimate:
		if _, ok := args["prompt"].(string); !ok {
			return DesignIntent{}, errors.New("missing or non-string prompt")
		}
	case ToolQuickEdit:
		if _, ok := args["preset"].(string); !ok {
			return DesignIntent{}, errors.New("missing or non-string preset")
		}
	case ToolExtendCanvas:
		// Reserved for future use.
	default:
		return DesignIntent{}, fmt.Errorf("unknown tool %q", call.Name)
	}

	return DesignIntent{
		Tool:       call.Name,
		Args:       args,
		Confidence: 0.9, // tool-calling providers don't expose a confidence — default high
		Rationale:  fmt.Sprintf("matched %s pattern", call.Name),
	}, nil
}
