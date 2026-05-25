// Package video is the Dream-Waver side of the Opendream integration.
//
// Architecturally this package is *not* a generation skill — it owns no
// LLMs, no prompts, no DAG. It is a thin authenticated bridge to the
// Opendream FastAPI service (see /Users/sheng/git/Opendream/server/),
// where the click-to-regen cinematic short pipeline actually lives.
//
// Splitting it this way means:
//   - Opendream stays a self-contained Python project; the Go side
//     never has to reason about story_spec.json shapes or DAG topology.
//   - Dream-Waver retains a single front door: the browser still talks
//     to `:8080/api/v1/...`, auth + billing + the usual chi middleware
//     fire before any request reaches the Python service.
//   - Either side can be swapped independently — e.g. a future "video
//     skill v2" can keep the same HTTP shape while replacing the Python
//     backend.
package video

import "time"

// CreateRunRequest mirrors server.schemas.RunCreateRequest in Opendream.
// `Spec` is intentionally typed as a raw map[string]any: the Go side
// neither validates nor manipulates it — Opendream's spec_validator
// owns that responsibility, and forwarding the dict verbatim avoids
// double-marshalling drift.
type CreateRunRequest struct {
	Spec    map[string]any `json:"spec"`
	Title   string         `json:"title,omitempty"`
	DryRun  bool           `json:"dry_run,omitempty"`
	Until   string         `json:"until,omitempty"` // sheets|frames|clips|compose
}

type CreateRunResponse struct {
	RunID        string `json:"run_id"`
	EventsURL    string `json:"events_url"`
	TimelineURL  string `json:"timeline_url"`
	ArtifactsURL string `json:"artifacts_url"`
}

// TimelineNode mirrors server.schemas.TimelineNode. The OutputURL is
// rewritten on the way out so the frontend hits the Dream-Waver bridge
// route (/api/v1/video/runs/{id}/artifacts/...) instead of the upstream
// Python service directly.
type TimelineNode struct {
	Key         string   `json:"key"`
	Kind        string   `json:"kind"`
	Subject     string   `json:"subject,omitempty"`
	State       string   `json:"state"` // pending|running|done|failed|skipped
	Deps        []string `json:"deps"`
	OutputURL   string   `json:"output_url,omitempty"`
	Error       string   `json:"error,omitempty"`
	LastRunISO  string   `json:"last_run_iso,omitempty"`
	CostUSD     float64  `json:"cost_usd"`
}

type Timeline struct {
	RunID      string         `json:"run_id"`
	Status     string         `json:"status"` // pending|running|finished|error
	Title      string         `json:"title"`
	StartedAt  string         `json:"started_at"`
	FinishedAt string         `json:"finished_at,omitempty"`
	Nodes      []TimelineNode `json:"nodes"`
	Errors     []string       `json:"errors,omitempty"`
}

type RegenRequest struct {
	NodeKeys []string `json:"node_keys"`
}

type RegenResponse struct {
	RunID  string   `json:"run_id"`
	Queued []string `json:"queued"`
}

// BridgeError carries the HTTP status + decoded body from a failed
// upstream call. The router uses this to mirror Opendream's 4xx codes
// back to the browser instead of collapsing every failure into 500.
type BridgeError struct {
	Status int
	Body   string
}

func (e *BridgeError) Error() string {
	return e.Body
}

// HTTPClientTimeout — every request EXCEPT the SSE stream uses this.
// The SSE handler opens an unbounded stream that lives for the
// duration of the run (up to ~45 min in practice for a full
// generation), so it bypasses the request timeout entirely.
const HTTPClientTimeout = 30 * time.Second
