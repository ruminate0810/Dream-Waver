// Thin client for the Go orchestrator. Routes go through Next.js rewrites
// during local dev so we never set Origin / CORS headers from the browser.

export type CreateSlidesRequest = {
  topic: string;
  audience?: string;
  slide_count?: number;
  style?: string;
  reference_text?: string;
  force_theme?: string;
  /**
   * Execution path. "agent" (default) runs a ToolCallAgent that picks
   * each tool and emits llm.thought / tool.* events. "pipeline" is the
   * deterministic outline → content → render shortcut — cheaper, but the
   * chat surface stays opaque.
   */
  mode?: "agent" | "pipeline";
};

export type CreateSlidesResponse = {
  job_id: string;
  session_id: string;
  events_url: string;
};

export type SlideJob = {
  job_id: string;
  session_id: string;
  status: "running" | "finished" | "error";
  mode?: "agent" | "pipeline";
  input?: {
    topic?: string;
    audience?: string;
    slide_count?: number;
    style?: string;
    reference_text?: string;
    force_theme?: string;
  };
  title?: string;
  slide_count?: number;
  download_url?: string;
  preview_urls?: string[];
  error?: string;
  started_at: string;
  finished_at?: string;
};

type Envelope<T> =
  | { ok: true; data: T }
  // The Go orchestrator usually sends a string error, but the video
  // bridge forwards upstream 4xx bodies verbatim — those come through
  // as a structured object (e.g. `{detail: {field_errors: [...]}}`
  // from Opendream's spec validator). Accept both shapes and let
  // `unwrap` convert the structured form into a readable message.
  | { ok: false; error: string | Record<string, unknown> };

async function unwrap<T>(res: Response): Promise<T> {
  const json = (await res.json()) as Envelope<T>;
  if (!json.ok) throw new ApiError(json.error);
  return json.data;
}

/**
 * ApiError carries the structured error payload so callers that want
 * to render per-field hints (e.g. /video/new) can do so without
 * re-parsing strings. `.message` is always a human-readable string;
 * `.fieldErrors` is populated when the upstream surface provided one.
 */
export class ApiError extends Error {
  readonly fieldErrors: string[];
  readonly raw: string | Record<string, unknown>;

  constructor(payload: string | Record<string, unknown>) {
    super(messageFromPayload(payload));
    this.name = "ApiError";
    this.raw = payload;
    this.fieldErrors = fieldErrorsFromPayload(payload);
  }
}

function messageFromPayload(p: string | Record<string, unknown>): string {
  if (typeof p === "string") return p;
  // FastAPI wraps app-thrown HTTPException bodies in a top-level
  // `detail` key. Our Opendream service puts the structured payload
  // inside that — so detail.detail is the human-readable summary.
  const detail = p["detail"];
  if (typeof detail === "string") return detail;
  if (detail && typeof detail === "object") {
    const inner = (detail as Record<string, unknown>)["detail"];
    if (typeof inner === "string") return inner;
  }
  if (typeof p["error"] === "string") return p["error"] as string;
  return "Request failed";
}

function fieldErrorsFromPayload(p: string | Record<string, unknown>): string[] {
  if (typeof p === "string") return [];
  const detail = p["detail"];
  if (detail && typeof detail === "object") {
    const fe = (detail as Record<string, unknown>)["field_errors"];
    if (Array.isArray(fe)) return fe.map((x) => String(x));
  }
  const fe = p["field_errors"];
  if (Array.isArray(fe)) return fe.map((x) => String(x));
  return [];
}

export async function createSlides(body: CreateSlidesRequest): Promise<CreateSlidesResponse> {
  const res = await fetch("/api/v1/slides", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<CreateSlidesResponse>(res);
}

/**
 * Send a follow-up edit instruction for an already-generated deck.
 * Backend resumes the agent with full conversation context and applies
 * the change via edit_slide_text / regenerate_slide / delete_slide.
 * Returns 202; the front-end keeps polling getSlideJob for the new
 * preview URLs.
 */
export async function postSlideMessage(jobId: string, content: string): Promise<void> {
  const res = await fetch(`/api/v1/slides/${jobId}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content }),
  });
  await unwrap<{ job_id: string; session_id: string }>(res);
}

export async function getSlideJob(id: string): Promise<SlideJob> {
  const res = await fetch(`/api/v1/slides/${id}`);
  return unwrap<SlideJob>(res);
}

// WebSocket URL for an existing session.
export function eventsURL(sessionId: string): string {
  const base = process.env.NEXT_PUBLIC_WS_BASE || (typeof window !== "undefined"
    ? `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}`
    : "");
  return `${base}/api/v1/sessions/${sessionId}/events`;
}

// ─── Games ────────────────────────────────────────────────────────────
//
// Single-shot pipeline that turns a natural-language brief into one
// self-contained HTML5 game. Same event surface as slides — the
// frontend connects to the WebSocket via AgentSessionProvider — but
// the artifact is HTML served directly into an iframe, not PPTX.

export type CreateGameRequest = {
  prompt: string;
  genre?: string;
  difficulty?: string;
};

export type CreateGameResponse = {
  job_id: string;
  session_id: string;
  events_url: string;
};

export type GameJob = {
  job_id: string;
  session_id: string;
  status: "running" | "finished" | "error";
  input: {
    prompt: string;
    genre?: string;
    difficulty?: string;
  };
  title?: string;
  bytes?: number;
  /** Set when status==finished. Same-origin URL the iframe loads. */
  play_url?: string;
  error?: string;
  started_at: string;
  finished_at?: string;
};

export async function createGame(body: CreateGameRequest): Promise<CreateGameResponse> {
  const res = await fetch("/api/v1/games", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<CreateGameResponse>(res);
}

export async function getGameJob(id: string): Promise<GameJob> {
  const res = await fetch(`/api/v1/games/${id}`);
  return unwrap<GameJob>(res);
}

/**
 * Send a follow-up edit. The backend re-prompts with the prior HTML in
 * system context so the model edits surgically; status flips back to
 * "running" until the new artifact is ready.
 */
export async function postGameMessage(jobId: string, content: string): Promise<void> {
  const res = await fetch(`/api/v1/games/${jobId}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content }),
  });
  await unwrap<{ job_id: string; session_id: string }>(res);
}

// ─── Video ─────────────────────────────────────────────────────────────
//
// Click-to-regen cinematic short pipeline. The Go orchestrator is a thin
// bridge to the Opendream FastAPI service — see
// services/orchestrator/internal/skill/video and
// /Users/sheng/git/Opendream/server/README.md.
//
// Wire shape is a *direct mirror* of Opendream's surface so future
// migrations (e.g. moving more of the pipeline to Go) don't ripple
// through the UI:
//
//   POST /api/v1/video/runs                      → { run_id, events_url, ... }
//   GET  /api/v1/video/runs/{id}                 → VideoTimeline snapshot
//   POST /api/v1/video/runs/{id}/regen           → kick off partial re-run
//   GET  /api/v1/video/runs/{id}/events  (SSE)   → timeline updates
//   GET  /api/v1/video/runs/{id}/artifacts/{p}   → asset file

export type CreateVideoRunRequest = {
  /** Full story_spec.json. Validated by Opendream before any node runs. */
  spec: Record<string, unknown>;
  /** Optional display title; defaults to spec.title. */
  title?: string;
  /** Plan-only — useful for smoke tests; no provider calls fire. */
  dry_run?: boolean;
  /** Stop at a stage boundary: sheets | frames | clips | compose. */
  until?: "sheets" | "frames" | "clips" | "compose";
};

export type CreateVideoRunResponse = {
  run_id: string;
  events_url: string;
  timeline_url: string;
  artifacts_url: string;
};

export type VideoNodeKind =
  | "char_sheet"
  | "vehicle_sheet"
  | "scene_frame"
  | "scene_clip"
  | "end_frame"
  | "final_compose";

export type VideoNodeState =
  | "pending"
  | "running"
  | "done"
  | "failed"
  | "skipped";

export type VideoTimelineNode = {
  key: string;          // e.g. "SceneFrame[34]"
  kind: VideoNodeKind;
  subject?: string;     // cid / sid / vid
  state: VideoNodeState;
  deps: string[];
  /** Pre-rewritten by the Go bridge to /api/v1/video/runs/{id}/artifacts/... */
  output_url?: string;
  error?: string;
  last_run_iso?: string;
  cost_usd: number;
};

export type VideoTimeline = {
  run_id: string;
  status: "pending" | "running" | "finished" | "error";
  title: string;
  started_at: string;
  finished_at?: string;
  nodes: VideoTimelineNode[];
  errors?: string[];
};

export type RegenVideoNodesRequest = {
  node_keys: string[];
};

export type RegenVideoNodesResponse = {
  run_id: string;
  /** All nodes that will (re-)run, including transitive descendants. */
  queued: string[];
};

export async function createVideoRun(
  body: CreateVideoRunRequest,
): Promise<CreateVideoRunResponse> {
  const res = await fetch("/api/v1/video/runs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<CreateVideoRunResponse>(res);
}

export async function getVideoTimeline(runId: string): Promise<VideoTimeline> {
  const res = await fetch(`/api/v1/video/runs/${runId}`);
  return unwrap<VideoTimeline>(res);
}

export async function regenVideoNodes(
  runId: string,
  body: RegenVideoNodesRequest,
): Promise<RegenVideoNodesResponse> {
  const res = await fetch(`/api/v1/video/runs/${runId}/regen`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<RegenVideoNodesResponse>(res);
}

/** Same-origin SSE URL — no rewrite, no WS, no special headers. */
export function videoEventsURL(runId: string): string {
  return `/api/v1/video/runs/${runId}/events`;
}
