package harness_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSharedLLMEnvironmentAndLegacyFallback(t *testing.T) {
	for _, suffix := range []string{"API_KEY", "BASE_URL", "MODEL", "PROVIDER"} {
		t.Setenv("CYBER_"+suffix, "shared-"+suffix)
		t.Setenv("CYBER_HARNESS_LLM_"+suffix, "legacy-"+suffix)
	}
	for _, shared := range []bool{true, false} {
		if !shared {
			for _, suffix := range []string{"API_KEY", "BASE_URL", "MODEL", "PROVIDER"} {
				t.Setenv("CYBER_"+suffix, " ")
			}
		}
		for _, include := range []bool{false, true} {
			env := strings.Join(testEnvironment(include), "\n")
			for _, suffix := range []string{"API_KEY", "BASE_URL", "MODEL", "PROVIDER"} {
				prefix := "legacy-"
				if shared {
					prefix = "shared-"
				}
				if strings.Contains(env, "CYBER_"+suffix+"="+prefix+suffix) != include {
					t.Fatalf("include=%v shared=%v suffix=%s", include, shared, suffix)
				}
			}
			if strings.Contains(env, "CYBER_HARNESS_LLM_") {
				t.Fatal("legacy credentials leaked into child environment")
			}
		}
	}
}

func TestRedactionCoversSharedAndLegacyKeys(t *testing.T) {
	t.Setenv("CYBER_API_KEY", `shared-"key`)
	t.Setenv("CYBER_HARNESS_LLM_API_KEY", "old-test-key")
	encoded, _ := json.Marshal(`shared-"key`)
	got := string(redactSecrets([]byte(`shared-"key old-test-key ` + string(encoded))))
	if strings.Contains(got, "shared-") || strings.Contains(got, "old-test-key") || strings.Count(got, "[REDACTED]") != 3 {
		t.Fatalf("redaction failed: %s", got)
	}
}
