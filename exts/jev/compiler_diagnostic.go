package jev

import (
	"context"
	"errors"
	"strings"
)

// CompilerDiagnostic describes an observable failure and the next operation
// available to the compiler. Waiting for evidence is distinct from code repair.
type CompilerDiagnostic struct {
	Code     string `json:"code"`
	Stage    string `json:"stage"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Action   string `json:"action"`
	Boundary *int   `json:"boundary,omitempty"`
	Call     *int   `json:"call_index,omitempty"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Replayed int    `json:"replayed,omitempty"`
	Recorded int    `json:"recorded,omitempty"`
}

type compilerValidationError struct {
	CompilerDiagnostic
}

func (e compilerValidationError) Error() string { return e.Message }

func compilerDiagnostic(err error) CompilerDiagnostic {
	var validation compilerValidationError
	if errors.As(err, &validation) {
		return validation.CompilerDiagnostic
	}
	var gap coverageGap
	if errors.As(err, &gap) && gap.diagnostic != nil {
		d := *gap.diagnostic
		d.Message = err.Error()
		return d
	}
	d := CompilerDiagnostic{Code: "artifact_invalid", Stage: "mechanism", Status: "repair", Message: err.Error(), Action: "Correct the submitted artifact using the exact native schemas and current recorded evidence, then validate it again."}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		d.Code, d.Status, d.Action = "compilation_interrupted", "canceled", "Retain this draft and diagnostic for resumption. Cancellation is not a correctness verdict."
	case strings.Contains(d.Message, "no recorded trajectory"), strings.Contains(d.Message, "recorded trajectory is truncated"):
		d.Code, d.Stage, d.Status, d.Action = "recorded_evidence_unavailable", "replay", "waiting", "Obtain a complete actual trajectory before qualification. inspect_evidence returns only real evidence; do not invent results or change the validation criteria."
	case strings.Contains(d.Message, "contract unavailable"), strings.Contains(d.Message, "native contracts unavailable"):
		d.Code, d.Stage, d.Action = "native_contract_unavailable", "native_contract", "Use an ID from native_contracts. If the required native capability is absent, retain an honest candidate and explain the missing capability."
	case strings.Contains(d.Message, "example arguments"), strings.Contains(d.Message, "example parameters"):
		d.Code, d.Stage, d.Action = "example_arguments_invalid", "parameters", "Use inspect_evidence to recover the exact current values. Supply every required example argument, preserving decoded Unicode, quotes and backslashes; example values are not executable defaults."
	case strings.Contains(d.Message, "explicit step and occurrence"), strings.Contains(d.Message, "declared step"), strings.Contains(d.Message, "occurrence"):
		d.Code, d.Stage, d.Action = "effect_identity_invalid", "native_contract", "Declare the effect step and its occurrence bound. Each write uses that step and an explicit zero-based occurrence; reads use read:true."
	case strings.Contains(d.Message, "read/effect"), strings.Contains(d.Message, "read flag"), strings.Contains(d.Message, "read assertion"):
		d.Code, d.Stage, d.Action = "native_access_invalid", "native_contract", "Match the tool-owned read/effect classification. Reads and polls use read:true; writes use read:false. Correct the operation instead of bypassing the contract."
	case strings.Contains(d.Message, "report provenance"):
		d.Code, d.Stage, d.Action = "report_evidence_invalid", "completion", "Use exactly {evidence:actualCallId,path:[stringObjectKey,nonnegativeIntegerArrayIndex,...]} to reference current actual results. Verify each field/index exists; do not traverse scalar text or copy a sample answer. Return a computed current value directly when transformation is needed."
	case strings.Contains(d.Message, "scene review"), strings.Contains(d.Message, "missing next progress"), strings.Contains(d.Message, "result validation"):
		d.Code, d.Stage, d.Action = "semantic_validation_failed", "semantic", "Repair the specific completion, binding or progress defect identified by independent review, then resubmit. Mechanism replay alone does not establish task completion."
	}
	return d
}
