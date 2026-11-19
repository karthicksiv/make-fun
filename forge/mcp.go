package main

// The Model Context Protocol endpoint at /mcp: JSON-RPC 2.0 over streamable HTTP, answered as plain
// JSON. It speaks the stateless 2026-07-28 revision (server/discover) and the handshake revisions
// (initialize). Every tool is a thin wrapper over a REST route, so MCP and REST act on one store
// through one code path.

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

const (
	mcpServerName    = "makefun-git-forge"
	mcpServerVersion = "1.0.0"
	mcpInstructions  = "A GitHub-compatible forge. Tools act on the repositories, issues and pull requests of the authenticated user; the same objects are served over REST at /api/v3 and GraphQL at /api/graphql."
)

var mcpRevisions = []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

type mcpProp struct {
	Name        string
	Type        string // string, integer, boolean, array (of strings)
	Description string
	Enum        []string
}

// restCall is the REST request a tool turns into.
type restCall struct {
	method string
	segs   []string
	query  url.Values
	body   map[string]any
	// issuesOnly keeps pull requests out of an issue listing.
	issuesOnly bool
}

type mcpTool struct {
	Name        string
	Description string
	Props       []mcpProp
	Required    []string
	Build       func(args toolArgs) (*restCall, string)
}

// toolArgs reads a tools/call arguments object leniently: numbers may arrive as strings and back.
type toolArgs map[string]any

func (t toolArgs) str(key string) string {
	switch v := t[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func (t toolArgs) has(key string) bool {
	v, ok := t[key]
	if !ok || v == nil {
		return false
	}
	if s, isString := v.(string); isString {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func (t toolArgs) integer(key string) (int, bool) {
	switch v := t[key].(type) {
	case float64:
		return int(v), v == float64(int(v))
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}

// repoSegs is the /repos/{owner}/{repo} prefix of a tool's REST path.
func (t toolArgs) repoSegs(rest ...string) []string {
	owner, repo := t.str("owner"), t.str("repo")
	if full := t.str("full_name"); full != "" && (owner == "" || repo == "") {
		owner, repo, _ = strings.Cut(full, "/")
	}
	return append([]string{"repos", owner, repo}, rest...)
}

func (t toolArgs) copyTo(body map[string]any, keys ...string) {
	for _, key := range keys {
		if t.has(key) {
			body[key] = t[key]
		}
	}
}

func (t toolArgs) queryOf(keys ...string) url.Values {
	q := url.Values{}
	for _, key := range keys {
		if t.has(key) {
			q.Set(key, t.str(key))
		}
	}
	return q
}

var ownerRepoProps = []mcpProp{
	{Name: "owner", Type: "string", Description: "Repository owner (the account login)"},
	{Name: "repo", Type: "string", Description: "Repository name"},
}

func withOwnerRepo(props ...mcpProp) []mcpProp {
	return append(append([]mcpProp{}, ownerRepoProps...), props...)
}

// mcpToolList is every tool the endpoint offers, in the order tools/list names them.
var mcpToolList = []*mcpTool{
	{
		Name:        "get_me",
		Description: "Get the profile of the authenticated user, the account every repository created here belongs to.",
		Build: func(args toolArgs) (*restCall, string) {
			return &restCall{method: "GET", segs: []string{"user"}}, ""
		},
	},
	{
		Name:        "create_repo",
		Description: "Create a new repository in the authenticated user's account.",
		Props: []mcpProp{
			{Name: "name", Type: "string", Description: "Repository name"},
			{Name: "description", Type: "string", Description: "Repository description"},
			{Name: "private", Type: "boolean", Description: "Whether the repository is private"},
			{Name: "autoInit", Type: "boolean", Description: "Initialize the repository with a README"},
		},
		Required: []string{"name"},
		Build: func(args toolArgs) (*restCall, string) {
			body := map[string]any{}
			args.copyTo(body, "name", "description", "private")
			if init, ok := args["autoInit"].(bool); ok {
				body["auto_init"] = init
			}
			return &restCall{method: "POST", segs: []string{"user", "repos"}, body: body}, ""
		},
	},
	{
		Name:        "get_repo",
		Description: "Get a repository's details: name, owner, default branch, visibility and URLs.",
		Props:       withOwnerRepo(),
		Required:    []string{"owner", "repo"},
		Build: func(args toolArgs) (*restCall, string) {
			return &restCall{method: "GET", segs: args.repoSegs()}, ""
		},
	},
	{
		Name:        "list_repos",
		Description: "List the repositories of the authenticated user.",
		Props: []mcpProp{
			{Name: "page", Type: "integer", Description: "Page number for pagination (min 1)"},
			{Name: "perPage", Type: "integer", Description: "Results per page for pagination (min 1, max 100)"},
		},
		Build: func(args toolArgs) (*restCall, string) {
			q := args.queryOf("page")
			if args.has("perPage") {
				q.Set("per_page", args.str("perPage"))
			}
			return &restCall{method: "GET", segs: []string{"user", "repos"}, query: q}, ""
		},
	},
	{
		Name:        "create_issue",
		Description: "Open a new issue in a repository.",
		Props: withOwnerRepo(
			mcpProp{Name: "title", Type: "string", Description: "Issue title"},
			mcpProp{Name: "body", Type: "string", Description: "Issue body content"},
			mcpProp{Name: "labels", Type: "array", Description: "Labels to apply to this issue; each must already exist in the repository"},
			mcpProp{Name: "assignees", Type: "array", Description: "Usernames to assign to this issue"},
		),
		Required: []string{"owner", "repo", "title"},
		Build: func(args toolArgs) (*restCall, string) {
			body := map[string]any{}
			args.copyTo(body, "title", "body", "labels", "assignees")
			return &restCall{method: "POST", segs: args.repoSegs("issues"), body: body}, ""
		},
	},
	{
		Name:        "get_issue",
		Description: "Get the details of one issue in a repository.",
		Props:       withOwnerRepo(mcpProp{Name: "issue_number", Type: "integer", Description: "The number of the issue"}),
		Required:    []string{"owner", "repo", "issue_number"},
		Build: func(args toolArgs) (*restCall, string) {
			n, ok := args.integer("issue_number")
			if !ok {
				return nil, "issue_number must be an integer"
			}
			return &restCall{method: "GET", segs: args.repoSegs("issues", strconv.Itoa(n))}, ""
		},
	},
	{
		Name:        "list_issues",
		Description: "List the issues of a repository, filtered by state and labels.",
		Props: withOwnerRepo(
			mcpProp{Name: "state", Type: "string", Description: "Filter by state", Enum: []string{"open", "closed", "all"}},
			mcpProp{Name: "labels", Type: "array", Description: "Filter by labels"},
			mcpProp{Name: "sort", Type: "string", Description: "Sort order", Enum: []string{"created", "updated", "comments"}},
			mcpProp{Name: "direction", Type: "string", Description: "Sort direction", Enum: []string{"asc", "desc"}},
			mcpProp{Name: "page", Type: "integer", Description: "Page number for pagination (min 1)"},
			mcpProp{Name: "perPage", Type: "integer", Description: "Results per page for pagination (min 1, max 100)"},
		),
		Required: []string{"owner", "repo"},
		Build: func(args toolArgs) (*restCall, string) {
			q := args.queryOf("sort", "direction", "page")
			if args.has("state") {
				q.Set("state", strings.ToLower(args.str("state")))
			}
			if list, ok := args["labels"].([]any); ok {
				names := make([]string, 0, len(list))
				for _, item := range list {
					if name, ok := item.(string); ok {
						names = append(names, name)
					}
				}
				q.Set("labels", strings.Join(names, ","))
			}
			q.Set("per_page", "100")
			if args.has("perPage") {
				q.Set("per_page", args.str("perPage"))
			}
			return &restCall{method: "GET", segs: args.repoSegs("issues"), query: q, issuesOnly: true}, ""
		},
	},
	{
		Name:        "update_issue",
		Description: "Update an existing issue: its title, body, labels or state (open or closed).",
		Props: withOwnerRepo(
			mcpProp{Name: "issue_number", Type: "integer", Description: "The number of the issue to update"},
			mcpProp{Name: "title", Type: "string", Description: "New title"},
			mcpProp{Name: "body", Type: "string", Description: "New body"},
			mcpProp{Name: "state", Type: "string", Description: "New state", Enum: []string{"open", "closed"}},
			mcpProp{Name: "labels", Type: "array", Description: "New labels"},
		),
		Required: []string{"owner", "repo", "issue_number"},
		Build: func(args toolArgs) (*restCall, string) {
			n, ok := args.integer("issue_number")
			if !ok {
				return nil, "issue_number must be an integer"
			}
			body := map[string]any{}
			args.copyTo(body, "title", "body", "state", "labels")
			return &restCall{method: "PATCH", segs: args.repoSegs("issues", strconv.Itoa(n)), body: body}, ""
		},
	},
	{
		Name:        "add_issue_comment",
		Description: "Add a comment to an issue or a pull request.",
		Props: withOwnerRepo(
			mcpProp{Name: "issue_number", Type: "integer", Description: "The number of the issue or pull request to comment on"},
			mcpProp{Name: "body", Type: "string", Description: "Comment content"},
		),
		Required: []string{"owner", "repo", "issue_number", "body"},
		Build: func(args toolArgs) (*restCall, string) {
			n, ok := args.integer("issue_number")
			if !ok {
				return nil, "issue_number must be an integer"
			}
			return &restCall{method: "POST", segs: args.repoSegs("issues", strconv.Itoa(n), "comments"), body: map[string]any{"body": args.str("body")}}, ""
		},
	},
	{
		Name:        "create_branch",
		Description: "Create a new branch in a repository, starting from another branch (the default branch when none is named).",
		Props: withOwnerRepo(
			mcpProp{Name: "branch", Type: "string", Description: "Name for the new branch"},
			mcpProp{Name: "from_branch", Type: "string", Description: "Source branch (defaults to the repository's default branch)"},
		),
		Required: []string{"owner", "repo", "branch"},
		Build: func(args toolArgs) (*restCall, string) {
			body := map[string]any{"ref": "refs/heads/" + args.str("branch"), "__from": args.str("from_branch")}
			return &restCall{method: "POST", segs: args.repoSegs("git", "refs"), body: body}, ""
		},
	},
	{
		Name:        "list_branches",
		Description: "List the branches of a repository with the commit each points at.",
		Props:       withOwnerRepo(),
		Required:    []string{"owner", "repo"},
		Build: func(args toolArgs) (*restCall, string) {
			return &restCall{method: "GET", segs: args.repoSegs("branches"), query: url.Values{"per_page": {"100"}}}, ""
		},
	},
	{
		Name:        "list_commits",
		Description: "List the commits of a branch of a repository, newest first.",
		Props: withOwnerRepo(
			mcpProp{Name: "sha", Type: "string", Description: "Commit SHA, branch or tag name to list commits of (defaults to the default branch)"},
			mcpProp{Name: "page", Type: "integer", Description: "Page number for pagination (min 1)"},
			mcpProp{Name: "perPage", Type: "integer", Description: "Results per page for pagination (min 1, max 100)"},
		),
		Required: []string{"owner", "repo"},
		Build: func(args toolArgs) (*restCall, string) {
			q := args.queryOf("sha", "page")
			if args.has("perPage") {
				q.Set("per_page", args.str("perPage"))
			}
			return &restCall{method: "GET", segs: args.repoSegs("commits"), query: q}, ""
		},
	},
	{
		Name:        "get_file_contents",
		Description: "Get the contents of a file or the entries of a directory in a repository.",
		Props: withOwnerRepo(
			mcpProp{Name: "path", Type: "string", Description: "Path to the file or directory"},
			mcpProp{Name: "ref", Type: "string", Description: "Branch, tag or commit to read from (defaults to the default branch)"},
		),
		Required: []string{"owner", "repo"},
		Build: func(args toolArgs) (*restCall, string) {
			segs := args.repoSegs("contents")
			if path := strings.Trim(args.str("path"), "/"); path != "" {
				segs = append(segs, strings.Split(path, "/")...)
			}
			return &restCall{method: "GET", segs: segs, query: args.queryOf("ref")}, ""
		},
	},
	{
		Name:        "create_or_update_file",
		Description: "Create or update a single file in a repository with a commit. Updating an existing file needs its current blob sha.",
		Props: withOwnerRepo(
			mcpProp{Name: "path", Type: "string", Description: "Path where to create or update the file"},
			mcpProp{Name: "content", Type: "string", Description: "Content of the file, as plain text"},
			mcpProp{Name: "message", Type: "string", Description: "Commit message"},
			mcpProp{Name: "branch", Type: "string", Description: "Branch to commit to (defaults to the default branch)"},
			mcpProp{Name: "sha", Type: "string", Description: "Blob SHA of the file being replaced; required when the file exists"},
		),
		Required: []string{"owner", "repo", "path", "content", "message"},
		Build: func(args toolArgs) (*restCall, string) {
			body := map[string]any{"content": base64.StdEncoding.EncodeToString([]byte(args.str("content")))}
			args.copyTo(body, "message", "branch", "sha")
			segs := append(args.repoSegs("contents"), strings.Split(strings.Trim(args.str("path"), "/"), "/")...)
			return &restCall{method: "PUT", segs: segs, body: body}, ""
		},
	},
	{
		Name:        "create_pull_request",
		Description: "Open a new pull request from a head branch into a base branch of the same repository.",
		Props: withOwnerRepo(
			mcpProp{Name: "title", Type: "string", Description: "Pull request title"},
			mcpProp{Name: "body", Type: "string", Description: "Pull request description"},
			mcpProp{Name: "head", Type: "string", Description: "Branch containing the changes"},
			mcpProp{Name: "base", Type: "string", Description: "Branch to merge into"},
			mcpProp{Name: "draft", Type: "boolean", Description: "Create as a draft pull request"},
		),
		Required: []string{"owner", "repo", "title", "head", "base"},
		Build: func(args toolArgs) (*restCall, string) {
			body := map[string]any{}
			args.copyTo(body, "title", "body", "head", "base", "draft")
			return &restCall{method: "POST", segs: args.repoSegs("pulls"), body: body}, ""
		},
	},
	{
		Name:        "get_pull_request",
		Description: "Get the details of one pull request: state, head and base branches, mergeability.",
		Props:       withOwnerRepo(mcpProp{Name: "pull_number", Type: "integer", Description: "Pull request number"}),
		Required:    []string{"owner", "repo", "pull_number"},
		Build: func(args toolArgs) (*restCall, string) {
			n, ok := args.integer("pull_number")
			if !ok {
				return nil, "pull_number must be an integer"
			}
			return &restCall{method: "GET", segs: args.repoSegs("pulls", strconv.Itoa(n))}, ""
		},
	},
	{
		Name:        "list_pull_requests",
		Description: "List the pull requests of a repository, filtered by state, head and base branch.",
		Props: withOwnerRepo(
			mcpProp{Name: "state", Type: "string", Description: "Filter by state", Enum: []string{"open", "closed", "all"}},
			mcpProp{Name: "head", Type: "string", Description: "Filter by head branch"},
			mcpProp{Name: "base", Type: "string", Description: "Filter by base branch"},
			mcpProp{Name: "page", Type: "integer", Description: "Page number for pagination (min 1)"},
			mcpProp{Name: "perPage", Type: "integer", Description: "Results per page for pagination (min 1, max 100)"},
		),
		Required: []string{"owner", "repo"},
		Build: func(args toolArgs) (*restCall, string) {
			q := args.queryOf("state", "head", "base", "page")
			q.Set("per_page", "100")
			if args.has("perPage") {
				q.Set("per_page", args.str("perPage"))
			}
			return &restCall{method: "GET", segs: args.repoSegs("pulls"), query: q}, ""
		},
	},
	{
		Name:        "get_pull_request_files",
		Description: "List the files a pull request changes, with their added and deleted line counts.",
		Props:       withOwnerRepo(mcpProp{Name: "pull_number", Type: "integer", Description: "Pull request number"}),
		Required:    []string{"owner", "repo", "pull_number"},
		Build: func(args toolArgs) (*restCall, string) {
			n, ok := args.integer("pull_number")
			if !ok {
				return nil, "pull_number must be an integer"
			}
			return &restCall{method: "GET", segs: args.repoSegs("pulls", strconv.Itoa(n), "files"), query: url.Values{"per_page": {"100"}}}, ""
		},
	},
	{
		Name:        "merge_pull_request",
		Description: "Merge a pull request into its base branch with a merge commit, a squash or a rebase.",
		Props: withOwnerRepo(
			mcpProp{Name: "pull_number", Type: "integer", Description: "Pull request number"},
			mcpProp{Name: "merge_method", Type: "string", Description: "Merge method", Enum: []string{"merge", "squash", "rebase"}},
			mcpProp{Name: "commit_title", Type: "string", Description: "Title for the merge commit"},
			mcpProp{Name: "commit_message", Type: "string", Description: "Extra detail for the merge commit"},
		),
		Required: []string{"owner", "repo", "pull_number"},
		Build: func(args toolArgs) (*restCall, string) {
			n, ok := args.integer("pull_number")
			if !ok {
				return nil, "pull_number must be an integer"
			}
			body := map[string]any{}
			args.copyTo(body, "merge_method", "commit_title", "commit_message")
			return &restCall{method: "PUT", segs: args.repoSegs("pulls", strconv.Itoa(n), "merge"), body: body}, ""
		},
	},
	{
		Name:        "search_issues",
		Description: "Search issues and pull requests across repositories by text and the repo:, is: and state: qualifiers.",
		Props: []mcpProp{
			{Name: "query", Type: "string", Description: "Search query, for example: repo:owner/name is:issue is:open crash"},
		},
		Required: []string{"query"},
		Build: func(args toolArgs) (*restCall, string) {
			return &restCall{method: "GET", segs: []string{"search", "issues"}, query: url.Values{"q": {args.str("query")}, "per_page": {"100"}}}, ""
		},
	},
}

// renderMCPTools serialises the tools/list result once: the same bytes answer every tools/list.
func renderMCPTools() []byte {
	tools := make([]any, 0, len(mcpToolList))
	for _, tool := range mcpToolList {
		properties := &orderedMap{}
		for _, prop := range tool.Props {
			schema := om("type", prop.Type)
			if prop.Type == "array" {
				schema.set("items", om("type", "string"))
			}
			schema.set("description", prop.Description)
			if len(prop.Enum) > 0 {
				schema.set("enum", prop.Enum)
			}
			properties.set(prop.Name, schema)
		}
		schema := om("type", "object", "properties", properties)
		if len(tool.Required) > 0 {
			schema.set("required", tool.Required)
		}
		readOnly := strings.HasPrefix(tool.Name, "get_") || strings.HasPrefix(tool.Name, "list_") || strings.HasPrefix(tool.Name, "search_")
		tools = append(tools, om(
			"name", tool.Name,
			"description", tool.Description,
			"inputSchema", schema,
			"annotations", om("title", strings.ReplaceAll(tool.Name, "_", " "), "readOnlyHint", readOnly),
		))
	}
	return appendJSON(nil, om("tools", tools))
}

func findMCPTool(name string) *mcpTool {
	for _, tool := range mcpToolList {
		if tool.Name == name {
			return tool
		}
	}
	return nil
}

// rpcReply builds one JSON-RPC response around an already serialised result or error.
func rpcResult(id json.RawMessage, result []byte) []byte {
	out := make([]byte, 0, len(result)+64)
	out = append(out, `{"jsonrpc":"2.0","id":`...)
	out = append(out, id...)
	out = append(out, `,"result":`...)
	out = append(out, result...)
	return append(out, '}')
}

func rpcError(id json.RawMessage, code int, message string) []byte {
	out := make([]byte, 0, 128)
	out = append(out, `{"jsonrpc":"2.0","id":`...)
	out = append(out, id...)
	out = append(out, `,"error":`...)
	out = appendJSON(out, om("code", code, "message", message))
	return append(out, '}')
}

var nullID = json.RawMessage("null")

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  *string         `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (a *App) handleMCP(req *Request) *Response {
	if ok, present := a.authorized(req); !ok {
		return unauthorized(present).setHeader("WWW-Authenticate", `Bearer realm="forge"`)
	}
	if req.Method != "POST" {
		res := jsonResponse(405, om("jsonrpc", "2.0", "id", nil, "error", om("code", rpcInvalidRequest, "message", "Method Not Allowed: the MCP endpoint answers POST")))
		return res.setHeader("Allow", "POST")
	}
	reply := func(status int, body []byte) *Response {
		return &Response{Status: status, ContentType: "application/json", Body: body}
	}
	trimmed := strings.TrimSpace(string(req.Body))
	if trimmed == "" || !json.Valid(req.Body) {
		return reply(400, rpcError(nullID, rpcParseError, "Parse error"))
	}
	base := baseURL(req)
	if trimmed[0] == '[' {
		var batch []json.RawMessage
		_ = json.Unmarshal(req.Body, &batch)
		if len(batch) == 0 {
			return reply(400, rpcError(nullID, rpcInvalidRequest, "Invalid Request: empty batch"))
		}
		out := []byte{'['}
		answered := 0
		for _, raw := range batch {
			if one := a.mcpMessage(base, raw); one != nil {
				if answered > 0 {
					out = append(out, ',')
				}
				out = append(out, one...)
				answered++
			}
		}
		if answered == 0 {
			return &Response{Status: 202}
		}
		return reply(200, append(out, ']'))
	}
	one := a.mcpMessage(base, req.Body)
	if one == nil {
		return &Response{Status: 202}
	}
	return reply(200, one)
}

// mcpMessage answers one JSON-RPC message; a notification has no answer (nil).
func (a *App) mcpMessage(base string, raw json.RawMessage) []byte {
	var msg rpcMessage
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Method == nil {
		id := msg.ID
		if len(id) == 0 {
			id = nullID
		}
		return rpcError(id, rpcInvalidRequest, "Invalid Request")
	}
	if len(msg.ID) == 0 {
		return nil // a notification
	}
	method := *msg.Method
	serverInfo := om("name", mcpServerName, "version", mcpServerVersion)
	capabilities := om("tools", om("listChanged", false))
	switch method {
	case "server/discover":
		return rpcResult(msg.ID, appendJSON(nil, om(
			"supportedVersions", mcpRevisions,
			"capabilities", capabilities,
			"serverInfo", serverInfo,
			"instructions", mcpInstructions,
			"_meta", om("io.modelcontextprotocol/serverInfo", serverInfo),
		)))
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		version := "2025-11-25"
		for _, known := range mcpRevisions {
			if known == params.ProtocolVersion {
				version = known
			}
		}
		return rpcResult(msg.ID, appendJSON(nil, om(
			"protocolVersion", version,
			"capabilities", capabilities,
			"serverInfo", serverInfo,
			"instructions", mcpInstructions,
		)))
	case "ping":
		return rpcResult(msg.ID, []byte("{}"))
	case "tools/list":
		return rpcResult(msg.ID, a.mcpTools)
	case "resources/list":
		return rpcResult(msg.ID, []byte(`{"resources":[]}`))
	case "resources/templates/list":
		return rpcResult(msg.ID, []byte(`{"resourceTemplates":[]}`))
	case "prompts/list":
		return rpcResult(msg.ID, []byte(`{"prompts":[]}`))
	case "tools/call":
		var params struct {
			Name      *string        `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil || params.Name == nil {
			return rpcError(msg.ID, rpcInvalidParams, "Invalid params: tools/call needs a tool name and an arguments object")
		}
		tool := findMCPTool(*params.Name)
		if tool == nil {
			return rpcError(msg.ID, rpcInvalidParams, "Unknown tool: "+*params.Name)
		}
		return rpcResult(msg.ID, a.callTool(base, tool, toolArgs(params.Arguments)))
	}
	return rpcError(msg.ID, rpcMethodNotFound, "Method not found: "+method)
}

func toolFailure(text string) []byte {
	return appendJSON(nil, om("content", []any{om("type", "text", "text", text)}, "isError", true))
}

// callTool runs a tool and renders its CallToolResult.
func (a *App) callTool(base string, tool *mcpTool, args toolArgs) []byte {
	if args == nil {
		args = toolArgs{}
	}
	var missing []string
	for _, name := range tool.Required {
		if !args.has(name) {
			// owner and repo may arrive together as full_name.
			if (name == "owner" || name == "repo") && strings.Contains(args.str("full_name"), "/") {
				continue
			}
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return toolFailure("missing required argument(s): " + strings.Join(missing, ", "))
	}
	call, problem := tool.Build(args)
	if problem != "" {
		return toolFailure(problem)
	}
	for _, seg := range call.segs {
		if seg == "" {
			return toolFailure("a required argument is empty")
		}
	}
	req := &Request{Method: call.method, Path: "/api/v3/" + strings.Join(call.segs, "/"), RawPath: "/api/v3/" + pathEscapeSlashes(strings.Join(call.segs, "/")), RawQuery: call.query.Encode()}
	c := &rc{a: a, req: req, v: &view{a: a, base: base}, body: call.body, issuesOnly: call.issuesOnly}
	if c.body == nil {
		c.body = map[string]any{}
	}
	res := func() *Response {
		if call.method == "GET" {
			a.mu.RLock()
			defer a.mu.RUnlock()
		} else {
			a.mu.Lock()
			defer a.mu.Unlock()
		}
		// create_branch names its source branch; the REST route wants that branch's commit.
		if from, isBranch := c.body["__from"].(string); isBranch {
			delete(c.body, "__from")
			if repo := a.repos[repoKey(call.segs[1], call.segs[2])]; repo != nil {
				if from == "" {
					from = repo.DefaultBranch
				}
				if tip := repo.Branch(from); !tip.IsZero() {
					c.body["sha"] = tip.String()
				}
			}
		}
		return c.dispatch(call.segs)
	}()
	if res == nil {
		res = notFound()
	}
	if res.Status >= 400 {
		var failure struct {
			Message string `json:"message"`
			Errors  []any  `json:"errors"`
		}
		_ = json.Unmarshal(res.Body, &failure)
		text := tool.Name + " failed (HTTP " + strconv.Itoa(res.Status) + ")"
		if failure.Message != "" {
			text += ": " + failure.Message
		}
		if len(failure.Errors) > 0 {
			text += " " + string(appendJSON(nil, failure.Errors))
		}
		return toolFailure(text)
	}
	body := res.Body
	if len(body) == 0 {
		body = []byte(`{"ok":true}`)
	}
	result := om("content", []any{om("type", "text", "text", string(body))})
	if body[0] == '{' {
		result.set("structuredContent", rawJSON(body))
	}
	result.set("isError", false)
	return appendJSON(make([]byte, 0, 2*len(body)+128), result)
}
