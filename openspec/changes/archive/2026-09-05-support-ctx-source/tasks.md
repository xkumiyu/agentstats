## 1. 入力ソース境界と共通メタデータ

- [x] 1.1 既存の Codex 入力を維持したまま、入力ソース（`codex` / `ctx`）と canonical agent ID を扱う共通の source context を usage 層へ追加し、source・agent・provider session の識別情報を後段へ渡せるようにする。既存の Codex 集計結果と JSON の `agent` 値が変わらないことを既存テストで確認する。
- [x] 1.2 CLI の `--source`、`--codex-home`、`--ctx-data-root` を定義し、デフォルトを `codex` とする。ソース固有オプションの不正な組み合わせと、Codex と ctx の同時指定をエラーにする。`stats`、`tools`、`skills` の CLI テストで各入力境界と終了コードを確認する。

## 2. ctx イベントストリームの読み込み

- [x] 2.1 ctx の公開 read-only event enumeration を呼び出す入力アダプターを追加し、`--ctx-data-root` を `ctx --data-root` へ渡せるようにする。コマンド実行部分を差し替え可能にし、stdout・stderr・終了コード・実行失敗を合成テストで検証する。
- [x] 2.2 ctx の JSONL 出力をページ、cursor、完了状態を含む内部イベント列へ変換し、cursor を追跡して全ページを読み終えるまで取得する。空ページ、最終ページ、cursor の不変、ストリーム未完了、壊れた JSONL を合成イベントとテストで確認し、未完了データを成功扱いしないことを確認する。
- [x] 2.3 ctx イベントから agent/provider/session/event ID、時刻、イベント種別、payload を抽出し、未知または個別に壊れたイベントを warning 付きでスキップできるようにする。複数 agent と同一 provider session ID の fixture で、source と agent を含む一意なセッション識別子が衝突しないことを確認する。

## 3. ctx イベントの正規化と集計

- [x] 3.1 ctx のユーザーメッセージ、model activity、command completion/runtime、構造化された skill activity、skill marker を既存の turn・tool・skill evidence へ正規化する。injection や出力のみを prompt/tool call と誤認せず、invocation の重複計上を避けることを synthetic fixture で確認する。
- [x] 3.2 provider turn ID とイベント順序を使って ctx の turn 境界を復元し、source・agent を含む dedupe key と session key で重複除去する。異なる agent の同名 tool/skill は別イベントとして数えつつ、集計行では canonical name ごとに合算されることをテストする。
- [x] 3.3 ctx の全 agent を deterministic order で収集して集計入力へ渡し、Overview の Sessions、User Prompts、Tool Calls、Skill Uses が全 agent の合計になるようにする。Codex 単一ソースと ctx 単一 agent の既存互換性、および unknown agent の分離と warning をテストする。
- [x] 3.4 `--days`、strict mode、warning 表示、`--strict-input` を ctx 入力にも適用し、個別イベントの回復可能な問題と source 全体の失敗を区別する。各モードの終了コードと表示を CLI テストで確認する。

## 4. レポート出力

- [x] 4.1 human report の共通コンテキストに `Source:` と `Agents:` を追加し、ctx の複数 agent を `Codex, OpenCode` のように deterministic order で表示する。tool/skill の表では canonical name ごとの合算を表示し、agent 別の表を新設しないことを出力テストで確認する。
- [x] 4.2 JSON report に `source` と `agents` 配列を追加し、後方互換の `agent` 文字列を Codex/単一 agent では従来値、複数 agent では canonical ID の comma-separated 値として保持する。キー順・agent 順・行順が deterministic で、既存 JSON consumer の `agent` 読み取りを壊さないことをテストする。
- [x] 4.3 human report の `Source` 行にCodexの有効なCodex homeまたは明示されたctx data rootを括弧内でcompactに表示し、別の `History` / `Data root` 行を追加しない。Codexのpath短縮、ctxの明示指定時だけのpath表示、およびJSON不変を出力テストで確認する。

## 5. 未使用スキル報告

- [x] 5.1 `skills --unused` の inventory identity を canonical skill name と absolute physical path の組み合わせとして維持し、同名・異なる PATH の未使用 skill を複数行で human/JSON 出力する。合成 inventory と既存 Codex history で physical count と path 表示を確認する。
- [x] 5.2 ctx history を使う `skills --unused` で全 agent の skill 使用を canonical name の union として判定し、いずれかの agent で使われた名前を未使用から除外する。異なる PATH の同名 skill が同時に存在する場合の既存 name-level semantics と、ctx の source/agents 表示をテストする。
- [x] 5.3 `skills --unused` の inventory root と history source を独立して扱い、`--source` と `--ctx-data-root` の source 境界、既存の Codex inventory option、エラー表示を確認する。Codex default の挙動が変わらないことを CLI 回帰テストで確認する。

## 6. ドキュメント、fixture、品質確認

- [x] 6.1 `README.md` と `README.ja.md` に source 選択、`CODEX_HOME` と `--codex-home` の優先順、ctx data root、ctx の複数 agent 集計、human/JSON の `Source`・`Agents`・`agent` 互換性、`skills --unused` の同名別 PATH 行を追記し、両 README のコマンド・オプション・節構成が同等であることを確認する。
- [x] 6.2 ctx の synthetic JSONL/page fixture と入力・正規化・集計・出力・unused のテストを追加し、実ユーザーの ctx DB や個人データをリポジトリへ持ち込まない。fixture が複数 agent、unknown agent、pagination、partial failure、同名別 PATH を網羅することを確認する。
- [x] 6.3 formatter、lint、vet、race test、build、npm wrapper test、npm package contents を含む `mise run check` と OpenSpec validation を実行し、Codex default と ctx source の双方が完了条件を満たすことを確認する。
