package billing

// Pricing table — micro-units (1 USD = 1_000_000). The map is
// deliberately static, not config-driven: price changes are deploys,
// not runtime knobs. Keeps the invariant "every call to X this hour
// paid the same" trivially true and makes the cost of a deploy
// explicit (a price change is a code change).
//
// Document each price's reasoning so future "why $0.005?" archaeology
// is one grep away.
//
// Margin posture: we charge ~2-3× the underlying provider cost so a
// run that fans out (e.g. variants → 4 calls) doesn't push us
// negative when DeepSeek tokens + DreamAPI fees + Fly compute stack.
// Sharper unit economics is a Phase 6 problem, not an MVP one.
var Prices = map[string]int64{
	// ─── Design (DreamAPI Flux + Gemini) ─────────────────────────
	"generate_image":      5_000,  // $0.005 — DreamAPI Flux text2image ≈ $0.002/call
	"generate_variants":   18_000, // $0.018 — 4 variants in one call; pricing reflects 4 outputs
	"edit_remove_bg":      6_000,  // $0.006 — DreamAPI matting ≈ $0.003
	"edit_enhance":        8_000,  // $0.008 — super-resolution ≈ $0.004
	"edit_outpaint":       8_000,  // $0.008 — same cost band as enhance
	"edit_image2image":    6_000,  // $0.006 — Flux i2i similar to text2image
	"edit_colorize":       4_000,  // $0.004 — DreamAPI colorize ≈ $0.002
	// NanoBanana — Gemini 3.1 Flash Image Preview. ~$0.005 cost via the
	// df-ability gateway; price at ~2× margin.
	"generate_nano_banana":     10_000, // $0.010 — Gemini Flash w/ optional refs
	// NanoBanana Pro — Gemini 3 Pro Image Preview ≈ $0.012 cost.
	"generate_nano_banana_pro": 25_000, // $0.025 — Gemini Pro

	// Smart chat routing — planner-tier LLM classification call.
	// Tiny by design (~150-token completion, prompt is small) so the
	// per-message cost stays well under 1% of the eventual image-gen
	// fee that the routed action triggers.
	"chat_route": 200, // $0.0002

	// Seedance 1.5 Pro image-to-video — three resolution tiers. Costs
	// scale roughly with output pixel-seconds; the table reflects
	// 5s/720p as the baseline (≈ $0.06 cost), with ½ for 480p and 3×
	// for 1080p. Frontend caps duration at 12s so worst-case spend
	// is 12s/1080p ≈ $0.36 / call billed at $0.60 (1.6× margin).
	"video_seedance_480p":  30_000,  // $0.030 — 5s/480p baseline
	"video_seedance_720p":  100_000, // $0.100 — 5s/720p baseline
	"video_seedance_1080p": 300_000, // $0.300 — 5s/1080p baseline

	// ─── Slides ──────────────────────────────────────────────────
	// outline + content are pure LLM (DeepSeek); per-token billing
	// is X5+ — for now we charge per render which captures the
	// real expensive bit (chromedp screenshot + unioffice
	// assembly).
	"slide_render":        1_000,  // $0.001 — per-deck full render
	"slide_render_dirty":  300,    // $0.0003 — incremental edit render

	// ─── Video (Opendream / Seedance) ────────────────────────────
	// Seedance i2v is the dominant cost driver — ~$0.05 per clip
	// at current rates. Charge near cost; we make margin on the
	// orchestrator + storage rather than the LLM passthrough.
	"video_create_run":    20_000, // $0.020 — orchestration + char sheet generation
	"video_clip":          75_000, // $0.075 — per Seedance scene clip
	"video_regen":         75_000, // $0.075 — same cost as initial clip

	// ─── Sandbox + research ──────────────────────────────────────
	"code_execute":        2_000,  // $0.002 — sandbox CPU time amortised
	"web_research":        500,    // $0.0005 — Tavily search

	// Unmetered tools (no entry below) — agent terminators,
	// internal planning calls — charge 0 micro and skip Debit().
	// Listed here as comments to make the gap intentional:
	//   "terminate"        → 0 (agent loop control)
	//   "plan_outline"     → 0 (rolled into slide_render)
	//   "write_content"    → 0 (rolled into slide_render)
}

// PriceOf returns the cost of one invocation in micro-units. Returns
// 0 for tools not in the table — Debit() should be skipped for those.
func PriceOf(toolName string) int64 {
	if p, ok := Prices[toolName]; ok {
		return p
	}
	return 0
}

// TrialGrantMicro is the credit pool seeded on a user's first login.
// $1.00 = enough to render 1000 slides or generate 50 variants or
// kick off 10 short video runs — comfortably more than someone needs
// to evaluate Dream-Waver.
const TrialGrantMicro int64 = 1_000_000

// FormatUSD converts a micro-unit amount to a human-readable dollar
// string. Used by the 402 response so the user sees "$0.005 required"
// rather than "5000 required". Always rounds to 4 decimals — Flux
// pricing matters to the millicent.
func FormatUSD(micro int64) string {
	dollars := float64(micro) / 1_000_000.0
	if dollars >= 1 {
		return formatTwoDecimals(dollars)
	}
	return formatFourDecimals(dollars)
}

func formatTwoDecimals(d float64) string {
	// stdlib fmt is fine; we just don't want a slow Sprintf in the
	// hot path of a 402. Two helpers keeps the call site readable.
	return floatToString(d, 2)
}

func formatFourDecimals(d float64) string {
	return floatToString(d, 4)
}

func floatToString(d float64, precision int) string {
	// Lightweight formatter — strconv.FormatFloat without the
	// reflect-fmt path. Inputs are always non-negative.
	negative := d < 0
	if negative {
		d = -d
	}
	mult := int64(1)
	for i := 0; i < precision; i++ {
		mult *= 10
	}
	scaled := int64(d*float64(mult) + 0.5)
	whole := scaled / mult
	frac := scaled % mult

	out := "$"
	if negative {
		out = "-$"
	}
	out += itoa(whole) + "."
	fracStr := itoa(frac)
	// Pad with leading zeros to `precision` digits.
	for len(fracStr) < precision {
		fracStr = "0" + fracStr
	}
	out += fracStr
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
