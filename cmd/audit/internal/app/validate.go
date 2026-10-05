package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	coretool "github.com/chainreactors/cyber/core/tool"
)

// All entry points use the same validation as finalization. Validation is
// read-only and does not initialize a provider, prepare tools or modify run.json.
func validateReport(ctx context.Context, directory string, out io.Writer) error {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if err := (&runReport{Directory: directory}).validate(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Audit report valid: %s\n", directory)
	return err
}

func reportCommand(directory string) coretool.Command {
	return coretool.Command{
		Name: "audit", Usage: "audit validate [report-directory]",
		QuickReference:  "### audit - validate the complete audit report\n  audit validate [report-directory]   Check coverage/findings JSON, evidence files and OKF together; defaults to the assigned report",
		DescriptionPath: "cyber://skills/audit/report.md",
		Run: func(ctx context.Context, execution *coretool.Execution) (any, error) {
			if execution == nil || len(execution.Args) < 1 || len(execution.Args) > 2 || execution.Args[0] != "validate" {
				return nil, fmt.Errorf("usage: audit validate [report-directory]")
			}
			target := directory
			if len(execution.Args) == 2 {
				target = execution.Args[1]
				if !filepath.IsAbs(target) {
					target = filepath.Join(execution.Dir, target)
				}
			}
			if target == "" {
				return nil, fmt.Errorf("no report assigned; use audit validate <report-directory>")
			}
			return nil, validateReport(ctx, target, execution.Stdout)
		},
	}
}
