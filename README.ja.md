# agentstats

agentstatsは、Codexの利用状況を確認するコマンドラインツールです。ローカルの履歴を集計し、Session, Tool, Skillの利用状況を表示します。

[English version](README.md)

> [!NOTE]
> agentstatsは、Codexの履歴とctxが提供する読み取り専用イベントストリームに対応しています。

## クイックスタート

```sh
npx @xkumiyu/agentstats
```

## インストール

npmでインストールします。

```sh
npm install --global @xkumiyu/agentstats
```

または、[GitHub Releases](https://github.com/xkumiyu/agentstats/releases)からダウンロードできます。

または、ソースからインストールできます。

```sh
go install github.com/xkumiyu/agentstats/cmd/agentstats@latest
```

## 使い方

```sh
agentstats stats   # 全体の利用状況
agentstats tools   # Toolごとの利用状況
agentstats skills  # Skillごとの利用状況
```

機械可読な出力が必要な場合は`--json`を指定します。

## 履歴ソースの選択

デフォルトはCodexです。1回の実行で選択できるsourceは1つだけで、Codexとctxの
履歴は合算しません。ctxの履歴を使う場合は`--source ctx`を指定します。

## 出力例: `agentstats tools`

```text
TOOL USAGE
Source: ctx (/path/to/ctx-data)
Agents: Codex, OpenCode
Period: all time
Layer: effective

Tool       Calls  Failures  Last Used
────────────────────────────────────────────────────
shell          42         0  2026-09-03 14:20 JST

1 tool, 42 calls total
```

JSON出力には`source`とcanonicalな`agents`配列が追加されます。後方互換の
`agent`文字列も残ります。デフォルトのCodexソースでは`codex`、ctxの単一Agentでは
そのcanonical Agent ID、複数Agentでは決定的順序のcomma区切り値になります。

```json
{
  "source": "ctx",
  "agents": ["codex", "opencode"],
  "agent": "codex,opencode"
}
```

## Skill集計の見方

`agentstats skills`は、次の項目を表示します。

| 軸 | 項目 | 意味 |
| --- | --- | --- |
| activation mode | `Explicit` | `$skill-name`のような明示的なSkill指定、または構造化されたSkill呼び出しが記録されている。 |
| activation mode | `Implicit` | 明示指定なしで、Skillの`SKILL.md`やscriptsへのruntime accessから利用を推定している。 |
| activation mode | `Unknown` | Skill利用の証拠はあるが、明示的・暗黙的のどちらで起動されたか履歴から判別できない。 |
| evidence state | `Confirmed` | Skillの指示やSkill item/toolが読み込まれた、または呼び出されたことを直接示す履歴がある。 |
| evidence state | `Inferred` | ファイルやスクリプトへのruntime accessから利用を推定している。 |
| evidence state | `Unconfirmed` | 明示的な指定はあるが、利用を確認できる証拠がない。 |
| 集計 | `Total` | 重複を除いた利用回数。既定ではturn単位、`--group-by session`指定時はsession単位で数える。 |

activation modeとevidence stateは独立した軸です。1つの利用に複数のactivation modeの証拠が含まれる場合があるため、内訳の合計が`Total`と一致しないことがあります。`--strict`を指定すると`Confirmed`だけを集計します。

### Skill利用表の表示方法

Skill利用表の表示形式は、`--view`で切り替えられます。

```sh
agentstats skills --view mode
agentstats skills --view state
agentstats skills --view all
```

デフォルトの`--view auto`は端末幅に応じて`compact`、`mode`、`all`を選び、
context行に`View: auto (selected: mode)`のような形式で実際に選ばれたviewを表示します。`mode`はactivation evidence、
`state`はevidence stateと`Last Used`、明示した`all`は両方の表を分けて表示します。

## 未使用Skillの確認

`skills`に`--unused`を指定すると、選択した履歴ソースとインストール済みSkillの
inventoryを比較できます。

```sh
agentstats skills --source ctx --ctx-data-root /path/to/ctx-data \
  --unused --root /path/to/skills
```

inventoryのidentityはcanonical skill nameと絶対物理PATHの組み合わせです。そのため、
異なるPATHに同名Skillがある場合、その名前が未使用なら別々の行として表示されます。
使用済み判定はcanonical name単位のままです。選択したctxのいずれかのAgentがその名前を
使用していれば、その名前のinventory行はすべて使用済みとみなします。inventory root
（`--root`）と履歴ソース（`--source`）は独立しています。

## データの扱い

Codexの履歴またはctxの公開された読み取り専用イベントストリームだけを読み取り、
選択したデータを変更しません。履歴を外部へ送信せず、通常の出力にユーザー本文、
コマンド本文、その他のraw event詳細を含めません。
