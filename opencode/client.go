// Package opencode provides a thin wrapper around the local `opencode` CLI
// (https://opencode.ai/docs/cli). It drives OpenCode in non-interactive mode
// by shelling out to `opencode run`, and exposes helpers for listing and
// exporting sessions.
package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBin is the binary name used when no explicit path is configured.
	DefaultBin = "opencode"

	// DefaultRunTimeout caps how long a single `opencode run` may take.
	DefaultRunTimeout = 30 * time.Minute

	// quickTimeout caps fast, read-only commands (session list, export).
	quickTimeout = 2 * time.Minute
)

// Client invokes the local opencode CLI.
type Client struct {
	// Bin is the path to (or name of) the opencode binary. Defaults to "opencode".
	Bin string

	// ConfigJSON is an inline OpenCode config document. When set it is passed
	// to opencode via OPENCODE_CONFIG_CONTENT and takes precedence over ConfigPath.
	ConfigJSON string

	// ConfigPath is the path to an OpenCode config file, passed via OPENCODE_CONFIG.
	ConfigPath string

	// SkipPermissions, when true, adds --dangerously-skip-permissions so the
	// run can edit files and execute commands without interactive approval.
	SkipPermissions bool

	// OutputFormat is "default" (human-readable) or "json" (raw JSON events).
	OutputFormat string

	// RunTimeout caps a single `opencode run` invocation. Defaults to DefaultRunTimeout.
	RunTimeout time.Duration
}

// New returns a Client with sensible defaults applied.
func New(bin string) *Client {
	if bin == "" {
		bin = DefaultBin
	}
	return &Client{
		Bin:          bin,
		OutputFormat: "default",
		RunTimeout:   DefaultRunTimeout,
	}
}

// Session is one entry from `opencode session list --format json`.
type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Updated   int64  `json:"updated"`
	Created   int64  `json:"created"`
	ProjectID string `json:"projectId"`
	Directory string `json:"directory"`
}

// RunOptions configures a single `opencode run` invocation.
type RunOptions struct {
	// Prompt is the message sent to OpenCode (passed as the positional argument).
	Prompt string
	// Dir is the working directory OpenCode operates in (--dir). May be empty.
	Dir string
	// Model overrides the configured model, in "provider/model" form (--model). May be empty.
	Model string
	// SessionID resumes a specific existing session (--session). May be empty.
	SessionID string
	// Continue resumes the most recent session (--continue).
	Continue bool
	// Title sets the session title for a new run (--title). May be empty.
	Title string
}

// RunResult holds the captured output of an opencode invocation.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// Started reports whether the process actually started. When false the
	// binary could not be launched (e.g. not found on PATH).
	Started bool
}

// env returns the process environment with OpenCode config injected.
func (c *Client) env() []string {
	env := os.Environ()
	switch {
	case c.ConfigJSON != "":
		env = append(env, "OPENCODE_CONFIG_CONTENT="+c.ConfigJSON)
	case c.ConfigPath != "":
		env = append(env, "OPENCODE_CONFIG="+c.ConfigPath)
	}
	return env
}

// run executes the opencode binary with args and returns captured output.
func (c *Client) run(ctx context.Context, dir string, timeout time.Duration, args ...string) (*RunResult, error) {
	if timeout <= 0 {
		timeout = DefaultRunTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, c.Bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = c.env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &RunResult{
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
		Started: true,
	}

	if cctx.Err() == context.DeadlineExceeded {
		return res, fmt.Errorf("opencode timed out after %s", timeout)
	}

	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			// The process never started (binary missing, permission denied, ...).
			res.Started = false
			return res, fmt.Errorf("could not start %q: %w", c.Bin, err)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, fmt.Errorf("opencode exited with code %d", res.ExitCode)
		}
		return res, fmt.Errorf("opencode run: %w", err)
	}

	return res, nil
}

// Run invokes `opencode run` with the given options and waits for completion.
func (c *Client) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	args := []string{"run"}

	format := c.OutputFormat
	if format == "" {
		format = "default"
	}
	args = append(args, "--format", format)

	if opts.Dir != "" {
		args = append(args, "--dir", opts.Dir)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SessionID != "" {
		args = append(args, "--session", opts.SessionID)
	}
	if opts.Continue {
		args = append(args, "--continue")
	}
	if opts.Title != "" {
		args = append(args, "--title", opts.Title)
	}
	if c.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	// The prompt is the positional message argument and must come last.
	args = append(args, opts.Prompt)

	return c.run(ctx, opts.Dir, c.RunTimeout, args...)
}

// ListSessions returns sessions known to OpenCode, newest first. When maxCount
// is greater than zero it caps the number returned.
func (c *Client) ListSessions(ctx context.Context, dir string, maxCount int) ([]Session, error) {
	args := []string{"session", "list", "--format", "json"}
	if maxCount > 0 {
		args = append(args, "--max-count", strconv.Itoa(maxCount))
	}

	res, err := c.run(ctx, dir, quickTimeout, args...)
	if err != nil {
		return nil, err
	}

	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return nil, nil
	}

	var sessions []Session
	if err := json.Unmarshal([]byte(out), &sessions); err != nil {
		return nil, fmt.Errorf("parse session list: %w", err)
	}
	return sessions, nil
}

// Export returns the full session transcript as JSON via `opencode export`.
// When sanitize is true, sensitive transcript and file data is redacted.
func (c *Client) Export(ctx context.Context, dir, sessionID string, sanitize bool) (string, error) {
	args := []string{"export", sessionID}
	if sanitize {
		args = append(args, "--sanitize")
	}

	res, err := c.run(ctx, dir, quickTimeout, args...)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}
