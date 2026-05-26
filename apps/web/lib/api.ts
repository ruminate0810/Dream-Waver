// Thin client for the Go orchestrator. Routes go through Next.js rewrites
// during local dev so we never set Origin / CORS headers from the browser.
//
// Phase 1 (Sprint X1) introduces apiFetch — a centralized wrapper that
// injects the active X-Workspace-ID header on every call. Existing
// helpers below still call `fetch()` directly because their backend
// routes haven't migrated to required-auth yet (Phase 2 does that).
// NEW helpers (workspace CRUD, /me) use apiFetch so they round-trip
// the workspace context cleanly.

import { getActiveWorkspaceID } from "@/lib/workspace";

/**
 * apiFetch is the thin wrapper around fetch() that:
 *   - Prepends nothing — caller passes the full /api/v1/... path.
 *   - Injects the X-Workspace-ID header from the workspace cookie
 *     when one is set. Skipped when not (so anonymous calls still go).
 *   - Defaults Content-Type to application/json when the caller
 *     passes a body but no header.
 *
 * Future Phase 2 migration: every existing `fetch("/api/v1/…")` call
 * below swaps to apiFetch(…) so workspace context is uniform. For
 * now we leave the existing call sites alone to keep the Sprint X1
 * diff focused.
 */
export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers);
  const wsID = getActiveWorkspaceID();
  if (wsID && !headers.has("X-Workspace-ID")) {
    headers.set("X-Workspace-ID", wsID);
  }
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  return fetch(path, { ...init, headers });
}

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
  // Sprint L1/N1 — status now also surfaces "awaiting_*" sentinels
  // (awaiting_wizard / awaiting_outline_approval / awaiting_clarification)
  // when an interactive gate is paused for user input.
  status:
    | "running"
    | "finished"
    | "error"
    | "awaiting_wizard"
    | "awaiting_outline_approval"
    | "awaiting_clarification";
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
  /**
   * Sprint N1.h — populated when status is awaiting_*. Mirrors the
   * Go-side slides.PendingUserAction. Lets the chat page hydrate its
   * WizardCard / OutlineReviewCard on initial mount or refresh,
   * because the WebSocket pause event won't replay for late
   * subscribers.
   */
  pending?: SlidePendingAction;
};

/** Sprint N1.h — typed wire mirror of slides.PendingUserAction. */
export type SlidePendingAction =
  | { kind: "wizard"; wizard: SlideWizardStepView; wizard_scenario?: string; wizard_answers?: Record<string, string> }
  | { kind: "outline_review"; outline_json: string }
  | { kind: "clarification"; questions: string[] };

/** Sprint N1.h — typed wire mirror of slides.WizardStepView. */
export type SlideWizardStepView = {
  step: number;
  total: number;
  kind: "scenario" | "select" | "free-text";
  question: string;
  placeholder?: string;
  options?: Array<{ value: string; label: string; icon: string }>;
  optional: boolean;
  suggested_value?: string;
  /** Sprint N1.i breadcrumb + back-step context. */
  previous_answers?: Record<string, string>;
  previous_scenario?: string;
  can_go_back?: boolean;
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
  if (!json.ok) throw new ApiError(json.error, res.status);
  return json.data;
}

/**
 * ApiError carries the structured error payload so callers that want
 * to render per-field hints (e.g. /video/new) can do so without
 * re-parsing strings. `.message` is always a human-readable string;
 * `.fieldErrors` is populated when the upstream surface provided one.
 * `.status` is the HTTP status code, useful for distinguishing
 * 404 (resource gone) from 500 (transient).
 */
export class ApiError extends Error {
  readonly fieldErrors: string[];
  readonly raw: string | Record<string, unknown>;
  readonly status: number;

  constructor(payload: string | Record<string, unknown>, status = 0) {
    super(messageFromPayload(payload));
    this.name = "ApiError";
    this.raw = payload;
    this.fieldErrors = fieldErrorsFromPayload(payload);
    this.status = status;
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

// Sprint L1 — HILT resume wrappers. Both hit the same /messages
// endpoint but carry an `action` field that routes to a different
// AgentRunner.ResumeFrom* method on the backend. See routes_slides.go
// PostSlideMessage switch.

export async function postSlideClarification(
  jobId: string,
  answers: string[],
): Promise<void> {
  const res = await fetch(`/api/v1/slides/${jobId}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "clarify", answers }),
  });
  await unwrap<{ job_id: string; session_id: string }>(res);
}

export type SlideOutlineEdits = {
  theme?: string;
  renames?: Array<{ index: number; title: string }>;
  // Sprint S — per-slide forced layout override from the outline
  // review card. Each `layout` must match a schema.SlideLayout const
  // (backend silently drops unknown values).
  relayouts?: Array<{ index: number; layout: string }>;
  delete_indices?: number[];
};

export async function postSlideOutlineApproval(
  jobId: string,
  edits?: SlideOutlineEdits,
): Promise<void> {
  const res = await fetch(`/api/v1/slides/${jobId}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "approve_outline", edits }),
  });
  await unwrap<{ job_id: string; session_id: string }>(res);
}

// Sprint N1 — multi-step wizard resume. Each step the user clicks
// 下一步 / 跳过 / ← we POST here; backend either emits the next (or
// previous) step's view (still awaiting_wizard) OR completes the
// wizard and proceeds to outline planning.
//
// N1.i — `back` true tells the backend to re-emit step N's view
// (with prior answers preserved as defaults) instead of advancing.
// `step` then is the step to go BACK to. `back` is mutually
// exclusive with `skip`.
export async function postSlideWizardStep(
  jobId: string,
  step: number,
  answer: string,
  skip: boolean,
  back = false,
): Promise<void> {
  const res = await fetch(`/api/v1/slides/${jobId}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      action: "wizard_step",
      wizard_step: step,
      wizard_answer: answer,
      wizard_skip: skip,
      wizard_back: back,
    }),
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
  /**
   * Visual style preset: "minimalist" | "neon" | "paper" | "pixel" |
   * "editorial". Overrides the default palette in the system prompt.
   * Omit (or pass undefined) to let the exemplar's palette dominate.
   */
  aesthetic?: string;
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
    aesthetic?: string;
  };
  title?: string;
  bytes?: number;
  /** Set when status==finished. Same-origin URL the iframe loads. */
  play_url?: string;
  /**
   * Workspace file manifest, sorted by path. Always contains "index.html"
   * when the job is finished. Surfaced so the Source tab can render a
   * file picker for multi-file games.
   */
  files?: string[];
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

// ─── Game revisions ───────────────────────────────────────────────────
//
// Every successful generation appends one immutable Revision to the
// session. The frontend lists them as v0/v1/v2/… and lets the user
// preview an older version (read-only) or restore to fork from there.

export type GameRevision = {
  idx: number;
  title: string;
  description: string;
  bytes: number;
  /** RFC3339 timestamp. */
  at: string;
};

export async function listGameRevisions(jobId: string): Promise<GameRevision[]> {
  const res = await fetch(`/api/v1/games/${jobId}/revisions`);
  const data = await unwrap<{ revisions: GameRevision[] }>(res);
  return data.revisions ?? [];
}

/**
 * Truncate the session back to revision idx. Subsequent edits fork from
 * this point. A thought event "已回到 vN" streams via the existing SSE
 * channel so the chat picks it up alongside normal output.
 */
export async function restoreGameRevision(jobId: string, idx: number): Promise<void> {
  const res = await fetch(`/api/v1/games/${jobId}/revisions/${idx}/restore`, {
    method: "POST",
  });
  await unwrap<{ job_id: string; restored_to: number }>(res);
}

/** URL the iframe should load to preview a historical revision. */
export function gameRevisionPlayURL(jobId: string, idx: number): string {
  return `/api/v1/games/${jobId}/revisions/${idx}/play`;
}

/** URL the Source view should fetch for a historical revision. */
export function gameRevisionSourceURL(jobId: string, idx: number): string {
  return `/api/v1/games/${jobId}/revisions/${idx}/source`;
}

/** URL the Source view should fetch for the current head revision. */
export function gameSourceURL(jobId: string): string {
  return `/api/v1/games/${jobId}/source`;
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

// ─── Design ────────────────────────────────────────────────────────────
//
// The TLDraw canvas at /design calls this surface to drop AI-generated
// images onto the artboard. Single endpoint for now — image generation
// via DreamAPI Flux. Background removal, enhancement, variants, etc.
// will follow as separate endpoints; see services/dreamapi-sidecar.

export type GenerateDesignImageRequest = {
  prompt: string;
  /** Width in px, multiple of 16, 256-1600. Default 1024. */
  width?: number;
  /** Height in px, multiple of 16, 256-1600. Default 1024. */
  height?: number;
  /** Optional seed for deterministic variants. */
  seed?: number;
};

export type GenerateDesignImageResponse = {
  url: string;
  width: number;
  height: number;
  task_id: string;
};

/**
 * Synchronous text-to-image. Resolves after 30-60 s with the asset URL.
 * The canvas should optimistically place a "loading" placeholder while
 * the promise is pending — once SSE-based progress lands on the
 * sidecar this same helper can swap for a streamed variant without
 * changing callers.
 */
export async function generateDesignImage(
  body: GenerateDesignImageRequest,
): Promise<GenerateDesignImageResponse> {
  const res = await fetch("/api/v1/design/images/generate", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<GenerateDesignImageResponse>(res);
}

// ─── Variants (N images, one prompt) ──────────────────────────────────

export type GenerateDesignVariantsRequest = {
  prompt: string;
  /** Sidecar caps at [2, 6]. Default 4. */
  count?: number;
  width?: number;
  height?: number;
};

export type DesignImageVariant = {
  url: string;
  width: number;
  height: number;
};

export type GenerateDesignVariantsResponse = {
  variants: DesignImageVariant[];
  task_id: string;
};

export async function generateDesignVariants(
  body: GenerateDesignVariantsRequest,
): Promise<GenerateDesignVariantsResponse> {
  const res = await fetch("/api/v1/design/images/variants", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<GenerateDesignVariantsResponse>(res);
}

// ─── Edit ops (operate on an existing image URL) ──────────────────────

export type EditDesignImageRequest = {
  image_url: string;
};

export type EditDesignImageResponse = {
  url: string;
  /** Output dimensions when DreamAPI echoes them. Often missing for
   *  edit ops; the canvas falls back to natural image size on load. */
  width?: number;
  height?: number;
  task_id: string;
};

/** Cut the background out of an existing image. Output is PNG with alpha. */
export async function removeDesignImageBg(
  body: EditDesignImageRequest,
): Promise<EditDesignImageResponse> {
  const res = await fetch("/api/v1/design/images/remove_bg", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<EditDesignImageResponse>(res);
}

/** Super-resolution + sharpen. Output is 2-4× input resolution. */
export async function enhanceDesignImage(
  body: EditDesignImageRequest,
): Promise<EditDesignImageResponse> {
  const res = await fetch("/api/v1/design/images/enhance", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<EditDesignImageResponse>(res);
}

// ─── Outpaint (extend image borders) ──────────────────────────────────

export type OutpaintDesignImageRequest = {
  image_url: string;
  /** Pixels to extend each side. Sidecar rejects all-zero. */
  left?: number;
  right?: number;
  top?: number;
  bottom?: number;
};

/** Extend an image's borders — DreamAPI infills the new area from
 *  surrounding context. Typical use: aspect-ratio change. */
export async function outpaintDesignImage(
  body: OutpaintDesignImageRequest,
): Promise<EditDesignImageResponse> {
  const res = await fetch("/api/v1/design/images/outpaint", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<EditDesignImageResponse>(res);
}

// ─── Image-to-image (transform an existing image via prompt) ──────────

export type Image2ImageDesignRequest = {
  image_url: string;
  prompt: string;
  width?: number;
  height?: number;
};

/** Transform an existing image via a text prompt — source is reference
 *  only, output is fresh pixels at the requested dimensions. */
export async function image2imageDesignImage(
  body: Image2ImageDesignRequest,
): Promise<GenerateDesignImageResponse> {
  const res = await fetch("/api/v1/design/images/image2image", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<GenerateDesignImageResponse>(res);
}

// ─── SSE-based generate (in-canvas progress) ──────────────────────────
//
// Two-step flow vs. the synchronous generateDesignImage:
//   1. submitDesignGenerate → returns {task_id}
//   2. open an EventSource on designGenerateEventsURL(task_id) →
//      receives `progress` ticks then a terminal `done` (carries
//      {url, width, height, task_id}) or `error` (carries {message}).
//
// The canvas uses this to render a placeholder shape immediately on
// submit and swap it for the real image on `done` — turns 30-60 s
// of blank wait into "queued / 8 s / 24 s / done" feedback.

export type SubmitDesignGenerateResponse = {
  task_id: string;
};

export async function submitDesignGenerate(
  body: GenerateDesignImageRequest,
): Promise<SubmitDesignGenerateResponse> {
  const res = await fetch("/api/v1/design/images/generate/submit", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return unwrap<SubmitDesignGenerateResponse>(res);
}

/** Same-origin SSE URL — no rewrite, no WS, no special headers. */
export function designGenerateEventsURL(taskId: string): string {
  return `/api/v1/design/images/jobs/${taskId}/events`;
}

// ─── Workspace + identity (Sprint X1, Phase 1) ──────────────────────
//
// All helpers below use apiFetch so the X-Workspace-ID header rides
// along. These routes 401 without auth on the backend — the web
// middleware bounces unauth users to /login before they get here.

export type Workspace = {
  id: string;
  name: string;
  kind: "personal" | "team";
  owner_user_id: string;
  created_at: string;
};

export type Me = {
  user_id: string;
  email: string;
  active_workspace_id: string;
  active_workspace: Workspace | null;
};

export async function getMe(): Promise<Me> {
  const res = await apiFetch("/api/v1/me");
  return unwrap<Me>(res);
}

export async function listWorkspaces(): Promise<{ workspaces: Workspace[] }> {
  const res = await apiFetch("/api/v1/workspaces");
  return unwrap<{ workspaces: Workspace[] }>(res);
}

export async function createWorkspace(
  body: { name: string },
): Promise<Workspace> {
  const res = await apiFetch("/api/v1/workspaces", {
    method: "POST",
    body: JSON.stringify(body),
  });
  return unwrap<Workspace>(res);
}

export type WorkspaceMember = {
  user_id: string;
  email: string;
  role: "owner" | "admin" | "member";
  created_at: string;
};

export async function listWorkspaceMembers(
  workspaceID: string,
): Promise<{ members: WorkspaceMember[] }> {
  const res = await apiFetch(`/api/v1/workspaces/${workspaceID}/members`);
  return unwrap<{ members: WorkspaceMember[] }>(res);
}

export async function addWorkspaceMember(
  workspaceID: string,
  body: { email: string; role?: "owner" | "admin" | "member" },
): Promise<WorkspaceMember> {
  const res = await apiFetch(`/api/v1/workspaces/${workspaceID}/members`, {
    method: "POST",
    body: JSON.stringify(body),
  });
  return unwrap<WorkspaceMember>(res);
}

export async function removeWorkspaceMember(
  workspaceID: string,
  userID: string,
): Promise<void> {
  const res = await apiFetch(
    `/api/v1/workspaces/${workspaceID}/members/${userID}`,
    { method: "DELETE" },
  );
  await unwrap<{ status: string }>(res);
}
