package claw

// roles.go is Claw's extensibility seam — the data-driven worker registry
// (the backend half of the "Skill Pack" model, mirrored by the frontend
// WORKERS table). Each Role declares one pixel-worker / sub-agent: its tool
// subset, model tier, discipline prompt, and step budget. The coordinator
// (coordinator.go) dispatches sub-agents by reading this table, so ADDING A
// WORKER is a config entry here + (if it needs a new capability) one tool —
// the orchestration core doesn't change.
//
// Role keys double as the agent name each sub-agent runs under, so every
// tool.start/tool.end/llm.* event the sub-agent emits carries Agent=Key and
// the frontend lights up exactly that worker.

// Role keys — also the AgentName each sub-agent runs under and the worker id
// the frontend maps events to.
const (
	RoleCoordinator = "coordinator" // 调度员 — plans, assigns roles, tracks progress
	RoleResearcher  = "researcher"  // 调研员 — web_search
	RoleEngineer    = "engineer"    // 工程师 — code_execute
	RoleDesigner    = "designer"    // 设计师 — generate_image
	RoleWriter      = "writer"      // 撰稿员 — write_document (assembles the report)
	RoleCritic      = "critic"      // 评审员 — reviews + revises the report once (write_document)
	RoleProducer    = "producer"    // 制片 — generate_deck
	RoleVideographer = "videographer" // 视频师 — generate_video (animates a figure via i2v)
)

// Role is one worker definition. ToolNames are resolved to concrete tool
// instances by the sub-agent factory (subagent.go) which injects the shared
// deps (Session / Emitter / Images / Router). EnabledTools gates a role on
// whether its capability is actually wired (e.g. designer needs an image
// provider); a role with no enabled tools is rendered idle/grey, never
// errors — same posture as v1's Tavily/sandbox gating.
type Role struct {
	Key          string
	DisplayName  string
	ModelTier    string // "planner" | "worker" — llm.Router.ModelFor tier
	ToolNames    []string
	SystemPrompt string
	MaxSteps     int
}

// allRoles is the ordered registry. Order = display order on the WorkerDesk.
var allRoles = []Role{
	{
		Key:         RoleCoordinator,
		DisplayName: "调度员",
		ModelTier:   "planner",
		ToolNames:   []string{"plan_tasks", "update_task"},
		MaxSteps:    8,
		SystemPrompt: "你是调度员(工头)。把用户的目标拆成 3–7 个有序子任务,每个子任务指派给最合适的角色" +
			"(researcher 联网调研 / engineer 跑代码算数 / designer 配图 / writer 撰写报告)。" +
			"只负责规划与分派,不亲自执行子任务。",
	},
	{
		Key:         RoleResearcher,
		DisplayName: "调研员",
		ModelTier:   "worker",
		ToolNames:   []string{"web_search"},
		MaxSteps:    10,
		SystemPrompt: "你是调研员。用 web_search 检索事实、最新数据、来源链接来完成分配给你的子任务,可多次检索。" +
			"完成后用一段简洁的中文小结你的发现(含关键数字与来源链接),然后 terminate。不要写最终报告——那是撰稿员的活。",
	},
	{
		Key:         RoleEngineer,
		DisplayName: "工程师",
		ModelTier:   "worker",
		ToolNames:   []string{"code_execute"},
		MaxSteps:    10,
		// Bug A: the model kept calling code_execute with an empty language
		// → "unsupported language". Hard-require it here.
		SystemPrompt: "你是工程师。用 code_execute 做计算、解析数据、核对数字来完成分配给你的子任务。" +
			"调用 code_execute 时**必须**传 language 参数,统一用 \"python\"(支持 python / node / bash)。" +
			"完成后用一段中文小结结果(含算出的关键数字),然后 terminate。不要手算。",
	},
	{
		Key:         RoleDesigner,
		DisplayName: "设计师",
		ModelTier:   "worker",
		ToolNames:   []string{"generate_image", "edit_image", "generate_poster", "generate_storybook"},
		MaxSteps:    8,
		SystemPrompt: "你是设计师,也是团队的美术 / 视觉产出担当。按任务选对工具:\n" +
			"- 报告配图:generate_image(清晰英文画面描述 + 中文图注)。\n" +
			"- 修图:edit_image(remove_bg 抠图 / enhance 高清化 / colorize 上色 / outpaint 扩图 / img2img 图生图)。\n" +
			"- 海报 / 宣传图 / 社媒主图:generate_poster(给大标题、主题、风格、画幅)。\n" +
			"- 绘本 / 多格插画 / 漫画分镜:generate_storybook(给 2–6 个分镜场景 + 统一风格)。\n" +
			"用户要的不一定是报告 —— 看清需求,选最贴切的产出形式。完成后一句话说明你做了什么,然后 terminate。",
	},
	{
		Key:         RoleWriter,
		DisplayName: "撰稿员",
		ModelTier:   "planner",
		ToolNames:   []string{"write_document"},
		MaxSteps:    4,
		// Bug B: the model re-called write_document repeatedly, each time
		// regenerating a huge report → one stream tripped the run deadline.
		// Single-shot discipline: write once, terminate.
		SystemPrompt: "你是撰稿员。基于调研员/工程师/设计师交来的发现,**一次性**调用 write_document 写出完整的 Markdown 报告" +
			"(标题、小标题、必要的表格、结论,并在结尾「## 引用来源」列出来源链接;如有配图用 Markdown 图片语法插入)。" +
			"**只调用一次 write_document**,写完立即 terminate——不要反复重写。默认用中文。",
	},
	{
		Key:         RoleCritic,
		DisplayName: "评审员",
		ModelTier:   "critic",
		ToolNames:   []string{"write_document"},
		MaxSteps:    4,
		// Single-purpose quality gate: review against a rubric, then output
		// the improved report in ONE write_document call, then terminate.
		// Allowed to make only minimal touch-ups if the draft is already solid.
		SystemPrompt: "你是质量评审员。审阅撰稿员交来的报告草稿,逐条对照标准挑刺:" +
			"①完整性(是否覆盖目标的每个要点、有无残缺/占位)②结构(标题层级、逻辑顺序、过渡)" +
			"③有据(关键结论是否有数据/事实支撑,有无凭空编造的数字)④引用(是否有「## 引用来源」且可点击)" +
			"⑤无矛盾(前后数字/结论不打架)⑥深度与受众匹配(详略、术语是否贴合既定受众与篇幅)。" +
			"然后**一次性**调用 write_document 输出**改进后的完整报告**(修掉发现的问题、补齐缺口、保留并改进原有配图引用)。" +
			"若草稿已经很好,就只做最小润色。**只调用一次 write_document**,写完立即 terminate。默认用中文。",
	},
	{
		Key:         RoleProducer,
		DisplayName: "制片",
		ModelTier:   "planner",
		ToolNames:   []string{"generate_deck"},
		MaxSteps:    4,
		SystemPrompt: "你是制片。把已完成的报告用 generate_deck 转成一份幻灯片 deck(传报告主题与要点)。" +
			"完成后 terminate。",
	},
	{
		Key:         RoleVideographer,
		DisplayName: "视频师",
		ModelTier:   "worker",
		ToolNames:   []string{"generate_video"},
		MaxSteps:    4,
		// Post-production: animates one already-generated figure into a short
		// clip via image-to-video. Single-shot to bound the slow/costly render.
		SystemPrompt: "你是视频师。把作品包里某张已生成的配图用 generate_video 做成一段几秒的短视频" +
			"(传一句英文的运镜/动态描述,如镜头推拉、光影流动、人物微动;可指定 figure_index、resolution(480p/720p/1080p)、duration(4–12 秒))。" +
			"**只调用一次 generate_video**,完成后用一句中文说明你做了什么,然后 terminate。",
	},
}

// Roles returns the full ordered registry (read-only copy of intent; the
// slice is package-private and never mutated at runtime).
func Roles() []Role { return allRoles }

// RoleByKey returns the role for a key, or (Role{}, false).
func RoleByKey(key string) (Role, bool) {
	for _, r := range allRoles {
		if r.Key == key {
			return r, true
		}
	}
	return Role{}, false
}

// executionRoles are the roles dispatched as CONCURRENT sub-agents in the
// coordinator's execution phase (Phase 2). coordinator (planning) and
// writer/producer (assembly, depend on others' output) run in their own
// phases, not here.
func executionRoles() []string {
	return []string{RoleResearcher, RoleEngineer, RoleDesigner}
}
