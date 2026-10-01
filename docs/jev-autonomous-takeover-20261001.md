# JEV 自动接管及真实验收（2026-10-01）

普通 LLM 的实际交互用于发现能力和生成运行时 Observe；已有场景由 JEV 优先接管执行。JEV 判断是否需要 LLM、是否编译或修复、选择哪个 Reflex，以及下一动作、读取、完成或交接。JEV 完成执行后，LLM 只整理最终回答。

## 实现边界

- 场景只有 `When / Decide / Observe`。Observe 使用纯 JavaScript，把实际输入和工具结果变成状态及有限原生调用绑定；外部读取通过普通 Executor 执行。
- 移除 CommandRegistry、curl 和 Playwright 的专用观察回调、候选 ID 及执行分支。JEV 生产代码不导入具体工具，不按工具名分支。Playwright 只补充已有 selector 和 evaluate 语法文档。
- `{state,candidates}` 的完整原生绑定可由运行时生成的普通读取程序提供。调用和结果按 call_id 关联，JSON 信封在私有投影预算前归一化，主模型已提交前缀保持不变。
- 所有效果和读取走已有工具准入。绑定必须显式声明 `read`；效果后的实际检查、跨交接去重、失败场景撤下和修复债务由通用控制器维护。
- 编译在任务完成后进行，有界草稿通过纯程序试算和 JEV 有限复审。修复必要性由 JEV 比较交接时的缺口与普通 LLM 后来的实际补充，不因能力已经存在而忽略缺绑定。
- 场景库使用 version 3，旧库完整备份后保留 Claim 供重新编译；运行时只执行 JavaScript。资源预算及限制见[实现契约](jev-implementation-20260927.md)。

## 真实验收

使用真实 JEV 和 `https://api.chainreactors.cn/v1` 的 `deepseek-v4.1-flash`，后台 reasoning effort 为 `low`。两次独立运行均从空库开始，没有预置场景、固定生成器响应或工具观察适配。凭据仅通过进程环境注入。

`TestBrowserAutomaticTakeoverAfterBoundedDiscovery` 包含 3 个发现任务及 5 个新验证页面，改变 URL、随机 ID、目标和干扰标签、元素类型，并覆盖无 ID 元素。服务端要求正确效果恰好一次、错误效果零；修改页面属性也计为错误效果。验证页要求 execution 日志中有实际 JEV 打开、点击及成功原生 receipt，主 LLM 各一次最终回答且无工具调用，场景及已提交请求前缀不变。

| 独立运行 | 库版本 | 结果 | 耗时 | 验证页 |
| --- | --- | --- | --- | --- |
| 原控制链 | 2 | PASS | 103.15 秒 | 5/5 完整接管 |
| 纯 JS 与 JEV 修复判别合并后 | 3 | PASS | 143.19 秒 | 5/5 完整接管 |

两次均在第一项普通任务后自动形成场景，后续七项由 JEV 完成执行。逐页脱敏指标、用量及原报告 SHA-256 保存在[验收摘要](validation/jev-browser-takeover-20261001.json)。摘要不包含原始会话、生成程序、凭据或本机绝对路径；原始报告和执行日志保留在原工作区 `.runlogs`，未作为 PR 附件提交。

开发期间的旧版本曾因入口遗漏、缺效果绑定、读取协议不匹配、提前报告及普通模型误操作失败，部分实验超时或中断。旧版本成功及未完成报告不计入上述验收。这里证明的是所定义浏览器场景的独立功能通过，不证明所有开放任务都可封闭，也不宣称全链路 token、费用或延迟优于纯 LLM。

## 复现

设置 `TYPESAFE_API_KEY`、`CYBER_API_KEY`、`CYBER_BASE_URL` 和 `CYBER_MODEL` 后运行：

```powershell
$env:JEV_BROWSER_LIVE='1'
$env:JEV_DECLARATION_EFFORT='low'
$env:JEV_BROWSER_REPORT='<新的报告路径>'
go test -tags full,sqlite ./pkg/exts/jev -run '^TestBrowserAutomaticTakeoverAfterBoundedDiscovery$' -count=1 -timeout=20m -v
```
