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
