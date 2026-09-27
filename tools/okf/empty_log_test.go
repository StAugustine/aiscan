package okf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogWithoutHeadingsReturnsValidationError(t *testing.T) {
	for _, content := range []string{"", " \n\t", "An unfinished log without a heading."} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "log.md")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("invalid log crashed the validator: %v", recovered)
				}
			}()
			report, err := Validate(t.Context(), path, false)
			if err != nil {
				t.Fatal(err)
			}
			if report.Valid() || len(report.Issues) == 0 || report.Issues[0].Rule != "reserved.structure" {
				t.Fatalf("invalid log accepted: %+v", report)
			}
		})
	}
}
