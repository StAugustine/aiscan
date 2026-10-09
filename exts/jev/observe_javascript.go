package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"mvdan.cc/sh/v3/syntax"
)

// The same pure JSON helpers can be serialized into an ordinary programmable
// read. They contain no tool names, resource access, or execution capability.
const observeHelpersJS = `
function bind(name, arguments, read) {
  if (typeof read !== "boolean") throw new Error("bind requires an explicit boolean read flag: true only for effect-free inspection/polling, false for effects");
  return {name:name, arguments:arguments, read:read};
}
function choices(items) {
  if (!Array.isArray(items) || items.length > 64) throw new Error("invalid finite choices");
  const out = {};
  items.forEach((item, i) => { if (item != null) out["c" + i] = item; });
  return out;
}
function quote(value) {
  if (typeof value !== "string" || value.indexOf("\u0000") !== -1) throw new Error("invalid argument string");
  if (/^[A-Za-z0-9_./:,-]+$/.test(value)) return value;
  return "$'" + value.replace(/\\/g,"\\\\").replace(/'/g,"\\'").replace(/[\x01-\x1f\x7f]/g,
    function(c) { return "\\x" + ("0" + c.charCodeAt(0).toString(16)).slice(-2); }) + "'";
}
function command(name, argv) { return {name:name, argv:argv}; }
`

// runReflexJS executes one ordinary function. Only the native JEV and Executor
// bridges can perform external work; every bridge receives and returns JSON.
func runReflexJS(ctx context.Context, reflex *Reflex, input, arguments map[string]any,
	judge func(jevapi.Request) (*jevapi.Response, error), execute func(binding) (map[string]any, error)) (map[string]any, error) {
	if reflex == nil || reflex.program == nil {
		return nil, fmt.Errorf("Reflex program has not been validated")
	}
	if err := checkRuntimeNumbers(arguments); err != nil {
		return nil, err
	}
	runtime := goja.New()
	stop := context.AfterFunc(ctx, func() { runtime.Interrupt(ctx.Err()) })
	defer stop()
	fail := func(err error) { runtime.Interrupt(err); panic(runtime.NewGoError(err)) }
	decode := func(value goja.Value, out any) error {
		if err := checkRuntimeNumbers(value.Export()); err != nil {
			return err
		}
		data, err := json.Marshal(value.Export())
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		return decoder.Decode(out)
	}
	export := func(value any) goja.Value {
		if err := checkRuntimeNumbers(value); err != nil {
			fail(err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			fail(err)
		}
		if err = runtime.Set("returned", string(data)); err != nil {
			fail(err)
		}
		output, err := runtime.RunString("JSON.parse(returned)")
		if err != nil {
			fail(err)
		}
		return output
	}
	// JSON numbers outside JavaScript's safe range must never silently become
	// a different identity. Native schemas are documentation, not task values.
	for key, value := range input {
		if key != "tools" && key != "commands" {
			if err := checkRuntimeNumbers(value); err != nil {
				return nil, err
			}
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	_ = runtime.Set("encoded", string(data))
	_, err = runtime.RunString(`const context=JSON.parse(encoded); const user=context.user,history=context.history,messages=context.messages;
const tools=context.tools,commands=context.commands,omitted_evidence=context.omitted_evidence;
Math.random=function(){throw new Error("randomness unavailable")};globalThis.Date=undefined;` + observeHelpersJS)
	if err != nil {
		return nil, err
	}
	_ = runtime.Set("jev", func(call goja.FunctionCall) goja.Value {
		var request jevapi.Request
		if err := decode(call.Argument(0), &request); err != nil {
			fail(fmt.Errorf("JEV request: %w", err))
		}
		if err := validateQuestions(request.Questions); err != nil {
			fail(err)
		}
		if len(request.State) == 0 {
			request.State = json.RawMessage(`{}`)
		}
		if len(request.State) > 32<<10 || !json.Valid(request.State) {
			fail(fmt.Errorf("invalid JEV state"))
		}
		if err := ctx.Err(); err != nil {
			fail(err)
		}
		response, err := judge(request)
		if err != nil {
			fail(err)
		}
		if response == nil {
			fail(handoffError{"missing JEV response"})
		}
		for id, q := range request.Questions {
			switch q.Type {
			case "choice":
				_, err = response.Choice(id, q)
			case "score":
				_, err = response.Score(id, q)
			case "noul":
				_, err = response.Noul(id)
			}
			if err != nil {
				fail(handoffError{"invalid JEV response: " + err.Error()})
			}
		}
		return export(response)
	})
	_ = runtime.Set("execute", func(call goja.FunctionCall) goja.Value {
		var candidate binding
		if err := decode(call.Argument(0), &candidate); err != nil {
			fail(fmt.Errorf("native arguments: %w", err))
		}
		var explicit struct {
			Read       *bool `json:"read"`
			Occurrence *int  `json:"occurrence"`
		}
		if err := decode(call.Argument(0), &explicit); err != nil || explicit.Read == nil {
			fail(fmt.Errorf("native call needs explicit read flag"))
		}
		if reflex.APIVersion == reflexABI && !candidate.Read && (candidate.Step == "" || explicit.Occurrence == nil) {
			fail(fmt.Errorf("effect requires an explicit step and occurrence"))
		}
		var err error
		candidate, err = prepareBinding(candidate)
		if err != nil {
			fail(err)
		}
		if err := validateBindingSchema(candidate, input); err != nil {
			fail(fmt.Errorf("native arguments: %w", err))
		}
		if err := ctx.Err(); err != nil {
			fail(err)
		}
		result, err := execute(candidate)
		if err != nil {
			fail(err)
		}
		return export(result)
	})
	_ = runtime.Set("quote", func(value string) (string, error) { return syntax.Quote(value, syntax.LangBash) })
	_ = runtime.Set("program", func(call goja.FunctionCall) goja.Value {
		name, ok := call.Argument(0).Export().(string)
		source := reflex.Readers[name]
		if !ok || source == "" || goja.IsNull(call.Argument(1)) || goja.IsUndefined(call.Argument(1)) || call.Argument(1).ToObject(runtime).ClassName() != "Array" {
			fail(fmt.Errorf("program requires a named reader and argument array"))
		}
		data, err := json.Marshal(call.Argument(1).Export())
		if err != nil {
			fail(err)
		}
		return runtime.ToValue("(function(){" + observeHelpersJS + "return (" + source + ").apply(null," + string(data) + ");})()")
	})
	value, err := runtime.RunProgram(reflex.program)
	if err != nil {
		return nil, err
	}
	function, ok := goja.AssertFunction(value)
	if !ok {
		return nil, fmt.Errorf("reflex must evaluate to a function")
	}
	value, err = function(goja.Undefined(), runtime.Get("context"), export(arguments))
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var result map[string]any
	if err = decode(value, &result); err != nil || result == nil {
		return nil, fmt.Errorf("reflex must return an object containing report or defer")
	}
	_, reported := result[report]
	reason, deferred := result[Defer].(string)
	_, hasDefer := result[Defer]
	if reported == hasDefer || (hasDefer && (!deferred || strings.TrimSpace(reason) == "")) {
		return nil, fmt.Errorf("reflex must return exactly one of report or defer")
	}
	if data, err := json.Marshal(result); err != nil || len(data) > 32<<10 {
		return nil, fmt.Errorf("reflex result exceeds budget")
	}
	return result, nil
}

func checkRuntimeNumbers(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var plain any
	if err = decoder.Decode(&plain); err != nil {
		return err
	}
	var check func(any) error
	check = func(value any) error {
		switch value := value.(type) {
		case json.Number:
			number, err := strconv.ParseFloat(string(value), 64)
			if err != nil || math.Abs(number) > 9007199254740991 {
				return handoffError{"numeric argument or result exceeds JavaScript's safe range; preserve the original value and use ordinary model handling"}
			}
		case map[string]any:
			for _, item := range value {
				if err := check(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range value {
				if err := check(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return check(plain)
}

func validateQuestions(questions map[string]jevapi.Question) error {
	if len(questions) == 0 || len(questions) > 40 {
		return fmt.Errorf("invalid JEV questions")
	}
	for id, q := range questions {
		if strings.TrimSpace(id) == "" || q.Instructions == nil {
			return fmt.Errorf("invalid JEV question")
		}
		encoded, _ := json.Marshal(q.Criteria)
		switch q.Type {
		case "choice":
			var options map[string]any
			if json.Unmarshal(encoded, &options) != nil || len(options) < 2 || len(options) > maxCandidates || options[Defer] == nil {
				return fmt.Errorf("choice requires finite options and defer")
			}
		case "score":
			var levels []any
			if json.Unmarshal(encoded, &levels) != nil || len(levels) < 2 || len(levels) > 10 {
				return fmt.Errorf("invalid score levels")
			}
		case "noul":
		default:
			return fmt.Errorf("unsupported JEV question type %q", q.Type)
		}
	}
	return nil
}

// Parsing never evaluates the reader or its native globals.
func validateReader(id, source string) error {
	parsed, err := parser.ParseFile(nil, "reader:"+id, "("+strings.TrimSpace(source)+")", 0)
	if err != nil {
		return fmt.Errorf("reader %q syntax: %w", id, err)
	}
	if len(parsed.Body) == 1 {
		if expression, ok := parsed.Body[0].(*ast.ExpressionStatement); ok {
			switch expression.Expression.(type) {
			case *ast.FunctionLiteral, *ast.ArrowFunctionLiteral:
				return nil
			}
		}
	}
	return fmt.Errorf("reader %q must be a function expression", id)
}
