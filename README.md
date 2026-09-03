# agentstats

agentstats is a command-line tool for inspecting Codex usage. It aggregates local history and reports session, tool, and skill usage.

[日本語版 / Japanese](README.ja.md)

> [!NOTE]
> Currently, agentstats supports Codex history.

## Installation

```sh
go install github.com/xkumiyu/agentstats/cmd/agentstats@latest
```

## Usage

```sh
agentstats stats
agentstats tools
agentstats skills
```

- `stats`: Overall usage summary
- `tools`: Usage by tool
- `skills`: Usage by skill, including activation mode and evidence state

Skill results can be grouped by session and limited to confirmed usage:

```sh
agentstats skills --group-by session
agentstats skills --strict
```

Choose the human-readable Skill usage view when the automatic table layout is
not the most useful one:

```sh
agentstats skills --view mode
agentstats skills --view state
agentstats skills --view all
```

The default `--view auto` selects `compact`, `mode`, or `all` from the
terminal width and reports it as `View: auto (selected: mode)` in the context
lines. `mode`
shows activation evidence, `state` shows evidence state and `Last Used`, and
explicit `all` shows the two tables separately so they remain readable on
ordinary terminals.

Use `--json` when machine-readable output is needed.

## Example output

```text
TOOL USAGE
Agent: Codex
Period: all time
Layer: effective

Tool       Calls  Failures  Last Used
────────────────────────────────────────────────────
shell          42         0  2026-09-03 14:20 JST

1 tool, 42 calls total
```

## Understanding skill usage

`agentstats skills` reports two independent dimensions: activation mode and evidence state.

| Field | Meaning |
| --- | --- |
| `Explicit` | An explicit skill request or invocation was observed, such as `$skill-name` or a structured Skill call. |
| `Implicit` | Usage inferred from runtime access to a skill's `SKILL.md` or scripts without an explicit request. |
| `Unknown` | Skill evidence was found, but the history does not reveal whether activation was explicit or implicit. |
| `Confirmed` | The history contains direct evidence that the skill instructions or a skill item/tool was loaded or invoked. |
| `Inferred` | Usage inferred from runtime file or script access. |
| `Unconfirmed` | An explicit request was observed, but no confirming evidence was found. |
| `Total` | Deduplicated usage count: once per turn by default, or once per session with `--group-by session`. |

Mode and state are independent. A single usage can have evidence for more than one mode, so the mode counts do not always add up to `Total`. `--strict` includes only `Confirmed` usage.

## Tips

### List skills with no recorded usage

Use the built-in unused view:

```sh
agentstats skills --unused
```

By default, only `~/.agents/skills` is scanned. The scan is recursive, but only recognized `.agents/skills`, `.codex/skills`, `.codex/skills/.system`, and plugin-cache layouts are included. An absent default directory is treated as an empty scope.

Use `--root` to scan repository-local Skills. It can be repeated; specifying it replaces the default root rather than adding to it:

```sh
# All repositories below ~/src:
agentstats skills --unused --root ~/src

# Personal Skills plus repositories below ~/src:
agentstats skills --unused --root ~/.agents/skills --root ~/src
```

Add `--json` for machine-readable rows, `--strict` to count only `Confirmed` history, or `--days 30` to compare against the last 30 days:

```sh
agentstats skills --unused --strict --days 30 --json
```

When a valid `SKILL.md` frontmatter `name` exists, it is used as the canonical name; otherwise the Skill directory name is used. The JSON output includes `name_source` and `name_mismatch` so frontmatter and directory-name differences are visible. Matching is exact and case-sensitive. The built-in command avoids the old manual `find`/`jq` comparison. For ad-hoc filesystem inspection, `find` is the most portable choice; `fd` is faster and friendlier when available, but use `fd -H -I` (or `fd -u`) to include hidden and ignored paths.

## Data handling

agentstats reads only history under the Codex home directory and never modifies those files. It does not send history externally, and normal reports do not include prompt text, command text, or other raw event details.
