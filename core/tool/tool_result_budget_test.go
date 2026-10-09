package tool

import (
	aop "github.com/chainreactors/cyber/aop"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundToolResultOutputPreservesMediaAndMetadata(t *testing.T) {
	full := "HEAD-OF-OUTPUT " + strings.Repeat("中", 2<<20) + " TAIL-OF-OUTPUT"
	result := &aop.ToolResult{
		CallId: "call", Name: "read", IsError: true, Terminate: true, DurationMs: 12,
		Output: []*aop.Content{aop.Text(full), aop.Image("image/png", []byte("image")), aop.Text("last block")},
	}
	boundToolResultOutput(result)
	got := ResultText(result)
	if len(got) > toolResultBudgetBytes || !utf8.ValidString(got) {
		t.Fatalf("preview bytes=%d valid=%v", len(got), utf8.ValidString(got))
	}
	if !strings.Contains(got, "HEAD-OF-OUTPUT") || !strings.Contains(got, "TAIL-OF-OUTPUT") || !strings.Contains(got, "last block") {
		t.Fatal("preview lost the head or tail")
	}
	if len(result.Output) != 2 || result.Output[1].GetMedia() == nil || !result.IsError || !result.Terminate || result.DurationMs != 12 || result.CallId != "call" {
		t.Fatalf("result lost metadata or media: %+v", result)
	}
}

func TestBoundToolResultOutputUnderBudgetUnchanged(t *testing.T) {
	result := TextResult("small result")
	content := result.Output[0]
	boundToolResultOutput(result)
	if result.Output[0] != content || ResultText(result) != "small result" {
		t.Fatal("under-budget result changed")
	}
}
