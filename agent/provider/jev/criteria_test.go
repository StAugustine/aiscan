package jev

import (
	"encoding/json"
	"testing"
)

func TestNativeCriteriaBindingAndShapeValidation(t *testing.T) {
	type optionName string
	type options map[optionName]any
	response := &Response{Answers: map[string]Answer{"choice": {Type: "choice", Choice: "ready"}, "score": {Type: "score", Score: floatPtr(1.5)}}}
	for _, criteria := range []any{
		map[string]string{"ready": "description"},
		map[string]any{"ready": map[string]any{"description": "structured", "value": nil}},
		options{"ready": nil},
	} {
		if got, err := response.Choice("choice", Question{Type: "choice", Criteria: criteria}); err != nil || got != "ready" {
			t.Fatalf("choice %T: %q, %v", criteria, got, err)
		}
	}
	for _, criteria := range []any{nil, []string{"ready"}, map[int]string{1: "ready"}, map[string]string{"other": "description"}} {
		if _, err := response.Choice("choice", Question{Type: "choice", Criteria: criteria}); err == nil {
			t.Fatalf("accepted choice criteria %T", criteria)
		}
	}
	for _, criteria := range []any{[]string{"low", "medium", "high"}, []any{"low", nil, map[string]string{"description": "high"}}, [3]string{"low", "medium", "high"}} {
		if got, err := response.Score("score", Question{Type: "score", Criteria: criteria}); err != nil || got != 1.5 {
			t.Fatalf("score %T: %v, %v", criteria, got, err)
		}
	}
	for _, criteria := range []any{nil, []string{"only"}, make([]string, 11), []byte{0, 1, 2}, json.RawMessage(`["low","medium","high"]`), "levels", map[string]string{"a": "low", "b": "high"}} {
		if _, err := response.Score("score", Question{Type: "score", Criteria: criteria}); err == nil {
			t.Fatalf("accepted score criteria %T", criteria)
		}
	}
}

func floatPtr(value float64) *float64 { return &value }
