# Claw 论文·相关工作扫描（lit-review）

> 生成：2026-07-02，deep-research lit-review 模式（4 文献簇并行检索 + 1 对抗性新颖性核查）。
> 每条文献均经联网核实（arXiv / ACM DL / ACL Anthology / OpenReview / 出版社页面），
> 无法确认存在的候选一律剔除。灰色文献（无论文的工具/产品）已明确标注。
> 用途：为 `/ars-plan` 的研究问题定义与实验设计提供文献版图。

---

## 0. 执行摘要（先读这个）

1. **Claw 的编排机制（角色分工/辩论/评审改稿/动态组队）全部是已知技术**——
   MetaGPT、ChatDev、CAMEL、AutoGen、AgentVerse、multi-agent debate、
   Self-Refine/Reflexion 已覆盖，且已被两篇综述系统化。论文**必须**把编排让位为
   "known art"，贡献不能落在这里。
2. **⚠️ 关键发现：naive gap 主张已死。**「没有人用实时空间办公室隐喻做真实多
   agent 系统的监控界面」被 2026 年初的一批公开工具证伪：**Pixel Agents**
   （2026-02-24 上线，8.4k stars，Fast Company 报道）、**Bit Office**（规划-编码-
   评审角色流水线 + 实时像素办公室）、**AgentFleet**（角色、圆桌会议、庆祝动画，
   与 Claw 特性几乎逐条对应）等至少 8 个实现。2026 年 7 月投稿已无法以
   "concurrent work" 处理 2 月的病毒式发布。
3. **收窄后的 gap 依然成立，且对 CHI 类会议反而是更好的故事**：
   办公室隐喻可视化已成为一个**无人研究过的新兴流派**（全部是工程制品，
   零学术处理、零实证评估）。幸存的贡献 =
   **概念化（设计理论）+ 保真机制（场景语法与真实流水线语义绑定）+ 实证评估**。
   **用户实验从"加分项"变成"没有它论文不存在"。**
4. 评审最可能拿来打的三篇：**Pixel Agents**（灰色文献先例）、**ChatDev**
   （办公室隐喻在架构层的起源）、**Generative Agents/Smallville**（像素小人视觉
   语言的来源）。另有一篇**危险文献**：Carter et al. 2024（HATEM）证明表面拟人化
   **不能**校准信任——它预言办公室隐喻可能反而误导信任，论文必须正面回应。
5. 测量工具链完备：信任校准（Lee & See 2004；Wischnewski et al. CHI 2023 工具箱）、
   态势感知三层（Endsley 1995 → SAT 模型 Chen et al. 2018）、legibility
   （Dragan et al. 2013）。最接近的实验模板：He et al. CHI 2025（Plan-Then-Execute,
   n=248）与 Cocoa CHI 2026（文档式交互计划——Claw 空间式表征的天然对照组）。

---

## 1. 簇 1 — 角色化多 agent LLM 框架（定位为 known art）

| # | 文献 | venue | 对 Claw 的意义 |
|---|---|---|---|
| 1 | Hong et al. 2024, **MetaGPT** | ICLR'24 oral | SOP 角色流水线（PM/架构师/工程师/QA）= Claw 角色分工的直接先例 |
| 2 | Qian et al. 2024, **ChatDev** | ACL'24 | 「虚拟软件公司」隐喻起源 + reviewer 质量门；其 Visualizer 是日志回放非空间化 |
| 3 | Li et al. 2023, **CAMEL** | NeurIPS'23 | 角色条件化对话的奠基工作 |
| 4 | Wu et al. 2024, **AutoGen** | COLM'24 | 可组合、带工具的 conversable agents ≈ 改绑的开发者库版本 |
| 5 | Chen et al. 2024, **AgentVerse** | ICLR'24 | recruit→协商→执行→评估动态组队 ≈ 开工辩论先例 |
| 6 | Du et al. 2024, Multiagent Debate | ICML'24 | 辩论提升事实性——Claw 开工辩论的机制先例 |
| 7 | Liang et al. 2024, MAD | EMNLP'24 | debater–judge 框架 = 调度员归并共识的先例 |
| 8 | Madaan et al. 2023, **Self-Refine** | NeurIPS'23 | 单 agent 自反馈 → Claw 把反馈外化为独立 critic |
| 9 | Shinn et al. 2023, **Reflexion** | NeurIPS'23 | 语言反馈作为质量信号 |
| 10 | Guo et al. 2024, LLM-MA Survey | IJCAI'24 | 综述分类中**没有**"end-user 空间可视化"类目 → gap 佐证 |
| 11 | Tran et al. 2025, Collaboration Mechanisms Survey | arXiv (tier-2) | 同上，协调协议已系统化 |

URL 与完整条目见附录 A。
**簇结论**：引用本簇来"缴械"——承认编排是成熟设计空间，把新颖性完全押在 HCI 层。

## 2. 簇 2 — 透明度/信任校准/态势感知（理论与测量仪器）

| # | 文献 | venue | 提供什么 |
|---|---|---|---|
| 1 | Lee & See 2004 | *Human Factors* | 核心因变量=**信任校准**（appropriate reliance），不是信任最大化 |
| 2 | Endsley 1995 | *Human Factors* | SA 三层：L1 谁在干什么(工位)/L2 为什么(会议+叙事)/L3 接下来(计划)——与办公室隐喻逐层对应 |
| 3 | Chen et al. 2018, **SAT 模型** | *TIES* | 最直接的理论脚手架：Claw = ambient SAT display；含实验范式模板 |
| 4 | Dragan et al. 2013, Legibility | HRI'13 | "从行为推断意图"的 legibility 构念 + 测量（goal-inference 速度/准确率） |
| 5 | Langley et al. 2017, Explainable Agency | AAAI/IAAI | 角色第一人称叙事的构念出处（position paper，只引构念） |
| 6 | Amershi et al. 2019, HAI Guidelines | CHI'19 | 18 条设计准则 = 可用作专家评审 rubric（G1/G2/G11/G16-18） |
| 7 | Wischnewski et al. 2023 | CHI'23 | 信任校准**测量工具箱**（96 篇系统综述：主观量表 vs 行为依赖度量） |
| 8 | He et al. 2025, Plan-Then-Execute | CHI'25 | **最接近的实验模板**（n=248；阶段性监督为自变量；发现用户过度信任"看起来合理"的计划——正是 Claw 声称缓解的失效模式） |
| 9 | Feng et al. 2026, **Cocoa** | CHI'26 | 设计空间主对照：文档/清单式计划表征 vs Claw 空间式表征；lab+field 评估模板 |
| 10 | Naik et al. 2026, Catch-22 | arXiv (tier-2) | **唯一**针对多 agent LLM 透明度的研究——但只是访谈（13 名从业者），点名 detail-vs-comprehensibility 张力，没做界面没做测量 |

**簇结论**：所有构念和仪器都是现成的，但**几乎全部只研究过单 agent/单自动化系统**。
「没有人对多 agent LLM 团队的透明界面**测过**信任校准/SA/legibility」这半句 gap 依然成立。

## 3. 簇 3 — agent 过程可视化界面（现有范式分类）

四种既有范式：

1. **trace/log/时间线**（开发者调试）：LangSmith（灰色文献）、
   **AGDebugger**（Epperson et al. CHI'25，消息级编辑/回滚+14人研究）、
   **AgentLens**（Lu et al. TVCG'25，事后层级时间线+因果追踪——学术上最接近的
   "看 agent 干了什么"工作，但是回溯式、抽象、专家向）、DiLLS（arXiv'26 preprint）。
2. **node-link/DAG 画布**（结构编辑）：AI Chains（CHI'22，透明性-via-分解论证的源头）、
   PromptChainer（CHI'22 EA）、AutoGen Studio（EMNLP'24 demo）；
   Sensecape/Graphologue（UIST'23）空间化的是 LLM **内容**而非**过程**。
3. **空间模拟-as-研究对象**：**Generative Agents/Smallville**（Park et al. UIST'23——
   像素视觉语言的来源；关键反转：那里模拟本身是研究对象，agent 不干真活，观众是
   研究者；Claw 里空间模拟是真实任务执行之上的透明层，观众是任务委托人）、
   Project Sid（arXiv'24）、AgentSociety（arXiv'25）。
4. **CSCW 职场感知媒介血统**（人类协作）：Media Spaces（CACM 1993，
   ambient awareness 的源头）、**Social Translucence**（Erickson & Kellogg,
   TOCHI 2000——visibility→awareness→accountability，Claw 主张的理论词汇表；
   Babble 选了极简抽象 proxy，Claw 选了极繁具象办公室——这是一条**可命名的设计
   光谱**）、Gather.town 研究（RELC 2024, tier-2）。

**簇结论**：写相关工作的定位动作是现成的——拿 Smallville 的表征 + Social
Translucence 的理论 + trace 工具的数据源，指出没有（学术工作）把三者合起来。

## 4. 簇 4 — 人在环上的控制与干预

| 干预面 | 已有工作 | 未覆盖 |
|---|---|---|
| (a) 澄清门 | Horvitz 1999 混合主动（"以对话消解关键不确定性"）；Ask-before-Plan（EMNLP'24 Findings，ask-vs-act 形式化）；Andukuri et al. ICLR'25（为何模型默认不问） | 无人做过**界面层**的人因评估；无多 agent 规划器内的澄清 |
| (b) 运行中转向 | AGDebugger（CHI'25，消息编辑/回滚，开发者向）；CowPilot（NAACL'25 demo，动作级接管，单 agent） | 面向**end-user**、自然语言追问式的团队转向 |
| (c) 团队构成控制 | AutoGen Studio（**设计时**编排 roster/工具）；AgentCoord（arXiv'24+C&G'25，策略**探索期**干预）；经典框架：Parasuraman/Sheridan LOA、Scerri et al. 可调自治、Lubars & Tan 委托偏好（NeurIPS'19） | **运行时**由 end-user 改绑工具↔角色、且规划器真正围绕新构成重规划——**这个槽位是空的**（Claw 的次级贡献点，成立） |

## 5. ⚠️ 对抗性新颖性核查——完整结论

### 5.1 击杀原始主张的灰色文献（2026 年初的流派化爆发）

| 工具 | 时间 | 与 Claw 重叠 | 缺什么 |
|---|---|---|---|
| **Pixel Agents**（De Lucca） | 2026-02-24；8.4k★；Fast Company 报道 | 真实 Claude Code/Cursor/Codex 会话 → 像素办公室；打字/读码/走动/等审批气泡；**子 agent 生成独立角色** | 无会议、无庆祝、无角色流水线、无叙事（镜像终端会话而非角色团队） |
| **AgentFleet** | 2026-04 | 角色定义、派工、**圆桌会议**、走到工位、**完成庆祝** | 68★小项目；无学术处理 |
| **Bit Office** | ~2026-03（Product Hunt） | Team Leader–planner–coder–reviewer 流水线 + 实时像素办公室 | 同上 |
| AgentOffice / claude-office / Agent Pixels / Pixel Office (JetBrains) 等 | 2026 上半年 | 同流派 | 同上 |

→ **裁决：作为"制品新颖性"的 gap 已死；作为"研究空白"的 gap 幸存。**
这批工具全部是工程制品：把低层工具活动镜像到 sprite 上，
**没有一个被学术研究过，没有任何实证评估，没有设计理论**。

### 5.2 幸存的（可辩护的）gap 陈述

> Office-metaphor visualizations of live LLM agents have recently emerged as a
> gray-literature genre (Pixel Agents, Bit Office, AgentFleet, 2026), but exist
> only as engineering artifacts: they mirror low-level tool activity onto
> sprites, and none has been the subject of academic study. No peer-reviewed
> work (a) treats the spatial workplace metaphor as a **designed transparency
> mechanism** whose scene grammar is semantically bound to the phases of a
> role-structured agent pipeline (kickoff debate → concurrent execution →
> critic review) with in-character narration grounded in actual agent messages,
> or (b) **empirically evaluates** how such a metaphor affects users'
> understanding, trust calibration, and oversight of a real multi-agent system
> executing their own task, against non-metaphorical baselines (logs,
> timelines, visual analytics).

对应的贡献重定位：

1. **概念化**：命名并刻画这条设计光谱（Babble 抽象 proxy ←→ 具象职场模拟），
   提出「场景语法↔流水线语义绑定」的设计框架（Claw 的会议=规划/评审阶段、
   工位=执行、庆祝=完成，是**语义**映射，不是活动镜像——这是与 Pixel Agents
   一类的本质区别，论文要讲清楚）；
2. **系统**：Claw 作为该框架的完整实例（+运行时改绑作为次级交互贡献，§4c 槽位）；
3. **实证**：首个对照用户实验（隐喻界面 vs 日志/时间线基线），测信任校准、
   SA、错误发现、干预行为。**没有(3)，论文不成立。**

### 5.3 评审会引用来打的文献 + 必须正面回应的反论

- **Pixel Agents / ChatDev / Generative Agents**（三大必被引用的"先例"）；
  AgentLens 作为学术可视化基线。
- **危险反论**：Carter et al. 2024（HATEM, *Human Factors*）——表面拟人化
  **不能**校准信任；de Visser et al. 2016——拟人化增加 trust **resilience**
  （可能导致该撤回信任时不撤回）。这两篇预言办公室隐喻可能**误**校准信任。
  应对：把它变成实验假设之一（H：语义绑定的隐喻 ≠ 表面拟人化；用错误注入
  条件测"该不信时是否及时不信"）。回应得好这反而是论文最有趣的部分。
- 相关理论正在升温：Beyond Anthropomorphism（arXiv 2026，LLM 界面隐喻光谱）——
  说明这个问题空间正被理论化，时机合适但窗口在收窄。

---

## 6. 对 /ars-plan 的输入（建议的研究问题雏形）

- **RQ1（理解）**：与日志/时间线基线相比，语义绑定的空间职场隐喻是否提升用户对
  多 agent 流水线状态的态势感知（SA L1–L3）？
- **RQ2（信任校准）**：在含注入错误的运行中，隐喻界面用户的信任校准
  （该依赖时依赖、该干预时干预）是否优于基线？还是如 HATEM 预言的更差？
- **RQ3（干预）**：隐喻界面是否改变干预的及时性与粒度（澄清回复、追问转向、
  运行时改绑）？
- 设计维度自变量候选：隐喻具象度（抽象 proxy / 清单 Cocoa 式 / 空间办公室）×
  叙事有无；因变量仪器全部可从簇 2 直接取。
- 目标 venue 建议：CHI（完整实验）或 UIST（若强调系统+机制）；
  DIS/CSCW 若往设计理论/协作走。demo track（ACL 系）可先占位。

## 7. 局限与时效声明

- 5 条 2025–2026 文献是 arXiv preprint（Tran 2025、Naik 2026、DiLLS 2026、
  Project Sid、AgentSociety），投稿前需复查是否已有 archival 版本。
- 灰色文献（Pixel Agents 等）的 star 数/报道为 2026-07-02 检索快照，易变；
  论文引用前需重新截取证据（网页存档）。
- Gather.town 的 CHI/CSCW 级实证研究未找到，视为开放检索项，勿凭记忆引用。
- 本扫描聚焦英文文献；未扫中文库。

---

## 附录 A — 完整核实条目（含 URL）

### 簇 1
1. MetaGPT — https://arxiv.org/abs/2308.00352 (ICLR'24 oral, OpenReview VtmBAGCN7o)
2. ChatDev — https://aclanthology.org/2024.acl-long.810/
3. CAMEL — https://arxiv.org/abs/2303.17760 (NeurIPS'23)
4. AutoGen — https://arxiv.org/abs/2308.08155 (COLM'24)
5. AgentVerse — https://arxiv.org/abs/2308.10848 (ICLR'24)
6. Du et al. debate — https://arxiv.org/abs/2305.14325 (ICML'24)
7. Liang et al. MAD — https://aclanthology.org/2024.emnlp-main.992/
8. Self-Refine — https://arxiv.org/abs/2303.17651 (NeurIPS'23)
9. Reflexion — https://arxiv.org/abs/2303.11366 (NeurIPS'23)
10. Guo et al. survey — https://www.ijcai.org/proceedings/2024/0890.pdf
11. Tran et al. survey — https://arxiv.org/abs/2501.06322

### 簇 2
1. Lee & See 2004 — https://journals.sagepub.com/doi/10.1518/hfes.46.1.50_30392
2. Endsley 1995 — https://journals.sagepub.com/doi/10.1518/001872095779049543
3. Chen et al. 2018 SAT — https://www.tandfonline.com/doi/full/10.1080/1463922X.2017.1315750
4. Dragan et al. 2013 — DOI 10.1109/HRI.2013.6483603
5. Langley et al. 2017 — IAAI-17, pp. 4762–4763
6. Amershi et al. 2019 — https://dl.acm.org/doi/10.1145/3290605.3300233
7. Wischnewski et al. 2023 — https://dl.acm.org/doi/10.1145/3544548.3581197
8. He et al. 2025 — https://dl.acm.org/doi/10.1145/3706598.3713218
9. Feng et al. 2026 Cocoa — https://dl.acm.org/doi/10.1145/3772318.3791673 / https://arxiv.org/abs/2412.10999
10. Naik et al. 2026 — https://arxiv.org/abs/2606.08323

### 簇 3
1. AgentLens — https://arxiv.org/abs/2402.08995 (TVCG 31, 2025)
2. AGDebugger — https://dl.acm.org/doi/10.1145/3706598.3713581 / https://arxiv.org/abs/2503.02068
3. AI Chains — https://dl.acm.org/doi/10.1145/3491102.3517582
4. PromptChainer — https://dl.acm.org/doi/10.1145/3491101.3519729 (CHI'22 EA)
5. Sensecape — https://dl.acm.org/doi/10.1145/3586183.3606756
6. Graphologue — https://dl.acm.org/doi/abs/10.1145/3586183.3606737
7. AutoGen Studio — https://aclanthology.org/2024.emnlp-demo.8/
8. DiLLS — https://arxiv.org/pdf/2602.05446 (preprint)
9. LangSmith — https://docs.langchain.com/langsmith/observability (灰色文献)
10. Generative Agents — https://dl.acm.org/doi/10.1145/3586183.3606763
11. Project Sid — https://arxiv.org/abs/2411.00114 (preprint)
12. AgentSociety — https://arxiv.org/abs/2502.08691 (preprint)
13. Media Spaces — https://dl.acm.org/doi/abs/10.1145/151233.151235
14. Social Translucence — https://dl.acm.org/doi/10.1145/344949.345004
15. Gather.Town review — RELC Journal 55(1), 2024 (tier-2)

### 簇 4
1. Horvitz 1999 — https://dl.acm.org/doi/10.1145/302979.303030
2. Parasuraman, Sheridan & Wickens 2000 — DOI 10.1109/3468.844354
3. Scerri et al. 2002 — https://jair.org/index.php/jair/article/view/10312
4. Lubars & Tan 2019 — https://arxiv.org/abs/1902.03245 (NeurIPS'19)
5. AGDebugger（同簇3-2）
6. CowPilot — https://aclanthology.org/2025.naacl-demo.17/
7. AutoGen Studio（同簇3-7）
8. AgentCoord — https://arxiv.org/abs/2404.11943 (+ Computers & Graphics 2025)
9. Ask-before-Plan — https://aclanthology.org/2024.findings-emnlp.636/
10. Andukuri et al. 2025 — https://arxiv.org/abs/2410.13788 (ICLR'25)

### 新颖性核查（灰色文献 + 反论）
- Pixel Agents — https://github.com/pixel-agents-hq/pixel-agents ; Fast Company: https://www.fastcompany.com/91497413/
- AgentFleet — https://github.com/DBell-workshop/AgentFleet
- Bit Office — https://www.producthunt.com/products/github-311
- AgentOffice — https://github.com/harishkotra/agent-office
- claude-office — https://github.com/paulrobello/claude-office
- Agent Pixels — https://www.agent-pixels.com/blog/tutorials/agent-pixels-launch-day
- TheAgentCompany — https://arxiv.org/pdf/2412.14161
- VirT-Lab — https://arxiv.org/html/2510.08242v1
- Beyond Anthropomorphism — https://arxiv.org/pdf/2603.04613
- Waytz et al. 2014 — https://www.sciencedirect.com/science/article/abs/pii/S0022103114000067
- de Visser et al. 2016 — https://pubmed.ncbi.nlm.nih.gov/27505048/
- **Carter et al. 2024 (HATEM)** — https://journals.sagepub.com/doi/10.1177/00187208231218156

---

*AI 披露：本扫描由 AI 辅助研究工具（Claude Code + deep-research skill，5 个并行
检索 agent）生成；检索策略与纳入标准如正文所述，所有条目经 URL 级存在性核实。*
