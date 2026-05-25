package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"crypto/rsa"
	"encoding/base64"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// jwtVerifier is the JWT verification engine. Created via newJWTVerifier
// from Config; the auth middleware calls Verify(ctx, r) per request.
//
// Two paths:
//   - Real Supabase JWT: signature verified against JWKS (cached 24 h),
//     `sub` + `email` extracted.
//   - Dev fallback (Config.JWKSURL == "" && Config.DevMode): trust the
//     X-Dev-User-Id + X-Dev-User-Email headers. The middleware logs a
//     loud warning once at boot to make accidental prod use obvious.
type jwtVerifier struct {
	cfg  Config
	jwks *jwksCache
}

func newJWTVerifier(cfg Config) *jwtVerifier {
	v := &jwtVerifier{cfg: cfg}
	if cfg.JWKSURL != "" {
		v.jwks = newJWKSCache(cfg.JWKSURL)
	}
	if cfg.JWKSURL == "" && cfg.DevMode {
		slog.Warn("auth: DEV MODE — accepting X-Dev-User-Id headers without signature verification. Do NOT use in production.")
	}
	return v
}

// Verify returns (userID, email, error). Both userID and email come
// from the JWT `sub` and `email` claims, or from the dev-mode headers.
func (v *jwtVerifier) Verify(ctx context.Context, r *http.Request) (uuid.UUID, string, error) {
	// Dev-mode fallback short-circuits before any header parsing so
	// it's obvious in trace what path we took.
	if v.jwks == nil && v.cfg.DevMode {
		raw := strings.TrimSpace(r.Header.Get("X-Dev-User-Id"))
		if raw == "" {
			return uuid.Nil, "", errors.New("missing X-Dev-User-Id header (dev mode, set Authorization for prod)")
		}
		uid, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, "", fmt.Errorf("bad X-Dev-User-Id: %w", err)
		}
		email := strings.TrimSpace(r.Header.Get("X-Dev-User-Email"))
		if email == "" {
			email = fmt.Sprintf("%s@dev.local", uid.String()[:8])
		}
		return uid, email, nil
	}

	if v.jwks == nil {
		return uuid.Nil, "", errors.New("auth not configured (set SUPABASE_JWKS_URL)")
	}

	// Real path: parse + verify the Bearer token.
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return uuid.Nil, "", errors.New("missing Authorization: Bearer <jwt>")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if raw == "" {
		return uuid.Nil, "", errors.New("empty Bearer token")
	}

	token, err := jwt.Parse(raw, v.keyFunc(ctx), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("verify jwt: %w", err)
	}
	if !token.Valid {
		return uuid.Nil, "", errors.New("invalid jwt")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return uuid.Nil, "", errors.New("unexpected jwt claims shape")
	}
	if v.cfg.Audience != "" {
		// jwt.MapClaims.VerifyAudience is deprecated in v5; do the
		// check by hand because Supabase emits `aud` as a single
		// string ("authenticated") not an array.
		aud, _ := claims["aud"].(string)
		if aud != v.cfg.Audience {
			return uuid.Nil, "", fmt.Errorf("unexpected aud=%q (want %q)", aud, v.cfg.Audience)
		}
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return uuid.Nil, "", errors.New("jwt missing sub claim")
	}
	uid, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("jwt sub is not a uuid: %w", err)
	}
	email, _ := claims["email"].(string)
	return uid, email, nil
}

// keyFunc returns the per-request key resolver. We look up the
// `kid` header in the JWKS cache and return the matching RSA pubkey.
func (v *jwtVerifier) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("jwt missing kid header")
		}
		key, err := v.jwks.Get(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("jwks lookup: %w", err)
		}
		return key, nil
	}
}

// ─── JWKS cache ─────────────────────────────────────────────────────

type jwksCache struct {
	url string

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time

	httpClient *http.Client
}

func newJWKSCache(url string) *jwksCache {
	return &jwksCache{
		url:        url,
		keys:       map[string]*rsa.PublicKey{},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Get returns the public key for `kid`. Refreshes the cache once per
// 24h (or on first miss) — kid rotation triggers a forced refresh.
func (c *jwksCache) Get(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	stale := time.Since(c.fetched) > 24*time.Hour
	c.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown kid %q", kid)
}

func (c *jwksCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch %s: %s", c.url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("parse jwks: %w", err)
	}

	fresh := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		key, err := parseRSAFromJWK(k.N, k.E)
		if err != nil {
			slog.Warn("auth: skipping malformed jwk", "kid", k.Kid, "err", err)
			continue
		}
		fresh[k.Kid] = key
	}
	c.mu.Lock()
	c.keys = fresh
	c.fetched = time.Now()
	c.mu.Unlock()
	return nil
}

// parseRSAFromJWK reconstructs an RSA public key from base64url-encoded
// n and e (the JWK encoding per RFC 7518).
func parseRSAFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	// E is small (typically 65537); read big-endian into int.
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("e=0")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}
