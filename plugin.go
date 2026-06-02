package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ericlakich/squadron-plugin-opencode/opencode"
	squadron "github.com/mlund01/squadron-sdk"
)

// tools defines the metadata for all tools provided by this plugin.
var tools = map[string]*squadron.ToolInfo{
	"code_develop": {
		Name: "code_develop",
		Description: "Spawn a local OpenCode session and have it work on a coding task. " +
			"OpenCode runs in the given working directory, implements the requested changes " +
			"(editing files and running commands directly on the local machine), and returns " +
			"a summary of what it did. Use this for feature development, bug fixes, refactoring, " +
			"or any code changes on a local project. Returns a session ID that can be passed to " +
			"continue_session or check_session.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"task": {
					Type:        squadron.TypeString,
					Description: "A description of the development task to perform (e.g. 'Add cursor-based pagination to the GET /users endpoint and tests').",
				},
				"cwd": {
					Type:        squadron.TypeString,
					Description: "Absolute path to the local project directory OpenCode should work in. Falls back to the plugin's default_cwd setting if omitted.",
				},
				"model": {
					Type:        squadron.TypeString,
					Description: "Optional model override in 'provider/model' form (e.g. 'bedrock-mantle/qwen.qwen3-coder-480b-a35b-instruct'). Defaults to the model in the OpenCode config.",
				},
				"instructions": {
					Type:        squadron.TypeString,
					Description: "Optional additional context, constraints, or coding guidelines for the task.",
				},
				"title": {
					Type:        squadron.TypeString,
					Description: "Optional title for the OpenCode session. Defaults to a truncated form of the task.",
				},
			},
			Required: []string{"task"},
		},
	},
	"continue_session": {
		Name: "continue_session",
		Description: "Send a follow-up instruction to an existing OpenCode session, resuming where it " +
			"left off with full prior context. Use this to iterate on work started by code_develop " +
			"(e.g. 'also add validation for the page_size parameter' or 'the tests are failing, fix them').",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"session_id": {
					Type:        squadron.TypeString,
					Description: "The OpenCode session ID to resume (e.g. 'ses_17ab050c4ffeZIB2FNgaLWUx3M'), as returned by code_develop.",
				},
				"message": {
					Type:        squadron.TypeString,
					Description: "The follow-up instruction or question to send to the session.",
				},
				"cwd": {
					Type:        squadron.TypeString,
					Description: "Absolute path to the project directory. Falls back to the plugin's default_cwd setting if omitted.",
				},
				"model": {
					Type:        squadron.TypeString,
					Description: "Optional model override in 'provider/model' form. Defaults to the model in the OpenCode config.",
				},
			},
			Required: []string{"session_id", "message"},
		},
	},
	"check_session": {
		Name: "check_session",
		Description: "Inspect an existing OpenCode session. Returns the session metadata plus the full " +
			"exported transcript (messages and tool calls) as JSON. Use this to review what a session " +
			"did or to retrieve details for a session created earlier.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"session_id": {
					Type:        squadron.TypeString,
					Description: "The OpenCode session ID to inspect (e.g. 'ses_17ab050c4ffeZIB2FNgaLWUx3M').",
				},
				"cwd": {
					Type:        squadron.TypeString,
					Description: "Absolute path to the project directory. Falls back to the plugin's default_cwd setting if omitted.",
				},
				"sanitize": {
					Type:        squadron.TypeBoolean,
					Description: "When true, redact sensitive transcript and file data from the export. Defaults to false.",
				},
			},
			Required: []string{"session_id"},
		},
	},
	"list_sessions": {
		Name: "list_sessions",
		Description: "List recent OpenCode sessions (newest first) with their IDs, titles, and working " +
			"directories. Use this to discover the session ID of work done earlier so it can be passed " +
			"to continue_session or check_session.",
		Schema: squadron.Schema{
			Type: squadron.TypeObject,
			Properties: squadron.PropertyMap{
				"cwd": {
					Type:        squadron.TypeString,
					Description: "Absolute path to a project directory. When set, only sessions in that directory are returned. Falls back to the plugin's default_cwd setting if omitted.",
				},
				"limit": {
					Type:        squadron.TypeInteger,
					Description: "Maximum number of sessions to return. Defaults to 20.",
				},
			},
		},
	},
}

// Plugin implements the squadron.ToolProvider interface for local OpenCode.
type Plugin struct {
	client       *opencode.Client
	defaultCwd   string
	defaultModel string
}

// Configure receives settings from the Squadron HCL config.
//
// Optional settings:
//   - opencode_bin:     Path to the opencode binary. Defaults to "opencode" (resolved on PATH).
//   - config_json:      Inline OpenCode config document (JSON). Takes precedence over config_path.
//   - config_path:      Path to an OpenCode config file.
//   - default_cwd:      Default working directory used when a tool call omits cwd.
//   - default_model:    Default model override ("provider/model"). Applied when a call omits model.
//   - skip_permissions: "true" to pass --dangerously-skip-permissions so runs can edit files and
//     run commands without interactive approval. Defaults to "false".
//   - output_format:    "default" (human-readable) or "json" (raw JSON events). Defaults to "default".
//   - timeout_minutes:  Maximum time in minutes for a single OpenCode run. Defaults to 30.
//   - env_<NAME>:        Any setting prefixed with "env_" is exported to the opencode
//     subprocess as the environment variable <NAME> (case preserved). This lets a secret
//     supplied by Squadron (e.g. env_AWS_BEARER_TOKEN_BEDROCK = vars.aws_bearer_token_bedrock)
//     resolve a "{env:NAME}" placeholder in the OpenCode config, instead of relying on a
//     local env file.
func (p *Plugin) Configure(settings map[string]string) error {
	client := opencode.New(settings["opencode_bin"])

	if cfg := strings.TrimSpace(settings["config_json"]); cfg != "" {
		if !json.Valid([]byte(cfg)) {
			return fmt.Errorf("config_json is not valid JSON")
		}
		client.ConfigJSON = cfg
	} else if path := strings.TrimSpace(settings["config_path"]); path != "" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("config_path %q is not accessible: %w", path, err)
		}
		client.ConfigPath = path
	}

	if v := strings.TrimSpace(settings["skip_permissions"]); v != "" {
		skip, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid skip_permissions %q: must be true or false", v)
		}
		client.SkipPermissions = skip
	}

	if v := strings.TrimSpace(settings["output_format"]); v != "" {
		if v != "default" && v != "json" {
			return fmt.Errorf("invalid output_format %q: must be 'default' or 'json'", v)
		}
		client.OutputFormat = v
	}

	client.RunTimeout = opencode.DefaultRunTimeout
	if v := strings.TrimSpace(settings["timeout_minutes"]); v != "" {
		minutes, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid timeout_minutes %q: %w", v, err)
		}
		if minutes < 1 {
			return fmt.Errorf("timeout_minutes must be at least 1, got %d", minutes)
		}
		client.RunTimeout = time.Duration(minutes) * time.Minute
	}

	// Collect env_<NAME> settings into environment variables for the subprocess.
	// These carry secrets supplied by Squadron (e.g. via vars.*) into OpenCode's
	// "{env:NAME}" config placeholders.
	const envPrefix = "env_"
	extraEnv := make(map[string]string)
	for k, v := range settings {
		name := strings.TrimPrefix(k, envPrefix)
		if name == k || name == "" {
			continue
		}
		extraEnv[name] = v
	}
	if len(extraEnv) > 0 {
		client.ExtraEnv = extraEnv
	}

	p.client = client
	p.defaultCwd = strings.TrimSpace(settings["default_cwd"])
	p.defaultModel = strings.TrimSpace(settings["default_model"])

	return nil
}

// Call dispatches a tool invocation to the appropriate handler.
func (p *Plugin) Call(ctx context.Context, toolName string, payload string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("plugin not configured: call Configure first")
	}

	switch toolName {
	case "code_develop":
		return p.callCodeDevelop(ctx, payload)
	case "continue_session":
		return p.callContinueSession(ctx, payload)
	case "check_session":
		return p.callCheckSession(ctx, payload)
	case "list_sessions":
		return p.callListSessions(ctx, payload)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

// GetToolInfo returns metadata for a specific tool.
func (p *Plugin) GetToolInfo(toolName string) (*squadron.ToolInfo, error) {
	info, ok := tools[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
	return info, nil
}

// ListTools returns metadata for all tools provided by this plugin.
func (p *Plugin) ListTools() ([]*squadron.ToolInfo, error) {
	result := make([]*squadron.ToolInfo, 0, len(tools))
	for _, info := range tools {
		result = append(result, info)
	}
	return result, nil
}

// codeDevelopParams are the parameters for the code_develop tool.
type codeDevelopParams struct {
	Task         string `json:"task"`
	Cwd          string `json:"cwd,omitempty"`
	Model        string `json:"model,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Title        string `json:"title,omitempty"`
}

// continueSessionParams are the parameters for the continue_session tool.
type continueSessionParams struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Cwd       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
}

// checkSessionParams are the parameters for the check_session tool.
type checkSessionParams struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd,omitempty"`
	Sanitize  bool   `json:"sanitize,omitempty"`
}

// listSessionsParams are the parameters for the list_sessions tool.
type listSessionsParams struct {
	Cwd   string `json:"cwd,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// resolveCwd returns the absolute working directory for a call, falling back to
// the configured default. An empty result means "let opencode decide".
func (p *Plugin) resolveCwd(cwd string) string {
	if cwd == "" {
		cwd = p.defaultCwd
	}
	if cwd == "" {
		return ""
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// resolveModel applies the default model when a call omits one.
func (p *Plugin) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return p.defaultModel
}

// callCodeDevelop runs a fresh OpenCode session on a development task.
func (p *Plugin) callCodeDevelop(ctx context.Context, payload string) (string, error) {
	var params codeDevelopParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if strings.TrimSpace(params.Task) == "" {
		return "", fmt.Errorf("task is required")
	}

	cwd := p.resolveCwd(params.Cwd)
	model := p.resolveModel(params.Model)
	prompt := buildDevelopPrompt(params.Task, params.Instructions)

	// Snapshot existing sessions so we can identify the one this run creates.
	before, _ := p.client.ListSessions(ctx, cwd, 0)

	res, runErr := p.client.Run(ctx, opencode.RunOptions{
		Prompt: prompt,
		Dir:    cwd,
		Model:  model,
		Title:  params.Title,
	})
	if res == nil {
		return "", fmt.Errorf("opencode run failed: %w", runErr)
	}

	sessionID := p.detectNewSession(ctx, cwd, before)

	return formatRunResult("OpenCode Development", sessionID, cwd, model, res, runErr), nil
}

// callContinueSession resumes an existing OpenCode session with a follow-up message.
func (p *Plugin) callContinueSession(ctx context.Context, payload string) (string, error) {
	var params continueSessionParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(params.Message) == "" {
		return "", fmt.Errorf("message is required")
	}

	cwd := p.resolveCwd(params.Cwd)
	model := p.resolveModel(params.Model)

	res, runErr := p.client.Run(ctx, opencode.RunOptions{
		Prompt:    params.Message,
		Dir:       cwd,
		Model:     model,
		SessionID: params.SessionID,
	})
	if res == nil {
		return "", fmt.Errorf("opencode run failed: %w", runErr)
	}

	return formatRunResult("OpenCode Session Continued", params.SessionID, cwd, model, res, runErr), nil
}

// callCheckSession exports an existing session and returns its transcript.
func (p *Plugin) callCheckSession(ctx context.Context, payload string) (string, error) {
	var params checkSessionParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return "", fmt.Errorf("session_id is required")
	}

	cwd := p.resolveCwd(params.Cwd)

	// Best-effort metadata lookup so the summary has a title and directory.
	var meta *opencode.Session
	if sessions, err := p.client.ListSessions(ctx, cwd, 0); err == nil {
		for i := range sessions {
			if sessions[i].ID == params.SessionID {
				meta = &sessions[i]
				break
			}
		}
	}

	export, err := p.client.Export(ctx, cwd, params.SessionID, params.Sanitize)
	if err != nil {
		return "", fmt.Errorf("export session %s: %w", params.SessionID, err)
	}

	return formatCheckSession(params.SessionID, meta, export), nil
}

// callListSessions lists recent sessions, optionally scoped to a directory.
func (p *Plugin) callListSessions(ctx context.Context, payload string) (string, error) {
	var params listSessionsParams
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return "", fmt.Errorf("invalid payload: %w", err)
	}

	cwd := p.resolveCwd(params.Cwd)
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	sessions, err := p.client.ListSessions(ctx, cwd, 0)
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}

	return formatSessionList(sessions, cwd, limit), nil
}

// detectNewSession compares the session list after a run against a pre-run
// snapshot and returns the ID of the newly created session. It prefers a new
// session whose directory matches cwd, falling back to the newest new session.
// Returns "" if it cannot be determined.
func (p *Plugin) detectNewSession(ctx context.Context, cwd string, before []opencode.Session) string {
	after, err := p.client.ListSessions(ctx, cwd, 0)
	if err != nil {
		return ""
	}

	seen := make(map[string]bool, len(before))
	for _, s := range before {
		seen[s.ID] = true
	}

	var newest, newestInDir opencode.Session
	for _, s := range after {
		if seen[s.ID] {
			continue
		}
		if s.Created >= newest.Created {
			newest = s
		}
		if cwd != "" && s.Directory == cwd && s.Created >= newestInDir.Created {
			newestInDir = s
		}
	}

	if newestInDir.ID != "" {
		return newestInDir.ID
	}
	return newest.ID
}

// buildDevelopPrompt composes the message sent to OpenCode for a development task.
func buildDevelopPrompt(task, instructions string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(task))
	if instructions != "" {
		b.WriteString("\n\nAdditional instructions:\n")
		b.WriteString(strings.TrimSpace(instructions))
	}
	return b.String()
}

// formatRunResult renders the outcome of an opencode run into a text summary.
func formatRunResult(heading, sessionID, cwd, model string, res *opencode.RunResult, runErr error) string {
	var b strings.Builder
	status := "completed"
	if runErr != nil {
		status = "failed"
	}

	b.WriteString(fmt.Sprintf("=== %s %s ===\n\n", heading, status))
	if sessionID != "" {
		b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	} else {
		b.WriteString("Session: (unknown — run `list_sessions` to find it)\n")
	}
	if cwd != "" {
		b.WriteString(fmt.Sprintf("Working dir: %s\n", cwd))
	}
	if model != "" {
		b.WriteString(fmt.Sprintf("Model: %s\n", model))
	} else {
		b.WriteString("Model: (OpenCode config default)\n")
	}
	b.WriteString(fmt.Sprintf("Status: %s\n", status))
	if runErr != nil {
		b.WriteString(fmt.Sprintf("Error: %s\n", runErr.Error()))
	}
	b.WriteString("\n")

	b.WriteString("--- OpenCode Output ---\n\n")
	if out := strings.TrimSpace(res.Stdout); out != "" {
		b.WriteString(out)
		b.WriteString("\n")
	} else {
		b.WriteString("(no output captured)\n")
	}

	if errOut := strings.TrimSpace(res.Stderr); errOut != "" {
		b.WriteString("\n--- Diagnostics (stderr) ---\n\n")
		b.WriteString(errOut)
		b.WriteString("\n")
	}

	return b.String()
}

// formatCheckSession renders a session export into a text summary.
func formatCheckSession(sessionID string, meta *opencode.Session, export string) string {
	var b strings.Builder
	b.WriteString("=== OpenCode Session ===\n\n")
	b.WriteString(fmt.Sprintf("Session: %s\n", sessionID))
	if meta != nil {
		if meta.Title != "" {
			b.WriteString(fmt.Sprintf("Title: %s\n", meta.Title))
		}
		if meta.Directory != "" {
			b.WriteString(fmt.Sprintf("Directory: %s\n", meta.Directory))
		}
	}
	b.WriteString("\n--- Exported Transcript (JSON) ---\n\n")
	if out := strings.TrimSpace(export); out != "" {
		b.WriteString(out)
		b.WriteString("\n")
	} else {
		b.WriteString("(empty export)\n")
	}
	return b.String()
}

// formatSessionList renders the session list into a text summary.
func formatSessionList(sessions []opencode.Session, cwd string, limit int) string {
	var b strings.Builder
	b.WriteString("=== OpenCode Sessions ===\n\n")

	count := 0
	for _, s := range sessions {
		if cwd != "" && s.Directory != "" && s.Directory != cwd {
			continue
		}
		if count >= limit {
			break
		}
		b.WriteString(fmt.Sprintf("- %s\n", s.ID))
		if s.Title != "" {
			b.WriteString(fmt.Sprintf("    Title: %s\n", s.Title))
		}
		if s.Directory != "" {
			b.WriteString(fmt.Sprintf("    Directory: %s\n", s.Directory))
		}
		count++
	}

	if count == 0 {
		b.WriteString("(no matching sessions found)\n")
	}
	return b.String()
}
