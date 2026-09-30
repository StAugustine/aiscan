package jev

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestLiveChoiceBatch(t *testing.T) {
	if os.Getenv("JEV_LIVE") != "1" {
		t.Skip("set JEV_LIVE=1 and TYPESAFE_API_KEY for a real finite-choice request")
	}
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Fatal("TYPESAFE_API_KEY is required")
	}
	c := New(key, DefaultModel, 15*time.Second)
	defer c.Close()
	questions := map[string]Question{
		"entry":   {Type: "choice", Instructions: "Choose inspect only if the state contains a visible enabled Continue control and the user requested continuing. Otherwise defer.", Criteria: map[string]string{"inspect": "A permitted Continue control exists", "defer": "No permitted control"}},
		"inspect": {Type: "choice", Instructions: "Assuming continuation is permitted, select the currently enabled Continue control. Defer if missing.", Criteria: map[string]string{"continue": "Click button n1, visible, enabled, label Continue", "cancel": "Click button n2, label Cancel", "defer": "No eligible action"}},
	}
	start := time.Now()
	out, err := c.Exchange(t.Context(), Request{State: json.RawMessage(`{"task":"Continue to the next stage","controls":[{"id":"n1","label":"Continue","visible":true,"enabled":true},{"id":"n2","label":"Cancel","visible":true,"enabled":true}]}`), Questions: questions})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := out.Choice("entry", questions["entry"])
	if err != nil || entry != "inspect" {
		t.Fatalf("entry=%s error=%v", entry, err)
	}
	chosen, err := out.Choice(entry, questions[entry])
	if err != nil || chosen != "continue" {
		t.Fatalf("choice=%s error=%v", chosen, err)
	}
	t.Logf("real JEV batch: model=%s elapsed=%s attempts=%d usage=%v", out.Model, time.Since(start), out.Attempts, out.TokenUsage())
}
