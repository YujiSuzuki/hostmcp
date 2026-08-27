# HostMCP - Conventions for AI Assistants

> **Language policy:** This file is written in English. Source code comments in this repo follow the bilingual EN/JA convention (an English explanation immediately followed by its Japanese translation) already used throughout `internal/`.

## Changing an MCP Tool's Argument Contract

`internal/mcp` defines each MCP tool's argument schema (in `Get*Tools()` functions like `GetHostTools()`), but that schema is not the only implementation of the wire contract it describes: `internal/cli`'s `HTTPBackend` methods (`backend.go`) independently reconstruct the arguments for the same tools, for the `hostmcp client` CLI fallback path. Nothing ties these two to the same source of truth.

**When you add, rename, or change the meaning of an MCP tool's argument, grep `internal/cli` for that tool name and argument, and update/test the CLI side in the same change.** A tool argument added only in `internal/mcp` will silently never be sent by CLI callers — they won't error, they'll just omit it, which is exactly what happened before `client_timeout_seconds` was forwarded (see `internal/cli/backend.go`'s `RunHostTool` and `internal/mcp/tools_host.go`'s `CheckClientTimeoutArgs`).

Where the server has real validation/decision logic for an argument (not just "is it present"), prefer exporting that logic as a pure function (as `CheckClientTimeoutArgs` is) so `internal/cli`'s tests can assert against the actual logic instead of a hand-rolled mock. A mock that unconditionally echoes success cannot catch drift between what the CLI sends and what the server actually requires — that gap is exactly how the bug above went unnoticed for a month.

## Comment Conventions

This section documents comment style for the hostmcp repository. When nested inside a parent workspace that defines its own default comment style (see that workspace's root `CLAUDE.md`), this section intentionally overrides that default for this repository only.

## Why this repo comments differently

Code and tests in this repo should be understandable by a reader who does not want to trace through the implementation line by line. Comments here should let a reader understand what's happening — including things a terser house style would consider "self-evident" or "just restating the identifier." Verbosity here is deliberate, not a smell.

## Allowed comment patterns

The following patterns are accepted, deliberate style in this repo. `/ais-local-comment-review` and similar review tooling should not report these as Balance/Necessity issues:

- **Self-evident label comments** — a one-line comment naming what a following block does, even when the block is a single statement (e.g. `# Install required tools` above an `apt-get install` block).
- **Simple paraphrase of a command** — a comment that restates, in words, what the line(s) directly below it do.
- **Simple paraphrase, generally** — comments that add no information beyond what the adjacent code already expresses are fine here.
- **Simple paraphrase of an `if` block** — a comment above a conditional that says in prose what the condition/branch already expresses.
- **Explanations of general language semantics** — e.g. noting that `init()` runs automatically on package import, even though that's true of Go generally and not specific to this project.
- **One-line comments directly above test assertions** — e.g. `// Verify the flag type is bool.` above `if flag.Value.Type() != "bool" { ... }`.

## Explicit non-application

- Godoc convention calls for a doc comment to lead with the declared name (e.g. `// TestClientListCommand verifies ...`). In this repo, drop that leading name when the declaration is immediately below and the name would just be repeated (e.g. `// Verifies that ...` directly above `func TestClientListCommand(t *testing.T)`). This is the one place terseness wins over this file's usual verbosity preference — repeating the identifier a second time in prose is redundant, not explanatory.

## Guiding principle

Write comments assuming the reader will not carefully parse the adjacent code — spell out what a variable, flag, or block is or does explicitly, even when it seems obvious from the identifier name or surrounding code.

## What still applies

This file relaxes Balance/Necessity-style findings only. It does not exempt comments from being factually accurate (Accuracy) or from being unambiguous to a first-time reader (Clarity) — stale or misleading comments should still be fixed.
