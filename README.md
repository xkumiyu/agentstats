# agentstats

agentstats is a command-line tool for inspecting Codex usage. It aggregates local history and reports session, tool, and skill usage.

[日本語版 / Japanese](README.ja.md)

> [!NOTE]
> agentstats supports:
> - Codex
> - [ctx](https://github.com/ctxrs/ctx)

## Quick start

```sh
npx @xkumiyu/agentstats
```

## Installation

Install with npm:

```sh
npm install --global @xkumiyu/agentstats
```

Or download from [GitHub Releases](https://github.com/xkumiyu/agentstats/releases).

Or install from source:

```sh
go install github.com/xkumiyu/agentstats/cmd/agentstats@latest
```

## Usage

```sh
agentstats stats   # Overall usage summary
agentstats tools   # Usage by tool
agentstats skills  # Usage by skill
```

Use `--json` when machine-readable output is needed.

### Choosing a history source

Codex is the default. Each invocation reads one source only; history from
Codex and ctx is never combined. Use `--source ctx` for ctx history.

## Example output

`agentstats tools`:

```text
TOOL USAGE
Source: Codex (~/.codex)
Agents: Codex
Period: all time
Layer: effective

Tool       Calls  Failures  Last Used
────────────────────────────────────────────────────
shell          42         0  2026-09-01 12:34 JST

1 tool, 42 calls total
```

## Understanding skill usage

`agentstats skills` reports the following fields:

| Dimension | Field | Meaning |
| --- | --- | --- |
| Activation mode | `Explicit` | An explicit skill request or invocation was observed, such as `$skill-name` or a structured Skill call. |
| Activation mode | `Implicit` | Usage inferred from runtime access to a skill's `SKILL.md` or scripts without an explicit request. |
| Activation mode | `Unknown` | Skill evidence was found, but the history does not reveal whether activation was explicit or implicit. |
| Evidence state | `Confirmed` | The history contains direct evidence that the skill instructions or a skill item/tool was loaded or invoked. |
| Evidence state | `Inferred` | Usage inferred from runtime file or script access. |
| Evidence state | `Unconfirmed` | An explicit request was observed, but no confirming evidence was found. |
| Summary | `Total` | Deduplicated usage count: once per turn by default, or once per session with `--group-by session`. |

Activation mode and evidence state are independent. A single usage can have evidence for more than one activation mode, so the mode counts do not always add up to `Total`. `--strict` includes only `Confirmed` usage.

### Choosing a skill usage view

Choose the Skill usage view with `--view`:

```sh
agentstats skills --view mode
agentstats skills --view state
agentstats skills --view all
```

The default `--view auto` selects `compact`, `mode`, or `all` from the
terminal width and reports it as `View: auto (selected: mode)` in the context
lines. `mode` shows activation evidence, `state` shows evidence state and
explicit `all` shows the two tables separately, including `Last Used`, so they
remain readable on ordinary terminals.

## Finding unused skills

Use `--unused` with `skills` to compare the selected history source with the
installed skill inventory:

```sh
agentstats skills --source ctx --ctx-data-root /path/to/ctx-data \
  --unused --root /path/to/skills
```

Inventory identity is the canonical skill name plus its absolute physical path.
Therefore, same-name skills at different paths are shown as separate rows when
the name is unused. Usage matching remains canonical-name based: if any selected
ctx agent used a name, all inventory rows with that name are considered used.
The inventory roots (`--root`) and history source (`--source`) are independent.

## Data handling

agentstats reads Codex history or ctx's public read-only event stream and never
modifies the selected data. It does not send history externally, and normal
reports do not include prompt text, command text, or other raw event details.
