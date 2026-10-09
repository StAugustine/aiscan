package tool

import (
	"fmt"
	"strings"
	"unicode/utf8"

	aop "github.com/chainreactors/cyber/aop"
)

// The transport bounds inline text. Recoverable output stays at its source;
// this boundary never creates a second storage or reference mechanism.
const (
	// toolResultBudgetBytes caps the inline text of one tool result. 64 KiB is
	// ~16-20k tokens, ~2% of a 1M context window.
	toolResultBudgetBytes = 64 << 10
	// toolResultTailBytes keeps the conclusion at the bottom of the output.
	toolResultTailBytes = 16 << 10
	// toolResultMarkerReserve leaves room for the separator and the reference
	// marker so the assembled preview never exceeds the budget.
	toolResultMarkerReserve = 2 << 10
	// toolResultHeadBytes keeps the structure at the top; head + tail + reserve
	// equals the budget.
	toolResultHeadBytes = toolResultBudgetBytes - toolResultTailBytes - toolResultMarkerReserve
)

// boundToolResultOutput bounds text while preserving media and result metadata.
func boundToolResultOutput(result *aop.ToolResult) {
	if result == nil {
		return
	}
	total := 0
	for _, content := range result.Output {
		if text := content.GetText(); text != nil {
			total += len(text.Text)
		}
	}
	if total <= toolResultBudgetBytes {
		return
	}

	var sb strings.Builder
	sb.Grow(total)
	for _, content := range result.Output {
		if text := content.GetText(); text != nil {
			sb.WriteString(text.Text)
		}
	}
	full := sb.String()

	var preview strings.Builder
	preview.WriteString(headBytes(full, toolResultHeadBytes))
	preview.WriteString("\n\n...[output truncated]...\n\n")
	preview.WriteString(tailBytes(full, toolResultTailBytes))
	preview.WriteString("\n\n")
	fmt.Fprintf(&preview,
		"[output too large: %d bytes total; truncated to fit the context budget. Read the original source for more.]", total)
	// Final clamp guarantees the inline output never exceeds the budget even if
	// the marker is longer than the reserve.
	out := headBytes(preview.String(), toolResultBudgetBytes)
	output := make([]*aop.Content, 0, len(result.Output))
	addedText := false
	for _, content := range result.Output {
		if content.GetText() != nil {
			if !addedText {
				output = append(output, aop.Text(out))
				addedText = true
			}
		} else {
			output = append(output, content)
		}
	}
	result.Output = output
}

// headBytes returns the first n bytes of s, cut on a rune boundary. The input is
// already valid UTF-8 (results are sanitized before bounding), so this only avoids
// splitting a multi-byte rune at the cut point.
func headBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// tailBytes returns the last n bytes of s, cut on a rune boundary.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
