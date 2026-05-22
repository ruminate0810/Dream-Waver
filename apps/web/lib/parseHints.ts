// parseHints — extract structured parameters the user wrote in natural
// language into the topic field. The Hero input and /slides/new chat
// surface both accept a single sentence; users naturally write "做一份
// 10 页投资路演 PPT" but the API takes slide_count as a separate
// number. Without this parse the natural-language hint is silently
// ignored and we use a default that doesn't match what the user asked
// for (the "我说 10 页做了 8 页" bug).
//
// Returns the parsed hints + a cleaned topic with the matched fragment
// removed (so the planner LLM doesn't see the redundant "10 页" once
// we've already passed slide_count=10).

export type TopicHints = {
  slideCount?: number; // 3..40 valid range, matches the API + UI cap
};

export type ParsedTopic = {
  cleanTopic: string;
  hints: TopicHints;
};

// Patterns in priority order. Chinese "页" and "张" are the most
// common; English "pages" / "slides" / "X-slide" cover the bilingual
// cases. (?<!\d) prevents "100页" from being captured as "00" (which
// would clamp to 3 and leave a stray "1" in the topic).
const PATTERNS: ReadonlyArray<RegExp> = [
  /(?<!\d)(\d{1,3})\s*页/u, // 10页, 10 页, 100 页
  /(?<!\d)(\d{1,3})\s*张/u, // 10张
  /(?<!\d)(\d{1,3})\s*-?\s*(?:slides?|pages?)\b/iu, // 10 slides, 12-slide, 20 pages
];

export function parseTopic(text: string): ParsedTopic {
  const hints: TopicHints = {};
  let cleanTopic = text;

  for (const re of PATTERNS) {
    const m = cleanTopic.match(re);
    if (!m) continue;
    const n = Number(m[1]);
    if (!Number.isFinite(n)) continue;
    // Clamp to the API's accepted range. 3 is the minimum a deck
    // makes sense at (title + content + closing); 40 is the upper
    // bound the form picker uses.
    hints.slideCount = Math.max(3, Math.min(40, n));
    // Strip the matched fragment from the topic so the planner doesn't
    // see "10 页" again as a redundant hint inside the topic text.
    cleanTopic = (cleanTopic.slice(0, m.index) + cleanTopic.slice((m.index ?? 0) + m[0].length))
      .replace(/\s{2,}/g, " ")
      .trim();
    break; // only honour the first match — multiple competing numbers
           // ("10 页 8 章 5 节") is ambiguous; let the first win.
  }

  return { cleanTopic, hints };
}
