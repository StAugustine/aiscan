package jev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var bindingSchemas = struct {
	sync.Mutex
	values map[string]*jsonschema.Schema
}{values: map[string]*jsonschema.Schema{}}

type localSchemas struct{}

func (localSchemas) Load(url string) (any, error) {
	return nil, fmt.Errorf("native schema has an unavailable external reference %q", url)
}

// Compile only registered schemas; validation never fetches schema URLs.
func validateBindingSchema(candidate binding, capabilities map[string]any) error {
	tools, _ := capabilities["tools"].([]any)
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if tool["name"] != candidate.Name {
			continue
		}
		doc := tool["input_schema"]
		if doc == nil { // A native tool without an input schema imposes no extra constraints.
			return nil
		}
		key := digest(doc)
		bindingSchemas.Lock()
		cached, exists := bindingSchemas.values[key]
		bindingSchemas.Unlock()
		if !exists {
			compiler := jsonschema.NewCompiler()
			compiler.UseLoader(localSchemas{})
			url := "urn:jev:tool:" + key
			if err := compiler.AddResource(url, doc); err != nil {
				return fmt.Errorf("native schema: %w", err)
			}
			schema, err := compiler.Compile(url)
			if err != nil {
				return fmt.Errorf("native schema: %w", err)
			}
			bindingSchemas.Lock()
			if len(bindingSchemas.values) >= 128 {
				// Tool schemas may change across many sessions. Keep the cache
				// bounded; eviction changes compilation cost, never validation.
				clear(bindingSchemas.values)
			}
			bindingSchemas.values[key] = schema
			bindingSchemas.Unlock()
			cached = schema
		}
		var arguments any
		decoder := json.NewDecoder(bytes.NewReader(candidate.Arguments))
		decoder.UseNumber()
		if err := decoder.Decode(&arguments); err != nil {
			return err
		}
		return cached.Validate(arguments)
	}
	return fmt.Errorf("unknown native tool %q", candidate.Name)
}
