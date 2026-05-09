package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/skills"
	goflags "github.com/jessevdk/go-flags"
)

const Version = "0.1.0"

type Option struct {
	LLMOptions     `group:"LLM Options"`
	ScannerOptions `group:"Scanner Options"`
	AgentOptions   `group:"Agent Options"`
	ACPOptions     `group:"ACP Options"`
	MiscOptions    `group:"Miscellaneous Options"`
}

type LLMOptions struct {
	Provider      string `long:"llm-provider" description:"LLM provider name (openai, deepseek, openrouter, ollama, etc.)"`
	BaseURL       string `long:"llm-base-url" description:"LLM API base URL"`
	APIKey        string `long:"llm-api-key" description:"LLM API key (or set env: OPENAI_API_KEY, AISCAN_API_KEY)"`
	Model         string `long:"llm-model" description:"LLM model name"`
	Proxy         string `long:"llm-proxy" description:"HTTP proxy for LLM API"`
	ProviderAlias string `long:"provider" description:"Alias for --llm-provider"`
	BaseURLAlias  string `long:"base-url" description:"Alias for --llm-base-url"`
	APIKeyAlias   string `long:"api-key" description:"Alias for --llm-api-key"`
	ModelAlias    string `long:"model" description:"Alias for --llm-model"`
	ProxyAlias    string `long:"proxy" description:"Alias for --llm-proxy"`
	AI            bool   `long:"ai" description:"Use the configured LLM to process direct scanner output with the relevant tool skill"`
}

type ScannerOptions struct {
	CyberhubURL  string `long:"cyberhub-url" description:"Cyberhub server URL for loading fingers/templates"`
	CyberhubKey  string `long:"cyberhub-key" description:"Cyberhub API key"`
	CyberhubMode string `long:"cyberhub-mode" description:"Cyberhub resource mode: merge or override"`
}

type AgentOptions struct {
	Prompt   string   `short:"p" long:"prompt" description:"Natural language task for the agent"`
	Inputs   []string `short:"i" long:"input" description:"Target input: IP, URL, IP:port, or CIDR. Can specify multiple"`
	Skills   []string `short:"s" long:"skill" description:"Embedded skill to apply. Can specify multiple"`
	TaskFile string   `long:"task-file" description:"File containing task description"`
	Loop     bool     `long:"loop" description:"Run as an ACP loop worker instead of local agent mode"`
	MaxTurns int      `long:"max-turns" description:"Maximum agent loop iterations" default:"50"`
	Timeout  int      `long:"timeout" description:"Overall timeout in seconds" default:"3600"`
}

type ACPOptions struct {
	ACPURL      string `long:"acp-url" description:"ACP server URL for agent tools"`
	ACPNodeID   string `long:"acp-node-id" description:"Existing ACP node id for agent tools"`
	ACPNodeName string `long:"acp-node-name" description:"ACP node name when auto-registering"`
	ACPDB       string `long:"acp-db" description:"ACP SQLite database path for 'aiscan acp serve'" default:"./acp.db"`
	Space       string `long:"space" description:"ACP space name for 'aiscan agent --loop'" default:"default"`
}

type MiscOptions struct {
	Debug   bool `long:"debug" description:"Enable debug logging"`
	Quiet   bool `short:"q" long:"quiet" description:"Quiet mode"`
	NoColor bool `long:"no-color" description:"Disable ANSI colors in scanner output"`
	Version bool `long:"version" description:"Print version and exit"`
}

type cliOptions struct {
	Option
	Agent   struct{}   `command:"agent" description:"Run the LLM agent"`
	ACP     acpCommand `command:"acp" description:"ACP server commands"`
	Scan    struct{}   `command:"scan" description:"Run the scan pipeline"`
	Gogo    struct{}   `command:"gogo" description:"Run gogo scanner"`
	Spray   struct{}   `command:"spray" description:"Run spray scanner"`
	Zombie  struct{}   `command:"zombie" description:"Run zombie weakpass scanner"`
	Neutron struct{}   `command:"neutron" description:"Run neutron POC scanner"`
}

type acpCommand struct {
	Serve struct{} `command:"serve" description:"Run the ACP HTTP server"`
}

type runMode string

const (
	runModeAgent     runMode = "agent"
	runModeACPServe  runMode = "acp serve"
	runModeScanner   runMode = "scanner"
	runModeNoCommand runMode = ""
)

type parsedCLI struct {
	Option      Option
	Mode        runMode
	ScannerArgs []string
	Help        bool
}

func AiScan() {
	parsed, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	option := parsed.Option
	if option.Version {
		fmt.Printf("aiscan v%s\n", Version)
		return
	}
	if parsed.Help {
		return
	}
	if parsed.Mode == runModeNoCommand {
		fmt.Fprintln(os.Stderr, "error: missing subcommand: use agent, acp serve, scan, gogo, spray, zombie, or neutron")
		os.Exit(1)
	}

	applyDefaults(&option)
	logger := telemetry.GlobalLogger(telemetry.LogConfig{Debug: option.Debug, Quiet: option.Quiet, Output: os.Stderr})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(option.Timeout)*time.Second)
	defer cancel()

	setupSignalHandler(cancel, logger)

	switch parsed.Mode {
	case runModeAgent:
		if err := runAgentMode(ctx, &option, logger); err != nil {
			logger.Errorf("agent failed: %s", err)
			os.Exit(1)
		}
	case runModeACPServe:
		if err := runACPServe(ctx, &option, logger); err != nil {
			logger.Errorf("acp server failed: %s", err)
			os.Exit(1)
		}
	case runModeScanner:
		if err := runDirectScannerMode(ctx, &option, parsed.ScannerArgs, logger); err != nil {
			logger.Errorf("scanner command failed: %s", err)
			os.Exit(1)
		}
	}
}

func parseCLI(args []string) (parsedCLI, error) {
	if scannerName, rootArgs, scannerRest, ok := splitScannerCommand(args); ok {
		return parseScannerCLI(scannerName, rootArgs, scannerRest)
	}

	var cli cliOptions
	parser := newCLIParser(&cli, parserOptionsForArgs(args))
	rest, err := parser.ParseArgs(args)
	if err != nil {
		if flagsErr, ok := err.(*goflags.Error); ok && flagsErr.Type == goflags.ErrHelp {
			if scannerName := firstCommandName(args, rootFlagValueArity); isScannerCommandName(scannerName) {
				option := cli.Option
				normalizeLLMAliases(&option)
				option.Timeout = 3600
				scannerArgs := append([]string{scannerName}, argsAfterCommand(args, scannerName)...)
				return parsedCLI{Option: option, Mode: runModeScanner, ScannerArgs: scannerArgs}, nil
			}
			printHelp(parser)
			return parsedCLI{Mode: runModeNoCommand, Help: true}, nil
		}
		return parsedCLI{}, err
	}

	option := cli.Option
	normalizeLLMAliases(&option)
	if cli.Version {
		return parsedCLI{Option: option, Mode: runModeNoCommand}, nil
	}

	mode := selectedMode(parser)
	if mode == runModeNoCommand {
		return parsedCLI{Option: option, Mode: runModeNoCommand}, nil
	}

	if mode == runModeScanner {
		scannerName := selectedScanner(parser)
		option.Timeout = 3600
		scannerRest, err := applyScannerRootArgs(rest, &option)
		if err != nil {
			return parsedCLI{}, err
		}
		scannerArgs := append([]string{scannerName}, scannerRest...)
		return parsedCLI{Option: option, Mode: mode, ScannerArgs: scannerArgs}, nil
	}
	return parsedCLI{Option: option, Mode: mode}, nil
}

func parseScannerCLI(scannerName string, rootArgs, scannerRest []string) (parsedCLI, error) {
	var manual Option
	filteredRootArgs, err := applyScannerCommandArgs(scannerName, rootArgs, &manual)
	if err != nil {
		return parsedCLI{}, err
	}
	var cli cliOptions
	parser := newCLIParser(&cli, goflags.Default&^goflags.PrintErrors)
	if scannerName == "scan" {
		parser = newCLIParser(&cli, (goflags.Default&^goflags.PrintErrors)|goflags.IgnoreUnknown)
	}
	if _, err := parser.ParseArgs(filteredRootArgs); err != nil {
		if flagsErr, ok := err.(*goflags.Error); ok && flagsErr.Type == goflags.ErrHelp {
			printHelp(parser)
			return parsedCLI{Mode: runModeNoCommand, Help: true}, nil
		}
		return parsedCLI{}, err
	}

	option := cli.Option
	normalizeLLMAliases(&option)
	mergeManualScannerOptions(&option, manual)
	if cli.Version {
		return parsedCLI{Option: option, Mode: runModeNoCommand}, nil
	}
	option.Timeout = 3600

	scannerArgs, err := applyScannerCommandArgs(scannerName, scannerRest, &option)
	if err != nil {
		return parsedCLI{}, err
	}
	return parsedCLI{
		Option:      option,
		Mode:        runModeScanner,
		ScannerArgs: append([]string{scannerName}, scannerArgs...),
	}, nil
}

func mergeManualScannerOptions(option *Option, manual Option) {
	option.Provider = resolveString(manual.Provider, option.Provider)
	option.BaseURL = resolveString(manual.BaseURL, option.BaseURL)
	option.APIKey = resolveString(manual.APIKey, option.APIKey)
	option.Model = resolveString(manual.Model, option.Model)
	option.Proxy = resolveString(manual.Proxy, option.Proxy)
	if manual.AI {
		option.AI = true
	}
	option.CyberhubURL = resolveString(manual.CyberhubURL, option.CyberhubURL)
	option.CyberhubKey = resolveString(manual.CyberhubKey, option.CyberhubKey)
	option.CyberhubMode = resolveString(manual.CyberhubMode, option.CyberhubMode)
	if manual.NoColor {
		option.NoColor = true
	}
	option.Prompt = resolveString(manual.Prompt, option.Prompt)
	option.TaskFile = resolveString(manual.TaskFile, option.TaskFile)
	if len(manual.Skills) > 0 {
		option.Skills = append(option.Skills, manual.Skills...)
	}
}

func normalizeLLMAliases(option *Option) {
	option.Provider = resolveString(option.Provider, option.ProviderAlias)
	option.BaseURL = resolveString(option.BaseURL, option.BaseURLAlias)
	option.APIKey = resolveString(option.APIKey, option.APIKeyAlias)
	option.Model = resolveString(option.Model, option.ModelAlias)
	option.Proxy = resolveString(option.Proxy, option.ProxyAlias)
}

func newCLIParser(cli *cliOptions, options goflags.Options) *goflags.Parser {
	parser := goflags.NewParser(cli, options)
	parser.SubcommandsOptional = true
	parser.Usage = `[OPTIONS] <command>

aiscan - Agentic Security Scanner powered by LLM

Commands:
  agent       Run the LLM agent
  acp serve   Run the ACP HTTP server
  scan        Run the scan pipeline
  gogo        Run gogo scanner
  spray       Run spray scanner
  zombie      Run zombie weakpass scanner
  neutron     Run neutron POC scanner

Examples:
  aiscan agent -p "find web services and check vulnerabilities" -i 192.168.1.0/24
  aiscan agent --llm-provider deepseek --llm-model deepseek-chat -p "enumerate services" -i 10.0.0.0/24
  aiscan agent --llm-provider ollama --llm-model llama3 --llm-base-url http://localhost:11434/v1 -p "check this host" -i http://target.com
  aiscan scan -i 127.0.0.1 --mode quick --verify=high --llm-api-key KEY --llm-model gpt-4o
  aiscan scan -i 192.168.1.0/24 --mode full
  aiscan acp serve
  aiscan agent --loop -p "localhost web scanner" -s aiscan --space case-1`
	return parser
}

func parserOptionsForArgs(args []string) goflags.Options {
	options := goflags.Options(goflags.Default &^ goflags.PrintErrors)
	if len(args) == 0 {
		return options
	}
	if isScannerCommandName(firstCommandName(args, rootFlagValueArity)) {
		options |= goflags.IgnoreUnknown
	}
	return options
}

func splitScannerCommand(args []string) (string, []string, []string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isScannerCommandName(arg) {
			return arg, append([]string(nil), args[:i]...), append([]string(nil), args[i+1:]...), true
		}
		if shouldSkipRootFlagValue(arg) && i+1 < len(args) {
			i++
		}
	}
	return "", nil, nil, false
}

func shouldSkipRootFlagValue(arg string) bool {
	key, _, hasValue := strings.Cut(arg, "=")
	if hasValue {
		return false
	}
	return rootFlagValueArity[key] > 0
}

func firstCommandName(args []string, valueArity map[string]int) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			key, _, hasValue := strings.Cut(arg, "=")
			if !hasValue {
				i += valueArity[key]
			}
			continue
		}
		return arg
	}
	return ""
}

type knownFlag struct {
	names []string
	arity int // 0 for bool, 1 for value
	apply func(opt *Option, val string)
}

var scannerKnownFlags = []knownFlag{
	{names: []string{"--cyberhub-url"}, arity: 1, apply: func(o *Option, v string) { o.CyberhubURL = v }},
	{names: []string{"--cyberhub-key"}, arity: 1, apply: func(o *Option, v string) { o.CyberhubKey = v }},
	{names: []string{"--cyberhub-mode"}, arity: 1, apply: func(o *Option, v string) { o.CyberhubMode = v }},
	{names: []string{"--no-color"}, arity: 0, apply: func(o *Option, _ string) { o.NoColor = true }},
	{names: []string{"--ai"}, arity: 0, apply: func(o *Option, v string) {
		if v != "" {
			o.AI = truthyFlagValue(v)
		} else {
			o.AI = true
		}
	}},
	{names: []string{"--prompt"}, arity: 1, apply: func(o *Option, v string) { o.Prompt = v }},
	{names: []string{"--task-file"}, arity: 1, apply: func(o *Option, v string) { o.TaskFile = v }},
	{names: []string{"--skill"}, arity: 1, apply: func(o *Option, v string) { o.Skills = append(o.Skills, v) }},
	{names: []string{"--llm-provider", "--provider"}, arity: 1, apply: func(o *Option, v string) { o.Provider = v }},
	{names: []string{"--llm-base-url", "--base-url"}, arity: 1, apply: func(o *Option, v string) { o.BaseURL = v }},
	{names: []string{"--llm-api-key", "--api-key"}, arity: 1, apply: func(o *Option, v string) { o.APIKey = v }},
	{names: []string{"--llm-model", "--model"}, arity: 1, apply: func(o *Option, v string) { o.Model = v }},
	{names: []string{"--llm-proxy", "--proxy"}, arity: 1, apply: func(o *Option, v string) { o.Proxy = v }},
	{names: []string{"--max-turns"}, arity: 1, apply: func(o *Option, v string) {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			o.MaxTurns = n
		}
	}},
}

var rootFlagValueArity = buildRootFlagValueArity()

func buildRootFlagValueArity() map[string]int {
	m := make(map[string]int, len(scannerKnownFlags)*2)
	for _, f := range scannerKnownFlags {
		for _, name := range f.names {
			m[name] = f.arity
		}
	}
	return m
}

func argsAfterCommand(args []string, command string) []string {
	for i, arg := range args {
		if arg == command {
			return append([]string(nil), args[i+1:]...)
		}
	}
	return nil
}

func isScannerCommandName(name string) bool {
	switch name {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return true
	}
	return false
}

func selectedMode(parser *goflags.Parser) runMode {
	active := parser.Active
	if active == nil {
		return runModeNoCommand
	}
	if active.Name == "acp" && active.Active != nil && active.Active.Name == "serve" {
		return runModeACPServe
	}
	switch active.Name {
	case "agent":
		return runModeAgent
	case "serve":
		return runModeACPServe
	case "scan", "gogo", "spray", "zombie", "neutron":
		return runModeScanner
	}
	return runModeNoCommand
}

func selectedScanner(parser *goflags.Parser) string {
	active := parser.Active
	if active == nil {
		return ""
	}
	switch active.Name {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return active.Name
	}
	return ""
}

func applyScannerRootArgs(args []string, option *Option) ([]string, error) {
	return applyScannerCommandArgs("", args, option)
}

func applyScannerCommandArgs(_ string, args []string, option *Option) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		matched := false
		for _, f := range scannerKnownFlags {
			if !containsString(f.names, key) {
				continue
			}
			matched = true
			if f.arity == 0 {
				if hasValue {
					f.apply(option, value)
				} else {
					f.apply(option, "")
				}
			} else {
				v, err := flagValue(arg, hasValue, value, args, &i)
				if err != nil {
					return nil, err
				}
				f.apply(option, v)
			}
			break
		}
		if !matched {
			out = append(out, arg)
		}
	}
	return out, nil
}

func flagValue(arg string, hasValue bool, value string, args []string, i *int) (string, error) {
	if hasValue {
		return value, nil
	}
	if *i+1 >= len(args) {
		return "", fmt.Errorf("%s requires a value", arg)
	}
	*i++
	return args[*i], nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func truthyFlagValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func hasAgentOneShotInput(opt *Option) bool {
	if strings.TrimSpace(opt.Prompt) != "" || opt.TaskFile != "" || len(opt.Inputs) > 0 {
		return true
	}
	return !stdinIsTerminal()
}

func stdinIsTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func resolveTask(opt *Option) (string, error) {
	prompt := strings.TrimSpace(opt.Prompt)
	if prompt != "" {
		if len(opt.Inputs) > 0 {
			return fmt.Sprintf("%s\n\nTargets:\n%s", prompt, formatInputs(opt.Inputs)), nil
		}
		return prompt, nil
	}

	if opt.TaskFile != "" {
		data, err := os.ReadFile(opt.TaskFile)
		if err != nil {
			return "", fmt.Errorf("read task file: %w", err)
		}
		task := strings.TrimSpace(string(data))
		if len(opt.Inputs) > 0 {
			return fmt.Sprintf("%s\n\nTargets:\n%s", task, formatInputs(opt.Inputs)), nil
		}
		return task, nil
	}

	if !stdinIsTerminal() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		task := strings.TrimSpace(string(data))
		if task != "" {
			if len(opt.Inputs) > 0 {
				return fmt.Sprintf("%s\n\nTargets:\n%s", task, formatInputs(opt.Inputs)), nil
			}
			return task, nil
		}
	}

	if len(opt.Inputs) > 0 {
		return fmt.Sprintf("Scan the provided targets using scan and summarize findings.\n\nTargets:\n%s", formatInputs(opt.Inputs)), nil
	}

	return "", fmt.Errorf("no prompt specified: use -p, --prompt, --task-file, or pipe via stdin")
}

func isDirectScannerJSONOutput(rest []string) bool {
	if !isDirectScannerCommand(rest) {
		return false
	}

	for _, arg := range rest[1:] {
		if arg == "-j" || arg == "--json" {
			return true
		}
		if strings.HasPrefix(arg, "--json=") {
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--json=")))
			return value != "false" && value != "0" && value != "no"
		}
	}
	return false
}

func isDirectScannerCommand(rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return true
	}
	return false
}

func shouldStreamScannerOutput(rest []string) bool {
	if len(rest) == 0 || rest[0] != "scan" {
		return false
	}
	if isDirectScannerJSONOutput(rest) {
		return false
	}
	for _, arg := range rest[1:] {
		if arg == "--report" {
			return false
		}
		if strings.HasPrefix(arg, "--report=") {
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--report=")))
			if value != "false" && value != "0" && value != "no" {
				return false
			}
		}
	}
	return true
}

func hasScannerFlag(args []string, long string) bool {
	for _, arg := range args {
		if arg == long || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}

func applySelectedSkills(text string, selected []string, store *skills.Store) (string, error) {
	if len(selected) == 0 {
		return text, nil
	}
	var sb strings.Builder
	for _, name := range selected {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		skill, ok := store.ByName(name)
		if !ok {
			return "", fmt.Errorf("unknown skill %q", name)
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(skills.FormatInvocation(skill, ""))
	}
	if strings.TrimSpace(text) != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(strings.TrimSpace(text))
	}
	return sb.String(), nil
}

func formatInputs(inputs []string) string {
	var sb strings.Builder
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(input)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func setupSignalHandler(cancel context.CancelFunc, logger telemetry.Logger) {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sigCount := 0
		for range sigChan {
			sigCount++
			if sigCount == 1 {
				logger.Warnf("signal=shutdown action=finish_current_turn")
				cancel()
			} else {
				logger.Warnf("signal=shutdown action=force_exit")
				os.Exit(1)
			}
		}
	}()
}

func printHelp(parser *goflags.Parser) {
	parser.WriteHelp(os.Stdout)
}
