package config

import (
	"fmt"
	"strings"
)

// ResolveOutputFormat gives every host the same aliases and accepted values.
func ResolveOutputFormat(option *Option) error {
	format := strings.ToLower(strings.TrimSpace(option.OutputFormat))
	if option.JSON {
		format = "json"
	}
	if format == "" {
		format = "text"
	}
	switch format {
	case "text", "json", "stream-json":
		option.OutputFormat = format
		return nil
	default:
		return fmt.Errorf("unsupported --output-format %q: use text, json, or stream-json", format)
	}
}

func validateExecutionOptions(option *Option) error {
	if option.Timeout < 0 {
		return fmt.Errorf("--timeout must be nonnegative (0 disables it)")
	}
	return ResolveOutputFormat(option)
}
