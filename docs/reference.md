# CLI 参考

本文档是 aiscan 的命令行参考手册，涵盖命令结构、全局参数、各扫描器快速参考、cyberhub 资源查询、场景建议和常见问题。

## 命令结构

```text
aiscan [全局参数] <subcommand> [子命令参数]
```

| 命令 | 类型 | 功能 |
| --- | --- | --- |
| `agent` | agentic | LLM agent；无任务输入时进入交互式 REPL，`--loop` 时作为 IOA worker |
| `scan` | pipeline | 自动流水线：gogo → spray → zombie → neutron，可选 AI 验证/sniper/deep |
| `gogo` | scanner | 主机存活、端口、服务、banner 和指纹发现 |
| `spray` | scanner | Web 探测、HTTP 指纹、常见文件、爬取和路径检查 |
| `zombie` | scanner | 授权弱口令检测 |
| `neutron` | scanner | 模板化 POC 检测 |
| `katana` | scanner | Web 爬虫（仅 full 版） |
| `passive` | scanner | 网络空间搜索 FOFA/Hunter（仅 full 版） |
| `cyberhub` | query | 查询已加载的指纹和 POC 模板 |
| `ioa serve` | service | 启动 IOA HTTP server |
| `ioa spaces/messages/context/nodes` | query | IOA 查询 |

查看帮助：`aiscan -h`、`aiscan scan -h`、`aiscan neutron -h`

## 全局参数

全局参数建议放在子命令之前。只有 `scan` 支持在命令之后继续写全局参数并自动提取；其他 scanner 后面的参数原样传给对应引擎，避免短参数冲突。

### LLM 参数

| 参数 | 说明 |
| --- | --- |
| `--provider` | LLM provider 名称（openai、deepseek、openrouter、ollama 等） |
| `--base-url` | LLM API base URL |
| `--api-key` | LLM API key（也可用环境变量 `OPENAI_API_KEY`、`AISCAN_API_KEY` 等） |
| `--model` | 模型名称（默认 `gpt-4o`） |
| `--llm-proxy` | 访问 LLM API 的 HTTP 代理 |
| `--ai` | 对 scanner 输出启用 LLM 分析 |

### Agent 参数

| 参数 | 说明 |
| --- | --- |
| `-p, --prompt` | 自然语言任务描述 |
| `-i, --input` | 目标输入（IP、URL、IP:port、CIDR），可重复 |
| `-s, --skill` | 指定 skill 名称或文件路径，可重复 |
| `--task-file` | 从文件读取任务描述 |
| `--loop` | 作为 IOA loop worker 运行 |
| `--heartbeat <分钟>` | loop 模式下 heartbeat 间隔（0 表示关闭，默认 0） |
| `--timeout <秒>` | 整体超时（默认 3600） |
| `-e, --eval` | 目标评估标准 — 独立 LLM 判断任务是否达成 |

### Scanner 参数

| 参数 | 说明 |
| --- | --- |
| `--proxy` | Scanner 代理，支持 `socks5://`、`trojan://`、`vless://`、`clash://`（订阅自动负载均衡） |
| `--cyberhub-url` | Cyberhub 资源服务 URL |
| `--cyberhub-key` | Cyberhub API key |
| `--cyberhub-mode` | 资源模式：`merge`（默认，合并内置和远程资源）或 `override`（使用远程资源覆盖内置） |

### IOA 参数

| 参数 | 说明 |
| --- | --- |
| `--ioa-url` | IOA server URL |
| `--ioa-node-id` | 已有 IOA 节点 ID |
| `--ioa-node-name` | 注册时使用的节点名（默认自动生成） |
| `--space` | IOA 空间名（默认 `default`） |
| `--json` | IOA 查询结果以 JSON 输出 |

### 通用参数

| 参数 | 说明 |
| --- | --- |
| `--debug` | 输出调试日志 |
| `-q, --quiet` | 减少日志输出 |
| `--no-color` | 禁用 ANSI 颜色 |
| `--version` | 输出版本号并退出 |

> **参数名冲突说明**：顶层参数和 scanner 子命令参数可能同名。例如 `aiscan agent -p` 中 `-p` 是自然语言 prompt，`aiscan gogo -p` 中 `-p` 是端口参数，`aiscan zombie -p` 中 `-p` 是密码参数。aiscan 会根据子命令自动区分。

## 直接使用扫描器

### gogo：服务发现

```bash
aiscan gogo -i 192.168.1.0/24 -p top100
aiscan gogo -i 10.0.0.10 -p 80,443,8080
aiscan gogo -i targets.txt -p all
```

### spray：Web 探测和指纹

```bash
aiscan spray -u http://target.example
aiscan spray -u http://target.example --finger
aiscan spray -l urls.txt --finger
```

### zombie：弱口令检测

```bash
aiscan zombie -i ssh://127.0.0.1:22 --top 3
aiscan zombie -i ssh://admin@127.0.0.1:22 -p admin123
aiscan zombie -l services.txt --top 10
```

> 注意：`zombie -p` 是密码参数，不是 agent 的 prompt 参数。

### neutron：POC 检测

模板化 POC 执行，支持按 ID、tag、severity、fingerprint 和模板路径过滤。参数较多，完整列表如下：

| 参数 | 说明 |
| --- | --- |
| `-u, --target` | URL、host 或 ip:port，可重复 |
| `-i, --input` | target 别名 |
| `-l, --list` | 目标文件 |
| `-t, --templates` | 自定义模板文件或目录 |
| `--id` | 按模板 ID 执行 |
| `--finger` | 按指纹过滤模板 |
| `--tags, --tag` | 按 tag 过滤模板 |
| `-s, --severity` | 按严重性过滤（critical, high, medium, low, info） |
| `-c, --concurrency` | 模板并发数 |
| `--rate-limit` | 每秒执行上限 |
| `-j, --json` | JSON Lines 输出 |
| `-o, --output` | 写入文件 |
| `--template-list` | 列出匹配模板（不执行） |

```bash
# 按严重性过滤
aiscan neutron -u http://target.example -s critical,high

# 按指纹过滤
aiscan neutron -u http://target.example --finger nginx

# 按 tag 过滤，指定并发和速率限制
aiscan neutron -l targets.txt --tags cve,rce -c 10 --rate-limit 20

# 使用自定义模板，指定模板 ID
aiscan neutron -u http://target.example -t ./pocs --id shiro-detect -j -o loots.jsonl

# 列出匹配的模板（不执行扫描）
aiscan neutron -u http://target.example --template-list
```

### katana：Web 爬虫（仅 full 版）

```bash
aiscan katana -u https://target.example -d 3 -jc
aiscan katana -u https://target.example -d 2 -silent -jsonl
aiscan katana -u https://target.example -f qurl
aiscan katana -list urls.txt -d 2 -jc -timeout 60
```

Headless / Hybrid 引擎标志：

| 参数 | 说明 |
| --- | --- |
| `-hl, --headless` | 启用 headless 浏览器爬取（实验性） |
| `-hh, --hybrid` | 启用 headless hybrid 爬取（实验性） |
| `-cwu, --chrome-ws-url` | 连接已有 Chrome 实例的 debugger URL |

```bash
aiscan katana -u https://target.example -hl -d 3 -jc       # headless
aiscan katana -u https://target.example -hh -d 2            # hybrid
aiscan katana -u https://target.example -cwu ws://127.0.0.1:9222  # 远程 Chrome
```

### passive：网络空间搜索（仅 full 版）

```bash
aiscan passive -s fofa 'domain="example.com"'
aiscan passive -s hunter 'domain.suffix="example.com"'
aiscan passive -s shodan-idb '1.2.3.4'
```

| 数据源 | 凭据参数 | 环境变量 |
| --- | --- | --- |
| `fofa` | `--fofa-email`, `--fofa-key` | `FOFA_EMAIL`, `FOFA_KEY` |
| `hunter` | `--hunter-api-key` | `HUNTER_API_KEY` |
| `shodan-idb` | 无需 API key | — |

附加参数：`--recon-proxy`（搜索引擎 HTTP 代理）、`--recon-limit`（单次查询上限，0 = 无限）。

## cyberhub 资源查询

`cyberhub` 子命令查询和搜索已加载的指纹库与 POC 模板。

### 命令格式

```text
aiscan cyberhub list [finger|poc|all] [options]
aiscan cyberhub search [finger|poc|all] <query> [options]
aiscan cyberhub id <name-or-id>
```

### 结构化查询标志

| 参数 | 说明 |
| --- | --- |
| `--finger` | 按指纹名过滤（支持别名和 CPE 关联） |
| `--cve` | 按 CVE ID 过滤 |
| `--vendor` | 按厂商名过滤 |
| `--product` | 按产品名过滤 |
| `--poc` | 仅显示有关联 POC 模板的条目 |
| `--tag` | 按 tag 过滤，逗号分隔或重复 |
| `-s, --severity` | POC 严重性过滤 |
| `--limit` | 最大输出行数（默认 50，0 为全部） |
| `-j, --json` | JSON Lines 输出 |

### 本地缓存

Cyberhub 资源缓存在 `~/.aiscan/cache/`，TTL 24 小时。过期后自动从 Cyberhub 服务重新拉取。

### 示例

```bash
aiscan cyberhub search --finger tomcat
aiscan cyberhub search --cve CVE-2021-44228
aiscan cyberhub search --vendor apache --product tomcat
aiscan cyberhub search finger --poc
aiscan cyberhub list poc --severity critical --limit 10
aiscan cyberhub id tomcat
aiscan cyberhub search poc spring --tag rce -j
```

## 场景选择建议

| 场景 | 推荐命令 |
| --- | --- |
| 快速资产发现和风险初筛 | `aiscan scan -i <target>` |
| 完整扫描（含路径爆破） | `aiscan scan -i <target> --mode full` |
| 搜索已知漏洞情报 | `aiscan scan -i <target> --sniper` |
| 深度动态测试 | `aiscan scan -i <target> --deep` |
| AI 主动验证 + 漏洞搜索 | `aiscan scan -i <target> --verify=high --sniper` |
| 自动解释结果和生成结论 | `aiscan agent -p "<任务>" -i <target>` |
| 对 scanner 输出做 AI 摘要 | `aiscan --ai -p "<意图>" <scanner> ...` |
| 查询已加载的指纹和 POC | `aiscan cyberhub list poc --severity critical` |
| 机器可读输出 | `aiscan scan -i <target> -j` |
| 人可读报告 | `aiscan scan -i <target> --report` |
| 回看历史扫描记录 | `aiscan -F result.jsonl` |
| 多 worker 协作 | `aiscan ioa serve` + `aiscan agent --loop --space case-1` |
| 交互式探索 | `aiscan agent` |
| 目标驱动 + 自动评估 | `aiscan agent -e "确认所有高危漏洞已验证" -p "<任务>" -i <target>` |

## 常见问题

### agent 报 provider 未配置

`agent` 必须有可用的 LLM provider。设置对应环境变量或通过 `--api-key` 显式传入：

```bash
export OPENAI_API_KEY="sk-..."
aiscan agent --model gpt-4o -p "检查目标" -i http://target.example
```

### scan --verify 没有产生 AI 验证

1. 检查是否配置了 LLM provider（`--api-key` 或环境变量）
2. 确认发现的风险优先级达到了 `--verify` 指定的阈值（`--verify=high` 只验证 high 及以上）
3. 未显式传 `--verify` 时默认 `auto`（等效 `high`），LLM 不可用时静默跳过

### 输出太多或包含颜色

```bash
aiscan scan -i 127.0.0.1 -f result.txt          # 文件输出（自动去除 ANSI）
aiscan scan -i 127.0.0.1 --no-color              # 禁用颜色
```

### 扫描太慢

```bash
aiscan scan -i 192.168.1.0/24 --port top100      # 缩小端口范围
aiscan scan -i 192.168.1.0/24 --thread 500        # 降低并发
```

### --ai 需要 LLM 但 scan 不需要

顶层 `--ai` 在 scanner 执行后启动 LLM agent 分析输出，必须配置 LLM。`scan` 核心流水线（gogo → spray → zombie → neutron）不依赖 LLM。`scan --verify` 在 LLM 不可用时自动跳过验证，不影响其余扫描。

### cyberhub 没有结果

1. 检查 `--cyberhub-url` 和 `--cyberhub-key` 是否正确、服务是否可达
2. 本地缓存在 `~/.aiscan/cache/`（TTL 24 小时），如需强制刷新可删除缓存文件

### 信号处理

aiscan 使用两阶段信号处理（连续按键间隔超过 5 秒时计数器重置）：

| 操作 | 行为 |
| --- | --- |
| 第一次 Ctrl+C | 停止当前任务；无活跃任务时提示再按一次退出 |
| 第二次 Ctrl+C | 取消上下文，完成当前 turn 后退出 |
| 第三次 Ctrl+C | 强制退出进程 |
