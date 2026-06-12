package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// VideoGenerator is the videographer's image-to-video capability — a tiny
// interface so the claw package stays decoupled from the design bridge (same
// posture as image.Searcher for the designer). main.go injects an adapter
// around design.Bridge.SeedanceI2V. nil greys out the videographer worker.
type VideoGenerator interface {
	// ImageToVideo animates a source image into a short clip and returns the
	// servable video URL. resolution is "480p"|"720p"|"1080p"; duration is
	// seconds (4–12).
	ImageToVideo(ctx context.Context, imageURL, prompt, resolution string, duration int) (videoURL string, err error)
}

// GenerateVideo is the videographer worker's tool. It animates one already-
// generated work-package figure (image-to-video) and stores the clip as a
// work-package video, emitting claw.artifact.updated(kind="video") so the
// frontend gallery picks it up. On-demand by design: the render is slow and
// billed, so the role is only planned when the user asks for a video. A
// missing capability / no figures degrades gracefully (a "skip" observation,
// not an error) — same posture as the rest of Claw's optional capabilities.
type GenerateVideo struct {
	Video   VideoGenerator
	Session *Session
	Emitter event.Emitter
}

func (*GenerateVideo) Name() string { return "generate_video" }

func (*GenerateVideo) Description() string {
	return "Animate one already-generated work-package figure into a short clip (image-to-video). " +
		"Pass a short English motion/camera description (e.g. slow zoom-in, drifting light, subtle " +
		"character motion). Optionally pick which figure (figure_index, 1-based), resolution, and " +
		"duration. Call once for the key figure, then terminate."
}

func (*GenerateVideo) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["prompt"],
		"properties": {
			"prompt":       {"type": "string", "description": "English motion/camera description for the clip."},
			"figure_index": {"type": "integer", "description": "1-based index of the work-package figure to animate (default 1)."},
			"resolution":   {"type": "string", "enum": ["480p", "720p", "1080p"], "description": "Output resolution (default 720p)."},
			"duration":     {"type": "integer", "description": "Clip length in seconds, 4-12 (default 5)."},
			"caption":      {"type": "string", "description": "Short Chinese caption shown under the video."}
		}
	}`)
}

func (t *GenerateVideo) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Prompt      string `json:"prompt"`
		FigureIndex int    `json:"figure_index"`
		Resolution  string `json:"resolution"`
		Duration    int    `json:"duration"`
		Caption     string `json:"caption"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	prompt := strings.TrimSpace(p.Prompt)
	if prompt == "" {
		return schema.ToolResult{Error: "prompt is required"}, nil
	}
	if t.Video == nil {
		return schema.ToolResult{Output: "视频生成未启用,跳过。"}, nil
	}

	figs := t.Session.Figures()
	if len(figs) == 0 {
		return schema.ToolResult{Output: "作品包里还没有配图,无法做成视频(先让设计师配图)。"}, nil
	}
	idx := p.FigureIndex
	if idx < 1 || idx > len(figs) {
		idx = 1 // default to the first/cover figure
	}
	src := figs[idx-1]

	resolution := normalizeResolution(p.Resolution)
	duration := clampDuration(p.Duration)

	url, err := t.Video.ImageToVideo(ctx, src.URL, prompt, resolution, duration)
	if err != nil {
		return schema.ToolResult{Error: "video generation failed: " + err.Error()}, nil
	}
	if strings.TrimSpace(url) == "" {
		return schema.ToolResult{Output: "视频没有生成成功,跳过。"}, nil
	}

	caption := strings.TrimSpace(p.Caption)
	if caption == "" {
		caption = src.Caption
	}
	version := t.Session.AddVideo(Video{
		URL: url, Poster: src.URL, Caption: caption, Resolution: resolution, Duration: duration,
	})
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("video", version, len(url)))
	}
	return schema.ToolResult{Output: fmt.Sprintf(
		"已把配图 #%d 做成 %s/%ds 短视频 #%d:%s\nURL: %s", idx, resolution, duration, version, caption, url)}, nil
}

// normalizeResolution defaults to 720p and only allows the supported tiers.
func normalizeResolution(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "480p":
		return "480p"
	case "1080p":
		return "1080p"
	default:
		return "720p"
	}
}

// clampDuration keeps the clip within Seedance's 4–12s window (default 5).
func clampDuration(d int) int {
	if d <= 0 {
		return 5
	}
	if d < 4 {
		return 4
	}
	if d > 12 {
		return 12
	}
	return d
}
