package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PMQ C1 — document upload as input.
//
// Users would rather hand us a real document ("做成 PPT") than retype its gist
// into the topic box. ExtractDocument accepts ONE uploaded file (PDF / Markdown
// / plain text), pulls its text, and hands it back so the client can drop it
// into `reference_text` on the next POST /slides — where the outline planner
// already grounds itself on reference text (Input.ReferenceText). Nothing is
// stored: this is a stateless parse, so it stays anonymous-safe and adds no
// schema / persistence surface.

const (
	// maxDocUpload bounds the wire payload. 12 MB comfortably holds a long
	// PDF or a big markdown export without inviting abuse.
	maxDocUpload = 12 << 20
	// maxDocChars caps the text we feed the outline LLM. The planner wants a
	// DIGEST, not the whole book — 24k runes (~12–16k tokens) is plenty of
	// grounding while keeping the outline call cheap and in-budget.
	maxDocChars = 24000
)

type extractDocumentResponse struct {
	Filename  string `json:"filename"`
	Chars     int    `json:"chars"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"` // true when the doc exceeded maxDocChars
}

// ExtractDocument handles POST /api/v1/documents/extract (multipart, field
// "file"). Returns the extracted text or a friendly Chinese error.
func (h *handlers) ExtractDocument(w http.ResponseWriter, r *http.Request) {
	// Hard cap the body a touch above maxDocUpload so a giant upload is
	// rejected by the reader rather than buffered whole.
	r.Body = http.MaxBytesReader(w, r.Body, maxDocUpload+(1<<20))
	if err := r.ParseMultipartForm(maxDocUpload); err != nil {
		errorJSON(w, http.StatusBadRequest, "无法解析上传（文件过大或不是有效的表单）："+err.Error())
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "缺少上传文件（字段名应为 file）")
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "读取文件失败："+err.Error())
		return
	}

	name := strings.TrimSpace(hdr.Filename)
	ext := strings.ToLower(filepath.Ext(name))

	var text string
	switch ext {
	case ".pdf":
		text, err = extractPDFText(raw)
		if err != nil {
			errorJSON(w, http.StatusUnprocessableEntity, "PDF 解析失败："+err.Error())
			return
		}
	case ".md", ".markdown", ".txt", ".text", ".csv", "":
		// Treat anything text-shaped (or extensionless) as UTF-8 text.
		text = string(raw)
	default:
		errorJSON(w, http.StatusUnsupportedMediaType,
			"暂仅支持 PDF / Markdown / txt 文件（收到 "+ext+"）")
		return
	}

	text = normalizeExtractedText(text)
	if strings.TrimSpace(text) == "" {
		errorJSON(w, http.StatusUnprocessableEntity,
			"没有从文档中提取到文本（可能是扫描版 / 纯图片 PDF）")
		return
	}

	truncated := false
	if runes := []rune(text); len(runes) > maxDocChars {
		text = strings.TrimSpace(string(runes[:maxDocChars]))
		truncated = true
	}

	writeJSON(w, http.StatusOK, extractDocumentResponse{
		Filename:  name,
		Chars:     len([]rune(text)),
		Text:      text,
		Truncated: truncated,
	})
}

// extractPDFText pulls plain text from a PDF byte slice via ledongthuc/pdf.
// That library is known to panic on some malformed PDFs, so we recover into a
// plain error — a bad upload must never take down the server.
func extractPDFText(raw []byte) (text string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			text = ""
			err = fmt.Errorf("文件已损坏或为不支持的 PDF 变体 (%v)", rec)
		}
	}()
	pr, perr := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if perr != nil {
		return "", perr
	}
	body, perr := pr.GetPlainText()
	if perr != nil {
		return "", perr
	}
	var buf bytes.Buffer
	if _, perr := buf.ReadFrom(body); perr != nil {
		return "", perr
	}
	return buf.String(), nil
}

var (
	// strip BOM / zero-width no-break / zero-width space. Written with \x{..}
	// codepoint escapes in a raw string so the SOURCE stays pure ASCII — Go
	// rejects a literal U+FEFF mid-file.
	docZeroWidthRe = regexp.MustCompile(`[\x{FEFF}\x{200B}\x{200C}\x{200D}]`)
	// non-breaking + ideographic spaces → a regular space.
	docNbspRe = regexp.MustCompile(`[\x{00A0}\x{3000}]`)
	// collapse runs of 2+ spaces/tabs (PDF extraction sprays these) to one.
	docMultiSpaceRe = regexp.MustCompile(`[ \t]{2,}`)
	// collapse 3+ newlines to a paragraph break.
	docMultiNewlineRe = regexp.MustCompile(`\n{3,}`)
)

// normalizeExtractedText tidies raw extraction output into a compact digest:
// normalize line endings, strip NULs/BOM/zero-widths, convert non-breaking
// spaces, collapse whitespace runs, keep paragraph structure (markdown headings
// survive — useful signal for the outline planner).
func normalizeExtractedText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\x00", "")
	s = docZeroWidthRe.ReplaceAllString(s, "")
	s = docNbspRe.ReplaceAllString(s, " ")
	s = docMultiSpaceRe.ReplaceAllString(s, " ")
	s = docMultiNewlineRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
