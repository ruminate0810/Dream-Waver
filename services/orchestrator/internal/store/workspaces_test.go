package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// These tests target the in-memory implementation, but the contract
// they assert is the Workspaces interface — so the same suite can be
// rerun against pgxWorkspaces once a Postgres test container is
// wired (Phase 4 follow-up). Keep all assertions interface-typed.

func TestMemWorkspaces_EnsurePersonal_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := newMemWorkspaces()
	user := uuid.New()
	// Foreign-key constraint is not enforced in memory — we don't
	// need to pre-insert the user row for the in-memory contract.

	first, created, err := ws.EnsurePersonal(ctx, user)
	if err != nil {
		t.Fatalf("first EnsurePersonal: %v", err)
	}
	if !created {
		t.Fatalf("first EnsurePersonal should report created=true")
	}
	if first.Kind != "personal" {
		t.Fatalf("kind = %q, want personal", first.Kind)
	}
	if first.OwnerUserID != user {
		t.Fatalf("owner_user_id mismatch")
	}

	// Second call must return the SAME workspace AND created=false —
	// auth middleware calls EnsurePersonal on every request and the
	// trial-grant hook keys off created==true; drift here would
	// regrant credit forever.
	second, created2, err := ws.EnsurePersonal(ctx, user)
	if err != nil {
		t.Fatalf("second EnsurePersonal: %v", err)
	}
	if created2 {
		t.Fatalf("second EnsurePersonal should report created=false (would re-fire one-time hooks)")
	}
	if first.ID != second.ID {
		t.Fatalf("EnsurePersonal not idempotent: got two workspaces %v and %v", first.ID, second.ID)
	}

	// Owner is auto-added as member with role=owner.
	ok, err := ws.IsMember(ctx, first.ID, user)
	if err != nil {
		t.Fatalf("IsMember: %v", err)
	}
	if !ok {
		t.Fatalf("owner is not a member of their own personal workspace")
	}
}

func TestMemWorkspaces_CreateTeam_OwnerAutoMember(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := newMemWorkspaces()
	owner := uuid.New()

	team, err := ws.CreateTeam(ctx, owner, "Engineering")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team.Kind != "team" {
		t.Fatalf("kind = %q, want team", team.Kind)
	}
	if team.Name != "Engineering" {
		t.Fatalf("name = %q", team.Name)
	}

	// The constructor MUST insert the owner as a member with role
	// "owner" — otherwise the route layer's owner-only-can-write
	// rule would reject the creator from their own workspace.
	members, err := ws.ListMembers(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != owner || members[0].Role != "owner" {
		t.Fatalf("expected single owner member, got %+v", members)
	}
}

func TestMemWorkspaces_CreateTeam_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := newMemWorkspaces()
	if _, err := ws.CreateTeam(ctx, uuid.New(), "  "); err == nil {
		t.Fatalf("CreateTeam with whitespace-only name should fail")
	}
}

func TestMemWorkspaces_AddRemoveMember(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := newMemWorkspaces()
	owner := uuid.New()
	team, _ := ws.CreateTeam(ctx, owner, "Team")

	// Invite second user.
	invitee := uuid.New()
	if err := ws.AddMember(ctx, team.ID, invitee, "member"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	ok, _ := ws.IsMember(ctx, team.ID, invitee)
	if !ok {
		t.Fatalf("invitee not a member after AddMember")
	}

	// Re-inviting → conflict (idempotent intent would need an
	// upsert flag — for now the store rejects so the route handler
	// can surface a sensible "already a member" message).
	if err := ws.AddMember(ctx, team.ID, invitee, "member"); !errors.Is(err, ErrConflict) {
		t.Fatalf("AddMember twice expected ErrConflict, got %v", err)
	}

	// Remove the invitee.
	if err := ws.RemoveMember(ctx, team.ID, invitee); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if ok, _ := ws.IsMember(ctx, team.ID, invitee); ok {
		t.Fatalf("invitee still a member after RemoveMember")
	}

	// Removing the owner is forbidden — workspace ownership transfer
	// is a separate route (not implemented in Phase 1).
	if err := ws.RemoveMember(ctx, team.ID, owner); err == nil {
		t.Fatalf("RemoveMember(owner) should fail")
	}
}

func TestMemWorkspaces_IsMember_IsolatesPerWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := newMemWorkspaces()
	alice := uuid.New()
	bob := uuid.New()

	aliceWS, _, _ := ws.EnsurePersonal(ctx, alice)
	bobWS, _, _ := ws.EnsurePersonal(ctx, bob)

	// Alice ∈ alice's WS but NOT in bob's. This is the gate every
	// job-table read/write checks; getting it wrong is a tenant leak.
	cases := []struct {
		name      string
		workspace uuid.UUID
		user      uuid.UUID
		want      bool
	}{
		{"alice in alice ws", aliceWS.ID, alice, true},
		{"alice in bob ws", bobWS.ID, alice, false},
		{"bob in bob ws", bobWS.ID, bob, true},
		{"bob in alice ws", aliceWS.ID, bob, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := ws.IsMember(ctx, tc.workspace, tc.user)
			if err != nil {
				t.Fatalf("IsMember: %v", err)
			}
			if ok != tc.want {
				t.Fatalf("IsMember = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestMemWorkspaces_ListForUser_OnlyMembershipMatters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := newMemWorkspaces()
	alice := uuid.New()
	bob := uuid.New()

	// alice has a personal + a team she created; bob has only a
	// personal. After alice invites bob, bob's list grows.
	aliceP, _, _ := ws.EnsurePersonal(ctx, alice)
	team, _ := ws.CreateTeam(ctx, alice, "Engineering")
	_, _, _ = ws.EnsurePersonal(ctx, bob)

	bobList, _ := ws.ListForUser(ctx, bob)
	if len(bobList) != 1 {
		t.Fatalf("bob should see 1 workspace (personal), got %d", len(bobList))
	}

	if err := ws.AddMember(ctx, team.ID, bob, "member"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	bobList2, _ := ws.ListForUser(ctx, bob)
	if len(bobList2) != 2 {
		t.Fatalf("after invite bob should see 2 workspaces, got %d", len(bobList2))
	}

	// Alice still sees her personal + team (size unchanged by the
	// invite — invites add members to a workspace, not the inviter's
	// list).
	aliceList, _ := ws.ListForUser(ctx, alice)
	if len(aliceList) != 2 {
		t.Fatalf("alice list = %d, want 2 (personal + team)", len(aliceList))
	}

	// Sanity: alice's personal workspace is in alice's list.
	found := false
	for _, w := range aliceList {
		if w.ID == aliceP.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("alice's personal workspace missing from her list")
	}
}
