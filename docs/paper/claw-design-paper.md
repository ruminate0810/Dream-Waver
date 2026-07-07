# Claw: The Office Is the Interface — Semantically Grounded Workplace Simulation as a Transparency Layer for Multi-Agent LLM Systems

**Claw：办公室即界面——语义绑定的职场模拟作为多 Agent LLM 系统的透明层**

> 状态：设计文档（design paper, full draft）。系统已实现并可运行（Dream-Waver
> main 分支，v9.2；工程细节另见 [CLAW.md](../CLAW.md)）。第 7 节的用户实验为
> 拟议方案，尚未执行。全部编号引用经存在性核实，核实记录与 URL 见
> [claw-related-work-scan.md](claw-related-work-scan.md)。
> 本稿为中文工作稿；投稿版需转写为英文并按 venue 模板重排。

---

## Abstract

Multi-agent LLM systems increasingly deliver real work (research reports,
figures, slide decks, video clips) through pipelines of role-specialized
sub-agents. Yet their process interfaces remain developer-oriented: log
streams, chat transcripts, and node-link traces that end users neither read
nor act on. Users are left with two options: a progress bar that hides
everything, or a transcript that shows everything and is understood by no one.
We present **Claw**, a multi-agent "AI employee team" whose execution is
rendered as a live pixel-art office in which sprite workers spatially enact
the pipeline state: the team convenes a kickoff meeting when planning,
role-specific workers walk to their desks during concurrent execution, tool
failures surface as visible reactions, a review meeting stages the critic's
revision pass, and the office celebrates on delivery. Unlike the recently
emerged gray-literature genre of "pixel office" agent viewers that mirror
low-level tool activity onto sprites, Claw's scene grammar is semantically
bound to the phases of a role-structured pipeline (clarify, plan, debate,
concurrent execution, write, critic review, produce), and every spatial event
is driven by a typed orchestration-event protocol rather than inferred
activity. We contribute (1) a design framework: semantic binding of scene
grammar to pipeline semantics, four design goals, and a legibility spectrum
from abstract activity proxies to figurative workplace simulation, with design
lessons from nine major iterations; (2) the Claw system, an end-to-end
implementation including an adaptive clarification gate, a kickoff debate
stage, a critic-revise quality gate, and runtime tool-to-role rebinding that
the planner re-plans around; and (3) a proposed controlled study evaluating
the metaphor's effect on situation awareness, trust calibration, and
intervention behavior against log- and checklist-based baselines, including an
adversarial hypothesis derived from the anthropomorphism-miscalibration
literature.

**摘要**：多 agent LLM 系统日益以角色化子 agent 流水线交付真实工作产物（调研
报告、配图、演示文稿、短视频），但其过程界面仍停留在面向开发者的日志流、聊天
记录与节点图。留给 end user 的选择只有两种：隐藏一切的进度条，或展示一切却
无人能读的转录。本文提出 Claw，一个「AI 员工团队」系统，其执行过程被渲染为
一间实时像素办公室：规划时全员开例会，并发执行时各角色走到工位，工具失败表现
为可见的反应，评审改稿时召开评审会，交付时全员庆祝。与 2026 年初涌现的「像素
办公室」灰色文献流派（将低层工具活动镜像到 sprite 上）不同，Claw 的场景语法与
角色化流水线的阶段语义绑定，全部空间行为由类型化编排事件协议驱动。贡献：
(1) 设计框架，包括场景语法的语义绑定原则、四项设计目标、从抽象活动代理到具象
职场模拟的易读性光谱，以及九次大版本迭代沉淀的设计教训；(2) Claw 系统的端到端
实现，含自适应澄清门、开工辩论、评审改稿质量门，与规划器真实重规划的运行时
工具-角色改绑；(3) 一项拟议对照实验，对比日志与清单基线，测量态势感知、信任
校准与干预行为，并纳入一条源自拟人化误校准文献的对抗性假设。

---

## 1. Introduction

### 1.1 问题：工作发生在别处

把任务委托给一个多 agent 系统的用户，面对一种新的不透明。传统软件的执行对
用户是同步且可见的：点击，等待，结果。单 agent 对话式 AI 的执行至少是线性的：
一条思考流、若干次工具调用、一个回答。而一支多 agent 团队（规划者拆解任务，
多个执行者并发调用检索、沙箱、图像生成工具，撰稿者汇总，评审者复审改写）会在
几分钟内产生数十次工具调用、数万 token 的中间消息，以及多次内部移交与返工。
这个过程既不是线性的，也不是一条"思考流"能概括的。

现有界面用两种方式回应。一种是折叠：一根进度条或一个旋转指示器，用户失去全部
中间信号，只能在结束时接受或重来。另一种是展开：完整的消息日志或 trace 树
（LangSmith 类工具的形态），细节完备，但那是给系统开发者做故障分析用的。
Naik et al. [26] 访谈了 13 名多 agent LLM 系统的构建者，把这个两难称为透明度
的 "catch-22"：细节与可理解性难以兼得。值得注意的是，他们的访谈对象还是专业
开发者；对委托任务的普通用户，展开式界面等于没有界面。

不可见有真实的行为后果。He et al. [14] 在 CHI 2025 的 248 人实验中发现，用户
在 plan-then-execute 范式下会过度信任"看起来合理"的 agent 计划：因为执行过程
无从观察，计划的表面质量成了信任的代理指标。信任研究的经典结论 [12] 指出，
当系统复杂到无法完全理解时，引导依赖行为的是信任，而信任的校准（该依赖时
依赖、该干预时干预）需要关于系统能力与当前状态的持续证据。过程不可见时，
这种证据不存在。用 Endsley [10] 态势感知（situation awareness, SA）的框架说，
用户在三个层次上都缺乏输入：L1 感知（现在谁在干什么）、L2 理解（为什么、
意味着什么）、L3 预测（接下来会发生什么）。

### 1.2 设计回答：把过程做成一个场所

Claw 的设计回答是让用户观察一个正在运转的场所，而不是阅读过程记录。

Claw 将一支真实的 8 角色 LLM 团队（调度员、调研员、工程师、设计师、撰稿员、
评审员、制片、视频师）渲染为一间实时像素办公室。接到含糊任务时，调度员先在
对话里提一两个澄清问题（澄清门）。计划确定时，全员走进会议室开例会，计划卡
逐条亮起，各执行角色就分工发表提案并归并为一致方案（开工辩论）。执行阶段，
被指派的角色回到各自工位并发干活：调研员举起放大镜（检索进行中），工程师
敲键盘（沙箱代码执行），设计师作画（图像生成）；工具成功触发 eureka 手势，
失败触发捂脸。撰稿完成后，评审员召集评审会复审改稿，报告版本从 v1 翻到 v2。
交付时全办公室庆祝，产物窗口自动弹出。用户全程可以追问、回答澄清、或打开
绑定面板重构团队，下一轮规划会围绕新团队构成展开。

空间与社会隐喻在这里承担实际的信息功能。"谁在哪、和谁在一起、什么姿态"是
人类视觉系统以极低认知成本持续解析的前注意（pre-attentive）信号。CSCW 有
三十年的传统利用这一点维持对人类同事的外围感知：media spaces [29] 用常开的
音视频通道再造"共享走廊"，social translucence [11] 主张让数字系统中的活动
可见（visibility），以支撑感知（awareness）与问责（accountability）。Claw
把这条线索里的"同事"替换为 AI worker，把视频流替换为编排器的事件流。

### 1.3 必须诚实面对的事实：制品不新，研究是空的

把 AI agent 画进像素办公室，这个界面直觉在 2026 年已经不属于任何人。2026 年
2 月起，一批公开工具形成了可辨认的流派：Pixel Agents [G1]（VS Code 扩展，
把真实 Claude Code/Cursor/Codex 会话镜像成像素办公室小人，2 月 24 日上线，
8.4k GitHub stars，获 Fast Company 报道）、AgentFleet [G2]（自定义角色、圆桌
会议、完成庆祝）、Bit Office [G3]（planner-coder-reviewer 流水线加实时像素
办公室），以及至少五个同类项目 [G4–G6]。2026 年下半年的投稿无法把这个流派
当作 concurrent work 处理，本文也不这样做。

但这个流派确立的是需求信号与工程可行性，不是研究。三件事在已核实文献中仍然
不存在。(a) 对该隐喻作为透明机制的概念化：现有工具的映射是活动镜像（终端在
打字，小人就打字），映射方式任意、无理论依据、不携带流程语义。(b) 任何形式
的实证评估：没有一个工具回答过它是否真的让用户更理解、信任更校准、干预更
及时。(c) 与角色化流水线语义的绑定：镜像单个编码 agent 的会话，与表达一条
组织化流水线（规划、辩论、执行、评审）的状态，是两个不同的设计问题。与此
同时，拟人化文献给出一条必须正面回应的反面预测：表面拟人化线索并不能改善
信任校准 [24]，甚至会增加"信任韧性"，即系统出错后该撤回信任时不撤回 [38]。
可爱的办公室完全可能让用户看得更多、却更不批判。

### 1.4 贡献

- **C1（设计框架，§3）**：提出「场景语法与流水线语义绑定」（semantic
  binding）作为职场隐喻界面的设计原则，与灰色文献流派的活动镜像（activity
  mirroring）形成可操作的区分；给出四项设计目标（一瞥即知、阶段可读、忠实性、
  可干预）；将该隐喻安放在一条从抽象活动代理（Babble [11]）经结构化清单
  （Cocoa [18]）到具象职场模拟的易读性光谱上；并报告九次大版本迭代沉淀的
  设计教训（§3.5）。
- **C2（系统，§4–§6）**：Claw 的完整设计与实现，包括 6 阶段编排流水线、
  8 角色数据驱动注册表与双层模型路由、5 种 claw.* 类型化事件协议、办公室
  tick 模拟引擎（寻路、槽位、手势、会议编排）、事件驱动的第一人称叙事层，
  以及三个干预面：自适应澄清门、运行中追问、运行时工具-角色改绑。其中运行时
  改绑（end user 在运行间隙重新指派工具与开关角色，规划器围绕新构成重规划）
  在已核实的设计时组队工具 [30,31] 中没有先例。
- **C3（拟议评估，§7）**：一项对照实验设计：3 界面条件 × 2 运行类型（正常与
  错误注入），测量 SA（SAGAT 式冻结探针）、信任校准（主观量表加行为依赖度量
  [13]）与干预行为（发现延迟、干预及时性与粒度），并把 Carter et al. [24]
  的拟人化误校准预测转化为双向假设 H3。无论 H3 落在哪个方向，结果都构成对
  这个新兴流派的第一份实证证据。

## 2. Related Work

### 2.1 角色化多 agent LLM 框架

Claw 的编排机制没有一项是新的，本文不主张其新颖性。本节的作用是划清界线，
使贡献能落在 HCI 层。

角色条件化（role-conditioning）由 CAMEL [3] 确立：两个 agent 以 inception
prompting 扮演互补角色（AI user 与 AI assistant）自主协作。MetaGPT [1] 把
人类组织的标准作业程序（SOP）编码为 prompt 序列，让产品经理、架构师、工程师、
QA 角色在流水线上传递结构化中间产物，这是 Claw "调度员规划角色标注子任务"
的直接先例。ChatDev [2] 是「虚拟软件公司」隐喻在架构层的起源：CEO、CTO、
程序员、评审员、测试员通过 chat chain 走完瀑布阶段，含 reviewer 式质量门。
需要指出，ChatDev 后来的 Visualizer 是浏览器里的日志回放与 ChatChain 图，
属于开发者调试工具。若评审以 ChatDev 反驳本文隐喻的新颖性，回应是：隐喻
存在于架构命名，空间上演（spatial enactment）是本文的增量。

AutoGen [4] 提供可组合、可对话、可带工具的 agent 编程框架，可视为 Claw
运行时改绑的开发者库版本。AgentVerse [5] 的 recruit、协商、行动、评估循环
动态调整团队构成，是开工辩论与动态组队的机制先例。辩论本身有两条线：Du et
al. [6] 证明同质 LLM 实例多轮辩论收敛可提升事实性与推理；Liang et al. [7]
发现自反思的"思维退化"问题并提出 debater-judge 框架，Claw 的调度员归并共识
是 judge 角色的工程化。质量改进环有 Self-Refine [8]（单模型生成、自反馈、
改写）与 Reflexion [9]（语言化反思存入情景记忆）。Claw 把自反馈外化为独立的
评审员角色，动机与 [7] 对自反思局限的发现一致。

两篇综述确认该设计空间已系统化：Guo et al. [27] 沿 profiling、communication、
environment、capability 四轴分类；Tran et al. [28] 沿 actors、interaction
types、structures、strategies、coordination protocols 五维分类。两者的分类法
中都不存在「面向 end user 的实时过程可视化」类目。界面在该社区的话语中是
开发者日志、聊天转录或最终产物。这个缺位是 §2.5 gap 陈述的佐证之一。

### 2.2 透明度、信任校准与态势感知

评估 Claw 主张所需的构念都有经典出处，且各自带可用仪器。

信任校准方面，Lee & See [12] 的奠基性综述确立了核心立场：设计目标不是信任
最大化，而是 appropriate reliance，即信任与系统真实能力的对齐（calibration）、
分辨（resolution）与特异性（specificity）。Wischnewski et al. [13] 系统综述
了 96 项研究中信任校准的操作化方式，给出主观量表与行为依赖度量的完整菜单，
§7 的仪器直接从中选取。

态势感知方面，Endsley [10] 的 SA 三层（L1 感知、L2 理解、L3 预测）与 SAGAT
冻结探针是人因工程的标准配置。Chen et al. [15] 的 SAT（Situation
awareness-based Agent Transparency）模型把 SA 三层转译为 agent 应当外显的
三层信息（目标与行动、推理依据、结果预测与不确定性），并在军用自治系统的
人在环实验中证明：提高透明层级可改善操作者表现，且不必然增加负荷。SAT 是
本文最直接的理论脚手架。Claw 的办公室可定位为一种 ambient SAT display：
工位活动对应 SAT L1，会议与叙事对应 L2，计划卡与流程阶段对应 L3。SAT 同时
提供了实验范式模板（操纵透明层级，测表现、信任、负荷）。

Legibility 构念来自 Dragan et al. [16] 的机器人运动研究：legible 指旁观者能
从进行中的行为快速推断目标，predictable 指行为符合已知目标的预期，两者可能
冲突；测量方式是从部分行为推断目标的速度与准确率。Claw 把该构念从机械臂
轨迹移植到虚拟 agent 的空间行为："走向会议室"之于"即将规划"，如同 legible
motion 之于抓取目标。Langley et al. [17] 的 explainable agency（agent 应能
自述其决策理由）为叙事层提供构念出处。Amershi et al. [19-G] 的 18 条人机
交互准则（G1 能力可见、G11 行为可解释、G16–18 高效纠正）可用作专家评审
rubric。

LLM 时代的实证工作方面，He et al. [14]（CHI 2025, n=248）是最接近的实验
模板：在 plan-then-execute 范式下操纵用户在规划与执行阶段的参与，测信任与
人机团队表现；其核心发现（用户过度信任表面合理的计划）正是 Claw 声称要缓解
的失效模式。Cocoa [18]（CHI 2026）提出 interactive plans 设计模式：把 agent
计划嵌入文档，用户可把步骤指派给人或 agent，交错规划与执行；lab（n=16）加
一周 field（n=7）评估。Cocoa 是本文空间式表征最重要的设计对照：同样的目标
（让计划与过程可读可控），Cocoa 选择文本与清单表征，Claw 选择空间与具身
表征，两者构成 §3.4 光谱上的相邻点位。

关键的空白在于，上述工作几乎全部研究单 agent 或单自动化系统。唯一针对多
agent LLM 透明度的已核实研究是 Naik et al. [26]：13 名从业者的访谈，命名了
detail-vs-comprehensibility 张力，把"可视化"列为透明度的框架之一，但没有
构建界面，也没有测量。因此一个精确的空白成立：没有 peer-reviewed 工作对
多 agent LLM 团队的透明界面测量过 SA、信任校准或 legibility。

### 2.3 agent 过程可视化：四种既有范式

已核实的过程界面工作可归为四类，Claw 不落入其中任何一类。

第一类是 trace、日志与时间线，服务开发者调试。工业界的主导形态是 LangSmith、
Langfuse、AgentOps 类 trace 树（灰色文献，无学术论文）。学术端，AGDebugger
[19]（CHI 2025）让开发者浏览、编辑、回滚 AutoGen 团队的消息并从 checkpoint
重放，14 人研究提炼了转向策略；AgentLens [20]（TVCG 2025）对 agent 系统的
事后运行记录做层级时间摘要与因果追踪，是"可视化 agent 干了什么"最接近的
学术工作，但回溯式、抽象编码、专家受众三点都与 Claw 相反；DiLLS [39]
（preprint）用三层行为层级支持失败根因的交互式诊断。这一类的共同点：受众是
排障的开发者，表征是抽象的时间或消息结构。

第二类是 node-link 与 DAG 画布，服务结构编辑与内容组织。AI Chains [21]
（CHI 2022）确立了"透明性来自分解"的论证：把 LLM 工作拆成可见的链步骤，
用户感到更透明可控。Claw 继承这条论证，但把"可见的结构"从用户编辑的链换成
自主运转的组织。PromptChainer [22] 是链的可视化编程画布；AutoGen Studio [30]
（EMNLP 2024 demo）把多 agent 工作流做成拖拽式声明配置。Sensecape 与
Graphologue [23]（UIST 2023）虽然使用空间布局，但空间化的对象是 LLM 输出
内容（sensemaking 画布、实时概念图），不是执行过程。内容的意义建构与过程的
易读性是两个问题。

第三类是空间模拟本身作为研究对象。Generative Agents [25]（UIST 2023）让
25 个带记忆、反思、规划的 agent 栖居于像素小镇 Smallville，研究涌现社会行为。
它是 Claw 视觉语言的来源，但两者之间有一个方向相反的关系：Smallville 里模拟
本身是研究对象（模拟社会的可信度），agent 不做真实工作，屏幕服务于研究者的
观察；Claw 里空间模拟是真实任务执行之上的透明层，像素后面是真实的检索、
沙箱执行与产物，屏幕服务于任务委托人。Project Sid [40] 与 AgentSociety [41]
把 agent 社会推向更大尺度，同属"模拟即对象"。TheAgentCompany [42] 用模拟
公司做 benchmark 环境（同事是聊天式 NPC），无空间 UI。

第四类是 CSCW 的职场感知媒介，服务人类协作。media spaces [29]（CACM 1993）
用常开音视频再造分布式团队的共享办公室与走廊，确立外围感知（peripheral
awareness）的价值。social translucence [11]（TOCHI 2000）给出理论框架
（visibility、awareness、accountability），并以 Babble 的极简"社会代理"
（圆圈中的点）实例化。Gather.town 类空间办公室对人类远程协作的参与感有文献
支持 [43]（tier-2，教育场景综述）。这条线索证明职场形态的媒介能维持对同事
活动的外围感知。Claw 的迁移命题是把同事换成 AI worker，把视频流换成事件流；
迁移是否保有效性，是 §7 要测的问题。

定位小结：四类范式沿三个轴与 Claw 分离——受众（开发者、研究者，还是任务
委托人）、时态（事后分析还是实时外围感知）、表征（抽象结构还是具象场所）。
没有已核实的学术工作同时占据实时、end-user、空间职场、真实任务四个属性；
占据这个交点的是 §2.5 的灰色文献。

### 2.4 人在环上的控制与干预

Claw 的三个干预面各有文献基础，其中第三个落在空位上。

澄清门方面，Horvitz [32] 的混合主动原则中"以对话消解关键不确定性"是决策论
版本的澄清门。可调自治 [34] 把 agent 向人的控制移交形式化为 MDP 策略，且
部署于多 agent 办公助理 Electric Elves，是罕见的多 agent 先例，但移交的是
单个决策的 authority，不是团队结构。LLM 端，Ask-before-Plan [36] 把"该问才
问"形式化为 proactive agent planning 任务并给出基准；Andukuri et al. [37]
（ICLR 2025）解释了为什么需要显式的门：单轮 RLHF 使模型默认以假设代替提问，
需要以未来对话轮建模来奖励提问。两者都是模型与基准工作，澄清交互的界面层
人因评估仍是空白。

运行中转向方面，AGDebugger [19] 支持中断、编辑、回滚，但面向开发者且以消息
为操作粒度；CowPilot [35]（NAACL 2025 demo）在网页导航中实现人与 agent 的
动作级交替接管（协作模式 95% 成功率，人只做 15% 的步骤），但是单 agent。
Lubars & Tan [33]（NeurIPS 2019）的委托框架从偏好侧提供动机：用户系统性
偏好 machine-in-the-loop 而非完全自治，信任是首要因子。

团队构成控制是空位所在。AutoGen Studio [30] 让用户在设计时声明 agent roster
与工具指派；AgentCoord [31] 让用户在协调策略的探索期介入分工方案的生成与
修改。经典自动化文献中，功能分配（function allocation）[33-P] 同样是设计时
决策。没有已核实工作研究过 end user 在运行时改绑工具与角色、开关角色，且
规划器在下一轮围绕新构成重规划。Claw 实现了这一交互（§4.5），本文将其作为
次级贡献主张。

### 2.5 灰色文献流派与拟人化反论

先盘点流派（2026 上半年）。Pixel Agents [G1]：VS Code 扩展加 CLI，经 Hooks
API 与 JSONL 转录把 15 种以上编码工具的真实会话镜像成像素办公室（写代码时
打字、检索时读书、等审批时冒气泡，子 agent 生成挂靠父会话的独立角色）；
2026-02-24 上线，8.4k stars，Fast Company 报道。AgentFleet [G2]：像素 RPG
工作区，用户定义角色、派工、圆桌讨论、走到工位、完成时火花庆祝。Bit Office
[G3]：Team Leader、planner、coder、reviewer 单流，宣传语是"在一间可观看、
可控制、可分享的实时像素办公室里规划、编码、评审、交付"。另有 AgentOffice、
claude-office、Agent Pixels、JetBrains Pixel Office 插件等 [G4–G6]。

流派的共同形态有三点。其一，全部是工程制品，无学术处理、无评估、无设计
理论。其二，映射是活动镜像：低层工具或终端活动直接翻译为 sprite 动画，不
携带流程阶段语义（Pixel Agents 明确说明镜像的是终端会话而非角色团队）。
其三，最接近 Claw 的 AgentFleet 与 Bit Office 具备角色与会议元素，但同样
停留在制品层。本文的三项贡献（概念化、语义绑定系统、实证评估）对应流派
缺失的三件事；相关工作把这批工具作为动机证据而非竞争对手引用。

拟人化反论必须正面回应。Carter et al. [24]（*Human Factors* 2024）发现表面
拟人化线索不能带来更好的信任校准。de Visser et al. [38] 发现拟人化增加
trust resilience，即系统出错后信任衰减更慢，这在需要撤回信任的场景里是负面
属性。Waytz et al. [44] 则显示拟人化提升信任与责任归因。三者合并读的含义
是：可爱的办公室可能让用户看得更多，却更不批判。本文不回避这个预测，而是
把它做成 §7 的双向假设 H3：语义绑定的隐喻是否不同于表面拟人化，尤其当错误
被空间化（捂脸、评审会返工）时，信任撤回是更快还是更慢。另注意到界面隐喻
问题正在被理论化（LLM 界面隐喻光谱的 preprint [45]），研究时机合适。

## 3. Design Framework

### 3.1 用户与使用情境

目标用户是委托了真实任务、但不具备 agent 系统专业知识的 end user：市场运营
者要一份带配图的调研报告，教师要一套演示文稿。使用情境有三个约束。第一，
陪伴式而非全神贯注：任务运行数分钟，用户可能同时做别的事，界面必须支持
外围监视。第二，委托后仍有干预责任：澄清、追问、纠偏的时机短暂。第三，无
调试素养假设：不能预设用户会读 trace，或理解 "tool call" 这类词汇。这三个
约束排除了 §2.3 的前两类范式（它们为专注的专家设计），并把设计问题定为：
如何让一个组织的运转状态可以被外围地、无词汇门槛地持续感知，且感知能转化
为行动。

### 3.2 设计目标

- **DG1 一瞥即知（glanceability）**：任意时刻扫一眼即可回答 SA L1（谁在
  干活、谁空闲、谁出错了），零文本阅读。实现依赖空间编码（在工位即在工作）
  与姿态编码（手势区分工作种类，反应区分成败）。
- **DG2 阶段可读（phase legibility）**：流水线处于哪个阶段由场景表达，而非
  状态标签。全员会议表示在规划或辩论，分散工位表示在执行，评审员召集表示
  在复审，派对表示已交付。用户不需要知道 "Phase 1.5" 这种词，场景本身就是
  词汇。
- **DG3 忠实性（faithfulness）**：屏幕上的每个工作行为必须由真实编排事件
  驱动。禁止装饰性"假忙碌"暗示不存在的工作，禁止叙事层虚构未发生的内容。
  氛围性行为（喝咖啡、串门、看绿植）只允许出现在真实空闲状态，且在视觉上
  与工作手势可区分。DG3 是与娱乐化 idle game 的分界线：Claw 的办公室是证据
  的剧场，不是宠物缸。
- **DG4 可干预（actionability）**：看见必须能转化为行动。澄清问题在对话中
  直接回答，运行中随时追问，点击小人查看其调用统计与人设，打开绑定面板
  重构团队。按 [11] 的 accountability 论证，不通向行动的透明只是展示。

### 3.3 核心原则：语义绑定与活动镜像的区分

灰色文献流派 [G1–G6] 的映射方式可以称为活动镜像（activity mirroring）：
探测到低层活动（进程在写文件、会话在流式输出），播放对应动画（打字、
读书）。镜像保真，但语义扁平：它回答"这个 agent 在动吗"，不回答"团队处于
流程的哪一步、这一步的产出物是什么、出了错谁负责修"。当被镜像的对象从单个
编码会话变成一条组织化流水线，扁平性的代价变大：八个小人各自打字的画面，
无法区分"规划已过、辩论达成共识、三个执行者并发中、评审还没开始"这样的
流程事实。

Claw 的映射方式是语义绑定（semantic binding）：场景语法的每个元素对应流水
线的一个语义单元，而非一次低层活动。绑定表如下：

| 场景元素 | 绑定的流水线语义 | 驱动事件（类型化） | SAT 层 |
|---|---|---|---|
| 全员例会，调度员坐主位 | Phase 1 规划与 Phase 1.5 辩论进行中 | `claw.plan`（触发集合） | L2/L3 |
| 计划卡逐条亮起（pending→doing→done） | 子任务清单与逐项进度 | `claw.plan` + `claw.task.update` | L1/L3 |
| 会议桌上各角色提案气泡与共识卡 | 辩论的 proposals 到 agreed 方案 | `claw.debate` payload | L2 |
| 角色走到自己工位，角色化工作手势 | 该角色被指派且其子 agent 运行中 | `tool.start`（带 `agent` 归属） | L1 |
| eureka 手势或捂脸手势 | 工具调用成功或失败 | `tool.end`（成败字段） | L1/L2 |
| 评审员召集评审会 | Phase 3.5 critic 复审进行中 | critic 子 agent 的事件流活跃 | L2 |
| 产物窗口弹出，版本徽标 v1→v2 | work package 新版本落地 | `claw.artifact.updated`（只发版本通知） | L1 |
| 调度员在对话中提问，办公室待命 | 澄清门挂起，等待用户输入 | `claw.clarify` + `awaiting_input` | L3+DG4 |
| 下班派对 | 终态 finished，产物齐备 | run status 翻转 | L1 |
| 回工位闲坐，偶尔串门喝咖啡（同时离位 ≤2 人） | 真实空闲（无任务指派） | 无事件（缺省态） | L1 |

绑定的载体是类型化事件协议（5 种 claw.* 事件加带角色归属的 tool.*，§4.4），
而非对文本日志的启发式解析。这给出 DG3 的忠实性下界：动画可以省略细节，但
不能虚构状态。一个场景元素出现，当且仅当其绑定的语义事件发生。叙事层同理：
角色发言气泡的内容取自真实事件 payload（检索的查询词、子任务的标题、报告的
版本号），人设只改变语气，不产生事实。

这个区分是可操作的：判断一个界面属于哪类，检查其映射的定义域是低层活动还是
流程语义即可。它也是可检验的：§7 的 SA L2/L3 探针测的正是流程语义是否被
传达。镜像式界面预期在 L1 上与 Claw 持平，在 L2/L3 上落后。

### 3.4 易读性光谱

把已核实的设计点排成一条抽象度递减、隐喻负载递增的光谱：

```
抽象活动代理 ──────── 结构化文本/清单 ──────── 节点图/时间线 ──────── 具象空间模拟
Babble 社会代理 [11]    Cocoa 交互计划 [18]      AutoGen Studio [30]     Claw / 流派 [G1-G6]
（圈中的点）            （文档中的清单）          AgentLens [20]          （像素办公室）
```

右移买到的东西：空间与社会隐喻自带"谁在哪、和谁、什么状态"的前注意编码
（DG1），场景转换自带阶段语义（DG2），且隐喻自带词汇，用户不需要学习"会议"
意味着什么。右移付出的是两类风险。一是隐喻失配：会议场景暗示的审议深度可能
超过一次 LLM 归并调用的实际深度；工位与走动暗示的物理约束（一次只能在一处）
与并发执行的真实结构有张力。二是拟人化风险，即 §2.5 的反论：具象小人可能
触发过度信任与迟滞的信任撤回 [24,38]。语义绑定（§3.3）是对第一类风险的结构
性回应：隐喻的每个元素锚定真实语义，失配被限制在程度而非有无。第二类风险
无法靠设计论证消解，只能实证回答（§7 H3）。

光谱的价值在于把"要不要像素办公室"从审美之争变成设计决策：给定用户（专家
还是非专家）、情境（专注还是外围）与风险敞口（错误代价），可以在光谱上选点。
本文的假设是，非专家、外围监视、中等风险的组合下，最优点在右端。这正是 §7
要检验的。

### 3.5 设计过程与教训

Claw 经历九次大版本迭代（v1 单 agent 计划-执行-写报告；v2 并发多 agent 团队；
v3 评审员与澄清门；v4–v5 办公室模拟与叙事；v6 辩论；v7 视频师；v8 办公室
美术与氛围；v9 图像能力），期间的日常使用反馈沉淀出四条设计教训。

**L1 清单状态只由事件驱动。** 早期版本允许前端从 LLM 输出猜测子任务完成
状态，随即出现"计划漂移"：干的和勾的对不上。最终纪律是，计划卡的每次状态
翻转只由 `claw.task.update` 事件驱动，事件由编排器在子 agent 生命周期边界
发出，LLM 无权直接改 UI 状态。一般化的教训：隐喻界面的忠实性要由协议保证，
不能由模型自觉保证。

**L2 安静办公室。** v8.2 之前，空闲角色随机漫游，使用者明确反感（"乱窜"）：
氛围行为淹没了工作信号，DG1 失效。修正为行为规则：空闲缺省态是回自己工位，
休息区仅限下班；同时离位闲逛者全局不超过 2 人；空闲漫步间隔 16–34 秒。
教训：氛围行为需要预算约束，工作信号必须在视觉上占主导。media spaces 文献
中"外围感知不能变成干扰"的结论 [29] 与此一致。

**L3 失败要空间化。** 工具失败最初只是日志里的红字。改成捂脸手势加评审员
走到出错者工位查看之后，错误从可查的记录变成可见的事件。这直接服务 §7 H2
（错误发现延迟）。教训：负面信号比正面信号更需要抢占前注意通道，因为它们
是干预时机的触发器。

**L4 叙事第一人称且事实锚定。** 早期叙事是系统视角的播报（"正在执行
web_search"），没有人读。改为角色第一人称、带人设、内容锚定事件 payload
（"我去查『2026 国产大模型价格』相关的资料"）之后，叙事成为会议与工位场景
的 L2 补充通道。教训：explainable agency [17] 在隐喻界面中的合适形态是
in-character self-report，persona 只修饰语气，不触碰事实（DG3）。

## 4. The Claw System

本节给自足的系统描述；逐文件的工程细节见 [CLAW.md](../CLAW.md)。

### 4.1 架构总览

```
用户 prompt ── POST /api/v1/claw ──▶ Go orchestrator（internal/skill/claw）
   Phase 0 澄清门 → 1 规划 → 1.5 辩论 → 2 并发执行(≤3) → 3 撰稿 → 3.5 评审 → 4 制片 → 5 视频师
        │ 类型化事件（WebSocket，claw.* + tool.*）          │ 产物（HTTP GET，按版本）
        ▼                                                    ▼
Next.js 前端：ClawOffice（officeSim tick 引擎）· ClawChat（叙事+追问+澄清）
              · ArtifactPanel（报告/配图/PPT/视频）· BindingsPanel（改绑）
```

后端约 2,900 行 Go（含测试），前端约 4,000 行 TypeScript/React。LLM 走双层
路由：planner tier 用于规划、撰稿、制片等质量敏感调用，worker tier 用于执行
与澄清判断等大批量调用；当前默认 DeepSeek，可单行配置切换。系统与同仓库的
slides、games 纵切共享 agent 循环（Base、ReAct、ToolCall 三层抽象）、工具
注册表、事件 Hub 与持久化形态。

### 4.2 编排流水线

协调器按下表推进。所有尽力而为的阶段失败即放行，单点增强功能不阻塞交付：

| Phase | 内容 | 门控与降级 |
|---|---|---|
| 0 澄清 | worker-tier LLM 三元判断（清楚/含糊/失败）；含糊则发 `claw.clarify`、状态 `awaiting_input`、协程退出；用户回复经 Resume 续跑 | 仅新任务；判断失败视为放行 |
| 1 规划 | planner-tier LLM 产出角色标注子任务清单；发 `claw.plan` | 失败降级为最小计划 |
| 1.5 辩论 | 每个被指派执行角色并发提案（100 秒超时），调度员归并一致方案；发 `claw.debate`；方案注入撰稿与评审 prompt | 追问轮跳过；少于 2 个声音跳过；失败放行 |
| 2 执行 | 被指派角色作为并发子 agent 运行（goroutine 加信号量，上限 3）；每个子 agent 是以角色 key 命名的 ReAct 循环，事件自动带 `agent` 归属 | 角色停用或工具未接线则该行任务发 skipped |
| 3 撰稿 | 撰稿员汇总全部执行产出，`write_document` 产出报告 v1 | 失败则跳过 3.5–5，收尾守卫报错 |
| 3.5 评审 | 评审员按 rubric（结构、事实引用、图表引用、任务覆盖）复审并重写为 v2 | 软失败保留 v1 |
| 4 制片 | 用户要了 PPT 且角色开启时，经 slides 管线产出真 .pptx | 不在计划或停用则 skipped |
| 5 视频师 | 已有至少一张配图时，Seedance i2v 把配图动画成短片 | 无图或停用则 skipped |
| 收尾守卫 | 产物版本必须大于 0，否则发 error；残留 pending/doing 任务补记 done | — |

子 agent 超时按角色分级（制片 8 分钟，设计师 6 分钟，其余 4 分钟），步数
上限按角色 4–10 步。评审改稿有过一个真实案例：一次运行中评审员发现报告嵌入
了错误的配图 URL 路径，在 v2 中修正。这个案例同时推动了 §3.5 L3 的设计。

### 4.3 角色注册表与能力接线

8 角色为数据驱动注册表（后端 roles.go，前端 workers.ts，按 key 对齐）：

| 角色 | tier | 工具 | 骨干 | 说明 |
|---|---|---|---|---|
| 调度员 coordinator | planner | plan_tasks, update_task | ✓ | 规划、派工、辩论归并 |
| 调研员 researcher | worker | web_search (Tavily) | | 联网检索 |
| 工程师 engineer | worker | code_execute (Rust/wasmtime 沙箱) | | 计算与验证 |
| 设计师 designer | worker | generate_image, edit_image | | 配图生成与修图（抠图、高清、上色、扩图） |
| 撰稿员 writer | planner | write_document | ✓ | 汇总成带版本报告 |
| 评审员 critic | critic | write_document | | rubric 复审改写 |
| 制片 producer | planner | generate_deck | | HTML 转 PPTX 管线 |
| 视频师 videographer | worker | generate_video (i2v) | | 配图转短片 |

工具按运行时能力探测接线：没有 Tavily key 则 web_search 不注册，调研员自动
降级停用，规划器的能力广告随之收缩。换句话说，规划器只会把任务派给真实可用
的角色。加一个新角色需要两端各一条注册项加一个工具适配器，坐标布局、事件
归属、办公室走位都由注册表推导。这个数据驱动结构是 §4.5 运行时改绑得以贯穿
全链的前提。

### 4.4 事件协议与忠实性保证

| 事件 | payload | 消费方 |
|---|---|---|
| `claw.plan` | task_titles[], task_roles[] | 计划卡初始化；小人分工；召集例会 |
| `claw.task.update` | task_index(1-based), status(pending/doing/done/skipped) | 计划卡状态翻转的唯一来源（§3.5 L1） |
| `claw.clarify` | questions[] | 对话中的提问气泡；挂起 |
| `claw.debate` | {proposals:[{role,text}], agreed} | 提案气泡、共识卡、会议延长 |
| `claw.artifact.updated` | kind(report/deck/video), version, bytes | 版本通知；正文经 HTTP 按版本拉取，大 payload 不过 WS |

加上带 `agent` 归属的 `tool.start` 与 `tool.end`（驱动手势与成败反应），构成
§3.3 绑定表的完整驱动集。协议给出三条忠实性保证：场景元素与语义事件一一对应
（无事件不上演）；UI 状态没有模型直写通道（LLM 只能经工具产生事件、间接影响
UI）；产物正文与通知分离（版本徽标不会先于可下载的产物出现）。

### 4.5 运行时工具-角色改绑

绑定配置分两半：可改绑池（web_search、code_execute、generate_image、
edit_image，可在调研员、工程师、设计师之间重新指派）与角色开关（除两个骨干
角色外均可停用）。配置持久化为 JSON，经 `GET/PUT /api/v1/claw/roles` 暴露，
前端绑定面板可视化编辑。生效路径贯穿全链：规划 prompt 的能力广告、子 agent
的工具构建、角色 enabled 判定读同一份 `EffectiveTools`。因此改绑之后，下一轮
规划会围绕新团队构成展开。实测：把 generate_image 改绑给工程师后，规划器把
配图任务指派给工程师执行。

设计动机有两个。工程侧，能力接线本就是动态的（key 有无、沙箱起没起），改绑
是同一机制的用户暴露。交互侧，它实现了 §2.4 的空位：运行时的功能再分配，
相当于 LOA 文献 [33-P] 中 function allocation 的交互式即时版本，也把团队从
系统的既定事实变成用户可修改的对象。用户把团队配坏的风险由降级链兜底：配置
无效时校验拒绝，角色全停时规划器收缩到骨干闭环。

### 4.6 办公室模拟引擎

officeSim 是一个约 90ms 一帧的 tick 引擎，从注册表推导静态几何（每角色工位、
8 个会议席、8 个休息位、3 个公共物件点），运行时维护每个小人的位置、朝向、
路径、槽位与停留期。

寻路采用曼哈顿走廊方案：同排直走，跨排取最近的两条横向走廊折线；恒速移动
（x 方向 0.85%/tick，y 方向 0.6%/tick），朝向随位移方向翻转，行走中低概率
驻足张望。槽位登记保证每个位置（工位、会议席、休息位、物件点）同刻只容
一人，先到先得，离开释放；这使任意人数下无重叠，会议席次稳定。

场景调度上，会议模式优先于一切：`claw.plan` 触发 kickoff，critic 活跃触发
评审会，终态触发派对。非会议时段，工作者去工位，下班者去休息区，空闲者按
§3.5 L2 的氛围预算偶尔离位。

手势系统在小人到位后按场景取手势池：工位工作取该角色的动作池（每角色约
10 个，如写、画、举镜、打电话、点头、思考），会议主持与与会各有节拍池，
派对有庆祝池。`tool.end` 的成败反应（eureka、捂脸）作为限时覆盖层抢占当前
手势，之后自动恢复。一个实现要点：手势动画拥有小人 rig 的 transform，朝向
翻转必须包在外层 wrapper 上，否则两个 transform 互相覆盖。

叙事层（narrate 模块）把 `claw.plan`、`tool.start`、`tool.end` 转为角色
第一人称发言气泡（去重、事实锚定，§3.5 L4），完成后按产物上下文给出下一步
chips（加配图、做成 PPT、做成短视频、高清化、抠图），把"下一步做什么"从
用户的回忆负担变成界面的建议。

产物以可拖拽、可最大化的像素 OS 窗口浮于办公室之上（报告 Markdown 渲染、
配图画廊、PPT 滑动预览、视频播放，均可单件下载），报告落地时自动弹出。空间
场景负责过程，窗口负责结果，两者不争夺同一层视觉。

### 4.7 持久化与会话连续性

每次运行落 Postgres（计划、产物、版本、图、视频、deck、对话记忆全量快照，
带行级安全）。内存 miss 时从库水合整个会话，服务重启后可继续追问迭代。产物
版本单调递增，支持按版本拉取。这使 §7 实验中"错误注入的 v1 到修正的 v2"有
稳定的可复现基础。

## 5. Interaction Walkthrough

以一次真实任务走查全部机制。用户输入：「调研 2026 国产大模型价格战，出一份
对比报告，含价格表和配图」。

T+0s，澄清门。调度员判断任务清楚（目标、产物、约束齐备），直接开工。作为
对照，若输入是「帮我做个报告」，调度员会在对话中问"关于什么主题？需要哪些
产物？"，办公室保持待命，用户在常规输入框回答后续跑。

T+5s，例会。`claw.plan` 到达：全员走进会议室，调度员坐主位，计划卡浮现四条
子任务（调研员两条、工程师一条、设计师一条，撰稿员收尾）。随后 `claw.debate`
到达：调研员提案"先查官方定价页再交叉验证媒体报道"，工程师提案"价格表用
统一单位换算"，设计师提案"趋势图用像素风格"，共识卡落定。此刻用户已经知道
接下来几分钟会发生什么（SA L3）。在日志界面中，同样的信息要到事后才能拼出。

T+30s，并发执行。三人回工位：调研员举放大镜（气泡："我去查『国产大模型 API
定价 2026』"），工程师敲键盘，设计师作画。第 90 秒工程师的沙箱调用超时，
捂脸，评审员起身走到工程师工位（失败空间化，§3.5 L3）；第二次调用成功，
eureka。用户此间在另一个窗口写邮件，靠余光维持监视（DG1）。

T+3m，撰稿与评审。撰稿员汇总出报告 v1，产物窗口弹出；评审员召集评审会，
发现价格表漏了一家厂商的新档位，改出 v2，版本徽标翻转。

T+4m，交付与迭代。派对；chips 出现。用户点「做成 PPT」，制片入场，同一会话
续跑出 8 页 .pptx。用户再追问「价格表改成人民币」，得到 v3。次日用户重开
页面（服务已重启过），会话从库水合，继续追问。

团队重构支线：用户打开绑定面板，把 generate_image 改绑给工程师、停用调研员
（例如不信任外部检索）。下一轮任务的例会上，计划卡里配图任务标注给工程师，
调研类子任务不再出现。

## 6. 与灰色文献流派的逐项对照

| 维度 | Pixel Agents [G1] | AgentFleet [G2] / Bit Office [G3] | Claw |
|---|---|---|---|
| 被可视化的对象 | 编码工具的终端会话 | 自建 agent 的任务执行 | 角色化 6 阶段流水线（真实产物交付） |
| 映射方式 | 活动镜像 | 活动镜像加部分场景（会议、庆祝） | 语义绑定（场景对应阶段，事件协议驱动） |
| 忠实性保证 | Hooks 与转录启发式 | 未说明 | 类型化事件协议三保证（§4.4） |
| 叙事 | 无 | 群聊式 | 第一人称、事实锚定、人设只改语气 |
| 干预面 | 无（观赏） | 派工、群聊 | 澄清门、运行中追问、运行时改绑 |
| 设计理论 | 无 | 无 | DG1–4、语义绑定、易读性光谱、四条教训 |
| 实证评估 | 无 | 无 | 拟议对照实验（§7） |

## 7. Proposed Evaluation（拟议，未执行）

本节按可直接执行的粒度给出方案；执行前需伦理审查与预注册。

### 7.1 研究问题与假设

- **RQ1（理解）**：相对日志与清单基线，语义绑定的空间职场隐喻是否提升用户
  对多 agent 流水线的态势感知？
  H1a：办公室条件在 SA L1（谁在干什么）上不低于基线。镜像逻辑上 L1 谁都能
  做，预期持平或小幅占优。
  H1b：在 SA L2 与 L3（为什么、接下来）上显著优于日志基线。这是语义绑定
  独有的承诺，对应 §3.3 末的可检验区分。
- **RQ2（干预）**：隐喻是否改善干预行为？
  H2：办公室条件对注入错误的发现延迟更短，干预（追问、纠偏指令）更及时、
  更有针对性。
- **RQ3（信任校准，双向假设）**：
  H3-risk（依 [24,38]）：拟人化使办公室条件在错误注入运行中信任撤回更慢，
  对产物缺陷的验收更宽松。
  H3-benefit（依 §3.3 与 L3）：错误的空间化使撤回更快。
  方向本身是实证问题；任一方向的显著结果都构成对该流派的第一份实证证据。
- 探索性问题：参与度、愉悦度与作业负荷（NASA-TLX）的代价收益；新奇效应留待
  后续 field study。

### 7.2 设计

Between-subjects 三条件，组内两种运行类型，定序 counterbalance。

- 条件 A（日志基线）：聊天时间线加工具调用行，LangSmith 与聊天转录风格，
  代表工业界现状。
- 条件 B（清单基线）：结构化任务清单加阶段时间线，Cocoa [18] 式文本表征。
  这是光谱中点的强基线，用于分离"结构化"与"空间隐喻"各自的贡献。
- 条件 C（Claw 办公室）：完整系统。
- 三条件由同一后端事件流驱动。§4.4 的协议保证三条件信息等价、仅表征不同，
  表征因此成为唯一自变量。这是系统架构对实验内部效度的直接贡献。
- 运行类型：正常运行与错误注入运行。注入两类错误（经录制回放保证跨参与者
  一致）：(i) 过程错误，一次检索工具失败后由不合适的角色续做，可从过程观察
  发现；(ii) 产物错误，报告 v2 中保留一处评审未捕获的事实错误，考验信任
  撤回后验收是否更仔细。

### 7.3 任务与材料

两个真实委托任务（counterbalance）：市场调研报告（含数据表与配图）与科普
演示文稿，单次任务时长目标 6 到 8 分钟。运行采用录制回放：预先录制真实运行
的事件流，实验中按真实时间戳回放，保证跨条件跨参与者的过程完全一致，同时
保留追问与干预接口（干预触发预录的分支流）。回放法牺牲部分生态效度换取
可比性；生态效度由后续 field study 补足（§9）。

### 7.4 参与者

目标 N=48（每条件 16），以非开发者为主（有委托 AI 做文书类工作的经验、无
agent 框架开发经验），经 Prolific 或校内池招募，预筛问卷排除 HCI 与 ML
从业者。功效依据：SAT 文献 [15] 与 He et al. [14] 报告的透明度操纵效应量多
为中等（f 约 0.25–0.35），alpha=.05、power=.80 下三组 ANOVA 需约 42–52 人。
先跑 n=6 的 pilot 校准任务时长与探针时机。

### 7.5 流程

知情同意，训练任务（熟悉所在条件界面，5 分钟），正式任务两次（正常与注入，
定序平衡）。每次任务中安排 SAGAT 式冻结探针三次：随机时点冻结屏幕，回答
L1、L2、L3 各一题（如"现在哪些角色在工作？""评审员为什么走向工程师？"
"接下来会产出什么？"）。任务中可自由追问与干预，行为全程记录。任务后完成
信任量表、产物验收（对报告给出接受、修改或拒绝，并指出问题）与 NASA-TLX。
全部结束后做半结构访谈（10 分钟），围绕"你靠什么判断它在正常工作"。总时长
约 60 分钟。

### 7.6 测量

| 构念 | 仪器 | 来源 |
|---|---|---|
| SA L1/L2/L3 | SAGAT 冻结探针正确率 | [10,15] |
| 信任（主观） | 校准敏感的信任量表（区分能力、过程、意图维度） | 从 [13] 的菜单选取 |
| 信任（行为） | 依赖行为：产物验收决策与产物真实质量的对齐（signal detection 框架：该收时收为 hit，不该收时收为 false alarm） | [12,13] |
| 信任撤回速率 | 错误暴露前后信任评分差；验收严格度变化 | H3 专用，[24,38] |
| 错误发现 | 从错误事件到用户首次相关行为（探针自发报告、追问、访谈提及）的延迟 | H2 |
| 干预 | 追问与纠偏的次数、时机、针对性（编码为泛泛与指向具体角色或子任务两类） | H2, DG4 |
| 负荷与参与 | NASA-TLX；短式参与度量表 | 探索性 |

### 7.7 分析计划

SA 与信任校准指标做 3（界面）×2（运行类型）混合 ANOVA，或对应的混合效应
模型（参与者随机截距）。错误发现延迟做生存分析（未发现记为右删失）。H3
预注册为双尾。访谈做主题分析，聚焦用户判断"正常运转"的证据来源。预注册的
排除标准：训练任务未通过操作检查者。

### 7.8 效度威胁

条件 C 同时改变隐喻与美学（像素风），存在混淆；缓解方式是条件 B 使用同等
精致的视觉设计，并探索性地在 C 内测美学元素关闭版（去人设叙事）作为补充
分析。单一系统问题：隐喻与 Claw 流水线绑定，结论向其他流水线结构的泛化需
谨慎（§9）。任务时长问题：分钟级 lab 任务短于真实委托，外围监视的价值可能
被低估，这对条件 C 是保守方向。实验者期望问题：探针与编码由对条件盲的第二
编码员复核。

## 8. Discussion

**隐喻的忠实性边界。** 语义绑定保证屏幕上的状态真实，但不保证隐喻的暗示
真实：评审会场景暗示的审议深度超过一次 rubric 复审调用的实际深度，会议桌
暗示的对等协商也掩盖了归并实际上由调度员单方完成。这类表征层的过度承诺是
光谱右端的固有属性，无法靠设计消除，只能缓解：用事实锚定的叙事约束内容，
在 H3 中测其后果，并在界面中保留通往原始事件的下钻路径（点击小人查看调用
统计；办公室是缺省视图，不是唯一视图）。一个更根本的开放问题：透明界面的
义务是传达系统的真实机制，还是传达足以支撑正确决策的状态抽象？Claw 的立场
是后者，这个立场本身可以争论。

**从监督界面到协作界面。** 当前的干预面（澄清、追问、改绑）都发生在办公室
之外的聊天与面板中，空间本身只输出信息、不接受操作。自然的延伸是让场所可
操作：把任务卡拖到某个角色桌上表示指派，点某张桌子要求进度汇报，把两个小人
拉进会议室触发协商。文本界面没有这类操作的自然映射，这可能是光谱右移在
DG4 维度上尚未兑现的收益。

**对流派的意义。** 若 H1b 与 H2 成立且 H3-risk 不成立，像素办公室流派可以
从观赏品升级为有实证支撑的透明度设计模式，语义绑定也给出可迁移的设计规范
（绑定表方法适用于任何有类型化事件的流水线）。反之，若 H3-risk 成立，本文
将给这个正在扩散的流派提供第一份实证警示：在错误代价高的场景，这类界面
可能有害。两种结果的价值不对称，但都为正。

**超越办公室。** 语义绑定原则不限于职场隐喻：手术室（高风险审批流）、厨房
（流水线产出）、乐队（并发协奏）都是候选场景语法。选择办公室，是因为其
语法与角色化流水线的语义单元天然同构（会议、工位、评审、交付），且用户对
该语法的先验知识最完整。什么样的流水线语义适配什么样的场景语法，是框架的
下一步问题。

## 9. Limitations

第一，实验未执行，本文当前是设计文档，全部实证主张待 §7 检验。第二，单一
实现，隐喻评估与 Claw 的特定流水线和美学耦合。第三，回放法牺牲部分生态
效度，且分钟级任务无法回答新奇效应与长期使用问题；需要 field study 补足
（把 Claw 部署给真实委托任务的用户 2 到 4 周，比较使用第 1 天与第 14 天的
监视模式）。第四，文化与美学特异性：像素办公室的可读性可能依赖特定的游戏
文化素养。第五，引用中 5 条为 preprint（[26,28,39–41]），投稿前需复查
archival 版本；灰色文献 [G1–G6] 的 star 数与报道为 2026-07 检索快照，引用
前需做网页存档。

## 10. Conclusion

Claw 把观察多 agent 团队的方式从阅读日志换成观察场所：一间由类型化编排事件
驱动、场景语法与流水线语义绑定的像素办公室，背后是一条真实交付报告、配图、
PPT 与视频的 8 角色流水线。设计框架为一个正在流行的界面直觉（office as
interface）提供了可检验的理论位置：语义绑定把它与活动镜像区分开，易读性
光谱给出它的坐标，四条设计教训给出工程纪律。拟议实验给出了检验它的具体
路径，包括检验它可能失败的方式（拟人化误校准）。办公室是否是多 agent 时代
合适的界面，应当由数据回答；本文的工作是把这个问题问到足够精确，使数据
能够回答。

---

## References

（编号引用均经 URL 核实；完整 URL 与核实记录见
[claw-related-work-scan.md](claw-related-work-scan.md) 附录 A。）

[1] Hong, S. et al. 2024. MetaGPT: Meta Programming for a Multi-Agent Collaborative Framework. *ICLR 2024* (oral).
[2] Qian, C. et al. 2024. ChatDev: Communicative Agents for Software Development. *ACL 2024*, 15174–15186.
[3] Li, G. et al. 2023. CAMEL: Communicative Agents for "Mind" Exploration of Large Language Model Society. *NeurIPS 2023*.
[4] Wu, Q. et al. 2024. AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation. *COLM 2024*.
[5] Chen, W. et al. 2024. AgentVerse: Facilitating Multi-Agent Collaboration and Exploring Emergent Behaviors. *ICLR 2024*.
[6] Du, Y. et al. 2024. Improving Factuality and Reasoning in Language Models through Multiagent Debate. *ICML 2024*.
[7] Liang, T. et al. 2024. Encouraging Divergent Thinking in Large Language Models through Multi-Agent Debate. *EMNLP 2024*.
[8] Madaan, A. et al. 2023. Self-Refine: Iterative Refinement with Self-Feedback. *NeurIPS 2023*.
[9] Shinn, N. et al. 2023. Reflexion: Language Agents with Verbal Reinforcement Learning. *NeurIPS 2023*.
[10] Endsley, M.R. 1995. Toward a Theory of Situation Awareness in Dynamic Systems. *Human Factors* 37(1), 32–64.
[11] Erickson, T. & Kellogg, W.A. 2000. Social Translucence: An Approach to Designing Systems that Support Social Processes. *ACM TOCHI* 7(1), 59–83.
[12] Lee, J.D. & See, K.A. 2004. Trust in Automation: Designing for Appropriate Reliance. *Human Factors* 46(1), 50–80.
[13] Wischnewski, M., Krämer, N. & Müller, E. 2023. Measuring and Understanding Trust Calibrations for Automated Systems. *CHI 2023*.
[14] He, G., Demartini, G. & Gadiraju, U. 2025. Plan-Then-Execute: An Empirical Study of User Trust and Team Performance When Using LLM Agents as a Daily Assistant. *CHI 2025*.
[15] Chen, J.Y.C. et al. 2018. Situation Awareness-Based Agent Transparency and Human-Autonomy Teaming Effectiveness. *Theoretical Issues in Ergonomics Science* 19(3), 259–282.
[16] Dragan, A.D., Lee, K.C.T. & Srinivasa, S.S. 2013. Legibility and Predictability of Robot Motion. *HRI 2013*, 301–308.
[17] Langley, P. et al. 2017. Explainable Agency for Intelligent Autonomous Systems. *IAAI-17*, 4762–4763.
[18] Feng, K.J.K. et al. 2026. Cocoa: Co-Planning and Co-Execution with AI Agents. *CHI 2026*.
[19] Epperson, W. et al. 2025. Interactive Debugging and Steering of Multi-Agent AI Systems. *CHI 2025*.
[19-G] Amershi, S. et al. 2019. Guidelines for Human-AI Interaction. *CHI 2019*.
[20] Lu, J. et al. 2025. AgentLens: Visual Analysis for Agent Behaviors in LLM-Based Autonomous Systems. *IEEE TVCG* 31, 4182ff.
[21] Wu, T., Terry, M. & Cai, C.J. 2022. AI Chains: Transparent and Controllable Human-AI Interaction by Chaining Large Language Model Prompts. *CHI 2022*.
[22] Wu, T. et al. 2022. PromptChainer: Chaining Large Language Model Prompts through Visual Programming. *CHI 2022 EA*.
[23] Suh, S. et al. 2023. Sensecape: Enabling Multilevel Exploration and Sensemaking with Large Language Models. *UIST 2023*; Jiang, P. et al. 2023. Graphologue: Exploring Large Language Model Responses with Interactive Diagrams. *UIST 2023*.
[24] Carter, et al. 2024. (HATEM) — superficial anthropomorphism does not improve trust calibration. *Human Factors* (DOI 10.1177/00187208231218156).
[25] Park, J.S. et al. 2023. Generative Agents: Interactive Simulacra of Human Behavior. *UIST 2023*.
[26] Naik, S. et al. 2026. "So There's a Catch-22 Here": How Early Adopters Who Build Multi-Agent LLM Systems Conceptualize Transparency. arXiv:2606.08323 (preprint).
[27] Guo, T. et al. 2024. Large Language Model Based Multi-Agents: A Survey of Progress and Challenges. *IJCAI 2024*, 8048–8057.
[28] Tran, K.-T. et al. 2025. Multi-Agent Collaboration Mechanisms: A Survey of LLMs. arXiv:2501.06322 (preprint).
[29] Bly, S.A., Harrison, S.R. & Irwin, S. 1993. Media Spaces: Bringing People Together in a Video, Audio, and Computing Environment. *CACM* 36(1), 28–46.
[30] Dibia, V. et al. 2024. AutoGen Studio: A No-Code Developer Tool for Building and Debugging Multi-Agent Systems. *EMNLP 2024 System Demonstrations*.
[31] Pan, B. et al. 2024/2025. AgentCoord: Visually Exploring Coordination Strategy for LLM-Based Multi-Agent Collaboration. arXiv:2404.11943 / *Computers & Graphics* 2025.
[32] Horvitz, E. 1999. Principles of Mixed-Initiative User Interfaces. *CHI 1999*, 159–166.
[33] Lubars, B. & Tan, C. 2019. Ask Not What AI Can Do, But What AI Should Do: Towards a Framework of Task Delegability. *NeurIPS 2019*.
[33-P] Parasuraman, R., Sheridan, T.B. & Wickens, C.D. 2000. A Model for Types and Levels of Human Interaction with Automation. *IEEE Trans. SMC-A* 30(3), 286–297.
[34] Scerri, P., Pynadath, D.V. & Tambe, M. 2002. Towards Adjustable Autonomy for the Real World. *JAIR* 17, 171–228.
[35] Huq, F. et al. 2025. CowPilot: A Framework for Autonomous and Human-Agent Collaborative Web Navigation. *NAACL 2025 System Demonstrations*.
[36] Zhang, X. et al. 2024. Ask-before-Plan: Proactive Language Agents for Real-World Planning. *Findings of EMNLP 2024*, 10836–10863.
[37] Andukuri, C. et al. 2025. Modeling Future Conversation Turns to Teach LLMs to Ask Clarifying Questions. *ICLR 2025*.
[38] de Visser, E.J. et al. 2016. Almost Human: Anthropomorphism Increases Trust Resilience in Cognitive Agents. *JEP: Applied* 22(3).
[39] Sheng, R. et al. 2026. DiLLS: Interactive Diagnosis of LLM-Based Multi-Agent Systems via Layered Summary of Agent Behaviors. arXiv:2602.05446 (preprint).
[40] Altera.AL. 2024. Project Sid: Many-Agent Simulations Toward AI Civilization. arXiv:2411.00114 (preprint).
[41] Piao, J. et al. 2025. AgentSociety: Large-Scale Simulation of LLM-Driven Generative Agents. arXiv:2502.08691 (preprint).
[42] CMU. 2024. TheAgentCompany. arXiv:2412.14161 (preprint).
[43] Zhao, X. & McClure, C.D. 2024. Gather.Town: A Gamification Tool to Promote Engagement and Establish Online Learning Communities. *RELC Journal* 55(1), 240–245.
[44] Waytz, A., Heafner, J. & Epley, N. 2014. The Mind in the Machine: Anthropomorphism Increases Trust in an Autonomous Vehicle. *JESP* 52, 113–117.
[45] 2026. Beyond Anthropomorphism: A Spectrum of Interface Metaphors for LLMs. arXiv:2603.04613 (preprint).

**Gray literature（灰色文献；引用前须做网页存档，star 数为 2026-07 快照）**
[G1] De Lucca, P. 2026. Pixel Agents. github.com/pixel-agents-hq/pixel-agents（2026-02-24 上线；8.4k stars；Fast Company 报道）.
[G2] DBell-workshop. 2026. AgentFleet. github.com/DBell-workshop/AgentFleet（2026-04）.
[G3] longyangxi. 2026. Bit Office. Product Hunt / github.com/longyangxi/bit-office（2026-03）.
[G4] harishkotra. 2026. AgentOffice. github.com/harishkotra/agent-office.
[G5] Robello, P. 2026. claude-office. github.com/paulrobello/claude-office.
[G6] Agent Pixels. 2026-05. agent-pixels.com；另有 JetBrains Pixel Office 插件等同类。

---

*AI disclosure: 本设计文档由 AI 辅助工具（Claude Code）起草，基于对 Claw 代码库
的自动化架构梳理与经 URL 核实的多 agent 文献扫描（检索与核实方法见
claw-related-work-scan.md）；系统设计、迭代决策与全部产品判断来自作者。投稿版
需按目标 venue 的 AI 使用政策补充正式披露声明。*
