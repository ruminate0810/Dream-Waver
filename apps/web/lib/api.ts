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

type Envelope<T> = { ok: true; data: T } | { ok: false; error: string };

async function unwrap<T>(res: Response): Promise<T> {
  const json = (await res.json()) as Envelope<T>;
  if (!json.ok) throw new Error(json.error);
  return json.data;
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
