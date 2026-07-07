package truncate

import (
	"strings"
	"unicode/utf8"
)

// Clip collapses whitespace (newlines → spaces, multiple spaces → one),
// then truncates to maxLen bytes at a UTF-8 boundary, appending "...".
func Clip(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return "..."
	}
	cut := safeUTF8Cut(s, maxLen-3)
	return cut + "..."
}

// ClipRunes truncates by rune count, appending "…" (unicode ellipsis).
func ClipRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	n := 0
	bytePos := 0
	for bytePos < len(s) && n < maxRunes-1 {
		_, size := utf8.DecodeRuneInString(s[bytePos:])
		bytePos += size
		n++
	}
	return s[:bytePos] + "…"
}
