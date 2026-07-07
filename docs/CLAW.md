# Claw — 多 Agent AI 员工团队

> Claw 是 Dream-Waver 里的「通用 AI 员工」纵切（对标 Genspark Super Agent / Manus）：
> 用户派一个活，一支由 8 个角色组成的 AI 团队并发完成它，交付**工作产物**
> （报告 + 配图 + 可选 PPT / 短视频），全过程在一间像素风办公室里可视化上演。
>
> 本文档描述 main 分支上的当前实现（v9.2）。最初的设计方案见
> [claw-v1-design.md](claw-v1-design.md)，现实现已远超 v1 范围（见 §9）。

---

## 1. 一句话架构

```
用户 prompt
   │  POST /api/v1/claw
   ▼
┌──────────────────────── Go orchestrator (internal/skill/claw) ────────────────────────┐
│ Phase 0 澄清门 → Phase 1 规划 → Phase 1.5 开工辩论 → Phase 2 并发执行(≤3 sub-agent)     │
│ → Phase 3 撰稿 → Phase 3.5 评审改稿 → Phase 4 制片(PPT) → Phase 5 视频师(i2v)          │
└──────────────┬────────────────────────────────────────────────────────────────────────┘
               │ claw.* 事件 (WebSocket)                     │ 工作产物 (HTTP GET)
               ▼                                             ▼
┌────────────── Next.js 前端 (components/claw) ──────────────────────────────────────────┐
│ ClawOffice 像素办公室（officeSim tick 引擎：走位/会议/串门/手势）                        │
│ ClawChat 过程叙事 + 追问   ·   ArtifactPanel 报告/配图/PPT/视频   ·   BindingsPanel 改绑 │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

两个核心设计立场：

1. **交付产物而非回答** — 每次运行的终点是一个带版本号的 work package
   （Markdown 报告 + 生成的图片 + 可选 .pptx / .mp4），聊天流只是过程记录。
2. **过程即界面** — 多 agent 的执行状态不用日志或进度条表达，而是映射成一间
   办公室里 8 个像素小人的空间行为（走到工位=开工、开会=规划/评审、
   下班派对=完成），事件协议就是这套「舞台指令」。

## 2. 用户旅程

```
/claw/new  输入任务（如「调研 2026 国产大模型价格战，出对比报告，含价格表和配图」）
   │
   ├─ 目标含糊 → 调度员在聊天里提 1–2 个澄清问题，回答后继续（awaiting_input）
   │
   ▼
/claw/{id}  全屏像素办公室
   ·  全员走进会议室开例会 → 任务计划卡逐条亮起（claw.plan）
   ·  各角色发表分工提案，达成一致方案（claw.debate，⚖ 例会共识卡）
   ·  调研员/工程师/设计师并发干活：搜索、跑代码、生成配图（tool.* 事件）
   ·  撰稿员汇总成报告 v1 → 评审员按 rubric 复审改出 v2（claw.artifact.updated）
   ·  （用户要 PPT/视频时）制片出 .pptx、视频师把配图动画成短片
   ·  完成 → 全员下班派对；产物窗口自动弹出，可下载
   │
   └─ 追问（「把价格表改成人民币」）→ 同一会话续跑，报告出 v3
```

## 3. 后端流水线（coordinator.go）

| Phase | 函数 | 门控 |
|---|---|---|
| 0 澄清 | `triageClarify` | 仅新任务；LLM 判定目标含糊 → 发 `claw.clarify`、状态 `awaiting_input`、暂停等用户回复（`Runner.Resume` 续跑）；判定失败则放行（advisory） |
| 1 规划 | `planWithRoles` → `callPlanner` | planner-tier LLM 产出角色标注的子任务清单；失败降级为最小计划 |
| 1.5 辩论 | `runDebate` → `proposeAngle` → `reconcileDebate` | 每个被指派的执行角色并发提方案（worker tier），调度员归并成一致方案，注入撰稿/评审 prompt；追问轮和 <2 个声音时跳过；尽力而为，失败放行 |
| 2 执行 | `runExecutionPhase` | 被指派角色作为**并发 sub-agent** 跑（goroutine + 信号量，并发上限 3）；角色停用或工具未接线 → 该行任务标记 skipped |
| 3 撰稿 | `runWriter` | 汇总所有执行产出写成报告（`write_document`）；失败则跳过 3.5–5，收尾守卫报错 |
| 3.5 评审 | `runCritic` | 评审员按 rubric 复审并重写一版（报告 → v2）；软失败保留 v1 |
| 4 制片 | `runProducer` | 仅当计划里有制片（用户要了 PPT）且角色开启；走 slides Pipeline 出真 .pptx |
| 5 视频师 | `runVideographer` | 仅当计划里有、角色开启、且已有 ≥1 张配图；Seedance i2v 把配图动画成短片 |
| 收尾守卫 | — | artifact 版本必须 >0，否则报错；残留 pending/doing 任务补记 done |

超时预算按角色分级：制片 8 分钟、设计师 6 分钟、其余 4 分钟。

每个 sub-agent 都是一个以角色 key 命名的 `ToolCallAgent`（复用
`internal/agent` 的 Base → ReAct → ToolCall 三层抽象），事件自动带上
`agent` 字段——前端就靠这个字段点亮对应的小人。

## 4. 角色表（roles.go，8 个）

| 角色 | key | 模型 tier | 工具 | 可停用 | 工具可改绑 |
|---|---|---|---|---|---|
| 调度员 | coordinator | planner | plan_tasks, update_task | 否（骨干） | 否 |
| 调研员 | researcher | worker | web_search (Tavily) | 是 | 是 |
| 工程师 | engineer | worker | code_execute (Rust 沙箱) | 是 | 是 |
| 设计师 | designer | worker | generate_image, edit_image | 是 | 是 |
| 撰稿员 | writer | planner | write_document | 否（骨干） | 否 |
| 评审员 | critic | critic | write_document | 是 | 否 |
| 制片 | producer | planner | generate_deck | 是 | 否 |
| 视频师 | videographer | worker | generate_video (Seedance i2v) | 是 | 否 |

角色是**数据驱动**的：后端 `roles.go` + 前端 `workers.ts` 各持一份注册表，
按 key 对齐。加一个新角色 = 两边各加一条注册项 + 接线一个工具适配器，
坐标布局、事件归属、办公室走位全部自动跟上。

**工具接线（wired）**：每个工具按运行时能力探测决定是否可用——
`web_search` 要 `TAVILY_API_KEY`、`code_execute` 要 sandbox gRPC、
`generate_image`/`edit_image` 要 design bridge（NanoBanana / DreamAPI）、
`generate_deck` 要 slides Pipeline、`generate_video` 要 Seedance 适配器。
没接线的工具不注册，对应角色自动降级停用，规划器也不会派活给它。

## 5. 运行时改绑（config.go）

工具↔角色绑定不是写死的：

- **可改绑池**：web_search / code_execute / generate_image / edit_image
  可在调研员/工程师/设计师之间任意重新指派；
- **可停用角色**：除调度员和撰稿员外的 6 个角色都可开关；
- 生效路径贯穿全链：规划 prompt 的能力广告、sub-agent 的 buildTools、
  角色 enabled 判定都读同一份 `EffectiveTools`；
- 持久化为 JSON（`SLIDE_OUT_DIR/claw-roles.json`），
  API `GET/PUT /api/v1/claw/roles`，前端 dock 的「绑定」窗口（BindingsPanel）可视化编辑。

实测：把 generate_image 改绑给工程师后，规划器真的会把配图任务派给工程师。

## 6. 事件协议（5 个 claw.* + 复用 tool.*）

所有事件走既有 `GET /api/v1/sessions/{id}/events` WebSocket，零新增传输层。

| Kind | 载荷 | 前端用途 |
|---|---|---|
| `claw.plan` | task_titles[], task_roles[] | 任务计划卡（全 pending）；小人分工；召集例会 |
| `claw.task.update` | task_index (1-based), task_status | 勾选/高亮——清单状态**只**由该事件驱动，防止计划漂移 |
| `claw.clarify` | questions[] | 澄清问题气泡 + `awaiting_input` 暂停 |
| `claw.debate` | {proposals:[{role,text}], agreed} | 各角色提案气泡 + 一致方案卡；会议动画延长 |
| `claw.artifact.updated` | kind(report/deck/video), version, bytes | **只发通知不发正文**——前端收到后 GET `/claw/{id}/artifact` 拉取，避免大 payload 过 WS |

`tool.start` / `tool.end` 原样复用：带 `agent` 字段归属到角色，前端映射成
工具进度条、小人手势（搜索=举放大镜、写代码=敲键盘）、
成败反应（报错=捂脸、成功=eureka）。

## 7. 持久化与恢复

- 表 `claw_runs`（migration 0009–0011）：plan / artifact / artifact_version /
  figures / videos / deck / memory 全部 jsonb/text 落库，带 RLS；
- `POST /api/v1/claw` 先落行再开跑；跑完 `finishClawJob` 写终态；
- `SessionStore.GetOrLoad`：内存 miss 时从 DB 水合整个会话
  （含对话记忆），**服务重启后可继续追问迭代**；
- artifact 版本单调递增，每次 `write_document` 产生 v+1，支持按版本 GET；
- 匿名运行（无 workspace）跳过持久化，行为与其它纵切一致。

## 8. 前端：像素办公室（apps/web/components/claw）

`ClawOffice.tsx`（舞台）+ `officeSim.ts`（模拟引擎）把事件流翻译成空间行为：

- **tick 引擎**（~90ms/帧）：曼哈顿寻路（沿两条走廊 lane 折线走）、
  槽位登记表防重叠（工位/会议席/休息区/饮水机每个位置同刻只容一人）、
  朝向翻转、走路中随机驻足；
- **事件→行为**：`claw.plan` → 全员进会议室开例会（调度员坐主位）；
  critic 活跃 → 评审会；`tool.*` → 对应小人回工位干活 + 角色手势池循环；
  完成 → 下班派对；空闲 → 回自己工位，偶尔去接咖啡/看绿植/串门
  （并发外出上限 2，保持安静——这是用户反复调过的「办公室手感」）；
- **过程叙事**（`narrate.ts`）：claw.plan / tool.start / tool.end 转成
  各角色第一人称、带人设的发言气泡（去重），完成后按上下文给
  「加配图 / 做成 PPT / 做成短视频 / 高清化 / 抠图」等下一步 chips；
- **产物即窗口**：work package 以可拖拽/可最大化的像素 OS 窗口
  （react-rnd）浮在办公室上，报告 Markdown 渲染、配图画廊、
  PPT 滑动预览、视频原生播放，均可单件下载；
- 其它：衣柜换装（localStorage）、点击小人看统计 popover、
  Shift+点击锤人、办公室猫、昼夜切换。

关键实现约束：手势动画拥有 `.claw-rig` 的 transform，朝向翻转必须包在
外层 wrapper 上，两者才互不打架。

## 9. 代码地图与规模

```
services/orchestrator/
  internal/skill/claw/          # ~2,900 行 Go（含测试）
    coordinator.go   743        # 流水线核心（本文 §3）
    session.go       485        # 会话 + 持久化快照
    config.go        272        # 运行时改绑（§5）
    roles.go         159        # 角色注册表（§4）
    subagent.go      139        # sub-agent 工厂
    tools_*.go / write_document.go   # 5 个产物工具
    runner.go         79        # Run / Continue / Resume 入口
  internal/api/routes_claw.go   # REST：创建/查询/追问/artifact/roles
  internal/store/claw_runs.go   # Postgres + in-memory 双实现
  migrations/0009..0011         # claw_runs 表及演进

apps/web/
  components/claw/              # ~4,000 行 TS/React
    ClawOffice.tsx  1209        # 办公室舞台
    ClawChat.tsx     523        # 聊天/叙事/chips
    officeSim.ts     374        # tick 模拟引擎
    ArtifactPanel.tsx 369       # 产物面板
    workers.ts / useWorkerStates.ts / narrate.ts / BindingsPanel.tsx / …
  app/claw/{new,[id]}/          # 页面
```

**演进史**（19 个里程碑提交，`git log -- internal/skill/claw components/claw`）：
v1 单 agent 计划-执行-写报告 → v2 真·多 agent 并发团队 → v3 评审员 + 澄清门
→ v4–v5 办公室模拟/叙事/chips → v6 UI 库化 + 真·辩论 → v7 视频师
→ v8 cozy 办公室美术 → v9 设计师真出图 + edit_image + 修图 chips。
更多能力（海报/绘本工具、对话式澄清、KOL 调研工具 find_kol、Q 版角色重绘等）
在开发分支上，尚未并入 main。

## 10. 与 slides / games 纵切的关系

Claw 是「事件流 + 聊天时间线 + 工具进度」这条已验证管道上的第三个纵切：

- 复用：ToolCallAgent 循环、Tool Registry、LLM Router（planner/worker
  双 tier）、事件 Hub + WS、games 式持久化/水合形态；
- 反向依赖：制片的 `generate_deck` 直接调 slides Pipeline 出真 .pptx；
  设计师/视频师走 design bridge（与 DreamFace 功能共用适配器）；
- 差异：Claw 首创了并发 sub-agent 编排（`svg_parallel.go` 的形态推广）、
  角色数据注册表、运行时改绑、以及把事件流可视化为空间模拟的前端范式。

## 11. 本地跑起来

```bash
# 后端（需要 DEEPSEEK_API_KEY；可选 TAVILY_API_KEY / sandbox / design bridge）
cd services/orchestrator && go build -o /tmp/dw-orchestrator ./cmd/server
set -a; source ../../.env; set +a
SUPABASE_JWKS_URL="" PORT=8080 /tmp/dw-orchestrator

# 图像/视频能力需要 dreamapi-sidecar：
cd services/dreamapi-sidecar && python3 -m uvicorn main:app --port 8091

# 前端
cd apps/web && pnpm dev    # http://localhost:3000/claw/new
```

没配的能力会自动降级（对应角色显示「已停用」，规划器绕开它们）——
最小只需 DeepSeek key 即可体验 调度→规划→工程师→撰稿→评审 的完整闭环。
