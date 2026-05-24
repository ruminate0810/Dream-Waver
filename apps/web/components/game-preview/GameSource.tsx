"use client";

import { useEffect, useMemo, useState } from "react";
import { Copy, Check, Download, AlertCircle, Loader2 } from "lucide-react";
import clsx from "clsx";

import { gameRevisionSourceURL, gameSourceURL } from "@/lib/api";

// GameSource is the alternate view of the iframe pane: it loads the raw
// HTML as text and renders it as a code listing with bare-bones inline
// highlighting (HTML tags / attributes / strings / comments — done with
// regex, no Prism/Shiki dependency).
//
// When revisionIdx is set we render the historical revision instead of
// the current head, so the strip's read-only preview also has a source
// counterpart.

type Props = {
  jobId: string;
  revisionIdx?: number;
  title: string;
};

export function GameSource({ jobId, revisionIdx, title }: Props) {
  const url = useMemo(
    () =>
      revisionIdx !== undefined
        ? gameRevisionSourceURL(jobId, revisionIdx)
        : gameSourceURL(jobId),
    [jobId, revisionIdx],
  );
  const [code, setCode] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error(`${r.status} ${r.statusText}`);
        return r.text();
      })
      .then((text) => {
        if (!cancelled) {
          setCode(text);
          setLoading(false);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : "Failed to load source");
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [url]);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard API can fail in non-secure contexts; surface nothing
      // since the Download button still gives the user a way out.
    }
  };

  const onDownload = () => {
    const safe = (title || "game").replace(/[^a-zA-Z0-9\-_一-龥]/g, "_") || "game";
    const blob = new Blob([code], { type: "text/html;charset=utf-8" });
    const href = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = href;
    a.download = `${safe}.html`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(href);
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-x border-t border-[color:var(--rule)] bg-white/60 px-3 py-2 backdrop-blur-[1px]">
        <span className="font-mono-jb text-[10px] uppercase tracking-[0.24em] text-[color:var(--ink-faint)]">
          Source · {(code.length || 0).toLocaleString()} bytes
          {revisionIdx !== undefined ? ` · v${revisionIdx}` : ""}
        </span>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={onCopy}
            disabled={!code}
            title="Copy to clipboard"
            className={clsx(
              "inline-flex items-center gap-1.5 rounded px-2 py-1 font-mono-jb text-[10px] uppercase tracking-[0.22em] transition-colors",
              !code
                ? "cursor-not-allowed text-[color:var(--ink-faint)]"
                : "text-[color:var(--ink-soft)] hover:bg-[color:var(--ink)]/5 hover:text-[color:var(--ink)]",
            )}
          >
            {copied ? <Check size={11} strokeWidth={2} /> : <Copy size={11} strokeWidth={1.8} />}
            <span>{copied ? "Copied" : "Copy"}</span>
          </button>
          <button
            type="button"
            onClick={onDownload}
            disabled={!code}
            title="Download .html"
            className={clsx(
              "inline-flex items-center gap-1.5 rounded px-2 py-1 font-mono-jb text-[10px] uppercase tracking-[0.22em] transition-colors",
              !code
                ? "cursor-not-allowed text-[color:var(--ink-faint)]"
                : "text-[color:var(--ink-soft)] hover:bg-[color:var(--ink)]/5 hover:text-[color:var(--ink)]",
            )}
          >
            <Download size={11} strokeWidth={1.8} />
            <span>.html</span>
          </button>
        </div>
      </div>

      <div className="relative flex-1 overflow-auto border border-[color:var(--rule)] bg-[#fbf8f0]">
        {loading ? (
          <div className="flex h-full items-center justify-center">
            <Loader2 size={20} className="animate-spin text-[color:var(--ink-faint)]" />
          </div>
        ) : error ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
            <AlertCircle size={20} className="text-red-700" />
            <p className="font-mono-jb text-[10px] uppercase tracking-[0.22em] text-red-800">
              {error}
            </p>
          </div>
        ) : (
          <pre className="m-0 whitespace-pre overflow-x-auto p-5 font-mono-jb text-[12.5px] leading-relaxed text-[#2a2620]">
            <code dangerouslySetInnerHTML={{ __html: highlight(code) }} />
          </pre>
        )}
      </div>
    </div>
  );
}

// highlight does a bare-minimum pass over the HTML body: tags get one
// color, attributes another, string literals a third, comments dimmed.
// It's not a real tokenizer — just enough to make the listing pleasant.
// Order matters: we escape first, then layer regexes from most-specific
// to least.
function highlight(raw: string): string {
  const escaped = raw
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  // Comments first (so we don't paint tags inside <!-- … -->).
  let out = escaped.replace(
    /(&lt;!--[\s\S]*?--&gt;)/g,
    '<span style="color:#a59b8a;font-style:italic">$1</span>',
  );

  // String literals — both " and ' kept compact, single-line only.
  out = out.replace(
    /(&quot;[^&\n]*?&quot;|&#39;[^&\n]*?&#39;|"[^"\n]*?"|'[^'\n]*?')/g,
    '<span style="color:#a05223">$1</span>',
  );

  // HTML tags — opening or closing, captures the tag name region.
  out = out.replace(
    /(&lt;\/?)([a-zA-Z][\w-]*)/g,
    '$1<span style="color:#3060b5">$2</span>',
  );

  // Highlight attribute names (word=).
  out = out.replace(
    /(\s)([a-zA-Z_:][\w:-]*)(=)/g,
    '$1<span style="color:#7a4eac">$2</span>$3',
  );

  return out;
}
