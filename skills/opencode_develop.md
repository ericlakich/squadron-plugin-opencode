# OpenCode Plugin Skill Guide

Use the OpenCode plugin to delegate local code development to an OpenCode session and retrieve results. OpenCode runs on the same machine as Squadron, edits files directly in a local working directory, and can run commands.

## Workflow

### 1. Develop Code with `code_develop`

Use `code_develop` to spawn a local OpenCode session and assign it a task. OpenCode works in the given directory, implements changes, and returns a summary.

**Required parameters:**
- `task` — clear description of what to implement

**Optional parameters:**
- `cwd` — absolute path to the local project directory (falls back to the plugin's `default_cwd`)
- `model` — model override in `provider/model` form (falls back to the plugin's `default_model`, then the config default)
- `instructions` — additional context, constraints, or coding guidelines
- `title` — a title for the session

**Tips for effective prompts:**
- Be specific about what to change and where in the codebase
- Mention coding conventions, test requirements, or files to modify
- Reference existing patterns in the repo when relevant
- Include acceptance criteria so OpenCode knows when the task is complete

**Example:**
```json
{
  "task": "Add cursor-based pagination to the GET /users API endpoint",
  "cwd": "/Users/me/Projects/my-api",
  "model": "bedrock-mantle/qwen.qwen3-coder-480b-a35b-instruct",
  "instructions": "Follow the existing pagination pattern used in the /orders endpoint. Add tests."
}
```

The response includes the session ID, working directory, model, status, and OpenCode's output. Save the session ID — you can pass it to `continue_session` or `check_session`.

### 2. Iterate with `continue_session`

Use `continue_session` to send a follow-up instruction to a session created by `code_develop`. The session resumes with full prior context.

**Required parameters:**
- `session_id` — the session ID returned by `code_develop`
- `message` — the follow-up instruction or question

**Optional parameters:**
- `cwd`, `model` — same meaning as in `code_develop`

**Example:**
```json
{
  "session_id": "ses_17ab050c4ffeZIB2FNgaLWUx3M",
  "message": "The new tests are failing on an off-by-one in the cursor. Please fix and re-run them."
}
```

### 3. Inspect with `check_session`

Use `check_session` to review what a session did. It returns the session metadata plus the full exported transcript (messages and tool calls) as JSON.

**Required parameter:**
- `session_id`

**Optional parameters:**
- `cwd` — directory context for the lookup
- `sanitize` — when `true`, redact sensitive transcript and file data

Use `check_session` when:
- You need OpenCode's detailed step-by-step actions, not just the run summary
- You want to review a session created earlier
- You need to confirm exactly which files were changed

### 4. Discover with `list_sessions`

Use `list_sessions` to find session IDs of earlier work. Returns recent sessions (newest first) with IDs, titles, and directories.

**Optional parameters:**
- `cwd` — only return sessions in that directory
- `limit` — maximum number of sessions (default 20)

## Interpreting Responses

**OpenCode Output section** — OpenCode's own account of what it did. Use this to understand the changes and decide next steps.

- If `Status: failed`, read the `Error` line and the `Diagnostics (stderr)` section. Common causes: the model/provider in the config is unreachable, missing credentials (e.g. an unset bearer token referenced by `{env:...}`), or permission prompts blocking a headless run (see the plugin's `skip_permissions` setting).
- If `Session: (unknown ...)`, the run completed but the plugin could not determine the new session ID. Use `list_sessions` (scoped to the same `cwd`) to find it.

## Notes

- OpenCode makes real changes to local files. Point `cwd` at the intended project, and review changes (e.g. `git diff`) before committing or merging.
- OpenCode operates on whatever is in the working directory; it does not clone repositories. Check out the branch you want before running.
