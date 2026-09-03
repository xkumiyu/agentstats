# agentstats

agentstatsは、Codexの利用状況を確認するコマンドラインツールです。ローカルの履歴を集計し、Session・Tool・Skillの利用状況を表示します。

[English version](README.md)

> [!NOTE]
> 現在はCodexの履歴を対象としています。

## インストール

```sh
go install github.com/xkumiyu/agentstats/cmd/agentstats@latest
```

## 使い方

```sh
agentstats stats
agentstats tools
agentstats skills
```

- `stats`: 全体の利用状況
- `tools`: Toolごとの利用状況
- `skills`: Skillごとの利用状況と、起動方法・証拠の状態

Skillの集計単位をsessionに変更したり、確認済みの利用だけに絞ったりできます。

```sh
agentstats skills --group-by session
agentstats skills --strict
```

機械可読な出力が必要な場合は`--json`を指定します。

## 出力例

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

`agentstats skills`は、Skillごとに「起動方法」と「証拠の状態」という2つの軸を表示します。

| 項目 | 意味 |
| --- | --- |
| `Explicit` | `$skill-name`のような明示的なSkill指定、または構造化されたSkill呼び出しが記録されている。 |
| `Implicit` | 明示指定なしで、Skillの`SKILL.md`やscriptsへのruntime accessから利用を推定している。 |
| `Unknown` | Skill利用の証拠はあるが、明示的・暗黙的のどちらで起動されたか履歴から判別できない。 |
| `Confirmed` | Skillの指示やSkill item/toolが読み込まれた、または呼び出されたことを直接示す履歴がある。 |
| `Inferred` | ファイルやスクリプトへのruntime accessから利用を推定している。 |
| `Unconfirmed` | 明示的な指定はあるが、利用を確認できる証拠がない。 |
| `Total` | 重複を除いた利用回数。既定ではturn単位、`--group-by session`指定時はsession単位で数える。 |

起動方法と証拠の状態は別の軸です。1つの利用に複数の起動方法の証拠が含まれる場合があるため、内訳の合計が`Total`と一致しないことがあります。`--strict`を指定すると`Confirmed`だけを集計します。

## データの扱い

Codexホーム配下の履歴だけを読み取り、元のファイルは変更しません。履歴を外部へ送信せず、通常の出力にユーザー本文、コマンド本文、その他のraw event詳細を含めません。
