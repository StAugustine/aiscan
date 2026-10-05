---
type: reference
---

# Evidence and report contract

The harness creates run.json (scope, Git root/subdirectory, tracked file count,
revision when the scope contains tracked files, tool versions, run status),
session.jsonl (resumable AOP events), raw/ and evidence/. Write full tool outputs
there when running scans; include command, working directory, stderr and exit
code in adjacent evidence notes. An interrupted run remains incomplete.

Update coverage.json using this structure. All arrays are required; empty arrays
mean explicitly no items. Set reviewed=true only after recording what was examined.
Scope and array items must contain meaningful text, not whitespace. Each tool
appears once in checks; combine its outcome and limitations in that record.

```json
{"reviewed":true,"scope":"requested repository and goal","examined":["entry points and exact paths reviewed"],"excluded":[],"unsupported":[],"unresolved":[],"checks":[{"tool":"osv-scanner","status":"completed","evidence":"raw/osv.json"},{"tool":"proton","status":"completed","evidence":"raw/proton.jsonl"}]}
```

Check status is completed, incomplete or not_applicable. A failed scan must be
incomplete, with an explanation in unresolved. not_applicable needs a reason in
the evidence field. Lack of a supported ecosystem belongs in unsupported.
For completed checks, evidence must be exactly one existing report-relative file
path, without explanations appended. Record notes and evidence reuse in log.md
or unresolved; reuse does not introduce a new status such as `reused`.
Evidence must remain in the report bundle. Relative symlinks within the bundle
are allowed; copy files instead of using absolute symlinks or Windows junctions
so the evidence remains portable.

findings.json is an array of objects:

```json
[{"id":"AUD-001","title":"Concrete weakness","status":"candidate","severity":"medium","location":"src/file.ts:42","preconditions":"Required attacker control and state","trace":["input -> validation -> sensitive operation"],"impact":"Observed or reasoned effect","evidence":["evidence/AUD-001.md"],"verification":"static","reproduction":"Not executed","recommendation":"Proposed correction"}]
```

status: candidate, confirmed, dismissed, inconclusive. verification: static,
reproduced, not_attempted. Use reproduced only when you actually executed the
reproduction and observed the predicted security impact. A dismissed candidate
must explain the effective protection; inconclusive identifies missing evidence.
Confirmed findings need an exact location, preconditions, trace, impact and
supporting evidence. Do not label advisory matches confirmed without establishing
the claimed impact. Do not include complete secret values.

index.md is the final OKF summary. Its frontmatter contains only
`okf_version: "0.2"`. Cover scope/revision,
prioritized findings, reasoning, evidence links, coverage and unresolved limits.
log.md is required, records dated investigation decisions and has no frontmatter.
Both index.md and log.md must contain Markdown headings. Other
Markdown evidence files are concepts and require a non-empty `type` frontmatter. Link raw/evidence artifacts with
bundle-relative paths. Run `audit validate` through bash before completion; it
defaults to the assigned report. `audit validate <report-directory>` selects an
explicit directory. This checks coverage/findings JSON, enum values, evidence
paths and OKF together. `okf validate` checks only the OKF portion.
The external CLI also provides `cyber-audit validate <report-directory>` without
starting a model. One-shot runs return validation errors to the same model
context for at most two repair rounds, within the overall timeout. Repair report
formatting and reuse real evidence; do not invent results to satisfy validation.

File tools are limited to the workspace. When the assigned report is outside
the workspace, use bash with quoted absolute paths for report reads and writes.
Never claim a clean repository solely because findings.json is empty.
