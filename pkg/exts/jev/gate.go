package jev

import (
	aop "github.com/chainreactors/cyber/aop"
	"math"
)

// Admission policy is fixed in this implementation, not an unvalidated set of
// tuning knobs. The window and minimum counts are shared by learner and gate.
const (
	validationWindow    = 32
	validationDefers    = 8
	validationTasks     = 3
	validationPositives = 8
)

func passes(checks []check) bool {
	if len(checks) < validationWindow {
		return false
	}
	tasks := map[string]bool{}
	agree, self, deferred, positive := 0, 0, 0, 0
	var jms, lms int64
	var jc, lc float64
	for _, c := range checks {
		if c.Unsafe || c.JEVCost < 0 || c.L2Cost < 0 || math.IsNaN(c.JEVCost) || math.IsNaN(c.L2Cost) || math.IsInf(c.JEVCost, 0) || math.IsInf(c.L2Cost, 0) {
			return false
		}
		tasks[c.Task] = true
		if c.Agree {
			agree++
		}
		if c.SelfAgree {
			self++
		}
		if c.Deferred {
			deferred++
		}
		if !c.Deferred && c.Agree {
			positive++
			jms += c.JEVMS
			lms += c.L2MS
			jc += c.JEVCost
			lc += c.L2Cost
		}
	}
	return len(tasks) >= validationTasks && deferred >= validationDefers && positive >= validationPositives && agree >= self && jms < lms && jc < lc
}

func cost(usage *aop.TokenUsage, prices map[string]float64) float64 {
	if usage == nil || prices == nil || usage.Detail["usage_missing"] > 0 {
		return -1
	}
	for _, key := range []string{"input", "output", "cache_read"} {
		v, ok := prices[key]
		if !ok || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return -1
		}
	}
	cache := min(usage.InputTokens, usage.Detail["cache_read"])
	// Cache writes need their own price; they are not DeepSeek cache misses.
	write := min(usage.InputTokens-cache, usage.Detail["cache_write"])
	writePrice := prices["input"]
	if write > 0 {
		var ok bool
		writePrice, ok = prices["cache_write"]
		if !ok || writePrice < 0 || math.IsNaN(writePrice) || math.IsInf(writePrice, 0) {
			return -1
		}
	}
	return (float64(usage.InputTokens-cache-write)*prices["input"] + float64(cache)*prices["cache_read"] + float64(write)*writePrice + float64(usage.OutputTokens)*prices["output"]) / 1e6
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
