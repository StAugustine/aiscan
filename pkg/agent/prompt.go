package agent

import (
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/pkg/tool"
	"github.com/chainreactors/aiscan/skills"
)

type PromptConfig struct {
	Tools            *tool.ToolRegistry
	ScannerDocs      string
	CustomPreamble   string
	Skills           []skills.Skill
	ScannerAgentMode bool
	ScannerName      string
}

func BuildSystemPrompt(cfg *PromptConfig) string {
	if cfg == nil {
		cfg = &PromptConfig{}
	}
	tools := cfg.Tools
	if tools == nil {
		tools = tool.NewToolRegistry()
	}

	var sb strings.Builder

	if cfg.CustomPreamble != "" {
		sb.WriteString(cfg.CustomPreamble)
		sb.WriteString("\n\n")
	} else if cfg.ScannerAgentMode {
		sb.WriteString(fmt.Sprintf(`You are aiscan's %s analysis agent. Execute the requested scanner command using the bash tool, analyze the results, and provide findings.

You can use parse_results and filter_results tools for structured analysis of JSON scanner output — run scanners with -j flag to get JSON when you need structured data. Without a specific user intent, follow the %s skill guidelines to decide what analysis to perform.

`, cfg.ScannerName, cfg.ScannerName))
	} else {
		sb.WriteString(`You are aiscan, an autonomous security scanning agent powered by the chainreactors toolkit.
Your job is to perform security assessments based on the user's task description.
You work autonomously — analyze, plan, and execute using the provided tools until the task is complete.

`)
	}

	sb.WriteString("## Available Tools\n\n")
	for _, t := range tools.All() {
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", t.Name(), t.Description()))
	}

	if hasIOATools(tools) {
		sb.WriteString(`## IOA Collaboration

IOA tools provide shared message spaces for coordination with other nodes:
- Use ioa_space to create or join a collaboration space and capture the returned space id.
- Use ioa_send to publish structured findings, questions, or task updates.
- Use ioa_read to read messages addressed to this node, or pass all=true when full space context is needed.

`)
	}

	if cfg.ScannerDocs != "" {
		sb.WriteString("## Scanner Pseudo-Commands (available via bash tool)\n\n")
		sb.WriteString("The bash tool intercepts the following scanner commands. Use them as if they were CLI tools:\n\n")
		sb.WriteString(cfg.ScannerDocs)
		sb.WriteString("\n\n")
	}

	if skillPrompt := skills.FormatForPrompt(cfg.Skills); skillPrompt != "" {
		sb.WriteString(skillPrompt)
		sb.WriteString("\n\n")
	}

	if cfg.ScannerAgentMode {
		sb.WriteString(`## Workflow Guidelines

1. **Execute the scanner command** provided in the task using the bash tool.
2. **Analyze the output** — identify key findings, patterns, and risks.
3. **For deeper analysis**: re-run the scanner with -j flag and use parse_results/filter_results tools for structured data processing.
4. **For follow-up scans**: use other scanner pseudo-commands via bash (e.g., spray after gogo discovers web services).
5. **Document findings**: write structured results to files when useful.
6. **When done**: stop calling tools and provide a structured findings summary.

## Rules

- Execute the scanner command exactly as provided in the task, then analyze.
- Use appropriate thread counts — do not overwhelm targets.
- If a scan fails, adjust parameters and retry.
- Write intermediate findings to files so they are not lost.
- When done, stop calling tools and provide your final summary.
`)
	} else {
		sb.WriteString(`## Workflow Guidelines

1. **Use scan first for broad tasks**: For normal asset or vulnerability assessment requests, start with scan. It assembles gogo, spray, zombie, and neutron capabilities into deterministic streaming profiles.
2. **Use step commands for retries**: Use gogo, spray, zombie, or neutron directly when a stage fails, needs narrower parameters, or needs confirmation.
3. **Service identification**: Analyze the scan results to understand what services are running before launching targeted retries.
4. **Document findings**: Write structured results to files for record-keeping when useful.

## Output Format

When you have completed the assessment, provide a structured summary including:
- Discovered hosts and open ports
- Identified services and technologies (fingerprints)
- Vulnerabilities found (with severity)
- Recommended remediation steps

## Rules

- Prefer scan -i <target> --mode quick for normal coverage, including gogo -p all -v, web check/finger, spray common/crawl/bak/active with recon, weakpass, and fingerprint-based POC checks. Use --mode full when gogo full-port scanning and spray_brute default dictionary probing are appropriate.
- Use scan --debug when you need to explain how targets moved through the pipeline, and scan -j when raw gogo/spray JSONL is needed.
- Use direct pseudo-commands with the original tool-style flags when running a specific stage: gogo -i <ip/cidr> -p <ports>, spray -u <url>, zombie -i <service-url> -p <pwd>.
- Always check scan results before proceeding to targeted follow-up steps.
- Use appropriate thread counts — do not overwhelm targets.
- If a scan fails, adjust parameters and retry.
- Write intermediate findings to files so they are not lost.
- When done, stop calling tools and provide your final summary.
`)
	}

	return sb.String()
}

func hasIOATools(tools *tool.ToolRegistry) bool {
	if tools == nil {
		return false
	}
	if _, ok := tools.Get("ioa_space"); ok {
		return true
	}
	if _, ok := tools.Get("ioa_send"); ok {
		return true
	}
	if _, ok := tools.Get("ioa_read"); ok {
		return true
	}
	return false
}
