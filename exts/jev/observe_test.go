package jev

import (
	"context"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"testing"
	"time"
)

func TestExecutableReflexIsolationAndComputeBudget(t *testing.T) {
	for _, source := range []string{`js:function(){return {report:require("fs")};}`, `js:function(){while(true){};}`, `js:function(){return {report:Date.now()};}`, `js:function(){return {report:Math.random()};}`, `js:function(){return Promise.resolve({report:1});}`} {
		r := Reflex{When: "test", Decide: "test", Observe: source}
		if err := r.validate(); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		_, err := runReflexJS(ctx, &r, map[string]any{"tools": []any{}}, nil, nil, nil)
		cancel()
		if err == nil {
			t.Fatalf("accepted unsafe/non synchronous program: %s", source)
		}
	}
}
func TestExecutableReflexJEVProtocolValidation(t *testing.T) {
	r := Reflex{When: "test", Decide: "test", Observe: `js:function(){return {report:jev({questions:{route:{type:"choice",instructions:"choose",criteria:{a:"a",defer:"other"}}}}).answers.route.choice};}`}
	_ = r.validate()
	_, err := runReflexJS(t.Context(), &r, map[string]any{"tools": []any{}}, nil, func(jevapi.Request) (*jevapi.Response, error) {
		return &jevapi.Response{Answers: map[string]jevapi.Answer{"route": answer("unbound")}}, nil
	}, nil)
	if err == nil {
		t.Fatal("unbound JEV choice accepted")
	}
}
