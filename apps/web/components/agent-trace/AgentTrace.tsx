"use client";

import { useEffect, useRef, useState } from "react";
import { eventsURL } from "@/lib/api";

type Event = {
  session_id: string;
  kind: string;
  at: string;
  data?: Record<string, unknown>;
};

const KIND_LABEL: Record<string, string> = {
  "step.start":         "📍 Step",
  "llm.thought":        "🧠 Thought",
  "tool.start":         "🔧 Tool",
  "tool.end":           "✅ Tool result",
  "slides.outline":     "📝 Outline ready",
  "slides.content":     "📄 Slide rendered",
  "slides.render.start":"🎨 Rendering",
  "slides.render.end":  "📦 PPTX ready",
  "agent.finish":       "🏁 Done",
  "agent.error":        "🚨 Error",
};

export function AgentTrace({ sessionId }: { sessionId: string }) {
  const [events, setEvents] = useState<Event[]>([]);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    if (!sessionId) return;
    const url = eventsURL(sessionId);
    const ws = new WebSocket(url);
    wsRef.current = ws;
    ws.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data) as Event;
        setEvents((prev) => [...prev, ev].slice(-200));
      } catch {
        /* skip malformed frames */
      }
    };
    return () => ws.close();
  }, [sessionId]);

  return (
    <div className="border border-slate-200 rounded-2xl p-6 bg-slate-50 max-h-[60vh] overflow-auto">
      {events.length === 0 && (
        <div className="text-slate-400 text-sm">Waiting for agent to start…</div>
      )}
      <ol className="space-y-3 text-sm">
        {events.map((ev, i) => (
          <li key={i} className="flex gap-3">
            <span className="text-slate-400 tabular-nums w-20 shrink-0">
              {new Date(ev.at).toLocaleTimeString()}
            </span>
            <span className="font-medium w-44 shrink-0">{KIND_LABEL[ev.kind] ?? ev.kind}</span>
            <span className="text-slate-600 break-words">{summarize(ev)}</span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function summarize(ev: Event): string {
  const d = ev.data ?? {};
  switch (ev.kind) {
    case "step.start":          return `${d.agent ?? "agent"} step ${d.step}`;
    case "llm.thought":         return `${d.tool_calls ? `→ ${(d.tool_calls as string[]).join(", ")}` : (d.text as string ?? "").slice(0, 120)}`;
    case "tool.start":          return `${d.name}`;
    case "tool.end":            return d.error ? `error: ${d.error}` : `${(d.output as string ?? "").slice(0, 120)}`;
    case "slides.outline":      return `${d.title} — ${d.slides} slides`;
    case "slides.content":      return `slide ${d.slide} (${d.bytes} bytes)`;
    case "slides.render.end":   return `${d.path}`;
    case "agent.error":         return `${d.error}`;
    default:                    return JSON.stringify(d);
  }
}
