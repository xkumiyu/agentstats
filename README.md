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

Use `--json` when machine-readable output is needed.

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

## Data handling

agentstats reads only history under the Codex home directory and never modifies those files. It does not send history externally, and normal reports do not include prompt text, command text, or other raw event details.
