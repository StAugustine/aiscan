package session

import (
	"reflect"
	"testing"
)

func TestSessionIDsScopesLiveDescendantsAndLogicalRoot(t *testing.T) {
	runtime := &Runtime{sessions: map[string]*sessionState{
		"root":       {id: "physical-root"},
		"child":      {id: "child", parentSessionID: "physical-root"},
		"grandchild": {id: "grandchild", parentSessionID: "child"},
		"other":      {id: "other"},
	}}
	want := []string{"child", "grandchild", "physical-root"}
	for _, root := range []string{"root", "physical-root"} {
		if got := runtime.SessionIDs(root); !reflect.DeepEqual(got, want) {
			t.Fatalf("root=%s got=%v", root, got)
		}
	}
	if got := runtime.SessionIDs("absent"); len(got) != 0 {
		t.Fatal("unknown session obtained reviews")
	}
}
