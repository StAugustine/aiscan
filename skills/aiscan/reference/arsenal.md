
# arsenal

Security tool package manager. Search, install, and manage external CLI tools from chainreactors, projectdiscovery, and any GitHub repo.

Installed tools are immediately available via `bash`.

## Quick reference

```
arsenal(action="list")                              — browse all tools with install status and version
arsenal(action="search", query="port scanner")      — find tools by keyword
arsenal(action="info", name="nuclei")               — tool detail + latest version
arsenal(action="install", name="httpx")             — install latest
arsenal(action="install", name="dnsx", version="1.2.3") — install pinned version
arsenal(action="releases", name="nuclei")           — check latest release
arsenal(action="add", repo="ffuf/ffuf", pattern="{name}_{version}_{os}_{arch}.tar.gz") — register third-party repo
```

## When to use

- Need a CLI tool that is **not** a built-in pseudo-command (scan/gogo/spray/zombie/neutron).
- User names a specific external tool (nuclei, httpx, ffuf, sqlmap, ...).
- Exploring what tools exist for a task — start with `arsenal search`.

## Typical workflow

1. `arsenal(action="list")` or `arsenal(action="search", query="subdomain")` — discover tools
2. `arsenal(action="install", name="subfinder")` — install
3. `bash(command="subfinder -d target.com")` — use via bash

## Adding unknown tools

Any GitHub repo with release binaries can be added at runtime:

```
arsenal(action="add", repo="owner/repo", pattern="{name}_{version}_{os}_{arch}.tar.gz")
arsenal(action="install", name="repo")
```

Common patterns: `{name}_{version}_{os}_{arch}.tar.gz` (Go tools), `{name}_{version}_{os}_{arch}.zip` (PD style), `{name}_{os}_{arch}` (raw binary).
