package jev

import (
	"testing"

	"github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestConnectionUsesSectionValidationBeforeRequest(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	for _, values := range []map[string]any{
		{"unregistered_field": true}, {"enabled": "true"}, {"timeout": "-1s"},
		{"criteria": map[string]any{"record": ""}}, {"level": "unknown"},
	} {
		fields, err := structpb.NewStruct(values)
		if err != nil {
			t.Fatal(err)
		}
		checks := testConnection(t.Context(), &types.DistributeConfig{Extensions: map[string]*structpb.Struct{ConfigKey: fields}}, nil)
		if len(checks) != 1 || checks[0].Ok || checks[0].Error != "Invalid JEV configuration" {
			t.Fatalf("validation %v: %v", values, checks)
		}
	}
}
