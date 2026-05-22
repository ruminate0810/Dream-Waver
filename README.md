# Dream-Waver

> 一个开源的 Multi-Agent 平台 — Manus / Genspark 类产品的 Go + Rust 实现。
> MVP 以 **AI PPT 生成** 为首个能力，长期目标是通用 Agent 平台 (Slides / Sheets / Docs / Code / Video)。

## 设计目标

- **Multi-Agent 架构**：Planner → Researcher → Worker，配合 ReAct 工具调用循环
- **生产可上线**：SaaS 化（多租户、Credit 计费、可观测、可扩展）
- **性能 & 安全**：Go orchestrator 负责并发编排，Rust 沙箱负责隔离执行用户/Agent 代码
- **HTML-first PPT**：Tailwind 模板 + chromedp 渲染 + unioffice 组装 PPTX；用户拿到的是可编辑的真 PPTX
- **多 LLM 可切换**：默认走 DeepSeek v4-pro（OpenAI 兼容 API），一行 env 切换 Claude / OpenAI

## 仓库结构

```
services/
  orchestrator/   # Go：主服务（API、Agent、LLM、PPT 工具）
  sandbox/        # Rust：gRPC 沙箱（wasmtime 隔离执行代码工具）
apps/
  web/            # Next.js 15：用户界面
packages/
  slide-templates/  # HTML+Tailwind 模板库
proto/            # 共享 gRPC schema
infra/            # Docker / fly.io / terraform
docs/             # 架构与开发文档
```

## Quickstart

```bash
# 1. 复制环境变量
cp .env.example .env
# 编辑 .env，至少填 DEEPSEEK_API_KEY（默认 primary 是 DeepSeek，
# 想换 Claude 把 LLM_PRIMARY_PROVIDER=anthropic 并填 ANTHROPIC_API_KEY 即可）

# 2. 启动开发环境
make dev    # 等同 docker-compose up --build

# 3. 打开浏览器
open http://localhost:3000
```

依赖（本地裸跑而非 docker）：
- Go 1.23+
- Rust 1.80+
- Node 20+
- Postgres 16、Redis 7、MinIO（或用 docker-compose 起）
- protoc + protoc-gen-go + protoc-gen-go-grpc + tonic（用于生成 gRPC 代码）

## 路线图

详见 [`/Users/sheng/.claude/plans/github-manus-genspark-ppt-jaunty-owl.md`](.claude-plan)。

- **Week 1**：仓库骨架 + Agent 抽象 + Rust 沙箱 hello
- **Week 2**：PPT 端到端跑通（背景图模式）
- **Week 3**：双层 PPTX（可编辑文本）+ 模板扩到 5 套
- **Week 4**：联网研究 + 多模型路由 + Claude prompt caching
- **Week 5**：SaaS 化（Stripe + Credit-wallet + 用量监控）
- **Week 6**：上线 Fly.io / Vercel + Beta 邀请

## 借鉴的开源项目

仅借鉴机制，未 fork 代码：
- [OpenManus](https://github.com/FoundationAgents/OpenManus) — Agent 三层抽象 (Base → ReAct → ToolCall)
- [Presenton](https://github.com/presenton/presenton) — HTML+Tailwind → PPTX 思路
- [PPTAgent](https://github.com/icip-cas/PPTAgent) — 反思式生成 + PPTEval 评估

## License

MIT
