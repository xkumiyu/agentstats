# agentstats

Codex のローカル session 履歴から、Tool と Skill の利用状況を集計する MVP CLI です。履歴は read-only で処理し、外部 network や永続 DB は使用しません。

## Build

```sh
go build -o agentstats ./cmd/agentstats
```

Go 1.27 を使用します。

## Commands

```sh
agentstats stats [options]
agentstats tools [options]
agentstats skills [options]
```

既定の出力は title、期間・filter、見出し付きの summary/table、footer を持つ静的な human-readable report です。デバッグ dump や system log のような raw event 列は表示しません。count は桁区切り、数値は右揃え、`Last Used` は local timezone 付きで表示します。

共通 option:

- `--days N`: 直近 N 日（1 以上）。省略時は全期間。
- `--codex-home PATH`: 読み取る Codex home。省略時は `CODEX_HOME`、`~/.codex` の順。
- `--color auto|always|never`: human report の装飾。`auto` は TTY のみ、`NO_COLOR` も尊重します。
- `--verbose`: skip した履歴の warning を file・line 単位で stderr に表示します。省略時は件数・理由・対象 file 数の要約を1行だけ表示します。
- `--strict-input`: skip された履歴がある場合に非0で終了します（CI向け）。
- `--json`: schema version 1 の JSON。
- `--csv`: CSV（`--json` とは併用不可）。

`tools` は `--layer effective|runtime|model` を受け付けます。既定の `effective` は runtime action を優先し、code-mode の外側の `exec` を二重計上しません。

`skills` は Skill ごとに `Explicit`、`Implicit`、`Unknown`、`Confirmed`、`Inferred`、`Unconfirmed`、`Total`、`Last Used` を表示します。`Total` は同一 session・turn 内で重複排除した「利用が観測された turn 数」です。1つのturnに複数のmodeの証拠がある場合、`Explicit` と `Implicit` はそれぞれ加算されるため、内訳の合計が `Total` を超えることがあります。`Unknown` は Skill本文がロードされたことは確認できるものの、explicit/implicitの起動経路を履歴から特定できない場合です。

Skill の検出根拠は、Codexのselected-skill instruction注入・UserMessageのskill item、その他の `<skill>` block、structured Skill tool、先頭の `$skill-name`、既知Skill pathへのruntime accessです。初期Skill一覧・description・候補選択ログは利用として数えず、アシスタントの説明文だけでも確定しません。`--strict` を指定すると `confirmed` の証拠だけを集計します。`implicit` のruntime accessは、履歴から静的に推定したものです。

端末幅が狭い場合は補助 column を省略した compact layoutへ切り替え、長い名前は `…` で省略します。TTY でない出力、`NO_COLOR`、`--color never` では ANSI escape sequence を生成しません。JSON/CSV は `--color always` でも常に ANSI-free です。

## Privacy and scope

対象は Codex の `sessions` と `archived_sessions` 配下の JSONL のみです。user本文、raw command、tool output、Skill本文は保存・表示せず、必要な名前・status・timestamp・source位置だけを集計します。既知だが集計に不要なmetadataは黙ってskipし、未知または破損した record は warning を stderr に要約表示して処理を継続します。詳細が必要な場合は `--verbose`、skip を許容しない場合は `--strict-input` を指定します。

この MVP は Codex のみを対象とし、OpenCode、他 Agent、token/cost 集計、interactive TUI、Web UI、network service、永続 DB は対象外です。
