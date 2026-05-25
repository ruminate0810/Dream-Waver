package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgxUsers is the production implementation. The interface lives in
// store.go alongside the other entity interfaces.
type pgxUsers struct {
	pool *pgxpool.Pool
}

// UpsertFromJWT mirrors a Supabase auth.users row into our local
// `users` table. Called by the auth middleware on every request — we
// could cache and skip if the user already exists, but a single
// `insert ... on conflict do update set email = excluded.email` per
// request is cheap and keeps the email fresh when users change it on
// the Supabase side.
func (u *pgxUsers) UpsertFromJWT(ctx context.Context, id uuid.UUID, email string) (*User, error) {
	row := u.pool.QueryRow(ctx, `
		insert into users (id, email)
		values ($1, $2)
		on conflict (id) do update set email = excluded.email
		returning id, email, created_at
	`, id, email)
	out := &User{}
	if err := row.Scan(&out.ID, &out.Email, &out.CreatedAt); err != nil {
		return nil, err
	}
	return out, nil
}

func (u *pgxUsers) Get(ctx context.Context, id uuid.UUID) (*User, error) {
	row := u.pool.QueryRow(ctx, `select id, email, created_at from users where id = $1`, id)
	out := &User{}
	if err := row.Scan(&out.ID, &out.Email, &out.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return out, nil
}

func (u *pgxUsers) GetByEmail(ctx context.Context, email string) (*User, error) {
	row := u.pool.QueryRow(ctx, `select id, email, created_at from users where email = $1`, email)
	out := &User{}
	if err := row.Scan(&out.ID, &out.Email, &out.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return out, nil
}

// ─── In-memory implementation (local dev / tests) ──────────────────

type memUsers struct {
	byID    map[uuid.UUID]*User
	byEmail map[string]*User
	mu      lock
}

func newMemUsers() *memUsers {
	return &memUsers{
		byID:    map[uuid.UUID]*User{},
		byEmail: map[string]*User{},
	}
}

func (m *memUsers) UpsertFromJWT(_ context.Context, id uuid.UUID, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.byID[id]; ok {
		u.Email = email
		m.byEmail[email] = u
		return u, nil
	}
	u := &User{ID: id, Email: email, CreatedAt: time.Now().UTC()}
	m.byID[id] = u
	if email != "" {
		m.byEmail[email] = u
	}
	return u, nil
}

func (m *memUsers) Get(_ context.Context, id uuid.UUID) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.byID[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}

func (m *memUsers) GetByEmail(_ context.Context, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.byEmail[email]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}
