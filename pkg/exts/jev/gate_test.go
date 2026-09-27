package jev

import (
	"fmt"
	"math"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func passingChecks() []check {
	checks := make([]check, 32)
	for i := range checks {
		checks[i] = check{Task: fmt.Sprint(i % 4), Agree: true, SelfAgree: true, Deferred: i < 8, JEVMS: 2, L2MS: 20, JEVCost: 0.01, L2Cost: 0.1}
	}
	return checks
}
func TestActivationRequiresMeasuredSafetyAndValue(t *testing.T) {
	if !passes(passingChecks()) {
		t.Fatal("valid measurements rejected")
	}
	for name, change := range map[string]func([]check){
		"false takeover":             func(c []check) { c[0].Unsafe = true },
		"less agreement than replay": func(c []check) { c[10].Agree = false },
		"unknown price":              func(c []check) { c[0].JEVCost = -1 },
		"nan price":                  func(c []check) { c[0].L2Cost = math.NaN() },
		"not faster": func(c []check) {
			for i := range c {
				c[i].JEVMS = c[i].L2MS
			}
		},
		"not cheaper": func(c []check) {
			for i := range c {
				c[i].JEVCost = c[i].L2Cost
			}
		},
		"one task": func(c []check) {
			for i := range c {
				c[i].Task = "one"
			}
		},
		"missing defer cases": func(c []check) { c[0].Deferred = false },
	} {
		t.Run(name, func(t *testing.T) {
			c := passingChecks()
			change(c)
			if passes(c) {
				t.Fatal("invalid activation accepted")
			}
		})
	}
}
func TestCostDoesNotTreatMissingUsageOrCachePriceAsFree(t *testing.T) {
	p := map[string]float64{"input": 2, "output": 8, "cache_read": 0.5}
	u := &aop.TokenUsage{InputTokens: 100, OutputTokens: 10, Detail: map[string]uint64{"cache_read": 60, "cache_miss": 40, "reasoning": 5}}
	if got := cost(u, p); math.Abs(got-0.00019) > 1e-10 {
		t.Fatal(got)
	}
	u.Detail["cache_write"] = 10
	if cost(u, p) >= 0 {
		t.Fatal("unpriced write accepted")
	}
	p["cache_write"] = 3
	if cost(u, p) < 0 {
		t.Fatal("priced write rejected")
	}
	u.Detail["usage_missing"] = 1
	if cost(u, p) >= 0 {
		t.Fatal("retry of unknown cost accepted")
	}
	if cost(nil, p) >= 0 {
		t.Fatal("missing usage accepted")
	}
}
