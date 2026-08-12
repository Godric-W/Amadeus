# Amadeus 开发进度

> 最近更新：2026-08-12
> 唯一目标架构：`docs/design.md`
> 当前阶段：C. Tool + Approval
> 下一任务：C-01 Tool Contract 与旧调用链审计

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

开发计划分为四个已经完成设计讨论的核心工作流，以及两个后续整合工作流：

| 工作流 | 范围 | 设计状态 | 实施状态 |
|---|---|---|---|
| A. Runtime + Persistence | Thread、Session、Turn、Task、JSONL Rollout、SQLite Index、Resume | 已冻结 | `DONE` |
| B. Context + Prompt | BaseInstructions、ContextManager、Token、Projection、Compaction | 已冻结 | `DONE` |
| C. Tool + Approval | Tool Contract、Edit/Write、Permission、Approval、Command | 已冻结 | 等待 A 核心边界 |
| D. Event + TUI + Slash Command | SessionEvent、InteractiveRequest、TurnItem、HistoryCell、Slash Routing | 已冻结 | 等待 A/C 核心边界 |
| E. Agent Engine | Plan-guided ReAct、`update_plan`、Plan Mode、中断与终态 | 待 A/B/C/D 接入 | 未开始 |
| F. Extensions + Release | MCP、Skill、Web、兼容迁移、发布验证 | 待主链稳定 | 未开始 |

推荐实施顺序：

```text
A Runtime Contract + Canonical Persistence
→ B Context/Prompt 与 C Tool/Approval 可并行推进
→ D Event Protocol、TUI Projection 与 Slash Routing
→ E Plan-guided ReAct 整合
→ F Extensions/Release
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
- `DONE`：删除 Envelope、WindowRequest、RequestView、RequestViewProvider、ContextRevision、WindowManager 与分类 Budget 主链。

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
→ Tool.ValidateInput
→ Tool.CheckPermission
→ Allow / Ask / Deny
→ Approval Runtime（Ask）
→ Tool.Execute
→ ToolResult
→ Rollout / ContextManager / TUI
```

文件 Session Approval 对齐 Claude Code 的 `accept_edits + Working Directories`；命令使用独立的 Session Command Rule。

### C-01：Tool Contract — `DONE`

- `DONE`：统一使用 `Spec`、`ToolCall`、`Invocation`、`Output`、`Handler`、`Registry` 和 `Router`。
- `DONE`：Router 负责 Schema Normalize、生命周期事件、串行/有界并行调度和 ToolResult 顺序恢复。
- `DONE`：未引入 `PreparedCall`、通用 Hook、TargetStrategy 或第二套 Dispatcher；旧 `Router` 是当前唯一运行时入口。

### C-02：`read` 与文件状态 — `DONE`

- `DONE`：`read` 支持 canonical path、范围读取、输出上限和截断元数据。
- `DONE`：结果记录 canonical path、content hash、行范围和输出统计。
- `DONE`：文件状态只服务 `edit/write` stale check，不成为历史源。

### C-03：`edit` + Diff Approval — `DONE`

- `DONE`：实现 `old_string/new_string/replace_all`、唯一匹配校验和 Read-before-write。
- `DONE`：共享 `internal/tool/textdiff` 生成 Unified Diff、插入/删除统计和 FileChange。
- `DONE`：Approval 后重新读取并执行 stale check，再进行同目录临时文件原子写入。
- `DONE`：文件 Approval 发布 `ApprovalRequested/ApprovalResolved`，TUI/Plain 只消费 Tool 提供的 Presentation。

### C-04：`write` — `DONE`

- `DONE`：支持新文件创建与已有文件完整覆盖，并分别展示新增/覆盖 Diff。
- `DONE`：复用 Approval、stale check、原子写入和结果校验。
- `DONE`：`write` ToolResult 自己返回准确 `FileChange`，不依赖 Workspace Diff attribution。

### C-05：Permission State 与 Approval Runtime — `PARTIAL`

- `DONE`：文件修改使用 Session 级目录授权；命令使用 canonical CWD + 精确命令的 `SessionApprovalStore`；Web/MCP 使用独立 `SessionRuleStore`。
- `DONE`：Denied roots、Read-only roots、路径 canonicalization、符号链接检查优先于文件 Session Approval。
- `DONE`：Resume/Session Close 不持久化、不重放内存中的授权。
- `PARTIAL`：旧 `project.PermissionProfile` 仍作为路径策略兼容实现存在；它已不再承载默认 Session writable-root 授权主链，后续单独收敛为更小的 FileSystemPolicy。

### C-06：Approval Presentation 与 TUI Contract — `PARTIAL`

- `DONE`：定义 `ApprovalRequest`、`ApprovalPresentation`、`ApprovalOption`、`ApprovalDecision.OptionID` 并支持 CLI/TUI 结构化选择。
- `DONE`：文件、命令、Web Fetch 和 MCP Call 都由 Tool 生成 Presentation，TUI 不硬编码选项语义。
- `DONE`：CLI/Plain 支持 `y/s/n` 与结构化 Option ID；全屏 TUI 支持方向键、Enter、Esc 的动态选项。
- `PARTIAL`：Tab Feedback 与正式 `ApprovalDecisionOp` SessionIo 路由仍属于 D 阶段 Event 重构，不在当前旧 Event Hub 主链中伪装完成。

### C-07：`execute_command` Host Execution — `DONE`

- `DONE`：校验 command、canonical CWD、timeout、TTY、输出上限和进程取消。
- `DONE`：空命令、NUL、非法 CWD 和灾难性命令直接拒绝。
- `DONE`：Session Rule 只匹配 canonical CWD + 最小规范化后的精确命令。
- `DONE`：默认 Host Execute，返回 stdout、stderr、exit code、duration 和截断信息；不解析 Shell AST、不生成文件归因 Diff。

### C-08：Tool 命名、条件 Tool 与并发 — `DONE`

- `DONE`：默认模型主链使用 `read/edit/write/glob/grep/execute_command/update_plan`，并按能力暴露条件工具。
- `DONE`：旧 `read_file/list_dir/glob_files/grep_code/apply_patch/request_permissions` 只作为隐藏兼容 Handler，不进入模型可见快照。
- `DONE`：读取与网络只读 Tool 可有界并行；修改、命令、Plan、MCP Call 串行；结果按原调用顺序恢复。

### C-09：MCP、Skill 与 Web Approval — `DONE`

- `DONE`：MCP List/Resource Read 默认 Allow，MCP Call 按 `server/tool` 询问并支持 Session Rule。
- `DONE`：Skill Markdown 默认 Allow；脚本执行继续统一复用 `execute_command`。
- `DONE`：保留可配置 Provider 的 `web_search`；Web Fetch 按 hostname 询问并支持 Session Rule。
- `DONE`：Web/MCP 审批发布 Requested/Resolved 事件，第二选项包含 hostname 或 server/tool 具体范围。

### C-10：Legacy Tool/Permission Cleanup — `PARTIAL`

- `DONE`：默认 Agent/Turn 不再注入旧 PatchProjector、RunDiff、Session writable-root Store、Sandbox 或 CommandAuthorizer。
- `DONE`：新请求不暴露 `requested_permissions`，旧工具被 Registry 标记为 Hidden，不进入模型工具快照。
- `PARTIAL`：`internal/project.PermissionProfile`、`internal/policy/CommandAuthorizer`、`internal/sandbox`、`internal/diff` 及旧 Handler 文件仍保留给历史测试/Replay 兼容；不得作为新主链继续扩展。
- `TODO`：后续将历史调用迁移完毕后删除这些兼容包，并清除遗留 RunDiff Event/渲染分支。

### C 出口

- [x] `edit/write` 写入前展示准确 Diff，No 时零写入，stale 时拒绝覆盖。
- [x] 文件 Session Approval 使用目录级 `accept edits` 语义。
- [x] 命令 Session Rule 只复用相同 canonical CWD 与精确命令。
- [x] Web/MCP 外部 Session Rule 和 Approval 事件具备回归测试。
- [x] TUI 与 `--plain` 都能完成结构化 Approval，且不直接拥有 Tool 状态。
- [x] 默认主链不存在 PreparedCall、通用 Hook、旧 PermissionStore 注入、RunDiff 投影器或 Sandbox 执行分支。
- [ ] 历史兼容包完全删除，并由 D 阶段统一 Event/InteractiveRequest 主链替代旧 Approval Event。

## 6. D. Event + TUI + Slash Command

### D 目标

```text
Submission / Op
→ internal Session
→ SessionEvent / InteractiveRequest
→ EventReducer
→ TranscriptState
→ ActiveHistoryCell / HistoryCell
→ Rich Inline TUI / --plain
```

Event、Interactive Request、Rollout、TUI Message 和 Trace 必须是五个独立概念；TUI 不再通过 LLM Call、Iteration、通用 Status 或后台 goroutine 返回猜测 Runtime 真相。

### D-01：Protocol Package 与 Envelope

- `TODO`：建立 `internal/agent/protocol`，定义 Submission、SessionEvent、InteractiveRequest 和 EventMessage 边界。
- `TODO`：SessionEvent Envelope 统一携带 ThreadID/TurnID，删除每个 Event 重复 Metadata 和反射注入。
- `TODO`：第一版只依赖 SessionIo 单通道顺序，不增加 Event Priority、Topic DSL 或额外 Sequence。

### D-02：TurnItem Contract

- `TODO`：定义 UserMessage、AssistantMessage、Reasoning、ToolCall、CommandExecution、FileChange、Plan 和 ContextCompaction Item。
- `TODO`：统一 in_progress/completed/failed/declined 状态与稳定 ItemID。
- `TODO`：Completed Item 包含独立 Replay 所需的完整事实，不依赖历史 Delta。

### D-03：EventMessage 与 Delta

- `TODO`：实现 ThreadConfigured、TurnStarted、TurnCompleted、TurnAborted、ItemStarted、ItemCompleted。
- `TODO`：实现 AssistantMessageDelta、ReasoningDelta 和 CommandOutputDelta，并强制携带 ItemID。
- `TODO`：实现 PlanUpdated、ThreadTokenUsageUpdated、ContextCompacted、Warning 和 StreamError。
- `TODO`：LLMCall、Iteration、Retry 和 Provider Attempt 只进入 Trace/Telemetry，不进入产品 Event Protocol。

### D-04：InteractiveRequest

- `TODO`：定义 ApprovalRequest 与 UserInputRequest，包含稳定 RequestID 和完整 Presentation。
- `TODO`：通过 ApprovalDecisionOp/UserInputResponseOp 回答，删除普通 ApprovalRequested/Resolved Event 主链。
- `TODO`：Tool/File/Command Completed Item 记录最终 completed/declined/failed 结果。

### D-05：SessionIo Delivery 与 Persistence Policy

- `TODO`：SessionIo 分离 Submissions、Events、Requests、Status 和 Terminated 通道。
- `TODO`：只持久化 Turn 生命周期、Completed Item、Plan、Usage、Compaction 和恢复所需 Context Facts。
- `TODO`：ItemStarted、Delta、Working、未决 Request、Popup 和动画 Tick 不进入 canonical Rollout。
- `TODO`：慢 Renderer、关闭 TUI 或调试观察者不能让 Agent Turn 失败；删除通用 Event Hub backpressure 主链。

### D-06：Runtime Event 迁移

- `TODO`：将模型输出、Tool、Command、FileChange、Plan、Usage、Compaction 和终态映射到新协议。
- `TODO`：删除 RunStarted/RunStatusChanged/RunCompleted、StatusChanged、IterationStarted/Completed 和 UI 可见 LLMCallStarted/Completed。
- `TODO`：终态只由 TurnCompleted/TurnAborted 表达，失败信息进入唯一终态。

### D-07：EventReducer 与 TranscriptState

- `TODO`：建立纯状态 Reducer，将 SessionEvent 投影为 Active Item、Committed History 和 Working 状态。
- `TODO`：Delta 只更新相同 ItemID；重复 Completed 和迟到 Delta 幂等处理并记录诊断。
- `TODO`：Bubble Tea Model 只负责输入、Popup、Viewport 和渲染，不再拥有 Turn 业务状态。

### D-08：Live/Replay HistoryCell

- `TODO`：实时路径使用 ItemStarted → Delta → ItemCompleted → HistoryCell。
- `TODO`：Resume 路径使用 Completed TurnItem → 相同 HistoryCell 映射，不重放 Delta 和 Working 动画。
- `TODO`：没有 Started 的 Completed Item 仍可直接生成正式 Cell；ActiveHistoryCell 每个 ItemID 只提交一次。

### D-09：Slash Command Routing

- `TODO`：统一 Catalog、描述、过滤、Popup 和参数校验，Catalog 不包含业务执行逻辑。
- `TODO`：`/copy /status` 路由为 TUI Local；`/resume /skills /rename /delete /mcp /clear /exit` 路由为 Application Action。
- `TODO`：`/compact` 提交 CompactOp；`/plan` 修改正式 Turn Setting，`/plan <task>` 设置模式后提交用户输入。
- `TODO`：删除 Slash Command 对 Store、ContextManager、ActiveTurn 或 Tool 状态的直接修改。

### D-10：Approval、Diff 与 Visual Runtime

- `TODO`：TUI 渲染 C 工作流定义的 ApprovalPresentation，不改写选项语义。
- `TODO`：支持方向键、Enter、Esc、Tab Feedback、Structured Diff 和等待期间 Interrupt。
- `TODO`：Working 只由 TurnStarted/TurnCompleted/TurnAborted 控制；后台 Task 返回只负责 goroutine 收尾。
- `TODO`：对齐 Working、Worked for、间距、颜色、状态栏、中文输入、原生 scrollback 和 resize。

### D-11：Plain Renderer 与协议验收

- `TODO`：Rich Inline TUI 和 `--plain` 复用相同 SessionEvent/InteractiveRequest Contract。
- `TODO`：覆盖慢 Renderer、关闭 TUI、迟到 Delta、重复 Completed、Resume Replay 和 Approval 响应测试。
- `TODO`：删除旧 `internal/agent/event` Hub/Metadata/Sink 主链和 TUI 双终态推断。

### D 出口

- [ ] Event、InteractiveRequest、Rollout、TUI Message 和 Trace 职责独立。
- [ ] TurnItem 是 Live、Replay 和 HistoryCell 的稳定业务模型。
- [ ] Working 与终态只有一套 Runtime 真相。
- [ ] Slash Command 不拥有业务状态。
- [ ] Approval、Plan、Compaction、Tool 和 Turn 终态在 Runtime、Rollout、TUI 与 `--plain` 中一致。

## 7. E. Plan-guided ReAct

### E-01：唯一 RegularTask/Reactor

- `TODO`：RegularTask 驱动唯一 Reactor。
- `TODO`：每个 Iteration 执行模型采样、Tool 调用、结果回灌和完成判断。
- `TODO`：删除双 Engine、DAG、Planner、Replanner 和 Scheduler 遗留。

### E-02：模型输出与 TurnItem

- `TODO`：Assistant、Reasoning 和 Tool Call 流统一产生 D 工作流定义的 Item 生命周期与 Delta。
- `TODO`：模型最终回答完成 AssistantMessageItem 后再结束 Turn。
- `TODO`：Provider/Context/Tool 错误进入 StreamError、Completed Item 或唯一 Turn 终态，不静默停止。

### E-03：`update_plan`

- `TODO`：`update_plan` 只更新 TurnState.Plan，不驱动 DAG。
- `TODO`：简单输入不强制创建 Plan。
- `TODO`：PlanUpdated 进入 canonical Rollout、SessionEvent 和 TUI。

### E-04：Plan Mode

- `TODO`：`/plan` 将 Session Permission Mode 切换为 `plan`，由 RegularTask 进入只规划行为。
- `TODO`：Plan Mode 复用同一 Reactor，禁止副作用 Tool。
- `TODO`：退出 Plan Mode 时恢复进入前 Permission Mode。

### E-05：终态与中断

- `TODO`：完成、失败、停滞、取消和最终回答使用 D 工作流定义的唯一终态。
- `TODO`：中断后的新 Turn 看到 TurnAborted 事实并重新规划，不恢复旧执行栈。

### E 出口

- [ ] 默认是单一 Plan-guided ReAct。
- [ ] `update_plan` 是软状态，`/plan` 是显式只规划模式。
- [ ] 简单对话不创建无关计划或 Tool 调用。
- [ ] Reactor 只发布稳定 SessionEvent，不暴露内部 Iteration/LLM Call 作为 UI Contract。

## 8. F. Extensions + Release

### F-01：MCP

- `TODO`：统一 Tool Registry、lazy discovery、catalog snapshot、Approval 和 ToolResult。

### F-02：Skill

- `TODO`：用户级与项目级 Skill discovery。
- `TODO`：Skill 内容按需注入，Script 统一走 `execute_command`。

### F-03：Web

- `TODO`：稳定 OpenAI、Tavily、Brave 等 Search Provider。
- `TODO`：超时、限流、来源和结果长度进入统一 ToolResult。

### F-04：Legacy Persistence Cleanup

- `TODO`：删除旧 projects/sessions/turns/messages/summaries/runs/rollout_items 主链。
- `TODO`：验证旧数据迁移、Rollout backfill、损坏恢复和 index rebuild。

### F-05：Release Validation

- `TODO`：Provider fixtures、全仓 test/race/build 和真实模型 smoke。
- `TODO`：同步 README、示例配置、架构文档和首个正式发布检查。

## 9. 当前保留能力

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
- `--plain` 非交互输出。

## 10. 当前下一步

当前关键路径：

```text
B-01 Context/Prompt 现状审计
→ B-02 BaseInstructions 与 Prompt Input
→ B-03 ContextManager canonical projection
→ B-04 Token Accounting
→ B-05 自动压缩与 /compact
→ B-09 Context 验收
```

下一项开发任务：**B-01：审计当前 ContextBuilder、Prompt 构建和 Rollout projection，删除旧的双重历史与按 Turn 临时组装路径。**

## 11. 更新模板

```text
日期：YYYY-MM-DD
任务：A-?? / B-?? / C-?? / D-?? / E-?? / F-??
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
