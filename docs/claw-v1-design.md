# Claw v1 设计方案 — 通用 AI 员工(对标 Genspark Super Agent)

> 状态:**待确认**(代码未动)。确认后按 M1→M4 实施。
> 范围来自产品决策:v1 四个能力 = 联网研究 / 文档报告产物 / 代码执行 / 任务计划可视化。

---

## 1. 对标拆解:Genspark Super Agent 的可借鉴点

公开资料里 Genspark Super Agent 的核心机制:

1. **Plan-first**:接到 prompt 先"想要什么结果",拆成子任务清单(读日历 → 总结参会人 → 拉每个人的资料),计划本身是 UI 一等公民;
2. **编排而非单脑**:系统把每个子任务派给最合适的组件(检索 / 抽取 / 推理 / 生成),Mixture-of-Agents(9 模型 80+ 工具),并行执行后再组装;
3. **交付"工作产物"而非回答**:产出是文档 / 幻灯片 / 表格等 artifact,聊天流只是过程记录;
4. **过程透明**:每一步工具调用可见、可展开,执行中可插话。

对应到 Claw v1 的设计取舍:

| Genspark 机制 | Claw v1 落法 | 不做(v2+) |
|---|---|---|
| Plan-first | `plan_tasks` 工具 + `claw.plan` 事件 + 前端任务清单卡(边执行边勾选) | 计划审批门(HILT) |
| 多模型/多工具编排 | 单 agent 循环 + 4 个工具,planner/worker 双 tier 路由 | 并行子 agent、80+ 工具 |
| 工作产物 | Markdown 报告 artifact,右栏实时预览 + 下载 | 幻灯片/表格 artifact(已有 slides 纵切,后续打通) |
| 过程透明 | 复用现有 tool.start/end 事件 + ToolStrip + 进度条 | 浏览器实况画面(computer-use) |
| 电话/语音 | 不做 | — |

> 我们已有的天然优势:slides/games 已经把「事件流 + 聊天时间线 + 工具进度」整条管道修好了,Claw 是这条管道上的第三个纵切,**复用率非常高**。

---

## 2. 用户旅程(v1)

```
/claw/new
  ┌─────────────────────────────────────────┐
  │ ~/claw/new — 给 Claw 派个活              │
  │ ┌─────────────────────────────────────┐ │
  │ │ 例:调研 2026 国产大模型价格战,出一份 │ │
  │ │ 对比报告,含价格表和趋势判断           │ │
  │ └─────────────────────────────────────┘ │
  │  [示例任务 chips]            [开工 ↵]    │
  └─────────────────────────────────────────┘
            │ POST /api/v1/claw
            ▼
/claw/{id}  双栏工作台
  ┌──────────────────┬──────────────────────┐
  │ 左:过程时间线     │ 右:Artifact 面板      │
  │  任务计划卡 ▣▣▢▢  │  (Markdown 实时渲染)  │
  │  web_search ▓▓ 4s │   # 国产大模型价格战   │
  │  思考流式…        │   ## 价格对比 …       │
  │  write_document ✓ │                      │
  │  [追问输入框]      │   [下载 .md] [复制]   │
  └──────────────────┴──────────────────────┘
```

- 任务进行中可在左栏继续打字(复用 slides 的"排队后自动发出"模式);
- 完成后 artifact 固定在右栏,可下载 / 复制,可继续追问迭代(每次 `write_document` 产生新版本)。

---

## 3. 后端设计(services/orchestrator)

### 3.1 新增 skill 包 `internal/skill/claw`

```
internal/skill/claw/
  runner.go        // Runner: Router/Emitter/Sessions/TavilyKey/SandboxClient
  session.go       // SessionState + SessionStore(in-memory + GameJobs 式持久化)
  prompt.go        // 系统提示词(中文产出、引用来源、计划纪律)
  tools/
    plan_tasks.go      // 拆解子任务 → emit claw.plan
    update_task.go     // 勾选/改状态 → emit claw.task.update
    write_document.go  // 写/改 Markdown artifact → emit claw.artifact.updated
```

**复用(不新写)**:
- 循环:`internal/agent/toolcall.go` 的 `ToolCallAgent`(Think/Act,slides + design 已在用);
- 工具:`internal/tool/web_search.go`(Tavily,名字 `web_search`)、`internal/tool/code_execute.go`(sandbox gRPC,未配置时不注册即自动降级);
- 事件:`event.NewStepStart/NewLLMThought/NewLLMToken/NewToolStart/NewToolEnd/NewAgentFinish/NewError`。

### 3.2 Agent 循环纪律(写进系统提示词 + 工具描述)

1. 第一步**必须**调 `plan_tasks`(3–7 个子任务);
2. 每完成一个子任务调 `update_task(index, "done")` —— 防止"计划漂移"(前端清单的勾选完全由该工具驱动,不靠猜);
3. 研究类子任务用 `web_search`(可多次),计算/验证类用 `code_execute`;
4. 收尾**必须**调 `write_document`(完整 Markdown,含引用链接),然后 `terminate`;
5. 追问(Continue)走同一循环:可 `update_task` 增改任务,`write_document` 覆盖出新版本。

### 3.3 新增事件(沿用现有 EventData 字段风格)

| Kind | 载荷 | 前端用途 |
|---|---|---|
| `claw.plan` | `task_titles []string`(复用 compose 的 titles 思路) | 任务计划卡(初始全 pending) |
| `claw.task.update` | `task_index int, task_status "doing"/"done"/"skipped"` | 勾选/高亮当前子任务 |
| `claw.artifact.updated` | `artifact_version int, artifact_bytes int` | 右栏拉取新版本(**WS 不传全文**,避免大 payload;前端收到后 GET artifact) |

### 3.4 API(chi 路由,复用 games 纵切的形态)

```
POST /api/v1/claw                 {prompt} → 202 {job_id, session_id, events_url}
GET  /api/v1/claw/{id}            → {status, title, plan, artifact_version, started_at, …}
POST /api/v1/claw/{id}/messages   {content} → 202(追问/继续,复用排队模式)
GET  /api/v1/claw/{id}/artifact   → text/markdown(?version=N 可选)
```

事件流复用现有 `GET /api/v1/sessions/{id}/events`(WS)+ `/log`(回放)——**零新增传输层**。

### 3.5 持久化(吸取 games 重启丢失的教训,v1 就带)

- 迁移 `migrations/0009_claw_runs.sql`:`claw_runs(id, workspace_id, session_id, status, prompt, title, plan jsonb, artifact text, artifact_version int, memory jsonb, error, started_at, finished_at)` + RLS(抄 game_jobs);
- `store.ClawRuns` 接口(Put/Get/List/UpdateStatus/SaveCheckpoint)+ pgx & in-memory 双实现(抄 `game_jobs.go`);
- 路由层终态 `persistTerminalClawRun` + `GetOrLoad` 水合(抄我们刚修好的 games 模式)。匿名(wsID==Nil)跳过,行为同现有纵切。

### 3.6 模型路由

- `plan_tasks` / `write_document` → planner tier(v4-pro,质量敏感);
- 循环主体 / 检索汇总 → worker tier(v4-flash,量大);
- 与 slides 相同的 `llm.Router.For("planner"/"worker")`,无新配置。

---

## 4. 前端设计(apps/web)

### 4.1 路由与页面

```
app/claw/new/page.tsx    // 任务输入(像素 WindowCard + 示例 chips,抄 slides/new 骨架)
app/claw/[id]/page.tsx   // 双栏工作台(抄 slides/[id] 骨架:左聊天 右面板)
components/claw/
  TaskPlanCard.tsx       // 任务计划卡(ComposeStrip 的形态:清单 + 勾选 + 当前高亮)
  ArtifactPanel.tsx      // Markdown 渲染 + 版本徽标 + 下载/复制(对位 LivePreviewStack)
```

### 4.2 复用与扩展

- `AgentSessionProvider` / `transport.tsx`:原样复用(同一 WS);
- `session.ts`:Turn 增加 `clawPlan?: { titles: string[]; status: ("pending"|"doing"|"done"|"skipped")[] }` 与 `artifactVersion?: number`,reducer 加 3 个 case(`claw.plan` / `claw.task.update` / `claw.artifact.updated`);
- 工具行 + 进度条:`ToolStrip` + `ToolProgress` 原样生效(web_search/code_execute 会自动出现实时秒数 + 扫光条);`TOOL_ICON/TOOL_LABEL_ZH` 补 `plan_tasks 规划任务 / update_task 勾选进度 / write_document 撰写报告`;
- `api.ts`:`createClawRun / getClawRun / postClawMessage / clawArtifactURL` + `ClawRun` 类型;
- 首页入口:`AgentGrid` 的 Claw tile 与 `Sidebar` 的 Claw 项从 `comingSoon` → `/claw/new`。

### 4.3 Markdown 渲染(新依赖,需确认)

仓库目前**没有** markdown 渲染器。建议加 `react-markdown` + `remark-gfm`(标准、小、无 dangerouslySetInnerHTML)。备选:v1 先用 `<pre>` 等宽展示 + 下载(零依赖,丑一点)。**默认按 react-markdown 实施,若你不想加依赖请说。**

### 4.4 像素视觉

- 任务计划卡:WindowCard 标题栏 `✦ CLAW PLAN`,子任务行 = 方形像素 checkbox(pending=空框 / doing=紫色脉冲 / done=绿勾),连接线复用 `pixel-wire`;
- Artifact 面板:WindowCard 标题 `report.md · v3`,正文 paper 底色,h1/h2 用 mono 粗体,引用链接 accent 色;
- 状态 chip:复用 `StatusChip`(working/queued/done/error)。

---

## 5. 里程碑(每个独立可验收)

| 里程碑 | 内容 | 验收 |
|---|---|---|
| **M1 后端纵切** | claw 包 + plan_tasks/update_task/write_document + 路由 + 事件 + 持久化 | curl 发任务 → WS 看到 plan/task/artifact 事件 → GET artifact 拿到报告;重启后 GET 仍可用 |
| **M2 前端工作台** | /claw/new + /claw/[id] + TaskPlanCard + ArtifactPanel + session.ts 扩展 + 首页入口 | 浏览器全流程:派活 → 看计划勾选/工具进度 → 右栏出报告 → 下载 |
| **M3 代码执行 + 追问** | 注册 code_execute(探测降级)+ Continue 多轮 + artifact 版本迭代 | 追问"把价格表改成人民币"→ v2 报告 |
| **M4 打磨** | /claw 历史列表(List)、错误恢复、空态/示例任务、导出 .md | — |

预估:M1+M2 是主体(后端 ~600 行 + 前端 ~700 行,大头都是抄现有纵切的形态);M3 增量小;M4 看需求。

---

## 6. 风险与对策

| 风险 | 对策 |
|---|---|
| LLM 计划漂移(干的和计划对不上) | 勾选只由 `update_task` 工具驱动;系统提示词强约束;终检:`terminate` 前未全 done 则提示 agent 补勾或改计划 |
| artifact 大文本过 WS | WS 只发 `artifact.updated` 通知,正文走 GET 拉取 |
| DeepSeek 工具调用稳定性 | 复用 slides 验证过的 ToolCallAgent(带重试);plan_tasks 失败→advisory 降级为无计划直跑(同 slides 向导的容错哲学) |
| sandbox 未启动 | 注册前探测,缺失则不注册该工具(agent 自然不会调) |
| 与 slides/games 路由风格漂移 | 路由/持久化/水合全部按 games(含我们刚修的重启恢复)的最终形态抄 |

---

## 7. 待你确认的点

1. **总体方案** OK 吗?(分层 / 事件 / API / 里程碑)
2. **react-markdown 依赖**:加(推荐)还是 v1 先 `<pre>` 凑合?
3. **M1 先行**:确认后我从 M1(后端纵切)开始,M1 验收过了再动前端。
