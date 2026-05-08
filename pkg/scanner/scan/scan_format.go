package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chainreactors/parsers"
)

func formatSummary(d *scanData) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	stats := d.statsSnapshotLocked()

	var sb strings.Builder
	sb.WriteString(summaryLine(d, stats))

	if len(stats.CapabilityRuns) > 0 {
		sb.WriteString("\n")
		sb.WriteString(metricLine("runs", stats.CapabilityRuns))
	}
	if len(stats.SprayByCapability) > 0 {
		sb.WriteString("\n")
		sb.WriteString(metricLine("spray", stats.SprayByCapability))
	}
	if len(stats.ErrorsBySource) > 0 {
		sb.WriteString("\n")
		sb.WriteString(metricLine("errors", stats.ErrorsBySource))
	}

	if len(d.trace) > 0 {
		sb.WriteString("\n## Pipeline Trace\n")
		for _, line := range d.trace {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func formatMarkdown(d *scanData) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	stats := d.statsSnapshotLocked()

	var sb strings.Builder
	sb.WriteString("# Scan Report\n\n")
	sb.WriteString(summaryLine(d, stats))
	sb.WriteString("\n\n")

	sb.WriteString("## Metrics\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("| --- | ---: |\n")
	sb.WriteString(fmt.Sprintf("| Inputs | %d |\n", stats.Inputs))
	sb.WriteString(fmt.Sprintf("| Open services | %d |\n", len(d.gogoResults)))
	sb.WriteString(fmt.Sprintf("| Web endpoints | %d |\n", len(d.webEndpoints)))
	sb.WriteString(fmt.Sprintf("| Web probes | %d |\n", len(d.sprayResults)))
	sb.WriteString(fmt.Sprintf("| Fingerprints | %d |\n", len(d.fingerprints)))
	sb.WriteString(fmt.Sprintf("| Weakpass findings | %d |\n", len(d.zombieResults)))
	sb.WriteString(fmt.Sprintf("| Vulnerability findings | %d |\n", len(d.neutronMatches)))
	sb.WriteString(fmt.Sprintf("| AI verifications | %d |\n", len(d.verifications)))
	sb.WriteString(fmt.Sprintf("| Errors | %d |\n", len(d.errors)))
	sb.WriteString(fmt.Sprintf("| Duration | %s |\n", stats.Duration().Round(time.Millisecond)))

	if len(stats.CapabilityRuns) > 0 {
		sb.WriteString("\n## Capability Runs\n\n")
		writeCountTable(&sb, "Capability", stats.CapabilityRuns)
	}

	if len(d.gogoResults) > 0 {
		sb.WriteString("\n## Open Services\n\n")
		for _, result := range sortedCopy(d.gogoResults, func(a, b *parsers.GOGOResult) bool {
			return a.GetTarget() < b.GetTarget()
		}) {
			sb.WriteString("- ")
			sb.WriteString(stripANSI(strings.TrimSpace(result.String())))
			sb.WriteString("\n")
		}
	}

	if len(d.webEndpoints) > 0 {
		sb.WriteString("\n## Web Endpoints\n\n")
		for _, endpoint := range sortedCopy(d.webEndpoints, func(a, b webEndpoint) bool {
			if a.URL == b.URL {
				return a.HostHeader < b.HostHeader
			}
			return a.URL < b.URL
		}) {
			sb.WriteString("- ")
			sb.WriteString(endpoint.URL)
			if endpoint.HostHeader != "" {
				sb.WriteString(" host=")
				sb.WriteString(endpoint.HostHeader)
			}
			if endpoint.Source != "" {
				sb.WriteString(" source=")
				sb.WriteString(endpoint.Source)
			}
			sb.WriteString("\n")
		}
	}

	if len(d.sprayResults) > 0 {
		sb.WriteString("\n## Web Probe Results\n\n")
		for _, item := range sortedCopy(d.sprayResults, func(a, b sprayObservation) bool {
			return sprayResultSortKey(a) < sprayResultSortKey(b)
		}) {
			if item.Result == nil {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(item.Capability)
			sb.WriteString(" ")
			sb.WriteString(stripANSI(strings.TrimSpace(item.Result.String())))
			sb.WriteString("\n")
		}
	}

	if len(d.fingerprints) > 0 {
		sb.WriteString("\n## Fingerprints\n\n")
		for _, finger := range sortedCopy(d.fingerprints, func(a, b fingerprint) bool {
			if a.Target == b.Target {
				return a.Name < b.Name
			}
			return a.Target < b.Target
		}) {
			sb.WriteString(fmt.Sprintf("- %s %s", finger.Target, finger.Name))
			if finger.Source != "" {
				sb.WriteString(" source=")
				sb.WriteString(finger.Source)
			}
			sb.WriteString("\n")
		}
	}

	if len(d.zombieResults) > 0 {
		sb.WriteString("\n## Weakpass Findings\n\n")
		for _, result := range d.zombieResults {
			sb.WriteString("- ")
			sb.WriteString(stripANSI(strings.TrimSpace(result.Format(parsers.ZombieFormatWeakpassFinding))))
			sb.WriteString("\n")
		}
	}

	if len(d.neutronMatches) > 0 {
		sb.WriteString("\n## Vulnerability Findings\n\n")
		for _, line := range sortedCopy(d.neutronMatches, func(a, b string) bool { return a < b }) {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	if len(d.verifications) > 0 {
		sb.WriteString("\n## AI Verification Results\n\n")
		for _, item := range sortedCopy(d.verifications, func(a, b verificationResult) bool {
			left := a.Finding
			right := b.Finding
			return string(left.Status)+"|"+left.Target+"|"+left.OriginalKey < string(right.Status)+"|"+right.Target+"|"+right.OriginalKey
		}) {
			finding := item.Finding
			sb.WriteString("- ")
			sb.WriteString(string(finding.Status))
			sb.WriteString(" priority=")
			sb.WriteString(string(finding.OriginalPriority))
			if finding.Target != "" {
				sb.WriteString(" target=")
				sb.WriteString(finding.Target)
			}
			if finding.OriginalKind != "" {
				sb.WriteString(" finding=")
				sb.WriteString(string(finding.OriginalKind))
			}
			if finding.Summary != "" {
				sb.WriteString(" summary=")
				sb.WriteString(finding.Summary)
			}
			if finding.Evidence != "" {
				sb.WriteString(" evidence=")
				sb.WriteString(finding.Evidence)
			}
			if item.Source != "" {
				sb.WriteString(" source=")
				sb.WriteString(item.Source)
			}
			sb.WriteString("\n")
		}
	}

	if len(d.errors) > 0 {
		sb.WriteString("\n## Errors\n\n")
		for _, line := range sortedCopy(d.errors, func(a, b string) bool { return a < b }) {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func formatJSONLines(d *scanData) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var sb strings.Builder
	for _, result := range d.gogoResults {
		line, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		sb.Write(line)
		sb.WriteByte('\n')
	}
	for _, item := range d.sprayResults {
		line, err := json.Marshal(item.Result)
		if err != nil {
			return "", err
		}
		sb.Write(line)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

func formatPlainText(d *scanData, fileLines []string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var sb strings.Builder
	for _, line := range fileLines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteString(summaryLine(d, d.statsSnapshotLocked()))
	return sb.String()
}

func summaryLine(d *scanData, stats statsSnapshot) string {
	return fmt.Sprintf("[scan] completed inputs=%d open=%d web=%d probes=%d fingerprints=%d weakpass=%d vulns=%d verified=%d errors=%d duration=%s\n",
		stats.Inputs,
		len(d.gogoResults),
		len(d.webEndpoints),
		len(d.sprayResults),
		len(d.fingerprints),
		len(d.zombieResults),
		len(d.neutronMatches),
		len(d.verifications),
		len(d.errors),
		stats.Duration().Round(time.Millisecond))
}

func sortedCopy[T any](items []T, less func(a, b T) bool) []T {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

func sprayResultSortKey(item sprayObservation) string {
	if item.Result == nil {
		return item.Capability
	}
	return item.Result.UrlString + "|" + item.Capability + "|" + item.Result.Source.Name()
}

func formatTraceEvent(event pipelineEvent) string {
	line := fmt.Sprintf("[scan:debug] action=%s", event.Action)
	if event.Capability != "" {
		line += " capability=" + event.Capability
	}
	line += fmt.Sprintf(" kind=%s key=%q source=%s", event.Event.label(), event.Event.key(), event.Event.Source)
	switch target := event.Event.Target.(type) {
	case scanTarget:
		if target.Target != "" {
			line += " target=" + target.Target
		}
	case serviceTarget:
		if target.Result != nil {
			line += " target=" + target.Result.GetTarget()
		}
	case webTarget:
		if target.URL != "" {
			line += " url=" + target.URL
		}
		if target.HostHeader != "" {
			line += " host=" + target.HostHeader
		}
	case webProbeTarget:
		if target.Result != nil && target.Result.UrlString != "" {
			line += " url=" + target.Result.UrlString
		}
		if target.HostHeader != "" {
			line += " host=" + target.HostHeader
		}
	case pocTarget:
		if target.Target != "" {
			line += " target=" + target.Target
		}
	case weakpassTarget:
		if target.Target.Address() != ":" {
			line += " target=" + target.Target.Address()
		}
	}
	if event.Event.Kind == eventError && event.Event.Error.Message != "" {
		line += " message=" + event.Event.Error.Message
	}
	return line
}
