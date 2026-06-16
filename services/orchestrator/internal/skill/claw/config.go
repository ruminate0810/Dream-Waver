package claw

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// config.go — 真·动态改绑. The role↔tool bindings and role enablement are
// runtime configuration, not compile-time constants: the three EXECUTION
// tools can be re-assigned between the execution-phase workers, and the
// optional workers can be switched off entirely. The config persists to a
// small JSON file (works in dev without a database) and every run reads the
// effective bindings when assembling sub-agents, so a change applies to the
// very next run.

// RebindableTools is the pool that can move between execution roles. The
// orchestration tools (plan_tasks/update_task on the coordinator,
// write_document on writer+critic, generate_deck on the producer) are FIXED
// — the phase machine runs those roles by key.
var RebindableTools = []string{"web_search", "code_execute", "generate_image", "edit_image"}

// RebindTargets are the roles a rebindable tool may be assigned to.
var RebindTargets = []string{RoleResearcher, RoleEngineer, RoleDesigner}

// togglableRoles can be enabled/disabled; coordinator and writer are the
// spine of every run and stay on.
var togglableRoles = map[string]bool{
	RoleResearcher: true,
	RoleEngineer:   true,
	RoleDesigner:   true,
	RoleCritic:       true,
	RoleProducer:     true,
	RoleVideographer: true,
}

type roleConfig struct {
	Assign  map[string]string `json:"assign"`  // tool → role key
	Enabled map[string]bool   `json:"enabled"` // role key → on/off (absent = on)
}

func defaultAssign() map[string]string {
	out := map[string]string{}
	for _, t := range RebindableTools {
		for _, r := range allRoles {
			for _, rt := range r.ToolNames {
				if rt == t {
					out[t] = r.Key
				}
			}
		}
	}
	return out
}

// ── Runner config surface ────────────────────────────────────────────────

func (r *Runner) cfgInit() {
	r.cfgOnce.Do(func() {
		r.cfgAssign = defaultAssign()
		r.cfgEnabled = map[string]bool{}
	})
}

// LoadConfig reads the persisted bindings (best-effort; missing file = defaults).
func (r *Runner) LoadConfig(path string) {
	r.cfgMu.Lock()
	defer r.cfgMu.Unlock()
	r.cfgInit()
	r.cfgPath = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c roleConfig
	if json.Unmarshal(b, &c) != nil {
		return
	}
	if err := validate(c); err != nil {
		return // a hand-edited bad file falls back to defaults
	}
	apply(r, c)
}

// SetConfig validates, applies, and persists new bindings.
func (r *Runner) SetConfig(assign map[string]string, enabled map[string]bool) error {
	c := roleConfig{Assign: assign, Enabled: enabled}
	if err := validate(c); err != nil {
		return err
	}
	r.cfgMu.Lock()
	defer r.cfgMu.Unlock()
	r.cfgInit()
	apply(r, c)
	if r.cfgPath != "" {
		if b, err := json.MarshalIndent(roleConfig{Assign: r.cfgAssign, Enabled: r.cfgEnabled}, "", "  "); err == nil {
			_ = os.MkdirAll(filepath.Dir(r.cfgPath), 0o755)
			_ = os.WriteFile(r.cfgPath, b, 0o644)
		}
	}
	return nil
}

func apply(r *Runner, c roleConfig) {
	for t, role := range c.Assign {
		r.cfgAssign[t] = role
	}
	for k, v := range c.Enabled {
		r.cfgEnabled[k] = v
	}
}

func validate(c roleConfig) error {
	for t, role := range c.Assign {
		ok := false
		for _, p := range RebindableTools {
			if p == t {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("工具 %q 不可改绑(固定绑定)", t)
		}
		ok = false
		for _, tr := range RebindTargets {
			if tr == role {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("工具 %q 只能指派给执行角色(researcher/engineer/designer),收到 %q", t, role)
		}
	}
	for k, on := range c.Enabled {
		if !on && !togglableRoles[k] {
			return fmt.Errorf("角色 %q 不可停用(每单必需)", k)
		}
		if _, exists := RoleByKey(k); !exists {
			return fmt.Errorf("未知角色 %q", k)
		}
	}
	return nil
}

// EffectiveTools returns a role's live tool list: its fixed tools plus any
// rebindable tools currently assigned to it.
func (r *Runner) EffectiveTools(key string) []string {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	role, ok := RoleByKey(key)
	if !ok {
		return nil
	}
	var out []string
	for _, t := range role.ToolNames {
		if _, rebindable := r.cfgAssignSafe()[t]; !rebindable {
			out = append(out, t)
		}
	}
	for t, owner := range r.cfgAssignSafe() {
		if owner == key {
			out = append(out, t)
		}
	}
	return out
}

func (r *Runner) cfgAssignSafe() map[string]string {
	if r.cfgAssign == nil {
		return defaultAssign()
	}
	return r.cfgAssign
}

// roleSwitchedOff reports an explicit user disable.
func (r *Runner) roleSwitchedOff(key string) bool {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	if r.cfgEnabled == nil {
		return false
	}
	on, set := r.cfgEnabled[key]
	return set && !on
}

// toolWired reports whether a tool's backing capability is configured.
func (r *Runner) toolWired(name string) bool {
	switch name {
	case "web_search":
		return strings.TrimSpace(r.TavilyKey) != ""
	case "code_execute":
		return r.SandboxClient != nil
	case "generate_image", "generate_poster", "generate_storybook":
		return r.ImagesEnabled && r.Images != nil
	case "generate_deck":
		return r.Pipeline != nil
	case "generate_video":
		return r.Video != nil
	case "edit_image":
		return r.Editor != nil
	case "find_kol":
		return r.KOL != nil
	default: // plan_tasks / update_task / write_document — always available
		return true
	}
}

// ConfigView is the GET /claw/roles payload: effective per-role state plus
// the assignment map the editor mutates.
type RoleView struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Tier    string   `json:"tier"`
	Tools   []string `json:"tools"`
	Enabled bool     `json:"enabled"`
	Locked  bool     `json:"locked"` // cannot be disabled
	Wired   bool     `json:"wired"`  // at least one tool's capability is configured
}

type ConfigView struct {
	Roles  []RoleView        `json:"roles"`
	Assign map[string]string `json:"assign"`
	Pool   []string          `json:"pool"`
	Wired  map[string]bool   `json:"tool_wired"`
}

func (r *Runner) ConfigSnapshot() ConfigView {
	r.cfgMu.RLock()
	assign := map[string]string{}
	for k, v := range r.cfgAssignSafe() {
		assign[k] = v
	}
	r.cfgMu.RUnlock()

	wired := map[string]bool{}
	for _, t := range RebindableTools {
		wired[t] = r.toolWired(t)
	}
	wired["generate_deck"] = r.toolWired("generate_deck")

	var roles []RoleView
	for _, role := range allRoles {
		tools := r.EffectiveTools(role.Key)
		anyWired := len(tools) == 0
		for _, t := range tools {
			if r.toolWired(t) {
				anyWired = true
			}
		}
		roles = append(roles, RoleView{
			Key:     role.Key,
			Name:    role.DisplayName,
			Tier:    role.ModelTier,
			Tools:   tools,
			Enabled: !r.roleSwitchedOff(role.Key),
			Locked:  !togglableRoles[role.Key],
			Wired:   anyWired,
		})
	}
	return ConfigView{Roles: roles, Assign: assign, Pool: append([]string(nil), RebindableTools...), Wired: wired}
}

// ── Runner fields (declared here to keep runner.go focused) ─────────────

type runnerConfigState struct {
	cfgMu      sync.RWMutex
	cfgOnce    sync.Once
	cfgPath    string
	cfgAssign  map[string]string
	cfgEnabled map[string]bool
}
