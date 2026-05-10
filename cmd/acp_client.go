package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chainreactors/aiscan/pkg/acp"
	acpclient "github.com/chainreactors/aiscan/pkg/acp/client"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func runACPClientCommand(ctx context.Context, mode runMode, option *Option, args acpClientArgs, logger telemetry.Logger) error {
	acpURL := option.ACPURL
	if acpURL == "" {
		acpURL = "http://127.0.0.1:8765"
	}
	client, err := acpclient.NewClient(acpURL, "")
	if err != nil {
		return fmt.Errorf("connect to ACP server: %w", err)
	}

	switch mode {
	case runModeACPSpaces:
		return runACPSpaces(ctx, client, option)
	case runModeACPMessages:
		return runACPMessages(ctx, client, option, args)
	case runModeACPContext:
		return runACPContext(ctx, client, option, args)
	case runModeACPNodes:
		return runACPNodes(ctx, client, option, args)
	default:
		return fmt.Errorf("unknown acp mode: %s", mode)
	}
}

func runACPSpaces(ctx context.Context, client *acpclient.Client, option *Option) error {
	spaces, err := client.ListSpaces(ctx)
	if err != nil {
		return err
	}
	if option.ACPJSON {
		return writeJSONOutput(spaces)
	}
	if len(spaces) == 0 {
		fmt.Fprintln(os.Stderr, "no spaces found")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tNAME\tNODES\tMESSAGES\n")
	for _, s := range spaces {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\n", s.ID, s.Name, len(s.Nodes), s.MessageCount)
	}
	return w.Flush()
}

func runACPMessages(ctx context.Context, client *acpclient.Client, option *Option, args acpClientArgs) error {
	space, err := client.ResolveSpace(ctx, args.Space)
	if err != nil {
		return err
	}
	messages, err := client.ReadPublic(ctx, space.ID, acp.ReadOptions{})
	if err != nil {
		return err
	}
	if option.ACPJSON {
		return writeJSONOutput(messages)
	}
	if len(messages) == 0 {
		fmt.Fprintf(os.Stderr, "no start messages in space %q\n", space.Name)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tSENDER\tCONTENT\n")
	for _, m := range messages {
		fmt.Fprintf(w, "%s\t%s\t%s\n", m.ID, m.Sender, contentPreview(m.Content, 80))
	}
	return w.Flush()
}

func runACPContext(ctx context.Context, client *acpclient.Client, option *Option, args acpClientArgs) error {
	space, err := client.ResolveSpace(ctx, args.Space)
	if err != nil {
		return err
	}
	messages, err := client.ReadPublic(ctx, space.ID, acp.ReadOptions{MessageID: args.MessageID})
	if err != nil {
		return err
	}
	if option.ACPJSON {
		return writeJSONOutput(messages)
	}
	if len(messages) == 0 {
		fmt.Fprintf(os.Stderr, "no messages in context of %s\n", args.MessageID)
		return nil
	}
	for _, m := range messages {
		marker := " "
		if m.ID == args.MessageID {
			marker = "*"
		}
		fmt.Printf("%s [%s] %s:\n  %s\n", marker, m.ID, m.Sender, contentPreview(m.Content, 120))
	}
	return nil
}

func runACPNodes(ctx context.Context, client *acpclient.Client, option *Option, args acpClientArgs) error {
	if args.Space != "" {
		space, err := client.ResolveSpace(ctx, args.Space)
		if err != nil {
			return err
		}
		if option.ACPJSON {
			return writeJSONOutput(space.Nodes)
		}
		if len(space.Nodes) == 0 {
			fmt.Fprintf(os.Stderr, "no nodes in space %q\n", space.Name)
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\tDESCRIPTION\n")
		for _, n := range space.Nodes {
			fmt.Fprintf(w, "%s\t%s\t%s\n", n.ID, n.Name, n.Description)
		}
		return w.Flush()
	}

	nodes, err := client.ListNodes(ctx)
	if err != nil {
		return err
	}
	if option.ACPJSON {
		return writeJSONOutput(nodes)
	}
	if len(nodes) == 0 {
		fmt.Fprintln(os.Stderr, "no nodes found")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tNAME\n")
	for _, n := range nodes {
		fmt.Fprintf(w, "%s\t%s\n", n.ID, n.Name)
	}
	return w.Flush()
}

func contentPreview(content map[string]any, maxLen int) string {
	if text, ok := content["text"].(string); ok {
		if len(text) > maxLen {
			return text[:maxLen] + "..."
		}
		return text
	}
	data, _ := json.Marshal(content)
	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func writeJSONOutput(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
