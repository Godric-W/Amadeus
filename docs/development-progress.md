# Amadeus 开发进度

> 最近更新：2026-08-28
> 主要架构与 Contract 工作文档：`docs/design.md`
> 当前阶段：AC. Basic Multi-Agent Terminal + Persistence Lifecycle Realignment（DONE）
> 下一任务：未排定

本文只记录开发阶段、任务状态、依赖和验收出口。架构决策、数据模型和实现细节统一记录在 `docs/design.md`，不在这里重复展开。

A-AC 的条目保留为历史与当前阶段记录；其中与当前 `docs/design.md` 冲突的 token/compaction 结论由 W 取代，第一轮外层 package 归属由 X/Y 收敛，internal package、目录和责任文件布局由 Z 取代，Prompt ownership/text/WorldState/wire/Resume 结论由 AA 最终收敛，R/T 中的Multi-Agent final-message、wait、notification和persisted child membership结论由AC取代。Codex Multi-Agent V2、AgentPath/mailbox/residency、history fork、write worker、child交互、team/worktree/remote和完整agent picker是明确产品非目标，不安排后续阶段。AB 在 2026-08-28 的真实 renderer/contract 审计后重新开启；此前 AB `DONE` 只记录第一轮迁移事实，不证明 transcript viewport、completion ordering、parser boundary 或 layout contract 已闭环。历史 DONE 不构成恢复旧 owner、旧路径、旧 Prompt prefix map、Assistant Item final-message推断或已删除中间 frontend 的依据。两份文档都可能过期或不完整；不确定处必须回查对应参考源码并同步修正。

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
→ M Codex Architecture Realignment
→ N update_plan Codex Lifecycle Alignment
→ O Same-Turn User Input + Turn Steer Lifecycle Alignment
→ P Model Reasoning Effort + Provider Thinking Contract
→ Q Web + View Image Tool Contract Closure
→ R Basic Multi-Agent Architecture Alignment
→ S Codex-style Statusline Architecture Alignment
→ T Thread + Session UUID Identity Alignment
→ U Next-Turn User Input Queue Alignment
→ V Versionless Config Schema + Example Naming
→ W Context Accounting + Compaction Realignment
→ X CLI + Bootstrap Package Architecture
→ Y Initial Prompt + Single TUI Frontend Alignment
→ Z Codex-aligned Internal Package + Source Layout
→ AA Prompt Ownership + Lifecycle Realignment
→ AB Source-backed Markdown Streaming + TUI Render Lifecycle
→ AC Basic Multi-Agent Terminal + Persistence Lifecycle Realignment
```

Codex 作为 Thread、Session、SessionServices、Turn、Context、SessionTask、`run_turn`、Slash Command、TUI 和 Model/Provider 配置所有权的主要架构参考；Tool 调用链组合 Codex 的 StepContext/ToolRouter snapshot 与 Claude Code 的 Validate/Prepare/Permission/Approval/Execute 内层协议。A-L 建立了可工作的基础能力，但 2026-08-19 的源码审计确认 G/H/J 中仍保留 `engine.Services` 聚合、factory closure 网络、自定义 completed-item Rollout projection、`ExtensionAssembly`、通用 instruction scope 和独立 InteractiveRequest/Status 输出主链。M 阶段取代这些过渡架构结论，按 `docs/design.md` 直接删除旧实现，不提供旧配置、旧 Protocol、旧 Rollout、旧 SQLite schema 或旧 API 的兼容 reader、writer、decoder、migration、alias、wrapper 或测试。实施发现 Contract 问题时先分析对应参考源码，再同步更新 `docs/design.md` 与本文。

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
- 删除 `codingTaskFactory → agentController.execute*Turn` 反向依赖；该阶段暂把配置解析、Composition Root、Thread/Application 启动和 Interface 适配保留在 `cmd/amadeus`，最终外层 package 归属由 X 重新划分。
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
- [x] 将当时复用 Permission/Approval 的旧 `request_user_input` 从 Tool Catalog 和 active Interactive Request 主链完整移除，不保留协议占位；N-06 起按新的独立 Request Event/Answer Op/Session waiter Contract 重新引入，不恢复该遗留实现。
- [x] C-T 只完成 Tool 层 Contract；Plan State、Resume、Plan Mode 与 continuation loop 的端到端实现由 F-06 完成。

#### C-T-07：Result Projection 与外部 Tool

- [x] 分离 Execution Result、模型侧 ToolResult、展示侧 ToolDisplayResult 和 canonical TurnItem，禁止 TUI 从模型文本反解析结果。
- [x] Web/MCP/Skill 迁移到同一 ToolUse 生命周期，统一 bounded output、typed error 和最小 host/tool grant。
- [x] Runtime、TUI、Rollout 和 ContextManager 对同一 Tool 结果使用一致 typed semantics。

#### C-T-08：遗留隔离与验证

- [x] 当时先将 `apply_patch` 与 sandbox 隔离为遗留能力；M-08 已进一步物理删除 `apply_patch` handler、patch executor、Diff Tracker 和兼容测试。
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
→ TUI Projection
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

### F-03：TurnContext 与 StepContext — `DONE`，Prompt field ownership 由 AA 取代

- `DONE`：生产 TurnContext 删除 ToolNames；Tool 集合只存在于 request-scoped StepContext。
- `DONE`：该阶段建立了每次采样前的 request snapshot；AA 最终收敛为先 capture capability-only StepContext，再记录 WorldState，最后独立构造 PromptSnapshot。
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

### F-06：Soft Plan、`update_plan` 与 Plan Mode — `DONE`，Plan State 结论由 N 取代

- `DONE`：当时完成 Session-owned Plan State、canonical `plan_update`、revision、Resume 与 TUI HistoryCell 主链；N 按当前 Codex 源码删除该持久状态扩展，保留 concise ToolResult、正确 Turn scope 和 Plan Mode mask。
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

## 9. G. Runtime Architecture Convergence — `SUPERSEDED BY M`

G 完成了当时的第一轮 Runtime 收敛和可工作主链，但其 `engine.Services`、ServicesBuilder/SessionSetup/TaskConstructors、CapabilityView 和多级 terminal result 仍是过渡实现。M 以新的源码审计结论取代 G 的最终架构出口；G 的功能完成记录保留为历史，不再作为当前 ownership 验收依据。

### G-01：SessionServices Ownership — `DONE`

G 当时完成了 capability 的 Session-scoped 复用；其过渡 `engine.Services`/builder/view 模型已由 M-04 删除。M 随后让 `SessionServices` 直接拥有 ModelClient、ToolRegistry、ToolExecutionService、ProcessManager、AgentsMdManager、MCPRuntime、SkillCatalog、Web、Approval、Permission、当时的 Compactor、LiveThread、Clock 与 ID service；W 已进一步用无状态 CompactionService 取代 Compactor，并把 token/compaction 状态归回 Session/ContextManager。

### G-02：删除 Coding Factory/Runtime — `DONE`

- 删除 `CodingFactory`、`CodingFactoryOptions`、`CodingRuntime`、`RuntimeOptions`、通用 `task.Factory`、`PrepareRequest`、`Prepared`、`Capabilities` facade 与惰性 `ensureRuntime` 主链。
- G 当时仍保留的 services builder、session setup 和 task constructor closure 已由 M-04/M-05 删除；Session 外部装配只注入 Adapter 与 Session spawn args，Session 自己构造 services 并按 Op 直接创建 Task；该装配代码的最终 package 归属由 X 迁入 `internal/bootstrap`。
- architecture guard 禁止生产代码重新引入 `Coding*Runtime`、`Coding*Factory`、`SessionRuntime` 或 Factory capability type assertion。

### G-03：SessionTask 与 `run_turn` 主链 — `DONE`

- Session 根据 Op 直接创建 `regularTask`/`compactTask`；`SessionTask.Run(context.Context, *Session, *TurnContext)` 返回最小 TaskOutput，不存在 constructor map、Prepare/Prepared、Abort hook 或 CLI controller callback。
- `run_turn` 是 Session 模块内唯一 regular continuation loop；RunningTask 只拥有 goroutine、cancellation、panic/error capture 和 completion notification，Session 唯一创建 terminal Event 并清理 ActiveTurn。
- G 的 Engine test-only `Services/RunTurn/RunResult` 复制循环也已由 M 删除，相关覆盖迁到真实 Session continuation loop。

### G-04：Turn、Mode 与 Interaction State — `DONE`

G 完成了 `TurnContext` 与 `ModeKind` 的第一轮术语迁移；M-05 随后删除未消费的 TurnState、TaskKind、TurnInput 和 pending user-input 模型。该阶段曾通过 TaskOutput 返回 Usage/Tool count；W 已删除 TaskOutput Usage/Items，改为每个 request 立即由 Session 记录 TokenUsageInfo，TaskOutput 只保留 outcome/summary/reason/Tool count。

### G-05：StepContext 与混合 Tool Boundary — `DONE`，Prompt/Base fields 由 AA 删除

- G 当时让 StepContext 捕获 PromptSnapshot 与 BaseInstructions；AA 已删除这两个字段。当前 StepContext只冻结Model、ToolRouter、LoadedAgentsMd、Skill/Permission/SubAgent snapshots和capability revisions，Session在WorldState记录后独立构造PromptSnapshot。
- Prompt 由 Session-owned Base、ContextManager、TurnContext 与当前 StepContext ToolRouter在 sampling request边界生成；同一ToolRouter同时提供模型可见Specs和执行路由。
- 保留 ToolExecutionService、Validate/Prepare/Permission/Approval/Execute、PreparedToolUse、RequestSnapshot、ApprovalCoordinator 与 ApprovalPort；明确它们是 Claude-style Tool 内层，不承担 Session、Turn terminal 或 Tool catalog owner。
- stale registry/MCP/Skill/AgentsMd snapshot 继续返回 typed ToolResult，不把 Codex 对齐误解为删除现有安全检查。

### G-06：Model Client、Context 与 Compaction Lifecycle — `DONE`

- 建立 Session-scoped provider client 与 Turn-scoped `ModelClientSession`；同一 Turn 的 continuation sampling 复用 session，不跨 Turn 复用。
- stream aggregation/delta 发布作为 sampling request 处理，不再以独立 `ModelSampler` aggregate 持有 Session capability；`ModelClientSession` 只在 Turn continuation 生命周期内复用。
- ContextManager 是 Session 内模型历史投影 owner；Task/`run_turn` 只通过 Session typed methods append canonical facts 和构建 PromptSnapshot。
- 当时自动压缩与 CompactTask 复用 Session-owned Compactor，`run_turn` 不嵌套 SessionTask；W 已删除 callback/Compactor Rollout writer，改为两者复用 Session.runCompaction/installCompaction 与无状态 CompactionService。

### G-07：Integration、迁移与架构验收 — `SUPERSEDED BY M`

- 迁移 Composition Root、ThreadManager、Session、Task、TUI capability query、测试 fixture 和 mocks，删除被替代文件、命名、错误文本与文档描述。
- 当时保留了 Tool/Approval/MCP/Skill/Web 用户行为和 canonical Rollout 兼容；该兼容策略现已被 M 取代，旧 migration/backward decode 将直接删除。
- 增加连续 Turn capability reuse、Session shutdown、Turn abort、pending waiter cleanup、Plan Mode、auto compact、ToolRouter stale snapshot 和无 CLI/TUI Session E2E。
- 完成 `make check`、全量测试、race test、architecture grep、`git diff --check` 与文档一致性检查后，G 才能标记 DONE。

当前实现已通过 `make check`、`go vet ./...`、`go build ./cmd/amadeus`、`go test ./...`、architecture grep、`git diff --check` 及受影响 Session/Engine/Manager 包 race；此前单独 PTY 重跑出现过环境时序波动，但项目验收门禁已通过，且该路径未被本次重构修改。

### G 出口

以下出口是 G 当时的验收记录；SessionServices ownership、ToolRouter snapshot、Rollout/Event 和 factory removal 结论已被 M 重新打开。

- [x] 生产代码不存在 `CodingFactory`、`CodingRuntime`、通用 TaskFactory/Prepare 主链或为规避 Session 所有权建立的 Host interface 网络。
- [x] G 建立的可工作 Session 主链已由 M 收敛为最终 SessionServices、SessionState、ActiveTurn、SessionTask、StepContext 与 `run_turn` ownership。
- [x] Tool 调用链明确为 Codex-style StepContext/ToolRouter + Claude-style ToolExecutionService/Approval，现有安全与用户交互行为不回退。
- [x] Default/Plan、ApprovalPolicy、PermissionProfile 与 SessionPermissionContext 不再混用同一个 PermissionMode。
- [x] regular、compact、plan、interrupt、approval、resume 与连续 Turn 通过同一 Session 主链端到端运行。

## 10. H. Extensions + Release — `DONE`

H 的 MCP、Skill 和 Web 行为能力仍保留；其中 `ExtensionAssembly` 作为装配层的架构结论被 M-07 取代，M 将让 SessionServices 直接拥有 MCPRuntime 与 SkillCatalog，并删除 generic Extension 聚合。

- `H-01 TUI Tool Projection Convergence`：`DONE`。按 Codex HistoryCell/树状 activity 和 Claude Code Tool-specific UI projection 收敛工具展示。以真实 `TurnItem.ToolName` 为身份来源：`read`/`grep`/`glob` 进入 Codex 风格 `Exploring`/`Explored` 树，`execute_command` 进入 Codex 风格 `Running`/`Ran` 树，`update_plan` 直接沿用 Codex Plan/PlanUpdated 展示，`write`/`edit` 使用 Claude Code 风格独立展示。已覆盖 Tool 状态生命周期、结构化 ToolDisplayResult/文件变更摘要、Approval waiting/denied 展示以及 Rich/Raw、Live/Replay 一致性测试；当时 `apply_patch` 仅排除在主链外，M-08 已物理删除其生产实现与兼容测试。受影响包测试、race、vet、build、architecture guard 和 `git diff --check` 通过；全量测试中仍有既有 `internal/thread/manager` 环境时序超时，相关代码未修改。
- `H-02 MCP Runtime Convergence`：`DONE`。已删除旧 `Manager`/动态 Adapter 生产主链，完成 `MCPRuntime`、`MCPBinding`、typed Tool/Resource Catalog、lazy discovery、schema validation、read-only permission、typed stale/remote error、refresh/reconnect、shutdown、Resume ToolResult persistence 和 TUI typed projection；全量测试与 race 验收通过。
  - `[x] H-02.1`：MCP 配置、Client、Server binding、ToolCatalog、ResourceCatalog 和 Binding Revision 已统一进入 `MCPRuntime` owner；当时保留的 `ExtensionAssembly` 装配层由 M-07 删除。
  - `[x] H-02.2`：已稳定 server/tool identity、description、input schema、read-only/idempotent/parallel capability、resource metadata 和 catalog revision；生产路径不再注册动态 MCP Tool Adapter。
  - `[x] H-02.3`：保留 lazy discovery；`mcp_list_tools`/`mcp_call` Prepare 验证 binding、server catalog、tool identity 和 input schema，Execute 再校验 catalog revision。
  - `[x] H-02.4`：MCP List/Resource Read 默认 Allow，MCP Call 默认 Ask；结果进入统一 ToolResult/TUI projection，远程输出保持不可信。
  - `[x] H-02.5`：Catalog 保留远程 annotation/capability；明确只读 Tool 标记为具备并行资格，写入/未知 Tool 保持串行。由于基础版统一通过参数化动态 `mcp_call` 暴露远程调用，Tool Registry 无法按单次调用选择并发，故动态入口安全固定串行；未来 direct MCP Tool 投影再启用有界并行，不复制第二套调用链。
  - `[x] H-02.6`：startup、lazy start、refresh、disconnect/reconnect、shutdown、schema/remote error、Session/Resume/stale revision 已有覆盖并通过全量测试与 race；不实现 OAuth、Elicitation、Plugin/Remote Connector 和 MCP dependency installer。
- `H-03 Skill Catalog and Resource Convergence`：`DONE`。已完成 metadata/document 分离、resource index、渐进式披露、explicit selection、stale read、script attribution、Resume projection 和旧 Skill 类型清理；全量测试与 race 验收通过。
  - `[x] H-03.1`：Skill discovery、metadata、enabled settings、resource index 和 revision 已统一由 `SkillCatalog` 管理；当时保留的 `ExtensionAssembly` 装配和 Context injection 转换由 M-07 删除。
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
- `H-05 Release Cleanup`：`SUPERSEDED BY M-07`。当时删除旧 MCP Manager/Tool Adapter、旧 Skill 混合对象和泛化 Extension Runtime，但仍保留 `ExtensionAssembly`；M-07 将直接删除该聚合。
- `H-06 Release Validation`：`DONE`。Linux/Windows/Darwin 目标构建、全量测试、全量 race、`go vet ./...`、架构 guard、文档同步和 `git diff --check` 已完成；当前沙箱偶发的 httptest loopback 监听失败不属于代码失败，独立重跑全量测试已通过。

## 11. I. Prompt Construction + Optimization — `DONE`，Prompt lifecycle 结论由 AA 取代

I 阶段完成了当时的 Prompt 类型、内置资产、StepContext/ToolRouter 和 Compact synthetic User 基础链，但 2026-08-26 对当前 Codex 源码的复查确认：Session Base provenance、Responses `instructions` wire、typed WorldState full/diff、fragment role/order、mode/tool owner、Prompt 文本完整度和 compact/resume baseline 仍未对齐。I 的完成记录保留为历史，I-02/I-03/I-05/I-08 的最终架构出口由 AA 取代。

### I-01：Codex Prompt 数据模型与所有权 — `DONE`

- [x] 将 Prompt 构造收敛到 Codex 风格 `Prompt`、`BaseInstructions`、`ResponseItem`、`ToolSpec`、`ModelMessages` 职责。
- [x] 统一 `Prompt` 字段语义，补齐 `OutputSchemaStrict` 等 Codex Prompt Contract；Prompt 在 StepContext/采样边界使用 immutable snapshot。
- [x] 将 `llm.Message`、`llm.ToolDefinition`、`prompt.Assets` 等旧 owner 迁移并删除，不保留 type alias、wrapper、fallback 或双写兼容路径。
- [x] 增加 architecture guard，禁止 `RegularTask`、`CompactTask`、Provider adapter、TUI 或 CLI 私自拼装 Prompt。

### I-02：ModelMessages 与 BaseInstructions — `SUPERSEDED BY AA`

- [x] 引入模型级 `ModelMessages`/instruction template 解析，支持 Codex 风格 personality 模板变量。
- [x] 将 Codex 模型 Base Instructions 迁入对应内置 Prompt 资产或模型配置来源，明确 revision、来源和覆盖优先级。
- [x] 删除旧 Prompt asset owner 作为生产 BaseInstructions owner 的路径；BaseInstructions 只在当前 ModelInfo/StepContext 解析。
- [x] 验证不同 ModelInfo、Personality 和 Provider 下 BaseInstructions 稳定、可追踪且不混入动态 Workspace/Permission/Tool 事实。

### I-03：WorldState 与 Collaboration Mode — `SUPERSEDED BY AA`

- [x] 建立 Codex 风格 WorldState/ContextualUserFragment owner，覆盖 collaboration mode、permissions、environment、AGENTS.md、skills 和 MCP。
- [x] 以 `CollaborationModeMessages` 选择 Default 与 Plan Developer Instructions；Plan 文本从 collaboration mode 资产迁移，不新增独立 Plan Task Prompt。
- [x] 以 Codex 风格稳定 marker、replace key 和 revision 生成 `ContextualUserFragment`/ContextUpdate，并通过 ContextManager canonical history 投影。
- [x] 删除 `DeveloperInstructions(mode, toolNames)` 字符串拼接主链，不将动态事实继续写入静态 Base Prompt。

### I-04：统一 Prompt Assembly 与 StepContext — `DONE`，记录顺序与 lifecycle 由 AA 取代

- [x] 将 Prompt 主链固定为 `ModelMessages/BaseInstructions + Dynamic Context + ContextManager ResponseItems + StepContext ToolSpecs + TurnContext OutputSchema → Prompt`。
- [x] 确保普通 Turn 没有独立 `RegularTaskPrompt`；`RegularTask` 只创建任务并调用 Session 内唯一 `run_turn`。
- [x] 确保每个 Model Step 重新 capture immutable StepContext、ToolRouter snapshot、PromptSnapshot 和 capability revisions。
- [x] 删除旧 Prompt 资产组合器和调用方，保证 Live/Resume 使用同一 projector 和 Prompt Snapshot 语义。

### I-05：Codex 普通、Plan 与 Compact Prompt — `SUPERSEDED BY AA`

- [x] 按 Codex 模型指令模板迁移普通 Agent 工作指引和最终交付规则，不混入 Claude Code 的系统提示词。
- [x] 按 Codex Collaboration Mode 迁移 Plan Prompt；复用同一 RegularTask、Context、Tool Mask、Event 和 Rollout 主链。
- [x] 迁移 Codex `SUMMARIZATION_PROMPT` 与 `SUMMARY_PREFIX`，使 CompactTask 只执行无 Tool 的 Summary 请求。
- [x] 保留 Amadeus `rollout.Compaction`、SourceHash、CoveredThroughSequence 和 Replacement History Contract，并删除旧短版 Compaction Prompt owner。

### I-06：Claude Code 文件与搜索 Tool Guidance — `DONE`，装配 owner 由 AA 取代

- [x] 按 Claude Code `FileReadTool` 迁移 `read` Prompt，适配 Amadeus 实际路径、行号和截断能力。
- [x] 按 Claude Code `FileEditTool` 迁移 `edit` Prompt，覆盖 Read-before-write、唯一匹配、`replace_all`、缩进和文件路径边界。
- [x] 按 Claude Code `FileWriteTool` 迁移 `write` Prompt，覆盖已有文件先读、Edit 优先、创建/完整重写和覆盖行为。
- [x] 按 Claude Code `GlobTool`/`GrepTool` 迁移 `glob`/`grep` Prompt，覆盖文件发现、正则、过滤、输出模式和 Amadeus 实际搜索能力。
- [x] Tool Guidance 只在对应 Tool 暴露时注入，且不声明 Amadeus 未实现的 PDF、Notebook、任意主机路径或其他能力。

### I-07：Codex Runtime Tool Guidance — `DONE`，装配 owner 由 AA 取代

- [x] 按 Codex Plan Tool 迁移 `update_plan` Prompt，保持 concise `Plan updated`、软计划和非调度语义；其中 Event/Session Plan owner 结论由 N 按当前 Codex lifecycle 取代。
- [x] 按 Codex unified exec 迁移 `write_stdin` Prompt，保持 `process_id`、`origin_call_id`、轮询、取消、输出预算和 Approval 复用语义。
- [x] 按 Codex unified exec 与 Amadeus Contract 收敛 `execute_command` Prompt，不引入 Claude Code Bash 的 Commit/PR 或不适用的 Sandbox 规则。
- [x] 对照 ToolSpec、ToolExecutionService、ProcessManager 和 TUI Projection，删除提示词与真实 Tool Contract 不一致的旧描述。

### I-08：Prompt Cache、Token、Debug 与 Contract 验证 — `SUPERSEDED BY AA`

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

- 在 Fullscreen Application 中建立持续存活的 active Thread attachment 和 event pump；当时仍消费 `Events`、`Requests`、`Status`，该多输出协议由 M-01 统一为 Event/EventMsg 单流。
- 普通输入与 `/compact` 只提交 typed Op；Turn running、Approval、History、Completion 和 Abort 统一由 Runtime Event 经 typed AppEvent 回到 TUI，不再由 `runTask → runOnce/waitTurn → fullscreenTaskDoneMsg` 维护第二套完成真相。
- 为 Thread switch、后台 query 和迟到结果建立 ThreadID/attachment generation 关联；旧 attachment 停止后不得继续污染当前 transcript。

### J-02：Canonical Replay Projector — `SUPERSEDED BY M-02`

- 当时建立 `ProjectThreadItems` replay projector 并统一完成项顺序，但仍依赖自定义 completed-item Rollout projection。
- M-02 改为 typed RolloutItem + persisted EventMsg，删除 `TurnItemCompleted`、fallback projector、legacy decode 和旧格式测试。

### J-03：`/resume` Transactional Switch + Replay — `DONE`

- 将 picker/直接参数统一转换为 typed Resume AppEvent，由 Application/ThreadWorkspace 执行目标解析、恢复、attach、replay 和当前 Thread 提交。
- Resume 成功后缓冲并一次性刷新目标 Thread 历史，再更新 Header、Composer、Collaboration Mode、Token 和 Session metadata；Notice 不能替代 replay。
- Resume 失败时保持原 Thread、attachment 和 transcript 可用；删除 `FullscreenSessionResumer`、`fullscreenResumeMsg.message` 与只刷新 Session ID 的完成路径。

### J-04：`/compact` Session Op + Visible Lifecycle — `DONE`

- 将 `/compact` 接入正式 `CompactOp → CompactTask` 主链，并让 `TurnStarted` 携带稳定 task kind；提交后立即进入可见 pending/running UI，Runtime Event 仍是最终真相。
- 当时 durable compaction item 写入后发布 `ContextCompacted → Warning → TurnCompleted`；W 已删除独立 ContextCompactedEvent，改为 live ContextCompaction ItemCompleted 与 Replay CompactedItem 共用一个 `• Context compacted` 投影，Warning/Turn terminal 顺序保持不变。
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

完成记录：Provider request retry 与 response stream reconnect 已拆分；Turn-scoped `ModelClientSession` 成为 sampling/compaction 的唯一 response retry owner，支持 idle timeout、Retry-After、可取消 backoff、attempt-local aggregation 和 typed transient Event。Fullscreen TUI 已实现 Codex 风格 `Reconnecting... n/m`、安全 details、status restore 与 draft replacement，并修正 Assistant/Reasoning `ItemStarted` 被误投影为 Tool/Explored activity 的旧问题。针对性测试、全量测试、race、vet、build、`make check`、architecture guards 与 `git diff --check` 均于 2026-08-19 通过。

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
- [x] 同一 Turn 内复用 ModelClientSession 与 sampling request 语义；TUI、SDK Adapter、RegularTask 和当时的 Compactor 不分别维护重试状态。W 的 CompactionService 延续该边界。
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
- [x] 当时的 Compactor 不拥有独立静默 retry loop；W 删除 Compactor 和 ContextCompactedEvent 后，重连仍由同一 ModelClientSession 负责，成功顺序改为 ContextCompaction ItemCompleted → Warning → TurnCompleted。
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

完成记录：稳定配置已升级到 Config v2，顶层 Model Runtime 与用户定义 `ModelProviderInfo` transport 分层完成；普通 Responses、Chat Completions 和 Compaction Request 使用模型厂商默认采样参数，`tool_output_token_limit=10000` 通过 effective ModelInfo 和唯一 Context projector 约束模型可见 Tool Result。当时实现的 v1 AST migration 属于开发期过渡代码，现由 M-08 直接删除；CLI/env/provenance、README、example config、实际忽略配置与 architecture guards 已同步。全量测试、全量 race、vet、build、`make check`、旧链搜索和 `git diff --check` 均于 2026-08-19 通过。

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

### L-06：Config Migration + User-facing Surfaces — `SUPERSEDED BY M-08`

- [x] 自动迁移无歧义字段：`default_provider → model_provider`、`providers → model_providers`、`api → wire_api`、`max_retries → request_max_retries`。
- [x] Provider-local model/context/compact/tool-output 字段提升存在多值冲突或新旧字段并存时返回准确路径错误；旧 temperature/max output 字段返回明确 removed-field 诊断，不静默忽略。
- [x] CLI flags、Environment names、`config check`、`config explain/show`、README、example config 和实际 `$AMADEUS_HOME/config.yaml` 模板同步到 Config v2。

上述 migration 是历史完成记录；M-08 删除 Config v1 decoder/migration 和相关 fixture，当前开发数据必须按 Config v2 重建。

### L-07：旧链删除、Architecture Guards 与验收 — `DONE`

- [x] 删除生产路径中的 `ProviderConfig.Model/Temperature/MaxOutputTokens/ContextWindow/AutoCompactTokenLimit/ToolOutputMaxTokens`、`APIMode` 和旧配置 source key。
- [x] 增加 architecture guards，禁止 Provider 重新拥有 Model Runtime policy、普通请求重新强制发送 temperature/max output，以及 Tool Output limit 混入 command/model output budget。
- [x] 覆盖 Config v1/v2 migration、无内置 Provider、Responses/Chat 参数 omission、Compaction、Context threshold、Tool projection、CLI/env/provenance 和 Resume equivalence；完成全量测试、race、vet、build、`make check` 与 `git diff --check` 后 L 才能标记 DONE。

### L 出口

- 配置使用 Codex 风格 `model`/`model_provider`/`model_providers.*.wire_api`，Provider transport 与 Model Runtime policy 具有单一 owner。
- 普通 sampling 与 Compaction 不再覆盖模型厂商 temperature 或最大输出 token 默认值；Context Window 和 Auto Compact 使用顶层 Model override。
- `tool_output_token_limit=10000` 作为唯一用户可配置 Tool/Function Output 上下文预算，并通过唯一 projector 保持 live/Resume 一致。
- 生产路径不存在 Provider-owned model/context/sampling 字段、Config v1 双主链或只换名称的兼容 Facade。

## 15. M. Codex Architecture Realignment — `DONE`

M 基于 2026-08-19 对当前源码与 Codex 架构的重新审计，纠正 G/H/J 阶段仍保留的过渡 owner、factory、Protocol、Rollout 和 Extension/Instruction 模型。Amadeus 尚未发布，本阶段不考虑旧读写兼容：替换完成后直接删除旧 reader、writer、decoder、migration、alias、wrapper、Adapter、Facade、fixture 和兼容测试；旧 JSONL、SQLite、Config 和本地开发数据由开发者清理后按当前 schema 重建。

完成记录：2026-08-20 完成 Protocol identity/Event envelope、typed Rollout、Context 增量 owner、SessionServices、SessionTask/terminal、immutable ToolRouter、AgentsMd/Skill/MCP owner 与 UI projection 的整体收敛；物理删除 Config v1 migration、Extension/Instruction aggregate、旧 Engine test loop、`apply_patch` handler/patch executor/Diff Tracker 和其他 compatibility-only 代码。README、design、Config v2 example、Provider fixture 与本地开发模板均已核对为当前 schema；旧 `bin` SQLite/Rollout 数据已清理并由当前构建重新生成二进制。针对性测试、live/Resume E2E、全量测试、全量 race、vet、build、`make check`、architecture grep 和 `git diff --check` 全部通过。

### M-01：Protocol Identity + Event Envelope — `DONE`

- 将 ThreadID、TurnID、SubmissionID、RequestID 和 ItemID 归入 Protocol/Identity domain，删除 rollout/thread/turn package 对 ID 的定义或 alias。
- 建立 `Submission{ID, Op}`、`Event{ID, Msg}` 和 typed `EventMsg`；按需在 payload 中携带 ThreadID、TurnID、RequestID 和 ItemID。
- 将 `SessionEvent`/`EventMessage` 收敛为 `Event`/`EventMsg`，完成 `SessionConfiguredEvent`、`ThreadSettingsAppliedEvent`、`TurnCompleteEvent`、`ErrorEvent`、`AgentMessageContentDeltaEvent`、`PlanUpdateEvent` 和 `TokenCountEvent` 等命名迁移。
- 将 Approval request 作为 EventMsg，回答使用 correlated `ApprovalDecisionOp`；删除 `SessionIo.Requests`、公开 `SessionIo.Status`、`InteractiveRequest`、`TurnRejected` 和重复 terminal/status 协议。
- AgentStatus 仅保留为 Event reducer 派生的内部 watch/read model，不成为第二公开输出主链。

### M-02：Typed RolloutItem + Persistence Reset — `DONE`

- 建立 `SessionMetaItem`、`ResponseItem`、`CompactedItem`、`TurnContextItem` 和 `EventMsgItem` typed sum，RolloutLine 只负责当前格式的 version/sequence/timestamp。
- 原样持久化 store policy 选中的 EventMsg；删除 `KindTurnItemCompleted`、`TurnItemCompleted`、`NewCompletedItem`、completed-item queue 和 response/completed fallback projector。
- 将 Thread/Turn identity 从 persistence package 移出，生产 writer、reader、Context 和 TUI 共享唯一 typed contract，不使用 `kind + RawMessage` 领域主链。
- 删除旧 Rollout reader/writer、legacy decoder、migration projector、旧 SQLite canonical tables 和兼容 fixture；未知旧格式直接返回 unsupported-format 诊断。
- SQLite 只接受当前 schema version；metadata index 可以从当前格式 Rollout 重建，旧 JSONL/SQLite 开发数据直接清理。

### M-03：SessionState + Context Ownership — `DONE`

- 让 ContextManager 成为活动 Session 的内存 history owner，SessionState 不保存 `[]rollout.Line` 或第二份历史集合。
- Resume 时从 typed Rollout 重建一次；运行期间在 durable append 成功后按 typed fact 增量 record，不再每次 append 全量 rebuild。
- ThreadStore append 不返回完整 Lines 驱动 Session；Context mutation、Plan projection 和 usage projection 均由 Session 按唯一顺序提交。
- Compaction replacement 继续允许原位 atomic rebuild，但与普通 append 的增量路径共享 normalization/projector 语义。
- 增加 live incremental 与 Resume rebuild 的 semantic-equivalence、ToolCall/ToolResult pairing、interrupt 和 compaction contract tests。

### M-04：SessionServices Ownership — `DONE`

- 让 `SessionServices` 直接拥有 ModelClient、ToolRegistry、ToolExecutionService、ProcessManager、AgentsMdManager、MCPRuntime、SkillCatalog、Web、Approval、Permission、当时的 Compactor、LiveThread 和 ID/Time services；W 已将该 slot 替换为无状态 CompactionService。
- 删除 `engine.Services`、`AgentServices`、`ServicesBuilder`、`SessionSetup`、`TaskConstructors`、CLI factory closure、`CapabilityView` 和相关 adapter/guard exceptions。
- bootstrap 只构造外部 Adapter 和 Session spawn args；Session 自己构造 services、tasks 并负责 shutdown。
- `SessionState.Configuration` 成为当前配置唯一事实源，删除 `engine.Services.configured`、首次 closure capture 和其他重复 configuration snapshot。
- 更新 ThreadManager、Application typed query、fixtures 和 mocks，禁止通过 capability facade 或 type assertion 取回 Session 内部服务。

### M-05：SessionTask + Turn Lifecycle — `DONE`

- Session 根据 Op 直接创建 RegularTask/CompactTask，不经过 constructor map、factory callback、Prepare/Prepared 或 CLI controller。
- 将 runtime `TurnContext` 与 durable `TurnContextItem` 分离；运行对象不直接序列化，durable DTO 只保存稳定恢复事实。
- 删除 `engine.RunResult → session.Result → rollout/protocol terminal` 多级转换；Task 只返回最小 TaskOutput，Session 唯一创建 TurnCompleteEvent、TurnAbortedEvent 和 ErrorEvent。
- ActiveTurn 只保存 Submission correlation、RunningTask 和 typed pending approval；usage/tool count 由 `run_turn` 局部累计并通过 TaskOutput 返回，cancellation 归 RunningTask，终态映射归 Session；删除重复 TaskKind、pending user input 和未消费 lifecycle 字段。
- 固化 accepted、startup error、completed、blocked、failed、aborted、panic、interrupt 和 shutdown 的唯一完成协议。

### M-06：Immutable StepContext + ToolRouter — `DONE`

- StepContext 中的 immutable ToolRouter 同时保存模型可见 specs、exact ToolDefinition handler、MCP binding、visibility 和 parallel flags。
- 同一次 sampling 的 Prompt Tool Specs 与随后 Tool dispatch 必须使用同一 router；执行时不得按名称重新查询当前 mutable registry。
- 删除 name-only `AllowedTools`、未约束 handler identity 的 `ToolRevision` 和 stale registry lookup 主链；deferred/lazy call 按 frozen identity/revision 返回 typed stale result。
- 保留 Claude-style `Validate → Prepare → Permission → Approval → Execute` 内层协议、Read-before-write、Diff Preview、revalidate 和 atomic apply。
- 增加 registry/MCP/Skill 变化、同名 handler replacement、Plan tool mask 和 parallel capability snapshot tests。

### M-07：AgentsMd + Skills + MCP Ownership — `DONE`

- 删除 `ExtensionAssembly`、generic Extension aggregate 和相关 conversion wrapper；SessionServices 直接拥有 MCPRuntime 与 SkillCatalog。
- 建立 `AgentsMdManager + LoadedAgentsMd` 唯一模型，删除 generic `WorkspaceResolver`、`InstructionScope`、target instruction service、`MarkSampled` 和 `context_refresh_required` 主链。
- capture StepContext 时统一解析 AgentsMd、Skill snapshot、MCP binding 和 ToolRouter；Tool 只报告 target/stale facts，不直接修改 ContextManager。
- 保留 MCP lazy discovery、typed Catalog/Binding、Skill progressive disclosure、script attribution 和普通 Permission/Approval 行为。
- 更新 Prompt、Context Update、Tool Prepare、TUI query 和 architecture guards，确认 AgentsMd/Skill/MCP 各有直接且唯一 owner。

### M-08：Protocol/UI Projection + Legacy Cleanup Acceptance — `DONE`

- 将 TranscriptState、EventReducer、History projection 和 Rollout replay 从 `internal/agent/protocol` 迁出；Protocol package 只保留 identity、DTO 和 contracts。
- 删除重复 TaskKind、旧 Event aliases、request/status side channel、completed-item projector、Config v1 migration、旧 schema tests 和所有只服务兼容的 production code。
- 更新 README、design、examples、fixtures 和 `$AMADEUS_HOME` 开发模板，只描述当前 Config/Protocol/Rollout schema；旧数据由开发者删除重建。
- 增加 architecture guards，禁止 `engine.Services`、factory closure network、`ExtensionAssembly`、`InteractiveRequest`、`KindTurnItemCompleted`、legacy reader/writer/decoder/migration 和 protocol-owned UI reducer 回归。
- 完成针对性测试、全量测试、race、vet、build、`make check`、architecture grep、`git diff --check` 和 live/Resume E2E 后，M 才能标记 DONE。

### M 出口

- 生产代码不存在旧读写兼容、旧 schema migration、legacy decoder、alias/wrapper Facade 或双格式探测；旧开发数据必须删除重建。
- SessionServices 是唯一 capability owner，不存在 `engine.Services`、AgentServices、ServicesBuilder、SessionSetup、TaskConstructors、CapabilityView 或 Composition closure 网络。
- SessionIo 只有 Submission、Event 和 Terminated 主边界；Approval 进入 EventMsg，不存在 SessionEvent/InteractiveRequest/Status 三输出协议。
- Rollout 使用 typed RolloutItem 并持久化选定 EventMsg；不存在 KindTurnItemCompleted、custom completed projection 或 fallback replay。
- ContextManager 在 Resume 时重建一次、运行时增量 record；SessionState 不保存第二份 Rollout lines。
- StepContext 的 ToolRouter 同时决定模型 specs 与 exact dispatch；registry/MCP/Skill 变化不会改变已冻结 step 的 handler identity。
- AgentsMdManager/LoadedAgentsMd、SkillCatalog 和 MCPRuntime 分别直接归 SessionServices 所有，不存在 generic Extension/Instruction assembly。
- Protocol package 只保留 identity/DTO/contracts，TUI reducer、TranscriptState 和 replay projector 位于各自职责 package。

## 16. N. Runtime Coordination Tools + Plan Mode Codex Lifecycle Alignment — `DONE`，Plan Prompt owner/text 由 AA 取代

### 目标

按当前 `../codex-main` 的真实实现完成三条相互依赖的收敛主链：先将 `update_plan` 从 Session-owned durable Plan State 收敛为 transient checklist Event；再以全新独立 Contract 引入 Default/Plan 通用的 `request_user_input`，不恢复 C-T-06 删除的 Permission/Approval 复用实现；最后把 `/plan` 重构为 Codex 风格 Collaboration Mode、原子 settings/input lifecycle 和 `<proposed_plan> → PlanDeltaEvent → completed PlanItem` 输出协议。全程保留 Amadeus 现有 `ToolDefinition → ToolExecutionService` 混合调用链，不引入 Handler。

N 不扩展 Planner、DAG、Plan Mode Task、plan file、`EnterPlanMode`/`ExitPlanMode` Tool 或复杂模式配置。N 只完成 Runtime coordination Tool、Session interactive waiter、Collaboration Mode、Plan Prompt、Proposed Plan Event/TurnItem、Slash/TUI 生命周期和旧链删除。

N 的 `update_plan`、`request_user_input`、settings、Proposed Plan Event/TurnItem 和 TUI lifecycle 继续有效；N-09/N-11 中“Plan Prompt 已完整迁移且只有一个 owner”的判断由 AA 取代。AA 不恢复 Planner/DAG，也不改变 N 的交互协议，只替换 Collaboration Mode 文本来源、WorldState lifecycle 和 Tool guidance owner。

### N-01：Protocol Contract + Tool Boundary — `DONE`

- 建立 Codex 对齐的 `StepStatus`、`PlanItemArg` 和 `UpdatePlanArgs` typed contract，`PlanUpdateEvent` 只增加 Amadeus 统一的 Thread/Turn scope。
- 从 Event 删除 `ItemID`、revision、更新时间和 snapshot 语义；字段统一使用 `plan`，不再使用内部 `items` 等第二套名称。
- 重写现有 `UpdatePlan` ToolDefinition，使其直接发布 transient `PlanUpdateEvent` 并严格返回 `Plan updated`；不新增 Handler、Recorder 或 Session capability。
- 保留状态/空步骤校验和至多一个 `in_progress`；允许空 `plan` 清空 checklist，删除固定最大条目数等非 Codex 基础限制。
- 默认注册 `update_plan`，删除 `PlanUpdater != nil` 隐式 feature gate；Plan Mode 继续由 frozen ToolRouter mask 隐藏该 Tool。

### N-02：Session、Rollout 与 Context State Removal — `DONE`

- 删除 `internal/agent/plan`、`PlanUpdater`、`SessionState.Plan`、`Session.UpdatePlan` 及 ToolRuntime/CoreToolOptions 的相关 wiring。
- 删除 plan snapshot、revision、durable append、latest-plan scan 和 Spawn/Resume restore 主链，不保留 wrapper、alias 或 legacy fallback。
- `PlanUpdateEvent` 不进入 canonical JSONL Rollout；正常 Tool Call 和 `ResponseToolResult` 继续按 ordered ResponseItem 主链持久化。
- 删除 ContextManager/Rollout projection 对 PlanUpdate 的 Developer Message 注入，确保模型历史中不再同时出现 Tool Call 参数和“Current soft execution plan”第二事实源。
- 删除 interactive history 中 `PlanUpdateEvent → ItemPlan` 的 replay projection；保留并重新定义 `ItemPlan` 只用于 N-12 的 Plan Mode Proposed Plan，checklist 不再生产或恢复该 TurnItem。

### N-03：Tool Lifecycle Event Policy — `DONE`

- 调整 `toolEventObserver`，使 `update_plan` 不产生普通 `ItemStartedEvent`、`ItemCompletedEvent` 或 Tool activity TurnItem，但无论成功或失败都保留模型可见 Tool Result。
- 将 dedicated-event Tool 判断集中在 observer 单一策略边界，不把 UI lifecycle 字段加入模型可见 `ToolSpec`，也不在多个 TUI reducer 中重复按名称隐藏。
- 保持普通 Tool 的 Started/Completed、ordered append、错误回灌和 Approval lifecycle 不变，避免为 `update_plan` 破坏统一 ToolExecutionService。
- 增加 observer contract tests，证明 `update_plan` 只有 ResponseToolResult persistence，而普通 Tool 仍同时产生 ResponseToolResult 与 completed TurnItem。

### N-04：Live TUI + Replay Semantics — `DONE`

- TUI 仅消费 live `PlanUpdateEvent` 生成一个 Codex 风格 `Updated Plan` HistoryCell，不显示 `Running update_plan` 或普通 Tool completion。
- 删除 revision 驱动的 planning/replanning 分支和分散的 ToolName 特判；UI-local progress 只从当前 live Event 计算。
- Resume/Replay 不恢复旧 checklist、不伪造 Plan TurnItem；Rich/Raw 对 live PlanUpdate 保持一致，对持久化普通 Tool Item 继续保证 Live/Replay 等价。
- 更新 transcript/application projector tests，明确 transient PlanUpdate 与 durable Tool Result 的边界。

### N-05：Documentation、Guards 与 Acceptance — `DONE`

- 同步 Tool Guidance、设计文档、开发进度、测试 fixture 和 E2E 预期，只描述 transient PlanUpdate + durable Tool Call/Result 主链。
- 增加 architecture guards，禁止 `internal/agent/plan`、`PlanUpdater`、`SessionState.Plan`、plan revision/restore 和 PlanUpdate Developer Message 回归。
- 覆盖合法更新、空 plan、多个 `in_progress`、malformed payload、Plan Mode mask、Event publish failure、continuation、Rollout、Context、Live TUI 和 Resume 测试。
- 运行该子链的针对性测试，并确认 `update_plan` 旧 Plan State 主链已物理删除；N 的最终 DONE 仍需等待 N-06 至 N-14 全部完成。

### N-06：`request_user_input` Protocol + Tool Contract — `DONE`

- 建立 `RequestUserInputOption`、`RequestUserInputQuestion`、`RequestUserInputArgs`、`RequestUserInputAnswer`、`RequestUserInputResponse`、`RequestUserInputEvent` 和 `UserInputAnswerOp` typed contract；问题使用稳定 `snake_case` ID，回答按 ID 映射。
- 输入限制为 1–3 个问题、每题 2–3 个有意义选项；支持基础 `multi_select`、推荐项说明和 TUI 自动 Other/free-form，不复制 Claude Code Preview、图片、annotations、permission `updatedInput` 或 ExitPlanMode 产品逻辑。
- 新增 direct core `request_user_input` ToolDefinition，在 Default 与 Plan Mode 使用同一 Schema/ToolResult；Permission 固定 Allow、root agent only、`SupportsParallelToolCalls=false`。
- `Execute` 仅通过 `ToolUseContext.Interactions` 中的窄 `UserInputRequester` 发起请求并等待回答；不引用 Session/Application/TUI，不引入 Handler、ApprovalRequest 或 PlanState。
- CLI/headless/SDK 缺少交互 adapter 时立即返回 typed unavailable Tool Result，不永久等待 stdin、不静默选择默认答案。

### N-07：Session Interactive Waiter Lifecycle — `DONE`

- 将 ActiveTurn pending map 从 Approval-only request 扩展为 tagged typed interactive waiter，分别承载 `ApprovalRequestEvent → ApprovalDecisionOp` 与 `RequestUserInputEvent → UserInputAnswerOp`。
- 两类请求共享 Session-owned RequestID 注册、Event 发布、回答路由、取消和 shutdown cleanup 机制，但 payload、response、UI 和业务语义完全分离；用户答案不得塞入 ApprovalDecision。
- `request_user_input` Tool Future 在回答、取消、Turn abort 或 Session shutdown 前保持等待，回答后在同一 `run_turn` continuation 中形成普通 ResponseToolResult 并继续下一 Model Step。
- 未决 RequestUserInputEvent/waiter/UserInputAnswerOp 不进入 canonical Rollout；Tool Call 与最终 Tool Result 继续使用普通 ResponseItem ordered append，Resume 不恢复旧 Overlay 或 Future。
- 增加重复 RequestID、迟到回答、错误 response type、Turn cancel、Session shutdown、无 active Turn 和 Event publish failure contract tests。

### N-08：Request User Input TUI + Adapter Integration — `DONE`

- 新增独立 Request User Input Overlay/Cell，显示 Header、Question、option description、multi-select、Other/free-form 和 submit/cancel hints；不复用 Approval Dialog 或通用 permission selection。
- Event reducer 按 RequestID 管理当前问题，TUI 只组装 typed `UserInputAnswerOp`；等待期间 ActiveTurn/Working 保持活动，回答后 Overlay 关闭且 Tool continuation 继续。
- Thread switch、Turn abort、Session shutdown 和迟到 Event 必须关闭或失效旧 Overlay；live transcript 可以显示回答摘要，但不得伪造可 Replay 的 pending request。
- 为 InteractiveApplication、AmadeusThread 和非 TUI adapter 接通同一 UserInputRequester contract；不得增加第二条 Request channel 或 Application callback side channel。
- 覆盖键盘导航、单选、多选、Other、取消、窄终端、Default/Plan 两种模式和 approval/request-user-input 并存测试。

### N-09：Request User Input Prompt + Acceptance — `DONE`，Prompt text/owner 由 AA 取代

- 在 Tool Prompt 和 Collaboration Mode Prompt 中明确 Default/Plan 使用差异：Default 优先探索与合理假设，只有高风险且无法发现时才询问；Plan 在探索后优先询问会实质改变方案的意图和取舍。
- Default 与 Plan 的每个 StepContext 都从同一 ToolRouter snapshot 暴露 `request_user_input`，不以 Plan Mode 作为 visibility gate；`update_plan` 仍仅在 Default 可见。
- 增加 provider-mock E2E：模型调用 `request_user_input`、TUI/adapter 回答、同一 Turn 继续调用 Tool 或返回最终文本；验证 Tool Call/Result persistence 和 Resume context。
- 增加 architecture guards，禁止 `request_user_input` 复用 Approval payload、Question 文本 key、permission `updatedInput`、Handler、独立 Request channel 或 Plan interview state。

### N-10：Collaboration Mode Domain + Atomic Submission — `DONE`

- 统一 `ModeKindDefault/ModeKindPlan` 术语，删除 TUI `CollaborationExecute/CollaborationPlan` 第二套 enum、Session `ModeState` 和其他模式镜像；SessionConfiguration 是当前 Collaboration Mode 的唯一 owner。
- 建立最小 `CollaborationMode{Mode ModeKind}` 与 `ThreadSettingsOverrides{CollaborationMode *CollaborationMode}`；暂不复制尚未使用的 per-mode model/reasoning settings 空壳。
- 扩展 `UserInputOp` 携带 ThreadSettingsOverrides：`/plan <task>` 在单个 Submission 中先应用 Plan mode settings、再冻结 TurnContext、再启动 RegularTask；删除 `pendingModeTask` 和 settings ack 后二次提交路径。
- `/plan` 与快捷切换继续使用独立 `ThreadSettingsOp`；ActiveTurn 运行期间 settings update 必须拒绝或进入 Session submission queue，不能改变已冻结 TurnContext。
- N 阶段的 `ThreadSettingsAppliedEvent` 返回完整生效模式 snapshot；S-02 将其 contract 进一步收敛为完整生效 `SessionConfiguration`。设置校验失败形成 correlated ErrorEvent，原模式保持不变且不得启动 Turn。

### N-11：Plan Prompt + ToolRouter Policy Alignment — `DONE`，Prompt text/owner 由 AA 取代

- 将 Plan Prompt 替换为 Codex conversational 三阶段语义：Ground in environment、Intent chat、Implementation chat、decision-complete finalization；模式只能由显式 Runtime settings update 结束。
- Prompt 明确区分 Plan Mode 与 `update_plan`，优先使用 `request_user_input` 询问不可发现且会改变方案的决策，并要求最终方案使用单一 `<proposed_plan>` block。
- StepContext.ToolRouter 成为 Plan Mode Tool 可见性的唯一事实源；删除 `prepareDynamicContext` 中对 mutable Registry 的第二次 Plan Tool 过滤和按 toolNames 拼接出来的漂移路径。
- 基础版继续隐藏 edit/write/write_stdin/update_plan、有副作用 MCP 和 execute_command；后续只有在命令副作用约束可靠后才开放非修改性命令，不为表面看齐放宽安全边界。
- 不在 ToolDefinition 中重新引入 Plan Handler；如存在非模型直接调用入口，由统一 ToolExecutionPolicy/ToolRouter 拒绝不可用 Tool。

### N-12：Proposed Plan Stream + TurnItem Protocol — `DONE`

- 新增仅在 frozen Plan Mode 启用的 `ProposedPlanStreamParser`，支持 tag prefix 跨 Delta、普通 Assistant 文本与 `<proposed_plan>` block 分流、stream completion flush 和明确 malformed/duplicate/nested block failure。
- 新增 `PlanDeltaEvent{ThreadID, TurnID, ItemID, Delta}`；首次方案内容产生 `ItemStartedEvent(Plan)`，完成后产生携带完整 Markdown 的 `ItemCompletedEvent(Plan)`。
- 原始 Assistant ResponseItem 继续作为模型 continuation history ordered append；completed PlanItem 作为 TUI/Replay 业务投影持久化，不建立 Session PlanState、revision、current plan snapshot 或 incremental patch。
- PlanDeltaEvent transient 不持久化；Resume 可从 completed PlanItem 直接重建 ProposedPlanCell，不恢复 parser、delta buffer 或未闭合 block。
- 将 `PlanUpdateEvent`/UpdatedPlanCell 与 `PlanItem`/ProposedPlanCell 完全分离，增加协议、stream retry、partial delta、completion authority 和 Live/Replay tests。

### N-13：`/plan` TUI + Implementation Transition — `DONE`

- `/plan` 只提交 Plan settings update，不创建 Turn；`/plan <task>` 使用 N-10 的原子 UserInputOp；模式显示、footer/status 和 resume snapshot 统一使用 Default/Plan 术语。
- 新增 ProposedPlanCell 的 live stream 与 completed projection；PlanDelta 只更新匹配 ItemID，completed PlanItem 是最终展示权威，丢失 Delta 不截断结果。
- live Plan Turn 完成、产生 completed PlanItem、无 queued follow-up 且无其他 modal 时显示基础 `Implement this plan?` 选择：`Implement this plan` / `Stay in Plan mode`。
- Implement 提交 `UserInputOp{"Implement the plan.", CollaborationMode: Default}` 创建后续普通 Turn；Stay 只关闭 Popup。不得在 Plan Turn 内直接执行，不增加 ExitPlanMode Tool。
- implementation Popup transient，Replay/Resume 不恢复；基础版不实现 clear-context-and-implement，避免扩大 Thread/Context 产品范围。

### N-14：Plan Mode Cleanup、Guards + End-to-End Acceptance — `DONE`

- 删除 `pendingModeTask`、TUI CollaborationExecute enum、Session ModeState、两阶段 `/plan <task>`、普通 AssistantMessage 充当 Proposed Plan、PlanUpdateEvent 复用 ItemPlan 和所有旧测试 fixture。
- 增加 architecture guards，禁止 PlanTask/Planner/DAG、plan file、Enter/ExitPlanMode Tool、Session PlanState/revision、Handler、重复 Tool mask、mode mirror 和 implementation callback side channel 回归。
- 覆盖 `/plan` 持续模式、用户要求执行仍只规划、探索后 request_user_input、完整/修订 proposed plan、mode-switch ordering、settings failure、ActiveTurn switch、stream retry、Turn abort、Resume 和 Implement transition。
- 运行针对性测试、全量测试、race、vet、build、`make check`、architecture grep、`git diff --check`、request-user-input provider-mock E2E 和 Plan Mode provider-mock E2E 后，N 才能标记 DONE。

### N 出口

- `update_plan` 没有 Handler、PlanUpdater、Session PlanState、revision、durable PlanUpdate 或 Resume restore。
- `PlanUpdateEvent` 是 scoped transient Event；模型历史只通过普通 Tool Call 参数和 Tool Result `Plan updated` 延续。
- `update_plan` 不生成普通 Tool activity，TUI 只显示一个 live `Updated Plan` cell，Replay 不恢复旧 checklist。
- `request_user_input` 在 Default/Plan 通用，使用独立 RequestUserInputEvent/UserInputAnswerOp、Session typed waiter 和同 Turn continuation，不复用 Approval 或 Handler。
- Collaboration Mode 只有 SessionConfiguration 一个 owner；`/plan <task>` 原子应用 mode override，TUI 不保存 pending task 或第二套模式 enum。
- Plan Mode 继续隐藏 `update_plan`，计划不驱动 Tool 调度、Task、DAG 或 completion 判定；最终方案使用 `<proposed_plan>`、PlanDeltaEvent 和 completed PlanItem。
- Proposed Plan 与 Updated Plan 是不同协议和 UI；Replay 恢复 completed Proposed Plan，不恢复 checklist、request overlay、parser buffer 或 implementation Popup。
- Implement transition 通过携带 Default mode override 的后续普通 UserInputOp 完成，不在 Plan Turn 中实施，不存在 Enter/ExitPlanMode Tool。
- 生产代码、文档和测试不存在被删除 Plan State 主链的兼容 wrapper、fallback 或第二事实源。

### N 验收

- 2026-08-20 完成 `update_plan` transient Event 收敛、`request_user_input` 独立交互链、Collaboration Mode 原子提交、Proposed Plan stream/TurnItem、TUI implementation transition 与 legacy cleanup。
- `make check`（含 vet、全量测试和 build）、`go test -race ./... -count=1`、architecture guards、`git diff --check` 均通过。

## 17. O. Same-Turn User Input + Turn Steer Lifecycle Alignment — `DONE`

### 目标

- 对齐 Codex 的 user message admission、turn steer、TurnInputQueue 与 same-Turn continuation lifecycle。
- 运行中的普通用户消息进入当前 Regular Turn，不再静默排队为后续新 Turn。
- 保持基础版范围：文字输入、Regular/Compact TaskKind、typed admission、canonical UserMessage 和 Rich TUI 投影；不复制 mailbox、多模态和复杂远程协议。

### O-01：User Message Admission + Protocol Contract — `DONE`

- 新增 `UserMessageAdmission{Started, Steered}`、`ClientUserMessageID` 与 typed `SteerInputError`；admission 是 submission request/response，不进入 EventMsg 或 Rollout。
- 为 `AmadeusThread` 增加 `SubmitUserInputAndWaitForAdmission`，按 SubmissionID 注册一次性 waiter，并保留普通 `Submit` 的 fire-and-accept-to-channel 语义。
- 增加 strict `SteerInput(ExpectedTurnID, input)` Core API，覆盖 NoActiveTurn、ExpectedTurnMismatch、ActiveTurnNotSteerable 和 EmptyInput。
- 增加协议校验、并发 waiter、取消、Session shutdown 和错误传播测试。

### O-02：TaskKind + TurnState + InputQueue Ownership — `DONE`

- 用 `TaskKindRegular/TaskKindCompact` 取代 `compact bool` 身份判断，RunningTask 持有明确 TaskKind，基础版只有 Regular 可 steer。
- 将 ActiveTurn 的 interactive waiter 迁入 `TurnState`，新增 Turn-scoped `TurnInputQueue` 和 Session-scoped `InputQueue` coordinator。
- `TurnInputQueue` 提供 FIFO enqueue/has/drain/seal 原子协议，解决 Session Submission 与 RunningTask Completion 同时 ready 的尾部竞态。
- 保留 Session deferred operation queue，但禁止 ActiveTurn 期间的 `UserInputOp` 进入该 queue；删除旧忙时用户输入排队语义和对应测试。

### O-03：Session Admission + Steer Routing — `DONE`

- Session Loop 对 `UserInputOp` 先校验并原子应用 ThreadSettingsOverrides，再尝试 steer；成功返回 Steered，无 ActiveTurn 时启动 RegularTask 并返回 Started。
- 当前 TurnContext 在 steer 时保持冻结；更新后的 SessionConfiguration 只影响后续 Turn，不建立 TUI mode shadow state。
- CompactTask 明确返回 ActiveTurnNotSteerable，Core 不静默改成下一 Turn；rejected-steer queue 如有需要只属于 Interface/Application。
- admission completion、Turn start failure、interrupt 和 terminal cleanup 使用唯一 Session owner，所有 pending admission 在 shutdown 时释放。

### O-04：Same-Turn Pending Input Continuation — `DONE`

- 在 `run_turn` 增加 pending input inspection，将 `modelNeedsFollowUp || hasPendingInput` 作为 continuation 条件。
- 初始 sampling 前禁止 drain，保证原始 UserInput 先进入第一请求；后续按 FIFO drain steered input。
- drain 时通过 Session canonical append 写入当前 Turn 的 ResponseUserMessage 与 completed UserMessage TurnItem，再 capture 新 StepContext 并继续同一 Turn。
- Final Response、Tool Calls、Tool failure 和多个连续 steer 均不产生第二个 TurnStartedEvent 或 Turn terminal。
- RunningTask 返回前执行 queue seal/completion handshake；已返回 Steered 的输入在 interrupt/terminal race 中不得静默丢失。

### O-05：Compaction + Input-dependent Context Refresh — `DONE`

- 引入 `canDrainPendingInput` 或等价状态，确保 steer 不进入正在执行的 compact request，也不抢在既有 model/tool continuation 前面。
- 覆盖 auto-compaction 后需要恢复原 continuation、只有 steer 需要 follow-up、Tool Result 触发 compact 三种顺序。
- 将 Turn preparation 拆分为 static Turn context 与 input-dependent context；初始输入和每批 steer 都刷新 explicit Skill/required MCP 相关上下文。
- StepContext 必须在 pending input canonical record 和输入相关 context refresh 后重新 capture，Prompt/ToolRouter 继续来自同一 snapshot。

### O-06：Canonical UserMessage + Client Identity — `DONE`

- 初始输入和 steered input 统一使用 ResponseUserMessage + completed UserMessage TurnItem 的 canonical/live lifecycle。
- UserMessage TurnItem 保留 ClientUserMessageID，用于 caller correlation、optimistic projection 确认和去重；未知 client ID 仍可正常 live/replay。
- ContextManager、LiveThread rollout 和 replay projector 继续作为唯一历史源，不在 InputQueue、TUI 或 ModelClientSession 保存第二份已消费输入历史。
- 增加同一 Turn 多 UserMessage、live/replay 等价、durability ordering 和 duplicate client ID contract tests。

### O-07：Application + Rich TUI Steer UX — `DONE`

- InteractiveApplication 提交用户消息时等待 Started/Steered admission，不再只依据本地 phase/running 推断 Runtime 接纳结果。
- Started 继续由 TurnStartedEvent 初始化新 Turn；Steered 保持 elapsed timer、Working/activity state、details store 和 active items，不重置 Turn UI。
- TUI 使用 ClientUserMessageID optimistic insert/confirm/dedupe；Runtime UserMessage Item 是 live 与 replay 的最终权威。
- ActiveTurnNotSteerable 等 rejection 恢复或保留 composer 内容并展示 typed error；不得显示已提交后在 Core 静默排队。
- 覆盖运行中普通 Enter、slash command availability、Approval/request_user_input overlay 隔离和同 Turn Worked separator 行为。

### O-08：Legacy Cleanup、Guards + End-to-End Acceptance — `DONE`

- 删除 ActiveTurn 时 UserInput append 到 Session queue、`compact bool` task identity、TUI 每次 Enter 重置 Turn 状态和 initial/live UserMessage 双来源旧链。
- 增加 architecture guards，禁止 `SteerOp`、steer 复用 UserInputAnswerOp/ApprovalDecisionOp、InputQueue 第二历史源和 TUI-only active Turn truth。
- 建立 provider mock E2E：首轮 stream 中 steer、Tool 执行中 steer、Final 边界 steer、连续 steer、compact 顺序、interrupt 和 explicit expected TurnID race。
- 运行 targeted tests、`make check`、`go test -race ./... -count=1`、`git diff --check` 和架构扫描；仅在代码、文档、测试与旧链删除全部完成后标记 O 为 DONE。

### O 出口

- 普通用户输入在无 ActiveTurn 时返回 Started，在 Active Regular Turn 时返回 Steered，caller 获得准确 TurnID。
- Steered input 在当前 Turn 的下一次允许 continuation 中进入模型，多个输入 FIFO，且一个逻辑 Turn 只有一组 TurnStarted/terminal lifecycle。
- Compact、completion、interrupt、Approval、request_user_input 和 compaction 边界不存在输入误路由、静默丢失或第二历史源。
- Rich TUI 与 replay 对初始/steered UserMessage 生成一致历史，运行中补充输入不重置 Turn timer 或制造第二个 Worked boundary。
- 生产代码中不存在忙时 UserInput deferred-new-Turn 旧行为、`SteerOp` 或其他与 `docs/design.md` 冲突的兼容路径。

### O 验收

- 2026-08-20 完成 typed user message admission、strict steer、TaskKind/TurnState/InputQueue ownership、same-Turn continuation、三类 compaction ordering、canonical UserMessage/client identity、Rich TUI optimistic confirmation 与 legacy cleanup。
- provider-mock E2E 覆盖首轮 stream 中连续 steer、首请求隔离、FIFO、单一 Turn lifecycle 与 interrupt residual input；Session tests 覆盖 expected TurnID、Compact rejection、queue seal、admission waiter/cancel/shutdown 和 compaction continuation 顺序。
- `make check`（含 vet、全量测试和 build）、`go test -race ./... -count=1`、architecture guards 与 `git diff --check` 均通过。

## 18. P. Model Reasoning Effort + Provider Thinking Contract — `DONE`

### 目标

- 增加 Codex 风格顶层 `model_reasoning_effort`，并确保它真实进入普通 sampling 与 Compaction wire request。
- 删除通用 reasoning enabled/preserve 假抽象；省略 effort 使用 Provider 默认，`none` 承担显式关闭语义。
- 将 `standard` 定义为 OpenAI-compatible opt-in passthrough，将 OpenAI 与 DeepSeek 的官方 effort contract 纳入 Dialect 契约测试。
- 保持基础版范围：不增加 `/effort`、Plan-specific effort、Model Catalog、数字 effort、Codex `ultra` 或任意 custom effort。

### P-01：Reasoning Domain + Config Contract — `DONE`

- 定义稳定 `ReasoningEffort`：`none/minimal/low/medium/high/xhigh/max`，并将通用 `ReasoningConfig` 收敛为仅持有 `Effort`。
- 增加顶层 `model_reasoning_effort`、patch/merge、validation、clone 与 Config v2 schema；省略值保持 unset，不注入客户端默认 effort。
- 删除生产 Domain 中通用 `Enabled/Preserve` 及其冲突语义；reasoning history 和 thinking cleanup 改由 Dialect 自动维护。
- 增加 config load/default/invalid value 测试，并更新 Architecture Guard 的 Model runtime field ownership。

### P-02：Turn Freeze + Rollout + Request Propagation — `DONE`

- 将 effective effort 从 Session Configuration 冻结到 TurnContext，并投影到 TurnContextItem；未配置时不记录伪造的 Provider 默认值。
- 普通 continuation 的每次 SampleRequest 复用冻结值，不从可变 Config 重新读取。
- 手动与自动 Compaction 使用相同 effort，ModelClientSession 将其统一映射到 `llm.Request.Reasoning.Effort`。
- 覆盖 same-Turn continuation、Rollout codec/replay、Resume diagnostic 和 sampling/compaction 一致性测试。

### P-03：Standard + OpenAI Wire Mapping — `DONE`

- 对 `standard` 与 `openai`，Responses 请求在显式配置时序列化 `reasoning.effort`，Chat Completions 序列化 `reasoning_effort`；未配置时两个字段均省略。该规则是 OpenAI-compatible 主线，不绕过其他已知 Dialect 的官方契约。
- `standard` 采用用户显式 opt-in passthrough，不用布尔 capability 将 unknown 错判为 unsupported；上游 Provider 4xx 保留原始分类与脱敏诊断。
- `openai` 使用 SDK typed field，并覆盖 `none/minimal/low/medium/high/xhigh/max` 的精确 JSON fixture。
- 不根据 Base URL 或模型名猜测 effort 支持，不在 Adapter 内补写默认等级。

### P-04：DeepSeek Current Contract Alignment — `DONE`

- 更新 DeepSeek Dialect 的 Chat Completions `reasoning_effort` 映射和 `none → thinking.type=disabled` 语义；省略时依赖官方默认 thinking/effort。
- 在官方 Responses contract 通过 fixture 后将 DeepSeek 从 Chat-only 扩展为 Chat + Responses，并复用 `reasoning.effort` 主链。
- 保持 `reasoning_content` tool-call continuation history，禁止同时发送多套 `thinking`/`enable_thinking`/`chat_template_kwargs` 猜测字段。
- 增加 DeepSeek low/high/max、兼容等级、unset、none、history replay、tool call 和 usage contract tests。

### P-05：Qwen + GLM Current Effort Contracts — `DONE`

- 未配置 effort 时不发送 enable/disable 字段，使用 Provider 默认 thinking 行为。
- Qwen Dialect 从 Chat-only 扩展为 Responses + Chat Completions：Responses 发送 `reasoning.effort`；Chat 的非 `none` 等级发送 `reasoning_effort`，`none` 映射为 `enable_thinking=false`。
- GLM Chat Completions 的非 `none` 等级发送 `reasoning_effort`，`none` 映射为 `thinking.type=disabled`；不把非 `none` 等级降级为简单 `thinking.type=enabled`。
- 不根据模型名维护客户端 support matrix；具体模型不支持某等级时保留上游 4xx，不静默忽略、改写或降级。
- reasoning history、旧 thinking toggle 与 GLM clear-thinking 行为保持 Dialect-owned，不重新引入用户可配置 `Preserve`。

### P-06：User-facing Config Surfaces — `DONE`

- 更新 provenance、`config check`、`config show/explain`、example config、README 和错误文案，明确 unset、`none` 与 Provider default 的区别。
- 增加 `AMADEUS_MODEL_REASONING_EFFORT` 与 `--model-reasoning-effort` 进程级 override，遵循 file < environment < CLI 的既有优先级。
- `/status` 只展示当前 Session/Turn 已冻结的 effective configured effort，不把未知 Provider 默认值显示为 `high` 或 `medium`。
- 不实现 `/effort` slash command、TUI picker 或 per-mode mutation。

### P-07：Guards + End-to-End Acceptance — `DONE`

- 增加 provider-mock E2E，分别验证 Responses、Chat、standard passthrough、OpenAI typed mapping、DeepSeek mapping、unsupported Dialect 和 Compaction。
- 增加 architecture guards，禁止 ProviderInfo 持有 effort、通用 `ReasoningConfig.Enabled/Preserve` 回流、普通请求伪造默认 effort、Qwen 未声明 Responses 支持，以及 Qwen/GLM 非 `none` effort 被拒绝或降级为简单 thinking enable。
- 运行 targeted tests、`make check`、`go test -race ./... -count=1`、`git diff --check` 和架构扫描；仅在代码、测试、文档和旧抽象删除全部完成后标记 P 为 DONE。

### P 出口

- `model_reasoning_effort` 从 Config、TurnContext、Rollout、sampling 与 Compaction 到 wire request 具有单一可验证主链；Wire API 选择候选字段形状，Dialect 再确认、覆写或拒绝。
- unset、`none` 和显式等级语义稳定；不存在通用 enabled/preserve 冲突配置或客户端伪造模型默认值。
- Standard/OpenAI/DeepSeek/Qwen/GLM 使用各自官方 Responses/Chat effort 字段；DeepSeek/Qwen/GLM 的 Chat `none` 使用厂商关闭字段，Qwen Dialect 明确支持 Responses；客户端不猜测模型级 supported levels，上游兼容性错误保持可见。
- 当前 Turn 的 effort 可诊断、可回放、不可被 same-Turn 配置变化污染，Provider 错误保持准确分类。

### 完成记录

- 2026-08-21 完成 `ReasoningEffort` 领域枚举、顶层 Config/Environment/CLI/provenance/validation/clone 主链，并删除生产 `ReasoningConfig.Enabled/Preserve`。
- Turn 启动冻结 effort 到 `TurnContext`/`TurnContextItem`，普通 continuation、手动 Compaction、自动 Compaction 与 retry 均复用同一 `llm.Request.Reasoning.Effort`。
- Responses 使用 typed `reasoning.effort`，Chat 使用 typed `reasoning_effort`；DeepSeek/Qwen 声明 Responses 支持，DeepSeek/Qwen/GLM 的 Chat `none` 使用各自官方关闭字段。
- `config show/explain`、`AMADEUS_MODEL_REASONING_EFFORT`、`--model-reasoning-effort`、example config、README 与 `/status` 已接通；unset 明确显示为 Provider default。
- config/domain/Turn/Rollout/Compaction/Dialect/Adapter provider-mock 与 Architecture Guard 覆盖完成；`make check`、`go test -race ./... -count=1` 和 `git diff --check` 通过。

## 19. Q. Web + View Image Tool Contract Closure — `DONE`

### 目标

- 保留独立 `web_fetch`，将其收敛为搜索摘要不足或精确 URL 核验时使用的有界全文读取 Tool；不把 Tavily search snippet 误标为 full-page evidence。
- 修复 URL 校验、Hostname Approval、跨域重定向和 DNS rebinding 边界，使获准 Hostname 不会隐式扩大到其他网络目标。
- 将正则剥标签输出替换为 bounded readable Markdown，并建立稳定 Content-Type、Partial、metadata 和 typed failure contract。
- 保持基础版范围：不增加自动 Top-N fetch、Browser rendering、Tavily Extract adapter、二进制持久化、secondary-model summarization 或持久化 Web Cache。
- 将 `view_image` 从 Provider/Dialect 粗粒度图片开关收敛为 model-aware Tool，并建立独立 image preparation、上下文预算、单份 Rollout 持久化和专用 TUI projection。
- 保留现有 canonical read-directory Approval、source size/dimension 安全上限和动态 GIF 拒绝；不复制 Codex Remote/Multi Environment、Analytics、图片生成或终端像素预览。

### Q-01：URL、配置与 Permission Contract — `DONE`

- 在 Approval 前完成 absolute `http/https`、Hostname 和 userinfo 校验，非法输入不进入 Approval/Execute。
- 修正 `max_redirects: 0` 语义，使其稳定表示拒绝重定向，并保持 Config default、validation、example config 和 Fetcher options 一致。
- 明确 `{url}` 基础 Schema、Hostname Session Grant、untrusted result 和搜索摘要不足时才抓取的 Tool Guidance。

### Q-02：Pinned Transport + Redirect Approval Boundary — `DONE`

- 将 DNS 安全检查与实际 Dial 收敛为同一 pinned public IP 主链，覆盖 IPv4/IPv6、DNS rebinding、private/link-local/loopback 和重定向目标。
- 受控跟随同 Hostname 重定向；跨 Hostname 返回 typed redirect result，不发起目标请求，并由下一次 `web_fetch` 触发独立 Approval。
- 保持 proxy、timeout、cancel、连接错误和 HTTP status 分类可见，不允许 Session Domain Grant 绕过确定性 SSRF 拒绝。

### Q-03：Readable Markdown + Content-Type Contract — `DONE`

- 使用 DOM-based 提取替换正则剥标签，保留标题、段落、列表、链接和代码块的基础 Markdown 结构，并消除标题重复。
- 只接受设计允许的文本 Content-Type，正确处理 charset；二进制或不支持内容返回 typed unsupported result。
- 保持最大字节限制和 `Partial` 语义，确保截断、空正文、畸形 HTML 和 JSON/text 输入均生成有界可用结果。

### Q-04：ToolResult、Guidance + Interface Projection — `DONE`

- 统一 success/failure/redirect ToolResult 的 final URL、Content-Type、Title、Markdown、Partial 和稳定 error metadata。
- 更新 Runtime Tool guidance，使模型优先使用足够的 `web_search` evidence，仅在全文核验必要时调用 `web_fetch`，失败后不进行等价搜索或无限重试。
- 保持 live TUI、Rollout 和 replay 使用既有 Tool lifecycle；Web Fetch 不新增第二 Event、Context owner 或自动 pipeline。

### Q-05：Tests、Guards + Acceptance — `DONE`

- 覆盖 Approval 前拒绝、Hostname Session Grant、同域/跨域重定向、redirect limit、DNS pinning、SSRF、timeout、cancel、size limit 和 Content-Type matrix。
- 增加 HTML/Markdown/text/JSON extraction fixtures、标题去重、链接/代码块保留、Partial 和 untrusted-content 测试。
- 更新 `docs/design.md`、example config、用户可见说明和 architecture guards；运行 targeted tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check` 后才可标记 Q 为 DONE。

### Web 子阶段出口（Q-01～Q-05 已完成）

- `web_search` 与 `web_fetch` 证据层级清晰：前者提供 search-summary evidence，后者只在必要时提供 full-page evidence。
- 任意实际网络目标都经过准确 URL 校验、Hostname Approval、redirect policy、SSRF 校验和 pinned Dial，不存在跨域授权扩大或二次 DNS 解析窗口。
- 模型获得有界、结构化、可诊断且明确不可信的页面内容；基础主链不依赖 Browser、Tavily Extract、额外模型调用或自动抓取流水线。

### Web 子阶段完成记录

- 2026-08-21 完成 Approval 前 URL/Hostname/userinfo 校验、精确 Hostname Session Grant、`max_redirects: 0` 配置语义与跨 Hostname typed redirect boundary。
- Fetcher 使用已验证 public IP 的 pinned Dial，保留原 Host/TLS SNI，并覆盖同域重定向重新解析、DNS rebinding、IPv4/IPv6、restricted/mixed DNS answer、HTTP/HTTPS proxy、timeout、cancel 和 HTTP status 分类。
- 删除正则剥标签与 legacy `Document.Text`/`Options.HTTPClient` bypass，按 URL、transport、proxy、content、Markdown 和 typed error 职责拆分 `internal/webfetch`；HTML、Markdown、text 与 JSON 统一投影为有界 readable Markdown。
- `web_search` 明确输出 search-summary evidence，`web_fetch` 明确输出 untrusted full-page evidence；Rich TUI 分离 Search/Fetch projection，MCP network Tool 回归 generic projection，README 与 example config 已补齐 DuckDuckGo、Approval、重定向和开关说明。
- targeted Web/Tool/Config/TUI/Architecture/CLI tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check` 全部通过。

### View Image 延伸目标

- 保留独立 `view_image` 与统一 ToolExecutionService，不重新并入文本 `read`，不新增第二套 Image Event/Context owner。
- 默认向模型发送有界 prepared image，而不是将最大 20 MiB 原始文件直接 Base64；图片成本进入 Context/Compaction 预算。
- Canonical ResponseToolResult 只保存一份图片 payload，完成事件和 TUI 只保存 display-safe metadata。

### Q-06：Model Capability + Tool Schema Contract — `DONE`

- 以冻结 `ModelInfo.InputModalities` 作为 `view_image` 可见性事实源，拆分 transport SupportsImages 与具体模型 image-input capability，并在 Execute/Adapter 保留防御性检查。
- 将 Schema 收敛为 `{path, detail?}`；默认 `high`，仅在模型明确支持 original image detail 时暴露 `original`，当前不增加 `environment_id`。
- 保持工作目录内 Allow、外部 canonical read-directory Ask/Session Grant，并在真实打开前重新验证路径、文件类型和客观文件系统规则。

### Q-07：Image Preparation Boundary — `DONE`

- 新建职责独立的 image preparation package，Tool 文件只负责 Validate/Prepare/Permission/Execute orchestration；删除原始字节直接 Base64 的生产主链。
- 保留 20 MiB、16384 单边和 6400 万像素 source safety limit；`high` 使用 2048/2500 patch 等价预算，`original` 使用 6000/10000 patch 等价预算并保持纵横比。
- 支持内容检测的 PNG/JPEG/WebP/静态 GIF；静态 GIF 规范化为 PNG，动态 GIF 继续拒绝，缩放/规范化后返回 typed PreparedImage metadata。

### Q-08：Provider + Context Image Budget Contract — `DONE`

- ToolResult 只返回一个 prepared image Part 和简短文本，metadata 包含 source/prepared dimensions、MIME、bytes 与 effective detail；Base64 不进入 Text/Data 摘要。
- Responses 使用 FunctionCallOutput image content；Chat 的 synthetic user image 仅作为 Adapter 兼容投影，并保持原 Tool Call 关联和 canonical result 不变。
- Context Manager、模型切换与 Compaction 对图片执行 modality/budget projection，超限或不支持时使用稳定 omission marker，不再把图片视为零成本。

### Q-09：Single-Payload Rollout + ViewImageCell — `DONE`

- Canonical `ResponseToolResult.Parts` 保存唯一 prepared image payload；`ItemCompletedEvent.ToolResult`、Event projection 和 TUI snapshot 必须移除 `ContentPart.Data`，只保留展示 metadata。
- Rich TUI 增加基于现有 Tool lifecycle 的 `ViewImageCell`，显示 Viewing/Viewed、路径、source→prepared dimensions、MIME、失败与 Approval 状态；live/Resume 使用同一 metadata。
- 不新增 Codex `ImageView` 专用 Event、Remote Environment、图片像素渲染、IDE Preview、Analytics 或跨 Session 图片缓存。

### Q-10：Tests、Guards + Acceptance — `DONE`

- 覆盖 text-only/image-capable model visibility、Execute defense、external read Approval、PNG/JPEG/WebP/static/animated GIF、伪装格式、大小/尺寸/像素限制与 cancellation。
- 增加 high/original resize、prepared request、Responses/Chat projection、image budget/omission、Compaction、单份 Rollout payload、Resume 与 ViewImageCell snapshot 测试。
- 增加 architecture guards，禁止 Dialect blanket image capability 成为模型事实源、原始字节直传、完成事件重复 Base64 和图片处理重新堆回 Tool 文件；运行 targeted tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check` 后才可标记 Q 为 DONE。

### Q 总出口

- Web Search/Fetch 子阶段成果保持不回退；`view_image` 的可见性、图片准备、Provider projection、Context budget、Rollout 和 TUI 形成单一可验证主链。
- 任意图片只在 canonical model result 中保存一份有界 prepared payload；展示事件不携带 Base64，Resume 与 Compaction 不依赖重新读取可能变化的源文件。
- Amadeus 保留基础 Agent 范围，不依赖 Remote Environment、图片生成、终端图像协议、Analytics 或持久化图片资产服务。

### 完成记录

- 2026-08-21 完成顶层 `model_input_modalities` / `model_supports_original_image_detail` 配置、CLI/Environment/provenance/validation/example/README 主链；Session 将能力冻结进 ModelInfo，`view_image` 使用 `model.image_input` 条件可见性，不再从 Dialect `SupportsImages` 推断具体模型能力。
- `view_image` Schema 已收敛为 `{path, detail?}`，默认 `high`；仅 original-capable ModelInfo 暴露 `original`。工作目录外继续使用 canonical read-directory Approval/Session Grant，Execute 在打开前重新解析并验证真实文件目标。
- 新增独立 `internal/imageprep`，实现 20 MiB/16384/6400 万 source safety limit、high 2048/2500 patch、original 6000/10000 patch、内容格式检测、PNG/JPEG/WebP、静态 GIF→PNG、动态 GIF拒绝与有界缩放/重编码。
- ToolResult、Responses FunctionCallOutput、Chat synthetic user projection、Context image budget、模型切换和 Compaction 已贯通 effective detail 与 prepared metadata；不支持或超预算图片使用稳定 omission marker。
- Canonical `ResponseToolResult.Parts` 保存唯一 Base64 payload，Response Result 与 completed Event/TUI 使用 display-safe ToolResult；Rich TUI 增加 `ViewImageCell`，Resume 从 canonical payload 恢复模型上下文且不重新读取源文件。
- PNG/JPEG/WebP/static/animated GIF、伪装格式、大小/尺寸/像素、cancellation、external Approval、动态 Schema、Provider defense、image budget、Compaction、single-payload Rollout、Resume、TUI 与 architecture guard 覆盖完成；`make check`、`go test ./... -count=1`、`go test -race ./... -count=1` 和 `git diff --check` 通过。

## 20. R. Basic Multi-Agent Architecture Alignment — `DONE`，terminal/final-message/wait/close-persistence 结论由 AC 取代

### 目标

按 `docs/design.md` 的 Basic Multi-Agent Contract，实现 Codex V1 风格的最小 Multi-Agent 闭环：SubAgent 是完整 AmadeusThread/Session，同一 Root tree 共享 AgentControl，Root 通过 `spawn_agent`、`send_input`、`wait_agent` 和 `close_agent` 管理 child；child 首版固定为 read-only explorer，并贯通 Prompt、WorldState、completion notification、typed Event/Rollout 和 Codex 风格 TUI projection。

R 不扩展 Codex Multi-Agent V2、AgentPath/mailbox/residency、Claude Code team/worktree/remote/background task、history fork、自定义 agent definition、write-capable worker 或 child interactive Approval。实现期间不得以通用 Task Bus、nested SessionTask、Tool handler 直调 `run_turn` 或字符串 UI wrapper 保留第二套 runtime。R 建立的Thread/Session/AgentControl/Tool隔离骨架继续有效；其“最近completed AssistantMessage等于FinalMessage”、wait-all、Shutdown重复notification和无durable close edge等结论由AC替换。

### R-01：Protocol Identity + SessionSource Contract — `DONE`，terminal projection 由 AC 取代

- [x] 建立 `SessionSource`、`RootSessionSource`、`SubAgentSessionSource`、`AgentMetadata` 和 Codex 语义的 `AgentStatus`；首版 AgentID 直接使用 child ThreadID。
- [x] 将 parent ThreadID、depth、nickname 和 role 冻结进 child Session/SessionMetaItem；role 固定为 `explorer`，最大 depth 固定为配置值 1。
- [x] 定义 AgentStatus 从 TurnStarted/TurnComplete/TurnAborted/Error/ShutdownComplete 的唯一 reducer；不得新增公开 Session status channel。
- [x] 更新当前 schema、fixtures 和 codec；Amadeus 未发布，不保留旧 SessionMeta reader、alias 或 migration。

### R-02：Root-scoped AgentControl + Reservation — `DONE`

- [x] 新增 root-tree scoped `AgentControl`、`AgentRecord`、status snapshot/subscription 和 `AgentHost` 窄接口；ThreadManager 继续是 live Thread 的唯一 registry。
- [x] 实现 open-agent slot、nickname reservation、commit/rollback 和并发安全；spawn 失败、取消、panic 或 child 创建中断不得泄漏计数和预留状态。
- [x] 实现默认 `max_agents=4`、`max_depth=1` 与 child budget 配置验证；agent tree 容量不得复用 `max_parallel_tools`。
- [x] 禁止进程级 singleton、TUI/Application agent map、generic event bus 和 capability facade。

### R-03：ThreadManager Child Spawn + Event Ownership — `DONE`，final-message reducer 由 AC 取代

- [x] ThreadManager 创建 Root Thread 时创建 AgentControl；spawn child 时向 child SessionServices 传递同一个 control，并写入 SubAgentSessionSource。
- [x] child 必须走正常 LiveThread、Session::Spawn、RegularTask 和 `run_turn` 主链；不得新增 provider-only runner 或父 Session nested task。
- [x] 为每个 child 建立 owner 明确的 Event consumer，持续 drain SessionIo.Events、派生 AgentStatus、提取最近 completed AssistantMessage，并在 Terminated 时完成清理。
- [x] Root shutdown 按固定顺序停止 spawn、关闭全部 child、等待 Terminated，再关闭 Root；Root 当前 Turn Interrupt 不自动关闭 child。

### R-04：Child Prompt、Context + Tool Isolation — `DONE`

- [x] 新增版本化 `SubagentDeveloperInstructions` Prompt 资产，通过 ModelMessages/SessionSource 选择，不在 Tool handler 或 TUI 拼接身份文本。
- [x] child fresh context 只包含 BaseInstructions、SubagentDeveloperInstructions、WorldState 和 delegated UserInput；不复制父 reasoning、Tool history、Plan、pending steer 或 compaction history。
- [x] 以 StepContext/ToolRouter exact snapshot 实施 child allowlist：`read`、`glob`、`grep`、条件 `read_skill`、条件 `web_search`；隐藏 edit/write/command/process/input/MCP/multi-agent Tool。
- [x] child 使用独立 SessionPermissionContext/FileReadState/ToolExecutionService，不复制 Root Session grant；任何需要 Approval 的调用必须稳定失败为 ToolResult，不能产生无人处理的 request waiter。
- [x] child 使用同一 CWD/workspace/filesystem 并可观察并发变化；Prompt 明确只读、单一任务、无父 conversation、证据化且简洁的最终报告。

### R-05：Codex V1 Collaboration Tools — `DONE`，wait/close lifecycle 由 AC 取代

- [x] 注册仅 Root 可见的 `spawn_agent`、`send_input`、`wait_agent`、`close_agent` ToolDefinition；schema、命名、返回字段和错误语义以 design Contract 为准。
- [x] `spawn_agent` 完成 reservation、child spawn、初始 UserInput admission 后立即返回 agent_id/nickname；允许同一模型响应中的独立 spawn 有界并行。
- [x] `send_input` 正确处理 Running steer、`interrupt=true` 的 interrupt-and-restart，以及 Completed/Errored/Interrupted child 的新 Turn。
- [x] `wait_agent` 使用 status change notification 并发等待多个 ID，支持 timeout snapshot 和 Completed FinalMessage，不轮询 map。
- [x] `close_agent` 返回 previous status，关闭目标及 open descendants、释放 slot/nickname；首版不注册 resume/list/send_message/followup_task/interrupt_agent。

### R-06：WorldState + Completion Notification — `DONE`，terminal payload/delivery 由 AC 取代

- [x] Root Environment WorldState 增加 Codex 风格 `<subagents>`，由 AgentControl snapshot 和 revision 驱动，列出未 close child 的 ID、nickname、role 和 status。
- [x] child 每个 Turn 进入 Completed/Errored/Shutdown 后，向直接 parent 注入一次 `<subagent_notification>` contextual user fragment；不得投影为普通 UserMessage。
- [x] notification 通过 parent Session canonical append/context owner 落盘，并对 final/error payload 应用稳定长度限制；wait_agent 不得重复注入同一终态。
- [x] `spawn_agent` Tool description 写入 Codex 风格 delegation guidance：先识别 critical path、只委派独立 side task、避免重复工作、并行 spawn 独立探索、等待时继续本地工作，并明确首版 child 只读。

### R-07：CollabAgent TurnItem + Rollout Lifecycle — `DONE`

- [x] 新增 `CollabAgentTool`、`CollabAgentToolCallStatus`、`CollabAgentRef`、`CollabAgentState` 和 `CollabAgentToolCallItem` typed protocol。
- [x] spawn/send/wait/close 统一发布 ItemStarted/ItemCompleted，completed item 进入 EventMsgItem/canonical Rollout；live TUI 与 Resume 使用同一 projector。
- [x] Tool Provider 协议继续使用标准 ToolCall/ToolResult；Tool event policy 对 collaboration Tool 抑制 generic ToolHistoryCell，避免双重展示。
- [x] Root rollout 只记录 collaboration item 和 bounded notification，不复制 child transcript、Tool delta 或 reasoning。

### R-08：Codex-style Multi-Agent TUI — `DONE`，blocked/LastTurn projection 由 AC 取代

- [x] 新增 `CollabAgentHistoryCell` 与 active wait projection，使用 `Spawned`、`Sent input to`、`Waiting for`、`Finished waiting`、`Closed` 等 Codex 风格标题。
- [x] nickname/role、prompt preview、AgentStatus、FinalMessage/error preview 使用专用 typed fields 和稳定截断；不得解析 ToolResult 文本恢复状态。
- [x] wait in-progress 使用 ActiveHistoryCell，completed 后正确结束 spinner；completion notification 不显示为用户气泡，必要时投影为轻量 AgentStatusHistoryCell。
- [x] live TUI 与 Resume snapshot 覆盖同一视觉语义；R 不实现完整 `/agent` picker、Alt+Left/Right navigation 或 child transcript attach。

### R-09：Persistence、Listing + Shutdown Boundary — `DONE`，Root restore 由 T 引入，open/closed edge 由 AC 取代

- [x] JSONL/SQLite metadata 保存 SessionSource 与 parent Thread 信息；默认顶层 session list 和 `/resume` picker 排除 SubAgent Thread。
- [x] R 当时只恢复 canonical collaboration item/notification，不恢复旧 AgentControl tree；T 后续引入 persisted/unloaded child restore，AC 负责补齐只恢复 open edge、显式 close durable 和 live/Resume terminal 等价。
- [x] child writer、event consumer、completion watcher、status waiter 和 AgentControl close 都有明确 owner、context 和有限 cleanup timeout。
- [x] close/shutdown 后立即从 live Thread registry 移除 child，防止 terminated runtime 被错误复用；内部历史仍可供 diagnostics 读取。

### R-10：Tests、Guards + End-to-End Acceptance — `DONE`

- [x] 覆盖 spawn success/failure rollback、并发 slot、nickname 唯一性、depth/limit、child budget、状态 reducer、wait timeout 和 close previous status。
- [x] 覆盖 child fresh context、Prompt asset、Tool allowlist、Root grant 不继承、Approval 不等待、same-workspace 可见性和禁止 nested spawn。
- [x] 覆盖 send_input steer/interrupt/new Turn、completion notification 去重/截断、WorldState revision、Root interrupt 与 shutdown cancellation tree。
- [x] 覆盖 CollabAgentToolCallItem live/Resume、TUI snapshot、default session listing filter 和 interrupted rollout recovery。
- [x] 增加 architecture guards，禁止 nested SessionTask、Tool handler 直调 `run_turn`/Provider、第二 Thread registry、generic agent task bus、child write Tool、TUI agent truth 和 fire-and-forget watcher。
- [x] 运行 targeted tests、`make check`、`go test ./... -count=1`、`go test -race ./... -count=1`、`git diff --check` 和 architecture grep 后才可标记 R 为 DONE。

### R 出口

- Root Agent 可以同时启动多个 read-only explorer SubAgent，并在继续自身 critical path 的同时通过 notification/wait 获取结果。
- 每个 child 是完整、可取消、可持久化且状态可观察的 Thread/Session；AgentControl 只负责 root-tree control plane，不形成第二 runtime。
- Prompt、WorldState、Tool、Event/Rollout 和 TUI 使用 Codex 术语与生命周期；child Tool isolation 吸收 Claude Code 的 allowlist 和权限不升级原则。
- 第一版不依赖 Multi-Agent V2、history fork、worker edit、跨 Thread Approval、team/worktree/remote 或 agent resume。

### R 验收

- 同一模型响应并行调用多个 spawn_agent 时，成功 child 独立运行，失败 reservation 完整回滚，Root Turn 不因 child event channel 无消费者而阻塞。
- child 只能看到真实 read-only ToolSet，无法编辑、执行命令、请求用户输入或继续 spawn；外部/受限读取不会悬挂 Approval。
- child completion 只产生一次 bounded notification；wait_agent 返回相同 typed status，Root TUI 显示专用 collaboration cell，Resume 与首次 live 语义一致。
- Root Turn Interrupt 后 child 可继续完成；Root shutdown 或 close_agent 后 child goroutine、writer、watcher 和 slot 全部释放。

### 完成记录

- 2026-08-21 完成 Codex V1 风格 Basic Multi-Agent 主链：Root tree 共享 `AgentControl`，SubAgent 作为完整 `AmadeusThread/Session` 运行，`ThreadManager` 保持唯一 live Thread registry；slot/nickname reservation、状态 reducer、事件驱动 wait、send/interrupt/restart、close 与 bounded shutdown 均已落地。
- `SessionSource`、`AgentMetadata`、`AgentStatus`、`CollabAgentToolCallItem`、canonical notification 与 SQLite source index 已贯通；默认列表和直接 Resume 排除 child。R 当时不恢复旧 agent tree，T 已替换该结论，AC 再关闭 open/closed edge 与 terminal reconstruction 缺口。
- Root-only `spawn_agent`、`send_input`、`wait_agent`、`close_agent` 使用统一 Tool pipeline；child 固定为 read-only `explorer`，拥有 fresh context、独立 permission/execution state、exact ToolRouter allowlist 与 deny-only Approval port。
- `SubagentDeveloperInstructions`、ToolSpec delegation guidance、`<subagents>` WorldState、`<subagent_notification>` contextual fragment 和 Codex 风格 live/Resume TUI projection 已统一到 typed Event/Rollout 协议。
- spawn rollback/并发容量/depth、send_input、wait timeout、notification 去重、失败 shutdown 保留 slot、Prompt/Tool isolation、Persistence、完整 Root→child→notification E2E、TUI 与 architecture guards 均有覆盖；`make check`、`go test ./... -count=1`、`go test -race ./... -count=1` 和 `git diff --check` 于 2026-08-21 通过。

## 21. S. Codex-style Fullscreen TUI Architecture Alignment — `DONE`

### 目标

在不复制 `/statusline` 配置功能的前提下，将 Fullscreen TUI 的固定 statusline 在 Session ownership、typed projection、Footer state、命名和生命周期上对齐 Codex；删除 Startup/Application status/TUI 之间的重复动态状态，并保持 Working、statusline 与 collaboration mode indicator 三者职责独立。在此基础上继续收敛 `/exit` 的 Application shutdown、renderer drain、typed exit result 与 CLI exit presentation 生命周期。

### S-01：Session Configuration + Snapshot Ownership — `DONE`

- 将 `ThreadViewSnapshot` 收敛为 `Generation + ThreadID + Title + Configuration + Items + Usage + ContextWindow`，其中 `Configuration` 直接复用 Event Protocol 的完整 `SessionConfiguration`。
- 为 `AmadeusThread`/Session 暴露并消费 typed configuration snapshot，保证 new、resume、attach 得到真实 CWD、Provider、Model、ReasoningEffort 和 Mode。
- 将 `FullscreenStartup` 收敛为真正静态的启动信息；删除 Startup、Application 和 snapshot 中对 cwd/model/provider/mode/context 的重复 owner。

### S-02：TUI Session State + Configuration Lifecycle — `DONE`

- 建立 active `fullscreenSessionState`，统一持有当前 ThreadID、Configuration、title、usage/context 与 attachment generation。
- 让 initial snapshot、`SessionConfiguredEvent`、`ThreadSettingsAppliedEvent` 和 `ThreadAttached` 共用 `applySessionConfiguration()`；settings applied event 返回完整生效 Configuration，不再只返回 Mode。
- Resume、new thread、delete/switch 与 attachment replacement 必须清理旧 session/footer 派生状态，并丢弃旧 generation 的迟到 Event 或异步结果。

### S-03：Typed Fixed StatusLine Projection — `DONE`

- 引入固定 `StatusLineItem` 集合：ModelWithReasoning、CurrentDir、GitBranch、ThreadTitle、ContextUsed、ContextWindowSize；不实现 `/statusline`、picker、持久化排序或用户自定义 items。
- 建立 value resolver、typed segment、cached state 与统一 accent mapping；值不可用时省略对应 item，不显示 placeholder，也不回退到通用 status 文本。
- 仅在 canonical session/title/token/branch state 改变时 refresh projection；terminal resize 不重新解析业务数据。

### S-04：Footer State + Pure Layout — `DONE`

- 建立 `footerState`/`footerProps`，分离左侧 statusline、右侧 Plan collaboration indicator 和 Footer 外部的 Working/status indicator。
- 将 `View()` 收敛为纯组合与渲染：不查询 `Application.Status()`、不访问文件系统、不启动 Git 查询、不修改 TUI state。
- 删除 `model.status` 作为 statusline fallback 的路径；实现确定性窄宽度裁剪，优先移除 title/context/model/current-dir，GitBranch 最后删除，并保证 Plan indicator 右对齐。

### S-05：CurrentDir-keyed Workspace Metadata — `DONE`

- 将 statusline 的目录术语统一为 `CurrentDir`；保留 `ProjectRoot` 作为未来 workspace policy/指令发现边界，不再用 `Project` 混指当前 CWD。
- 将 Git branch 建模为 CurrentDir-keyed derived cache；CWD 改变时立即清空旧 branch 并异步刷新。
- branch lookup 携带 attachment generation 与 CWD，完成时校验二者，禁止旧目录的迟到结果覆盖新 Session 状态。

### S-06：Legacy Cleanup、Guards + Acceptance — `DONE`

- 删除旧 `statusBar*` 数据模型、动态 `FullscreenStartup` 字段、snapshot 分散字段和 Application status fallback；迁移测试到新的 statusline/footer 术语。
- 增加 architecture guards，禁止 `View()` 查询 Application/文件系统、禁止 statusline 自建 Session owner、禁止 Mode 进入固定 `StatusLineItem`。
- 覆盖 new/resume/attach/settings update、窄终端、无颜色、branch stale result、缺失 optional item 与 live event/snapshot 等价性；运行 targeted tests、`make check`、`go test ./... -count=1`、`go test -race ./... -count=1` 和 `git diff --check`。

### S-07：Codex-style `/exit` App Lifecycle — `DONE`

- 建立 `ExitMode`、`ExitReason` 与 `AppExitInfo` typed model；正常 `/exit`、空输入退出快捷键及其他 user-requested exit 统一进入 `ShutdownFirst`，fatal/escape-hatch 才允许显式 `Immediate`。
- 将 `Shutting down…` 从 HistoryCell 移为 Composer/BottomPane 区域的 transient shutdown presentation；关闭 Popup/Modal、禁止继续输入，并保证重复退出请求只提交一次 shutdown。
- 以 pending shutdown target 协调 active Thread detach、Runtime shutdown、Rollout flush、child process cleanup 与 bounded timeout，删除 `/exit → tea.Quit` 和 `ShutdownFinished → tea.Quit` 的单阶段退出路径。
- 在 `ShutdownFinished` 后进入 `DrainingFrame`，让 `View()` 清空 Composer、Popup、Footer 与 shutdown indicator；Bubble Tea 完成至少一次空 active-frame redraw 并收到 `ExitFrameDrained` 后才执行 `tea.Quit`。
- 将 `FullscreenApplication.Run` 收敛为返回 `AppExitInfo`；X 将最终 exit presenter 迁入 `internal/cli`，并保持只在 renderer 停止、终端恢复后输出非零 token usage、可用 resume hint 或 fatal diagnostics，不重新查询已关闭 Runtime。
- 分层覆盖 renderer drain、Application shutdown 和 CLI summary 测试，包括重复退出、zero usage、不可恢复、fatal、timeout 与无 placeholder/Popup/Footer scrollback 残留；增加 guard 禁止 cursor writer、terminal-height padding 和手写 ANSI reposition 回归。

### S 出口

- statusline 数据链唯一为 `typed Session/Thread state → fullscreenSessionState → StatusLineItem projection → footerState/footerProps → render`；Configuration、title、usage/context 与 branch 各自保留准确 typed source。
- fixed statusline 不依赖 `/statusline` 功能，也不从 Startup、Application Status、Working header 或渲染期 IO 补全数据。
- Working/status indicator、固定 statusline 与 Plan collaboration indicator 拥有独立数据模型、生命周期和布局职责。
- CurrentDir、GitBranch、ThreadTitle、Model/Reasoning 与 Context 在 new、resume、attach 和 settings update 后立即与 active Session 一致。
- user-requested exit 唯一经过 `ShutdownFirst → shutdown/flush/cleanup → renderer drain → AppExitInfo → CLI exit presentation`；TUI active frame、Application resource shutdown 和进程退出信息各自拥有明确 owner 与阶段边界。

### S 验收

- `SessionConfiguredEvent`、`ThreadSettingsAppliedEvent` 与 `ThreadViewSnapshot` 使用同一完整 `SessionConfiguration`；TUI 不保留 Mode/Model/CWD 的并列 snapshot owner。
- `View()` 不调用 `Application.Status()`、不访问文件系统且不改变状态；resize 只触发布局计算。
- CurrentDir 改变后旧 branch 立即消失，迟到 lookup 不覆盖新 CWD；optional item 缺失时其余 segment 正常布局。
- 窄终端仍优先保留 GitBranch workspace identity；Plan mode 标签保持右对齐，Default mode 不显示 mode 标签，Working/retry 文本不进入 statusline。
- 固定 item 顺序稳定，代码和文档中不存在 `/statusline` 命令、配置 schema、picker 或持久化 customization 主链。
- `/exit`、空输入退出快捷键与重复退出请求共享幂等 `ShutdownFirst` lifecycle；正常退出 scrollback 不残留 Composer placeholder、Popup、Footer 或 transient shutdown presentation。
- `FullscreenApplication.Run` 在终端恢复后返回唯一 `AppExitInfo`；token usage/resume hint 各输出至多一次，zero usage、不可恢复、fatal 与 timeout 路径均有确定性测试。
- 最终 `tea.Quit` 只能发生在 shutdown 终态及 `ExitFrameDrained` 之后；实现不包含 output writer wrapper、terminal-height padding、`CursorUp`/`CursorDown` 或其他手写 ANSI cursor reposition。

### 完成记录

- 2026-08-22 将 `SessionConfiguredEvent`、`ThreadSettingsAppliedEvent` 与 `ThreadViewSnapshot` 统一到完整 typed `SessionConfiguration`；Session 增加并发安全 configuration snapshot，`AmadeusThread` 删除 stale configuration mirror 与 Mode-only query。
- `InteractiveApplication` 删除 Project/Provider/Model/ContextWindow 并列动态字段，`FullscreenStartup` 收敛为 Version；Fullscreen TUI 以 `fullscreenSessionState` 统一承接 snapshot、configured、settings applied、rename 与 token lifecycle。
- 新增独立 `session_state.go`、`status_line.go`、`status_line_workspace.go` 与 `footer.go`，固定六个 StatusLineItem，通过 cached projection 和 pure Footer renderer 展示；Working/status 与 Plan collaboration indicator 保持独立。
- Git branch 改为 CurrentDir + attachment generation keyed async cache，CWD/Thread attach 时清空并刷新，迟到 generation/CWD 结果被丢弃；`View()` 不查询 Application、不访问文件系统，也不使用 status fallback。
- 删除旧 `statusBar*`、动态 Startup 字段、snapshot 分散配置字段和 TUI Mode/Model/Usage mirrors；新增完整 configuration、缺失 item、窄终端、stale branch、纯 Footer 与 architecture guards 覆盖。
- 修复 Shift+Tab 模式切换回归：删除将 Codex/Ratatui surface 生搬到 Bubble Tea 的 terminal-height padding 与自定义 cursor writer；Footer 只在活动 frame 当前行内右对齐 Plan indicator。`ThreadSettingsAppliedEvent` 插入描述真实状态变化的 `• Mode changed to <Mode>.` Info HistoryCell，history flush 后由 Bubble Tea 原生 renderer 重绘单份 Composer/Footer。
- 补齐 Codex Footer/Popup 生命周期：Footer 为右侧 mode 预留独立列并在不足时收缩 cycle hint，左侧 statusline 在列边界前裁剪；Slash popup 激活时替换 Footer，`/e` 仅保留 `/exit`，filter 变化重置 selection，避免旧 `/skills` 行与 statusline 残留。
- Selection overlay 收敛到 Codex `SelectionViewParams` 风格：公共 renderer 不再合成默认按键提示，Hint 由调用方显式选择；subtitle 后统一保留一行。`/skills` 顶层与 `/resume` 省略 Hint，其他明确配置的提示保持不变。
- 修复 Slash Popup 的物理行残留：prefix filter 本身保持正确；popup 活跃期间保留已使用的可见行数，`/re → /res` 与 `/s → /sk` 只清空消失的候选行，不缩短 Bubble Tea active frame，因此 `/resume`、`/skills` 不再被增量 renderer 清除。Thread 切换继续由 Application 事务化 attach lifecycle 负责。
- Composer 改用 Bubbles `SetPromptFunc`，仅第一条视觉行显示 `› `，后续换行保留等宽 gutter；`SetWidth` 使用完整终端宽度，避免 textarea 已扣除 prompt 后再次减去两列导致提前换行。针对 Bubbles v0.21.0 soft-wrap viewport 不持久化 content、无法更新 YOffset 的限制，展示层改为渲染完整 textarea 后按全局 cursor 视觉行截取最多五行，输入末尾与 Home 导航均保持可见。当前基础版保留 Bubble Tea 软件光标，不增加会扰乱 Footer/Popup 的硬件 cursor writer。
- 2026-08-22 继续收敛默认 Codex 视觉：Slash Command Popup 不再显示 Modal picker 的 `›` cursor glyph，只通过选中 command name/description style 表达 selection；Resume/Approval/Skills 等 picker 保留 cursor。
- Statusline 从硬编码 fallback RGB 改为 theme-first：TrueColor/ANSI256 使用明暗自适应 Catppuccin Mocha/Latte Chroma semantic token，并执行 Codex 同款 85% saturation softening；ANSI16 与 No Color 继续走稳定 fallback。
- 2026-08-22 完成 Codex-style `/exit` lifecycle：新增 `ExitMode`、`ExitReason`、`AppExitInfo`、pending exit target 与独立 `fullscreenExitState`；`/exit` 和空 Composer Ctrl+D 在 idle/running 状态统一进入幂等 `ShutdownFirst`，durable delete 完成后通过显式 `Immediate` 进入同一 renderer drain 终态。
- `InteractiveApplication.Shutdown` 收敛为同步 typed operation 并迁入独立 `interactive_shutdown.go`，负责关闭 ThreadWorkspace/ThreadManager、flush Rollout、清理 child/process 资源与 detach active Thread；删除 `ShutdownStarted`/`ShutdownFinished` Application Event 和 `shutdownRequested` mirror。
- `Shutting down…` 改为唯一 transient active-frame presentation；shutdown completion/timeout 进入 `DrainingFrame`，`View()` 先渲染空 active frame，再由 `ExitFrameDrained` 触发唯一 `tea.Quit`。删除 History shutdown cell 与 Application event 直退路径，并用 architecture guard 禁止 cursor writer、terminal padding 和手写 ANSI reposition 回归。
- `FullscreenApplication.Run` 现返回最终 `AppExitInfo`；CLI 在 Bubble Tea renderer 停止、终端恢复且 InteractiveApplication 关闭后输出非零 token usage、可用 resume hint、timeout warning 或 fatal diagnostics，并复用 `commandExitError.reported` 避免主入口重复报错。Color terminal 对齐 Codex，仅将 resume command 使用 ANSI cyan 高亮并以 foreground reset 收尾；No Color 保持纯文本。
- `make check` 与 `go test -race ./... -count=1` 于 2026-08-22 全量通过，`git diff --check` 通过。

## 22. T. Thread + Session UUID Identity Alignment — `DONE`，persisted child membership/status 结论由 AC 收紧

### 目标

在不兼容旧本地开发数据的前提下，将 Thread identity 从 `thread-<unixnano>-<sequence>` 字符串体系替换为 Codex 风格 UUID value object：Amadeus-generated ThreadID 使用 UUIDv7，Resume、Rollout、SQLite、Event 和 Agent routing 使用同一个 canonical ThreadID；同时建立真实 SessionID，明确 Root/child 的 agent-tree ownership，并一次性贯通 Session、AgentControl、persisted child restore、Tool/Audit、Provider metadata、Application/TUI 与 Persistence。SQLite StoredThread 只保存 Thread metadata/parent relation，canonical SessionID 只来自 Rollout SessionMeta。

### T-01：Protocol UUID Identity + Naming Contract — `DONE`

- 在 Protocol/Identity domain 建立可比较的 `ThreadID` 与 `SessionID` UUID value object，提供 UUIDv7 constructor、parse、canonical string、zero-state 和 JSON/Text codec；将 `github.com/google/uuid` 提升为直接依赖。
- Amadeus-generated ID 固定为 UUIDv7，parser 接受合法 UUID；删除 ThreadID 的 arbitrary safe-string contract、直接 string conversion 和 `thread-` prefix 语义。
- 将实际承载 Resume target 的 `SessionID` 字段、参数和 helper 改为 `ThreadID`/`ResumeThreadID`，不以用户界面的 Session 文案污染内部 identity naming。

### T-02：Thread Creation + Resume Lifecycle — `DONE`

- Root 与 child New Thread 统一从 Protocol/Identity 生成 UUIDv7；从 ThreadManager 和外层装配删除 `NextID("thread")`、thread prefix factory 分支及对应注入点。
- New Root 由 ThreadID 派生 SessionID；Resumed Root 从 Rollout SessionMeta 恢复 SessionID，并校验 requested ThreadID、StoredThread.ID、Rollout path identity、SessionMeta.ID 与 Root `SessionIDFromThreadID(ID)` 全部一致。
- Session configured 成功后再注册 live Thread；生成失败、重复 ID、history mismatch 或 spawn 失败必须完整关闭 writer/runtime，不得留下半注册 Thread。

### T-03：Session + AgentControl Dual Identity — `DONE`

- SessionSpawnArgs、Session、AmadeusThread 与 root-scoped AgentControl 同时持有 typed SessionID/ThreadID；Control 暴露 `SessionID()` 与 `RootThreadID()`，不保留误名 string mirror。
- 所有 child 从 AgentControl 继承同一个 SessionID 并生成独立 ThreadID；child SessionID 不能从 child ThreadID 派生。
- ThreadManager registry、AgentID、parent/child relation、send/wait/close routing 继续使用 ThreadID；共享 SessionID 只表达 session-level ownership，不能替代 Thread key。

### T-04：Persisted SubAgent Restore + Internal Resume — `DONE`，open-edge selection 与 terminal reducer 由 AC 取代

- Root Resume 后按 SQLite parent/source relation 查找 persisted descendants，读取每个 child Rollout SessionMeta，并校验 child ID、ParentThreadID 与 `SessionID == AgentControl.SessionID()`；不通过 SQLite SessionID 查询 tree。
- AgentControl record 支持 persisted/unloaded child metadata；ThreadManager/AgentHost 提供内部 child resume，`send_input` 等操作按 child ThreadID 加载并恢复 runtime，AgentControl 不直接打开 Rollout 或构造 Session。
- 公开 `/resume`、`--resume`、picker 和 sessions list 仍只选择 Root Thread；不增加公开 `resume_agent` Tool、child attach UI 或 arbitrary detached child recovery。

### T-05：Protocol + Tool + Audit Identity Contract — `DONE`

- `SessionConfiguredEvent`、ThreadViewSnapshot 与 StatusSnapshot 同时携带 SessionID/ThreadID，SessionConfiguredEvent 另带可选 ParentThreadID；其他 Event、attachment matching 与 `ThreadIDOf` 继续按具体 ThreadID 路由。
- Tool Invocation/InvocationMetadata 同时携带 typed SessionID、ThreadID、TurnID；`spawn_agent` 使用 Invocation.ThreadID 作为 parent，Multi-Agent target 参数在 Tool boundary 通过 ParseThreadID 解析。
- Audit record 增加 SessionID、ThreadID 与 TurnID，所有 Tool 从 Invocation metadata 取得 identity；ApprovalRequestEvent 继续按 ThreadID + TurnID 路由，Root/child SessionPermissionContext 不因共享 SessionID 自动合并。
- 保持 `execute_command`/`write_stdin` 模型 schema 中进程 `session_id` 的外部术语，但内部明确使用 process ID，禁止与 Agent SessionID 类型转换或复用。

### T-06：Provider Request Identity Metadata — `DONE`

- Runtime TurnContext 持有 typed SessionID/ThreadID/TurnID，TurnContextItem 继续只持久化 ThreadID/TurnID；Resume 后从 canonical SessionMeta 重新注入 SessionID。
- 普通 sampling 与 Compaction 使用同一 request metadata projector，向实际 Provider Adapter 贯通 `session_id`、`thread_id`、`turn_id` 和可选 `parent_thread_id`；Root/child 共享 SessionID 但 ThreadID 不同。
- Adapter 不从 UI、CWD、Tool metadata 或当前 active Thread 猜测 identity，不增加未接线的空 metadata 扩展点。

### T-07：Rollout + SQLite Persistence Reset — `DONE`

- 将 SessionMetaItem 收敛为 `session_id + id + optional parent_thread_id`，删除旧 `thread_id` metadata 字段；Event、TurnContextItem、ResponseItem 和 collaboration payload 继续按具体作用域使用 `thread_id`。
- SQLite `threads.id` 保持 ThreadID 主键并保存 parent/source/agent metadata；StoredThread 不增加 SessionID。Rebuild 校验 SessionMeta identity contract，但不把 SessionID 复制进 SQLite。
- Rollout 文件名、SessionMeta.ID、StoredThread.ID 与 Resume target 使用同一 canonical UUID；删除旧 SQLite、Rollout、fixture 和 decoder，不增加 migration、legacy reader 或双格式兼容。

### T-08：CLI、Application + TUI Identity Boundary — `DONE`

- `--resume`、`/resume`、session picker、exit resume hint 和 sessions command 在输入边界调用 `ParseThreadID`，输出统一调用 `ThreadID.String()`；非法 UUID 返回 typed/user-visible error。
- 将 CLI invocation 中实际承载 resume target 的字段统一命名为 ResumeThreadID；SessionOption.ID、AppExitInfo.ThreadID 和 resume hint 始终使用 ThreadID。
- TUI/Application attachment、迟到 Event、MCP inventory、Git branch lookup 和 overlay matching 继续使用 generation + ThreadID；不以 SessionID 替代 Thread routing，也不执行强制类型转换、prefix trimming 或维护 display-only ID。
- 保持用户可见的 session/list/resume 产品术语；状态诊断可以持有 SessionID，但用户可恢复 ID 与 Codex 风格 status item 显示当前 ThreadID。

### T-09：Legacy Cleanup、Guards + Acceptance — `DONE`

- 将测试 fixture 全部替换为稳定合法 UUID，增加 UUIDv7 generation/parse/codec、Root/child SessionID、Root resume child restore、internal child resume、Provider/Audit metadata、SQLite rebuild 与 CLI/TUI invalid input 覆盖。
- Architecture guards 禁止 `NextID("thread")`、`protocol.ThreadID(value)`、`strings.TrimPrefix(..., "thread-")`、旧 SessionMeta `thread_id`、SQLite/StoredThread SessionID、将共享 SessionID 用作 Thread registry key，以及从 Tool Invocation.SessionID 路由 SubAgent。
- 删除非当前 SQLite、Rollout、fixture 和本地开发数据路径；运行 identity/thread/session/multi-agent/tool/audit/provider/persistence/app/TUI/CLI targeted tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check`，全部通过后才能将 T 标记为 DONE。

### T 出口

- 新建 Root/child Thread 的 ID 均为 canonical UUIDv7，整个生产链不存在 `thread-` 前缀和通用 thread ID factory。
- `/resume`、StoredThread、Rollout filename/metadata、Event scope 与 live registry 对同一 Thread 使用一个 typed canonical ThreadID，非法或不一致 identity 在边界被拒绝。
- Root/child 共享真实 SessionID，但各自拥有独立 ThreadID；Root Resume 可恢复并校验 persisted child metadata，后续 child 操作继续按 child ThreadID 路由。
- Tool/Audit/Provider metadata 同时表达真实 SessionID/ThreadID/TurnID；Event、Agent target、Application attachment、CLI/TUI resume 和 SQLite Thread index 不误用 SessionID。
- canonical SessionID 只存在于 Rollout SessionMeta 和运行时 identity snapshot，SQLite StoredThread 不复制 SessionID。
- 当前代码、schema、fixtures 和文档只保留 UUID identity contract，不包含旧数据兼容层。

### T 验收

- 连续创建多个 Root/child Thread 时 UUIDv7 唯一且可 canonical round-trip；Root SessionID 与 Root ThreadID 使用同一个 UUID value，child SessionID 继承 Root SessionID 且 child ThreadID 独立。
- 通过 CLI、TUI picker 和 direct `/resume <uuid>` 恢复时均定位同一 StoredThread；非法 UUID、metadata mismatch 和不存在 Thread 返回明确错误且不改变 active attachment。
- Root Resume 后 persisted child 以同一 SessionID 进入 Control metadata；对 unloaded child 执行 send_input 时按 child ThreadID 内部恢复，错误 SessionID/parent relation 被拒绝且不污染 registry。
- 清空 SQLite 后可从当前 UUID Rollout 重建相同 ThreadID、parent relation 和 metadata index，并从每个 Rollout SessionMeta 恢复/校验 SessionID；生产代码只保留当前 schema 与 codec。
- Root/child Provider request 与 Audit record 显示相同 SessionID、不同 ThreadID 和正确 TurnID；`spawn_agent` 不读取 Invocation.SessionID 作为 parent target。

### T 完成记录

- Protocol/Identity 已使用封装 `uuid.UUID` 的可比较 `ThreadID`/`SessionID` value object；新 Thread 统一生成 UUIDv7，CLI、Tool、JSON/Text、SQLite 和 filename boundary 使用显式 parse/format。
- Root/child Runtime 已贯通 typed SessionID/ThreadID/ParentThreadID；ThreadManager 在 SessionConfigured 成功后注册 live Thread，Root Resume 校验 StoredThread、Rollout filename 和 SessionMeta identity。
- AgentControl 保存 shared SessionID 与 RootThreadID，支持 persisted/unloaded child record；Root Resume 从 parent relation 恢复 child metadata，`send_input` 按 child ThreadID 触发完整 AmadeusThread/Session lazy resume。
- SessionConfiguredEvent、ThreadViewSnapshot、StatusSnapshot、Tool Invocation、Audit Record、TurnContext 与 Provider RequestMetadata 已统一 identity contract；Responses 与 Chat Completions 都实际发送 session/thread/turn/parent metadata。
- Rollout 升级为当前 v3 `session_id + id + parent_thread_id` SessionMeta，SQLite 升级为当前 v3 且只保存 Thread metadata/parent relation；SQLite rebuild 可从 Root+child Rollout 恢复 index。
- CLI `ResumeThreadID`、`--resume`、TUI `/resume`、picker、status、exit hint 与 Multi-Agent Tool boundary 均使用 typed ThreadID；非法 UUID 在边界拒绝且不改变 attachment。
- 新增 identity architecture guard，禁止通用 thread ID factory、直接 ThreadID conversion、旧 SessionMeta field 和 SQLite SessionID 回归；本地开发数据库已直接清理。
- `make check`、`go test -race ./... -count=1` 与 `git diff --check` 于 2026-08-22 全量通过；Provider root/child identity、persisted child lazy resume、Audit identity 和 SQLite child rebuild targeted tests 通过。

## 23. U. Next-Turn User Input Queue Alignment — `DONE`

### 目标

在 O 已完成的 same-turn steer 主链之外，增加 Codex 风格显式下一 Turn 排队：Regular/Plan/Compact Turn 运行期间，普通文字 Enter 继续使用现有 Runtime admission（Regular 可 steer，Compact 保持 typed rejection），普通文字 Tab 只进入 Fullscreen TUI 的 attachment-scoped transient FIFO；当前 Turn terminal 后每次只通过既有 `UserInputOp`/UserMessageAdmission 主链启动一个新 Turn。Queue 不提前进入 Session、Context、Rollout、Resume 或 SQLite，也不复用 Session deferred submission/TurnInputQueue。

本阶段依据 Codex `ChatComposer.InputResult::Queued`、`ChatWidget.InputQueueState`、`maybe_send_next_queued_input` 和 interrupted-turn draft restore 源码确定 owner 与时序；不复制 queued Slash/Shell、图片/paste、多 thread-tab composer state 或可配置 keymap 等当前 Amadeus 不需要的产品复杂度。

### U-01：Queue Contract + Ownership — `DONE`

- [x] 在 `internal/interface/tui/input_queue.go` 建立 `QueuedUserInput` 与 `NextTurnQueue{Pending, InFlight}`；字段包含 Content、Mode、ThreadID 和 attachment generation，职责只覆盖尚未提交的未来用户输入。
- [x] 保持 Session `InputQueue`/`TurnInputQueue` 只拥有已被 Runtime 接纳的 same-turn input；禁止新增 `QueuedInputOp`、`UserMessageAdmissionQueued`、queued EventMsg/RolloutItem 或把 Tab 输入塞进 `Session.deferred`。
- [x] 明确队列是 transient TUI input state，不成为第二 conversation history、Application Thread registry 或 Runtime terminal owner；在架构 guard 中保护 owner 和禁止项。

### U-02：Composer Input Result + Tab Queue — `DONE`

- [x] 扩展 Composer/InputResult 以区分普通 submit 与 queue disposition；运行中普通文字 Enter 保持现有 SubmitUser/Started-or-Steered admission，Tab 只 enqueue 并清空 composer。
- [x] Slash Popup selection 的 Tab completion 优先于 queue；初始版本不 queue Slash Command/Shell action，不把 `/...` 原始字符串作为普通 UserInputOp 延迟提交。
- [x] enqueue 时只更新本地 input recall 与 queued preview，不生成 ClientUserMessageID、不插入 UserMessageCell、不调用 Runtime；真正 dequeue 时才复用 `prepareTaskSubmission` optimistic/canonical lifecycle。

### U-03：Terminal Drain + Admission Handshake — `DONE`

- [x] 只有 matching `TurnCompleteEvent` 才自动 drain；当前 Turn UI 先完成，再将 FIFO head 同步移入 InFlight、关闭本地 drain gate，然后创建异步 SubmitUser command。
- [x] 一个 terminal 至多启动一条 queued input；InFlight/start-pending 在 matching `TurnStartedEvent` 前阻止重复 terminal、resize、status refresh、admission callback 或按键触发第二次发送。
- [x] dequeue admission 必须为 Started；Steered 作为 Thread/attachment/ordering invariant violation 显示诊断，不能静默接受为 queue success。下一条 Pending 等待新 Turn 自己的 terminal。
- [x] Plan Turn 存在 queued follow-up 时跳过 `Implement this plan?` overlay，并优先启动 queued Plan input；无 queue 时保持现有 Proposed Plan transition。

### U-04：Abort、Failure + Attachment Isolation — `DONE`

- [x] `TurnAbortedEvent` 和 `TurnOutcomeBlocked` 不自动提交，将 InFlight/Pending 按 FIFO 恢复到 composer 并清空 queue；普通 ErrorEvent 等待唯一 TurnComplete，不提前 drain。
- [x] queued submission error、malformed admission 或 application cancellation 在 Runtime 接纳前将 InFlight 恢复到 composer，保留其余 Pending，不跳过失败 head 继续自动发送；已经接纳的 unexpected Steered 只停止 drain 并显示 invariant diagnostic，不重复恢复。
- [x] dequeue、admission 和异步结果校验 origin ThreadID + attachment generation；Resume、Clear、Delete、shutdown 或新 Thread attach 清理旧 attachment queue，绝不把旧输入发到新 Thread。
- [x] dequeue 前校验当前 Session mode 与 queued Mode；不一致时恢复 composer，不静默改写 queued input 的 Collaboration Mode。
- [x] 明确 queue 不持久化、不进入 Resume replay；进程退出或 attachment replacement 后不声称 queued input 已被接纳。

### U-05：Queued Preview + Rich TUI Lifecycle — `DONE`

- [x] 在 Composer area 增加紧凑、有界的 FIFO preview，显示 queued 数量与截断后的内容；它不是 HistoryCell、Tool activity、Working header 或固定 statusline item。
- [x] 宽屏、窄屏、No Color、Slash Popup、Approval/User Input overlay 和五行 composer viewport 下保持稳定布局，不覆盖 Composer/Footer，也不把 preview 刷入 terminal history。
- [x] enqueue 不改变当前 Turn 的 elapsed timer、Working/status、active item、details store 或 plan stream；dequeue 后的新 Turn 仍完全由 TurnStartedEvent 初始化。

### U-06：Tests、Docs、Guards + Acceptance — `DONE`

- [x] 覆盖运行中 Enter steer 与 Tab queue 分流、Slash completion 优先、空输入、FIFO、多 terminal 去重、InFlight/TurnStarted handshake 和每 Turn 仅一条自动提交。
- [x] 覆盖 completed/failed 自动 drain、aborted/blocked restore、submit rejection、malformed/unexpected admission、Plan popup suppression 和旧 generation/ThreadID isolation。
- [x] 覆盖 queue 不进入 Event/Rollout/Context/Resume、optimistic UserMessage 只在 dequeue 时产生、preview 窄宽度/No Color/overlay 布局和 input recall。
- [x] 增加 architecture guards，禁止 Core queued Op/admission、Session deferred UserInput、TUI 第二 terminal truth 和 queue state 进入 canonical projector；运行 targeted tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check`。

### U-07：Codex-style Queue Hint Footer — `DONE`

- [x] 为 `footerProps` 增加 `HasQueueableDraft` 或等价纯派生输入；只在 Turn running、Composer 普通文字非空、`ParseInput` 为 Text 且没有 popup/overlay 时启用，不写入 `footerState`、Session、NextTurnQueue 或 statusLineState。
- [x] 对齐 Codex `FooterMode::ComposerHasDraft + is_task_running`：queueable draft 出现时固定 statusline 临时让位，Footer 左侧显示 dim `tab to queue message`；窄屏收缩为 `tab to queue`。
- [x] 建立 queue-hint 专用纯布局函数或内聚分支：完整 hint + Plan indicator 可共存时保留两者，不足时依次使用短 hint、删除 Plan indicator，禁止先隐藏 queue hint 或让左右内容重叠。
- [x] Tab enqueue 清空 Composer 后 hint 立即消失并恢复普通 statusline；已有 `Queued (n)` preview 时继续输入下一条 queueable draft，preview 与 footer hint 同时显示。
- [x] Slash Popup、Selection、Approval 和 Request User Input overlay 继续拥有更高优先级并隐藏普通 Footer/queue hint；Slash Command、invalid slash、空输入和 idle draft 不显示误导性提示。

### U-08：Queue Hint Visual Tests、Guards + Docs — `DONE`

- [x] 增加 pure Footer 与 fullscreen snapshot/semantic tests，覆盖 running empty/draft、idle draft、Default/Plan、已有 queued preview、Slash Popup、各类 overlay、ordinary/slash input 和 Tab enqueue 前后状态转换。
- [x] 覆盖 40 列及更窄有效布局、完整/短 hint fallback、Plan indicator 让位顺序、No Color/ANSI16/ANSI256/TrueColor 和动态输入清空恢复，保证文字不截断成不可辨识状态且不进入 scrollback。
- [x] 扩展 architecture guard，禁止将 queue hint 建模为 StatusLineItem、footerState 持久字段、HistoryCell 或 Runtime Event；`View()`/renderFooter 保持纯渲染且不查询 Application/Runtime。
- [x] 同步 README、TUI visual contract、architecture whitepaper、design 和本进度文档；运行 focused snapshot tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check` 后才重新将 U 标记 DONE。

### U 出口

- Enter 与 Tab 对运行中普通文字具有稳定且可见的不同语义：Enter 属于当前 Turn，Tab 属于后续独立 Turn。
- NextTurnQueue 只有 Fullscreen input layer 一个 owner；Session、Protocol、Rollout、Context 和 Persistence 不保存未提交 queue state。
- terminal 后按 FIFO 每次只启动一个新 Turn，admission 为 Started；aborted/blocked/rejection 不会丢失输入或发送到错误 attachment。
- queued preview、Plan transition、Resume/Clear/Exit 和现有 same-turn steer 在宽/窄终端下形成一致生命周期。
- running Turn 中 queueable ordinary draft 将 Footer 切换为 Codex 风格 Tab queue hint；固定 statusline 暂时让位，窄屏优先保留完整或短 hint，Plan indicator 只在可容纳时显示。

### U 验收

- Regular Turn 运行期间依次 Tab queue `second`、`third`，当前 Turn 的 Rollout/Prompt 都不包含二者；当前 Turn 完成后启动只含 `second` 的新 Turn，第二个 Turn 完成后再启动 `third`。
- 同一状态下按 Enter 提交 `clarification` 返回 Steered 并进入当前 Turn；Tab queue 不产生 Steered、额外 TurnStarted 或当前 Turn UserMessage Item。
- 当前 Turn aborted 或 blocked 时 queued 内容恢复到 composer 且没有新 Submission；queued submit 失败时首项恢复、后续项保持原 FIFO。
- Resume/Clear 切换 attachment、迟到 terminal/admission 和重复 Bubble Tea message 均不能把旧 queue 发送到新 Thread 或重复提交。
- Plan Turn 有 queue 时不弹 implementation overlay 并继续 queued Plan Turn；无 queue 时保留现有 implementation transition。
- Default/Plan Turn 运行时输入 ordinary draft，Footer 分别显示完整/短 `tab to queue message`/`tab to queue` 且不与右列重叠；清空或 Tab enqueue 后恢复固定 statusline，Slash/popup/overlay 不显示错误 hint。

### U-01～U-06 阶段完成记录

- 2026-08-24 完成 Codex 风格 next-turn queue：新增独立 `input_queue.go`，由 Bubble Tea `fullscreenModel` 串行拥有 `QueuedUserInput`、Pending FIFO、InFlight/start-pending gate 和 bounded preview；Session、Protocol、Rollout、Context 与 Persistence 未增加 queued state。
- 运行中普通文字 Enter 保持原 Started/Steered admission，Tab 在 Slash completion 之后进入 transient queue；真正 dequeue 才生成 ClientUserMessageID、optimistic UserMessage 和普通 `UserInputOp`，一个 terminal 只发送 FIFO 一项。
- TurnStarted confirmation、failed terminal drain、aborted/blocked restore、pre-admission rejection、unexpected Steered halt、Mode/ThreadID/generation isolation、Plan popup suppression 与 attachment clear 均已实现并有针对性测试。
- 新增 next-turn queue architecture guard，继续复用 O 阶段 UserInput 不得进入 Session deferred queue 的 AST guard；README、design、TUI visual contract 与本进度文档已同步。
- `make check`、`go test -race ./... -count=1`、focused TUI/architecture race tests 与 `git diff --check` 于 2026-08-24 通过。

### U-07～U-08 完成记录

- 2026-08-24 对齐 Codex ComposerHasDraft queue hint：`footerProps.HasQueueableDraft` 从 running、Composer `ParseInput` 与 overlay state 纯派生，`footerState`、StatusLineItem、NextTurnQueue 和 Runtime 均未增加提示状态。
- 新增 `renderQueueHintFooter` 纯布局：宽屏显示 `tab to queue message`，窄屏显示 `tab to queue`；Plan indicator 只在可容纳时右对齐保留，固定 statusline 在 queueable draft 期间让位。
- Tab enqueue 后 fixed statusline 恢复；已有 queued preview 时新 ordinary draft 同时显示 preview 与 hint；Slash/invalid slash、empty/idle、popup 和各类 interactive overlay 不显示误导提示。
- 新增 pure/fullscreen snapshot、宽度 fallback、Plan priority、No Color/ANSI16/ANSI256/TrueColor、状态转换与 render purity 测试；architecture guard 禁止 hint 进入 footerState、StatusLineItem、HistoryCell 或 Runtime/canonical package。
- README、TUI visual contract、architecture whitepaper、design 与本进度文档已同步；focused race、`make check`、`go test -race ./... -count=1` 和 `git diff --check` 于 2026-08-24 通过。

## 24. V. Versionless Config Schema + Example Naming — `DONE`

### 目标

删除 Amadeus 用户配置中没有运行时价值的顶层 `version` gate，将当前配置收敛为唯一 versionless strict schema；任何旧 `version:` 字段通过 `KnownFields(true)` 直接拒绝，不保留 migration、alias 或兼容 decoder。同时将仓库模板从 `configs/amadeus.example.yaml` 唯一重命名为 `configs/config.yaml.example`，运行时自动发现文件仍固定为 `$AMADEUS_HOME/config.yaml`。

### V-01：Versionless Config Domain + Loader — `DONE`

- [x] 从 `config.Config`、`configPatch`、Default、Clone/apply、Validation 和 Loader 删除 Version/CurrentVersion；不增加替代 revision 字段或隐藏 schema marker。
- [x] 从 Sources/provenance 与 `config explain` 删除 version 路径，从 `config show` YAML 删除 version 输出；CLI/env/provider/model 配置优先级保持不变。
- [x] 保持 strict YAML KnownFields；`version: 2`、`version: 1` 和其他旧 schema 字段作为 unknown field 明确失败，不静默忽略且不进入兼容分支。

### V-02：Example File Rename + Current Fixtures — `DONE`

- [x] 将 `configs/amadeus.example.yaml` 直接重命名为 `configs/config.yaml.example`，不保留旧文件、symlink 或 duplicate template。
- [x] 删除模板、README minimal config 和全部当前测试 fixture 中的 `version: 2`；更新 README copy/link、模板自说明与 `internal/config/example_test.go` 路径。
- [x] 保持用户运行时文件名 `$AMADEUS_HOME/config.yaml`、`--config` 行为和 Config v2 历史进度记录不变；历史名称不构成生产兼容入口。

### V-03：Docs、Guards + Acceptance — `DONE`

- [x] 同步 design、architecture whitepaper、README 与本进度文档，明确 versionless schema、strict rejection、模板/运行时文件名边界和无旧配置兼容。
- [x] 增加 architecture guard，禁止 `internal/config` 重新出现 Config Version/CurrentVersion/version yaml tag，要求新模板存在、旧模板不存在，并禁止 README/当前设计引用旧路径。
- [x] 覆盖 missing file defaults、versionless file load/validation、removed `version` rejection、show/explain/provenance 无 version、CLI/env override、example validation 与 Provider E2E fixture。
- [x] 运行 focused config/CLI tests、`make check`、`go test -race ./... -count=1` 和 `git diff --check` 后，将 V 标记 DONE。

### V 出口

- 用户配置只有一个严格当前 schema，Domain、Patch、Loader、Validation、Sources 和 CLI output 均不存在顶层 version 事实或兼容路径。
- `configs/config.yaml.example` 是仓库唯一完整 config 模板；`$AMADEUS_HOME/config.yaml` 是唯一自动发现文件，旧示例路径不再存在。
- 当前 versionless 配置、默认值、override、show/explain、E2E 和文档一致；旧 `version:` 输入可见失败而不是被迁移或忽略。

### V 验收

- versionless `config.yaml` 通过 `config check/show/explain`；输出和 provenance 不包含 version。
- 添加 `version: 2` 后 strict decoder 返回包含 `version` 的 unknown-field error；删除后同一配置恢复有效。
- `configs/config.yaml.example` 无环境变量即可加载和验证；仓库与 README 不存在 `configs/amadeus.example.yaml` 当前引用或文件。

### V 完成记录

- 2026-08-24 删除用户 Config/patch/default/loader/validation/provenance/explain 中的 Version/CurrentVersion 主链；Rollout 自身的持久化版本保持独立，不受影响。
- YAML loader 继续使用 `KnownFields(true)`，旧 `version:` 与其他删除字段一样返回明确 unknown-field error；没有 migration、alias、双 schema 或静默忽略路径。
- 仓库模板唯一重命名为 `configs/config.yaml.example`，README、模板自说明、example validation 和全部当前 CLI/Provider fixture 已切换；运行时自动发现仍固定为 `$AMADEUS_HOME/config.yaml`。
- architecture guard 固化 versionless Config 与唯一模板路径，config show/explain/provenance 测试确认不再输出 version。
- focused config/CLI/architecture tests、`make check`、`go test -race ./... -count=1` 与 `git diff --check` 于 2026-08-24 通过；首次全量 race 的未改动 PTY 测试发生一次时序波动，单包与全仓重跑均通过。

## 25. W. Context Accounting + Compaction Realignment — `DONE`，Prompt/baseline 部分由 AA 取代

### 目标

按 `docs/design.md` 当前 Contract 替换旧 token/context/compaction 主链：区分 Thread 累计 Token 消耗、最近 Provider request usage、当前 active context 和 exact Prompt preflight estimate；将 Compaction 收敛为 Session-owned lifecycle，使 CompactionService 只生成 typed output，手动与自动压缩共享 source validation、真实 request usage record、durable replacement install、ActiveContextTokens recompute、Item lifecycle 和失败顺序。

W 取代 B/L/M/O 中与当前实现一致但与最新 Codex reference 不再一致的 token/compaction 结论；历史阶段状态保持 DONE，但不得通过 alias、wrapper、双写或兼容 decoder 保留旧 `TaskOutput.Usage`、Role/Content replacement、Compactor Rollout writer 或 ContextCompactedEvent 完成协议。

W 的 TokenUsageInfo、typed Compaction domain、source hash、atomic install、failure ordering 和 continuation contract 继续有效；W-03 中 compact assets 属于 ModelMessages/current-prefix Prompt、W-04/W-05 中不区分 pre-turn/manual 与 mid-turn initial-context baseline 的部分由 AA 取代。AA 必须在不回退 W durability/token contract 的前提下完成 Codex compact context placement 与 Resume baseline。

### W-01：Token Usage Protocol + Context Status Contract — `DONE`

- [x] 将 LLM domain 的 `Usage` 收敛为 `TokenUsage`，建立 Codex 风格 `TokenUsageInfo{TotalTokenUsage, LastTokenUsage, ModelContextWindow}`；字段语义在 Responses、Chat Completions 和 compatible Provider 中一致。
- [x] 将 `TokenCountEvent` 改为完整 snapshot，携带 TokenUsageInfo、ActiveContextTokens、estimated marker 和 Provider 实际观察的 history watermark；删除 `Usage + EstimatedInputTokens + ContextWindow` 的优先级猜测模型。
- [x] 增加 typed `ContextWindowTokenStatus`、`context_window_exceeded` Provider error 和 compaction_no_history/stale/invalid_output/insufficient/persistence_failed error contract。
- [x] 将 CompactionTrigger/Reason/Phase 定义为 Agent Protocol 稳定枚举，供 Session、Event 和 CompactedItem 复用；禁止 Rollout 依赖 runtime CompactionService package 或在边界转换字符串。
- [x] 直接重置 TokenCountEvent Rollout codec、fixtures 和本地开发数据到新协议；CompactedItem 的格式替换随 W-03/W-04 同步完成，不保留旧 reader、migration、alias 或 fallback decoder。

### W-02：ContextManager Active Usage + Structured Estimator — `DONE`

- [x] ContextManager 保存最后一个 TokenUsageInfo 与 ActiveContextTokens/estimated snapshot、history version、TokenCount checkpoint sequence 和 rollout source sequence；Resume 使用最后 snapshot 并重算该 sequence 后的 local suffix，不累加所有历史 TokenCountEvent。
- [x] 实现 Session-owned `ContextWindowTokenStatus`：post-response 使用 LastTokenUsage.TotalTokens + local suffix，首次/缺失 usage/replacement 后使用 exact Prompt estimate checkpoint，preflight 与 active budget 由同一 policy 汇合；estimate 不覆盖 LastTokenUsage。
- [x] 以结构化 `ApproxTokenEstimator` 替换 `ConservativeEstimator` 和 Go struct JSON 偶然编码：分别覆盖 text、reasoning、Tool Call/Result、ToolSpec、OutputSchema 和固定协议开销。
- [x] 图片按 prepared dimensions/detail/patch cost 或稳定 fallback 估算并排除 Base64 payload；覆盖 ASCII、中文、high/original image、modality omission 和 tool_output_token_limit 交互。
- [x] 保持当前 total active context scope 和 90% auto limit；不引入 remote compaction、body_after_prefix、fallback compact prompt、window UUID 或 TokenBudget feature。

### W-03：Compaction Domain + Prompt/Replacement Contract — `DONE`，asset/baseline lifecycle 由 AA 取代

- [x] 新建责任独立的 `internal/agent/compact`，定义 CompactionSource/Request/Output 并复用 Protocol 的 Trigger/Reason/Phase；SessionServices 只持有无状态 CompactionService。
- [x] CompactionService 使用 exact PromptSnapshot + capability-only StepContext：保留Session BaseInstructions，将Codex `SUMMARIZATION_PROMPT`追加为最后一个synthetic User item，Tools为空，并复用frozen ModelInfo/Reasoning/ModelClientSession retry policy。
- [x] 摘要请求输入包含模型实际可见的 AGENTS.md、WorldState、Skill、MCP、conversation 和 modality projection；删除只读取裸 ContextProjection 的第二 Prompt 主链。
- [x] Context projector 增加 typed MessageOrigin，CompactionSource 只传递真实 User messages；ReplacementHistory 改为完整 typed ResponseItem，按 Codex 语义从最新真实 User messages 向前选择有界总预算，并以 `User(SUMMARY_PREFIX + summary)` 结束，不用 role/XML 字符串猜测来源。
- [x] CompactionService 只返回包含 Message、FinishReason、ReplacementHistory 和 TokenUsage 的 CompactionOutput，不构造 CompactedItem、TokenCountEvent 或其他 RolloutItem，不访问 Session/ContextManager/TUI。

### W-04：Session-owned Manual Compaction + Atomic Install — `DONE`

- [x] 实现唯一 `Session.runCompaction`/`installCompaction`，由 Session 创建 source snapshot、ItemID、trigger/reason/phase，调用服务，先记录成功 Provider response 的真实 TokenUsage，再校验 summary、finish reason、Tool Call absence、source version/hash 和 typed replacement。
- [x] `/compact` 保持 standalone non-steerable CompactTask，但 Task 只请求 Session 执行 `manual/user_requested/standalone_turn`，不通过 TaskOutput 返回待安装 RolloutItem。
- [x] 将 replacement 的 `CompactedItem + refreshed TokenCountEvent` 作为同一 durable append/flush boundary；成功后才更新 live ContextManager 并发布 TokenCount、ItemCompleted 和 Warning。output invalid/stale/install failure 仍保留先前已持久化的真实 Compaction request usage。
- [x] started、completed、failed、aborted 使用同一 ContextCompaction ItemID；Provider、取消、stale source、invalid output 和 persistence failure 均保持旧 ContextManager，不产生双终态或永久 Working。
- [x] CompactedItem 持久化 trigger/reason/phase、typed replacement、CoveredThroughSequence 和 SourceHash；合法 concurrent trailing facts 保留，旧 prefix 只替换一次。

### W-05：Automatic Compaction + Continuation/Failure Ordering — `DONE`

- [x] 每个成功普通/Compaction request 在下一模型动作前由 Session 只记录一次 TokenUsageInfo；删除 Turn 末尾从 TaskOutput 追加聚合 usage 的路径。
- [x] regular `run_turn` 在 exact StepContext preflight 执行 auto pre-turn compact，并在 sampling/Tool facts 已 canonical record 且确实需要 follow-up 时按 active usage 执行 auto mid-turn compact；不创建嵌套 CompactTask。
- [x] 保持 O 的 steer 顺序：未 drain steer 不进入 compact request；compact 后需要恢复原 model/tool continuation 时继续 pending，只有 steer 需要 follow-up 时可直接 drain。
- [x] Provider context length rejection 使用 typed recovery；compact request 自身超限时按完整 User/Tool-call group 从最旧端有界裁剪，禁止制造孤立 ToolCall/ToolResult。
- [x] compact install 后立即比较 before/after active tokens；没有实质下降或仍达到硬窗口时返回 compaction_insufficient，同一 history/window 不重复无界 compact。

### W-06：Protocol、Persistence、Application + TUI Projection — `DONE`

- [x] ThreadViewSnapshot/fullscreenSessionState/AppExitInfo 使用 TokenUsageInfo 与 ActiveContextTokens；live、attach 和 Resume reducer 只替换 snapshot，不累加 Event 或以 InputTokens 覆盖 estimate。
- [x] 删除独立 ContextCompactedEvent 完成协议；live 使用 ContextCompaction ItemStarted/ItemCompleted，Replay 从 CompactedItem 生成唯一 completed ContextCompactionItem/ContextCompactedCell。
- [x] SQLite `tokens_used` 投影最后一个 TokenCountEvent.Info.TotalTokenUsage.TotalTokens，不重复累加累计 snapshot；durable watermark 顺序保持不变。
- [x] manual/auto compaction 的 Working、retry、completed、warning、failed/aborted 和 statusline context lifecycle 在 live TUI、TranscriptState 与 Resume 中语义一致。

### W-07：Legacy Cleanup、Docs、Guards + Acceptance — `DONE`

- [x] 删除 `TaskOutput.Usage`、Turn usage aggregator、`UsageItem` terminal append、`UsageSnapshot.ProviderUsage/HasProviderUsage`、`PromptSnapshot.NeedsCompaction` 分散 policy 和 TUI/replay usage sum。
- [x] 删除旧 `engine.Compactor.Compact() -> []RolloutItem`、`SessionServices.Compact` wrapper、`compactFunc/compactCallback`、prefix-only `projectCompactionSource`、Role/Content ReplacementMessage、`publishCompactionEvents` 和字符串 no-history 判断。
- [x] 增加 architecture guards，验证 Session/Context/CompactionService 依赖方向、每 request 唯一 TokenUsage、atomic install、typed error、单一 live/replay compaction completion 和旧 symbol 不回流。
- [x] 同步 `docs/design.md`、本进度文档、README/config diagnostics 和必要架构白皮书；历史阶段的 superseded token/compaction 结论不得覆盖 W。
- [x] 运行 focused context/session/compact/provider/TUI/persistence tests、provider mock E2E、`make check`、`go test -race ./... -count=1` 和 `git diff --check` 后才将 W 标记 DONE。

### W 出口

- TotalTokenUsage、LastTokenUsage、ActiveContextTokens 和 EstimatedInputTokens 各有唯一 owner、命名和恢复语义；live、Resume、statusline、exit summary 与 SQLite 不再混用或重复累计。
- manual 与 auto compaction 使用同一 Session.runCompaction/install lifecycle，CompactionService 不拥有 Session mutation、Rollout schema、Event terminal 或 token state。
- compact prompt、typed replacement、source validation、durability、steer ordering、retry/cancel 和 context overflow recovery 与 `docs/design.md` 一致。
- 旧 token aggregator、Compactor Rollout writer、callback、Role/Content replacement、ContextCompactedEvent 和字符串 failure 路径全部删除，不存在新接口包裹旧执行链。

### W 验收

- 多 Step Tool Turn 的 TotalTokenUsage 单调累计、LastTokenUsage 只替换最近 request，Tool/Context local suffix 进入 ActiveContextTokens；Resume 后所有值与 live terminal snapshot 一致。
- ASCII/中文/图片/Tool/Schema prompt estimate 可预测，prepared image Base64 不导致虚假超限；Provider usage 缺失时稳定回退 estimate。
- `/compact` 和 auto pre-turn/mid-turn 生成相同 prompt/replacement contract，成功只产生一个 durable checkpoint 和一个用户可见 Context compacted cell。
- 大 Tool Result、pending steer、source race、context-window rejection、stream retry、取消、invalid summary、flush failure 和 insufficient reduction 都有确定性结果且不丢历史、不重复 compact、不双完成。

### W 完成记录

- 2026-08-25 将 LLM `Usage` 替换为单 request `TokenUsage`，Protocol 引入 `TokenUsageInfo` 与包含 active/estimated/observed-watermark 的完整 TokenCountEvent snapshot；ContextManager、live/Resume、TUI、exit summary 与 SQLite 全部改为 snapshot replace 语义。
- 新增结构化 `ApproxTokenEstimator` 和 ContextWindowTokenStatus，覆盖 ASCII、中文、Tool/Schema、prepared image/原始 Base64 排除、Provider checkpoint、local suffix、失败响应 input baseline 与 Compaction replacement estimate。
- 新建 `internal/agent/compact`，使用普通 BaseInstructions + synthetic Codex summarization User prompt、exact PromptSnapshot items、typed MessageOrigin/真实 User selection、有界 typed ReplacementHistory 和 context-window oldest-group retry。
- Session 现在唯一拥有 manual/auto pre-turn/mid-turn compaction、真实 request usage、source hash/watermark、atomic `CompactedItem + TokenCountEvent` install、before/after reduction、Item lifecycle 和 Warning/terminal ordering；删除 engine Compactor、callback、TaskOutput Usage/Items 和 ContextCompactedEvent。
- W 阶段当时将 Rollout 与 SQLite schema 均提升到 `4`；AA 后续将Rollout替换为v5而SQLite metadata schema保持v4，AC再将当前Rollout/SQLite分别重置为v6/v5。旧开发格式继续直接拒绝且不保留decoder/migration。
- focused functional/race tests、`make check`、全仓 `go test -race ./... -count=1`、Responses/Chat Provider mock E2E、core-tools Provider mock E2E 与 `git diff --check` 于 2026-08-25 通过。

## 26. 当前保留能力

- `amadeus [PROMPT]` 始终启动同一 Fullscreen TUI；非空 Prompt 在 startup/replay barrier 后通过正常 UserMessage lifecycle 自动提交，Turn 完成后 TUI 继续运行。当前不提供独立非交互 Agent frontend。
- 当前配置链和 Provider Adapter 已可使用 OpenAI Responses/Chat Completions 及兼容 Provider。
- JSONL Canonical Rollout + SQLite Metadata Index 已可支持 Session 恢复。
- TUI live projection 与 Resume replay 以当前代码和 `docs/design.md` 为准。
- 内置 Tool、Approval、Diff、Web Search 和 Slash Command 已进入基础主链；N 收敛 `update_plan`、引入 `request_user_input`，并将现有 `/plan` 重构为 Codex 风格 Collaboration Mode 与 Proposed Plan lifecycle；O 已补齐同 Turn 用户输入与 steer lifecycle。
- U 已补齐 Fullscreen next-turn queue 与 Codex-style pre-enqueue footer hint：运行中 Enter steer 当前 Turn，Tab queue 后续独立 Turn，terminal 后按 FIFO 逐条提交，aborted/blocked 路径恢复 composer；queueable draft 期间 fixed statusline 让位给完整/短 Tab hint。
- V 已将用户配置收敛为唯一 versionless strict schema，并将仓库模板统一为 `configs/config.yaml.example`；旧 `version:` 配置直接拒绝且不迁移。
- W 已完成 Context Accounting + Compaction Realignment：TokenUsageInfo/active context/estimate 各有单一语义，manual/auto compaction 共享 Session-owned lifecycle，无状态 CompactionService 不拥有 Rollout、Event terminal 或 Context mutation。
- Prompt 当前仍保留全局 builtin ModelMessages、Base-as-system input、ContextUpdate replace map、短版 Default/Plan 和按 toolNames 拼接 guidance；这些已确认为 AA 范围内的当前架构缺口，不得把 I/N/W 的历史 DONE 解读为已满足 `docs/design.md` 最新 Prompt contract。
- P 已完成 Model Reasoning Effort 与 Provider Thinking Contract；当前生产主链可从配置冻结到 Turn，并贯通普通 sampling、Compaction 与 Provider wire request。
- Q 已完成 Web Search/Fetch 与 `view_image` Contract Closure：Web 保持 pinned network、重定向 Approval、readable Markdown 与证据层级；图片主链完成 model-aware visibility、bounded preparation、Provider/Context projection、单份持久化和 `ViewImageCell`。

## 27. 当前执行规则

1. 每次只推进一个 `TODO`/`DOING` 主任务。
2. `docs/design.md` 与本文都可能存在过期或不完整结论；遇到不确定 Contract 时先分析对应 Codex/Claude Code 源码并结合 Amadeus 范围作出确定性设计，再同步更新两份文档和代码。
3. 每个 Architecture Closure 任务必须在同一任务内完成 ownership 迁移、调用方切换和对应旧主链删除；不接受“新接口包住旧 executor/projector”作为阶段性完成，不保留长期双实现。
4. 任务完成必须运行针对性测试和构建；环境限制导致的测试失败要单独记录。
5. 本文只更新任务状态和出口，不复制架构设计、源码审计或长篇讨论。

## 28. 源码结构清理 — `DONE`（外层 CLI 布局由 X 取代）

### 已完成

- 审计当前目录与 `docs/design.md` 的目标 package 边界，保留 Runtime、Protocol、Tool、Policy、Interface 和 Infrastructure 分层，同时将真实 Application 生命周期从 CLI controller 迁入 `internal/app`。
- 新增 `ThreadWorkspace` 作为当前 Thread 的唯一选择 owner；CLI 不再分别保存 `ThreadManager`、`currentThread` 和锁，Thread 切换、Resume、New Draft、Rename、Delete 与 metadata 查询均通过 Application Service。
- 修正 ThreadManager shutdown 后仅异步移除实例的问题，`ShutdownThread` 现在同步移除已终止 Runtime，立即 Resume 不会重新取得 terminated Thread。
- 将 internal Session 按 runtime loop、TaskHost/history 和 event/request interaction 拆为 `session.go`、`host.go` 与 `interaction.go`。
- 当时将 CLI Composition Root、Turn interface、Fullscreen wiring、interactive command adapter 和 history replay 在扁平 `package main` 内分成独立文件；X 已确认文件级拆分不足以表达真实 package/lifecycle 边界。
- 将 Fullscreen TUI 聚合文件拆为 lifecycle/model、update/input、event projection 和 view rendering，Slash Command 与 selection 保持原有独立文件。
- 将 Approval 核心拆为 types、request、decision 和 port，Presentation 与 Coordinator 保持独立职责。
- 将 FileTools 拆为 read、edit、write 和共享 file-change pipeline，并移除字符串分支入口与未使用的旧 Approval reason helper。
- 扩展 HistoryCell 架构约束测试，使其覆盖所有拆分后的 Application 主链文件。

### 当前仍有效的判断

- `internal/agent/session/session.go` 保留约 500 行的核心状态机与 Turn 生命周期；History/TaskHost 和 Event/Request 已拆出，不再包含互不相关的 adapter 或 persistence 实现。
- `internal/app` 只承载界面无关的 Application Service；X 新增的 `internal/bootstrap` 是 concrete composition boundary，不得变成 Application Service 或通用 Service Locator。

### 被 X/Y 取代的判断

- “Composition Root 固定保留在 `cmd/amadeus/composition.go`”不再是目标；具体装配迁入 `internal/bootstrap` 的窄构造函数，交互 invocation 的启动和关闭唯一归 TUI。
- “`cmd/amadeus` 继续维持扁平 main package”不再是目标；最终目录只保留 `main.go`，Cobra command tree、flags、output 和 exit semantics 迁入 `internal/cli`。
- 28 阶段形成的 `agentController`、`commandRuntime`、混合 `agentInvocation`、CLI-local Event loop 和不可达 Session selector 不构成兼容接口，X/Y 已在调用方切换后直接删除。

## 29. X. CLI + Bootstrap Package Architecture — `DONE`

### 目标

按 `docs/design.md` 第 7 章重构 Amadeus 外层 package：`cmd/amadeus` 缩为 thin process entry，`internal/cli` 拥有 Cobra 参数、管理命令与 dispatch，`internal/bootstrap` 拥有 concrete dependency composition，`internal/interface/tui` 拥有交互启动、renderer、Application 和退出生命周期。X 只调整外层所有权，不重写已经稳定的 Session、ThreadManager、Context、Compaction 或 Tool 语义。

### 已完成

- [x] `cmd/amadeus/main.go` 只建立 process context/标准流、调用 `cli.Run` 并映射退出码；Cobra command tree、flags、config/session/tool/web/version command 和输出语义迁入 `internal/cli`。
- [x] `internal/bootstrap` 按 environment、Adapter、audit、ThreadStore、thread catalog 和 Workspace composition 拆分，并固化部分启动失败的逆序清理与 bounded close。
- [x] `internal/interface/tui/run.go` 拥有 terminal preflight、Thread target、InteractiveApplication/FullscreenApplication construction、renderer drain、terminal restore 和 `AppExitInfo` 返回边界。
- [x] `app.ThreadTarget`/`PrepareStart` 统一 new/latest/explicit resume；无 ID 的 `--resume` 只进入 TUI Session picker，Thread 当前选择唯一归 `app.ThreadWorkspace`。
- [x] 删除旧 `agentController`、`commandRuntime`、混合 invocation、CLI-local Event loop、不可达 selector、无调用 wrapper 和 `internal/interface/cli` 过渡 package；测试迁到 CLI、bootstrap、Application、TUI 和 integration 的实际 owner。
- [x] architecture guard 验证 thin main、依赖方向、唯一 SessionIo consumer 和外层资源关闭顺序。

### X 出口

- `cmd/amadeus` 只拥有进程入口，`internal/cli`、`internal/bootstrap`、`internal/app` 和 `internal/interface/tui` 各自拥有明确边界。
- CLI raw args、TUI options、bootstrap inputs 和 Session configuration 是分层 typed model，不由通用 runtime/controller aggregate 贯穿生命周期。
- TUI 是唯一交互 Agent frontend；Application attachment 是唯一 SessionIo event pump，ThreadWorkspace、Session、Context 和 terminal truth 不产生第二 owner。

### X 完成记录

- 2026-08-25 完成 thin entry、CLI package、bootstrap composition、typed Thread target、TUI startup/exit boundary、旧 main controller 清理和测试 owner 迁移。
- X 实施期间出现过的临时 frontend 划分不属于当前架构，也不保留为历史 API；Y 以单一 TUI 与 initial UserMessage Contract 完成最终收敛。

## 30. Y. Initial Prompt + Single TUI Frontend Alignment — `DONE`

### 目标

按 Codex 根入口、TUI `Cli.prompt`、ChatWidget `initial_user_message`、Session configured、Resume replay 和正常 `submit_user_message` 源码 Contract，将 Amadeus 根 positional 定义为 TUI initial Prompt：`amadeus [PROMPT]` 始终启动同一 Rich TUI，Prompt 由 Fullscreen model 作为 pending UserMessage 持有，并在 active Thread configured、snapshot/replay 和 startup surface ready 后通过普通 user-message/admission 链 take-once 提交。当前范围只有这一套交互 frontend、Approval/UserInput overlay 和 SessionIo consumer。

Y 不改变 `run_turn`、Tool/Approval 内层、steer/admission、NextTurnQueue 或 Fullscreen exit 的既有生命周期；它统一 frontend、UI message 数据模型、启动时序、命名与测试 owner，并让 initial/normal/steered UserMessage 在 Interface→Session→canonical history 中保存同一非空 Text，而不是在下层重复 trim。

### Y-01：CLI `PROMPT` + Single TUI Dispatch — `DONE`

- [x] 将 root usage、字段和错误文本从 `amadeus [task]`/Task 改为 `amadeus [PROMPT]`/Prompt；CLI dispatch 只做 CRLF/CR→LF 归一化并保留其他文本，exact empty Prompt 表示无 initial message，不在 CLI 构造 UserInputOp。保留 Amadeus 既有 bounded message size 作为产品约束。
- [x] 删除按 stdin/TTY 选择不同 Agent frontend 或 Approval 协议的分支；无 subcommand 时统一调用 TUI runner，非 TTY 由 TUI terminal preflight 明确失败。
- [x] 保持 config/project/session flags 与 management subcommands；latest/explicit resume 可以携带 Prompt 并先恢复目标 Thread，无 ID picker 不同时接收 Prompt。CLI positional Prompt 不经过 Slash Command parser。

### Y-02：TUI `UserMessage` + Naming Realignment — `DONE`

- [x] 在 `internal/interface/tui/user_message.go` 建立当前范围最小 `UserMessage{Text}`；`tui.RunOptions.Prompt` 在 TUI boundary 转换为 `*UserMessage`，`FullscreenOptions`/`fullscreenModel` 使用唯一 `InitialUserMessage`/`initialUserMessage` owner。
- [x] 直接将 `TaskSubmission`、`prepareTaskSubmission`、`submitTask` 改为 `UserMessageSubmission`、`prepareUserMessageSubmission`、`submitUserMessage`；不保留 alias、wrapper 或双方法。
- [x] 将 `QueuedUserInput` 统一为 Codex 术语 `QueuedUserMessage` 并持有 UserMessage；Runtime `UserInputOp` 保持 Protocol boundary。将 `MaxTaskBytes`/`maxTaskBytes` 和相关文案改为 `MaxUserMessageBytes`/`maxUserMessageBytes`。
- [x] Prompt 不进入 `FullscreenStartup`、InteractiveApplication、ThreadWorkspace、Session Configuration、Protocol Event 或 Rollout；当前未支持的 CLI image/TextElement/mention 不增加占位字段。
- [x] 移除 InteractiveApplication/Session/canonical UserMessage 对非空 Text 的二次 TrimSpace；exact empty 仍拒绝，Composer 自身 parse policy保持独立，initial Prompt、ResponseUserMessage 和 completed UserMessage Item 保存一致文本。

### Y-03：Configured/Replay-gated Initial Submission — `DONE`

- [x] 利用现有 `ThreadManager.spawn` 等待 `SessionIo.Configured` 和 `InteractiveApplication.Start` 完成 canonical snapshot/replay/attachment 的同步 barrier，不复制 Codex app-server 专用的第二份 configured queue。
- [x] Fullscreen model 保存 pending initialUserMessage；Bubble Tea `Init` 只发送无 payload startup-ready lifecycle message，`Update` 调用 take-once `submitInitialUserMessageIfPending`。不得把 Prompt 数据搬进 tea.Msg、goroutine closure 或 timeout。
- [x] initial message 复用普通 Composer submit：当前 attachment generation 下生成 ClientUserMessageID、写 input recall、插入 optimistic UserMessageCell、调用 SubmitUser、等待 Started/Steered/rejection，并由 canonical completed UserMessage 确认去重；Application 不对 UserMessage 做第二次 trim/rewrite。
- [x] explicit resume/continue 必须先展示 replay history 再显示/提交 initial message；protected startup surface 或 direct-input block 时继续 pending或恢复 Composer。失败不得丢消息、自动入 NextTurnQueue、创建 InitialPromptOp 或退出 TUI。

### Y-04：Single Frontend Cleanup + Test Ownership — `DONE`

- [x] 删除被取代的非交互 Agent frontend、独立 renderer、terminal prompt/scanner 和相关 DTO/helper/test；Fullscreen Approval 与 RequestUserInput overlay 保持唯一人工交互 owner。
- [x] root/CLI tests 验证无 Prompt/有 Prompt/latest/resume 都只产生 TUI RunOptions，非 TTY 由 TUI preflight 拒绝。
- [x] 将 Provider/Core Tools/Skill/MCP/Web 跨层 E2E 迁入明确的 test-only integration package，通过 `InteractiveApplication` 驱动真实 Runtime 和 typed Approval/UserInput response；不得为保留测试而建立生产 single-turn runner。
- [x] 更新 architecture guards：`cmd/amadeus` thin entry 保持不变；生产代码只有 TUI Agent frontend、Fullscreen interaction owner 和一个 SessionIo consumer。

### Y-05：Lifecycle Tests、Docs + Acceptance — `DONE`

- [x] 覆盖 initial UserMessage take-once、CRLF normalization/非空文本保持、普通 Fresh、latest/explicit Resume replay-before-prompt、optimistic/canonical dedupe、submission rejection restore、literal `/compact` prompt、attachment generation、picker exclusion、startup cancellation 和 exit-after-turn-stays-in-TUI。
- [x] 覆盖 normal Composer、same-turn steer、Tab queue、Plan mode、Approval、request_user_input、Resume/Clear 和 shutdown 不因 UserMessage 命名迁移回退；验证 initial message 与普通 message 使用同一 submission/admission helper。
- [x] 同步 `docs/design.md`、本进度文档、README 和 architecture whitepaper，明确 Amadeus 当前只有单一 TUI Agent frontend。
- [x] 运行 focused CLI/TUI/Application/integration tests、Provider/Core Tools mock E2E、PTY initial prompt smoke、`make check`、`go test ./... -count=1`、`go test -race ./... -count=1`、`go build ./cmd/amadeus` 和 `git diff --check` 后才将 Y 标记 DONE。

### Y 出口

- `amadeus` 与 `amadeus "PROMPT"` 只有一套 TUI frontend、一个 active attachment event pump 和一套 Approval/UserInput overlay；TTY 只用于 terminal preflight，不改变安全协议。
- CLI Prompt、TUI UserMessage、pending initialUserMessage、UserMessageSubmission、QueuedUserMessage 和 Runtime UserInputOp 各有准确 owner；未提交 Prompt 不进入 canonical state，提交后只走普通 UserMessage lifecycle。
- Fresh configured、Resume replay、startup readiness、optimistic projection、admission、失败恢复和 TUI continued-running 顺序与 `docs/design.md` 的 Codex-aligned Contract 一致。
- 被取代的 frontend 和相关测试装配全部删除；跨层 E2E 不依赖生产第二 frontend。

### Y 完成记录

- 2026-08-26 将根入口统一为 `amadeus [PROMPT] → TUI`；CLI 只规范化 CRLF/CR 并传递 Prompt，不读取 stdin task，也不依据 TTY 改变 Approval 行为。
- 新增 TUI-owned `UserMessage`/`initialUserMessage`/`UserMessageSubmission`，将 `QueuedUserInput` 改为 `QueuedUserMessage`，并把 MaxTaskBytes 术语统一为 MaxUserMessageBytes；UI 与 Runtime UserInputOp 保持明确边界。
- Fullscreen model 通过 startup-ready typed message take-once 提交 initial UserMessage；Fresh 使用 configured/snapshot barrier，latest/explicit Resume 先 replay，随后复用普通 optimistic、ClientUserMessageID、SubmitUser 和 admission lifecycle。literal Slash Prompt 不进入 Slash dispatcher，拒绝时恢复 Composer。
- InteractiveApplication、Session、ResponseUserMessage、completed UserMessage Item 和 Resume projector 不再裁剪非空 Text；CLI Prompt 除换行规范化外原样进入同一 canonical lifecycle。
- 删除被取代的非交互 frontend、独立 renderer、独立 Approval/UserInput adapter 和第二 SessionIo consumer；Fullscreen Approval/UserInput overlay 成为唯一人工交互 frontend。
- Provider/Core Tools/Skill/MCP/Web/Resume E2E 迁到 `internal/integration`，通过 test-only InteractiveApplication harness 驱动真实 Runtime；不存在为测试保留的生产 single-turn runner。
- initial take-once、replay ordering、literal Slash、rejection restore、picker exclusion、文本保持、CLI dispatch 和 Unix PTY startup/continued-interactive smoke 已覆盖；`make check`、全仓 `go test -race ./... -count=1` 和 `git diff --check` 通过。

## 31. Z. Codex-aligned Internal Package + Source Layout — `DONE`

### 目标

按 `docs/design.md` 第 7 章和当前 Codex/Claude Code 源码重新收敛 `internal`：Codex 的 `protocol`、core `session/state/tools/context_manager`、`thread-store`、`rollout`、`tui` 和 `cli` 决定外层 owner 与依赖方向；Claude Code 的 `Tool`、tool orchestration、permissions 和 builtin tools 决定 Tool 内层 Validate/Prepare/Permission/Approval/Execute 与文件工具职责。迁移以 Go 的无环 package、同 package 责任文件和当前基础产品规模实现，不逐字复制 Rust crate 或 TypeScript 一 Tool 一目录。

Z 只改变 package/file ownership、命名、依赖方向和测试归属，不改变 Protocol wire shape、Rollout schema、SQLite schema、配置、Prompt、Tool schema、Approval 文案、TUI 交互或 Runtime 行为。任一行为 Contract 问题若在迁移中暴露，必须先回查参考源码、更新 `docs/design.md` 并在同一阶段明确纳入，不能借目录整理静默修改。

Z-01～Z-09 是同一次 Architecture Closure 的顺序分解，不是兼容阶段。迁移可以在工作分支中分步编译，但 Z 完成时生产与测试代码只能引用目标路径；禁止建立旧 import path forwarding package、type alias facade、双注册表、双 event reducer 或旧/新 ThreadStore adapter 链。

### Z-01：Protocol + Identity Package Boundary

- [x] 将 `internal/agent/protocol` 迁为顶层 `internal/protocol`，保留 `protocol/identity` 的无环低层 identity boundary，并迁移全部 production/test import。
- [x] 按 submission、session events、turn events、approval/input events、item lifecycle、scope/codec 拆分当前泛化 `protocol.go`；Protocol 只保留 identity、DTO 和 contract，不吸收 TUI reducer、Context projection 或 Runtime service。
- [x] 审计 `TurnItem`/ToolResult 等跨边界类型的依赖方向，保持现有 wire DTO 单一 owner且未把 builtin/runtime implementation 搬入 Protocol；更深的 Tool payload 解耦不属于本次无行为 schema 迁移。
- [x] 删除旧 `internal/agent/protocol` 目录和 architecture path exceptions，不保留 alias/import forwarding。

### Z-02：Session State + ContextManager Ownership

- [x] 将 `internal/agent/turn` 的 TurnContext、ModeKind、Personality 迁入 `internal/agent/session`；TUI/Application 只需要 Collaboration Mode 时直接使用 Protocol 类型，不依赖单类型 Turn package。
- [x] 将 StepContext、TurnBudget、model completion persistence、Proposed Plan stream 和 Tool lifecycle observer 从 `internal/agent/engine` 迁回 Session-owned 责任文件；对齐 Codex `session/step_context.rs`、`session/turn.rs` 和 `tools/events.rs` 的 owner。
- [x] 将 `internal/context`/`package agentcontext` 统一为 `internal/contextmanager`，按 manager/prompt snapshot/projection/world state 责任文件组织；Session 仍是唯一 mutation caller，Resume 与 compaction 语义不变。
- [x] 拆分 Session 聚合文件：`session.go` 只保留核心模型/constructor，serial loop、submission、request dispatch、turn start、service builder、persistence/context query 分别进入责任名文件；未创建会反向依赖 Session 的 `agent/state` package。

### Z-03：ModelClientSession + Agent Engine Removal

- [x] 新建窄 `internal/agent/modelclient`，只迁入 ModelClientSession、Sample/Complete、response stream consume、idle timeout、retry/reconnect 和 typed transient StreamError 发布。
- [x] `internal/llm` 继续拥有 Provider-neutral request/response/stream port，`internal/llm/openai` 继续拥有 wire adapter；modelclient 不接管 Provider configuration 或 Prompt/Tool ownership。
- [x] 将 Session service builder 中 concrete OpenAI adapter fallback 移到 `internal/bootstrap` ClientFactory composition；Runtime 只消费注入的 Provider-neutral factory，不反向依赖 `internal/llm/openai`。
- [x] 将 Tool runtime construction 移入 Session service builder，将 Tool Event/Plan completion/persistence 移入对应 Session/Tool owner后删除整个 `internal/agent/engine`。
- [x] 删除无调用的 `PlanModeTools`、ToolExecutionService convenience method 等 dead API；没有为旧 engine tests 建 wrapper。

### Z-04：ThreadStore + ThreadManager Boundary

- [x] 将 `internal/thread`、`internal/thread/local`、`internal/state` 和 `internal/state/sqlite` 收敛为 `internal/threadstore`、`threadstore/local` 与 `threadstore/local/sqlite`；StoredThread、ListQuery 和 metadata DB port 归 ThreadStore domain。
- [x] 将 `internal/thread/manager` 提升为 `internal/threadmanager`，对齐 Codex core ThreadManager/CodexThread 与独立 thread-store crate；ThreadManager 是唯一 Session spawn/live registry owner。
- [x] 将原 `manager.go` 拆为 manager registry 与 `amadeus_thread.go`，将 LocalThreadStore 拆为 store/writer/metadata/index；保持 JSONL durability、SQLite watermark/rebuild、child restore 和 shutdown failure order。
- [x] 删除含义过宽的 `internal/state` 和旧 nested manager/local 路径，不保留第二套 metadata port。

### Z-05：TUI Package + Projection Ownership

- [x] 将唯一 frontend 从 `internal/interface/tui` 迁为顶层 `internal/tui`，删除空 namespace `internal/interface`；CLI/bootstrap/import guards 同步迁移。
- [x] 将 `FullscreenApplication`/`fullscreenModel`/`fullscreenSessionState` 收敛为 package-local `Application`/`appModel`/`sessionViewState`；TTY preflight、AppExitInfo、renderer drain 和 single frontend Contract 不变。
- [x] 将只被 TUI 使用的 `internal/app/transcript` reducer 迁入 TUI projection，区分 `protocolEventState` 与视觉 active-cell state；`internal/app` 不保存 HistoryCell/TUI reducer。
- [x] 按 Codex App/ChatWidget/history_cell module 责任拆分 event reducer、history state、composer/transcript view、History render、Explore/Exec/Web tool cells；保持一个 Go package 共享 Bubble Tea model，未建立人工 widget subpackage。

### Z-06：Codex + Claude Code Tool Boundary

- [x] 保留 `internal/tool` + `internal/policy` + `internal/tool/builtin` 三层：generic Tool contract/router/execution、Permission/Approval、具体 builtin Tool；不复制 Claude Code 的一 Tool 一 Go package。
- [x] 移除 generic `internal/tool/presentation.go` 对 read/web/MCP/Multi-Agent 等完整工具名的枚举；具体 presentation 归 Session Tool Event policy，generic Tool 不再认识产品 catalog。
- [x] 将 `execution_service.go` 按 single-call lifecycle、batch concurrency、outcome mapping 拆为 `execution_service.go`、`execution_batch.go`、`execution_outcome.go`，保护 Normalize→Validate→Prepare→Permission/Approval→Execute 顺序和 deterministic completion order。
- [x] 将 `core.go`/`catalog.go` 改为 `core_registry.go`/`target_catalog.go`；`grep`、`execute_command` 仅在存在独立 parser/executor 等真实职责时在同 package 拆文件。

### Z-07：Capability File Cohesion

- [x] `mcp/runtime.go` 按 orchestration、connection lifecycle、binding snapshot 拆分；未拆第二 MCP runtime package。
- [x] `skill/catalog.go` 按 catalog、discovery、parser、revision 拆分；Skill metadata/resource/settings owner 不变。
- [x] `websearch/providers.go` 拆为 provider factory 与 DuckDuckGo/Tavily/SearXNG/Brave 文件；共享 HTTP/result contract 未复制。
- [x] `app/interactive_application.go` 按 application、commands、thread switching、attachment pump、capability queries 拆分；唯一 SessionIo consumer 不变。
- [x] 将 `rollout/items.go` 的 scope/clone helper 拆到 `item_scope.go`，将 `project/filesystem_policy.go` 的 canonical root/path helper 拆到 `filesystem_paths.go`；其余文件经职责审计后未机械按行数切割。

### Z-08：Dead Boundaries + Naming Cleanup

- [x] 删除空 `internal/agent/plan`、`internal/agent/task`；删除无生产调用方且已属明确非目标的 `internal/sandbox`。
- [x] 审计 package path 与 declared package name，消除 `context → agentcontext`、nested `manager → threadmanager` 等错位。
- [x] 审计通用文件名并将模糊 DTO 文件改为 `contract.go`、`domain.go`、`document.go`、`binding.go`、`change.go` 等责任名；`manager.go`、`runtime.go`、`service.go`、`store.go` 仅在确实表达 package 核心 owner 时保留，未制造一类型 package。
- [x] `go list ./internal/...` 只包含 `docs/design.md` 目标目录；production/test imports 零旧路径，architecture whitepaper 已同步新 owner，设计/进度文档中的旧路径仅保留为明确迁移历史。

### Z-09：Architecture Guards + Acceptance

- [x] 将单个 `internal/architecture/guard_test.go` 按 legacy、Runtime、Protocol/Persistence、Tool/Provider、TUI 和 shared helper 拆分；旧路径检查改为新 owner，避免单文件成为第二份进度文档。
- [x] 增加 AST package dependency guards：Protocol 为低层 contract；ThreadStore 不依赖 Session；ThreadManager 唯一 spawn Session；generic Tool 不依赖 builtin/TUI；TUI projection 不位于 App；Runtime 不依赖 CLI/bootstrap/TUI。
- [x] 迁移测试到 Protocol、ContextManager、ModelClient、ThreadStore/Manager、TUI、Tool 等新 owner，并通过 live/Resume、compaction、stream retry、same-turn steer、next-turn queue、Approval/request_user_input、Multi-Agent、Tool batch order 和 Thread durability 现有 contract tests。
- [x] 运行 focused tests、`go test ./... -count=1`、`go test -race ./... -count=1`、Provider/Core Tools mock E2E、`make check`、`go build ./cmd/amadeus`、`git diff --check`；同步 design/progress/whitepaper 后将 Z 标记 DONE。

### Z 出口

- 顶层 package 与 Codex owner 对齐，Tool 内层同时保持 Claude Code contract；不存在仅靠名字相似的 facade 或旧路径兼容层。
- Session/ContextManager/ModelClient、ThreadManager/ThreadStore、Protocol/Rollout、Application/TUI 各有单一 owner 和无环依赖方向。
- 大 package 通过同 package 责任文件表达 Codex private modules；目录只表示稳定 domain/runtime/adapter boundary，不按行数或参考语言语法机械拆分。
- 所有现有基础 Agent 行为、Protocol/Rollout/SQLite schema 和用户界面保持等价，旧目录、dead package、重复 projection 和宽泛文件 owner 全部清理。

### Z 完成记录

- 2026-08-26 将公共协议提升为 `internal/protocol` 并按 submission/event/session/turn/approval/scope 拆分；ContextManager、ModelClientSession、ThreadStore/ThreadManager 和 TUI 分别迁入目标顶层 owner，旧 `agent/engine`、`agent/turn`、`state`、`interface`、dead sandbox 与空 package 物理删除。
- Session 吸收 Codex 风格 TurnContext/StepContext/Tool Event/Plan completion owner，模型 stream/reconnect 收敛到窄 `agent/modelclient`；OpenAI concrete factory 回到 bootstrap，Runtime 不依赖 adapter。
- Thread persistence 收敛为 `threadstore` + `threadstore/local/sqlite`，Thread runtime registry 收敛为 `threadmanager`；Manager/AmadeusThread、Store/writer/metadata/index 各按职责拆分并保持 durability/shutdown/child restore ordering。
- TUI 迁为单一顶层 package，删除 Fullscreen 分支命名和 Application-owned transcript；event reducer、history state、composer/transcript view、History render 与 Explore/Exec/Web cells 分文件共享同一 Bubble Tea model。
- Tool 保持 Codex Router/Event 外层和 Claude Code Validate/Prepare/Permission/Approval/Execute 内层；generic presentation 的具体工具名枚举迁回 Session Tool Event，ExecutionService 拆为 single-call/batch/outcome，文件 Tool/Approval 行为与 E2E 保持不变。
- MCP、Skill、WebSearch、Application、ContextManager、Rollout 和 FileSystemPolicy 聚合文件按真实职责拆分；architecture guards 分域并新增 AST import dependency checks。最终 `make check`、全仓 functional/race tests、Responses/Chat Provider/Core Tools E2E 与 `git diff --check` 全部通过。

## 32. AA. Prompt Ownership + Lifecycle Realignment — `DONE`

### 目标

按 `docs/design.md` 第 12 章、当前 `../codex-main` Prompt/ToolSpec/WorldState/Compaction 源码和 `../claude-code-main` FileRead/FileEdit/FileWrite/Glob/Grep Tool 源码，替换 Amadeus 当前“全局 builtin ModelMessages + ContextUpdate replace map + Snapshot 前置 developer strings + short ToolSpec/额外 Tool Markdown”的 Prompt 主链。AA 同时收敛 Prompt 来源、角色、顺序、wire mapping、ToolSpec、persistence、compaction 和 Resume；不保留旧 ContextUpdateEvent、按 toolNames 拼接 guidance、短版 Plan suffix 或 Base-as-system input compatibility。

AA 不复制 Codex Remote ModelsManager、Apps/Plugins/Realtime、Remote Compaction、TokenBudget window identity 或未实现 Tool。文件/搜索 Tool 以 Claude Code 为主要行为参考；Runtime/Image/Multi-Agent/MCP Resource Tool 以 Codex ToolSpec 为骨架；Web、Skill 和 lazy MCP wrapper 以 Amadeus 已实现 Contract 为权威。所有 Tool guidance 必须归入对应 ToolSpec owner；外层 Prompt 生命周期、ModelMessages、WorldState、Default/Plan、Compact 和 Provider wire 以 Codex 为准。

AA-01～AA-09 是同一次 Architecture Closure 的依赖顺序，不是可长期共存的兼容阶段。若 WorldState/Rollout 当前格式无法表达目标 contract，直接替换 schema、codec、fixture 和本地开发数据，不新增双读、migration、alias 或 fallback decoder。

### AA-01：Reference Pinning + Prompt Source Matrix — `DONE`

- [x] 本地参考仓库不含 `.git` metadata，因此不虚构 commit；使用 snapshot date + source path + SHA-256 固定 model instructions、Default/Plan、compact prompt/prefix、Multi-Agent role 及内置 Tool Prompt/ToolSpec source。
- [x] 建立 source matrix：Claude Code file/search、Codex runtime/image/multi-agent/MCP resource、Amadeus-specific web/skill/lazy MCP；每个 Tool 明确权威行为、允许差异和禁止复制的未实现能力。
- [x] 审计当前每个内置 Tool 的 description、input property semantics、required/optional、visibility、parallel behavior、Permission/Approval 和 output shape，记录 Prompt 与 Runtime 不一致项并关闭 `edit/write` complete-read gap。
- [x] 建立“pinned source + explicit patch manifest + generated/golden fixture”规则；Base、Collaboration Mode、ToolSpec、Compaction 和 Multi-Agent role 分别 revision，不使用全局复合 hash。

### AA-02：Session BaseInstructions + Provider Wire Contract — `DONE`

- [x] 将 `BaseInstructions` 扩展为 exact text + `custom|model{slug}` provenance；Session 创建优先级固定为 optional explicit custom override > resumed SessionMeta > selected ModelInfo template，不为 AA 单独增加无调用方的公开配置。
- [x] 将 resolved Base 归 SessionState/SessionMeta 所有，Resume 恢复 exact text/provenance；StepContext 不再每个 request 重新解析 Base，模型/Personality 更新不得静默改写已有 Thread。
- [x] 修正 Provider wire：Responses 使用独立 `instructions` 字段且 input 不含 synthetic system Base；Chat Completions 由 Dialect 生成唯一 Base 前缀。删除通用 `InputMessages()` Base 注入语义。
- [x] 提供按 model profile 选择的 pinned Base catalog；neutral fallback 只描述通用 coding agent，不错误声称 GPT/Go 实现身份。
- [x] 增加 Responses/Chat/兼容 Dialect golden tests、new/resume Base precedence tests 和 unknown-model neutral fallback tests。

### AA-03：ModelMessages + Base Layer + Default/Plan Assets — `DONE`

- [x] 将 `ModelMessages` 收敛到 Codex 对应字段：instructions template/variables、approvals、permissions、collaboration modes、multi-agent；Approval/Permission 文案仍服从 Claude Code 内层 Contract；移除 `SummarizationPrompt`、`SummaryPrefix` 和平行 `SubagentDeveloperInstructions` owner。
- [x] 明确 Amadeus 只有 Default/Plan 两个 Collaboration Mode；BaseInstructions 是二者共享的稳定层而不是第三种模式。跨模式规则归 Base，执行/规划差异分别归 Default/Plan。
- [x] 用 pinned Codex Default template 替换 `Execute Mode/Think→Analyze→Act→Observe`，用 pinned conversational Plan template 替换五行 asset 和 Go hard-coded suffix；每个 Step 只注入当前模式的一份完整文本。
- [x] 通过 manifest 记录 Amadeus Plan Tool mask 差异；Default/Plan 不描述 Tool 参数或拼接 Tool guidance，也不宣称未暴露的 command capability。
- [x] 对 Base、Default、Plan 建 exact source/manifest snapshot tests，禁止退回 `strings.Contains` 级完整性检查。

### AA-04：ToolSpec Model Contract + Tool Prompt Migration — `DONE`

- [x] 将 Tool 模型合同收敛为 name +完整 description + property descriptions/required semantics + optional OutputSchema/Strict + request-scoped visibility；Strict/OutputSchema逐 Tool、逐 Provider 验证，不全局开启。
- [x] 将 Claude Code `FileRead/FileEdit/FileWrite/Glob/Grep` guidance按 Amadeus真实能力迁入对应 ToolSpec；删除图片/PDF/Notebook、mtime排序、multiline/output modes等虚假能力，并明确 `edit/write` 需要 complete non-truncated read。
- [x] 将 Codex `update_plan/request_user_input/exec/write_stdin/view_image`、Multi-Agent和MCP Resource guidance迁入对应 ToolSpec，但保留 Amadeus实际字段、process identity、Approval和single-environment边界，不机械复制 schema。
- [x] 为 `web_search/web_fetch/read_skill/mcp_list_tools/mcp_call` 建立 Amadeus-specific ToolSpec fixture，保持 evidence、bounded resource、lazy discovery、catalog revision和untrusted result语义。
- [x] 删除 `ToolPromptOrder`/`toolGuidance(toolNames)`及独立 Tool Markdown→Collaboration Mode装配链；同一工具不得保留短 ToolSpec + 第二份developer guidance。
- [x] 增加逐 Tool golden/behavior tests，验证description、schema、visibility variant、Validate/Prepare/Execute、Permission/Approval和ToolResult shape一致。

### AA-05：Typed WorldState + Context Fragment Model — `DONE`

- [x] 用 stable section ID + typed snapshot + `Absent/Unknown/Known` diff contract 替换 `UpdateKey/contextUpdateState` 和 universal marker switch。
- [x] 实现基础 section：model、可变 personality、collaboration mode、AGENTS.md、environment、permissions、skills catalog、multi-agent role/mode；每个 fragment 自有 role、markers、content kind 和 separate-message policy。
- [x] 增加 `WorldStateItem{full|patch}` durable contract 和 ContextManager baseline；模型可见 fragment 先 canonical append，随后 persistence snapshot/patch，失败时 baseline 不前移。
- [x] 删除 `ContextUpdateEvent` codec/scope/projector、Snapshot 前置 updates map、`ContextUpdate()` query 和旧 revision/hash 兼容路径。
- [x] 覆盖 full/diff/no-change/replacement/removal、retained-history fallback、fragment merge/order、CRLF-stable hash 和 persistence failure ordering。

### AA-06：Turn/Step Assembly + AGENTS.md/Skill Lifecycle — `DONE`

- [x] 调整首次 Turn 顺序为 full initial context → WorldState full → TurnContext reference → real User input → input-scoped explicit Skill/context items → sample；不得让 User input 先于其生效的 instruction/environment context。
- [x] 每个 Model Step 先 capture exact StepContext，再从同一 snapshot build WorldState、record diff，最后构造 PromptSnapshot；Prompt/ToolSpecs/dispatch 共用 frozen ToolRouter。
- [x] 将 AGENTS.md 改为 Codex contextual user fragment并实现 replacement/removal；available Skill catalog归 developer WorldState，显式 Skill正文归 canonical user `<skill>` item。
- [x] 删除 `prepareStaticTurnContext`、第二次 mutable Registry/Tool list、`prepareInputContext` replace-state 和 request-only budget developer append；runtime reminder改为 typed canonical fragment。
- [x] Prompt revision/preflight覆盖 exact Session Base、normalized history、ToolSpecs、OutputSchema和WorldState/TurnContext baseline；contextual AGENTS.md/Skill/SubAgent notification不进入真实User compaction selection。
- [x] 覆盖首轮/第二轮/mid-turn continuation/steer、mode change、permission grant、nested AGENTS、explicit Skill和 failure-before-baseline tests。

### AA-07：Multi-Agent Prompt Ownership — `DONE`

- [x] 将 child role instructions 迁入 `ModelMessages.MultiAgent.Role.Subagent` 或等价 Codex-shaped field，通过独立 developer fragment注入，删除平行 `SubagentDeveloperInstructions` 字段和 tool-name guidance append。
- [x] 将 active subagents 保持为 environment WorldState diff，将 mode/role instruction 与 transient status列表分离；状态不变不重复注入。
- [x] 保持 read-only explorer ToolRouter/policy、fresh child context、Root/child权限隔离与 bounded notification，不扩大到 Codex V2/fork/write-capable worker。
- [x] 覆盖 root/child initial context role/order、status transition、Resume、ToolSpec一致性和禁止 nested/interactive Tool。

### AA-08：Compaction Phase + Resume Baseline Alignment — `DONE`

- [x] 将 `SUMMARIZATION_PROMPT`/`SUMMARY_PREFIX` 移出 ModelMessages，使用独立 exact assets；`CompactionMessages` 不再用 `BaseInstructions` 类型包装 synthetic User文本。
- [x] 保持 W 的 source hash、真实 User selection、TokenUsage、atomic install和 failure ordering；compact request继续使用 Session Base、exact PromptSnapshot、Tools none。
- [x] manual/pre-turn install summary replacement并清空 WorldState/TurnContext reference，使下一正常 Turn full reinject；mid-turn将重新渲染的 full initial context插在最后真实 User/summary之前并安装新 baseline。
- [x] Rollout reconstruction恢复 Session Base、Compacted replacement、WorldState baseline和TurnContext reference；live/Resume下一 request byte/semantic equivalent。
- [x] 覆盖 manual、auto pre-turn、auto mid-turn、summary-last placement、context change during compact、install failure、asset update after Resume和truncated-tail recovery。

### AA-09：Cleanup、Guards + Acceptance — `DONE`

- [x] 删除旧 builtin agent/execution/handoff短版组合器、Mode Go suffix、global prompt revision、ContextUpdateEvent/update map、tool guidance append、已迁入 ToolSpec 的独立 Tool Markdown、request-only developer injection和相关 compatibility tests。
- [x] 更新 design/progress/README/architecture whitepaper与debug输出，明确 source revision、Base provenance、WorldState baseline和Prompt wire shape。
- [x] 增加 architecture guards，禁止 Base-as-input-system、Step-time Base resolution、`ContextUpdateEvent`、`ToolPromptOrder`、`toolGuidance`、ToolSpec之外的第二份Tool guidance、`SubagentDeveloperInstructions`、`SummarizationPrompt` in ModelMessages和生产 `strings.Contains` Prompt完整性测试回归。
- [x] 运行 focused prompt/tool/context/session/compact/provider/multi-agent/persistence tests、Responses/Chat/Core Tools provider mock E2E、`make check`、`go test ./... -count=1`、`go test -race ./... -count=1`、`go build ./cmd/amadeus`和`git diff --check`后才标记 AA DONE。

### AA 出口

- BaseInstructions 在 Thread 生命周期内有唯一 exact owner/provenance，Provider wire、Resume和模型切换语义与 Codex一致。
- WorldState current state、model-visible fragments、durable full/patch baseline和ContextManager history使用同一 typed lifecycle；没有 replace-prefix side map。
- BaseInstructions 稳定层、Default/Plan 两个模式、Compact request、Summary Prefix、三类 ToolSpec guidance和Multi-Agent role有明确上游source、允许差异和独立revision，不存在重复文本owner或第三种模式。
- Default、Plan、Tool continuation、Compaction和Resume共享同一Prompt assembly/ordering contract；现有Tool/Approval、W token/durability和N Plan interaction协议不回退。

### AA 完成记录

- 2026-08-27 完成 Prompt source manifest及其与全部可用内置 Tool 的集合等价守卫、Session-owned exact Base/provenance、Responses `instructions`/Chat single-prefix wire、Codex Default/Plan/compact assets及独立职责 revision；summarization prompt与summary prefix不再共享聚合revision，基础身份不再声明 GPT 或 Go 实现身份，Amadeus 仅有 Default 与 Plan 两个 Collaboration Mode。
- Tool guidance 收敛到 source-specific ToolSpec：Claude Code file/search、Codex runtime/image/multi-agent/MCP resource、Amadeus web/skill/lazy MCP；删除第二份 Tool Markdown/guidance owner并接通 `OutputSchema/Strict` transport。FileReadState按同一内容指纹累积无gap分页coverage，只有全部非截断行被模型观察后才允许edit/write，关闭大文件永远无法满足complete-read的死循环。
- WorldState 使用 typed section + canonical fragment + durable `ContextKind` 与 Absent/Unknown/Known full/patch baseline；换行在 owner入口规范化，首次输入、truncated-tail recovery、后续 diff、explicit Skill、SubAgent role/status、manual/pre-turn/mid-turn compaction 与 Resume 使用同一 ContextManager lifecycle。
- StepContext 只冻结 Model、ToolRouter、LoadedAgentsMd、atomic Skill metadata/revision、Permission profile/grants、active SubAgents和capability revisions；Session 在 WorldState 持久化后独立构造本次 PromptSnapshot，sampling、preflight、token watermark和compaction显式消费同一快照。Runtime budget reminder以带marker和durable ContextKind的canonical developer context在preflight前记录，不再做request-only append。
- `/status` 增加只读 Prompt diagnostics，展示 Base provenance、WorldState baseline/revision、Provider wire和各职责资产的短revision，不建立第二份Prompt状态。
- AA当时将Rollout提升为v5并拒绝旧格式decoder，SQLite metadata index保持v4；AC因TurnComplete/AgentSpawnEdge contract替换将当前Rollout/SQLite分别提升为v6/v5。两者版本域不同，不要求同步递增，也不提供开发期兼容migration。
- 验收通过：`make check`、全仓 `go test -race ./... -count=1`、Responses/Chat Coding Agent与Core Tools Provider mock E2E、architecture guards及`git diff --check`。

## 33. AB. Source-backed Markdown Streaming + TUI Render Lifecycle — `DONE`（2026-08-28 reopened and completed）

### 目标

按`docs/design.md`第19.3节和当前`../codex-main/codex-rs/tui/src/{insert_history.rs,tui.rs,markdown_render.rs,markdown_stream.rs,streaming/*,history_cell/messages.rs,chatwidget/streaming.rs}`，完成适合Amadeus基础Agent的source-backed Markdown/TUI主链。Codex决定source/stream/final-cell与native history lifecycle；Goldmark决定grammar/AST/source offset；Chroma决定code highlighting；Bubble Tea以immutable native scrollback + bounded mutable frame适配，不复制Ratatui terminal engine。

第一轮AB已经移除Glamour opaque ANSI和全局draft主链，但2026-08-28审计确认其把stock Bubble Tea超高`View()`误当成native scrollback、用空行扫描代替Goldmark top-level boundary、用trailing cell scan代替stream attachment，并缺失文档声明的indent/table/link/wrap行为。因此此前`DONE`撤销；保留已完成的迁移基础，按以下依赖顺序重新闭环。

### 已完成的迁移基础

- [x] 删除Glamour生产渲染、`appModel.draft`、`proposedPlanDraft`、`LastAgentMarkdown`、`recoverDeltaStart`和opaque `styleRendered` Markdown主链；Goldmark成为直接grammar依赖，Chroma承担syntax tokenization。
- [x] 建立exact `MarkdownSource`、newline-gated collector、Item-scoped Assistant/Plan controller、stable queue/tail类型、source-backed final cell和render cache；raw source、临时parse source与derived render不再混存。
- [x] Completed Assistant/Plan Text覆盖live preview并成为final authority；`AgentMarkdownCell`保存exact text和冻结CWD，`/copy`读取completed source而不是ANSI或active tail。
- [x] Protocol、Session、Application和Rollout不依赖Goldmark node、terminal width、Chroma theme或TUI render cache。

### AB-01：Reference Pin + Reopen Audit — `DONE`

- [x] Codex reference pin：`markdown_stream.rs` `e488482e601ec6558fcd15702ee4fa2e729a82c6cebaea27c3d15e29b714e71f`；`streaming/controller.rs` `85b7e0b5ebeab1483df382051b831121625806c308f0fcd30080709ffdc1041e`；`streaming/render.rs` `7661640d0b6adfd0417acb93458d083ab3343c1aa65b4b0f9a9eeff0e5178621`；`history_cell/messages.rs` `9a100cb23f3749ba63980c321ce7ea5a10dde3a7248272a79dba14dfc20b5a1f`；`chatwidget/streaming.rs` `cfa52d2f78cd2e7c411de162e538aabe97ea3b29d299fb054a6d325b3cf8e7e6`。
- [x] 通过Bubble Tea `standardRenderer`源码确认：超过terminal height的frame顶部行会被丢弃，不会自动进入native scrollback；原“surface返回完整超高history即可自然滚动”的结论和对应`View()`单测无效。
- [x] 初次选择model-owned bounded transcript viewport；该结论在真实长final输出测试后由AB-10取代。保留 Go/Bubble Tea frontend，不复制 Codex 的 Ratatui terminal engine。
- [x] 记录completion/reset尾部扫描、非stream event interleave、空行stable boundary、中宽table截断、fragment不flush、indent metadata缺失、local/web link contract和控制字符投影问题，并同步修订`docs/design.md`。

### AB-02：Bounded Transcript Surface + Real Renderer Contract — `SUPERSEDED` by AB-10

- [x] `TranscriptSurface`成为全部HistoryCell、active tail、viewport offset和follow-bottom的唯一owner；`appModel.View()`只组合surface viewport、working state、overlay和composer，不返回超过terminal height的frame。
- [x] 依据composer/footer/overlay实际高度计算transcript可用区域；`PgUp/PgDown`、回到底部、新内容到达和follow-bottom切换具有确定行为，用户查看旧历史时不被强制拉回底部。
- [x] Resize从source-backed cells重新投影并clamp offset；follow-bottom保持底部，非follow状态以visual-line anchor尽量保持同一位置，不保存旧width ANSI rows。
- [x] 增加真实12行PTY/Bubble Tea renderer contract test，证明超过一屏的早期history仍可导航、主frame不被standard renderer静默裁顶；删除“`View()`越高越正确”的旧测试。

### AB-03：Stream Attachment + Deferred Projection Ordering — `DONE`

- [x] `TranscriptSurface`在首个非空Assistant/Plan delta时冻结`StreamAttachment{ItemID, Kind, RunStart}`；stable cells和tail只能追加到该attachment，completion/reset/interrupt按精确range替换或删除，不使用`trailingStreamRun`猜测owner。
- [x] `markdownStreamHost`实现Codex-style defer-or-apply FIFO。Active visible stream期间会插入、完成或删除HistoryCell的Warning、Tool、Approval、UserInput、diagnostic和application projection先延迟；stream replacement完成后按原Event顺序flush。
- [x] `Reset=true`原子删除旧attempt range并清空collector/render/queue/tail；completed authoritative Text无delta、匹配delta和修正delta三条路径都只产生一个final cell。
- [x] 覆盖warning/diagnostic interleave、Approval、retry reset、wrong/late ItemID、completion mismatch、terminal error、Clear和attachment cleanup；Protocol state仍按到达顺序验证，只有UI projection延迟。

### AB-04：Goldmark-backed Stable Boundary + Incremental Cost — `DONE`

- [x] Collector保存exact attempt source，并仅在最后newline推进committed watermark；finalize处理无newline尾行。
- [x] `StreamingRender`只解析`stable_source_len`后的pending source，以Goldmark最后一个top-level node source start决定mutable boundary；fence、loose list、blockquote和HTML block内部空行不得推进stable prefix。
- [x] 使用Goldmark `parser.Context.References()`识别reference definitions；source-wide table-fence transform无法保持offset时full recompute且不推进stable boundary。删除`lastMarkdownBlockBoundary`和字符串`hasMarkdownReferenceDefinition`。
- [x] Collector只在新增delta查找newline，table holdback只扫描mutable suffix，incremental render复用stable prefix；增加100段stable-source推进测试和200段stream benchmark。

### AB-05：Structured Writer + Block/Indent Semantics — `DONE`

- [x] Goldmark AST直接生成typed spans，composable strong/emphasis/strikethrough/link style与Chroma token已经进入生产路径。
- [x] `MarkdownLine`实现`InitialIndent`、`SubsequentIndent`、`BlockKind`和`NoWrap`；Writer按paragraph/heading/list/item/blockquote/code/table生命周期维护indent/style/link stack，删除从dim prefix文本猜continuation的逻辑。
- [x] Goldmark soft break、hard break和soft wrap分别投影；nested/loose list、blockquote continuation、list内code/table和indented code保留正确gutter与结构化空行。
- [x] Fenced/indented code默认no-wrap并保留exact source；unknown explicit language plain fallback、highlight bytes/lines/line-length和NoColor降级不改变source。
- [x] Renderer panic/超限使用UTF-8 safe bounded plain projection；live下一delta从exact pending source重试，final source、`/copy`和Resume不受fallback影响。

### AB-06：Width Layout、Tables + Links — `DONE`

- [x] Wrap先跨完整logical line计算word/grapheme ranges，再remap原span/style/syntax/destination；普通词不拆，只有URL/path/hash token-heavy fallback可拆，每个fragment达到宽度后实际flush。
- [x] Table group按共享intrinsic widths布局；收缩列宽时真实wrap cell并生成等高physical rows，低于最小可读宽度或超过row/column/cell bounds时整表降级key/value records，禁止输出超宽row交给Bubble Tea截断。
- [x] Local link覆盖`file://`、Unix、`~/`、Windows drive/UNC及line/column/hash suffix；Web link提供label和可读destination fallback，OSC-8只接受安全`http/https`。
- [x] Wrap、table reconstruction、cache clone和stream clone完整保留indent、table prefix、syntax和destination；local-link display transform递归处理typed table cells。
- [x] Rich parse source和terminal span projection双层移除CSI/OSC与非法控制字符，但不改写`MarkdownSource`、`/copy`或Resume source。

### AB-07：Production Integration + Legacy Guard Closure — `DONE`

- [x] 新surface viewport、stream attachment、deferred projection和structured layout接入Assistant与Proposed Plan同一生产主链；Tool/Approval/UserInput/Queue/Footer/Composer仍使用单一Bubble Tea frontend。
- [x] 删除`trailingStreamRun`、超高full-frame/natural-scrollback假设、空行block scanner、prefix heuristic、fake table shrink和不完整link transform；不保留新旧双路径或target-shaped adapter。
- [x] Architecture guards验证bounded mutable View调用链、stream host owner及legacy禁止项，并由AB-10增加immutable native-history watermark contract。
- [x] 同步`docs/design.md`、`docs/architecture-whitepaper.md`和`docs/tui-visual-contract.md`的native finalized history、bounded mutable frame、attachment replacement、parser boundary与layout contract。

### AB-08：Contract Fixtures + Acceptance — `DONE`

- [x] 从Codex移植基础fixture：partial link/fence、fence内空行、setext、reference definition、nested/loose list、blockquote/code、wide glyph、styled long token、local/web/Windows link和table逐行stream。
- [x] 增加真实12行PTY renderer、interleaved Warning/Approval、retry/final mismatch、terminal error、Clear、Raw/Rich/NoColor、Resume、`/copy`、large source/code/table bounds和cache invalidation测试；AB-10进一步覆盖native history与live long-final completion。
- [x] 增加stable-prefix推进测试与200段stream benchmark；单次`BenchmarkStreamingRenderStableParagraphs`约23.8ms，普通append不重新解析stable prefix。
- [x] 验收通过：focused TUI/architecture tests、`make check`、`go test ./... -count=1`、`go test -race ./... -count=1`、`go build ./cmd/amadeus`与`git diff --check`。
- [x] 当前owner下的Provider mock E2E通过：`internal/integration`中的Core Tools/Coding Agent均覆盖Responses与Chat Completions；`internal/llm/openai` Reasoning E2E覆盖Qwen/DeepSeek/GLM/standard dialect。
- [x] 逐条核对本节出口和`docs/design.md`第19.3节；代码、真实renderer行为、failure ordering、性能边界和文档一致后标记AB DONE。

### AB-09：Session Header + First Stream Spacing Repair — `DONE`

- [x] 对照Codex `new_session_info → SessionHeaderHistoryCell`，将Logo/Version/Model/CWD建模为`TranscriptSurface`固定首HistoryCell，删除“只有transcript为空才由View追加banner”的生产分支。
- [x] 对照Codex `AgentMessageCell`与`StreamingAgentTailCell`的`is_stream_continuation = !is_first_line`，使首个stream cell从第一帧就拥有与final cell相同的前置spacing，后续run保持continuation。
- [x] 增加“首条hello后Session Header仍保留”“stream/final spacing完全相同”回归测试；AB-10进一步以真实PTY证明header和首条历史进入native scrollback。

### AB-10：Native Finalized History + Bounded Mutable Frame — `DONE`

- [x] 对照Codex `insert_history_lines`与inline viewport确认根因：stock Bubble Tea会裁掉超高`View()`顶部rows，全部history留在model viewport会让terminal scrollback只剩启动shell命令，并截断超过一屏的final output。
- [x] `TranscriptSurface`新增SessionHeader/history print watermark；immutable SessionHeader/User/Tool/final Assistant cells经有序`tea.Println`只提交一次并从active frame排除，transient `AgentMessageCell`/tail永不打印。
- [x] completion继续先按attachment range consolidation authoritative final source，再打印final cell；provisional stream rows可替换且不会与native final重复。printed history与首个active cell的leading spacing继续由`IsStreamContinuation`决定。
- [x] User/initial/Plan提交改用`tea.Sequence(history flush, runtime submit)`恢复确定性event order；native print每行按terminal width约束，避免queued output autowrap破坏cursor。
- [x] 新增真实PTY live测试：先提交早期User history，再流式完成超过一屏的Markdown，native输出同时包含SessionHeader、User、final首行和末行；活动frame不重复immutable history。

### AB-11：Native History / Working / Composer Spacing — `DONE`

- [x] 以Codex `single_line_final_answer_hides_working_status`和Working snapshots逐行核对：User/Assistant、Assistant/Composer、Working/Composer之间均为两条blank rows，而不是一条。
- [x] 引入统一`transcriptRegionBlankRows=2`，同时驱动HistoryCell间距、native print leading rows、TranscriptSurface leading boundary和View section separator，删除四处独立spacing数字。
- [x] 固化`printed history → blank×2 → Working → blank×2 → Composer`、`printed final → blank×2 → Composer`和`stream → blank×2 → Working → blank×2 → Composer`三个精确行布局测试。

### AB-12：Final Message Separator Spacing Override — `DONE`

- [x] 对照Codex `final_worked_for_uses_cumulative_turn_duration.snap`逐行确认：final response与`─ Worked for ...`之间只有一条blank row，不能套用普通HistoryCell的两条。
- [x] 增加previous/current `historySpacingCell` boundary contract；默认非continuation为2、stream continuation为0、`FinalMessageSeparator`与`ToolHistoryCell`为1。
- [x] 同一spacing policy驱动`renderHistoryCells`、native print watermark和active surface boundary，并以精确行索引测试防止两条路径漂移。
- [x] 对齐Codex `binary_size_ideal_response.snap`：Assistant↔Explored/Ran及相邻Tool trees统一为一条blank row；同一ToolHistoryCell内部和跨ToolHistoryCell边界不再出现1/2行差异。

### AB-13：Source-backed Terminal Resize Reflow — `DONE`（2026-08-31）

#### 根因

Amadeus 原先在收到 Bubble Tea `WindowSizeMsg` 后只更新 `appModel` 的 width/height、textarea 和 bounded viewport。已经通过 `tea.Println` 写入终端 scrollback 的行仍按旧宽度存在；stock Bubble Tea renderer不会重排历史scrollback，因此宽度或高度变化后会出现旧行、Composer和新frame错位。

#### 实现

- [x] 增加 TUI 内部 `transcriptReflowState`，首次窗口尺寸只建立基线；后续 width/height 变化按 Codex trailing debounce（75ms）合并，并以 generation 丢弃过期 resize message。
- [x] reflow从`TranscriptSurface`保存的immutable `HistoryCell` prefix重新生成当前宽度的完整 history；transient Assistant/Plan stream继续留在bounded active frame。
- [x] 通过 Bubble Tea `tea.ClearScreen`清理活动画面，再发送标准 terminal `CSI 3 J`清理scrollback并用`tea.Println`写入新布局；同步重置 session-header/history print watermark，避免重复显示或遗漏历史。
- [x] resize debounce期间暂停新的 native history flush，确保一次重建覆盖当时全部 immutable history；后续新增完成cell按新宽度继续追加。
- [x] 保留现有 stream attachment/completion lifecycle；stream期间的resize由active frame实时重投影，completion后final cell按正常watermark进入native history。

#### 验收

- [x] 增加 `transcriptReflowState` generation/debounce、transient prefix watermark 和 `WindowSizeMsg` contract tests。
- [x] 增加真实 PTY resize 测试：80列历史调整到52列后检测scrollback erase序列及source-backed sentinel仍可见。
- [x] 通过 focused TUI/architecture tests、`make check` 和 `git diff --check`。

### AB 出口

- Assistant Markdown在live、retry、completion、resize、Raw/Rich、Resume和`/copy`中有唯一authoritative source owner与一个attachment-based completion protocol；不存在全局draft、trailing-run owner推断、fabricated item lifecycle或ANSI反向解析。
- `TranscriptSurface`保留canonical history并以print watermark提交immutable native scrollback；主frame只显示bounded mutable cells。SessionHeader、早期输出和长final可由终端原生滚轮访问，provisional stream replacement不依赖撤回已打印rows。
- Goldmark source offset决定stable/mutable boundary；incomplete Markdown不污染stable run，completed item能修复stream缺失/不一致，writer/wrap/table/link/code在宽窄终端与NoColor下有界、可读地降级。
- Protocol、Session、Application、Rollout不依赖TUI Markdown实现；单一TUI及现有Tool/Approval/Plan/Queue生命周期不回退。

## 34. AC. Basic Multi-Agent Terminal + Persistence Lifecycle Realignment — `DONE`

### 目标

修复当前Basic Multi-Agent中已确认的terminal与persistence缺口。Codex Multi-Agent V2、AgentPath/mailbox/residency、write-capable worker、child Approval/用户输入、history fork、team/worktree/remote和完整agent picker是永久产品非目标，AC不得实现、预留或安排这些能力。外层以当前`../codex-main/codex-rs/core/src/{agent/control.rs,agent/control/legacy.rs,agent/status.rs,agent/control/spawn.rs,tools/handlers/multi_agents/*,session_prefix.rs}`及`codex-rs/protocol/src/protocol.rs`为权威；Claude Code只提供background child独立取消域、Tool allowlist、权限不升级和failed/killed partial-result经验，不复制其Query/AppState/Task外层。

AC保留R/Z已经正确的完整child Thread/Session、root-scoped AgentControl、ThreadManager唯一live registry、fresh Context、read-only ToolRouter和统一Tool pipeline；替换`latestAssistant`推断、blocked→completed字符串压缩、live/Resume双reducer、wait-all、先标记notified后丢写入错误、显式close无durable edge及raw SQLite child membership。AC-01～AC-08是同一次Architecture Closure，不允许以新增字段包装旧reducer、保留双status projection或从旧ToolResult/Assistant Item补数据。

### AC-01：Reference Contract + Protocol Reset — `DONE`

- [x] 固定Codex V1 `TurnCompleteEvent.last_agent_message`、AgentStatus、`is_final`、spawn/send/wait/close、completion notification和explicit-close edge的source path/hash；记录Claude Code async abort/terminal partial-result的允许借鉴与禁止复制边界。
- [x] 为Amadeus当前Protocol增加optional `TurnCompleteEvent.LastAgentMessage`，定义由terminal Event派生的`AgentTurnResult{TurnID, Outcome, Reason, LastAgentMessage}`；AgentStatus枚举保持Codex的pending/running/interrupted/completed/errored/shutdown/not_found，不新增`AgentStatusBlocked`。
- [x] 重置当前Rollout codec/schema/fixture，不保留旧TurnComplete decoder、alias、fallback或双字段；同步`TaskOutput`、Event scope/validation、CollabAgentState、WaitResult和notification DTO。

### AC-02：Session Final Message Authority — `DONE`

- [x] 使RegularTask/`run_turn`只在真实final model completion时返回optional LastAgentMessage，Session将其直接写入canonical TurnCompleteEvent；Plan/steer/continuation保持同一authority和terminal ordering。
- [x] 删除AgentControl对任意completed AssistantMessage、Tool Call preamble、stream tail或“最近有文本消息”的final fallback；普通Assistant TurnItem仍只负责History/TUI projection。
- [x] 覆盖无文本final、Tool preamble→final、Tool preamble→blocked、Plan final、failed/aborted和进程在Assistant Item后/TurnComplete前退出的contract tests。

### AC-03：AgentStatus + LastTurn Pure Reducer / Wait Lifecycle — `DONE`

- [x] 建立单一pure reducer同时供live child Event和Resume rollout使用：TurnStarted原子设置Running并清空LastTurn，TurnComplete生成Completed/Errored和exact LastTurn，TurnAborted生成Interrupted+aborted LastTurn，SessionMeta-only恢复PendingInit。
- [x] `AgentStatusCompleted`只表达Thread当前空闲；`AgentTurnResult.Outcome`无损区分completed/blocked，blocked reason不得进入`status.completed="result: blocked"`字符串或被解释为父Turn取消。
- [x] 按Codex V1将public `wait_agent`改为任一final status唤醒并返回当时全部final snapshot；Interrupted不属于final，timeout返回空final集合。`send_input(interrupt=true)`使用独立correlated turn-stop waiter，不借用public wait语义。
- [x] 删除`latestAssistant`、`persistedAgentStatus`和wait-all path，不保留wrapper；覆盖多Agent错峰完成、simultaneous final、Interrupted、timeout、NotFound和live/Resume等价。

### AC-04：Child Budget Finalization — `DONE`

- [x] 将child TurnBudget解释为含finalization reserve的总预算；在sample/tool/time任一soft boundary停止新探索并执行至多一次Tools为空的finalization sample，要求返回已验证事实、路径、未决项和限制。
- [x] finalization成功时通过LastAgentMessage交付；hard boundary已跨越、Provider失败或final text非法时返回typed blocked LastTurn并保留exact reason，不伪装为completed交付或infrastructure error。
- [x] 保持Root普通Turn现有budget contract和W token accounting；finalization request的usage正常进入TokenUsageInfo，不新增第二budget owner、额外无界重试或可变配置。
- [x] 覆盖sample/tool/duration soft/hard boundary、一次性保证、Tools为空、finalization Provider failure、steer/compaction交互和弱模型反复Tool Call场景。

### AC-05：Root-tree Lifetime + Durable Spawn Edge — `DONE`

- [x] 明确child Session绑定ThreadWorkspace/root-tree lifetime；spawn Tool/父Step/父ActiveTurn context只拥有spawn事务。父Turn completed/failed/blocked/Interrupt后child继续，Root/Application shutdown或explicit close才停止runtime。
- [x] 新增root canonical `AgentSpawnEdgeItem{AgentID, ParentThreadID, State: open|closed}`及可重建SQLite projection；spawn在child configured、initial admission和open edge durable后才commit/返回，任一步取消/失败完整回滚。
- [x] 显式`close_agent`先durable标记目标/descendant edge closed再shutdown；Root shutdown只卸载open child runtime而不关闭edge。closed/archived child不在Root Resume注册、不占slot，历史rollout仍可供diagnostics。
- [x] Root Resume只从open edge恢复persisted/unloaded AgentRecord并校验SessionID/ThreadID/ParentThreadID/SessionMeta；删除raw `ListChildren`，只保留`ListOpenChildren` projection，并删除默认Completed恢复。

### AC-06：Completion Notification + Tool/TUI Projection — `DONE`

- [x] notification、wait ToolResult、CollabAgentState和TUI detail共享同一个AgentControl snapshot，携带AgentStatus+optional LastTurn；blocked显示真实reason和optional partial report，TUI不解析ToolResult字符串。
- [x] notification按child Turn terminal identity去重，canonical SubagentNotificationEvent携带AgentID+TurnID watermark；只有parent durable append成功后才推进notified，Resume不解析Content且不重复旧交付。失败形成typed diagnostic/pending delivery，不静默丢弃，completed后explicit close不再追加重复Shutdown notification。
- [x] 对notification envelope、error和LastAgentMessage使用统一约1000-token budget并保留结构化envelope空间，替换16,000-rune截断。
- [x] 更新spawn/wait ToolSpec和Subagent role guidance，明确成功spawn的child不随父Turn结束、wait只在关键路径需要时使用、runtime要求finalize时立即交付；模型不得在无typed evidence时猜测取消原因。

### AC-07：Persistence Failure Order + Shutdown Cleanup — `DONE`

- [x] 固化open-edge append、spawn commit、close-edge append、runtime shutdown、record/slot释放和notification delivery的失败顺序；任一持久化失败不得产生模型可见成功或live/Resume相反事实。
- [x] 区分explicit close与root-tree unload helper，保证关闭失败、重复close、并发Root shutdown、部分spawn、parent unavailable和manager close均无writer/watcher/slot/nickname泄漏。
- [x] Root Resume对SessionMeta-only、未闭合Turn、completed、blocked、failed、explicitly closed及超过`max_agents`个历史closed child保持确定行为；SQLite清空后从current rollout重建相同open membership。

### AC-08：Legacy Cleanup、Docs、Guards + Acceptance — `DONE`

- [x] 删除旧Assistant Item final推断、status双reducer、wait-all、rune-bound notification、unconditional shutdown notification、raw child-list restore及close/shutdown共用无标记路径；禁止adapter/facade/兼容decoder回引，并增加guard禁止V2/mailbox/write-worker/child-interaction等非目标占位进入生产代码。
- [x] 同步design/progress/architecture whitepaper/README、TUI visual contract和Prompt source manifest，修复R/T中已被AC取代的历史结论但保留其完成事实。
- [x] 增加architecture guards和Root→child provider-mock E2E，覆盖父Turn先terminal、child后terminal、budget finalization/blocked、wait-any、notification failure、explicit close→Root Resume和open unloaded child continue。
- [x] 运行focused functional/race tests、`make check`、`go test ./... -count=1`、`go test -race ./... -count=1`、Responses/Chat Core Tools与Coding Agent E2E、`git diff --check`后才标记AC DONE。

### AC 出口

- Parent Turn、Tool Call和Model Step不拥有已spawn child lifetime；Root tree拥有唯一取消与shutdown边界。
- TurnCompleteEvent.last_agent_message是唯一final answer authority；AgentStatus保持Codex枚举，AgentTurnResult无损表达Amadeus blocked outcome/reason，live与Resume使用同一reducer。
- child在soft budget内获得一次有界no-tools finalization机会；即使hard blocked，parent也得到真实原因而不是无交付completed或推测性解释。
- wait、notification、WorldState、CollabAgentTurnItem和TUI从同一snapshot投影；任一final唤醒、token bound、去重和delivery failure均确定。
- Root canonical open/closed edge决定persisted child membership；explicit close不会在Resume复活，Root shutdown不会误关闭可恢复edge，closed历史不会耗尽slot。
- 生产代码不存在旧final推断、双status truth、无标记close/shutdown或target-shaped compatibility layer；V2/mailbox/history-fork/write-worker/child-interaction/team/worktree/remote/picker既不实现也不预留。

### AC 验收

- Root模型spawn child后立即完成自己的Turn；child在其后完成并注入唯一notification，Root下一Turn可见exact result，child rollout无父Turn取消。
- child先输出探索前导语并持续调用只读Tool直到soft boundary；最终交付来自no-tools finalization。若hard blocked，wait/notification/TUI显示exact blocked reason而不是前导语或`result: blocked`。
- 三个child错峰完成时第一次`wait_agent`在首个final后返回，不等待全部；后续notification/wait不重复交付同一Turn result。
- Root依次spawn/close超过`max_agents`个child并重启；closed child不恢复不占slot，仍open child以相同SessionID、独立ThreadID、exact LastTurn恢复并可继续`send_input`。

### AC 完成记录

- 2026-08-28固定Codex V1参考：`agent/control.rs` `eb7f299030946b0a633b17b768acf0570c84c4ecf5d4a1ecff1396ff3f2fdf9b`、`agent/control/legacy.rs` `1b0400ab99c05373d5abac585b5f56ae26a378b50e06344eadee89b6853bd8cd`、`agent/control/spawn.rs` `a081b645408c83031999c5dab0248b9419ae3046f78d01de8e74da1c404f4aa3`、`agent/status.rs` `3e72128ca9006ae3e100f130af3d5b4fe501ee32753f4175d6d91b3ebe883fa7`、V1 `wait.rs` `7f7ad857197772fd65c18ba510705e521661146c09c42373b408446a9fe841f8`、`session_prefix.rs` `ee14ee878910c87eb45555b5777a56f33afbbf06f878ffdf21c574a3553d0f64`和Protocol `28aa2dc7b5289ede64325c9b67f8df3ada3458db189f5d115e0484ffa4216b4e`。
- Claude Code局部参考固定为`runAgent.ts` `e36d9478dfbe52c337ac51b143cfd16f9c05daeca3ff5f1f9b4da14d8edc8d62`、`agentToolUtils.ts` `890a724813b6c0abd372f477a382d474d7e537e357ba1600f2af7a9d0c5224f4`和`AgentTool.tsx` `4a1d272651884d43322b2f71ee97cb0308ea9648c155aaa072a15d17c2f8d9ca`；只吸收async child独立取消、权限不升级、bounded partial-result思想，未引入其Task/AppState外层。
- `TaskOutput.LastAgentMessage → TurnCompleteEvent.LastAgentMessage`成为唯一final authority；新增AgentTurnResult和single live/Resume reducer，删除latestAssistant、persistedAgentStatus和Assistant Item fallback。wait改为任一final唤醒，interrupt-and-restart使用独立内部waiter。
- child soft budget使用一次Tools为空且受剩余duration约束的finalization sample；成功交付verified report，Tool Call/Provider failure/hard limit形成保留exact reason的blocked LastTurn。Root普通budget、compaction和TokenUsageInfo owner不变。
- Rollout当前格式重置为v6并新增Root-only AgentSpawnEdgeItem；SQLite当前schema重置为v5并投影agent_edge_state。spawn在initial admission+open edge durable后commit，explicit close先写closed edge，Root shutdown只unload。Root Resume从canonical open edge恢复，`ListChildren`已删除并替换为`ListOpenChildren`。
- notification渲染迁入contextmanager typed ContextFragment；SubagentNotificationEvent持有AgentID+TurnID delivery watermark，成功durable后才推进notified，失败保留diagnostic并可由wait重试。Status/LastTurn、ToolResult、CollabAgentState和TUI共享同一snapshot，文本按约1000-token envelope预算截断。
- 新增Root Turn先完成/child后完成、preamble→blocked、wait-any、intermediate Error、notification retry、open/closed edge failure ordering、closed child跨Resume不占slot、open child exact LastTurn lazy resume、SQLite rebuild、no-tools finalization及Provider failure等tests；architecture guards禁止旧final reducer、wait-all、raw child membership和V2非目标占位回归。
- 验收通过：focused functional/race tests、`make check`（含vet、全仓tests和build）、`go test -race ./... -count=1`、现有Responses/Chat Coding Agent/Core Tools integration E2E与`git diff --check`。
