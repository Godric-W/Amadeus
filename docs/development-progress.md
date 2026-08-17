# Amadeus 开发进度

> 最近更新：2026-08-17
> 唯一架构事实源：`docs/design.md`
> 当前阶段：G. Runtime Architecture Convergence
> 下一任务：G-01 SessionServices Ownership

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
```

Codex 作为 Thread、Session、SessionServices、Turn、Context、SessionTask、`run_turn`、Slash Command 和 TUI 的主要架构参考；Tool 调用链组合 Codex 的 StepContext/ToolRouter snapshot 与 Claude Code 的 Validate/Prepare/Permission/Approval/Execute 内层协议。A/B Architecture Closure 已完成，F 已删除早期经典 ReAct Engine 并建立可工作的 continuation loop；G 负责消除 F 中为快速落地引入的 `CodingFactory/CodingRuntime`、通用 TaskFactory、Host interface 和 PermissionMode 过渡结构，把 capability 所有权与 Codex 术语归位，同时保留已经稳定的 ToolExecutionService 与 Claude-style Approval。实施发现 Contract 问题时先更新 `docs/design.md`。

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

## 9. G. Runtime Architecture Convergence — `TODO`

G 不重写已经稳定的 Tool、Approval、Diff、MCP、Skill 或 Web 行为，而是将 F 的可工作实现收敛到 `docs/design.md` 的最终所有权与术语。每个任务必须同时迁移生产调用方、删除对应过渡抽象并补 architecture guard，不接受只重命名或长期 adapter 包裹。

### G-01：SessionServices Ownership — `TODO`

- 将 ModelClient、ToolRegistry、ToolExecutionService、ProcessManager、Instruction/AGENTS、Extension、MCP、Skill、Web、Approval、SessionPermissionContext、Compactor、LiveThread 与 TimeProvider 归位到真正的 SessionServices。
- Session spawn 一次性构造完整 SessionState/SessionServices；Session shutdown 直接关闭本 Session 拥有的资源，连续 Turn 复用同一 capability。
- SessionState 只保留 SessionConfiguration、ContextManager、Plan 与 PreviousTurnSettings 等跨 Turn 状态；canonical Rollout 由 LiveThread 持有，不并列维护第二份 history owner。

### G-02：删除 Coding Factory/Runtime — `TODO`

- 删除 `CodingFactory`、`CodingFactoryOptions`、`CodingRuntime`、`RuntimeOptions`、通用 `task.Factory`、`PrepareRequest`、`Prepared`、`Capabilities` facade 与惰性 `ensureRuntime` 主链。
- ThreadManager/Composition Root 直接向 Session spawn 注入 typed configuration、shared managers 与 SessionServices builder，不通过每 Thread TaskFactory 间接创建 capability。
- architecture guard 禁止生产代码重新引入 `Coding*Runtime`、`Coding*Factory`、`SessionRuntime` 或 Factory capability type assertion。

### G-03：SessionTask 与 `run_turn` 主链 — `TODO`

- Session 根据 Op 直接创建 `RegularTask`、`CompactTask` 和后续 ReviewTask，并通过 Session-owned spawn/start task 生命周期运行。
- `SessionTask` 直接接收 Session、TurnContext、TurnInput 与 cancellation；删除 `TaskHost`、`TurnHost`、`PromptHost`、`ContextHost`、`RolloutHost` 等碎片化 Host interfaces。
- 将 continuation loop 从 Runtime method 收敛为 Session 模块内唯一 `run_turn`；SessionTaskResult 只表达最后 Agent message 或 typed error，terminal append、Usage、Tool count、ActiveTurn cleanup 和 Event 发布由 Session/TurnState 统一负责。
- RunningTask 收敛为 Session 保存的运行记录；goroutine 启动、panic 收敛、abort、completion 与 cleanup 不形成第二套 owner。

### G-04：Turn、Mode 与 Interaction State — `TODO`

- 将 `turn.Context`、`turn.State`、`task.Kind`、`task.Input` 等泛化术语收敛为 `TurnContext`、`TurnState`、`TaskKind`、`TurnInput` 对应职责。
- 将 Default/Plan 从 `PermissionMode` 拆为 `ModeKind`/CollaborationMode；ApprovalPolicy、PermissionProfile 与 SessionPermissionContext 保持独立，迁移 Thread settings、Prompt、Tool mask、TUI callback 与 Rollout schema。
- 将 pending approval、pending user input、Turn input queue、tool-call count 和终态状态归入 ActiveTurn/TurnState 生命周期；Turn 结束或取消时统一清理 waiter。

### G-05：StepContext 与混合 Tool Boundary — `TODO`

- StepContext 只捕获 Turn、环境、MCP binding、Loaded AGENTS.md 和当前 Step 的 immutable ToolRouter/ToolSet，不持有 EventSink、mutable instruction handle 或完整 Prompt aggregate。
- Prompt 由 ContextManager、TurnContext 与 StepContext 在 sampling request 构建阶段统一生成；同一 ToolRouter 同时提供模型可见 Specs 和执行路由。
- 保留 ToolExecutionService、Validate/Prepare/Permission/Approval/Execute、PreparedToolUse、RequestSnapshot、ApprovalCoordinator 与 ApprovalPort；明确它们是 Claude-style Tool 内层，不承担 Session、Turn terminal 或 Tool catalog owner。
- stale registry/MCP/Skill/instruction snapshot 继续返回 typed ToolResult，不把 Codex 对齐误解为删除现有安全检查。

### G-06：Model Client、Context 与 Compaction Lifecycle — `TODO`

- 建立 Session-scoped ModelClient 与 Turn-scoped ModelClientSession；同一 Turn 内跨 retry/sampling 复用，不跨 Turn 复用。
- stream aggregation/delta 发布作为 sampling request 处理，不再以独立 ModelSampler aggregate 持有 Session capability。
- ContextManager 是 Session 内模型历史投影 owner；Task/`run_turn` 只通过 Session typed methods append canonical facts 和构建 PromptSnapshot。
- 自动压缩与 CompactTask 复用 SessionServices.Compactor，`run_turn` 不嵌套 SessionTask，也不通过外部 Compact callback 重新拼装 Session 能力。

### G-07：Integration、迁移与架构验收 — `TODO`

- 迁移 Composition Root、ThreadManager、Session、Task、TUI capability query、测试 fixture 和 mocks，删除被替代文件、命名、错误文本与文档描述。
- 保持 Tool/Approval/MCP/Skill/Web 用户行为和 canonical Rollout 兼容；需要变更持久化 schema 时提供显式 migration/backward decode，不保留双写主链。
- 增加连续 Turn capability reuse、Session shutdown、Turn abort、pending waiter cleanup、Plan Mode、auto compact、ToolRouter stale snapshot 和无 CLI/TUI Session E2E。
- 完成 `make check`、全量测试、race test、architecture grep、`git diff --check` 与文档一致性检查后，G 才能标记 DONE。

### G 出口

- [ ] 生产代码不存在 `CodingFactory`、`CodingRuntime`、通用 TaskFactory/Prepare 主链或为规避 Session 所有权建立的 Host interface 网络。
- [ ] SessionServices 是 Session capability 的唯一 owner，SessionState、ActiveTurn、TurnState、SessionTask、StepContext 与 `run_turn` 职责和术语与 Codex 对应。
- [ ] Tool 调用链明确为 Codex-style StepContext/ToolRouter + Claude-style ToolExecutionService/Approval，现有安全与用户交互行为不回退。
- [ ] Default/Plan、ApprovalPolicy、PermissionProfile 与 SessionPermissionContext 不再混用同一个 PermissionMode。
- [ ] regular、compact、plan、interrupt、approval、resume 与连续 Turn 通过同一 Session 主链端到端运行。

## 10. H. Extensions + Release — `TODO`

- `H-01 MCP`：按当前 ToolDefinition、Approval 和 Event Contract 接入 MCP 工具。
- `H-02 Skill`：实现 Skill 发现、说明、调用和脚本执行边界。
- `H-03 Web`：完善可配置 Web Search Provider、超时、重试和结果归一化。
- `H-04 Release Cleanup`：清理 Extensions 与发布阶段产生的临时适配代码；A/B/F/G 的旧 Runtime、Persistence、Context 和 Agent Engine 过渡主链必须在对应 Closure 内删除，不推迟到 H。
- `H-05 Release Validation`：跨平台构建、端到端测试、文档同步和发布验收。

## 11. 当前保留能力

- 默认启动：`amadeus` 或 `amadeus "<task>"`。
- 当前配置链和 Provider Adapter 已可使用 OpenAI Responses/Chat Completions 及兼容 Provider。
- JSONL Canonical Rollout + SQLite Metadata Index 已可支持 Session 恢复。
- TUI 和 Inline 输出以当前代码和 `docs/design.md` 为准。
- 内置 Tool、Approval、Diff、Web Search 和 Slash Command 已进入基础主链；A/B Architecture Closure 期间保持用户可见能力，同时收口 Tool Result projection、AGENTS.md target scope 和 Session capability ownership。

## 12. 当前执行规则

1. 每次只推进一个 `TODO`/`DOING` 主任务。
2. 先修改 `docs/design.md`，再修改代码；实现发现设计问题时暂停并同步 Contract。
3. 每个 Architecture Closure 任务必须在同一任务内完成 ownership 迁移、调用方切换和对应旧主链删除；不接受“新接口包住旧 executor/projector”作为阶段性完成，不保留长期双实现。
4. 任务完成必须运行针对性测试和构建；环境限制导致的测试失败要单独记录。
5. 本文只更新任务状态和出口，不复制架构设计、源码审计或长篇讨论。

## 13. 源码结构清理 — `DONE`

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
