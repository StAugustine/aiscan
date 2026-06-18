package arsenal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	crtm "github.com/chainreactors/crtm/pkg"
	"github.com/chainreactors/crtm/pkg/registry"

	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type arsenalArgs struct {
	Action  string `json:"action"           jsonschema:"description=Operation to perform,enum=search,enum=install,enum=update,enum=remove,enum=info,enum=list,enum=releases,enum=add"`
	Name    string `json:"name,omitempty"    jsonschema:"description=Tool name (for install/update/remove/info/releases)"`
	Query   string `json:"query,omitempty"   jsonschema:"description=Search keywords (for search action)"`
	Version string `json:"version,omitempty" jsonschema:"description=Specific version (default: latest)"`
	Repo    string `json:"repo,omitempty"    jsonschema:"description=GitHub owner/repo (for add action, e.g. ffuf/ffuf)"`
	Pattern string `json:"pattern,omitempty" jsonschema:"description=Asset name pattern for add (e.g. {name}_{version}_{os}_{arch}.tar.gz)"`
}

// ArsenalTool exposes the crtm package manager to the LLM agent.
type ArsenalTool struct {
	mgr    *crtm.Manager
	logger telemetry.Logger
}

func NewArsenalTool(logger telemetry.Logger) (*ArsenalTool, error) {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".aiscan", "arsenal")

	mgr, err := crtm.NewManager(crtm.ManagerOption{
		BinPath:    filepath.Join(base, "bin"),
		ConfigPath: filepath.Join(base, "config.yaml"),
	})
	if err != nil {
		return nil, fmt.Errorf("init arsenal: %w", err)
	}

	binPath := mgr.BinPath()
	os.MkdirAll(binPath, 0o755)
	if path := os.Getenv("PATH"); !strings.Contains(path, binPath) {
		os.Setenv("PATH", binPath+string(os.PathListSeparator)+path)
	}

	return &ArsenalTool{mgr: mgr, logger: logger}, nil
}

func (t *ArsenalTool) Name() string { return "arsenal" }

func (t *ArsenalTool) Description() string {
	return `Security tool package manager. Search, install, update, and remove CLI tools from chainreactors, projectdiscovery, and any GitHub repo. Installed tools become immediately available via bash.`
}

func (t *ArsenalTool) Definition() commands.ToolDefinition {
	return commands.ToolDef("arsenal", t.Description(), arsenalArgs{})
}

func (t *ArsenalTool) Execute(ctx context.Context, arguments string) (commands.ToolResult, error) {
	args, err := commands.ParseArgs[arsenalArgs](arguments)
	if err != nil {
		return commands.ToolResult{}, err
	}
	args.Action = strings.TrimSpace(strings.ToLower(args.Action))
	args.Name = strings.TrimSpace(args.Name)
	args.Query = strings.TrimSpace(args.Query)

	switch args.Action {
	case "search":
		return t.search(args.Query)
	case "list":
		return t.list()
	case "info":
		return t.info(args.Name)
	case "install":
		return t.install(args.Name, args.Version)
	case "update":
		return t.update(args.Name, args.Version)
	case "remove":
		return t.remove(args.Name)
	case "releases":
		return t.releases(args.Name)
	case "add":
		return t.add(args.Repo, args.Name, args.Pattern)
	default:
		return commands.ErrorResult(fmt.Sprintf("unknown action %q; valid: search, list, info, install, update, remove, releases, add", args.Action)), nil
	}
}

// --- actions ---

func (t *ArsenalTool) search(query string) (commands.ToolResult, error) {
	if query == "" {
		return commands.ErrorResult("query is required for search"), nil
	}
	results := t.mgr.Search(query)
	if len(results) == 0 {
		return commands.TextResult(fmt.Sprintf("No tools found for %q", query)), nil
	}
	return commands.TextResult(formatEntryList(results, t.mgr)), nil
}

func (t *ArsenalTool) list() (commands.ToolResult, error) {
	return commands.TextResult(formatEntryList(t.mgr.ListTools(), t.mgr)), nil
}

func (t *ArsenalTool) info(name string) (commands.ToolResult, error) {
	if name == "" {
		return commands.ErrorResult("name is required for info"), nil
	}
	info, err := t.mgr.GetToolInfo(name)
	if err != nil {
		return commands.ErrorResult(err.Error()), nil
	}
	return commands.TextResult(formatToolInfo(info)), nil
}

func (t *ArsenalTool) install(name, version string) (commands.ToolResult, error) {
	if name == "" {
		return commands.ErrorResult("name is required for install"), nil
	}

	// Idempotent: if already installed, report success with current version.
	if t.mgr.IsInstalled(name) {
		ver := t.mgr.InstalledVersion(name)
		return commands.TextResult(fmt.Sprintf("%s already installed (%s). Use update to refresh.", name, displayVer(ver))), nil
	}

	var err error
	if version != "" {
		err = t.mgr.InstallVersion(name, version)
	} else {
		err = t.mgr.InstallTool(name)
	}
	if err != nil {
		return commands.ErrorResult(fmt.Sprintf("install %s: %s", name, err)), nil
	}

	ver := t.mgr.InstalledVersion(name)
	result := fmt.Sprintf("Installed %s (%s). Available via bash.", name, displayVer(ver))
	if entry, ok := t.mgr.Catalog().Find(name); ok {
		if entry.DocsURL != "" {
			result += "\nDocs: " + entry.DocsURL
		}
		if entry.Hint != "" {
			result += "\nHint: " + entry.Hint
		}
	}
	return commands.TextResult(result), nil
}

func (t *ArsenalTool) update(name, version string) (commands.ToolResult, error) {
	if name == "" {
		return commands.ErrorResult("name is required for update"), nil
	}

	var err error
	if version != "" {
		err = t.mgr.InstallVersion(name, version)
	} else {
		err = t.mgr.UpdateTool(name)
	}
	if err != nil {
		return commands.ErrorResult(fmt.Sprintf("update %s: %s", name, err)), nil
	}

	ver := t.mgr.InstalledVersion(name)
	result := fmt.Sprintf("Updated %s (%s).", name, displayVer(ver))
	if entry, ok := t.mgr.Catalog().Find(name); ok {
		if entry.DocsURL != "" {
			result += "\nDocs: " + entry.DocsURL
		}
		if entry.Hint != "" {
			result += "\nHint: " + entry.Hint
		}
	}
	return commands.TextResult(result), nil
}

func (t *ArsenalTool) remove(name string) (commands.ToolResult, error) {
	if name == "" {
		return commands.ErrorResult("name is required for remove"), nil
	}
	if !t.mgr.IsInstalled(name) {
		return commands.TextResult(fmt.Sprintf("%s is not installed.", name)), nil
	}
	if err := t.mgr.RemoveTool(name); err != nil {
		return commands.ErrorResult(fmt.Sprintf("remove %s: %s", name, err)), nil
	}
	return commands.TextResult(fmt.Sprintf("Removed %s.", name)), nil
}

func (t *ArsenalTool) releases(name string) (commands.ToolResult, error) {
	if name == "" {
		return commands.ErrorResult("name is required for releases"), nil
	}
	releases, err := t.mgr.ListReleases(name)
	if err != nil {
		return commands.ErrorResult(err.Error()), nil
	}
	data, _ := json.MarshalIndent(releases, "", "  ")
	return commands.TextResult(string(data)), nil
}

func (t *ArsenalTool) add(repo, name, pattern string) (commands.ToolResult, error) {
	if repo == "" {
		return commands.ErrorResult("repo is required for add (e.g. ffuf/ffuf)"), nil
	}
	if !strings.Contains(repo, "/") {
		return commands.ErrorResult("repo must be owner/repo format (e.g. ffuf/ffuf)"), nil
	}
	if pattern == "" {
		pattern = "{name}_{version}_{os}_{arch}.tar.gz"
	}
	entry := registry.ToolEntry{
		Name:         name,
		Repo:         repo,
		AssetPattern: pattern,
	}
	if entry.Name == "" {
		entry.Name = entry.RepoName()
	}
	added, err := t.mgr.AddCustomTool(entry)
	if err != nil {
		return commands.ErrorResult(fmt.Sprintf("add: %s", err)), nil
	}
	if !added {
		return commands.TextResult(fmt.Sprintf("%s already registered.", repo)), nil
	}
	return commands.TextResult(fmt.Sprintf("Added %s from %s. Use arsenal install %s to install.", entry.Name, repo, entry.Name)), nil
}

// --- helpers ---

func displayVer(v string) string {
	if v == "" || v == "installed" {
		return "installed"
	}
	return "v" + v
}

func formatEntryList(entries []registry.ToolEntry, mgr *crtm.Manager) string {
	if len(entries) == 0 {
		return "No tools found."
	}
	var sb strings.Builder
	var nInstalled int
	for _, e := range entries {
		ver := mgr.InstalledVersion(e.Name)
		var status string
		switch {
		case ver == "":
			status = "  "
		case ver == "installed":
			status = "* "
			nInstalled++
		default:
			status = "* v" + ver
			nInstalled++
		}
		desc := e.Description
		if desc == "" && len(e.Tags) > 0 {
			desc = strings.Join(e.Tags, ", ")
		}
		sb.WriteString(fmt.Sprintf("%-10s %-18s [%-18s] %s\n", status, e.Name, e.Org(), desc))
	}
	sb.WriteString(fmt.Sprintf("\n%d/%d installed", nInstalled, len(entries)))
	return sb.String()
}

func formatToolInfo(info crtm.ToolInfo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name:      %s\n", info.Name))
	sb.WriteString(fmt.Sprintf("Repo:      %s\n", info.Repo))
	sb.WriteString(fmt.Sprintf("Org:       %s\n", info.Org()))
	if info.Description != "" {
		sb.WriteString(fmt.Sprintf("Desc:      %s\n", info.Description))
	}
	if len(info.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("Tags:      %s\n", strings.Join(info.Tags, ", ")))
	}
	if info.Category != "" {
		sb.WriteString(fmt.Sprintf("Category:  %s\n", info.Category))
	}
	if info.DocsURL != "" {
		sb.WriteString(fmt.Sprintf("Docs:      %s\n", info.DocsURL))
	}
	if info.Hint != "" {
		sb.WriteString(fmt.Sprintf("Hint:      %s\n", info.Hint))
	}
	sb.WriteString(fmt.Sprintf("Installed: %v\n", info.Installed))
	if info.InstalledPath != "" {
		sb.WriteString(fmt.Sprintf("Path:      %s\n", info.InstalledPath))
	}
	if info.LatestVersion != "" {
		sb.WriteString(fmt.Sprintf("Latest:    %s\n", info.LatestVersion))
	}
	if info.LatestVersionErr != "" {
		sb.WriteString(fmt.Sprintf("Version check: %s\n", info.LatestVersionErr))
	}
	return sb.String()
}
