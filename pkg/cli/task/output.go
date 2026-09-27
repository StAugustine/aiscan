// Package task owns the CLI presentation boundary around one-shot tasks.
package task

import (
	"context"
	"errors"
	"io"
	"os"

	agentsession "github.com/chainreactors/cyber/agent/session"
	cfg "github.com/chainreactors/cyber/pkg/config"
	"github.com/chainreactors/cyber/pkg/console"
	flags "github.com/jessevdk/go-flags"
)

// Output owns a CLI's output from option resolution through task completion.
// Before Run, Finish emits startup errors. Run transfers presentation to Console,
// which includes finalization failures in its single final result.
type Output struct {
	option         *cfg.Option
	stdout, stderr io.Writer
	started        bool
	Validation     console.TaskValidation
}

func NewOutput(option *cfg.Option, stdout, stderr io.Writer) *Output {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return &Output{option: option, stdout: stdout, stderr: stderr}
}

// Stdout is also used by direct command output, which keeps its native format.
func (o *Output) Stdout() io.Writer { return o.stdout }

func (o *Output) Finish(err error) error {
	var flagErr *flags.Error
	if err == nil || o.started || errors.As(err, &flagErr) && flagErr.Type == flags.ErrHelp {
		return err
	}
	format := o.option.OutputFormat
	if o.option.JSON {
		format = "json"
	}
	return errors.Join(err, console.WriteStartupError(o.stdout, format, err))
}

func (o *Output) Run(ctx context.Context, runtime *agentsession.Runtime, sessionID, label, display string, input agentsession.RunInput, finish func(error) error) error {
	o.started = true
	return console.RunTask(ctx, runtime, o.option, sessionID, label, display, input, finish,
		console.TaskOptions{Stdout: o.stdout, Stderr: o.stderr, Validation: o.Validation})
}
