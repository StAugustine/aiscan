package tool

import (
	"encoding/json"
	"fmt"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/invopop/jsonschema"
)

// SchemaOf generates a tool's input schema from a Go struct.
func SchemaOf(proto any) *jsonschema.Schema {
	r := &jsonschema.Reflector{
		DoNotReference: true,
	}
	schema := r.Reflect(proto)

	schema.Version = ""
	schema.ID = ""
	schema.Definitions = nil
	return schema
}

// Def builds a complete Definition from a name,
// description, and an args struct prototype.
func Def(name, description string, argsProto any) *aop.ToolDefinition {
	schema, err := aop.JSONValue(SchemaOf(argsProto))
	if err != nil {
		schema, _ = aop.JSONValue(&jsonschema.Schema{Type: "object"})
	}
	return &aop.ToolDefinition{
		Type:        "function",
		Name:        name,
		Description: description,
		InputSchema: schema,
	}
}

// ParseArgs unmarshals the raw JSON arguments string into a typed struct.
func ParseArgs[T any](arguments string) (T, error) {
	var args T
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return args, fmt.Errorf("invalid arguments: %w", err)
	}
	return args, nil
}
