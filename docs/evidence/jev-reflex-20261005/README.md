# Retained JEV / Reflex experiments

Each experiment preserves its original `report.json` and generated cost
analysis, with text line endings normalized to LF. Cold runs start with an empty
library; warm reports explicitly record `library_origin`. Qualification reflects
the rules used at the time of that run, not automatic approval under today's
stricter `entry_report` check.

Reports retain unsuccessful and provider-blocked attempts. Fees use the saved
tariff snapshot and returned usage, not invoices; missing usage remains explicit.
Warm runs exclude inherited compilation costs and do not establish break-even.

Raw call/event logs and browser test output remain in the local `output`
directory. Reusable UI event fixtures live in
`web/frontend/e2e/fixtures/jev-history`. No credentials are included.
