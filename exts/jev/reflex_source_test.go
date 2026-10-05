package jev

import (
	"encoding/json"
	"github.com/dop251/goja"
	"strings"
	"testing"
)

func TestNamedReaderSerializesCurrentArguments(t *testing.T) {
	r := Reflex{When: "read", Decide: "current input", Observe: `js:function(context,args){execute(bind("reader",{source:program("inspect",[args.resource])},true));return {report:1};}`, Readers: map[string]string{"inspect": `function(resource){return {resource:resource};}`}}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"a 'quoted' resource\nnext", "新会话资源；\"值\""} {
		_, calls, err := probeReflex(t.Context(), &r, observationCapabilities("reader"), map[string]any{"resource": resource})
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range calls {
			var args struct{ Source string }
			_ = json.Unmarshal(call.Arguments, &args)
			value, err := goja.New().RunString(args.Source)
			if err != nil || value.Export().(map[string]any)["resource"] != resource {
				t.Fatalf("value=%v err=%v", value, err)
			}
		}
	}
}
func TestRuntimeArgumentsAreNeverPersisted(t *testing.T) {
	r := Reflex{When: "capability", Decide: "choice", Observe: `js:function(context,args){return {report:args.actor};}`, arguments: map[string]any{"actor": "private-example"}}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "private-example") {
		t.Fatal("sample arguments persisted")
	}
}

func TestCompilerNormalizesAnOrdinaryFunctionWithoutChangingArguments(t *testing.T) {
	for _, source := range []string{`function(context,args){return {report:args.id};}`, `(context,args)=>({report:args.id})`} {
		var r *Reflex
		artifact := jsonText(map[string]any{"observe": source, "arguments": map[string]any{"id": json.Number("9007199254740993"), "path": `D:\project\a 'quoted' path`}})
		if err := decodeReflex(artifact, &r); err != nil {
			t.Fatal(err)
		}
		if r.Observe != "js:"+source || r.arguments["id"] != json.Number("9007199254740993") || r.arguments["path"] != `D:\project\a 'quoted' path` {
			t.Fatalf("compiler changed source or argument values: %+v", r)
		}
	}
}
