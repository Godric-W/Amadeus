# Amadeus 开发进度

> 最近更新：2026-08-13
> 唯一目标架构：`docs/design.md`
> 当前阶段：E. Slash Command 重构
> 下一任务：E-01 Codex 风格命令模型

## 1. 文档规则

### 1.1 状态

- `TODO`：尚未开始。
- `DOING`：正在实施，同一时间只允许一个主任务处于该状态。
- `BLOCKED`：存在明确外部阻塞。
- `DONE`：任务范围内的代码、测试、文档和验收全部完成。
- `SKIPPED`：经设计确认不再实现。
- `SUPERSEDED`：历史实现曾完成，但已被当前架构取代。

### 1.2 完成定义

任务只有同时满足以下条件才能标记 `DONE`：

1. 改动符合 `docs/design.md` 的最终 Contract。
2. 针对性测试、受影响包测试和构建通过。
3. 涉及并发、取消、持久化或权限时覆盖失败路径与 race。
4. 用户可见行为、事件、配置和命令同步文档。
5. 新主链验收后删除对应旧链，不长期维护双事实源。

### 1.3 执行原则

- Codex 是 Runtime、Persistence、Context、Plan-guided ReAct、Slash Command 和 TUI 的主要骨架。
- Claude Code 是内置 Tool、文件 Diff 和 Approval UX 的主要行为参考。
- 每次实现一个可以端到端验收的纵向切片。
- A、B、C、D、E 的设计已冻结；实现中发现 Contract 问题时先修改 `docs/design.md`。
- 本文只记录实施任务、状态、依赖和出口，不再保存长篇架构审计过程。

## 2. 工作流总览

开发计划按 Runtime、Context、Tool、Event、Slash Command、Agent 和 Extensions 分阶段推进：

| 工作流 | 范围 | 设计状态 | 实施状态 |
|---|---|---|---|
| A. Runtime + Persistence | Thread、Session、Turn、Task、JSONL Rollout、SQLite Index、Resume | 已冻结 | `DONE` |
| B. Context + Prompt | BaseInstructions、ContextManager、Token、Projection、Compaction | 已冻结 | `DONE` |
| C. Tool + Approval | Tool Contract、Edit/Write、Permission、Approval、Command | 已冻结 | `DONE` |
| C-R. Tool Runtime Refactor | Claude Code 风格 Tool Contract、SessionPermissionContext、ApprovalCoordinator、统一执行链 | 已冻结 | `DONE` |
| D. Event + TUI | SessionEvent、InteractiveRequest、TurnItem、HistoryCell、Rich Inline Projection | 已冻结 | `DONE` |
| E. Slash Command | Codex 风格 SlashCommand、InputResult、单一 TUI 分发与 Application/Session 操作 | 待 D 接入 | `DOING` |
| F. Agent Engine | Plan-guided ReAct、`update_plan`、Plan Mode、中断与终态 | 待 E 接入 | 未开始 |
| G. Extensions + Release | MCP、Skill、Web、兼容迁移、发布验证 | 待主链稳定 | 未开始 |

推荐实施顺序：

```text
A Runtime Contract + Canonical Persistence
→ B Context/Prompt 与 C Tool/Approval 可并行推进
→ C-R Tool Runtime、SessionPermissionContext 与 ApprovalCoordinator 重构（`DONE`）
→ D Event Protocol 与 TUI Projection（`DONE`）
→ E Codex 风格 Slash Command 重构
→ F Plan-guided ReAct 整合
→ G Extensions/Release
```

## 3. A. Runtime + Persistence

### A 目标

```text
ThreadManager
→ AmadeusThread
→ SessionIo
→ internal Session
→ ActiveTurn
→ RunningTask / SessionTask
→ LiveThread
→ ThreadStore
→ LocalThreadStore
→ JSONL Canonical Rollout
→ SQLite Metadata Index
```

JSONL 是完整历史的唯一事实源；SQLite 只保存可重建的 Thread metadata/index。

### A-01：当前 Runtime 调用链审计

- `DONE`：定位 `cmd/amadeus`、SessionRuntime、RunRuntime、Context 和 SQLite 的真实所有权。
- `DONE`：确认取消、终态事件、持久化和 TUI 状态的重复职责。

### A-02：Codex Runtime 对照

- `DONE`：确认 ThreadManager、AmadeusThread、SessionIo、internal Session、ActiveTurn 和 SessionTask 的目标层级。
- `DONE`：确认 JSONL canonical rollout + SQLite index 的持久化方向。

### A-03：目标术语与 Contract 冻结

- `DONE`：统一 Thread、Session、Turn、Iteration、RunningTask 和 SessionTask。
- `DONE`：删除目标架构中的 Domain Run、ConversationSession、TurnRuntime 和 Task 驱动 Turn 语义。
- `DONE`：冻结 RolloutItem、TurnContext、SessionIo 边界和终态顺序；完整 Event Protocol 由 D 工作流实现。

### A-04：Canonical Rollout Protocol 与 JSONL Recorder

- `DONE`：定义 versioned `RolloutLine` envelope 与 canonical `RolloutItem` registry。
- `DONE`：实现 create/open/append batch/flush/close、单 Writer mutex 和部分写回滚。
- `DONE`：实现 sequence、timestamp、路径安全 ThreadID/TurnID 与 `turn_context` envelope 一致性校验。
- `DONE`：实现不完整尾行截断、完整无换行尾行修复、未知 payload 保留和损坏中间行拒绝。
- `DONE`：覆盖并发 append、取消、panic-safe close、durability 和 reopen 测试。

### A-05：ThreadStore 与 LocalThreadStore

- `DONE`：定义 Materialize/OpenWriter/Append/Flush/Load/List/Rename/Archive/Rebuild 边界。
- `DONE`：LocalThreadStore 组合 RolloutRecorder 与 StateDB，默认 bootstrap 位于 `internal/thread/local`。
- `DONE`：Session/ThreadManager 只依赖 LiveThread/ThreadStore，不依赖本地路径、JSONL 文件或 SQLite 类型。

### A-06：SQLite State DB 与 Metadata Sync

- `DONE`：SQLite 生产 schema 仅保留 `schema_migrations` 与 `threads`。
- `DONE`：实现 StoredThread upsert/get/list、CWD 过滤、排序、rename projection 和 soft archive。
- `DONE`：MetadataSync 严格发生在 JSONL append + fsync 后，并直接从活动 Recorder path 重放 canonical metadata。
- `DONE`：实现启动 reconciliation、全索引 rebuild、活动索引丢失自愈和累计 Token projection。

### A-07：LiveThread

- `DONE`：LiveThread 封装 lazy materialize、append、flush 和 shutdown。
- `DONE`：LocalThreadStore registry + Recorder mutex 保证进程内每 Thread 单活动 Writer。
- `DONE`：SQLite 只观察已 durable 的 JSONL；metadata 失败仅产生 warning，可由 Rollout 重建。

### A-08：ThreadManager、AmadeusThread 与 SessionIo

- `DONE`：ThreadManager 成为 start/resume/get/list/delete/rename/shutdown 的唯一 Runtime 入口，并具备 close 生命周期闸门。
- `DONE`：AmadeusThread 成为 CLI/TUI 唯一活动句柄，提供 Submit、History、Io 和 Shutdown。
- `DONE`：SessionIo 使用有界 Submission/Event/Request/Status Channel 与 Terminated 信号。
- `DONE`：`cmd` 只做依赖装配；Store 生命周期归 ThreadManager，Provider/Tool/Context 组合生命周期归 Session 持有的具体 TaskFactory，后续 typed contract 分别由 B/C/D/F 收敛。

### A-09：internal Session、State 与 Services

- `DONE`：internal Session Loop 持有 SessionState、SessionServices、InputQueue 和唯一 ActiveTurn。
- `DONE`：SessionState 保存 canonical history snapshot、PreviousTurnSettings 和 Session PermissionState；正式 ContextManager 在 B 中由 canonical history 派生。
- `DONE`：SessionServices 持有 LiveThread、Session 级 TaskFactory、Clock 与 ID factory；具体 capability 由 TaskFactory 统一拥有并由 Session 关闭。
- `DONE`：New/Resumed InitialHistory 经过严格校验并重建 SessionState，不读取 SQLite Message/Turn Row。

### A-10：ActiveTurn、RunningTask 与 SessionTask

- `DONE`：生产 Runtime 已迁移为 TurnID/TurnContext/TurnState，无 Domain Run/RunRuntime/TurnRuntime。
- `DONE`：TurnContext 是单本地环境纯数据快照，启动前校验并写入 canonical rollout。
- `DONE`：建立唯一 ActiveTurn、RunningTask 和最小 TaskHost/SessionTask Contract。
- `DONE`：RegularTask/CompactTask 统一使用 CancelCause、单次 Completion、Abort 和 cleanup；Task panic 转换为失败 Completion。

### A-11：Turn 终态与持久化顺序

- `DONE`：终态顺序固定为 Task 返回、结果 append/flush、terminal append/flush、清理 ActiveTurn、发布 SessionIo terminal event。
- `DONE`：terminal persistence 失败时不发布伪完成事件，而是报告 StreamError 并终止 Session；Interface delivery 不修改 durable 事实。
- `DONE`：SessionIo 是唯一 canonical 生命周期协议；旧 Reactor Event Hub 仅保留为 D 前的非权威 Tool/Delta/TUI 展示适配器，不参与持久化、Resume 或 Session 判定。

### A-12：Resume、迁移与恢复验收

- `DONE`：New/Resumed Thread 统一产生并校验 InitialHistory。
- `DONE`：Resume 为未完成 ToolCall 去重补 cancelled ToolResult，并为最新未终态 Turn 写入 TurnAborted。
- `DONE`：Resume 只重建 canonical state，不恢复 goroutine、RunningTask、进程句柄或 Session Permission State。
- `DONE`：旧 SQLite Session/Run/Rollout 数据幂等导出为 JSONL，成功后删除旧 canonical 表；归档状态得到保留。
- `DONE`：全仓 tests/race、JSONL 损坏恢复、索引重建、迁移重入和 Provider mock E2E 全部通过。

### A 出口

- [x] JSONL Rollout 是唯一完整历史，SQLite 可从 Rollout 重建。
- [x] ThreadManager、AmadeusThread、SessionIo 和 internal Session 所有权唯一。
- [x] ActiveTurn 使用 RunningTask + SessionTask。
- [x] Turn canonical 终态先持久化和 flush，再发布 SessionIo terminal event。
- [x] Resume 只从 InitialHistory 重建，不恢复旧运行对象。
- [x] 生产主链不存在 Domain Run、ConversationSession、TurnRuntime 或 SQLite canonical rollout_items。

### A 验证记录

- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go test ./cmd/amadeus -run '^TestCodingAgentProviderMockE2E$' -count=1 -v`
- `go test ./cmd/amadeus -run '^(TestCoreToolsProviderMockE2E|TestCoreToolsProviderMockE2EDeniedWrite)$' -count=1 -v`

## 4. B. Context + Prompt

### B 目标

```text
BaseInstructions
+ ContextManager.ForPrompt()
+ Model-visible Tool Specs
+ OutputSchema
→ Prompt
→ Provider Adapter
```

ContextManager 是模型可见历史的唯一所有者；Provider Usage 是已完成请求的权威 Token 数据。

### B-01：Prompt 与 Domain Contract

- `DONE`：定义唯一 `BaseInstructions + []ResponseItem + []ToolDefinition + OutputSchema` Prompt Contract。
- `DONE`：SessionConfiguration 持有 BaseInstructions；TurnContext 不重复保存静态 Prompt，Iterator 不保存 fallback Prompt。
- `DONE`：Provider Adapter 只通过 Domain Prompt 转换 Responses/Chat Completions 方言；删除 Request 的旧 Messages/Tools 双轨字段。

### B-02：Prompt 资产收敛

- `DONE`：agent/base、execution、handoff、compaction 与跨 Tool 指引由 `internal/prompt` 内嵌资产统一加载。
- `DONE`：Tool 参数与 Schema 说明归入 Tool Spec，跨 Tool 纪律归入 Developer Instructions/BaseInstructions。
- `DONE`：删除 NamedBundle、运行时 Sources/SHA256、RequiredVariables、Repository/Composer 和旧 PromptAssembler 主链。

### B-03：唯一 ContextManager

- `DONE`：SessionState.Context 成为唯一模型可见历史所有者，Bootstrap Agent 不再创建第二 ContextManager。
- `DONE`：实现并发安全的 Record、Replace、原子 Rebuild、ForPrompt、UpdateUsage 和 HistoryVersion，Prompt 返回深拷贝快照。
- `DONE`：从 Rollout 重建 User、Assistant、ToolCall、ToolResult、Context Update、Turn failure/abort、Token Usage 和 Replacement History。
- `DONE`：删除 Envelope、WindowRequest、RequestView、RequestViewProvider、ContextRevision、WindowManager 与分类 Budget 主链；这些名称仅作为历史清理记录保留。

### B-04：Dynamic Context Updates

- `DONE`：按固定顺序构造 Developer Instructions、AGENTS.md、Environment、Permission Mode、Skills 和 MCP Context。
- `DONE`：AGENTS.md 注入用户级、项目级和目录级实际生效内容，而非只提供路径。
- `DONE`：稳定 replace key 只在值变化时追加 canonical Context Update；Resume 从 Rollout 恢复最新值。
- `DONE`：未引入通用 ContextContributor Registry 或 WorldState Diff。

### B-05：Token Accounting 与 ModelInfo

- `DONE`：Provider Usage 成为已完成请求的权威统计，并随 canonical token_usage 恢复。
- `DONE`：Conservative TokenEstimator 只负责发送前完整 Prompt 容量预估、Tool Result Projection 和 estimated UI。
- `DONE`：ModelInfo 统一 context window、只可收紧的 auto compact limit、max output、parallel tools 和 tool output limit。
- `DONE`：删除分类百分比 Budget 和 Runner 重复 max_output_tokens；模型输出上限只由 ModelInfo/Provider Request 驱动。

### B-06：History Normalization 与 Tool Result Projection

- `DONE`：保持 ToolCall/ToolResult 原子配对并保留模型顺序。
- `DONE`：删除孤立 ToolResult，为中断、Resume 或缺失结果补 synthetic ToolResult。
- `DONE`：按 ModelInfo 对超大文件、Shell 和搜索结果执行 head/tail 模型投影。
- `DONE`：Rollout 保存完整 ToolResult，Prompt 只接收安全投影；canonical Recorder 存在时不二次 Record Tool Replay。

### B-07：CompactTask 与 Replacement History

- `DONE`：`/compact` 提交 CompactOp，由 internal Session 创建 CompactTask。
- `DONE`：Compaction 使用独立 BaseInstructions、无 Tool 的 LLM Summary，并持久化该次 Provider Usage。
- `DONE`：Replacement History 保留初始用户目标、明确的 Compaction Checkpoint；最近用户消息留在覆盖范围外。
- `DONE`：Compaction 先追加 canonical Compaction/TokenUsage，Session 再原位 Rebuild 同一个 ContextManager。
- `DONE`：删除 deterministic local summary fallback，失败时保持原投影不变并返回明确错误。

### B-08：Auto Compaction

- `DONE`：Reactor 每次采样前调用同一 BeforeSample 门，包括新 Turn 首次采样。
- `DONE`：Tool Result 后下一次采样前再次基于完整 Prompt 和输出预留检查阈值。
- `DONE`：手动和自动压缩复用同一 CompactTask 与 Replacement History 语义。

### B-09：Context 验收

- `DONE`：验证超大 Tool Result 被投影而完整 canonical 输出仍保留，Reactor 可继续采样。
- `DONE`：验证中断、Resume、原子 Rebuild 与 Compaction 后 Tool 协议合法。
- `DONE`：Compaction Prompt 明确要求保留目标、修改、失败、测试、决策和未完成事项，Replacement History 使用 checkpoint。
- `DONE`：验证 Provider Usage 与完整 Prompt estimated usage 明确区分。

### B 出口

- [x] BaseInstructions、Prompt、ContextManager、ModelInfo 和 CompactTask 所有权唯一。
- [x] Runtime 主链不存在 Envelope、RequestView、RequestViewProvider 或分类 Budget。
- [x] Provider Usage 是权威统计，TokenEstimator 只承担发送前完整 Prompt 估算。
- [x] ToolCall/ToolResult 在截断、Compaction、中断和 Resume 后始终合法。
- [x] 手动和自动 Compaction 使用同一 Task 与 Replacement History 语义。

### B 验证记录

- `go test ./internal/prompt ./internal/context ./internal/llm ./internal/llm/openai ./internal/agent/react ./internal/agent/session ./internal/app/bootstrap ./cmd/amadeus -count=1`
- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `make check`

## 5. C. Tool + Approval

### C 目标

```text
Model Tool Call
→ Tool Registry
→ Schema Validate
→ ToolExecutionService
→ Tool.CheckPermissions（实现 PermissionChecker 的 Tool）
→ SessionPermissionContext
→ Allow / Ask / Deny
→ ApprovalCoordinator（仅 Ask）
→ Tool.Call
→ ToolResult
→ Rollout / ContextManager / TUI
```

当前 C-R 阶段已完成文件 Diff、命令审批和各类 Approval 的行为验证，并已将这些能力从各 Tool 内部的分散实现收敛到统一 `ToolExecutionService + SessionPermissionContext + ApprovalCoordinator` 主链。

### C-01：Tool Contract — `DONE`

- `DONE`：当前实现统一使用 `Tool`、`ToolSpec`、`ToolCall`、`Invocation`、`Output`、`Registry` 和 `ToolExecutionService`。
- `DONE`：ToolExecutionService 负责 Schema Normalize、生命周期事件、串行/有界并行调度和 ToolResult 顺序恢复。
- `DONE`：未引入 `PreparedCall`、通用 Hook、TargetStrategy 或第二套 Dispatcher；ToolExecutionService 是唯一运行时入口。

### C-02：`read` 与文件状态 — `DONE`

- `DONE`：`read` 支持 canonical path、范围读取、输出上限和截断元数据。
- `DONE`：结果记录 canonical path、content hash、行范围和输出统计。
- `DONE`：文件状态只服务 `edit/write` stale check，不成为历史源。

### C-03：`edit` + Diff Approval — `DONE`

- `DONE`：实现 `old_string/new_string/replace_all`、唯一匹配校验和 Read-before-write。
- `DONE`：共享 `internal/tool/textdiff` 生成 Unified Diff、插入/删除统计和 FileChange。
- `DONE`：Approval 后重新读取并执行 stale check，再进行同目录临时文件原子写入。
- `DONE`：文件 Approval 通过 InteractiveRequest/ApprovalDecisionOp 进入 Session，TUI 只消费 Tool 提供的 Presentation。

### C-04：`write` — `DONE`

- `DONE`：支持新文件创建与已有文件完整覆盖，并分别展示新增/覆盖 Diff。
- `DONE`：复用 Approval、stale check、原子写入和结果校验。
- `DONE`：`write` ToolResult 自己返回准确 `FileChange`，不依赖 Workspace Diff attribution。

### C-05：Permission State 与 Approval Runtime — `DONE`（行为完成，运行时上下文收敛列入 C-R）

- `DONE`：文件修改、命令、Web/MCP 的 session 级授权统一由当前 Session 持有；文件按 canonical directory 匹配，命令按 canonical CWD + exact command 匹配，Web/MCP 按各自外部资源 key 匹配。
- `DONE`：Amadeus 明确不实现 Approval 持久化；授权不写入 Rollout、SQLite、`config.yaml` 或任何用户/项目权限文件。
- `DONE`：`allow once` 只作用于当前调用；`allow for this session` 才写入当前 Session 的内存权限上下文；`deny` 默认只拒绝当前调用。
- `DONE`：Denied roots、Read-only roots、路径 canonicalization、符号链接检查优先于文件 Session Approval。
- `DONE`：Resume/Session Close 不持久化、不重放内存中的授权；进程退出后授权自动消失。
- `DONE`：`project.PermissionProfile` 仅提供 FileSystemPolicy 所需的根目录、只读根、拒绝根和符号链接边界；它不保存 Session Approval，也不承担旧 writable-root 授权主链。

### C-06：Approval Presentation 与 TUI Contract — `DONE`

- `DONE`：定义 `ApprovalRequest`、`ApprovalPresentation`、`ApprovalOption`、`ApprovalDecision.OptionID` 并支持 CLI/TUI 结构化选择。
- `DONE`：文件、命令、Web Fetch 和 MCP Call 都由 Tool 生成 Presentation，TUI 不硬编码选项语义。
- `DONE`：交互 TUI 支持结构化 Option ID，以及方向键、Enter、Esc 的动态选项。
- `DONE`：Tab Feedback 与正式 `ApprovalDecisionOp` SessionIo 路由的最终事件协议属于 D 阶段；当前 C 已冻结并验证 Approval 数据模型、Presentation 和 CLI/TUI 交互边界，D 只负责把同一 Contract 接入 canonical Event 主链。

### C-07：`execute_command` Host Execution — `DONE`

- `DONE`：校验 command、canonical CWD、timeout、TTY、输出上限和进程取消。
- `DONE`：空命令、NUL、非法 CWD 和灾难性命令直接拒绝。
- `DONE`：Session Rule 只匹配 canonical CWD + 最小规范化后的精确命令。
- `DONE`：默认 Host Execute，返回 stdout、stderr、exit code、duration 和截断信息；不解析 Shell AST、不生成文件归因 Diff。

### C-08：Tool 命名、条件 Tool 与并发 — `DONE`

- `DONE`：默认模型主链使用 `read/edit/write/glob/grep/execute_command/update_plan`，并按能力暴露条件工具。
- `DONE`：默认 Core Registry 统一注册 `read/edit/write/glob/grep/execute_command/write_stdin/update_plan`；条件 Tool 在同一 Registry 上按能力注册。
- `DONE`：读取与网络只读 Tool 可有界并行；修改、命令、Plan、MCP Call 串行；结果按原调用顺序恢复。

### C-09：MCP、Skill 与 Web Approval — `DONE`

- `DONE`：MCP List/Resource Read 默认 Allow，MCP Call 按 `server/tool` 询问并支持 Session Rule。
- `DONE`：Skill Markdown 默认 Allow；脚本执行继续统一复用 `execute_command`。
- `DONE`：保留可配置 Provider 的 `web_search`；Web Fetch 按 hostname 询问并支持 Session Rule。
- `DONE`：Web/MCP 审批发布 Requested/Resolved 事件，第二选项包含 hostname 或 server/tool 具体范围。

### C-10：Legacy Tool/Permission Cleanup — `DONE`

- `DONE`：默认 Agent/Turn 不再注入旧 PatchProjector、RunDiff、Session writable-root Store 或 Sandbox 执行分支；当前授权统一由 Session 内存权限上下文持有。
- `DONE`：新请求不暴露 `requested_permissions`；默认 Registry 不注册旧工具，默认 `execute_command` 不再注入 Sandbox 或 CommandAuthorizer 分支。
- `DONE`：旧 `read_file/list_dir/glob_files/grep_code/request_permissions` 实现、别名、注册入口与专属测试已物理删除。
- `DONE`：`apply_patch` 与 sandbox 实现保留，但不进入默认 Core Registry、模型可见快照或正式执行主链。

### C 出口

- [x] `edit/write` 写入前展示准确 Diff，No 时零写入，stale 时拒绝覆盖。
- [x] 文件 Session Approval 使用目录级 `accept edits` 语义。
- [x] 命令 Session Rule 只复用相同 canonical CWD 与精确命令。
- [x] Web/MCP 外部 Session Rule 和 Approval 事件具备回归测试。
- [x] TUI 能完成结构化 Approval，且不直接拥有 Tool 状态。
- [x] 默认主链不存在 PreparedCall、通用 Hook、RunDiff 投影器或 Sandbox 执行分支；工具共享 Session 内存权限上下文，但授权规则仍按工具语义匹配。
- [x] 默认 Agent/Turn 不再注册旧工具、注入旧 Sandbox/CommandAuthorizer 或暴露旧权限工具。
- [ ] 由 D 阶段统一 Event/InteractiveRequest 主链替代旧 Approval Event；该项属于 D，不阻塞 C 的默认 Tool/Approval Contract。

## 6. C-R. Tool Runtime Refactor

### C-R 目标

将 C 阶段已经实现的内置 Tool 和 Approval 能力收敛为一条 Claude Code 风格、Codex Runtime 兼容的统一主链：

```text
LLM Tool Call
→ ToolRegistry.Lookup
→ ToolExecutionService
→ Normalize / Backfill Input
→ Schema Validate
→ Tool.CheckPermissions
→ SessionPermissionContext
→ Allow / Ask / Deny
→ ApprovalCoordinator（仅 Ask）
→ Tool.Call
→ ToolResult
→ Event / Rollout / Context / TUI
```

C-R 不重新设计 Agent Engine，也不增加第二套执行器或 Permission Engine。`Registry` 负责注册和查找，`ToolExecutionService` 是唯一的工具执行入口。

### C-R-01：冻结 SessionPermissionContext — `DONE`

- `DONE`：Session 只持有内存中的文件目录、精确命令和外部资源 grant。
- `DONE`：提供统一的 `Match(PermissionGrant)`、`ApplyGrant(PermissionGrant)` 和 `Clear()` 语义。
- `DONE`：`allow once` 不写入 Context，`allow for this session` 写入 Context，`deny` 不保存；Session Close/Resume 不恢复授权。
- `DONE`：删除旧的文件、远端和运行级 Approval Store 及 `ApprovalRun` 语义。

### C-R-02：统一 Tool Contract — `DONE`

- `DONE`：生产接口统一使用 `Tool`、`ToolSpec`、`Invocation`、`Output`、`Call`，由 `ToolExecutionService` 负责唯一执行入口。
- `DONE`：Schema Normalize、生命周期事件、有界并行和结果顺序恢复集中在 `ToolExecutionService`。
- `DONE`：Tool 不直接读取 TUI、不解析键盘输入、不直接更新 SessionPermissionContext；需要权限判断的 Tool 通过可选 `PermissionChecker` 接口提供检查结果。

### C-R-03：拆分 PathResolver、FileSystemPolicy 与 SessionPermissionContext — `DONE`

- `DONE`：Path/FileSystemPolicy 负责客观路径边界，SessionPermissionContext 只负责当前 Session grant。
- `DONE`：删除 `FileApprovalStore`、`SessionApprovalStore`、`SessionRuleStore` 三个并列 legacy 类型。
- `DONE`：工具不再直接更新权限，Session grant 统一由 ApprovalCoordinator 应用。

### C-R-04：ApprovalCoordinator — `DONE`

- `DONE`：ApprovalCoordinator 统一负责请求校验、UI ApprovalPort、审批事件和 Session grant 应用；ApprovalPort 仅作为 UI 端口保留。
- `DONE`：文件、命令、Web、MCP 通过共享 Coordinator 进入 TUI/CLI；文件 Diff 仍由文件 Tool 生成。
- `DONE`：工具保留自身的参数解析、客观安全检查和执行逻辑，不再直接调用 UI 或发布审批事件。

### C-R-05：迁移 `read/edit/write` — `DONE`

- `DONE`：`read` 默认无需审批；`edit/write` 执行 Diff、Approval、Stale Check、Atomic Apply 和 Verify。
- `DONE`：`edit/write` 共用目录级 Session grant，TUI 渲染 ApprovalRequest 中的 Diff。
- `DONE`：ToolResult 保留最终文件状态、Structured Diff 和错误分类。

### C-R-06：迁移 `execute_command` 与其他 Tool — `DONE`

- `DONE`：`execute_command` 保留 cwd、危险命令、TTY、timeout、输出上限和 Process Manager，并将用户审批交给 Coordinator。
- `DONE`：Web/MCP 使用相同 Coordinator，分别按 hostname、server/tool key 复用 Session grant。
- `DONE`：`write_stdin` 继续继承进程生命周期，不创建第二套审批链。

### C-R-07：统一 ToolResult、Event 与 TUI — `DONE`

- `DONE`：ToolExecutionService 统一发布 ToolStarted/ToolCompleted；审批事件由 Coordinator 发布。
- `DONE`：ToolResult 保留文本、结构化 metadata、Diff 和错误分类，TUI 不从 Tool 或 ApprovalPort 状态推断 Runtime。
- `DONE`：D 阶段只需将现有事件与 InteractiveRequest/TUI Projection 继续整合，不再改变 Tool/Approval 语义。

### C-R 出口

- [x] 所有默认模型可见 Tool 都经过同一个 ToolExecutionService。
- [x] `FileSystemPolicy` 不再保存用户授权状态。
- [x] `SessionPermissionContext` 是 Session 唯一权限投影。
- [x] 文件、命令、Web、MCP 共享 ApprovalCoordinator；Skill 脚本继续复用 execute_command。
- [x] `edit/write` 的 Diff 在 Approval 前生成、Approval 后应用、ToolResult 中回传。
- [x] `apply_patch` 和 sandbox 仍可保留实现，但不进入默认 Tool 主链。
- [x] 删除旧 Router 文件、旧 Approval Store、旧文件读取/搜索工具和旧权限兼容 API；测试与架构守卫已同步到 `ToolExecutionService` 语义。
- [x] 审批并发去重、Session grant 复用、审批等待不占执行闸门、权限拒绝不进入 `Tool.Call` 均有回归测试。
- [x] `go test ./... -count=1`、`go test -race ./... -count=1` 与 `git diff --check` 已通过。

## 7. D. Event + TUI

### D 目标

```text
Submission / Op
→ internal Session
→ SessionEvent / InteractiveRequest
→ EventReducer
→ TranscriptState
→ ActiveHistoryCell / HistoryCell
→ Rich Inline TUI
```

Event、Interactive Request、Rollout、TUI Message 和 Trace 必须是五个独立概念；TUI 不再通过 LLM Call、Iteration、通用 Status 或后台 goroutine 返回猜测 Runtime 真相。

### D-01：Protocol Package 与 SessionEvent

- `DONE`：`internal/agent/protocol` 定义 Submission、SessionEvent、InteractiveRequest 和 EventMessage 边界。
- `DONE`：SessionEvent 固定携带 ThreadID/TurnID，不使用 Envelope、动态 Metadata 或反射注入。
- `DONE`：SessionIo 使用有界 Events channel 保证单 Session 发布顺序，不引入 Event Priority、Topic DSL 或第二 Event Bus。

### D-02：TurnItem Contract

- `DONE`：定义 UserMessage、AssistantMessage、Reasoning、ToolCall、CommandExecution、FileChange、Plan 和 ContextCompaction Item。
- `DONE`：统一 in_progress/completed/failed/declined 状态与稳定 ItemID。
- `DONE`：`turn_item_completed` 保存独立 Replay 所需的完整事实，不依赖历史 Delta。

### D-03：EventMessage 与 Delta

- `DONE`：实现 ThreadConfigured、TurnStarted、TurnRejected、TurnCompleted、TurnAborted、ItemStarted、ItemCompleted。
- `DONE`：实现 AssistantMessageDelta、ReasoningDelta 和 CommandOutputDelta，并携带 ItemID。
- `DONE`：实现 PlanUpdated、ThreadTokenUsageUpdated、ContextCompacted、Warning 和 StreamError。
- `DONE`：LLMCall、Iteration、Retry 和 Provider Attempt 不进入产品 Event Protocol。

### D-04：InteractiveRequest

- `DONE`：定义 ApprovalRequest 与 UserInputRequest，包含稳定 RequestID 和完整 Presentation。
- `DONE`：通过 ApprovalDecisionOp/UserInputResponseOp 回答，不使用普通 ApprovalRequested/Resolved Event 主链。
- `DONE`：Tool/File/Command Completed Item 记录最终 completed/declined/failed 结果。

### D-05：SessionIo Delivery 与 Persistence Policy

- `DONE`：SessionIo 分离 Submissions、Events、Requests、Status 和 Terminated 通道。
- `DONE`：持久化 Turn 生命周期、Completed Item、Plan、Usage、Compaction 和恢复所需 Context Facts。
- `DONE`：ItemStarted、Delta、Working、未决 Request、Popup 和动画 Tick 不进入 canonical Rollout。
- `DONE`：Session 是唯一事件出口，不保留通用 Event Hub backpressure 主链；Rollout append 在 Session/LiveThread 边界串行化，高频事实采用缓冲追加，Turn 终态前强制 flush。

### D-06：Runtime Event 迁移

- `DONE`：模型输出、Tool、Command、FileChange、Plan、Usage、Compaction 和终态进入新协议。
- `DONE`：产品事件不暴露 RunStarted/RunStatusChanged/RunCompleted、通用 StatusChanged、IterationStarted/Completed 或 LLM Call 生命周期。
- `DONE`：终态只由 TurnCompleted/TurnAborted 表达，失败信息进入唯一终态。

### D-07：EventReducer 与 TranscriptState

- `DONE`：`protocol.TranscriptState` 作为纯状态 Reducer，将 SessionEvent 投影为 Active Item、Committed History 和 Working 状态；TUI 使用同一 Reducer 做事件校验。
- `DONE`：Delta 只更新相同 ItemID；重复 Completed、迟到 Delta 和未知 ItemID 有确定性处理并保留诊断。
- `DONE`：Bubble Tea 只负责输入、Popup、Viewport 和渲染；Runtime 业务事实来自 SessionEvent/TranscriptState。

### D-08：Live/Replay HistoryCell

- `DONE`：实时路径使用 ItemStarted → Delta → ItemCompleted → HistoryCell。
- `DONE`：Resume 路径使用 `ProjectCompletedItems`/`LegacyResponseItemsToCompleted` → 相同 HistoryCell 映射，不重放 Delta 和 Working 动画。
- `DONE`：没有 Started 的 Completed Item 可直接生成正式 Cell；Replay 与 Live 共用稳定 TurnItem。

### D-09：Approval、Diff 与 Visual Runtime

- `DONE`：TUI 渲染 ApprovalPresentation，不改写选项语义。
- `DONE`：支持方向键、Enter、Esc、Tab Feedback、Structured Diff 和等待期间 Interrupt。
- `DONE`：Working 由 TurnStarted/TurnCompleted/TurnAborted 控制；后台 Task 返回只负责 goroutine 收尾。
- `DONE`：Rich Inline TUI 使用统一事件边界并覆盖 Working、Worked for、间距、颜色和 resize。
- `DONE`：覆盖关闭 Session、迟到 Delta、重复 Completed、Resume Replay 和 Approval 响应；IPv6 listener 依赖的 HTTP 测试受环境限制。
- `DONE`：删除 `internal/agent/event` Hub/Metadata/Sink 主链和 TUI 双终态推断。

### D 出口

- [x] Event、InteractiveRequest、Rollout、TUI Message 和 Trace 职责独立。
- [x] TurnItem 是 Live、Replay 和 HistoryCell 的稳定业务模型。
- [x] Working 与终态只有一套 Runtime 真相。
- [x] TUI 不拥有 Runtime 业务状态，Event、InteractiveRequest、Rollout 与 HistoryCell 边界稳定。
- [x] Approval、Plan、Compaction、Tool 和 Turn 终态在 Runtime、Rollout 与 TUI 中一致。

## 8. E. Slash Command

### E 目标

```text
Composer
→ InputResult
→ fullscreenModel.dispatchCommand
→ TUI Local Action / Application Command / Session Op
→ Runtime
→ SessionEvent 或 Application Result
→ HistoryCell / TUI Projection
```

本阶段完全采用 Codex 的 Slash Command 核心模式：强类型 `SlashCommand`、类型化 `InputResult`、单一 TUI 分发中心和 Application/Session 操作。删除独立 `SlashCommandCatalog`、Plain Controller、`CommandHandler`、`TaskHandler`、`--plain` 和 `cmd/amadeus` 内的 Slash Command switch。

### E-01：Codex 风格命令模型

- `TODO`：将命令身份收敛为 `SlashCommand`，通过类型方法提供名称、描述、参数能力、运行期间可用性和展示顺序。
- `TODO`：删除 `SlashCommandSpec`、`slashCommandCatalog` 及其业务无关的独立 Catalog API。
- `TODO`：保留 `/resume /skills /rename /delete /compact /plan /copy /status /mcp /clear /exit`，顺序与说明集中在命令类型附近。

### E-02：Composer 与 InputResult

- `TODO`：输入框统一解析普通文本、裸命令和带参数命令，不让 Plain/Fullscreen 各自解析。
- `TODO`：Popup 只负责过滤、展示、方向键选择和 Enter/Esc，不直接访问 Session、Store 或 Runtime。
- `TODO`：命令只有分发成功后才写入本地输入回忆；参数校验失败不污染命令历史。

### E-03：单一 Slash Dispatch

- `TODO`：由 Fullscreen TUI 活动模型承担类似 Codex `ChatWidget` 的唯一 Slash 分发职责。
- `TODO`：删除 `cmd/amadeus` 的 Slash Command `switch`，cmd 只保留 CLI 参数、依赖装配和 TUI 启动。
- `TODO`：`/copy` 作为 TUI Local；`/status`、`/mcp`、`/resume`、`/skills`、`/rename`、`/delete`、`/clear`、`/exit` 调用 Application 服务；`/compact`、`/plan` 提交正式 Session/Turn 操作。

### E-04：删除 Plain 第二主链

- `TODO`：删除 `--plain`、`TerminalInteractionController`、Plain 专用 Command/Task Handler 和 Plain Slash 路由。
- `TODO`：清理 Plain 专用状态、输出、Approval 和测试，避免同一命令维护两套语义。
- `TODO`：保留必要的非交互能力时另行设计机器可读输出，不恢复第二套交互运行时。

### E-05：Slash 与 Event/Runtime 验收

- `TODO`：命令本身不进入 SessionEvent；命令引起的状态变化通过 Session Op/Application Result、SessionEvent 和 TUI Projection 传播。
- `TODO`：验证 `/plan <task>` 先提交正式设置再提交用户输入，`/compact` 走 CompactOp，`/resume` 与 `/clear` 正确刷新 Thread 和 Transcript。
- `TODO`：验证 Plain/Fullscreen 路径删除后，Popup、命令历史、错误和运行期间可用性只有一个事实源。

### E 出口

- [ ] Slash Command 使用 Codex 风格强类型命令与 InputResult。
- [ ] 只有一个 TUI Slash Dispatch，不存在 cmd 或 Plain 的第二套 switch。
- [ ] 命令业务通过 Application Command 或 Session Op 执行，TUI 不直接管理 Session 状态。
- [ ] `--plain`、Plain Controller 和旧 Catalog 主链已删除。

## 9. F. Plan-guided ReAct

### F-01：唯一 RegularTask/Reactor

- `TODO`：RegularTask 驱动唯一 Reactor。
- `TODO`：每个 Iteration 执行模型采样、Tool 调用、结果回灌和完成判断。
- `TODO`：删除双 Engine、DAG、Planner、Replanner 和 Scheduler 遗留。

### F-02：模型输出与 TurnItem

- `TODO`：Assistant、Reasoning 和 Tool Call 流统一产生 D 工作流定义的 Item 生命周期与 Delta。
- `TODO`：模型最终回答完成 AssistantMessageItem 后再结束 Turn。
- `TODO`：Provider/Context/Tool 错误进入 StreamError、Completed Item 或唯一 Turn 终态，不静默停止。

### F-03：`update_plan`

- `DONE`：`update_plan` 通过 Session capability 更新 `SessionState.Plan`，不持有 Tool 私有状态且不驱动 DAG。
- `DONE`：Session 按 canonical Rollout persist-then-commit，Resume 从最近 `plan_update` 恢复 revision。
- `DONE`：简单输入不强制创建 Plan；PlanUpdated 已进入统一 SessionEvent/TurnItem/TUI 主链。

### F-04：Plan Mode

- `TODO`：`/plan` 将 Session Permission Mode 切换为 `plan`，由 RegularTask 进入只规划行为。
- `TODO`：Plan Mode 复用同一 Reactor，禁止副作用 Tool。
- `TODO`：退出 Plan Mode 时恢复进入前 Permission Mode。

### F-05：终态与中断

- `TODO`：完成、失败、停滞、取消和最终回答使用 D 工作流定义的唯一终态。
- `TODO`：中断后的新 Turn 看到 TurnAborted 事实并重新规划，不恢复旧执行栈。

### F 出口

- [ ] 默认是单一 Plan-guided ReAct。
- [ ] `update_plan` 是软状态，`/plan` 是显式只规划模式。
- [ ] 简单对话不创建无关计划或 Tool 调用。
- [ ] Reactor 只发布稳定 SessionEvent，不暴露内部 Iteration/LLM Call 作为 UI Contract。

## 10. G. Extensions + Release

### G-01：MCP

- `TODO`：统一 Tool Registry、lazy discovery、catalog snapshot、Approval 和 ToolResult。

### G-02：Skill

- `TODO`：用户级与项目级 Skill discovery。
- `TODO`：Skill 内容按需注入，Script 统一走 `execute_command`。

### G-03：Web

- `TODO`：稳定 OpenAI、Tavily、Brave 等 Search Provider。
- `TODO`：超时、限流、来源和结果长度进入统一 ToolResult。

### G-04：Legacy Persistence Cleanup

- `TODO`：删除旧 projects/sessions/turns/messages/summaries/runs/rollout_items 主链。
- `TODO`：验证旧数据迁移、Rollout backfill、损坏恢复和 index rebuild。

### G-05：Release Validation

- `TODO`：Provider fixtures、全仓 test/race/build 和真实模型 smoke。
- `TODO`：同步 README、示例配置、架构文档和首个正式发布检查。

## 11. 当前保留能力

以下行为有价值，应迁入新主链，但现有包边界不构成兼容约束：

- OpenAI Responses 与 Chat Completions Adapter。
- DeepSeek、GLM、Qwen Dialect 基础。
- 流式文本、reasoning 和 Tool Call 聚合。
- 现有 RolloutItem payload 与 PlanUpdate 语义。
- Reactor 与 `update_plan` 基础。
- AGENTS.md 指令发现。
- MCP、Skill 和 Web capability 边界。
- Bubble Tea Inline TUI、HistoryCell 和 Slash Popup。
- `execute_command` 的超时、取消、输出限制和危险命令保护。
- Rich Inline TUI、HistoryCell 和 Slash Command Popup。

## 12. 当前下一步

当前关键路径：

```text
C-R Tool Runtime Refactor（DONE）
→ D Event Protocol 与 TUI Projection（DONE）
→ E Codex 风格 Slash Command 重构
→ F Plan-guided ReAct 端到端验收
```

下一项开发任务：**E-01：Plan-guided ReAct 端到端验收。**

## 13. 更新模板

```text
日期：YYYY-MM-DD
任务：A-?? / B-?? / C-?? / D-?? / E-?? / F-?? / G-??
状态：TODO / DOING / BLOCKED / DONE / SKIPPED / SUPERSEDED
改动：
- ...
验证：
- ...
遗留：
- ...
下一步：
- ...
```
