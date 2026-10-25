// Package agentmcp exposes mdlive's local/remote Markdown workspace as an MCP
// stdio server for coding agents.
package agentmcp

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"mdlive/internal/agentclient"
)

const protocolVersion = "2024-11-05"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type server struct {
	client      *agentclient.Client
	explicitURL string
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func Run(args []string, version string) error {
	fs := flag.NewFlagSet("mdlive mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	urlFlag := fs.String("url", "", "mdlive server URL（默认读取 discovery 文件或 127.0.0.1:4571）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s := &server{
		client:      agentclient.New(agentclient.DefaultURL(*urlFlag)),
		explicitURL: *urlFlag,
	}
	fmt.Fprintf(os.Stderr, "mdlive mcp: connecting to %s\n", s.client.BaseURL())

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 20*1024*1024)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			fmt.Fprintf(os.Stderr, "mdlive mcp: invalid JSON: %v\n", err)
			continue
		}
		if req.Method == "" {
			continue
		}
		notification := len(req.ID) == 0 || string(req.ID) == "null"

		result, rpcErr := s.handle(req, version)
		if notification {
			continue
		}
		resp := response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
		if rpcErr != nil {
			resp.Result = nil
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *server) handle(req request, version string) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "mdlive", "version": version},
			"instructions": "mdlive exposes the Markdown workspace currently open in the mdlive app, " +
				"including local and SSH/SFTP remote roots. Use list_files to discover canonical paths, " +
				"read_file to read, and write_file to create or overwrite. Remote changes are cached; " +
				"set refresh=true to force a remote rescan. When editing in response to review comments, " +
				"read the full comment thread, honor the latest user feedback, then pass comment_ids and a " +
				"short comment to write_file (or call reply_comment afterwards) so the user sees this round's " +
				"before/after. Review prompts may include a document consensus and discussion excerpts imported by the user; " +
				"use them to preserve earlier decisions, but the latest explicit user comment always wins. " +
				"The user is the only person who can resolve a comment: never mark a comment resolved " +
				"or pass a resolve flag, even when your edit addresses it. Leave it open for the user to confirm. " +
				"The user can reply again and request another revision. write_file stores each full-document revision " +
				"as a rollback snapshot, so the user can locally restore the commented paragraph to an earlier round " +
				"from the comment UI without reverting unrelated edits. " +
				"Comments with the same non-empty groupId are related: " +
				"read every member's full thread, treat them as one coordinated requirement, and pass all of their " +
				"IDs together in write_file comment_ids so each gets this round's before/after. " +
				"Documents may have linked local code repositories (read_file returns codeRepos). " +
				"When writing code-aware documentation, use list_code_repos, code_repo_info, list_code_files, " +
				"search_code, and read_code to inspect the relevant repository first; code repositories are read-only.",
		}, nil

	case "notifications/initialized", "notifications/cancelled":
		return nil, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": tools()}, nil

	case "resources/list":
		return map[string]any{"resources": []any{}}, nil

	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil

	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
		}
		result := s.callTool(params)
		return result, nil

	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

func tools() []toolDef {
	return []toolDef{
		{
			Name:        "workspace_info",
			Description: "Show mdlive server URL, mode, and the local/remote roots in the current workspace.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "list_files",
			Description: "List Markdown files in the mdlive workspace. Paths returned as canonical <rootID>/<relPath> can be passed to read_file/write_file. Remote tree data can be stale; set refresh=true to rescan remote roots.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "optional case-insensitive path filter"},
					"root":    map[string]any{"type": "string", "description": "optional root name or root ID"},
					"refresh": map[string]any{"type": "boolean", "description": "rescan remote roots first (may be slow)"},
				},
			},
		},
		{
			Name:        "search_files",
			Description: "Search Markdown file paths across all roots. Equivalent to list_files with a query.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "case-insensitive path/name search"},
					"refresh": map[string]any{"type": "boolean", "description": "rescan remote roots first"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read a Markdown file from any local or remote root. Accepts a canonical path, <rootName>/<relPath>, or a unique file name/relative path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "canonical path or <rootName>/<relPath>"},
					"refresh": map[string]any{"type": "boolean", "description": "rescan remote roots first"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Create or overwrite a Markdown file in the current workspace, including remote SFTP roots. The mdlive preview updates and the file is written to disk. Use a canonical path or <rootName>/<relPath>. For related comments (same groupId), make one coordinated edit and pass all group comment IDs in comment_ids. Replies made through comment_ids automatically store a full-document snapshot for user rollback.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "canonical path or <rootName>/<relPath>"},
					"content": map[string]any{"type": "string", "description": "full Markdown content"},
					"comment_ids": map[string]any{
						"type": "array", "items": map[string]any{"type": "string"},
						"description": "optional review comment IDs addressed by this edit",
					},
					"comment": map[string]any{"type": "string", "description": "optional reply text added to those comments"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "list_comments",
			Description: "List inline review comments (with IDs, quoted text, line numbers, thread messages, and groupId). Comments sharing a non-empty groupId are related and should be read and modified together. Omit path for all comments, or pass a file path for one document.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "optional document path"},
				},
			},
		},
		{
			Name:        "reply_comment",
			Description: "Reply to a review comment as Code Agent. Include before/after excerpts when you changed the commented text so the user can compare. The agent cannot resolve comments; only the user can close a review.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "document path"},
					"id":     map[string]any{"type": "string", "description": "comment ID from list_comments"},
					"body":   map[string]any{"type": "string", "description": "explanation of the change"},
					"before": map[string]any{"type": "string", "description": "original text / excerpt"},
					"after":  map[string]any{"type": "string", "description": "new text / excerpt"},
				},
				"required": []string{"path", "id", "body"},
			},
		},
		{
			Name:        "list_code_repos",
			Description: "List code repositories. Pass a document path to list only repositories linked to that document; omit it to list all registered repositories. read_file also returns linked codeRepos.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "optional Markdown document path"},
				},
			},
		},
		{
			Name:        "code_repo_info",
			Description: "Show a linked code repository's path, Git branch/HEAD and dirty files.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo": map[string]any{"type": "string", "description": "code repository ID from list_code_repos or read_file"},
				},
				"required": []string{"repo"},
			},
		},
		{
			Name:        "list_code_files",
			Description: "List source files in a linked code repository, optionally filtered by relative path/name. Hidden and generated directories are skipped.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo":  map[string]any{"type": "string", "description": "code repository ID"},
					"query": map[string]any{"type": "string", "description": "optional case-insensitive path filter"},
					"limit": map[string]any{"type": "number", "description": "max files (default 200)"},
				},
				"required": []string{"repo"},
			},
		},
		{
			Name:        "read_code",
			Description: "Read a text/source file from a linked code repository. Paths are relative to the repository root; use start_line/end_line for large files.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo":       map[string]any{"type": "string", "description": "code repository ID"},
					"path":       map[string]any{"type": "string", "description": "file path relative to the repository root"},
					"start_line": map[string]any{"type": "number", "description": "first line (1-based, optional)"},
					"end_line":   map[string]any{"type": "number", "description": "last line (inclusive, optional)"},
				},
				"required": []string{"repo", "path"},
			},
		},
		{
			Name:        "search_code",
			Description: "Search text/source files in a linked code repository. Returns relative path, line number and matching line. Use this to find symbols, config and implementation details before writing documentation.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo":  map[string]any{"type": "string", "description": "code repository ID"},
					"query": map[string]any{"type": "string", "description": "case-insensitive text to search"},
					"limit": map[string]any{"type": "number", "description": "max matches (default 50)"},
				},
				"required": []string{"repo", "query"},
			},
		},
		{
			Name:        "changes",
			Description: "List Markdown files modified recently across local and remote roots. Set refresh=true to rescan remote roots first (recommended when checking remote changes).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"since_minutes": map[string]any{"type": "number", "description": "lookback window in minutes (default 30)"},
					"refresh":       map[string]any{"type": "boolean", "description": "rescan remote roots first"},
				},
			},
		},
	}
}

func (s *server) callTool(params toolCallParams) toolResult {
	s.client.SetTool(params.Name)
	defer s.client.SetTool("")
	// Re-read the discovery file so a restarted mdlive (possibly on another
	// port) is picked up without restarting the MCP server.
	s.client = agentclient.New(agentclient.DefaultURL(s.explicitURL))
	args := map[string]any{}
	if len(params.Arguments) > 0 && string(params.Arguments) != "null" {
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResult("invalid arguments: " + err.Error())
		}
	}
	switch params.Name {
	case "workspace_info":
		cfg, err := s.client.Config()
		if err != nil {
			return errorResult(err.Error())
		}
		tree, err := s.client.Tree(false)
		if err != nil {
			return errorResult(err.Error())
		}
		roots := make([]map[string]any, 0, len(tree.Roots))
		for _, root := range tree.Roots {
			location := root.Path
			if root.Kind == "remote" {
				location = root.Remote + ":" + root.Path
			}
			roots = append(roots, map[string]any{
				"id": root.ID, "kind": root.Kind, "name": root.Name,
				"path": location, "primary": root.Primary, "files": len(root.Files),
			})
		}
		return jsonResult(map[string]any{
			"url": s.client.BaseURL(), "config": cfg, "roots": roots,
		})

	case "list_files":
		refresh, _ := args["refresh"].(bool)
		tree, err := s.client.Tree(refresh)
		if err != nil {
			return errorResult(err.Error())
		}
		query, _ := args["query"].(string)
		rootFilter, _ := args["root"].(string)
		files := make([]agentclient.FileEntry, 0)
		for _, file := range tree.Files {
			if query != "" && !strings.Contains(strings.ToLower(file.RootName+"/"+file.RelPath), strings.ToLower(query)) {
				continue
			}
			if rootFilter != "" && file.RootName != rootFilter && file.RootID != rootFilter {
				continue
			}
			files = append(files, file)
		}
		return jsonResult(map[string]any{
			"roots": tree.Roots, "files": files,
			"total": len(files), "truncated": tree.Truncated,
		})

	case "search_files":
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return errorResult("query is required")
		}
		refresh, _ := args["refresh"].(bool)
		files, err := s.client.Search(query, refresh)
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(map[string]any{"query": query, "files": files, "total": len(files)})

	case "read_file":
		path, _ := args["path"].(string)
		if path == "" {
			return errorResult("path is required")
		}
		refresh, _ := args["refresh"].(bool)
		doc, resolved, err := s.client.Read(path, refresh)
		if err != nil {
			return errorResult(err.Error())
		}
		comments, _ := s.client.ListComments(doc.Path)
		codeRepos, _ := s.client.DocCodeRepos(doc.Path)
		return jsonResult(map[string]any{
			"path": doc.Path, "rootID": resolved.RootID, "relPath": resolved.RelPath,
			"content": doc.Content, "size": doc.Size, "modTime": doc.ModTime,
			"comments": comments, "groups": commentGroups(comments), "codeRepos": codeRepos,
		})

	case "list_comments":
		path, _ := args["path"].(string)
		comments, err := s.client.ListComments(path)
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(map[string]any{
			"comments": comments, "total": len(comments), "groups": commentGroups(comments),
		})

	case "reply_comment":
		path, _ := args["path"].(string)
		id, _ := args["id"].(string)
		body, _ := args["body"].(string)
		before, _ := args["before"].(string)
		after, _ := args["after"].(string)
		if path == "" || id == "" || body == "" {
			return errorResult("path, id and body are required")
		}
		comment, err := s.client.ReplyComment(path, id, body, before, after, "", "")
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(comment)

	case "write_file":
		path, _ := args["path"].(string)
		content, ok := args["content"].(string)
		if path == "" || !ok {
			return errorResult("path and content are required")
		}
		commentIDs := stringSlice(args["comment_ids"])
		commentText, _ := args["comment"].(string)
		var commentsBefore map[string]agentclient.Comment
		beforeContent := ""
		if len(commentIDs) > 0 {
			commentsBefore = make(map[string]agentclient.Comment)
			if comments, err := s.client.ListComments(path); err == nil {
				for _, comment := range comments {
					commentsBefore[comment.ID] = comment
				}
			}
			if doc, _, err := s.client.Read(path, false); err == nil {
				beforeContent = doc.Content
			}
		}
		doc, resolved, err := s.client.Write(path, content)
		if err != nil {
			return errorResult(err.Error())
		}
		replied := make([]agentclient.Comment, 0)
		warnings := make([]string, 0)
		warnedGroups := make(map[string]struct{})
		selected := make(map[string]struct{}, len(commentIDs))
		for _, id := range commentIDs {
			selected[id] = struct{}{}
		}
		for _, id := range commentIDs {
			comment, ok := commentsBefore[id]
			if !ok || comment.GroupID == "" {
				continue
			}
			if _, warned := warnedGroups[comment.GroupID]; warned {
				continue
			}
			var missing []string
			for _, peer := range commentsBefore {
				if peer.GroupID != comment.GroupID {
					continue
				}
				if _, ok := selected[peer.ID]; !ok {
					missing = append(missing, peer.ID)
				}
			}
			if len(missing) > 0 {
				warnings = append(warnings, "comment "+id+" belongs to related group "+comment.GroupID+
					"; also address and include these comment_ids: "+strings.Join(missing, ", "))
				warnedGroups[comment.GroupID] = struct{}{}
			}
		}
		for _, id := range commentIDs {
			comment, ok := commentsBefore[id]
			if !ok {
				warnings = append(warnings, "comment not found: "+id)
				continue
			}
			body := strings.TrimSpace(commentText)
			if body == "" {
				body = "已根据此评论修改文档。"
			}
			before := excerptForComment(beforeContent, comment.Quote, comment.Line)
			if strings.TrimSpace(before) == "" {
				before = comment.Quote
			}
			after := excerptForComment(doc.Content, comment.Quote, comment.Line)
			updated, err := s.client.ReplyComment(doc.Path, id, body, before, after, beforeContent, doc.Content)
			if err != nil {
				warnings = append(warnings, id+": "+err.Error())
				continue
			}
			replied = append(replied, updated)
		}
		return jsonResult(map[string]any{
			"path": doc.Path, "rootID": resolved.RootID, "relPath": resolved.RelPath,
			"size": doc.Size, "modTime": doc.ModTime, "written": true,
			"replied_comment_ids": commentIDs, "replied": replied, "warnings": warnings,
		})

	case "list_code_repos":
		path, _ := args["path"].(string)
		if path != "" {
			repos, err := s.client.DocCodeRepos(path)
			if err != nil {
				return errorResult(err.Error())
			}
			return jsonResult(map[string]any{"repos": repos, "total": len(repos)})
		}
		repos, err := s.client.CodeRepos()
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(map[string]any{"repos": repos, "total": len(repos)})

	case "code_repo_info":
		repo, _ := args["repo"].(string)
		if repo == "" {
			return errorResult("repo is required")
		}
		info, err := s.client.CodeInfo(repo)
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(info)

	case "list_code_files":
		repo, _ := args["repo"].(string)
		query, _ := args["query"].(string)
		if repo == "" {
			return errorResult("repo is required")
		}
		files, err := s.client.CodeFiles(repo, query, intArg(args, "limit", 200))
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(files)

	case "read_code":
		repo, _ := args["repo"].(string)
		file, _ := args["path"].(string)
		if repo == "" || file == "" {
			return errorResult("repo and path are required")
		}
		result, err := s.client.CodeRead(repo, file, intArg(args, "start_line", 0), intArg(args, "end_line", 0))
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(result)

	case "search_code":
		repo, _ := args["repo"].(string)
		query, _ := args["query"].(string)
		if repo == "" || query == "" {
			return errorResult("repo and query are required")
		}
		matches, err := s.client.CodeSearch(repo, query, intArg(args, "limit", 50))
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(matches)

	case "changes":
		minutes := 30.0
		if value, ok := args["since_minutes"].(float64); ok && value > 0 {
			minutes = value
		}
		refresh, _ := args["refresh"].(bool)
		files, err := s.client.Changes(time.Now().Add(-time.Duration(minutes*float64(time.Minute))), refresh)
		if err != nil {
			return errorResult(err.Error())
		}
		return jsonResult(map[string]any{
			"since_minutes": minutes, "files": files, "total": len(files),
		})

	default:
		return errorResult("unknown tool: " + params.Name)
	}
}

func jsonResult(value any) toolResult {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errorResult(err.Error())
	}
	return toolResult{Content: []toolContent{{Type: "text", Text: string(data)}}}
}

func errorResult(message string) toolResult {
	return toolResult{
		IsError: true,
		Content: []toolContent{{Type: "text", Text: message}},
	}
}

func intArg(args map[string]any, key string, fallback int) int {
	if value, ok := args[key].(float64); ok && value > 0 {
		return int(value)
	}
	return fallback
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if direct, ok := value.([]string); ok {
			return direct
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

type commentGroup struct {
	ID         string   `json:"groupId"`
	CommentIDs []string `json:"commentIds"`
}

func commentGroups(comments []agentclient.Comment) []commentGroup {
	order := make([]string, 0)
	members := make(map[string][]string)
	for _, comment := range comments {
		if comment.GroupID == "" {
			continue
		}
		if _, ok := members[comment.GroupID]; !ok {
			order = append(order, comment.GroupID)
		}
		members[comment.GroupID] = append(members[comment.GroupID], comment.ID)
	}
	out := make([]commentGroup, 0, len(order))
	for _, groupID := range order {
		if len(members[groupID]) < 2 {
			continue
		}
		out = append(out, commentGroup{ID: groupID, CommentIDs: members[groupID]})
	}
	return out
}

func excerptForComment(content, quote string, line int) string {
	if content == "" {
		return ""
	}
	lineNo := line
	if index := strings.Index(content, quote); index >= 0 {
		lineNo = 1 + strings.Count(content[:index], "\n")
	}
	lines := strings.Split(content, "\n")
	if lineNo < 1 {
		lineNo = 1
	}
	if lineNo > len(lines) {
		lineNo = len(lines)
	}
	index := lineNo - 1
	if strings.TrimSpace(lines[index]) == "" {
		for i := index - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				index = i
				break
			}
		}
	}
	start := index
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	end := index
	for end < len(lines)-1 && strings.TrimSpace(lines[end+1]) != "" {
		end++
	}
	text := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
	if runes := []rune(text); len(runes) > 600 {
		text = string(runes[:600])
	}
	return text
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
