package guardrail

import "testing"

func TestRedactTextPreservesArgumentStructure(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"curl --password 'dummy pass' https://target/", "curl --password [REDACTED] https://target/"},
		{"TOKEN=test-secret curl https://target/", "TOKEN=[REDACTED] curl https://target/"},
		{"curl --user 'audit:dummy pass' https://target/", "curl --user [REDACTED] https://target/"},
	} {
		if got := RedactText(test.input); got != test.want {
			t.Errorf("RedactText(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
