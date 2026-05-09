# aiscan 使用文档

本文档面向 `v0.0.1` 及后续 GitHub Release 发布的二进制版本。

aiscan 是一个 agentic security scanner：它既可以像普通 CLI 一样直接运行扫描器，也可以让 LLM agent 根据自然语言目标选择工具、执行扫描、读取证据并输出结论。请只在明确授权的目标上使用。

## 快速开始

下载最新正式版本：

```text
https://github.com/chainreactors/aiscan/releases/latest
```

`v0.0.1` 发布地址：

```text
https://github.com/chainreactors/aiscan/releases/tag/v0.0.1
```

选择对应平台的资产：

| 系统 | 架构 | 文件 |
| --- | --- | --- |
| Linux | amd64 | `aiscan_linux_amd64` |
| Linux | arm64 | `aiscan_linux_arm64` |
| macOS | Intel | `aiscan_darwin_amd64` |
| macOS | Apple Silicon | `aiscan_darwin_arm64` |
| Windows | amd64 | `aiscan_windows_amd64.exe` |

Linux amd64 安装示例：

```bash
curl -L -o aiscan https://github.com/chainreactors/aiscan/releases/download/v0.0.1/aiscan_linux_amd64
curl -L -o aiscan_checksums.txt https://github.com/chainreactors/aiscan/releases/download/v0.0.1/aiscan_checksums.txt
sha256sum -c aiscan_checksums.txt --ignore-missing
chmod +x aiscan
sudo mv aiscan /usr/local/bin/aiscan
aiscan --version
```

macOS Apple Silicon 安装示例：

```bash
curl -L -o aiscan https://github.com/chainreactors/aiscan/releases/download/v0.0.1/aiscan_darwin_arm64
chmod +x aiscan
xattr -d com.apple.quarantine aiscan 2>/dev/null || true
sudo mv aiscan /usr/local/bin/aiscan
aiscan --version
```

Windows PowerShell 示例：

```powershell
$Version = "v0.0.1"
$Base = "https://github.com/chainreactors/aiscan/releases/download/$Version"
Invoke-WebRequest "$Base/aiscan_windows_amd64.exe" -OutFile aiscan.exe
Invoke-WebRequest "$Base/aiscan_checksums.txt" -OutFile aiscan_checksums.txt
Get-FileHash .\aiscan.exe -Algorithm SHA256
.\aiscan.exe --version
```

## 命令结构

基本形式：

```bash
aiscan [全局参数] <subcommand> [子命令参数]
```

查看帮助：

```bash
aiscan -h
aiscan scan -h
aiscan neutron -h
```

主要 subcommand：

| 命令 | 类型 | 功能 |
| --- | --- | --- |
| `agent` | agentic | 运行 LLM agent；无任务输入时进入交互式 CLI，`--loop` 时作为 ACP worker 挂起监听 |
| `scan` | deterministic pipeline | 自动扫描流水线，串联发现、Web 探测、弱口令、POC 和可选 AI 验证 |
| `gogo` | scanner | 主机存活、端口、服务、banner 和指纹发现 |
| `spray` | scanner | Web 探测、HTTP 指纹、常见文件、爬取和路径检查 |
| `zombie` | scanner | 授权弱口令检测 |
| `neutron` | scanner | 模板化 POC 检测 |
| `acp serve` | service | 启动 ACP HTTP server，用于多 agent/worker 协作 |

常用全局参数：

| 参数 | 说明 |
| --- | --- |
| `--llm-provider` | LLM provider 名称，例如 `openai`、`deepseek`、`openrouter`、`ollama` |
| `--llm-base-url` | LLM API base URL |
| `--llm-api-key` | LLM API key |
| `--llm-model` | 模型名称 |
| `--llm-proxy` | 访问 LLM API 的 HTTP proxy |
| `--ai` | 直接 scanner 输出后，用 LLM 按相关 skill 再分析一次 |
| `--cyberhub-url` | Cyberhub 资源服务 URL |
| `--cyberhub-key` | Cyberhub API key |
| `--cyberhub-mode` | Cyberhub 资源模式：`merge` 或 `override` |
| `--debug` | 输出调试日志 |
| `-q, --quiet` | 减少日志输出 |
| `--no-color` | 禁用扫描输出颜色 |
| `--timeout` | 整体超时时间，单位秒 |

注意：顶层参数和 scanner 参数可能同名。例如 `aiscan agent -p` 是自然语言 prompt，`aiscan gogo -p` 是端口参数。

## LLM Provider

`agent`、`agent --loop`、`scan --verify` 和 scanner 的 `--ai` 模式需要 LLM provider。

默认 provider 是 `openai`，默认模型是 `gpt-4o`。可以通过参数或环境变量配置 API key。

| Provider | 默认 Base URL | API Key 环境变量 |
| --- | --- | --- |
| `openai` | `https://api.openai.com/v1` | `OPENAI_API_KEY` |
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| `deepseek` | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` |
| `groq` | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` |
| `moonshot` | `https://api.moonshot.cn/v1` | `MOONSHOT_API_KEY` |
| `anthropic` | `https://api.anthropic.com/v1` | `ANTHROPIC_API_KEY` |
| `ollama` | `http://localhost:11434/v1` | 不需要 |

也可以统一设置：

```bash
export AISCAN_API_KEY="..."
```

OpenAI 示例：

```bash
export OPENAI_API_KEY="sk-..."
aiscan agent --llm-model gpt-4o -p "发现 Web 服务并检查高风险漏洞" -i 192.168.1.0/24
```

DeepSeek 示例：

```bash
export DEEPSEEK_API_KEY="..."
aiscan agent --llm-provider deepseek --llm-model deepseek-chat -p "枚举服务并输出风险摘要" -i 10.0.0.0/24
```

Ollama 示例：

```bash
ollama run llama3
aiscan agent --llm-provider ollama --llm-model llama3 --llm-base-url http://localhost:11434/v1 -p "检查这个站点" -i http://target.example
```

如果 API 需要代理：

```bash
aiscan agent --llm-proxy http://127.0.0.1:7890 -p "检查目标暴露面" -i http://target.example
```

## scan：自动扫描流水线

`scan` 是最常用的 deterministic 自动扫描入口。它不依赖 LLM 也能运行，适合批量资产发现、Web 探测、弱口令检查和 POC 初筛。

基本用法：

```bash
aiscan scan -i <target> [options]
```

输入可以是 URL、IP、IP:port、CIDR，也可以用文件：

```bash
aiscan scan -i 127.0.0.1 --mode quick
aiscan scan -i http://target.example --mode quick
aiscan scan -i 192.168.1.0/24 --mode full
aiscan scan -l targets.txt --mode full
```

扫描流程：

```text
输入目标 -> gogo 发现服务 -> spray 探测 Web -> zombie 弱口令 -> neutron POC -> 可选 agent_verify
```

`scan` 的能力会通过事件队列串联。例如 gogo 发现 HTTP 服务后，Web 目标会进入 spray；spray 识别到指纹后，相关指纹会用于 neutron 选择 POC；弱口令和 POC 发现可以再进入 AI 验证。

### quick 和 full

| 模式 | 说明 |
| --- | --- |
| `quick` | 默认模式。包含端口发现、Web 探测、指纹、常见路径、备份文件、主动 Web 检查、弱口令和基于指纹的 POC |
| `full` | 在 quick 基础上增加更深的 crawl 和 spray brute 默认字典探测，耗时更长 |

示例：

```bash
aiscan scan -i 10.0.0.0/24 --mode quick
aiscan scan -i 10.0.0.0/24 --mode full
```

### 发现和探测参数

指定端口集合：

```bash
aiscan scan -i 192.168.1.0/24 --port top100
aiscan scan -i 192.168.1.0/24 --ports 80,443,8080
```

控制并发和超时：

```bash
aiscan scan -i 192.168.1.0/24 --threads 300 --timeout 5
aiscan scan -i http://target.example --spray-threads 20
```

启用自定义 Web 字典和规则：

```bash
aiscan scan -i http://target.example --dict paths.txt --rule rules.txt
aiscan scan -i http://target.example --default-dict
```

### 弱口令和 POC

弱口令检测：

```bash
aiscan scan -i 127.0.0.1 --user admin --pwd admin123
aiscan scan -l targets.txt --mode full --zombie-top 5
aiscan scan -i 10.0.0.0/24 --zombie-threads 50
```

POC 检测默认基于指纹选择模板。没有指纹时也要运行 POC，可以开启 broad POC：

```bash
aiscan scan -i http://target.example --broad-poc
```

限制每个指纹最多使用的 neutron 模板数量：

```bash
aiscan scan -i http://target.example --max-neutron-per-finger 10
```

### 输出格式

终端友好输出：

```bash
aiscan scan -i 127.0.0.1 --mode quick
```

JSON Lines 输出：

```bash
aiscan scan -i 127.0.0.1 --mode quick -j
```

Markdown 报告：

```bash
aiscan scan -i 127.0.0.1 --mode quick --report
```

写入文件：

```bash
aiscan scan -i 127.0.0.1 --mode quick -f result.txt
```

禁用颜色和打开调试：

```bash
aiscan scan -i 127.0.0.1 --no-color
aiscan scan -i 127.0.0.1 --mode quick --debug
```

### AI 验证

`scan --verify=<priority>` 会启用 `agent_verify`，只对达到指定优先级的发现进行 LLM 验证。

可选值：

```text
off, low, medium, high, critical
```

示例：

```bash
aiscan scan -i http://target.example --mode quick --verify=high --llm-api-key "$OPENAI_API_KEY" --llm-model gpt-4o
aiscan scan -i http://target.example --mode full --verify=critical --verify-turns 2 --verify-timeout 90
```

如果没有显式传 `--verify`，aiscan 的默认策略是尝试启用高优先级验证；当 LLM provider 未配置时，验证会被跳过，扫描主体仍可运行。

## agent：自然语言扫描代理

`agent` 是 aiscan 的 agentic 模式。它会构造系统提示词，加载内置工具、扫描器使用说明和 skills，然后由 LLM 在多轮循环中选择工具、执行命令、读取证据并输出最终报告。

直接运行 `aiscan agent` 且不提供 `-p`、`--task-file`、stdin pipe 或 `-i` 时，会进入交互式 CLI。REPL 基于 console/readline，支持命令历史和补全；会话上下文会保留，适合连续追问、调整扫描目标或让 agent 继续分析已有结果。

```bash
aiscan agent --llm-model gpt-4o
```

交互式 CLI 支持：

| 命令 | 说明 |
| --- | --- |
| `/help` | 显示交互命令 |
| `/reset` | 清空当前会话上下文 |
| `/continue` | 不追加新 prompt，让 agent 尝试继续当前上下文 |
| `/exit`, `/quit` | 退出交互式 CLI |
| `/<skill-name> ...` | 直接调用内置 skill，例如 `/scan 检查这个网段`、`/gogo 枚举端口` |

内置 skill 会自动注册为 REPL 命令，类似 Claude Code 的斜杠命令体验。当前包括 `/aiscan`、`/scan`、`/gogo`、`/spray`、`/zombie`、`/neutron`。这些命令会把对应 skill 内容和命令后的自然语言参数一起注入当前 agent 会话。旧形式 `/skill:<name> ...` 仍然兼容。

基本形式：

```bash
aiscan agent -p "<任务描述>" -i <target>
```

示例：

```bash
aiscan agent -p "发现 Web 服务并检查高风险漏洞，最后给出可复现证据" -i 192.168.1.0/24
```

多个输入：

```bash
aiscan agent -p "枚举服务并输出风险摘要" -i 10.0.0.10 -i http://10.0.0.20
```

从文件读取任务描述：

```bash
aiscan agent --task-file task.md -i 192.168.1.0/24
```

控制 agent 运行边界：

```bash
aiscan agent -p "检查目标暴露面" -i 10.0.0.0/24 --max-turns 20 --timeout 1800
```

使用特定 skill：

```bash
aiscan agent -s scan -s neutron -p "先做快速扫描，再分析高危 POC 命中" -i http://target.example
```

`agent` 适合：

- 任务描述不完全确定，需要 agent 自己选择扫描路径；
- 需要把多个扫描器结果串起来解释；
- 需要生成面向人的摘要、复现步骤或后续建议；
- 需要接入 ACP，把远程 worker 或协作空间作为工具使用。

不适合：

- 大范围无约束扫描；
- 对时间和输出格式要求严格的批处理；
- 没有 LLM provider 的环境。

## agentic scanner 模式：`--ai`

直接 scanner 命令加 `--ai` 时，aiscan 会先执行 scanner，再让 LLM 根据该 scanner 的 skill 解释结果。

示例：

```bash
aiscan --ai -p "只提取高风险暴露面，并给出证据" gogo -i 192.168.1.0/24 -p top100
aiscan --ai -p "判断这些 Web 指纹是否值得进一步验证" spray -u http://target.example --finger
aiscan --ai -p "解释命中的 POC 影响和复现条件" neutron -u http://target.example -s critical,high
```

对 `scan` 使用 AI 验证时，优先使用 `--verify`：

```bash
aiscan scan -i http://target.example --verify=high --llm-model gpt-4o
```

`--ai` 更适合对 scanner 输出做总结、解释和筛选；`scan --verify` 更适合对发现进行自动化证据验证。

## gogo：服务发现

`gogo` 用于主机、端口、服务、banner 和指纹发现。

常见用法：

```bash
aiscan gogo -i 192.168.1.0/24 -p top100
aiscan gogo -i 10.0.0.10 -p 80,443,8080
aiscan gogo -i targets.txt -p all
```

输出可作为后续 `spray`、`zombie`、`neutron` 的输入线索。对于多数任务，优先使用 `scan` 自动串联，而不是手动拆分。

## spray：Web 探测和指纹

`spray` 用于 Web 目标探测、HTTP 指纹、常见文件、路径和 crawl。

常见用法：

```bash
aiscan spray -u http://target.example
aiscan spray -u http://target.example --finger
aiscan spray -l urls.txt --finger
```

aiscan 包装的 spray 默认会附加非交互参数，避免进度条影响 agent 输出。

## zombie：弱口令检测

`zombie` 用于授权弱口令检测。

常见用法：

```bash
aiscan zombie -i ssh://127.0.0.1:22 --top 3
aiscan zombie -i ssh://admin@127.0.0.1:22 -p admin123
aiscan zombie -l services.txt --top 10
```

注意 `zombie -p` 是密码参数，不是 agent prompt。

## neutron：POC 检测

`neutron` 用于模板化 POC 执行，支持按 ID、tag、severity、fingerprint 和模板路径过滤。

常见用法：

```bash
aiscan neutron -u http://target.example -s critical,high
aiscan neutron -u http://target.example --finger nginx --max-per-finger 20
aiscan neutron -l targets.txt --tags cve,rce -c 10 --rate-limit 20
aiscan neutron -u http://target.example -t ./pocs --id shiro-detect -j -o findings.jsonl
aiscan neutron -u http://target.example --template-list
```

常用参数：

| 参数 | 说明 |
| --- | --- |
| `-u, --target` | URL、host 或 ip:port，可重复 |
| `-i, --input` | target 别名 |
| `-l, --list` | 目标文件 |
| `-t, --templates` | 自定义模板文件或目录 |
| `--id` | 按模板 ID 执行 |
| `--finger` | 按指纹过滤模板 |
| `--tags, --tag` | 按 tag 过滤模板 |
| `-s, --severity` | 按严重性过滤 |
| `-c, --concurrency` | 模板并发 |
| `--rate-limit` | 每秒执行上限 |
| `-j, --json` | JSON Lines 输出 |
| `-o, --output` | 写入文件 |

## ACP：服务和协作模式

ACP 是 aiscan 的协作层。`acp serve` 启动本地 HTTP server 和 SQLite store；`agent --loop` 连接到 server，在指定 space 中注册 worker、发布自身能力，并监听任务消息。

### 启动 ACP server

默认监听 `http://127.0.0.1:8765`，数据库为 `./acp.db`：

```bash
aiscan acp serve
```

指定地址和数据库：

```bash
aiscan acp serve --acp-url http://127.0.0.1:8765 --acp-db ./acp.db
```

常用参数：

| 参数 | 说明 |
| --- | --- |
| `--acp-url` | ACP server listen URL |
| `--acp-db` | SQLite 数据库路径 |
| `--timeout` | server 运行总超时 |
| `--debug` | 调试日志 |
| `--quiet` | 减少日志 |

### 启动 loop worker

`agent --loop` 需要 LLM provider。它会连接 ACP server，注册节点，进入指定 space，监听任务并执行 agentic 工作流。

```bash
aiscan agent --loop --acp-url http://127.0.0.1:8765 --space case-1 --llm-model gpt-4o
```

带初始 intent：

```bash
aiscan agent --loop --acp-url http://127.0.0.1:8765 --space case-1 -p "负责内网 Web 资产扫描和漏洞验证" -s aiscan -s scan
```

指定节点名：

```bash
aiscan agent --loop --acp-url http://127.0.0.1:8765 --space case-1 --acp-node-name web-scanner-1
```

### agent 接入 ACP 工具

`agent` 传入 `--acp-url` 后，会向 ACP server 注册节点和工具，让 agent 能使用 ACP 相关工具。

```bash
aiscan agent --acp-url http://127.0.0.1:8765 -p "在 case-1 中协调扫描任务" -i http://target.example
```

ACP 适合：

- 多个 aiscan worker 长期运行；
- 把任务按 space 组织；
- agent 和 worker 通过 server 协作；
- 需要持久化消息和节点状态。

## Cyberhub 资源

aiscan 可以从 Cyberhub 加载指纹、模板等扫描资源。

```bash
aiscan scan -i http://target.example --cyberhub-url http://127.0.0.1:9000 --cyberhub-key "$CYBERHUB_KEY"
```

资源模式：

| 模式 | 说明 |
| --- | --- |
| `merge` | 默认。合并内置资源和 Cyberhub 资源 |
| `override` | 使用 Cyberhub 资源覆盖内置资源 |

## 输出选择建议

| 目标 | 推荐命令 |
| --- | --- |
| 快速资产发现和风险初筛 | `aiscan scan -i <target> --mode quick` |
| 深入扫描和更多 Web 路径 | `aiscan scan -i <target> --mode full` |
| 自动解释结果和生成结论 | `aiscan agent -p "<任务>" -i <target>` |
| 对确定 scanner 输出做 AI 摘要 | `aiscan --ai -p "<意图>" <scanner> ...` |
| 机器读取结果 | `aiscan scan -i <target> -j` |
| 人读报告 | `aiscan scan -i <target> --report` |
| 多 worker 协作 | `aiscan acp serve` + `aiscan agent --loop` |

## 常见问题

### `agent` 报 provider 未配置

`agent` 必须有可用 LLM provider。设置对应环境变量或显式传入 `--llm-api-key`。

```bash
export OPENAI_API_KEY="sk-..."
aiscan agent --llm-model gpt-4o -p "检查目标" -i http://target.example
```

### `scan --verify` 没有产生 AI 验证

检查是否配置了 LLM provider，并确认发现的风险优先级达到了 `--verify` 指定阈值。

```bash
aiscan scan -i http://target.example --verify=low --llm-api-key "$OPENAI_API_KEY"
```

### 输出太多或包含颜色

使用文件输出或关闭颜色：

```bash
aiscan scan -i 127.0.0.1 -f result.txt --no-color
```

### 扫描太慢

降低范围，减少端口和字典，或使用 quick 模式：

```bash
aiscan scan -i 192.168.1.0/24 --mode quick --port top100 --threads 200
```

### 需要固定版本

使用 GitHub Release 中的正式版本 URL，不要依赖 nightly：

```text
https://github.com/chainreactors/aiscan/releases/tag/v0.0.1
```

Nightly 构建只用于验证最新主分支。
