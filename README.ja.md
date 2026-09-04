# agentstats

agentstatsは、Codexの利用状況を確認するコマンドラインツールです。ローカルの履歴を集計し、Session, Tool, Skillの利用状況を表示します。

[English version](README.md)

> [!NOTE]
> 現在はCodexの履歴を対象としています。

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

## 出力例: `agentstats tools`

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

## データの扱い

Codexホーム配下の履歴だけを読み取り、元のファイルは変更しません。履歴を外部へ送信せず、通常の出力にユーザー本文、コマンド本文、その他のraw event詳細を含めません。
