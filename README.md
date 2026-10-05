<p align="center">
  <img src="web/assets/logo.svg" width="180" alt="cyber logo">
  <h1 align="center">cyber-harness</h1>
  <p align="center">A general-purpose agent harness for cyber workflows — everything is an extension</p>
</p>

<p align="center">
  <a href="https://github.com/chainreactors/cyber-harness/releases"><img src="https://img.shields.io/github/v/release/chainreactors/cyber-harness?style=flat-square&color=00E59B" alt="Release"></a>
  <a href="https://github.com/chainreactors/cyber-harness/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/chainreactors/cyber-harness/ci.yml?branch=master&style=flat-square&label=CI" alt="CI"></a>
  <a href="https://github.com/chainreactors/cyber-harness/releases"><img src="https://img.shields.io/github/downloads/chainreactors/cyber-harness/total?style=flat-square&color=00B4D8" alt="Downloads"></a>
  <a href="https://github.com/chainreactors/cyber-harness/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square" alt="AGPL-3.0"></a>
  <a href="https://github.com/chainreactors/cyber-harness/stargazers"><img src="https://img.shields.io/github/stars/chainreactors/cyber-harness?style=flat-square&color=yellow" alt="Stars"></a>
</p>

<p align="center">
  <a href="README_CN.md">中文文档</a>
</p>

---

cyber-harness is a general-purpose agent harness for cyber workflows. Model reasoning, tool execution, knowledge, observation, and collaboration are assembled through the same extension lifecycle. Tasks and installed extensions determine the tools and workflows to use. Use the reference distribution or compose the capabilities your own Go application needs.

Choose an entry point for your task:

| Entry point | Use it for |
| --- | --- |
| `aiscan` / `aiscan-full` | Agent tasks, scanners, proxy routing and IOA; the full edition adds Web, browser automation, passive recon and crawling |
| [`cyber-audit`](audit/README.md) | Model-led source-code and binary audits, evidence collection and reports |
| `cyber-web` (from source) | A profile-neutral Web Hub for sessions, events and artifacts from scan, audit or custom AOP nodes |
| `agent` | A minimal local Agent built from source, with files, terminal, Skills and local subagents |

> Use only on explicitly authorized targets.

## Start here

Download the archive matching your operating system and architecture from [GitHub Releases](https://github.com/chainreactors/cyber-harness/releases/latest). For CLI use, choose `aiscan` or `cyber-audit`, extract it and put the executable on PATH. Configure a model as described below before running an Agent task:

```sh
# With an LLM configured: run a local task and save its events
aiscan agent -p "Read the current directory and explain its structure; do not modify files" -o first-run.jsonl

# Audit a local repository
cyber-audit --workdir /path/to/repository -p "Audit authentication and tenant isolation"

# Run a deterministic scan against your own local lab, without AI verification
aiscan scan -i http://127.0.0.1:3000 --verify=off
```

For the Web workbench, download `aiscan-full` and start it:

```sh
aiscan-full web --token replace-me
```

Open `http://127.0.0.1:8080` and sign in with the access key. Quick Connect generates a one-line command that downloads and starts a scan or audit node on the execution host. To connect a node you already installed, run one of these commands in another terminal:

```sh
aiscan agent --server-url http://replace-me@127.0.0.1:8080
cyber-audit --workdir /path/to/repository --server-url http://replace-me@127.0.0.1:8080
```

See [Getting started (中文)](docs/getting-started.md) for installation, PowerShell configuration and a complete first run. The Agent lets the model choose tool calls; the scan pipeline chooses work through rules and scan events, with optional AI stages.

## Configure a model

Run `cyber-web init` or `aiscan init` to configure your model in `~/.cyber/cyber.yaml`. For project overrides, add `--project`. These are shared cyber-harness commands, also available in the minimal `agent` CLI. Existing files are preserved. [Configuration guide](docs/configuration.md).

Example `cyber.yaml`:

```yaml
llm:
  provider: openai
  base_url: https://api.deepseek.com/v1
  model: deepseek-chat
```

Set `CYBER_API_KEY` to your credential and use the endpoint and model available to your account. For Anthropic-compatible services, use `provider: anthropic` and the corresponding settings. See the [configuration reference](docs/reference.md) for protocols, profiles and precedence.

## Documentation

Start at the [documentation home](docs/README.md). The guides and references describe current behavior and are maintained primarily in Chinese. For a release, read the documentation at its Git tag.

| What you need | Guide |
| --- | --- |
| Understand the harness, models, tools and sessions | [Concepts](docs/concepts.md) |
| Install, configure and complete a first task | [Getting started](docs/getting-started.md) · [Model configuration](docs/configuration.md) |
| Use sessions, tools, Skills, scanning and collaboration | [User guide](docs/README.md#使用者指南) · [Audit guide](audit/README.md) |
| Build a Go application or embed a session | [Developer guide](docs/development.md) |
| Connect an external client or look up parameters | [Client integration](docs/integration.md) · [API](docs/api.md) · [Configuration and commands](docs/reference.md) |
| Understand lifecycle, execution and data ownership | [Architecture](docs/architecture.md) |
| Review changes before upgrading | [Changelog](docs/changelog.md) |

## Build and embed

### Prepare the source

Install Git and the Go version declared in [go.mod](go.mod) (currently Go 1.26). The Makefile requires GNU Make and a POSIX shell; on Windows, install an MSYS2 toolchain and put its tools on PATH, or use the direct PowerShell build below.

```sh
git clone --recurse-submodules https://github.com/chainreactors/cyber-harness.git
cd cyber-harness
# For an existing checkout
git submodule update --init --recursive
```

### Build a distribution

Run these commands from the repository root. Outputs go to `bin/`; Windows binaries have an `.exe` suffix.

| Command | Output | Capabilities | Frontend required |
| --- | --- | --- | --- |
| `make` / `make standard` | `bin/aiscan` | Agent, core scanners, proxy, Skills and IOA | No |
| `make full` / `make web-build` | `bin/aiscan-full` | Standard capabilities plus Web, browser, passive recon and crawling | Yes |
| `make audit` | `bin/cyber-audit` | Source-code and binary audit; built from the independent `audit` module | No |
| `make agent` | `bin/agent` | Local Agent, files, terminal, Skills and local subagents | No |
| `make all` | `bin/aiscan` and `bin/aiscan-full` | Standard and full editions | Yes |

For targets that require the frontend, install Node.js/npm (CI uses Node.js 22), then install dependencies once before building:

```sh
npm --prefix web/frontend ci
make
make full
```

These targets use `CGO_ENABLED=0`. `make web-build` builds the full edition; `make web` also starts its Web service. Build tags are maintained in [editions.env](editions.env), and the build steps in [Makefile](Makefile). To build the separate `cyber-web` Hub from source, run `make frontend`, then `CGO_ENABLED=0 go build -tags "full sqlite" -o bin/cyber-web ./cmd/cyber-web`. Native recording uses `make record`, requires CGO and its SDK, and has separate [build instructions](docs/record.md).

### Build the minimal local Agent

`make agent` needs only Go and the Makefile toolchain. It excludes scanner, proxy, IOA, browser, recording and Web dependencies. After [configuring a model](docs/configuration.md), run it directly with `-p`:

```sh
make agent
./bin/agent -p "Read the current directory and explain its structure; do not modify files"
```

To build without Make, use PowerShell from the repository root:

```powershell
$env:CGO_ENABLED = "0"
go build -trimpath -buildvcs=false -ldflags "-s -w" -o bin/agent.exe ./cmd/agent
.\bin\agent.exe -p "Read the current directory and explain its structure; do not modify files"
```

### Customize a minimal application

Select capabilities in your own Go entry point. Start with the complete [tool-host example](examples/custom/main.go), which registers and calls a `hello` tool without a model:

```sh
mkdir -p cmd/my-agent
cp examples/custom/main.go cmd/my-agent/main.go
CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o bin/my-agent ./cmd/my-agent
./bin/my-agent
# Hello, Cyber!
```

In PowerShell, use `New-Item -ItemType Directory -Force cmd/my-agent` and `Copy-Item examples/custom/main.go cmd/my-agent/main.go`, set `$env:CGO_ENABLED = "0"`, then run the same `go build` command with `-o bin/my-agent.exe`.

Replace `helloTool` with your tool and register it using `extension.Add[coretool.Tool]`. The example calls `harness.BaseExtensions(harness.BaseConfig{...})`, appends the tool extension and its consumer in dependency order, then creates an `extension.Set`. The host calls `Load` before using capabilities and `Close` even if loading fails. Base capabilities include files, terminal, Skills, prompts, guardrail and provider state; optional product extensions are selected by your imports and composition code.

For model conversations, start from [examples/session/main.go](examples/session/main.go). It uses `harness.New(harness.Config{Base: ..., Extensions: ..., Session: ...})`: leaving `Session` nil creates a tool host; providing it adds the Agent loop and session runtime. For a real model, remove the extension that registers `demoProvider` and set `Base.Provider.Mode` to `provider.StartupRequired`, with your `Provider`, `BaseURL`, `Model` and `APIKey` in `Base.Provider.Config`. The [CLI composition](cmd/agent/profile.go) shows how to populate these settings from shared configuration. To retain the CLI and configuration commands, use [cmd/agent](cmd/agent) as the entry-point template.

Go links the dependencies reached from your entry point. Add only the extension packages you need; disabling a runtime option does not remove its imported dependencies. The `full` tag enables capabilities in packages that use it; it does not select individual extensions. Build the custom entry point with `CGO_ENABLED=0` unless a selected extension requires CGO. See the [developer guide](docs/development.md) for tool registration and embedded sessions.

### Embed resources and external tools

Scanner resources and external tool executables have separate switches:

```sh
make standard EMBED=1                  # Generate and embed scanner resources
make standard ARSENAL_EMBED=1          # Download and embed selected tool executables
make standard EMBED=1 ARSENAL_EMBED=1   # Include both
make audit ARSENAL_EMBED=1         # Embed the audit tool bundle
```

Arsenal maintains tool definitions and default versions in [arsenal.yaml](tools/arsenal/arsenal.yaml). Each distribution selects names in [cmd/aiscan/bundle.yaml](cmd/aiscan/bundle.yaml) or [audit/cmd/cyber-audit/bundle.yaml](audit/cmd/cyber-audit/bundle.yaml); override them with `ARSENAL_CONFIG` or `AUDIT_ARSENAL_CONFIG`. Embedded tools are extracted at startup without network access. The minimal `agent` has no Arsenal extension. See [Arsenal bundles](docs/arsenal-bundles.md) for cross compilation, offline distribution and tool upgrades.

## Contributing

Read the [development guide](docs/development.md) and [documentation standards](docs/maintaining-docs.md). Test instructions are available for the [repository harness](cmd/harness/README.md), [Web frontend](web/frontend/e2e/README.md) and [audit](audit/tests/README.md). Describe the behavior change, affected entry points and validation in your PR. Update the relevant guide or reference with behavior changes.

## Disclaimer

1. This harness supports **authorized agent tasks and research in cyber workflows**. Run scanning examples in your own lab or another environment you are authorized to test.
2. Before using this tool for any scanning, you must ensure compliance with local laws and regulations and obtain **sufficient authorization. Do not scan unauthorized targets.**
3. If you engage in any illegal activity while using this tool, you shall bear all consequences yourself. We assume no legal or joint liability.
4. Before installing and using this tool, please **carefully read and fully understand all terms**. Limitation and disclaimer clauses may be highlighted for your attention.
5. Unless you have fully read, understood, and accepted all terms of this agreement, please do not install or use this tool. Your use or any other express or implied acceptance constitutes your agreement to be bound by these terms.

## License

This project is licensed under the [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE).

## Links

- [chainreactors](https://github.com/chainreactors) — Organization
- [IOA](https://github.com/chainreactors/ioa) — Internet of Agents
- [sdk](https://github.com/chainreactors/sdk) — Scanner SDK
- [proxyclient](https://github.com/chainreactors/proxyclient) — Multi-protocol proxy client
- [crtm](https://github.com/chainreactors/crtm) — CLI tool package registry
- [utils](https://github.com/chainreactors/utils) — Shared utilities & PTY manager
- [parsers](https://github.com/chainreactors/parsers) — Protocol & data parsers

---

<p align="center">
  <a href="https://star-history.com/#chainreactors/cyber-harness&Date">
    <img src="https://api.star-history.com/svg?repos=chainreactors/cyber-harness&type=Date" alt="Star History" width="600">
  </a>
</p>
