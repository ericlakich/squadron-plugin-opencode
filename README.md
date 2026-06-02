# squadron-plugin-opencode

A [Squadron](https://github.com/mlund01/squadron-sdk) plugin that lets an agent spawn a **local [OpenCode](https://opencode.ai) session** and send it code-development instructions.

The plugin drives the local `opencode` CLI in non-interactive mode (`opencode run`). OpenCode runs on the same machine as Squadron, edits files directly in a working directory you choose, and can run commands. You supply an OpenCode config (the same `opencode.json` document you'd use locally) so you control the provider and model — for example a Bedrock-compatible endpoint serving Qwen3 Coder.

This plugin uses the [OpenCode CLI](https://opencode.ai/docs/cli) and [config](https://opencode.ai/docs/config) documented at [opencode.ai/docs](https://opencode.ai/docs).

## Tools

### `code_develop`

Spawns a local OpenCode session and has it work on a coding task. OpenCode runs in the given working directory, implements the requested changes, and returns a summary of what it did. Use this for feature development, bug fixes, refactoring, or any code changes on a local project.

Returns a **session ID** that can be passed to `continue_session` or `check_session`.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `task` | string | yes | Description of the development task to perform |
| `cwd` | string | no | Absolute path to the local project directory OpenCode should work in. Falls back to the `default_cwd` setting. |
| `model` | string | no | Model override in `provider/model` form. Falls back to `default_model`, then the OpenCode config's `model`. |
| `instructions` | string | no | Additional context, constraints, or coding guidelines |
| `title` | string | no | Title for the OpenCode session |

### `continue_session`

Sends a follow-up instruction to an existing OpenCode session, resuming where it left off with full prior context. Use this to iterate on work started by `code_develop`.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `session_id` | string | yes | The OpenCode session ID to resume (e.g. `ses_17ab050c4ffeZIB2FNgaLWUx3M`) |
| `message` | string | yes | The follow-up instruction or question to send to the session |
| `cwd` | string | no | Absolute path to the project directory. Falls back to `default_cwd`. |
| `model` | string | no | Model override in `provider/model` form. Falls back to `default_model`, then the config default. |

### `check_session`

Inspects an existing OpenCode session. Returns the session metadata plus the full exported transcript (messages and tool calls) as JSON, via `opencode export`. Use this to review what a session did in detail.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `session_id` | string | yes | The OpenCode session ID to inspect |
| `cwd` | string | no | Absolute path to the project directory used for the lookup. Falls back to `default_cwd`. |
| `sanitize` | boolean | no | When `true`, redact sensitive transcript and file data from the export. Defaults to `false`. |

### `list_sessions`

Lists recent OpenCode sessions (newest first) with their IDs, titles, and working directories. Use this to discover the session ID of work done earlier.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `cwd` | string | no | When set, only sessions in that directory are returned. Falls back to `default_cwd`. |
| `limit` | integer | no | Maximum number of sessions to return. Defaults to `20`. |

## Prerequisites

- Go 1.23+ (to build the plugin)
- The [`opencode` CLI](https://opencode.ai/docs) installed on the machine that runs Squadron, and on `PATH` (or referenced via the `opencode_bin` setting)
- An OpenCode config that defines the provider and model you want to use (see below)
- Any credentials your config references (e.g. an API key or bearer token), supplied either as a Squadron secret via an `env_<NAME>` setting (recommended) or already present in the environment Squadron runs the plugin in

## OpenCode Config

The plugin passes your OpenCode config to the CLI. You can supply it two ways (see [Settings](#settings)):

- **Inline** via `config_json` — the full config document as a string. The plugin passes it to OpenCode through `OPENCODE_CONFIG_CONTENT`.
- **By path** via `config_path` — a path to an `opencode.json` file. The plugin passes it through `OPENCODE_CONFIG`.

If both are set, `config_json` wins. If neither is set, OpenCode uses its own default config discovery.

Example config (Bedrock-compatible endpoint serving Qwen3 Coder):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "bedrock-mantle": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Bedrock Mantle",
      "options": {
        "baseURL": "https://bedrock-mantle.us-east-1.api.aws/v1",
        "apiKey": "{env:AWS_BEARER_TOKEN_BEDROCK}"
      },
      "models": {
        "qwen.qwen3-coder-480b-a35b-instruct": { "name": "Qwen3 Coder 480B" },
        "qwen.qwen3-coder-30b-a3b-instruct":   { "name": "Qwen3 Coder 30B" },
        "google.gemma-3-27b-it": { "name": "Gemma 3 27B" },
        "anthropic.claude-opus-4-8": { "name": "Claude Opus 4.8" },
        "openai.gpt-oss-120b-1:0": { "name": "GPT-OSS 120B" }
      }
    }
  },
  "model": "bedrock-mantle/qwen.qwen3-coder-480b-a35b-instruct"
}
```

OpenCode resolves `{env:VAR}` placeholders (like `AWS_BEARER_TOKEN_BEDROCK` above) from the environment of the `opencode` process. Rather than relying on a local env file, supply these as **Squadron secrets**: any plugin setting prefixed with `env_` is exported to the `opencode` subprocess as that environment variable. For example, `env_AWS_BEARER_TOKEN_BEDROCK = vars.aws_bearer_token_bedrock` injects the secret so `{env:AWS_BEARER_TOKEN_BEDROCK}` resolves at run time. These `env_*` values override any value already present in the environment, and the plugin still inherits the rest of the process environment. See [Settings](#settings).

## Installation

### From a GitHub release (recommended)

Reference the plugin by its repo path and a released version. Squadron downloads the prebuilt binary for your platform from the matching [GitHub release](https://github.com/ericlakich/squadron-plugin-opencode/releases) — no local build step required.

```hcl
plugin "opencode" {
  source  = "github.com/ericlakich/squadron-plugin-opencode"
  version = "v0.0.4"
  settings {
    # ...see Configuration below
  }
}
```

Releases are produced by the `release.yml` workflow, which cross-compiles for darwin/linux/windows × amd64/arm64 and attaches the archives plus `checksums.txt` to each tagged release.

### Local build (for development)

Build the plugin yourself and install it into Squadron's plugin directory:

```bash
# Clone the plugin project
git clone git@github.com:ericlakich/squadron-plugin-opencode.git

# Build
cd squadron-plugin-opencode
go mod tidy
go build -o plugin .

# Install into Squadron's plugin directory
mkdir -p ~/.squadron/plugins/opencode/local
cp plugin ~/.squadron/plugins/opencode/local/plugin
```

A locally built plugin is referenced with `version = "local"` instead of `source` + a released version.

## Configuration

Add the plugin to your Squadron HCL config. Supply the OpenCode config inline:

```hcl
plugin "opencode" {
  source  = "github.com/ericlakich/squadron-plugin-opencode"
  version = "v0.0.4"
  settings {
    config_json = <<-JSON
      {
        "$schema": "https://opencode.ai/config.json",
        "provider": {
          "bedrock-mantle": {
            "npm": "@ai-sdk/openai-compatible",
            "name": "Bedrock Mantle",
            "options": {
              "baseURL": "https://bedrock-mantle.us-east-1.api.aws/v1",
              "apiKey": "{env:AWS_BEARER_TOKEN_BEDROCK}"
            },
            "models": {
              "qwen.qwen3-coder-480b-a35b-instruct": { "name": "Qwen3 Coder 480B" }
            }
          }
        },
        "model": "bedrock-mantle/qwen.qwen3-coder-480b-a35b-instruct"
      }
    JSON

    # Injected into the opencode subprocess so {env:AWS_BEARER_TOKEN_BEDROCK}
    # in the config above resolves from a Squadron secret, not a local env file.
    env_AWS_BEARER_TOKEN_BEDROCK = vars.aws_bearer_token_bedrock

    default_cwd      = "/Users/me/Projects/my-api"
    skip_permissions = "true"
    timeout_minutes  = "45"
  }
}
```

Or point at a config file on disk:

```hcl
plugin "opencode" {
  source  = "github.com/ericlakich/squadron-plugin-opencode"
  version = "v0.0.4"
  settings {
    config_path = "/Users/me/.config/opencode/opencode.json"
    default_cwd = "/Users/me/Projects/my-api"
  }
}
```

Then attach the tools to an agent:

```hcl
agent "developer" {
  model = models.anthropic.claude_sonnet_4
  tools = [
    plugins.opencode.code_develop,
    plugins.opencode.continue_session,
    plugins.opencode.check_session,
    plugins.opencode.list_sessions,
  ]
}
```

### Settings

| Setting | Required | Description |
|---------|----------|-------------|
| `config_json` | no | Inline OpenCode config document (JSON). Passed to OpenCode via `OPENCODE_CONFIG_CONTENT`. Takes precedence over `config_path`. |
| `config_path` | no | Path to an OpenCode config file. Passed via `OPENCODE_CONFIG`. Used only if `config_json` is unset. |
| `opencode_bin` | no | Path to the `opencode` binary. Defaults to `opencode` (resolved on `PATH`). |
| `default_cwd` | no | Default working directory used when a tool call omits `cwd`. |
| `default_model` | no | Default model override (`provider/model`) applied when a tool call omits `model`. |
| `skip_permissions` | no | `"true"` to pass `--dangerously-skip-permissions` so runs can edit files and run commands without interactive approval. Defaults to `"false"`. See the warning below. |
| `output_format` | no | `"default"` (human-readable) or `"json"` (raw JSON events) for OpenCode's output. Defaults to `"default"`. |
| `timeout_minutes` | no | Maximum time in minutes for a single OpenCode run. Defaults to `30`. Increase for long-running tasks. |
| `env_<NAME>` | no | Exported to the `opencode` subprocess as environment variable `<NAME>` (case preserved). Use to feed secrets from Squadron (e.g. `env_AWS_BEARER_TOKEN_BEDROCK = vars.aws_bearer_token_bedrock`) into the config's `{env:NAME}` placeholders. Overrides any inherited value of the same name. |

> **Permissions:** OpenCode normally asks for approval before editing files or running commands. In a headless Squadron run there is no one to approve, so a development task can stall or refuse to make changes. Setting `skip_permissions = "true"` (which adds `--dangerously-skip-permissions`) lets the session act autonomously — only enable it for directories and tasks you trust. As a finer-grained alternative, configure a [`permission`](https://opencode.ai/docs/permissions) block in your OpenCode config instead.

## How It Works

1. The agent invokes a tool with the required parameters.
2. The plugin shells out to the local `opencode` CLI, injecting your config via `OPENCODE_CONFIG_CONTENT` (inline) or `OPENCODE_CONFIG` (path):
   - `code_develop` → `opencode run --dir <cwd> [--model …] [--title …] "<task>"`
   - `continue_session` → `opencode run --session <id> --dir <cwd> [--model …] "<message>"`
   - `check_session` → `opencode export <id>` (with `--sanitize` when requested)
   - `list_sessions` → `opencode session list --format json`
3. The run blocks until OpenCode finishes (up to `timeout_minutes`), and the captured output is returned as a text summary.
4. For `code_develop`, the plugin determines the new session ID by diffing `opencode session list` before and after the run (preferring a new session whose directory matches `cwd`). If it cannot be determined, the summary says so and you can find it with `list_sessions`.

All tools respect context cancellation, so Squadron can terminate long-running sessions cleanly.

## Project Structure

```
squadron-plugin-opencode/
  main.go            # Entry point - registers the plugin with Squadron
  plugin.go          # ToolProvider implementation, tool definitions, result formatting
  opencode/
    client.go        # Wrapper around the local `opencode` CLI
  skills/
    opencode_develop.md   # Skill guide for agents using the tools
  go.mod
  go.sum
```

## License

See [LICENSE](LICENSE) for details.
