// Package agentcli implements `commentloop agent ...` commands for coding agents.
package agentcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hzhengzhi/commentloop/internal/agentclient"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("commentloop agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	urlFlag := fs.String("url", "", "Commentloop server URL (defaults to the discovery file or 127.0.0.1:4571)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: commentloop agent [--url URL] <command> [args]")
		fmt.Fprintln(os.Stderr, "Commands: list, info, read, write, search, changes, comments, reply, refresh")
		fmt.Fprintln(os.Stderr, "Examples: commentloop agent list --refresh")
		fmt.Fprintln(os.Stderr, "      commentloop agent read Ali/notes/todo.md")
		fmt.Fprintln(os.Stderr, "      printf '# hi' | commentloop agent write Ali/notes/todo.md")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return nil
	}
	client := agentclient.New(agentclient.DefaultURL(*urlFlag))
	switch rest[0] {
	case "list":
		return runList(client, rest[1:])
	case "info":
		return runInfo(client, rest[1:])
	case "read":
		return runRead(client, rest[1:])
	case "write":
		return runWrite(client, rest[1:])
	case "search":
		return runSearch(client, rest[1:])
	case "changes":
		return runChanges(client, rest[1:])
	case "comments":
		return runComments(client, rest[1:])
	case "reply":
		return runReply(client, rest[1:])
	case "refresh":
		if err := client.Refresh(); err != nil {
			return err
		}
		fmt.Println("refreshed")
		return nil
	case "help", "-h", "--help":
		fs.Usage()
		return nil
	default:
		return fmt.Errorf("Unknown agent command %q\nAvailable: list, info, read, write, search, changes, refresh", rest[0])
	}
}

func runList(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "Output JSON")
	refresh := fs.Bool("refresh", false, "Refresh remote directories first (may be slow)")
	query := fs.String("query", "", "Only show files whose paths match")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tree, err := client.Tree(*refresh)
	if err != nil {
		return err
	}
	tree.Files = filterFiles(tree.Files, *query)
	if *jsonOut {
		return printJSON(tree)
	}
	fmt.Printf("server: %s\n", client.BaseURL())
	for _, root := range tree.Roots {
		kind := root.Kind
		location := root.Path
		if root.Kind == "remote" {
			location = root.Remote + ":" + root.Path
		}
		fmt.Printf("\n[%s] %s  %s", kind, root.Name, location)
		if root.Primary {
			fmt.Print("  (primary)")
		}
		fmt.Println()
		files := filterFiles(root.Files, *query)
		if len(files) == 0 {
			fmt.Println("  (no Markdown files)")
			continue
		}
		for _, file := range files {
			fmt.Printf("  %s\t%s\t%s\n", file.Path, formatSize(file.Size), file.ModTime.Local().Format("2006-01-02 15:04"))
		}
	}
	if tree.Truncated {
		fmt.Println("\n(tree truncated)")
	}
	return nil
}

func runInfo(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent info", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := client.Config()
	if err != nil {
		return err
	}
	tree, err := client.Tree(false)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(map[string]any{"config": cfg, "roots": tree.Roots})
	}
	fmt.Printf("url: %s\nversion: %s\nprimary: %s\nroots: %d\n", client.BaseURL(), cfg.Version, cfg.Root, cfg.RootCount)
	if cfg.PreviewOnly {
		fmt.Println("mode: preview-only (writes are disabled)")
	}
	for _, root := range tree.Roots {
		location := root.Path
		if root.Kind == "remote" {
			location = root.Remote + ":" + root.Path
		}
		fmt.Printf("  %-9s %-24s %s (%d files)\n", root.Kind, root.Name, location, len(root.Files))
	}
	return nil
}

func runRead(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent read", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Output JSON")
	refresh := fs.Bool("refresh", false, "Refresh remote directories first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("Usage: commentloop agent read [--json] [--refresh] <path>")
	}
	doc, resolved, err := client.Read(fs.Arg(0), *refresh)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(map[string]any{
			"path": doc.Path, "rootID": resolved.RootID, "relPath": resolved.RelPath,
			"content": doc.Content, "modTime": doc.ModTime, "size": doc.Size,
		})
	}
	fmt.Print(doc.Content)
	return nil
}

func runWrite(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent write", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Output JSON")
	contentFlag := fs.String("content", "", "Document content; read from stdin if omitted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("Usage: printf '...' | commentloop agent write [--json] <path>")
	}
	content := *contentFlag
	contentSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "content" {
			contentSet = true
		}
	})
	if !contentSet {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		content = string(data)
	}
	doc, resolved, err := client.Write(fs.Arg(0), content)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(map[string]any{
			"path": doc.Path, "rootID": resolved.RootID, "relPath": resolved.RelPath,
			"size": doc.Size, "modTime": doc.ModTime,
		})
	}
	fmt.Printf("wrote %s (%s)\n", doc.Path, formatSize(doc.Size))
	return nil
}

func runSearch(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent search", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Output JSON")
	refresh := fs.Bool("refresh", false, "Refresh remote directories first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("Usage: commentloop agent search [--json] [--refresh] <query>")
	}
	files, err := client.Search(strings.Join(fs.Args(), " "), *refresh)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(files)
	}
	for _, file := range files {
		fmt.Printf("%s\t%s\t%s\n", file.Path, formatSize(file.Size), file.ModTime.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

func runChanges(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent changes", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Output JSON")
	refresh := fs.Bool("refresh", true, "Refresh remote directories first")
	since := fs.Duration("since", 30*time.Minute, "Time window, e.g. 10m, 2h, 24h")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, err := client.Changes(time.Now().Add(-*since), *refresh)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(files)
	}
	if len(files) == 0 {
		fmt.Printf("no Markdown files modified in the last %s\n", *since)
		return nil
	}
	for _, file := range files {
		fmt.Printf("%s\t%s\t%s\n", file.Path, formatSize(file.Size), file.ModTime.Local().Format("2006-01-02 15:04:05"))
	}
	return nil
}

func filterFiles(files []agentclient.FileEntry, query string) []agentclient.FileEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]agentclient.FileEntry, 0, len(files))
	for _, file := range files {
		if q == "" || strings.Contains(strings.ToLower(file.RootName+"/"+file.RelPath), q) {
			out = append(out, file)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func formatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/1024/1024)
}

func runComments(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent comments", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := ""
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	comments, err := client.ListComments(path)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(comments)
	}
	for _, comment := range comments {
		line := "?"
		if comment.Line > 0 {
			line = fmt.Sprint(comment.Line)
		}
		status := "open"
		if comment.Resolved {
			status = "resolved"
		}
		fmt.Printf("[%s] %s:%s  %s\n", comment.ID, comment.Path, line, status)
		fmt.Printf("  quote: %s\n", strings.ReplaceAll(comment.Quote, "\n", " "))
		if comment.GroupID != "" {
			fmt.Printf("  group: %s\n", comment.GroupID)
		}
		for _, message := range comment.Messages {
			fmt.Printf("  %s: %s\n", message.Author, strings.ReplaceAll(message.Body, "\n", " "))
			if message.Before != "" || message.After != "" {
				fmt.Printf("    before: %s\n    after:  %s\n",
					strings.ReplaceAll(message.Before, "\n", " "),
					strings.ReplaceAll(message.After, "\n", " "))
			}
		}
	}
	return nil
}

func runReply(client *agentclient.Client, args []string) error {
	fs := flag.NewFlagSet("agent reply", flag.ContinueOnError)
	path := fs.String("path", "", "Document path")
	message := fs.String("message", "", "Reply body")
	before := fs.String("before", "", "Before content")
	after := fs.String("after", "", "After content")
	jsonOut := fs.Bool("json", false, "Output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *path == "" || *message == "" {
		return fmt.Errorf("Usage: commentloop agent reply --path <document> --message <text> [--before ...] [--after ...] <commentID> (only users can resolve comments in the UI)")
	}
	comment, err := client.ReplyComment(*path, fs.Arg(0), *message, *before, *after, "", "")
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(comment)
	}
	fmt.Printf("replied to %s\n", comment.ID)
	return nil
}
