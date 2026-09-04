# agentstats

agentstatsは、Codexの利用状況を確認するコマンドラインツールです。ローカルの履歴を集計し、Session・Tool・Skillの利用状況を表示します。

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

人間向けのSkill利用表は、`--view`で表示内容を切り替えられます。

```sh
agentstats skills --view mode
agentstats skills --view state
agentstats skills --view all
```

既定の`--view auto`は端末幅に応じて`compact`、`mode`、`all`を選び、
context行に`View: auto (selected: mode)`のような形式で実際に選ばれたviewを表示します。`mode`は起動方法の証拠、
`state`は証拠の状態と`Last Used`、明示した`all`は両方の表を分けて表示します。

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

## Tips

### 使っていないSkillを列挙する

組み込みの未使用viewを使います。

```sh
agentstats skills --unused
```

既定では`~/.agents/skills`だけをrecursiveに探索します。ただし、対象は`.agents/skills`、`.codex/skills`、`.codex/skills/.system`、既知のplugin cache layoutに限られます。既定ディレクトリが存在しない場合は、空のscopeとして扱います。

リポジトリ内のSkillを対象にする場合は、リポジトリを置いている親ディレクトリを`--root`で指定します。`--root`は複数回指定でき、指定した場合は既定rootを追加せず置き換えます。

```sh
# ~/src配下の全リポジトリ:
agentstats skills --unused --root ~/src

# 個人Skillと~/src配下のリポジトリ:
agentstats skills --unused --root ~/.agents/skills --root ~/src
```

`--json`で機械可読なrowsを出力できます。`--strict`は`Confirmed`の履歴だけを使用済みとし、`--days 30`は直近30日間の履歴だけを比較対象にします。

有効な`SKILL.md`のfrontmatterに`name`があればcanonical nameとして使い、ない場合や無効な場合はSkillディレクトリ名へfallbackします。JSONには`name_source`と`name_mismatch`も含まれるため、frontmatterとディレクトリ名の差を確認できます。比較は大文字小文字を変換しない完全一致です。従来の`find`/`jq`による手動比較は不要です。手動でfilesystemを確認する場合は、互換性の高い`find`が便利です。`fd`はインストール済みなら高速で扱いやすい一方、hidden・gitignore対象も含めるため`fd -H -I`（または`fd -u`）を指定してください。

## データの扱い

Codexホーム配下の履歴だけを読み取り、元のファイルは変更しません。履歴を外部へ送信せず、通常の出力にユーザー本文、コマンド本文、その他のraw event詳細を含めません。
