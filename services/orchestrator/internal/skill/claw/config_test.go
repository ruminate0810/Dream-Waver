package claw

import (
	"path/filepath"
	"testing"
)

// TestDynamicRebinding covers 真·动态改绑: re-assigning an execution tool to
// another role takes effect in EffectiveTools/roleEnabled, disables persist
// + reload, and invalid configs are rejected.
func TestDynamicRebinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claw-roles.json")
	r := &Runner{ImagesEnabled: true, Images: fakeImages{}}
	r.LoadConfig(path)

	// defaults: designer holds generate_image + edit_image
	if got := r.EffectiveTools(RoleDesigner); len(got) != 2 || got[0] != "generate_image" || got[1] != "edit_image" {
		t.Fatalf("designer default tools = %v", got)
	}

	// rebind generate_image → engineer, switch designer off
	if err := r.SetConfig(map[string]string{"generate_image": RoleEngineer}, map[string]bool{RoleDesigner: false}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if got := r.EffectiveTools(RoleEngineer); len(got) != 2 { // code_execute + generate_image
		t.Fatalf("engineer tools after rebind = %v", got)
	}
	if got := r.EffectiveTools(RoleDesigner); len(got) != 1 || got[0] != "edit_image" { // keeps edit_image
		t.Fatalf("designer should keep only edit_image, got %v", got)
	}
	if r.roleEnabled(RoleDesigner) {
		t.Fatal("designer should be disabled")
	}
	// engineer has generate_image wired (fake images) even with no sandbox
	if !r.roleEnabled(RoleEngineer) {
		t.Fatal("engineer should be enabled via rebound generate_image")
	}

	// persisted → a fresh runner loads the same config
	r2 := &Runner{ImagesEnabled: true, Images: fakeImages{}}
	r2.LoadConfig(path)
	if got := r2.EffectiveTools(RoleEngineer); len(got) != 2 {
		t.Fatalf("reloaded engineer tools = %v", got)
	}
	if !r2.roleSwitchedOff(RoleDesigner) {
		t.Fatal("designer disable should persist")
	}

	// invalid: fixed tool rebind / bad target / disabling the writer
	if err := r.SetConfig(map[string]string{"write_document": RoleEngineer}, nil); err == nil {
		t.Fatal("rebinding a fixed tool must fail")
	}
	if err := r.SetConfig(map[string]string{"web_search": RoleWriter}, nil); err == nil {
		t.Fatal("assigning to a non-exec role must fail")
	}
	if err := r.SetConfig(nil, map[string]bool{RoleWriter: false}); err == nil {
		t.Fatal("disabling the writer must fail")
	}
}
