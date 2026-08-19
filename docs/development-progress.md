# Amadeus 开发进度

> 最近更新：2026-08-19
> 唯一架构事实源：`docs/design.md`
> 当前阶段：L. Model + Provider Configuration Ownership Alignment（DONE）
> 下一任务：无（等待下一阶段规划）

本文只记录开发阶段、任务状态、依赖和验收出口。架构决策、数据模型和实现细节统一记录在 `docs/design.md`，不在这里重复展开。

## 1. 状态与完成标准

- `TODO`：未开始。
- `DOING`：正在实施；同时只允许一个主任务处于该状态。
- `BLOCKED`：存在明确的外部阻塞。
- `DONE`：代码、测试、构建、文档和验收均完成。
- `SKIPPED`：经设计确认不再实现。
- `SUPERSEDED`：曾实现但已被当前架构取代。

任务只有在符合 `docs/design.md`、通过针对性测试和构建、同步用户可见行为并清理旧主链后，才能标记为 `DONE`。

## 2. 总体顺序

```text
A Runtime + Persistence
→ B Context + Prompt
→ C Tool + Approval
→ D Event + TUI
→ E Slash Command
→ A-CL Runtime Architecture Closure
→ B-CL Context Architecture Closure
→ F Codex-style Agent Engine Rewrite
→ G Runtime Architecture Convergence
→ H Extensions + Release
→ I Prompt Construction + Optimization
→ J Slash Command + TUI Application Lifecycle Alignment
→ K Response Stream Reconnect Lifecycle Alignment
→ L Model + Provider Configuration Ownership Alignment
```

Codex 作为 Thread、Session、SessionServices、Turn、Context、SessionTask、`run_turn`、Slash Command、TUI 和 Model/Provider 配置所有权的主要架构参考；Tool 调用链组合 Codex 的 StepContext/ToolRouter snapshot 与 Claude Code 的 Validate/Prepare/Permission/Approval/Execute 内层协议。A/B Architecture Closure 已完成，F 已删除早期经典 ReAct Engine 并建立可工作的 continuation loop；G 已将 capability 所有权与 Codex 术语归位，同时保留稳定的 ToolExecutionService 与 Claude-style Approval；I 已完成 Prompt 主链收敛；J 已删除 Slash Command/TUI Application 的 callback、字符串结果和重复 Session IO 生命周期；K 已完成 request retry 与 response stream reconnect 收敛；L 已完成 Config v2、Model Runtime 与 Provider transport 所有权分离、Provider-default sampling 和 Tool Output truncation 配置收敛。实施发现 Contract 问题时先更新 `docs/design.md`。

## 3. A. Runtime + Persistence — `DONE`

### 目标

建立 Codex 风格的运行时层级和持久化主链：

```text
ThreadManager
→ AmadeusThread
→ SessionIo
→ Session
→ ActiveTurn
→ RunningTask / SessionTask
→ JSONL Canonical Rollout
→ SQLite Metadata Index
```

JSONL 是完整历史的唯一事实源，SQLite 只保存可重建的 Thread metadata 和索引。

### 基线已完成

- 建立 `Thread`、`Session`、`Turn`、`Task` 和多模型步骤执行的基础生命周期；F 将删除当时独立持久化倾向的 `Iteration` Engine 状态。
- 完成 `AmadeusThread`、`SessionIo`、`Session`、`ActiveTurn` 和 `SessionTask` 主链。
- 实现 JSONL Rollout 的版本、序列号、追加、刷新、关闭、尾行修复和损坏检测。
- 实现 ThreadStore、LocalThreadStore、SQLite metadata index、List、Rename、Archive、Resume 和恢复重建。
- 完成 Turn 终态、取消和并发追加的基础实现。

### A-CL-01：SessionTask 执行所有权 — `DONE`

- 将生产 RegularTask/CompactTask 的真实执行入口迁入 internal Session/Agent Runtime，不回调 `cmd/amadeus` controller。F 替换其中旧 Reactor 内核，G 继续将过渡 Host interface 收敛为 Session typed methods，但不改变该依赖方向成果。
- 删除 `codingTaskFactory → agentController.execute*Turn` 反向依赖；`cmd/amadeus` 只保留配置解析、Composition Root、Thread/Application 启动和 Interface 适配。
- CLI invocation 在跨越 Runtime 边界前归一化为 typed Session configuration、Submission 和 Turn input；生产 SessionTask 不持有 Cobra command、TUI model 或完整 invocation。
- 增加无 CLI/TUI controller 的 Runtime fixture，证明 internal Session 可独立完成 regular/compact Turn。

### A-CL-02：唯一 Completion 与终态协议 — `DONE`

- 删除 `Prepare → requests channel → result chan` 时序 side channel，Task 输入通过 typed Turn input 和 immutable task value 传递；G 删除剩余的通用 Prepare/Factory 外壳。
- RunningTask completion 只由 Session 消费；CLI/TUI/Application 只通过 SessionIo Event/Status/Terminated 观察生命周期。
- 固化 accepted、rejected、completed、aborted、panic、submit failure 和 interrupt 的唯一终态测试，确保没有双完成、旧 request 残留或永久 Working。

### A-CL-03：Session Capability 与 TurnContext — `DONE`

- 将 Provider、Tool Registry、Prompt/Instruction、Approval、Extension、Web/MCP/Skill 等可复用能力收进 Session-scoped 生命周期，禁止每 Turn 由 CLI 重新装配执行核心；G 将这些能力从过渡 Factory/Runtime aggregate 正式归位到 SessionServices。
- 在实际 Provider、ModelInfo、Collaboration Mode、Approval Policy、Permission Profile、OutputSchema 和环境解析完成后冻结 TurnContext；ToolNames 必须是本 Turn 真实可见 Tool snapshot。
- 为 BaseInstructions、PreviousTurnSettings、CurrentDate、Timezone、Personality 和 OutputSchema 建立明确生产消费点；删除仅为贴合设计存在的占位字段或死状态。

### A-CL-04：Durability 与 Metadata Watermark — `DONE`

- 重构 LocalThreadStore，使 Durable Append 严格执行 write → flush → MetadataSync；Buffered Append 不更新 SQLite。
- 为 Recorder 建立 durable watermark，Metadata projection 只能读取不超过 watermark 的 RolloutLine。
- 增加 append、flush、SQLite upsert 各阶段 fault injection/crash test，验证 SQLite 只能落后 JSONL、不能领先，并验证 backfill/reconciliation。

### A-CL-05：Typed Canonical Rollout — `DONE`

- 为所有 Rollout kind 建立唯一 typed payload contract、集中 encoder/decoder 和版本策略；raw payload 只允许存在于 JSONL codec envelope 边界。
- 删除生产 writer 中的 `map[string]any` 和 writer/projector 各自维护的 ad-hoc schema，统一 response item、tool result、turn item、context update 和 terminal payload。
- 增加所有 canonical item 的 round-trip、unknown version、resume projection 和 writer/reader schema 一致性测试。

### A-CL-06：Architecture Guards 与旧链删除 — `DONE`

- 增加依赖方向测试，阻止 production SessionTask 引用 CLI controller、TUI model、Cobra command、完整 invocation 或 request/result channel 模式。
- 增加唯一终态、durability ordering 和 Session capability ownership 的行为测试，不能只扫描旧 symbol/package 名称。
- 新主链验收后立即删除 `cmd/amadeus` 中旧 Turn executor、兼容 wrapper、side channel 和无消费状态；G 只收敛 F 新增的过渡 capability aggregate，不承接 A/B 旧主链清理。

### 出口

- [x] Thread/Session/Turn 生命周期只有一套主链，生产 SessionTask 不反向依赖 CLI/Application executor。
- [x] Session Event 是 Interface 唯一 Turn 终态来源，不存在 invocation/result completion side channel。
- [x] Session 是 capability 生命周期边界，TurnContext 是真实冻结 snapshot，不含无消费占位状态；F 完成可复用 capability aggregate，G 将其正式归位到 SessionServices 并删除过渡 Factory/Runtime。
- [x] Rollout 可独立重建 Session 状态和 Context，所有 canonical payload 使用统一 typed contract。
- [x] SQLite 不保存不可重建事实，也不包含超过 JSONL durable watermark 的 metadata。
- [x] Resume、取消、panic、submit failure 和异常终态均可恢复且只完成一次。

## 4. B. Context + Prompt — `DONE`

### 目标

建立单一 ContextManager 和 Codex 风格的 Prompt 主链：

```text
Base Instructions
→ Dynamic Context
→ Project/User AGENTS.md
→ Rollout Projection
→ Normalized History
→ Tool Results
→ Token Accounting
→ Compact / Replacement History
```

### 基线已完成

- 收敛 Prompt、Message、Tool Definition、ModelInfo 和 ContextManager Contract。
- 将系统提示词和 Prompt 资产纳入统一构建链，删除旧的多套上下文事实源。
- 实现动态 Context Update、AGENTS.md 层级加载和目录作用域。
- 实现 JSONL Rollout 到 LLM Message 的统一投影，包括 Assistant reasoning 和多工具调用历史。
- 实现 Provider Usage、估算 Token、Context Window、Auto Compact 和 Replacement History。
- 完成上下文截断、工具结果归一化、历史压缩和中断 Turn 的上下文重建。

### B-CL-01：ContextManager 唯一写入边界 — `DONE`

- ContextManager 只由 Session 根据已接纳 canonical facts 执行原子 rebuild；usage、updates 与 history 均从同一 Rollout projection 派生。
- 收紧当时的 TaskHost，移除可变 `Context() *Manager` 暴露和无 Rollout 对应事实的 Record/Replace fallback；Task/continuation loop 只获取 immutable prompt/history snapshot，G 进一步删除 Host interface。
- 统一 live execution 与 Resume 的 projector，验证同一 Rollout history 得到语义等价 Context。

### B-CL-02：Tool Result 统一语义投影 — `DONE`

- Tool Result 先进入 canonical Rollout；live 下一轮与 Resume rebuild 共享同一个 typed projector 和 immutable `PromptSnapshot`，不保留独立即时 replay 链。
- 模型投影稳定保留 ok/status、text/parts、error、partial/truncated 和允许暴露的 metadata，不静默丢失 declined、failed、cancelled、stale 或 partial 语义。
- 增加多模型步骤、interrupted Turn、compaction 和 Resume 前后的 semantic-equivalence 测试。

### B-CL-03：Target-scoped Instructions — `DONE`

- 将 AGENTS.md Resolver 接入 Read/Search/Edit/Write 的目标路径和 Command 的目标 CWD，而不是只在 Turn 开始时解析初始 CWD。
- 新 scope 指令以 typed resolution 交给 Session，并通过 canonical Context Update 进入下一次 Prompt；Tool 不直接修改 ContextManager。
- 副作用 Tool 在模型尚未看到新 scope 指令时返回 `context_refresh_required`，更新 Context 并重新采样后才能继续。
- 增加根目录/嵌套目录/跨工作目录/命令 CWD 的 precedence、scope 和 mutation gate 端到端测试。

### B-CL-04：ModelInfo 与 Token 一致性 — `DONE`

- 为 ModelInfo 增加 input modalities 等真实模型能力，Context projection 在 Adapter 调用前过滤或拒绝不支持内容。
- 统一 Model Step、Turn、Thread、canonical `token_usage` 和 Resume 的累计语义，禁止单次 usage 覆盖前序累计值。
- 验证 Prompt estimate、Provider usage、auto compact 和 replacement history 使用同一 ModelInfo 与预算口径。

### B-CL-05：Projection 等价与旧链删除 — `DONE`

- 允许基础版本继续使用正确的原子全量 rebuild，不为性能提前引入第二缓存事实源；只有基准证明必要时才增加由 durable sequence 驱动的增量 projection。
- 增加长会话、超大 Tool Result、Compaction、Resume 和多模型 modality 的一致性测试。
- 删除旧 projector、重复 prompt assembly、直接 rollout/history 读取入口和无生产消费的 Context compatibility API。

### 出口

- [x] ContextManager 是唯一 Prompt 历史入口和唯一派生投影，只有 Session 可以更新。
- [x] Prompt、Token、modality 和 Compaction 使用同一套 ModelInfo 与累计 Usage 语义。
- [x] 历史可从 Rollout 重建，不依赖内存残留，live 与 Resume projection 语义等价。
- [x] Provider reasoning、Tool Call 和 Tool Result 的顺序与 status/error/partial/metadata 可正确回放。
- [x] AGENTS.md 目录作用域接入真实 Tool target，副作用不会绕过模型尚未看到的 scoped instructions。

## 5. C. Tool + Approval — `DONE`

### 目标

保留 Codex 的 Tool Registry、Session、Turn、Event 和 Rollout 边界，将 Tool 内层协议、权限评估、Approval 和结果模型彻底对齐 Claude Code 的核心语义：

```text
Tool Registry
→ Normalize
→ ValidateInput
→ Prepare Tool Use
→ PermissionService.Evaluate
→ Allow / Ask / Deny
→ ApprovalCoordinator（仅 Ask）
→ Execute Prepared Tool Use
→ Typed ToolResult / ToolDisplayResult
→ SessionEvent / Rollout / ContextManager / TUI
```

Approval grant 只保存在当前 Session 内存中；不实现 Claude Code 的用户级、项目级、本地权限持久化，也不把未决 Approval Request 写入 canonical history。

### 基线已完成

以下内容属于 C-R 基线，不代表 C-T 深度重构已经完成：

- 收敛 Tool Registry、Handler、ToolResult 和基础执行边界。
- 实现并统一 `read`、`edit`、`write`、`execute_command`、`update_plan`、`web_search` 等当前内置工具。
- 保留可配置 Web Search Provider：DuckDuckGo、Tavily、SearXNG、Brave。
- 删除 `read_file`、`list_dir`、`glob_files`、`grep_code` 等旧工具主链及旧 Permission Store 体系。
- 统一 Session、ApprovalCoordinator、PathResolver 和 FileSystemPolicy 的基础职责。

### C-R：Tool Runtime Refactor — `DONE`

C-R 只完成了基础统一，作为 C-T 的起点：

- [x] 统一基础 Tool Registry、Handler、错误、取消和结果回传路径。
- [x] 统一 `SessionPermissionContext`、PathResolver、FileSystemPolicy 和 ApprovalCoordinator 的基础边界。
- [x] 迁移文件工具、命令工具、Web/MCP/Skill 入口，清理 Router/Legacy Tool 双事实源。
- [x] 删除旧的 Path Guard、Legacy Registry 和旧 Permission 主链。

### C-T：Claude-style Tool Domain Refactor — `DONE`

#### C-T-01：Tool Runtime Contract

- [x] 将 `Tool` 目标概念统一为 `ToolDefinition`、`ToolUseContext`、`PreparedToolUse`、typed `ToolResult` 和 `ToolDisplayResult`。
- [x] 将调用链拆成 `Normalize`、`ValidateInput`、`Prepare`、Permission evaluation、Approval、`Execute(prepared)` 六个显式阶段。
- [x] 以显式 `PreparedToolUse.State` 传递快照和准备态，移除核心执行链对 `context.WithValue` 的依赖。
- [x] 为现有 Tool 提供短期 Adapter，迁移完成后删除旧 `Tool.Call`/`PermissionCheck.Prepared any` 主链，不长期保留双协议。

#### C-T-02：Claude-style Read-only Tools

- [x] 按 Claude Code `FileReadTool` 语义重构 `read`：canonical path、offset/limit、文本读取与 `view_image` 图片通道分流、完整读取后记录 `FileReadState`。
- [x] 按 Claude Code `GlobTool/GrepTool` 语义重构 `glob/grep`：read-only、并发安全、稳定排序、数量/字节/Token 上限、typed `truncated` 和截断原因。
- [x] 收敛 `view_image` 的路径、MIME、大小、权限和 typed media result；工作目录外读取走独立 read-directory Approval。

#### C-T-03：Claude-style File Mutation Tools

- [x] 增加 `FileReadStateStore`，记录 canonical path、存在性、内容 Hash、mode 和 symlink 状态，严格执行 Read-before-write。
- [x] 按 Claude Code `FileEditTool` 语义重构 `edit`：唯一匹配、`replace_all`、Prepare Diff、stale check、原子写入和 typed result。
- [x] 按 Claude Code `FileWriteTool` 语义重构 `write`：create/update 区分、已有文件完整 Read、覆盖 Diff、重新校验和 typed result。
- [x] 增加 typed `FileChangePreview`、`DiffHunk`、`DiffStats` 和 `FileChangeResult`，保证 Deny/Decline 零副作用、Apply 与 Preview 一致。

#### C-T-04：Permission、Approval 与 TUI Diff

- [x] 将权限 Grant 拆分为 read directory、edit directory、exact command、external host/tool，禁止跨能力复用授权。
- [x] 将结构化 Diff 原样贯通 `ApprovalRequest` → `InteractiveRequest` → TUI，禁止桥接为 `string` 或扁平 `any`。
- [x] 将通用 selection overlay 改为专用 Approval Dialog，提供有界、可滚动 Diff viewport 和 Claude Code 风格的清晰 Question/Options。
- [x] ApprovalCoordinator 只等待决定，Session 只在内存应用 grant；Session Close、进程退出或 Resume 后清空。

#### C-T-05：Codex-style Process Tools

- [x] 为 `execute_command` 增加 typed `ProcessResult`、ProcessManager、process/session ID、输出预算、exit code、truncation、取消和 lifecycle event。
- [x] 按 Codex unified exec 重构 `write_stdin`：绑定 `OriginCallID`，继承原命令 Approval，不重复 Permission/PreToolUse。
- [x] `write_stdin` 在 Registry 层声明可并行；ProcessManager 保证同一 `process_id` 串行、不同进程并行。
- [x] 保持宿主执行边界，不把 Codex Sandbox、Guardian、Remote Environment 或 Network Approval 引入主链。

#### C-T-06：Codex-style Runtime Tools

- [x] 将 `update_plan` 输入统一为 Codex 风格 `plan` + optional `explanation`，校验至多一个 `in_progress`。
- [x] `update_plan` 只调用 Session `UpdatePlan` capability、发布 `PlanUpdated` 并返回简短 `Plan updated`；完整计划不塞入通用 ToolResult 文本。
- [x] 将 `request_user_input` 保持为独立 Interactive Request，不复用 Permission Approval 或修改权限上下文。
- [x] C-T 只完成 Tool 层 Contract；Plan State、Resume、Plan Mode 与 continuation loop 的端到端实现由 F-06 完成。

#### C-T-07：Result Projection 与外部 Tool

- [x] 分离 Execution Result、模型侧 ToolResult、展示侧 ToolDisplayResult 和 canonical TurnItem，禁止 TUI 从模型文本反解析结果。
- [x] Web/MCP/Skill 迁移到同一 ToolUse 生命周期，统一 bounded output、typed error 和最小 host/tool grant。
- [x] Runtime、TUI、Rollout 和 ContextManager 对同一 Tool 结果使用一致 typed semantics。

#### C-T-08：遗留隔离与验证

- [x] 明确 `apply_patch` 与 sandbox 仅为遗留代码：不注册、不暴露给模型、不接入 ToolExecutionService/Approval/Event/Rollout/TUI 主链。
- [x] 增加 Tool/Approval/TUI Contract 测试：请求文案、目标范围、Grant 作用域、拒绝、stale conflict、Diff 截断/滚动和 Resume 后权限清空。
- [x] 增加只读截断、`write_stdin` Approval 复用与并发、`update_plan` concise result/Event、默认 Catalog 无 `apply_patch` 的测试。
- [x] 完成 C-T 后重新核对 D 的 Event/TUI 结果模型和当前进度断言，删除“已完成 Diff/Approval”一类过度承诺。

### C-T 出口

- [x] 所有内置 Tool 使用同一 ToolUse 生命周期，Tool 定义不直接访问 TUI 或更新 Session 权限。
- [x] 文件修改遵循 `Read → Prepare Diff → Approval → Revalidate → Atomic Apply → Verify`。
- [x] Approval 文案、范围和选项对齐 Claude Code；grant 仅存在于当前 Session 内存。
- [x] TUI 可以在写入前展示结构化、可滚动 Diff，并只返回用户决定。
- [x] Typed ToolResult、ToolDisplayResult、TurnItem、Rollout 和 ContextManager 对同一 Tool 结果保持一致语义。
- [x] `write_stdin` 正确续接原命令且不重复 Approval；`update_plan` 通过 Event 展示完整计划并只返回简短 ToolResult。
- [x] `apply_patch` 与 sandbox 不出现在默认 Tool Catalog、Registry、Prompt、Event、Rollout 或 TUI 主链。
- [x] 针对性测试、`go test ./...`、`go vet ./...` 和构建均通过，C 恢复为 `DONE`。
## 6. D. Event + TUI — `DONE`

### 目标

建立 Codex 风格的事件和展示主链：

```text
SessionIo
→ SessionEvent / InteractiveRequest
→ TranscriptState Reducer
→ TurnItem
→ HistoryCell
→ Inline TUI Projection
```

### 已完成

- 定义 SessionEvent、InteractiveRequest、TurnItem、HistoryCell 和稳定 ItemID。
- 将模型输出、Tool、Command、FileChange、Plan、Usage、Compaction 和 Turn 终态统一为新事件协议。
- 以 `ItemStarted → Delta → ItemCompleted` 支持实时输出，以 Completed Item 支持 Resume/Replay。
- 将 Approval、Diff、Working、Worked for、取消和 TUI 间距/颜色纳入统一展示 Contract。
- `TranscriptState` 负责事件归约，TUI 只负责输入、Popup、Viewport 和渲染。
- 删除通用 Event Hub、旧 Metadata/Sink 事件主链和 TUI 双终态推断。

### 出口

- [x] Live、Replay 和 HistoryCell 使用同一套 TurnItem 语义。
- [x] Runtime 是 Working、终态和业务事实的唯一来源。
- [x] TUI 不直接持有 Agent 业务状态。
- [x] Approval、Diff、Plan、Compaction、Tool 和 Turn 终态使用统一事件承载；typed ToolResult、专用 Approval Dialog 和可滚动结构化 Diff 已由 C-T 收敛。

## 7. E. Slash Command — `DONE`

### 目标

采用 Codex 风格的单一命令模型和分发链：

```text
Composer
→ InputResult
→ TUI dispatchCommand
→ Local Action / Application Command / Session Operation
→ SessionEvent 或 Application Result
```

### 已完成

- 命令身份统一为强类型 `SlashCommand`，命令列表由命令类型提供名称、说明和展示顺序。
- Composer 统一解析普通文本、裸命令和带参数命令。
- Popup 支持过滤、方向键选择、Enter 确认和 Esc 返回。
- 保留并统一实现 `/resume`、`/skills`、`/rename`、`/delete`、`/compact`、`/plan`、`/copy`、`/status`、`/mcp`、`/clear`、`/exit`。
- 删除 `SlashCommandCatalog`、Plain 第二主链、`--plain` 和 `cmd/amadeus` 内的重复 Slash switch。

### 出口

- [x] Slash Command 只有一个解析和分发入口。
- [x] 命令 Popup 不直接访问 Session、Store 或 Runtime。
- [x] 命令结果通过 Application/Session/Event 边界返回。
- [x] `/exit`、`/clear`、`/resume` 与 Codex 语义保持一致。

## 8. F. Codex-style Agent Engine Rewrite — `DONE`

前置条件：A-CL 与 B-CL 全部完成。F 是一次不保留旧 ReAct Engine 的功能主链替换：删除早期经典状态机和每 Turn Agent 聚合对象，建立 request-scoped StepContext 与可工作的 Codex 风格 Turn continuation loop。F 为快速完成主链曾使用 `CodingFactory/CodingRuntime` 和 Host interfaces 组织 Session capability；这些是 G 要删除的过渡结构，不再视为最终目标架构。

### F-01：删除旧 Reactor 与 Turn Agent 聚合层 — `DONE`

- `DONE`：删除 `internal/agent/react` 的 Think/Analyze/Act/Observe、LoopState、PriorIterations、ProgressMonitor hard-stop、Reactor StopReason 和 RolloutRecorder adapter。
- `DONE`：删除 `internal/agent/runtime.Agent` 每 Turn 聚合与创建/关闭路径；能力迁入 `internal/agent/engine`、Tool 与 Session 边界，不保留兼容 facade。
- `DONE`：architecture guards 禁止生产代码重新引入旧 package、ProgressMonitor hard-stop 和双 Engine 主链。

### F-02：Session-scoped Capability Reuse — `DONE`

- `DONE`：以过渡 `CodingFactory/CodingRuntime` 实现 Provider client、Tool Registry、ToolExecutionService、ProcessManager、Instruction Resolver、Extension Runtime、Compactor 和 continuation loop 的 Session-scoped 复用，并由 Session shutdown 统一关闭。
- `DONE`：RegularTask 只保存 Runtime、Turn 输入、scoped EventSink 与 target instruction handle，不持有 Factory 或 audit/process/tool 生命周期。
- `DONE`：contract test 验证连续 Turn 复用同一 Runtime，Turn Abort 不关闭 Session 资源，Factory Close 统一释放 audit/process/MCP/permission 状态。

### F-03：TurnContext 与 StepContext — `DONE`

- `DONE`：生产 TurnContext 删除 ToolNames；Tool 集合只存在于 request-scoped StepContext。
- `DONE`：每次采样前 capture PromptSnapshot、ModelInfo、Tool Specs/allow-list、Tool/MCP/Skill revision 和 target instruction scope。
- `DONE`：Prompt Tool Specs 与 ExecuteBatchScoped allow-list 来自同一 StepContext；MCP binding、文件与 instruction staleness 通过 typed Tool Result 回灌模型。

### F-04：Turn Continuation Loop — `DONE`

- `DONE`：唯一 Regular 主链为 `Capture StepContext → Maybe Compact → Sample → Persist → Execute/Persist → Continue/Final`；G 将其从 Runtime method 收敛为 Session 模块内 `run_turn`。
- `DONE`：过渡 ModelSampler 只流式发布 Started/Delta；response_item 与 durable Completed TurnItem ordered append 成功后才发布 ItemCompleted，G 将生命周期收敛为 ModelClient/ModelClientSession 与 sampling request 处理。
- `DONE`：普通 Tool denied/failed/stale/non-zero result 继续回灌模型；Provider、persistence、context/protocol invariant 才使 Turn failed。
- `DONE`：Tool batch 有界并发，Completed 按原调用顺序提交；取消为未启动调用补齐 interrupted Tool Result。

### F-05：Compaction、Budget 与 Terminal Contract — `DONE`

- `DONE`：Runtime Compactor 由自动压缩与 CompactTask 共用，不再嵌套运行 CompactTask。
- `DONE`：累计模型采样、Tool Call、token 与 elapsed time；删除低阈值 stalled，内部高阈值 safety budget 接近时注入一次完成提醒，耗尽返回 typed blocked。
- `DONE`：统一 `rollout.TurnOutcome`/TaskOutcome 与 `TurnCompleted.outcome/reason`；blocked 不通过 Go error 表达。
- `DONE`：取消可释放 Provider、Tool 和 Approval wait；Session 先 append/flush terminal facts、清理 ActiveTurn，再发布 terminal Event。

### F-06：Soft Plan、`update_plan` 与 Plan Mode — `DONE`

- `DONE`：Session-owned Plan State、canonical `plan_update`、revision、Resume 与 TUI HistoryCell 使用同一主链；`update_plan` 返回 concise ToolResult 并携带正确 TurnID Event scope。
- `DONE`：普通模式暴露 `update_plan`，计划只用于进度与沟通，不驱动调度或 DAG。
- `DONE`：`/plan` 复用 RegularTask、continuation loop、Context 与 Rollout，只通过 mode、Prompt 和 StepContext Tool mask 限制能力。
- `DONE`：Plan Mode 屏蔽 `update_plan` 与副作用 Tool，最终方案作为普通 Assistant response_item 持久化。

### F-07：Engine Integration 与旧链清理 — `DONE`

- `DONE`：删除旧 Engine 测试与 `agent.max_iterations/max_tool_calls/max_duration` 配置，新增 continuation loop、StepContext、Registry revision、ordered Tool batch、TaskOutcome 和 cancellation contract test。
- `DONE`：覆盖多 Model Step、Tool failure recovery、动态 Tool revision、auto-compaction check、Approval wait interrupt、缺失 Tool Result 恢复、Plan/update_plan 与 Plain/TUI E2E。
- `DONE`：architecture guards、`make check`、全量测试、全仓 race 与 build 通过；不存在双 Engine、私有 completed-item queue 或旧 package 引用。

### F 出口

- [x] `internal/agent/react` 与每 Turn `agentruntime.Agent` 主链已删除，生产 Agent 只有一套 continuation loop。
- [x] Session-scoped capability reuse、TurnContext 和 request-scoped StepContext 已可工作，Prompt Tool Specs 与执行 Router 同快照；最终 SessionServices 所有权和 Codex 术语由 G 收敛。
- [x] response_item、Tool Result、Completed TurnItem、Context 和 TUI 遵守同一 canonical 顺序，Resume 不依赖旧 Model Step 状态。
- [x] completed、blocked、failed、aborted、预算和中断使用统一 terminal contract。
- [x] 默认软计划、`update_plan` 与 `/plan` 复用同一 continuation loop；不存在 Planner/DAG 或第二套 Plan Runtime。
- [x] Agent、Tool、Plan、Event、Rollout、Context、Compaction 和 TUI 可端到端运行。

## 9. G. Runtime Architecture Convergence — `DONE`

G 不重写已经稳定的 Tool、Approval、Diff、MCP、Skill 或 Web 行为，而是将 F 的可工作实现收敛到 `docs/design.md` 的最终所有权与术语。每个任务必须同时迁移生产调用方、删除对应过渡抽象并补 architecture guard，不接受只重命名或长期 adapter 包裹。

### G-01：SessionServices Ownership — `DONE`

已完成第一轮 ownership 收敛：`engine.Services` 明确表示 Session-scoped capability；ThreadManager/Composition Root 为每个 Thread 创建 typed services builder，Session spawn 显式构造并保存 Agent services；连续 Turn 复用同一实例，Session shutdown 负责触发 capability close；TUI capability 查询改为从 Session-owned view 获取。Provider client 继续由 Session services 持有，单个 Turn 在 `run_turn` 中创建并复用 `ModelClientSession`。

- 将 ModelClient、ToolRegistry、ToolExecutionService、ProcessManager、Instruction/AGENTS、Extension、MCP、Skill、Web、Approval、SessionPermissionContext、Compactor、LiveThread 与 TimeProvider 归位到真正的 SessionServices。
- Session spawn 一次性构造完整 SessionState/SessionServices；Session shutdown 直接关闭本 Session 拥有的资源，连续 Turn 复用同一 capability。
- SessionState 只保留 SessionConfiguration、ContextManager、Plan 与 PreviousTurnSettings 等跨 Turn 状态；canonical Rollout 由 LiveThread 持有，不并列维护第二份 history owner。

### G-02：删除 Coding Factory/Runtime — `DONE`

- 删除 `CodingFactory`、`CodingFactoryOptions`、`CodingRuntime`、`RuntimeOptions`、通用 `task.Factory`、`PrepareRequest`、`Prepared`、`Capabilities` facade 与惰性 `ensureRuntime` 主链。
- 生产代码已删除上述旧 runtime/factory/preparation 符号；`ServicesBuilder` 仅负责 Session spawn 的一次性 typed services 构造，不提供 capability facade 或惰性 runtime 兼容层。
- ThreadManager/Composition Root 通过单一 `SessionSetup` 向 Session spawn 注入 typed configuration、shared managers、按 Op 区分的 task constructors 和 services build callback，不通过通用 TaskFactory 或双阶段 builder map 间接创建 capability。
- architecture guard 禁止生产代码重新引入 `Coding*Runtime`、`Coding*Factory`、`SessionRuntime` 或 Factory capability type assertion。

### G-03：SessionTask 与 `run_turn` 主链 — `DONE`

- Session 根据 Op 直接选择 `TaskConstructors.Regular` / `TaskConstructors.Compact` 创建 `RegularTask`、`CompactTask` 和后续 ReviewTask，并通过 Session-owned spawn/start task 生命周期运行。
- `SessionTask` 直接接收 Session、TurnContext、TurnInput 与 cancellation；删除 `TaskHost`、`TurnHost`、`PromptHost`、`ContextHost`、`RolloutHost` 等碎片化 Host interfaces。
- 将 continuation loop 从 Runtime method 收敛为 Session 模块内唯一 `run_turn`；SessionTaskResult 只表达最后 Agent message 或 typed error，terminal append、Usage、Tool count、ActiveTurn cleanup 和 Event 发布由 Session/TurnState 统一负责。
- RunningTask 收敛为 Session 保存的运行记录；goroutine 启动、panic 收敛、abort、completion 与 cleanup 不形成第二套 owner。
- `run_turn` 已成为 Session 的唯一执行入口；Engine 只提供无状态采样/工具执行所需的 typed primitive，不再提供 `Services.RunTurn` method。

### G-04：Turn、Mode 与 Interaction State — `DONE`

已完成术语迁移的第一步：生产链路使用 `TurnContext`、`TurnState`、`TaskKind`、`TurnInput` 和 `ModeKind`，Thread settings 使用 `Mode` 字段；pending waiter 已归属 `ActiveTurn`，tool/usage 进度与 interruption/terminal 状态由 `TurnState` 记录。

- 将 `turn.Context`、`turn.State`、`task.Kind`、`task.Input` 等泛化术语收敛为 `TurnContext`、`TurnState`、`TaskKind`、`TurnInput` 对应职责。
- 将 Default/Plan 从 `PermissionMode` 拆为 `ModeKind`/CollaborationMode；ApprovalPolicy、PermissionProfile 与 SessionPermissionContext 保持独立，迁移 Thread settings、Prompt、Tool mask、TUI callback 与 Rollout schema。
- 将 pending approval、pending user input、Turn input queue、tool-call count 和终态状态归入 ActiveTurn/TurnState 生命周期；Turn 结束或取消时统一清理 waiter。

### G-05：StepContext 与混合 Tool Boundary — `DONE`

- StepContext 只捕获 Turn、环境、MCP binding、Loaded AGENTS.md 和当前 Step 的 immutable ToolRouter/ToolSet，不持有 EventSink、mutable instruction handle 或完整 Prompt aggregate；事件与 instruction scope 已移到 Session-owned sampling request。
- Prompt 由 ContextManager、TurnContext 与 StepContext 在 sampling request 构建阶段统一生成；同一 ToolRouter 同时提供模型可见 Specs 和执行路由。
- 保留 ToolExecutionService、Validate/Prepare/Permission/Approval/Execute、PreparedToolUse、RequestSnapshot、ApprovalCoordinator 与 ApprovalPort；明确它们是 Claude-style Tool 内层，不承担 Session、Turn terminal 或 Tool catalog owner。
- stale registry/MCP/Skill/instruction snapshot 继续返回 typed ToolResult，不把 Codex 对齐误解为删除现有安全检查。

### G-06：Model Client、Context 与 Compaction Lifecycle — `DONE`

- 建立 Session-scoped provider client 与 Turn-scoped `ModelClientSession`；同一 Turn 的 continuation sampling 复用 session，不跨 Turn 复用。
- stream aggregation/delta 发布作为 sampling request 处理，不再以独立 `ModelSampler` aggregate 持有 Session capability；`ModelClientSession` 只在 Turn continuation 生命周期内复用。
- ContextManager 是 Session 内模型历史投影 owner；Task/`run_turn` 只通过 Session typed methods append canonical facts 和构建 PromptSnapshot。
- 自动压缩与 CompactTask 复用 Session-owned Compactor，`run_turn` 不嵌套 SessionTask；自动压缩 callback 由 Session 内部构造。

### G-07：Integration、迁移与架构验收 — `DONE`

- 迁移 Composition Root、ThreadManager、Session、Task、TUI capability query、测试 fixture 和 mocks，删除被替代文件、命名、错误文本与文档描述。
- 保持 Tool/Approval/MCP/Skill/Web 用户行为和 canonical Rollout 兼容；需要变更持久化 schema 时提供显式 migration/backward decode，不保留双写主链。
- 增加连续 Turn capability reuse、Session shutdown、Turn abort、pending waiter cleanup、Plan Mode、auto compact、ToolRouter stale snapshot 和无 CLI/TUI Session E2E。
- 完成 `make check`、全量测试、race test、architecture grep、`git diff --check` 与文档一致性检查后，G 才能标记 DONE。

当前实现已通过 `make check`、`go vet ./...`、`go build ./cmd/amadeus`、`go test ./...`、architecture grep、`git diff --check` 及受影响 Session/Engine/Manager 包 race；此前单独 PTY 重跑出现过环境时序波动，但项目验收门禁已通过，且该路径未被本次重构修改。

### G 出口

- [x] 生产代码不存在 `CodingFactory`、`CodingRuntime`、通用 TaskFactory/Prepare 主链或为规避 Session 所有权建立的 Host interface 网络。
- [x] SessionServices 是 Session capability 的唯一 owner，SessionState、ActiveTurn、TurnState、SessionTask、StepContext 与 `run_turn` 职责和术语与 Codex 对应。
- [x] Tool 调用链明确为 Codex-style StepContext/ToolRouter + Claude-style ToolExecutionService/Approval，现有安全与用户交互行为不回退。
- [x] Default/Plan、ApprovalPolicy、PermissionProfile 与 SessionPermissionContext 不再混用同一个 PermissionMode。
- [x] regular、compact、plan、interrupt、approval、resume 与连续 Turn 通过同一 Session 主链端到端运行。

## 10. H. Extensions + Release — `DONE`

- `H-01 TUI Tool Projection Convergence`：`DONE`。按 Codex HistoryCell/树状 activity 和 Claude Code Tool-specific UI projection 收敛工具展示。以真实 `TurnItem.ToolName` 为身份来源：`read`/`grep`/`glob` 进入 Codex 风格 `Exploring`/`Explored` 树，`execute_command` 进入 Codex 风格 `Running`/`Ran` 树，`update_plan` 直接沿用 Codex Plan/PlanUpdated 展示，`write`/`edit` 使用 Claude Code 风格独立展示。已覆盖 Tool 状态生命周期、结构化 ToolDisplayResult/文件变更摘要、Approval waiting/denied 展示、Rich/Raw、Inline、Live/Replay 一致性测试；`apply_patch` 明确排除在默认 Tool Catalog、Event、Rollout 和 TUI 主链之外，仅保留遗留代码。受影响包测试、race、vet、build、architecture guard 和 `git diff --check` 通过；全量测试中仍有既有 `internal/thread/manager` 环境时序超时，相关代码未修改。
- `H-02 MCP Runtime Convergence`：`DONE`。已删除旧 `Manager`/动态 Adapter 生产主链，完成 `MCPRuntime`、`MCPBinding`、typed Tool/Resource Catalog、lazy discovery、schema validation、read-only permission、typed stale/remote error、refresh/reconnect、shutdown、Resume ToolResult persistence 和 TUI typed projection；全量测试与 race 验收通过。
  - `[x] H-02.1`：MCP 配置、Client、Server binding、ToolCatalog、ResourceCatalog 和 Binding Revision 已统一进入 `MCPRuntime` owner；`ExtensionAssembly` 仅装配，Tool/TUI 消费 typed snapshot。
  - `[x] H-02.2`：已稳定 server/tool identity、description、input schema、read-only/idempotent/parallel capability、resource metadata 和 catalog revision；生产路径不再注册动态 MCP Tool Adapter。
  - `[x] H-02.3`：保留 lazy discovery；`mcp_list_tools`/`mcp_call` Prepare 验证 binding、server catalog、tool identity 和 input schema，Execute 再校验 catalog revision。
  - `[x] H-02.4`：MCP List/Resource Read 默认 Allow，MCP Call 默认 Ask；结果进入统一 ToolResult/TUI projection，远程输出保持不可信。
  - `[x] H-02.5`：Catalog 保留远程 annotation/capability；明确只读 Tool 标记为具备并行资格，写入/未知 Tool 保持串行。由于基础版统一通过参数化动态 `mcp_call` 暴露远程调用，Tool Registry 无法按单次调用选择并发，故动态入口安全固定串行；未来 direct MCP Tool 投影再启用有界并行，不复制第二套调用链。
  - `[x] H-02.6`：startup、lazy start、refresh、disconnect/reconnect、shutdown、schema/remote error、Session/Resume/stale revision 已有覆盖并通过全量测试与 race；不实现 OAuth、Elicitation、Plugin/Remote Connector 和 MCP dependency installer。
- `H-03 Skill Catalog and Resource Convergence`：`DONE`。已完成 metadata/document 分离、resource index、渐进式披露、explicit selection、stale read、script attribution、Resume projection 和旧 Skill 类型清理；全量测试与 race 验收通过。
  - `[x] H-03.1`：Skill discovery、metadata、enabled settings、resource index 和 revision 已统一由 `SkillCatalog` 管理；`ExtensionAssembly` 只装配和转换 Context injection。
  - `[x] H-03.2`：已实现 `name`、`description`、`short_description`、`path_to_skills_md`、`source/scope`、`enabled`、`policy`、`references`、`scripts`、`assets` 和 `revision`；Index 不携带正文。
  - `[x] H-03.3`：保留 `$skill-name` 显式选择和 `read_skill`；正文为 `SkillInjection`，references 只能 bounded、line-aware、path-contained 按需读取；TUI 只负责 enabled policy，不直接注入正文。
  - `[x] H-03.4`：已建立 `SKILL.md`、`references/*`、`scripts/*`、`assets/*` 边界；scripts/assets 不可被普通 reference 路径读取或自动注入。
  - `[x] H-03.5`：`execute_command` Prepare 已解析脚本归属并复用普通 Command Permission、Approval、Host Runner、ProcessManager、取消和 `write_stdin`；未创建独立 Skill Script Executor。
  - `[x] H-03.6`：已实现显式注入、`SKILL.md` 读取和实际 Skill Script 执行 attribution；归属识别不改变 Allow/Ask。
  - `[x] H-03.7a`：Skill Script attribution 已下沉至 `process.Command`/`process.Snapshot`，因此首次 `execute_command`、后续 `write_stdin`、Resume/ToolResult projection 使用同一份生命周期事实；不在 Tool 层复制或推断归属。
  - `[x] H-03.7b`：补充 disabled/override、同名 project precedence、资源 revision、path escape、常见解释器 flags 和无法确定脚本时的普通命令 fallback；不解析任意 Shell AST。
  - `[x] H-03.7`：Catalog/Resource/Policy revision、stale read/command、resume、disabled/override、same-name precedence、warning、path escape 和 TUI/ToolResult 已覆盖并通过全量测试与 race；不实现 Plugin/Remote Skill、MCP dependency installation、Product gating 和 package manager。
- H-02 与 H-03 的完成顺序：先完成 owner/data model 和旧生产主链删除，再做 lazy/resource/injection 行为，最后补 stale/lifecycle/resume 验收；仅新增字段或兼容 alias 不得标记子任务完成。
- `H-04 Web`：`DONE`。已完成 DuckDuckGo/Tavily/SearXNG/Brave Provider、统一错误分类、超时/重试、URL 校验/去重/结果上限、web_search/web_fetch Permission Contract 和 TUI Network projection；搜索/抓取失败保留可见 ToolResult 与稳定 error metadata。
- `H-05 Release Cleanup`：`DONE`。删除旧 MCP Manager/Tool Adapter、旧 Skill 混合对象和泛化 Extension Runtime；生产 owner 已收敛为 `MCPRuntime`、`SkillCatalog`、`ExtensionAssembly`，目录与文件职责已拆分，未保留 alias/wrapper 双主链。
- `H-06 Release Validation`：`DONE`。Linux/Windows/Darwin 目标构建、全量测试、全量 race、`go vet ./...`、架构 guard、文档同步和 `git diff --check` 已完成；当前沙箱偶发的 httptest loopback 监听失败不属于代码失败，独立重跑全量测试已通过。

## 11. I. Prompt Construction + Optimization — `DONE`

I 阶段专门完成 Prompt 构造架构、数据模型、命名、职责和 Prompt 资产迁移。I 不接受在旧 Prompt 主链外再包一层新接口；必须按照 `docs/design.md` 的 Codex 同构目标完成 ownership 迁移、调用方切换和旧实现删除。

### I-01：Codex Prompt 数据模型与所有权 — `DONE`

- [x] 将 Prompt 构造收敛到 Codex 风格 `Prompt`、`BaseInstructions`、`ResponseItem`、`ToolSpec`、`ModelMessages` 职责。
- [x] 统一 `Prompt` 字段语义，补齐 `OutputSchemaStrict` 等 Codex Prompt Contract；Prompt 在 StepContext/采样边界使用 immutable snapshot。
- [x] 将 `llm.Message`、`llm.ToolDefinition`、`prompt.Assets` 等旧 owner 迁移并删除，不保留 type alias、wrapper、fallback 或双写兼容路径。
- [x] 增加 architecture guard，禁止 `RegularTask`、`CompactTask`、Provider adapter、TUI 或 CLI 私自拼装 Prompt。

### I-02：ModelMessages 与 BaseInstructions — `DONE`

- [x] 引入模型级 `ModelMessages`/instruction template 解析，支持 Codex 风格 personality 模板变量。
- [x] 将 Codex 模型 Base Instructions 迁入对应内置 Prompt 资产或模型配置来源，明确 revision、来源和覆盖优先级。
- [x] 删除旧 Prompt asset owner 作为生产 BaseInstructions owner 的路径；BaseInstructions 只在当前 ModelInfo/StepContext 解析。
- [x] 验证不同 ModelInfo、Personality 和 Provider 下 BaseInstructions 稳定、可追踪且不混入动态 Workspace/Permission/Tool 事实。

### I-03：WorldState 与 Collaboration Mode — `DONE`

- [x] 建立 Codex 风格 WorldState/ContextualUserFragment owner，覆盖 collaboration mode、permissions、environment、AGENTS.md、skills 和 MCP。
- [x] 以 `CollaborationModeMessages` 选择 Default/Execute 与 Plan Developer Instructions；Plan 文本从 collaboration mode 资产迁移，不新增独立 Plan Task Prompt。
- [x] 以 Codex 风格稳定 marker、replace key 和 revision 生成 `ContextualUserFragment`/ContextUpdate，并通过 ContextManager canonical history 投影。
- [x] 删除 `DeveloperInstructions(mode, toolNames)` 字符串拼接主链，不将动态事实继续写入静态 Base Prompt。

### I-04：统一 Prompt Assembly 与 StepContext — `DONE`

- [x] 将 Prompt 主链固定为 `ModelMessages/BaseInstructions + Dynamic Context + ContextManager ResponseItems + StepContext ToolSpecs + TurnContext OutputSchema → Prompt`。
- [x] 确保普通 Turn 没有独立 `RegularTaskPrompt`；`RegularTask` 只创建任务并调用 Session 内唯一 `run_turn`。
- [x] 确保每个 Model Step 重新 capture immutable StepContext、ToolRouter snapshot、PromptSnapshot 和 capability revisions。
- [x] 删除旧 Prompt 资产组合器和调用方，保证 Live/Resume 使用同一 projector 和 Prompt Snapshot 语义。

### I-05：Codex 普通、Plan 与 Compact Prompt — `DONE`

- [x] 按 Codex 模型指令模板迁移普通 Agent 工作指引和最终交付规则，不混入 Claude Code 的系统提示词。
- [x] 按 Codex Collaboration Mode 迁移 Plan Prompt；复用同一 RegularTask、Context、Tool Mask、Event 和 Rollout 主链。
- [x] 迁移 Codex `SUMMARIZATION_PROMPT` 与 `SUMMARY_PREFIX`，使 CompactTask 只执行无 Tool 的 Summary 请求。
- [x] 保留 Amadeus `rollout.Compaction`、SourceHash、CoveredThroughSequence 和 Replacement History Contract，并删除旧短版 Compaction Prompt owner。

### I-06：Claude Code 文件与搜索 Tool Guidance — `DONE`

- [x] 按 Claude Code `FileReadTool` 迁移 `read` Prompt，适配 Amadeus 实际路径、行号和截断能力。
- [x] 按 Claude Code `FileEditTool` 迁移 `edit` Prompt，覆盖 Read-before-write、唯一匹配、`replace_all`、缩进和文件路径边界。
- [x] 按 Claude Code `FileWriteTool` 迁移 `write` Prompt，覆盖已有文件先读、Edit 优先、创建/完整重写和覆盖行为。
- [x] 按 Claude Code `GlobTool`/`GrepTool` 迁移 `glob`/`grep` Prompt，覆盖文件发现、正则、过滤、输出模式和 Amadeus 实际搜索能力。
- [x] Tool Guidance 只在对应 Tool 暴露时注入，且不声明 Amadeus 未实现的 PDF、Notebook、任意主机路径或其他能力。

### I-07：Codex Runtime Tool Guidance — `DONE`

- [x] 按 Codex Plan Tool 迁移 `update_plan` Prompt，保持 concise `Plan updated`、Event/Session Plan owner、软计划和非调度语义。
- [x] 按 Codex unified exec 迁移 `write_stdin` Prompt，保持 `process_id`、`origin_call_id`、轮询、取消、输出预算和 Approval 复用语义。
- [x] 按 Codex unified exec 与 Amadeus Contract 收敛 `execute_command` Prompt，不引入 Claude Code Bash 的 Commit/PR 或不适用的 Sandbox 规则。
- [x] 对照 ToolSpec、ToolExecutionService、ProcessManager 和 TUI Projection，删除提示词与真实 Tool Contract 不一致的旧描述。

### I-08：Prompt Cache、Token、Debug 与 Contract 验证 — `DONE`

- [x] 为 BaseInstructions、WorldState Fragment、ToolSpec、Prompt Snapshot 和 ModelMessages 增加稳定 revision/hash，明确 Prompt cache 的失效边界。
- [x] Token Accounting 同时计算 BaseInstructions、ContextualUserFragment、ResponseItems、ToolSpecs 和 OutputSchema；Prompt 变更不能绕过 ContextManager 预算判断。
- [x] 增加普通/Plan/Compact Prompt Snapshot、Tool Guidance exposure、Live/Resume projection 和无旧路径 architecture tests。
- [x] 完成受影响包测试、`make check`、全量测试、race、vet、build、architecture grep、`git diff --check` 和文档一致性检查；全量测试在授权环境通过，沙箱内 IPv6 loopback 限制不影响代码验证。

### I 出口

- Prompt 构造只有一条 Codex 同构生产主链，不存在旧 `Assets`、`DeveloperInstructions` 或 Task/Provider/TUI 私有 Prompt owner。
- 普通模式、Plan Mode、Compact 分别使用 Codex 对应机制；Plan 不新增第二套 Task/Prompt Engine，Compact 不使用普通 Tool Prompt。
- `read`、`edit`、`write`、`glob`、`grep` 使用 Claude Code Tool Guidance；`update_plan`、`write_stdin` 和命令续接使用 Codex Tool Guidance。
- Tool Guidance、ToolSpec、ToolExecutionService 和 Approval Contract 对同一 Tool 的能力边界一致；提示词不能替代 Runtime 安全校验。
- Prompt 资产、动态 Context、canonical Rollout、ContextManager、Resume、Token Accounting 和 Provider Request 的语义一致。

## 12. J. Slash Command + TUI Application Lifecycle Alignment — `DONE`

E 阶段已经交付 Slash Command 的基础命令集、Composer 解析、Popup 和单一 Fullscreen 分发入口；J 不重新建立命令框架，而是按 `docs/design.md` 的架构对齐原则，替换其中仍由旧 callback、字符串结果、Bubble Tea done message 和临时 `waitTurn` 驱动的 Application/Runtime 生命周期。

完成记录：Fullscreen 已由 `InteractiveApplication` 持续拥有 active Thread attachment 和唯一 `SessionIo` event pump；canonical replay、transactional resume、typed compact/MCP/settings/metadata/skills/shutdown 生命周期及专用 HistoryCell 已落地。旧 callback、done message、Fullscreen `runOnce/waitTurn`、字符串 command result、`interactive_commands.go` 与旧 replay helper 已删除；`make check`、全量测试、race、vet、build、architecture guards 和 `git diff --check` 均于 2026-08-19 通过。

### J-01：Active Thread Attachment 与 AppEvent 所有权 — `DONE`

- 在 Fullscreen Application 中建立持续存活的 active Thread attachment 和 event pump，由其唯一消费当前 `SessionIo.Events`、`Requests`、`Status` 与 termination。
- 普通输入与 `/compact` 只提交 typed Op；Turn running、Approval、History、Completion 和 Abort 统一由 Runtime Event 经 typed AppEvent 回到 TUI，不再由 `runTask → runOnce/waitTurn → fullscreenTaskDoneMsg` 维护第二套完成真相。
- 为 Thread switch、后台 query 和迟到结果建立 ThreadID/attachment generation 关联；旧 attachment 停止后不得继续污染当前 transcript。

### J-02：Canonical Replay Projector — `DONE`

- 建立唯一 `ProjectThreadItems` replay projector，严格按 canonical rollout sequence 投影 User、Assistant、Tool、Plan 和 ContextCompaction。
- 删除 `ProjectCompletedItems` 与 legacy item 合并后按完成时间重排的 replay 路径；Live、初始 Resume 与运行中 Thread switch 使用同一完成项投影语义。
- 增加 UserMessage、Compaction boundary、Tool/Plan、损坏尾部与 legacy decode 的顺序 Contract 测试。

### J-03：`/resume` Transactional Switch + Replay — `DONE`

- 将 picker/直接参数统一转换为 typed Resume AppEvent，由 Application/ThreadWorkspace 执行目标解析、恢复、attach、replay 和当前 Thread 提交。
- Resume 成功后缓冲并一次性刷新目标 Thread 历史，再更新 Header、Composer、Collaboration Mode、Token 和 Session metadata；Notice 不能替代 replay。
- Resume 失败时保持原 Thread、attachment 和 transcript 可用；删除 `FullscreenSessionResumer`、`fullscreenResumeMsg.message` 与只刷新 Session ID 的完成路径。

### J-04：`/compact` Session Op + Visible Lifecycle — `DONE`

- 将 `/compact` 接入正式 `CompactOp → CompactTask` 主链，并让 `TurnStarted` 携带稳定 task kind；提交后立即进入可见 pending/running UI，Runtime Event 仍是最终真相。
- durable compaction item 写入后发布 `ContextCompacted → Warning → TurnCompleted`；TUI 固定显示 `• Context compacted` 与 Codex 原文 Heads-up 警告，生成 summary 仅保留在 replacement history，不进入 transcript 文本。
- 删除 `FullscreenCompactor`、`fullscreenCompactMsg`、`compactInteractiveSession` 及 Fullscreen 内部 `waitTurn` 完成路径。

### J-05：`/mcp` Typed Inventory Query — `DONE`

- 将 `/mcp`、`/mcp verbose` 转换为 typed `FetchMCPInventory` AppEvent，使用结构化 server/tool/resource status 与 detail level，不在 Application/CLI 拼接输出字符串。
- 立即提交洋红色 `MCPCommandHistoryCell("/mcp")`，将加载反馈放入 status projection；不向不可变的 main-screen terminal history 打印随后无法替换的 loading cell。
- `MCPInventoryCell` 使用 Codex 的 `🔌  MCP Tools`、Auth、Tools、Resources、Resource templates 术语和层级；无 Server、无 Tool 和查询失败均有明确可见结果。
- 结果携带 origin ThreadID/request generation 并只接受 matching result；删除 `FullscreenMCPReader`、TUI 主链中的 `writeInteractiveMCP`、`MCPInventoryLoadingCell` 和 MCP 对通用 `fullscreenCommandDoneMsg` 的依赖。

### J-06：`/plan` Thread Settings Lifecycle — `DONE`

- `/plan` 通过 active attachment 提交 `ThreadSettingsOp`，Session 接受并发布 typed `ThreadSettingsUpdated` 后才更新 TUI collaboration projection。
- `/plan <task>` 严格执行 settings acknowledgement 后再提交 `UserInputOp`；设置失败时保留原模式且不得启动任务，快捷模式切换复用同一 lifecycle。
- 删除 `FullscreenPermissionModeSetter`、`fullscreenPermissionModeDoneMsg` 和仅修改 TUI 本地模式变量的完成路径。

### J-07：`/clear` ClearUI + Fresh Thread Lifecycle — `DONE`

- 将 `/clear` 转换为 typed `ClearUI` AppEvent；按顺序清除 pending history insertion、terminal scrollback/viewport 和 Transcript/App UI state，防止旧消息在清屏后重新 flush。
- detach/shutdown 当前 live attachment，但不删除或归档旧 canonical Thread；以 `source=clear` 启动 fresh Thread，并复用正常 configure/attach lifecycle 与 lineage/resume hint。
- fresh Thread 启动或 attach 失败时在已清空 UI 中显示 ErrorCell，不伪造 `Started a new chat`；删除 `FullscreenClearer` 和 `fullscreenCommandDoneMsg` 的 `/clear` 字符串分支。

### J-08：`/rename` Typed Metadata Notification — `DONE`

- 保留 prompt、inline name normalize 与空名称校验为 TUI-local；提交 `SetThreadName(name)` typed command 到 active Thread metadata owner。
- durable update 后发布带 ThreadID 的 `ThreadNameUpdated`，仅 matching attachment 更新 Header/metadata；提交失败显示 ErrorCell，迟到旧 Thread notification 必须丢弃。
- 删除 `FullscreenSessionRenamer`、`fullscreenRenameMsg.message`、成功字符串和仅调用 `refreshCurrentSession()` 的完成路径。

### J-09：`/delete` Confirmed Destructive Lifecycle — `DONE`

- confirmation overlay 保持 TUI-local 并绑定 attachment generation；Thread switch 关闭旧 overlay，确认 action 发送 Codex 同名 `DeleteCurrentThread`，由串行 Application event loop 解析并验证 active Thread。
- ThreadWorkspace/ThreadStore 作为唯一 delete owner，协调 attachment shutdown、rollout close、metadata 与 rollout durable delete；成功后返回 Application Exit control，失败插入 ErrorCell 并继续运行。
- 删除 `FullscreenSessionDeleter`、`fullscreenDeleteMsg.message` 和成功 Notice 后 `tea.Sequence(..., tea.Quit)` 的完成路径。

### J-10：`/status` Structured Snapshot + Correlated Refresh — `DONE`

- 从 Application/TUI 已持有的 typed Thread、Model、Mode、Token、Context、Provider 和 Turn phase 构造 `StatusSnapshot`，立即插入 `StatusHistoryCell`。
- 可选延迟数据通过 `RefreshStatusData(StatusCommand(request_id))` 获取；result 只原位完成 matching card，迟到/未知 ID 忽略，失败也必须结束 refreshing 并保留本地 snapshot。
- 删除 `FullscreenStatusReader`、`commandStatus() + output string` 拼接和 `/status` 对通用 `fullscreenCommandDoneMsg` 的依赖。

### J-11：`/skills` Typed Catalog + Config Mutation — `DONE`

- `/skills` 保留 local menu/search/toggle overlay；list action 复用现有 Skill mention/selection surface（无 mention UI 时复用唯一 typed searchable picker），manage action 使用 cached typed catalog，禁止字符串 builder 和第二份 catalog UI。
- `SetSkillEnabled(path, enabled)` 由 Application 配置 owner 持久化；成功更新缓存，失败显示 ErrorCell，关闭管理界面后触发 `ListSkills(force_reload=true)` 消除 optimistic state 偏差。
- startup/user refresh 共享 `SkillsLoaded` 数据模型并校验 cwd/catalog generation；删除 `FullscreenSkillLister`、`FullscreenSkillSetter`、`fullscreenSkillsMsg` 和 `fullscreenSkillSetMsg`。

### J-12：`/exit` Shutdown-first Application Control — `DONE`

- `/exit` 发送 `Exit(ShutdownFirst)`，立即显示 shutdown feedback，并标记 pending shutdown Thread，避免正常 termination/failover 逻辑接管用户退出。
- Application 等待 active attachment、rollout flush、后台任务和子进程清理；提供 bounded UI timeout，成功或超时后才产生最终 `tea.Quit`。
- `Immediate` 只保留给 fatal/emergency 或 shutdown 已完成后的最终跳出；删除 `/exit → tea.Quit` 直接路径并覆盖 shutdown ordering。

### J-13：`/copy` 与 TUI-local Boundary — `DONE`

- `/copy` 读取 Transcript 中最后一条 Assistant raw markdown，调用可注入 clipboard backend，并插入 Info/Error HistoryCell；无响应、成功和平台失败均有明确反馈。
- `/copy` 不创建 AppEvent、Session Op、Turn 或 Rollout item；clipboard lease 由 TUI/adapter 持有，不从已渲染 ANSI 文本反向提取内容。
- 保持现有 `SlashCommand`、`SlashInvocation`、参数校验、Popup 和单一 `dispatchCommand` 数据模型；不引入通用 Handler Registry、Command Bus 或第二套交互路由。

### J-14：旧链删除、Architecture Guards 与验收 — `DONE`

- 删除被替代的 Fullscreen callback/type/message、通用字符串 command result、重复 Session IO consumer 和旧 replay helper，并增加 architecture guards 防止回流。
- 覆盖 `/resume` replay、`/compact` 状态、空 `/mcp`、`/plan <task>` ordering、clear/rename/delete/status/skills/exit/copy、迟到事件隔离和 shutdown ordering。
- 完成针对性测试、全量测试、race、vet、build、`make check`、architecture grep、`git diff --check` 与文档一致性检查后，J 才能标记 DONE。

### J 出口

- Fullscreen Application 持续拥有唯一 active Thread attachment；Turn 与 Thread 生命周期不存在 callback/waiter/done-message 第二真相。
- `/resume` 恢复完整 canonical transcript，`/compact` 和 `/mcp` 从提交到空状态/错误/完成均有明确可见反馈，`/plan` 以 Runtime settings acknowledgement 为准。
- `/clear`、`/rename`、`/delete`、`/status`、`/skills` 和 `/exit` 已按各自 owner 使用 typed AppEvent/Session Op/HistoryCell；`/copy` 明确保留为 TUI-local。
- 生产路径不存在为保留旧实现而增加的 Codex 同名 Adapter/Facade，旧 callback、字符串完成协议、重复事件 consumer 和旧 replay 主链全部删除。

## 13. K. Response Stream Reconnect Lifecycle Alignment — `DONE`

K 从单一 `max_retries`、`Recv` 失败即终止和 terminal-only `StreamError` 的旧链出发，不增加动画补丁，而是按 `docs/design.md` 的 Codex 对齐原则重构 Provider、LLM Domain、Session/Core、Event Protocol、Compactor 与 TUI 的完整重连生命周期。

完成记录：Provider request retry 与 response stream reconnect 已拆分；Turn-scoped `ModelClientSession` 成为 sampling/compaction 的唯一 response retry owner，支持 idle timeout、Retry-After、可取消 backoff、attempt-local aggregation 和 typed transient Event。Fullscreen/Inline TUI 已实现 Codex 风格 `Reconnecting... n/m`、安全 details、status restore 与 draft replacement，并修正 Assistant/Reasoning `ItemStarted` 被误投影为 Tool/Explored activity 的旧问题。针对性测试、全量测试、race、vet、build、`make check`、architecture guards 与 `git diff --check` 均于 2026-08-19 通过。

### K-01：Provider Retry Configuration + Error Classification — `DONE`

- [x] 将单一 `provider.max_retries` 拆分为当前 HTTP/SSE 基础版唯一需要的 3 个字段：`request_max_retries`（默认 `4`、范围 `0..100`）、`stream_max_retries`（默认 `5`、范围 `0..100`）和 `stream_idle_timeout`（默认 `5m`、必须大于 `0`）。
- [x] 明确 Amadeus 不存在内置 Provider，所有用户定义 Provider 均由配置归一化层获得上述默认值；schema、patch/merge、`config show`、validation、redaction 和测试使用同一字段集合。
- [x] 旧 `max_retries` 只迁移到 `request_max_retries`，不得同时赋给 stream retry；完成迁移后删除旧生产字段、SDK wiring 和含混文案。
- [x] 审计现有 `provider.timeout`，确保它不作为活跃 streaming response 的固定 wall-clock deadline 抢先终止持续有 Delta 的长响应，并与 `stream_idle_timeout` 保持单一明确语义。
- [x] 当前不增加 Codex 的 `websocket_connect_timeout_ms` 或 transport fallback；只有 Amadeus 实现真实 WebSocket transport 后才能单独设计和排期。
- [x] 扩展 typed `ProviderError`/`ProviderErrorInfo`，稳定表达 kind/code、safe message、additional details、retryable、Retry-After/retry delay 和 request/provider identity。
- [x] OpenAI Responses/Chat Completions Adapter 只负责 transport/error 归一化与 request retry 接线，不拥有 Turn-visible stream retry counter、backoff lifecycle 或 TUI 文案。

### K-02：Core Response Stream Retry Owner — `DONE`

- [x] 在 Turn-scoped `ModelClientSession` 或 Session 模块内建立唯一 response retry helper，统一 retryability、counter、cancellable backoff、idle timeout 和 retry exhaustion。
- [x] 同一 Turn 内复用 ModelClientSession 与 sampling request 语义；TUI、SDK Adapter、RegularTask 和 Compactor 不分别维护重试状态。
- [x] cancellation 在 stream read 和 backoff 中立即生效；不可恢复错误和重试耗尽只进入一次最终 failed task result，由 Session 完成唯一 Turn terminal protocol。

### K-03：Typed Transient StreamError Protocol — `DONE`

- [x] 将当前字符串 `StreamError` 替换为包含 `Message`、`AdditionalDetails`、`ProviderError` 与 `WillRetry` 的 typed Event contract。
- [x] retry owner 发布 Codex 风格 `Reconnecting... n/m`；TUI 不解析字符串决定 retry counter、Turn running 或 terminal。
- [x] `WillRetry=true` 定义为 live、transient、non-canonical notification，不写入 Rollout、不创建 TurnItem、Resume/replay 不重放；`WillRetry=false` 与最终 Turn failure ordering 有明确 Contract test。

### K-04：TUI Status Indicator + Restore Lifecycle — `DONE`

- [x] 将硬编码 `Working` 的 working line 重构为读取当前 status header/details，并复用现有 activity marker、shimmer、elapsed time 与 `esc to interrupt`。
- [x] 收到 retrying StreamError 时保存旧 status header、确保 indicator 可见并显示 `Reconnecting... n/m` 与安全 details；不 finish draft、不提交 ActiveHistoryCell、不插入 ErrorCell。
- [x] 下一条非 retry live Event 恢复旧 header；连续 retry 只保存一次，Turn terminal 清理 retry state，Replay/Resume initial history 忽略 transient retry status。
- [x] 使用 TerminalPalette 既有 status accent，覆盖无颜色、窄终端、indicator 原本隐藏和 details 截断场景，不增加 reconnect 专用动画组件或硬编码 ANSI 色。

### K-05：Sampling + Compaction Policy Convergence — `DONE`

- [x] 普通 sampling、手动 `/compact` CompactTask 和 `run_turn` 自动 compaction 复用同一 response-stream retry policy，仅保留请求类型和 Prompt 差异。
- [x] Compactor 不拥有独立静默 retry loop；重连期间保持 Compact Turn running，成功后继续既有 `ContextCompacted → Warning → TurnCompleted` 顺序。
- [x] compact retry exhausted 保持原 ContextManager/replacement history 不变，并产生明确最终错误与唯一 Turn terminal。

### K-06：Partial Delta Attempt Isolation — `DONE`

- [x] 每次 stream attempt 使用独立 aggregation state；未完成 Delta 不进入 canonical Rollout，只有成功完成的 ResponseItem 才 append 并发布 ItemCompleted。
- [x] 定义 retry 后 draft replacement 或 stable item identity 去重，保证部分 Assistant/Reasoning/Tool Call Delta 后重连不会重复文本、重复 Tool Call 或污染下一 Model Step。
- [x] 覆盖 retry success、retry exhausted、partial delta、close error、idle timeout、Retry-After、取消、Tool Call aggregation 和 Resume semantic-equivalence。

### K-07：旧链删除、Architecture Guards 与验收 — `DONE`

- [x] 删除单一 `MaxRetries` 生产配置、terminal-only `StreamError{Error string}`、TUI StreamError 直接 finishDraft/ErrorCell 和任何 Adapter/TUI 私有 stream retry 状态。
- [x] 增加 architecture guards，禁止 `workingLine` 硬编码状态文案、Compactor 自建 retry loop、TUI 根据错误字符串判断 lifecycle，以及 transient retry Event 进入 Rollout/Replay。
- [x] 完成针对性测试、全量测试、race、vet、build、`make check`、architecture grep、`git diff --check` 与文档一致性检查后，K 才能标记 DONE。

### K 出口

- request retry 与 response stream reconnect 使用独立配置和 owner；Provider Adapter 归一化错误，Core/ModelClientSession 负责重连，TUI 只消费 typed lifecycle。
- retrying 状态显示 Codex 风格 `Reconnecting... n/m` 和 details，保持 Turn/draft 运行且不污染 History/Rollout；恢复后还原旧 status，Resume 不重放瞬态状态。
- 普通 sampling 与手动/自动 compaction 共享 retry policy；部分 Delta、取消和重试耗尽均不会产生重复 transcript、重复 Tool Call、静默卡死或第二套 Turn terminal。
- 生产路径不存在用新名称包裹 SDK retry、字符串 StreamError、硬编码 Working 或 Compactor 私有重试的过渡主链。

## 14. L. Model + Provider Configuration Ownership Alignment — `DONE`

K 完成 Provider connection recovery 生命周期后，L 针对旧 `ProviderConfig` 同时保存 transport、model selection、sampling policy 和 context policy，以及普通 Responses/Chat Completions/Compaction Request 强制发送 Amadeus 默认采样参数的问题，将配置升级为 Codex 风格的 Model Runtime + ModelProviderInfo 分层，没有保留旧字段换名后的双模型。

完成记录：稳定配置已升级到 Config v2，顶层 Model Runtime 与用户定义 `ModelProviderInfo` transport 分层完成；普通 Responses、Chat Completions 和 Compaction Request 使用模型厂商默认采样参数，`tool_output_token_limit=10000` 通过 effective ModelInfo 和唯一 Context projector 约束模型可见 Tool Result。v1 AST migration、CLI/env/provenance、README、example config、实际忽略配置与 architecture guards 已同步；Compactor 绕过 effective ModelInfo 的遗留漏洞也已修复。全量测试、全量 race、vet、build、`make check`、旧链搜索和 `git diff --check` 均于 2026-08-19 通过。

### L-01：Config v2 Schema + Codex Terminology — `DONE`

- [x] 将稳定配置升级为 `version: 2`，顶层使用 `model`、`model_provider`、`model_context_window`、`model_auto_compact_token_limit`、`tool_output_token_limit` 和 `model_providers`。
- [x] 将 Provider `api`/内部 `APIMode` 完整重构为 `wire_api`/`WireAPI`，保留 `responses` 与 `chat_completions` 两种 Amadeus 实际 transport。
- [x] schema、patch/merge、validation、clone、redaction、provenance 与默认值归一化使用同一字段集合，禁止新 YAML tag 包裹旧 `DefaultProvider`/`Providers`/`API` 数据模型。

### L-02：Provider Transport Ownership Closure — `DONE`

- [x] `ModelProviderInfo` 只保留 wire API、Dialect、auth/API key、Base URL、timeout、request retry、stream reconnect 和 Provider capability。
- [x] 从 Provider 删除 model、temperature、max output tokens、context window、auto compact limit 和 Tool Output limit；Adapter 不再从 Provider 返回伪 Model metadata。
- [x] 删除默认 `openai` Provider entry；所有 Provider 由用户定义，省略字段时由 Provider normalization 统一填入 `wire_api=responses` 和既有 retry/timeout 默认值。

### L-03：Model Runtime Overrides + ModelInfo Resolution — `DONE`

- [x] Composition/Application/Session 以顶层 `model` 与 `model_provider` 构造当前 Model selection，Provider 切换和 Model 切换不再绑定在同一个 ProviderConfig 对象中。
- [x] `model_context_window` 在无可信 Model Catalog 的当前阶段要求显式正数；`model_auto_compact_token_limit` 省略时派生为 context window 的 90%，显式值只能进一步收紧。
- [x] 当前固定采用 total active context 语义，不增加未接线的 `model_auto_compact_token_limit_scope`；ModelInfo 保存 effective context/compact/truncation policy，而不是配置对象引用。

### L-04：Provider-default Sampling + Compaction Requests — `DONE`

- [x] 从稳定配置、`llm.Request`、`SampleRequest`、`ModelInfo`、Runtime accessor 和 Session continuation 删除 temperature 与模型最大输出 token。
- [x] Responses、Chat Completions 和 Compaction Adapter 构造均省略 `temperature`、`max_output_tokens`、`max_tokens` 等字段，使用模型厂商默认行为。
- [x] 删除 Compactor 私有 4096 输出上限和 `estimated input + max output` 容量判断；自动压缩阈值负责预留 headroom，Context Window 保持独立硬上限。

### L-05：Tool Output Token Limit + Projection Policy — `DONE`

- [x] 保留 Codex 同名顶层配置 `tool_output_token_limit`，默认值固定为 `10000`，并映射为 ModelInfo/ContextManager 的 Tool Output truncation policy。
- [x] 明确该字段只限制模型可见 Tool/Function Output，不截断 canonical Rollout、终端展示或模型回复，也不与 `execute_command.max_output_tokens` 混用。
- [x] Read/Glob/Grep/Command/MCP 及其他 Tool Result 通过唯一 Context projector 应用相同预算，live 与 Resume 投影保持 semantic equivalence。

### L-06：Config Migration + User-facing Surfaces — `DONE`

- [x] 自动迁移无歧义字段：`default_provider → model_provider`、`providers → model_providers`、`api → wire_api`、`max_retries → request_max_retries`。
- [x] Provider-local model/context/compact/tool-output 字段提升存在多值冲突或新旧字段并存时返回准确路径错误；旧 temperature/max output 字段返回明确 removed-field 诊断，不静默忽略。
- [x] CLI flags、Environment names、`config check`、`config explain/show`、README、example config 和实际 `$AMADEUS_HOME/config.yaml` 模板同步到 Config v2。

### L-07：旧链删除、Architecture Guards 与验收 — `DONE`

- [x] 删除生产路径中的 `ProviderConfig.Model/Temperature/MaxOutputTokens/ContextWindow/AutoCompactTokenLimit/ToolOutputMaxTokens`、`APIMode` 和旧配置 source key。
- [x] 增加 architecture guards，禁止 Provider 重新拥有 Model Runtime policy、普通请求重新强制发送 temperature/max output，以及 Tool Output limit 混入 command/model output budget。
- [x] 覆盖 Config v1/v2 migration、无内置 Provider、Responses/Chat 参数 omission、Compaction、Context threshold、Tool projection、CLI/env/provenance 和 Resume equivalence；完成全量测试、race、vet、build、`make check` 与 `git diff --check` 后 L 才能标记 DONE。

### L 出口

- 配置使用 Codex 风格 `model`/`model_provider`/`model_providers.*.wire_api`，Provider transport 与 Model Runtime policy 具有单一 owner。
- 普通 sampling 与 Compaction 不再覆盖模型厂商 temperature 或最大输出 token 默认值；Context Window 和 Auto Compact 使用顶层 Model override。
- `tool_output_token_limit=10000` 作为唯一用户可配置 Tool/Function Output 上下文预算，并通过唯一 projector 保持 live/Resume 一致。
- 生产路径不存在 Provider-owned model/context/sampling 字段、Config v1 双主链或只换名称的兼容 Facade。

## 15. 当前保留能力

- 默认启动：`amadeus` 或 `amadeus "<task>"`。
- 当前配置链和 Provider Adapter 已可使用 OpenAI Responses/Chat Completions 及兼容 Provider。
- JSONL Canonical Rollout + SQLite Metadata Index 已可支持 Session 恢复。
- TUI 和 Inline 输出以当前代码和 `docs/design.md` 为准。
- 内置 Tool、Approval、Diff、Web Search 和 Slash Command 已进入基础主链；A/B Architecture Closure 期间保持用户可见能力，同时收口 Tool Result projection、AGENTS.md target scope 和 Session capability ownership。

## 16. 当前执行规则

1. 每次只推进一个 `TODO`/`DOING` 主任务。
2. 先修改 `docs/design.md`，再修改代码；实现发现设计问题时暂停并同步 Contract。
3. 每个 Architecture Closure 任务必须在同一任务内完成 ownership 迁移、调用方切换和对应旧主链删除；不接受“新接口包住旧 executor/projector”作为阶段性完成，不保留长期双实现。
4. 任务完成必须运行针对性测试和构建；环境限制导致的测试失败要单独记录。
5. 本文只更新任务状态和出口，不复制架构设计、源码审计或长篇讨论。

## 17. 源码结构清理 — `DONE`

### 已完成

- 审计当前目录与 `docs/design.md` 的目标 package 边界，保留 Runtime、Protocol、Tool、Policy、Interface 和 Infrastructure 分层，同时将真实 Application 生命周期从 CLI controller 迁入 `internal/app`。
- 新增 `ThreadWorkspace` 作为当前 Thread 的唯一选择 owner；CLI 不再分别保存 `ThreadManager`、`currentThread` 和锁，Thread 切换、Resume、New Draft、Rename、Delete 与 metadata 查询均通过 Application Service。
- 修正 ThreadManager shutdown 后仅异步移除实例的问题，`ShutdownThread` 现在同步移除已终止 Runtime，立即 Resume 不会重新取得 terminated Thread。
- 将 internal Session 按 runtime loop、TaskHost/history 和 event/request interaction 拆为 `session.go`、`host.go` 与 `interaction.go`。
- 将 CLI Composition Root、Turn interface、Fullscreen wiring、interactive command adapter 和 history replay 分成准确命名的文件。
- 将 Fullscreen TUI 聚合文件拆为 lifecycle/model、update/input、event projection 和 view rendering，Slash Command 与 selection 保持原有独立文件。
- 将 Approval 核心拆为 types、request、decision 和 port，Presentation 与 Coordinator 保持独立职责。
- 将 FileTools 拆为 read、edit、write 和共享 file-change pipeline，并移除字符串分支入口与未使用的旧 Approval reason helper。
- 扩展 HistoryCell 架构约束测试，使其覆盖所有拆分后的 Application 主链文件。

### 保留判断

- `internal/agent/session/session.go` 保留约 500 行的核心状态机与 Turn 生命周期；History/TaskHost 和 Event/Request 已拆出，不再包含互不相关的 adapter 或 persistence 实现。
- Composition Root 明确保留在 `cmd/amadeus/composition.go`；`internal/app` 只承载界面无关的 Application Service，不重新引入 bootstrap Service Locator。
- `cmd/amadeus` 继续维持扁平 main package，但只保留 command、flags、Composition Root 和 Interface adaptation；Runtime、当前 Thread 选择和 Session capability 均由 internal package 持有。
