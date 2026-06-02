# CLAUDE.md

Project guidance for Claude Code when working in this repository.

## Release version / README sync

The README documents how to install this plugin by repo path, which pins a
released version (e.g. `version = "v0.0.2"`). Keep these in sync:

- Whenever the release version changes (a new tag is cut, or the user asks to
  bump it), update **every** `version = "vX.Y.Z"` reference in `README.md` to
  match the new release. There are currently multiple (the Installation example
  and each Configuration example) — update all of them, not just one.
- After changing any version string, verify consistency:
  `grep -n 'version *= *"v0' README.md` — all results should show the same version.
- The version shown in the README should reflect the latest published GitHub
  release tag. Until a release exists, only `version = "local"` actually resolves.
