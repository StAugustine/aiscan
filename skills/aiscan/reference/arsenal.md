
# arsenal

Security tool package manager. Search, install, update, remove CLI tools from chainreactors, projectdiscovery, and any GitHub repo.

Installed tools are immediately available via `bash`.

## First step

Before installing anything, call `arsenal(action="list")` to see what's available and what's already installed:

```
arsenal(action="list")
```

Output shows `*` and version for installed tools:
```
* v2.14.1  gogo               [chainreactors     ] Host, port, service and banner discovery
* v1.2.3   dnsx               [projectdiscovery  ] Fast DNS toolkit
           nuclei             [projectdiscovery  ] Fast vulnerability scanner with 9000+ templates
           httpx              [projectdiscovery  ] Fast HTTP toolkit with tech detection
3/22 installed
```

## Quick reference

```
arsenal(action="list")                                     — all tools + install status + version
arsenal(action="search", query="port scanner")             — find tools by keyword/tag
arsenal(action="info", name="nuclei")                      — detail + docs URL + hint + latest version
arsenal(action="install", name="httpx")                    — install latest (idempotent)
arsenal(action="install", name="dnsx", version="1.2.3")    — install pinned version
arsenal(action="update", name="nuclei")                    — re-download latest version
arsenal(action="remove", name="httpx")                     — delete installed binary
arsenal(action="releases", name="nuclei")                  — check latest release tag
arsenal(action="add", repo="ffuf/ffuf", pattern="{name}_{version}_{os}_{arch}.tar.gz") — register third-party repo
```

## Key behaviors

- **install is idempotent** — already-installed tools return success, not error. Use `update` to refresh.
- **version from git tag** — versions come from GitHub release tags, not filename parsing.
- **install output includes docs + hint** — follow the hint (e.g. "run nuclei -update-templates").
- **gogo/spray/zombie are built-in pseudo-commands** — no install needed for those. Arsenal has them for standalone binary use only.

## Typical workflow

1. `arsenal(action="list")` — see what's installed and available
2. `arsenal(action="search", query="subdomain")` — find tools for the task
3. `arsenal(action="install", name="subfinder")` — install
4. `bash(command="subfinder -d target.com")` — use via bash

## Adding unknown tools

```
arsenal(action="add", repo="ffuf/ffuf", pattern="{name}_{version}_{os}_{arch}.tar.gz")
arsenal(action="install", name="ffuf")
```

Common patterns: `{name}_{version}_{os}_{arch}.tar.gz` | `{name}_{version}_{os}_{arch}.zip` | `{name}_{os}_{arch}` (raw binary).
