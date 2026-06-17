# Changelog

## v0.2.2 (2026-06-16)

新增 goal evaluation 闭环机制——独立 LLM 评估 agent 任务完成度并自动注入反馈驱动重试；内嵌 katana 爬虫引擎支持 headless 浏览器；新增多 provider 容错降级链；重构 TUI/REPL 为统一 pkg/tui 模块；大幅整理包结构，aiscan 专用包从 pkg/ 移入 core/。

### New Features

**goal evaluation — 独立评估 + 反馈重试闭环（核心）**

- 新增 `-e` / `--eval` 指定目标评估标准，`--eval-model` 可选独立评估模型，`--eval-retries` 控制最大评估轮数（默认 3）
- 评估机制：agent 完成一轮执行后，独立 evaluator LLM 接收压缩后的 execution trace（tool call 序列 + assistant 摘要 + final output），通过强制 tool call（verdict tool）返回结构化判定（pass/reason/feedback）
- 闭环重试：verdict.pass=false 时，evaluator 的 feedback 作为新 prompt 注入 agent 继续执行，直到 pass=true 或达到最大评估轮数
- evaluator 调用失败时降级为通用反馈（"请检查你的工作并继续"），不中断主流程
- trace 压缩策略：仅保留 tool call 序列和 assistant 摘要，不传完整 tool result，最大 16KB 防止 context 膨胀
- 全程通过 eventbus 发射 `GoalEvalStart` / `GoalEvalEnd` / `GoalEvalError` 事件，TUI 实时展示评估进度和结果

**katana — 进程内爬虫 + headless 引擎**

- 将 katana 从外部二进制调用重构为进程内 SDK 集成，通过 goflags 解析参数保持完整 CLI 兼容性，OnResult 回调收集结果
- 新增 headless/hybrid 引擎支持，根据 `-hl`/`-hh`/`-cwu` 标志自动选择引擎

**multi-provider — 容错降级链**

- 当主 provider 重试耗尽后，agent loop 自动切换到降级链中的下一个 provider 并重放当前 turn
- 配置文件 `llm.providers` 数组定义降级链，启动时并行初始化（失败跳过）
- 新增 REPL `/provider` 命令展示 provider 链的 active/standby 状态

**agent — finish tool / thinking block / web search**

- 新增 finish tool：通过 `ToolResult.Terminate` 显式终止 agent loop
- 非流式响应支持解析 Anthropic thinking block 为 `ReasoningContent`
- 新增 `WebSearchProvider` 接口，Anthropic 走 `web_search_20250305` server tool，OpenAI 走 Responses API；provider 原生搜索失败时回退 Tavily/DDG

**heartbeat + tmux 增量监控**

- `--heartbeat` 接入 LoopScheduler 作为通用周期唤醒
- tmux 后台命令自动推送增量输出到 agent inbox（每 10s per-session goroutine）
- `capture-pane` 新增 `-n`（末尾 N 行）和 `-c`（末尾 N 字节）参数

**信号处理 — 两阶段 Ctrl+C**

- 第一次 Ctrl+C 停止当前任务，第二次退出 REPL，第三次强制退出

### Bug Fixes

- **scanner CLI**: `aiscan scan` / `aiscan gogo` 等直接命令模式因引擎异步加载导致 "unknown subcommand" 失败。新增 `WaitEngines(ctx)` 同步等待引擎就绪

### Refactoring

- `pkg/app` 合并进 `core/runner`，删除 `pkg/app`
- `eventbus`、`pidlock`、`resources`、`output`、`harness` 从 `pkg/` 移入 `core/`
- TUI/REPL 提取到 `pkg/tui`，合并 `pkg/repl`
- evaluator 使用 tool call 结构化输出替代 JSON text fallback
- cyberhub 基于 SDK association index 重建，新增结构化查询 flag
- provider 层简化：移除中间结构体，提取共享 HTTP 工具

### Dependencies

- SDK `v0.2.4` → `v0.3.2`
- 新增 SDK panic recovery
- 42 个 e2e 测试

---

## v0.2.1 — IOA 集成重构 + AI 驱动监听 (2026-06-09)

适配 IOA v0.1.0 的统一架构。核心变更：多 Agent 协作从自动推送切换为 AI 主动监听。

### Breaking Changes

- `--ai` 标志移除 — 使用 `--verify=high --sniper` 替代
- IOA build tag 移除 — SQLite、MCP、Auth 始终内置

### IOA 协作

- AI 驱动的实时监听替代 push-to-inbox
- ioa_read 新增 `--direction` 参数（upstream/downstream）
- IOA 内置 Server：`--ioa-db` 持久化，MCP endpoint 始终可用
- ioa_send 新增 `--content_type` 参数

### Skill 更新

- ioa/SKILL.md — 新增 Background Monitoring 段落、`--direction` 过滤文档
- ioa/swarm.md — 工作阶段从轮询改为 tmux peek

### 文档

- README、usage.md、quickstart.md、configuration.md 全面更新

---

## v0.2.0 — Playwright 浏览器引擎 + Agent/Skill/Pipeline 全面重构 (2026-06-08)

架构级大版本更新 (148 commits)。核心引入 Playwright 浏览器引擎、TMux 交互式终端、Proxy 代理管理、Passive Recon、Search 搜索等新工具模块，同时对 Agent / Tool / Skill / Scan Pipeline 四大子系统进行全面重构。

### Breaking Changes

- `browser` 和 `recon` build tag 合并为单一 `full` tag
- `ioa` 独立二进制移除，通过 `aiscan ioa` 子命令访问
- 每个平台仅产出 `aiscan`（基础版）和 `aiscan-full`

### Tool 更新

- **Playwright** — 22 个命令，Session Recorder 生成 nuclei headless 模板，完整兼容 nuclei headless 协议
- **TMux** — 统一 bash/tmux 执行层 + task manager，完整 PTY 支持
- **Proxy** — Clash 订阅解析，trojan/vless/anytls/hy2/ss 多协议，代理池管理
- **Passive Recon** — 集成 uncover，支持 FOFA/Hunter
- **Search** — WebSearch (Tavily)、WebFetch、CyberhubSearch、Multimodal vision

### Agent 更新

- 统一 Agent 抽象，SubAgent 三模式，模板化 Prompt
- 统一 EventBus，Per-turn Token 可观测性，LLM Prompt Cache

### Scan Pipeline 更新

- 基于订阅的 DAG Pipeline，统一 AI Skill 插件架构
- Loot 类型统一，`-f` JSONL 输出，Katana crawl 集成

### IOA & Swarm 更新

- `protocols/` 动态协议注册，Checkpoint 同步至 IOA Space
- Swarm 多节点协作调度增强

---

## v0.1.2 (2026-06-08)

- fix cli scanner flag isolation
- feat: add `--proxy` for scanner tools and `--llm-proxy` for LLM API

## v0.1.1 (2026-06-08)

- fix: resolve remaining CI test failures

## v0.1.0 (2026-06-08)

- refactor: unify capability pipeline, remove registry abstraction
- refactor: migrate pkg/acp to standalone github.com/chainreactors/ioa
- feat: agent loop resilience, capacity-driven concurrency, verification enhancement
- feat: add console agent REPL
- feat: add config.yaml system and build script
- feat: ACP CLI query subcommands and enhanced space tool
