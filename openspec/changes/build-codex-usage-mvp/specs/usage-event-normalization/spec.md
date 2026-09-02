## Purpose

Codex固有の履歴recordを、二重計上を避けながらsession・prompt・Tool・Skillの意味が明確で検出根拠を追跡できる共通利用Eventへ変換する。

## ADDED Requirements

### Requirement: sessionとuser promptを識別する
システムは `session_meta` のsession IDごとに1つのsessionを数えなければならない（SHALL）。user promptは実際のuser入力だけを数え、developer・system message、Skill本文の注入、tool outputをuser promptとして数えてはならない（MUST NOT）。

#### Scenario: 同一sessionに複数recordがある
- **WHEN** 同じsession IDに属する複数の履歴recordを処理する
- **THEN** session数は1となり、実際のuser入力だけがuser prompt数へ加算される

#### Scenario: 注入されたSkill本文がuser roleで保存される
- **WHEN** contextual inputとして `<skill>` blockを含むuser role messageが保存されている
- **THEN** システムはそのmessageをSkill検出の証拠として扱い、user prompt数には加算しない

### Requirement: Tool観測layerを区別する
システムは `response_item` の `function_call` と `custom_tool_call` を `model` layerのTool観測として扱わなければならない（SHALL）。`event_msg.ItemCompleted` の実行項目は `runtime` layerのTool観測として扱わなければならない（SHALL）。各観測はraw name、canonical name、callまたはitem ID、完了status、およびsource位置を取得できる範囲で保持しなければならない（SHALL）。

#### Scenario: modelがcode-mode execを選択する
- **WHEN** `custom_tool_call` のnameが `exec` である
- **THEN** システムはcanonical name `exec`、layer `model` のTool観測を生成する

#### Scenario: command実行が完了する
- **WHEN** `ItemCompleted` が `CommandExecution` を含む
- **THEN** システムはcanonical name `shell`、layer `runtime` のTool観測を生成する

#### Scenario: MCP Toolが完了する
- **WHEN** `ItemCompleted` が `McpToolCall` を含む
- **THEN** システムはMCP server名とTool名を失わないcanonical name、layer `runtime` のTool観測を生成する

#### Scenario: その他の既知runtime actionが完了する
- **WHEN** `ItemCompleted` がFile変更、Web検索、image閲覧、image生成、またはcollaboration actionを含む
- **THEN** システムはaction種別を表す安定したcanonical nameとlayer `runtime` のTool観測を生成する

### Requirement: effective Tool利用を二重計上しない
システムは通常のTool統計用に `effective` viewを生成しなければならない（SHALL）。同一turnにruntime観測がある場合はruntime観測を採用し、それを包むmodel layerの `exec` を加算してはならない（MUST NOT）。runtime観測が存在しない旧形式のturnではmodel観測をfallbackとして採用しなければならない（SHALL）。失敗して完了したToolも1回の利用として数えなければならない（SHALL）。

#### Scenario: execが2つのruntime actionを実行する
- **WHEN** 1つのmodel `exec` に対応するturnで2つのruntime actionが完了する
- **THEN** effective Tool利用はruntime action 2回となり、外側の `exec` は追加の1回として数えられない

#### Scenario: runtime eventのない旧履歴を読む
- **WHEN** turnにmodel Tool callはあるが対応するruntime観測がない
- **THEN** effective viewはmodel Tool callをfallbackとして数える

#### Scenario: Toolがerrorで完了する
- **WHEN** runtime Toolの完了statusが失敗である
- **THEN** effective viewはCallsを1増やし、Failuresも1増やす

### Requirement: Skill利用を証拠種別とともに検出する
システムはSkill専用recordの存在を前提にしてはならない（MUST NOT）。注入されたstructured `<skill>` blockを `explicit-injected`、structured Skill toolの実行を `structured-tool`、canonicalな先頭 `$skill-name` user入力を `explicit-request`、既知Skillの `SKILL.md` または `scripts` 配下への実行時accessを `implicit-access` として識別しなければならない（SHALL）。各Skill観測はSkill名、利用mode、検出方式、確認状態、timestamp、session、turn、およびsource位置を保持しなければならない（SHALL）。

#### Scenario: Skillがcontextへ正常に注入される
- **WHEN** contextual user inputに有効な `<skill>` blockとSkill名が含まれる
- **THEN** システムはmode `explicit`、method `explicit-injected`、state `confirmed` のSkill観測を生成する

#### Scenario: structured Skill toolが実行される
- **WHEN** native `Skill` toolまたはnamespaced `skills.read` が特定Skillを対象に完了する
- **THEN** システムはmethod `structured-tool`、state `confirmed` のSkill観測を生成する

#### Scenario: userがcanonical markerを入力する
- **WHEN** 実際のuser入力が先頭の `$skill-name` で始まり、対応する注入またはstructured Toolの証拠がない
- **THEN** システムはmethod `explicit-request`、state `unconfirmed` のSkill観測を生成する

#### Scenario: Skill fileへ実行時accessする
- **WHEN** runtime commandが具体的な既知Skillの `SKILL.md` またはそのSkillの `scripts` 配下を読み取りまたは実行する
- **THEN** システムはmode `implicit`、method `implicit-access`、state `inferred` のSkill観測を生成する

#### Scenario: prose内でSkillらしい文字列に言及する
- **WHEN** userまたはassistantの通常文中に `$skill-name` や `SKILL.md` という文字列が現れるだけで、上記の構造または実行証拠を満たさない
- **THEN** システムはSkill観測を生成しない

### Requirement: Skill利用をturn単位で重複排除する
システムは同一session・turn・Skillに属する複数の証拠を1回のSkill利用へ統合しなければならない（SHALL）。確認状態の優先順位は `confirmed`、`inferred`、`unconfirmed` とし、すべての検出方式は根拠一覧として保持しなければならない（SHALL）。同じSkillでも異なるturnまたは異なるsessionで利用された場合は別の利用として数えなければならない（SHALL）。

#### Scenario: requestと注入の両方が記録される
- **WHEN** 同一turn・Skillに `explicit-request` と `explicit-injected` が存在する
- **THEN** システムはstate `confirmed` の1回として数え、両方のmethodを根拠として保持する

#### Scenario: 同一turnでSKILL.mdを複数回読む
- **WHEN** 同じsession・turnで同一Skillの `SKILL.md` accessが複数回記録される
- **THEN** システムは1回のSkill利用として数える

#### Scenario: 複数turnで同じSkillを利用する
- **WHEN** 同一sessionの異なる2つのturnで同一Skillが検出される
- **THEN** システムは2回のSkill利用として数える

### Requirement: Skill名を安全に解決する
システムはstructured evidence内のname、履歴に保存されたSkill metadata、`SKILL.md` frontmatterの `name`、Skill directory名の順でSkill名を解決しなければならない（SHALL）。現在のfilesystemにSkill fileが存在しなくても、履歴に十分なpathまたはmetadataがあれば過去の利用を破棄してはならない（MUST NOT）。

#### Scenario: 使用後にSkillが削除されている
- **WHEN** 履歴には `/skills/example/SKILL.md` へのaccessがあるが現在のfileは存在しない
- **THEN** システムは利用可能な履歴metadataまたはdirectory名から `example` を解決する

#### Scenario: frontmatter nameとdirectory名が異なる
- **WHEN** 読取可能な `SKILL.md` のfrontmatter `name` がdirectory名と異なる
- **THEN** システムはfrontmatterのnameをcanonical Skill名として使用する
