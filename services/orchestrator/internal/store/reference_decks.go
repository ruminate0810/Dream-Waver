package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReferenceDeck is one row in reference_decks — a high-quality exemplar
// deck the outline planner uses as inspiration. Sprint BR.3.
//
// The OutlineJSON is the contract surface: stages.OutlineResult
// round-trips through it. Retrieve returns these whole, but
// plan_outline.go only ever serialises OutlineJSON back to the planner
// LLM (the rest is metadata for attribution + retrieval ranking).
type ReferenceDeck struct {
	ID            uuid.UUID
	Slug          string
	Scenario      string
	BlueprintID   string    // may be empty
	Theme         string    // schema.Theme key
	TopicTags     []string  // e.g. ["SaaS", "B2B", "AI"]
	Title         string
	OutlineJSON   []byte    // raw OutlineResult JSON
	ContentJSON   []byte    // optional; reserved for BR.next
	SourceJobID   uuid.UUID // uuid.Nil for hand-curated
	QualityScore  int       // 0–5 (0 = unscored)
	CreatedAt     time.Time
}

// RetrieveQuery is the input shape for Retrieve. Keep narrow — only
// the fields the keyword/tag scorer cares about.
type RetrieveQuery struct {
	Topic       string   // free text; we tokenize on whitespace
	Scenario    string   // optional — boosts matching scenario
	BlueprintID string   // optional — boosts matching blueprint id
	ExtraTags   []string // optional — explicit tags from caller
	K           int      // how many to return; defaults to 2
	// MinQuality filters out rows with quality_score < this. Defaults
	// to 0 (include unscored). Set to 3+ once the seed corpus has
	// human-scored entries to bias toward known-good examples.
	MinQuality int
}

// ReferenceDecks is the persistence boundary. Global (no workspace
// scope) — every user benefits from the same curated corpus.
type ReferenceDecks interface {
	Insert(ctx context.Context, r *ReferenceDeck) (*ReferenceDeck, error)
	GetBySlug(ctx context.Context, slug string) (*ReferenceDeck, error)
	GetByID(ctx context.Context, id uuid.UUID) (*ReferenceDeck, error)
	// Retrieve runs a keyword+tag overlap scorer and returns top-K
	// reference decks. The scoring is deliberately simple (MVP);
	// callers should treat the order as "best guess" not strict
	// relevance. Empty result is a normal outcome — fall back to
	// free-form planning when nothing scores high enough.
	Retrieve(ctx context.Context, q RetrieveQuery) ([]*ReferenceDeck, error)
}

// ErrReferenceDeckNotFound is returned by GetBySlug / GetByID when
// the slug/id doesn't exist. Insert returns it wrapped in a unique
// constraint error when slug collides.
var ErrReferenceDeckNotFound = errors.New("reference deck not found")

// ─── tokenize: lowercased word/CJK-char split ────────────────────────
//
// Topic relevance is just "how many of the topic's tokens appear in
// the candidate's title + tags". We split on whitespace + ASCII
// punctuation; CJK characters become their own tokens so e.g. "Series A
// 路演" produces ["series","a","路","演"]. Crude but works fine on
// MVP-scale corpora.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z':
			cur.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF: // CJK unified ideographs
			flush()
			out = append(out, string(r))
		default:
			flush()
		}
	}
	flush()
	return out
}

// scoreReference applies the same scoring formula whether we're in pgx
// or memory mode — keeps the two implementations aligned without
// duplicating the rule.
//
// Score rules (additive, integer):
//   +5  blueprint_id matches the query (strongest signal: same scenario)
//   +3  scenario matches the query
//   +1  per topic_tag that overlaps a query token (capped at 4 total)
//   +1  per quality_score point (so a 5-star scored deck = +5)
//   +1  baseline (so unscored, no-match references are still surfaceable)
func scoreReference(r *ReferenceDeck, queryTokens map[string]bool, q RetrieveQuery) int {
	score := 1
	if q.BlueprintID != "" && r.BlueprintID == q.BlueprintID {
		score += 5
	}
	if q.Scenario != "" && r.Scenario == q.Scenario {
		score += 3
	}
	tagHits := 0
	for _, tag := range r.TopicTags {
		for _, t := range tokenize(tag) {
			if queryTokens[t] {
				tagHits++
				break
			}
		}
		if tagHits >= 4 {
			break
		}
	}
	score += tagHits
	score += int(r.QualityScore)
	return score
}

// ─── pgx implementation ──────────────────────────────────────────────

type pgxReferenceDecks struct{ pool *pgxpool.Pool }

func newPgxReferenceDecks(pool *pgxpool.Pool) *pgxReferenceDecks {
	return &pgxReferenceDecks{pool: pool}
}

func (s *pgxReferenceDecks) Insert(ctx context.Context, r *ReferenceDeck) (*ReferenceDeck, error) {
	if strings.TrimSpace(r.Slug) == "" {
		return nil, fmt.Errorf("reference deck: slug required")
	}
	if r.Scenario == "" {
		return nil, fmt.Errorf("reference deck: scenario required")
	}
	if r.Theme == "" {
		return nil, fmt.Errorf("reference deck: theme required")
	}
	if len(r.OutlineJSON) == 0 {
		return nil, fmt.Errorf("reference deck: outline_json required")
	}
	// Validate json — defensive, errors out fast instead of letting PG
	// throw an opaque jsonb syntax error.
	if !json.Valid(r.OutlineJSON) {
		return nil, fmt.Errorf("reference deck: outline_json is not valid JSON")
	}
	if len(r.ContentJSON) > 0 && !json.Valid(r.ContentJSON) {
		return nil, fmt.Errorf("reference deck: content_json is not valid JSON")
	}

	var contentArg any
	if len(r.ContentJSON) > 0 {
		contentArg = r.ContentJSON
	}
	var sourceArg any
	if r.SourceJobID != uuid.Nil {
		sourceArg = r.SourceJobID
	}

	var (
		id        uuid.UUID
		createdAt time.Time
	)
	err := s.pool.QueryRow(ctx, `
		insert into reference_decks (
			slug, scenario, blueprint_id, theme, topic_tags,
			title, outline_json, content_json, source_job_id, quality_score
		) values ($1, $2, NULLIF($3,''), $4, $5,
		          $6, $7, $8, $9, $10)
		returning id, created_at
	`,
		r.Slug, r.Scenario, r.BlueprintID, r.Theme, r.TopicTags,
		r.Title, r.OutlineJSON, contentArg, sourceArg, r.QualityScore,
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert reference deck: %w", err)
	}
	out := *r
	out.ID = id
	out.CreatedAt = createdAt
	return &out, nil
}

func (s *pgxReferenceDecks) GetBySlug(ctx context.Context, slug string) (*ReferenceDeck, error) {
	return s.queryOne(ctx, `where slug = $1`, slug)
}

func (s *pgxReferenceDecks) GetByID(ctx context.Context, id uuid.UUID) (*ReferenceDeck, error) {
	return s.queryOne(ctx, `where id = $1`, id)
}

func (s *pgxReferenceDecks) queryOne(ctx context.Context, where string, args ...any) (*ReferenceDeck, error) {
	row := s.pool.QueryRow(ctx, `
		select id, slug, scenario, coalesce(blueprint_id, ''), theme,
		       topic_tags, title, outline_json,
		       coalesce(content_json, '{}'::jsonb),
		       coalesce(source_job_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       quality_score, created_at
		from reference_decks `+where, args...)
	r := &ReferenceDeck{}
	var contentJSON []byte
	if err := row.Scan(
		&r.ID, &r.Slug, &r.Scenario, &r.BlueprintID, &r.Theme,
		&r.TopicTags, &r.Title, &r.OutlineJSON,
		&contentJSON, &r.SourceJobID, &r.QualityScore, &r.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrReferenceDeckNotFound
		}
		return nil, err
	}
	// Treat the "{}" placeholder from coalesce as "no content".
	if len(contentJSON) > 0 && string(contentJSON) != "{}" {
		r.ContentJSON = contentJSON
	}
	return r, nil
}

// Retrieve fetches a CANDIDATE SET via cheap PG-side filtering (tag
// overlap OR scenario OR blueprint match), then scores in Go. We don't
// score in SQL because the per-tag/per-token math is awkward — and the
// candidate set after PG filtering is small enough (<100 rows) to
// score in-process under a millisecond. When the corpus grows past
// ~10k rows we'll revisit (likely with pgvector).
func (s *pgxReferenceDecks) Retrieve(ctx context.Context, q RetrieveQuery) ([]*ReferenceDeck, error) {
	if q.K <= 0 {
		q.K = 2
	}
	// Build PG filter: row must match at least ONE of (scenario, blueprint, tag-overlap).
	// If none of those are specified we return the top-K by quality_score
	// as a soft baseline.
	tokens := tokenize(q.Topic)
	for _, t := range q.ExtraTags {
		tokens = append(tokens, tokenize(t)...)
	}
	tokenSet := map[string]bool{}
	for _, t := range tokens {
		tokenSet[t] = true
	}

	// Build the WHERE clause.
	var conds []string
	var args []any
	idx := 1
	if q.Scenario != "" {
		conds = append(conds, fmt.Sprintf("scenario = $%d", idx))
		args = append(args, q.Scenario)
		idx++
	}
	if q.BlueprintID != "" {
		conds = append(conds, fmt.Sprintf("blueprint_id = $%d", idx))
		args = append(args, q.BlueprintID)
		idx++
	}
	if len(tokens) > 0 {
		// Tag overlap via GIN: any tag matches any query token.
		// We pass the query tokens as a text[] and use && (overlap).
		conds = append(conds, fmt.Sprintf("topic_tags && $%d::text[]", idx))
		args = append(args, tokens)
		idx++
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " OR ")
	}
	args = append(args, q.MinQuality)
	args = append(args, q.K*8) // candidate pool 8x final K
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		select id, slug, scenario, coalesce(blueprint_id, ''), theme,
		       topic_tags, title, outline_json,
		       coalesce(content_json, '{}'::jsonb),
		       coalesce(source_job_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       quality_score, created_at
		from reference_decks
		%s
		%s quality_score >= $%d
		order by quality_score desc, created_at desc
		limit $%d
	`, where, andOrWhere(where), idx, idx+1), args...)
	if err != nil {
		return nil, fmt.Errorf("retrieve reference decks: %w", err)
	}
	defer rows.Close()
	cands := []*ReferenceDeck{}
	for rows.Next() {
		r := &ReferenceDeck{}
		var contentJSON []byte
		if err := rows.Scan(
			&r.ID, &r.Slug, &r.Scenario, &r.BlueprintID, &r.Theme,
			&r.TopicTags, &r.Title, &r.OutlineJSON,
			&contentJSON, &r.SourceJobID, &r.QualityScore, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(contentJSON) > 0 && string(contentJSON) != "{}" {
			r.ContentJSON = contentJSON
		}
		cands = append(cands, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Score + sort in Go (full deterministic).
	type scored struct {
		r *ReferenceDeck
		s int
	}
	scoredCands := make([]scored, 0, len(cands))
	for _, c := range cands {
		scoredCands = append(scoredCands, scored{r: c, s: scoreReference(c, tokenSet, q)})
	}
	// Stable sort by score desc, tie-break by quality desc then created_at desc.
	for i := 1; i < len(scoredCands); i++ {
		j := i
		for j > 0 && scoredCands[j-1].s < scoredCands[j].s {
			scoredCands[j-1], scoredCands[j] = scoredCands[j], scoredCands[j-1]
			j--
		}
	}
	if len(scoredCands) > q.K {
		scoredCands = scoredCands[:q.K]
	}
	out := make([]*ReferenceDeck, 0, len(scoredCands))
	for _, sc := range scoredCands {
		out = append(out, sc.r)
	}
	return out, nil
}

// andOrWhere returns " and" when a non-empty where clause is already
// present, otherwise " where". Lets the Retrieve SQL slot a follow-up
// predicate cleanly without dangling AND.
func andOrWhere(where string) string {
	if where == "" {
		return " where"
	}
	return " and"
}

// ─── memory implementation (tests + DB-less dev) ─────────────────────

type memReferenceDecks struct {
	mu     sync.RWMutex
	bySlug map[string]*ReferenceDeck
	byID   map[uuid.UUID]*ReferenceDeck
	order  []*ReferenceDeck // insertion order for stable Retrieve fallback
}

func newMemReferenceDecks() *memReferenceDecks {
	return &memReferenceDecks{
		bySlug: map[string]*ReferenceDeck{},
		byID:   map[uuid.UUID]*ReferenceDeck{},
	}
}

func (m *memReferenceDecks) Insert(ctx context.Context, r *ReferenceDeck) (*ReferenceDeck, error) {
	if strings.TrimSpace(r.Slug) == "" {
		return nil, fmt.Errorf("reference deck: slug required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.bySlug[r.Slug]; exists {
		return nil, fmt.Errorf("reference deck: slug %q already exists", r.Slug)
	}
	out := *r
	if out.ID == uuid.Nil {
		out.ID = uuid.New()
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = time.Now().UTC()
	}
	m.bySlug[out.Slug] = &out
	m.byID[out.ID] = &out
	m.order = append(m.order, &out)
	return &out, nil
}

func (m *memReferenceDecks) GetBySlug(ctx context.Context, slug string) (*ReferenceDeck, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.bySlug[slug]; ok {
		return r, nil
	}
	return nil, ErrReferenceDeckNotFound
}

func (m *memReferenceDecks) GetByID(ctx context.Context, id uuid.UUID) (*ReferenceDeck, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.byID[id]; ok {
		return r, nil
	}
	return nil, ErrReferenceDeckNotFound
}

func (m *memReferenceDecks) Retrieve(ctx context.Context, q RetrieveQuery) ([]*ReferenceDeck, error) {
	if q.K <= 0 {
		q.K = 2
	}
	m.mu.RLock()
	cands := make([]*ReferenceDeck, 0, len(m.order))
	for _, r := range m.order {
		if int(r.QualityScore) < q.MinQuality {
			continue
		}
		cands = append(cands, r)
	}
	m.mu.RUnlock()

	tokens := tokenize(q.Topic)
	for _, t := range q.ExtraTags {
		tokens = append(tokens, tokenize(t)...)
	}
	tokenSet := map[string]bool{}
	for _, t := range tokens {
		tokenSet[t] = true
	}
	type scored struct {
		r *ReferenceDeck
		s int
	}
	scoredCands := make([]scored, 0, len(cands))
	for _, c := range cands {
		scoredCands = append(scoredCands, scored{r: c, s: scoreReference(c, tokenSet, q)})
	}
	for i := 1; i < len(scoredCands); i++ {
		j := i
		for j > 0 && scoredCands[j-1].s < scoredCands[j].s {
			scoredCands[j-1], scoredCands[j] = scoredCands[j], scoredCands[j-1]
			j--
		}
	}
	if len(scoredCands) > q.K {
		scoredCands = scoredCands[:q.K]
	}
	out := make([]*ReferenceDeck, 0, len(scoredCands))
	for _, sc := range scoredCands {
		out = append(out, sc.r)
	}
	return out, nil
}
