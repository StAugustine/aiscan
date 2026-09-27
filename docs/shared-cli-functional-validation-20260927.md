# 共享 CLI 功能修复验收（2026-09-27）

本轮将 audit 中已发现的同类问题追溯到共享配置、任务输出和生命周期，同时修复 aiscan 与独立 agent。仅做产品功能验收，不评价样本的业务漏洞或审计准确率。此前七轮见 [audit 功能验收](audit-functional-validation-20260927.md)。

## 共享修复

- `pkg/config` 统一任务输入解析、输出格式和超时校验。拒绝 prompt/task-file 冲突、显式空任务和负超时；显式空参数不会退回 stdin 或 REPL。保留各产品的 stdin 与 input-only 策略。
- 配置解析依据命令是否使用模型决定是否解析 LLM profile，工具维护和工具帮助不再被失效模型配置阻塞。公共收尾负责扩展配置、运行参数、数据目录和配置快照，删除 audit 的重复判断。
- `pkg/cli/task.Output` 统一启动错误与任务输出的交接。启动失败返回 JSON/stream-json 错误；任务接管输出后不重复写最终结果。解析失败仍保留已读到的格式选项。
- `pkg/profile.CloseOnce` 统一同步生命周期收尾。取消后的清理使用独立的五秒 deadline，保留原错误和 Close 错误，且只关闭一次。aiscan/agent 在最终结果之前收尾，避免输出成功后才发生未报告的清理失败。
- Console 文本输出保留 writer 错误；aiscan 普通工具输出也使用传入 writer 并返回写入错误。JSON 和 stream-json 的已有错误传播继续有效。
- aiscan 的 `runCLI` 返回错误，进程退出留在外层，正常错误路径能完成 defer 清理。独立 agent 补齐帮助输出、工作目录和 bash timeout 校验，总超时覆盖 profile.Load。
- 节点模式拒绝本地 prompt/input/task-file/resume，防止静默忽略任务。工具帮助先于扫描参数校验；不需要模型的原子工具不解析模型配置。
- 清理重复代码、无用帮助包装函数和导入；更新 full 版本测试中已废弃的 verify low/high 为 off/on。

统一使用 `CYBER_API_KEY`、`CYBER_BASE_URL`、`CYBER_MODEL`、`CYBER_PROVIDER`。三个二进制共享这套配置，不需要各自的模型密钥变量；旧兼容变量继续按原优先级处理。

## 自动验证

环境：Windows amd64、Go 1.26.1、`GOWORK=off`。使用基于已提交源码的隔离 worktree 验证补丁，再与主工作区已有修改合并；暂存区只包含本轮修复与验证记录。

已通过：

- `cmd/aiscan`、`cmd/agent`、`cmd/harness`、`pkg/config`、`pkg/cli/...`、`pkg/console`、`pkg/profile`、`pkg/harness` 包测试。
- audit 独立模块完整 `go test ./...` 与 `go vet ./...`。
- aiscan/agent/config/cli/console/profile 的 `go vet`。
- Console 与 profile 的 `go test -race`。
- 读取 `editions.env` 的 FULL_TAGS、CGO_ENABLED=0，执行 aiscan full 包测试、vet 和构建；独立 agent 与 audit 构建。
- 合回主工作区后，重新执行上述相关主模块包和 audit 全模块测试、vet，并执行 aiscan full 包测试。
- 三个 exe 共 15 个启动错误场景：负超时、冲突任务、显式空 prompt、非法 provider、未知参数。均非零退出且 stdout 为可解析的错误 JSON。
- 回归覆盖 profile Close 失败在最终 JSON 中呈现、关闭一次、取消后收尾、传入 writer 与三种输出格式写入失败、节点本地任务冲突、无效模型配置下的工具帮助。
- `git diff --cached --check`。

## 真实 DeepSeek 复测

使用 `https://api.chainreactors.cn/v1` 和 `deepseek-v4.1-flash`。真实请求经计量转发器发送；子进程仅通过共享 CYBER 环境变量接收本地端点、占位密钥和模型名，未传各二进制专属凭据。转发器不生成或修补模型响应，实际密钥不写入源码、配置或报告。

| 入口 | 功能范围 | API 请求（含启动探针） | 耗时 | 结果 |
| --- | --- | ---: | ---: | --- |
| aiscan agent | 读取 EVALUATION_SCOPE.md 和 package.json，返回包名、版本和验收标记 | 3 | 8.516 秒 | 退出 0，JSON is_error=false，标记正确 |
| cyber-audit | 文件访问、rg/AST、OSV/proton 调用、报告写入与校验 | 14 | 106.344 秒 | 退出 0，报告 completed，22 次工具调用均有返回 |

样本继续使用 OWASP/NodeGoat 固定快照 `c5cb68a7084e4ae7dcc60e6a98768720a81841e8`。37 个样本源文件哈希均未改变；audit 的 findings.json 为空。四项工具 coverage 均 completed，最终 audit 构建离线重验报告通过。未启动样本服务或开展业务漏洞分析。

两个入口的正式任务 token 用量均逐项匹配转发器计量；启动探针用量单独排除。验证记录位于本地 `.tmp/shared-cli-fixes/verification.json`、`smoke.json` 和 `runs/{aiscan,audit}/`；exe 和原始运行产物不提交。

本轮验证限于 Windows 功能与输出契约，未验收 Linux/macOS 发布产物、模型漏洞发现率或长任务稳定性。未推送或发布变更。

## PR 集成复核

本轮修复迁入基于远端 master `67e0f4e9` 的独立分支 `fix/audit-cli-functional`。保留每轮提交，解决 modes.go 与 scanner_mode.go 的冲突，沿用 master 已有的参数解析与事件订阅接口。未迁入其他 JEV/recap 功能或工作区未提交改动。

集成分支再次通过受影响包测试、audit 全模块 race/vet/构建、根模块 go vet 与 golangci-lint（0 issues）、full aiscan/scanner 测试与构建、架构检查、Console/profile/eventbus/telemetry race；三个 Go 模块执行 tidy 后无差异。

浏览器集成测试覆盖静态页面、工具帮助输出、AOP 会话流式回复，3 项均通过。cyber-ui 已整合本地 `4776841` 的连接状态、请求 deadline 和内联历史展示修复；新增 4 项 AOP 客户端回归，viewer 共 12 项测试通过，相关 5 个包的类型检查与构建通过。theme/markdown 补齐独立类型检查所需的开发依赖。

cyber-ui 的实际主分支名为 main，合入提交为 `9872741`，本 PR 固定到该提交。其推送采用普通快进更新；主仓库通过独立 PR 交付，不直接更新 master。云端跨平台与发布构建检查以 PR CI 为准。
