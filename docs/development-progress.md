# Amadeus 开发进度

> 最近更新：2026-08-14
> 唯一架构事实源：`docs/design.md`
> 当前阶段：C-T. Claude-style Tool Domain Refactor
> 下一任务：C-T-01 ToolUseContext 与 Prepare/Execute 主链

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
→ F Plan-guided ReAct
→ G Extensions + Release
```

Codex 作为 Runtime、Persistence、Context、Plan-guided ReAct、Slash Command 和 TUI 的主要架构参考；Claude Code 作为 Tool 内层协议、文件 Diff 和 Approval 行为的主要参考。A、B、D、E 的基线设计已冻结；C-T 实施若发现 Contract 问题，先更新 `docs/design.md`。

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

### 已完成

- 统一 `Thread`、`Session`、`Turn`、`Task`、`Iteration` 和运行时所有权。
- 完成 `AmadeusThread`、`SessionIo`、`Session`、`ActiveTurn` 和 `SessionTask` 主链。
- 实现 JSONL Rollout 的版本、序列号、追加、刷新、关闭、尾行修复和损坏检测。
- 实现 ThreadStore、LocalThreadStore、SQLite metadata index、List、Rename、Archive、Resume 和恢复重建。
- 完成 Turn 终态、取消、持久化顺序和并发追加验收。

### 出口

- [x] Thread/Session/Turn 生命周期只有一套主链。
- [x] Rollout 可独立重建 Session 状态和 Context。
- [x] SQLite 不保存不可重建的完整对话事实。
- [x] Resume、取消和异常终态可恢复。

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

### 已完成

- 收敛 Prompt、Message、Tool Definition、ModelInfo 和 ContextManager Contract。
- 将系统提示词和 Prompt 资产纳入统一构建链，删除旧的多套上下文事实源。
- 实现动态 Context Update、AGENTS.md 层级加载和目录作用域。
- 实现 JSONL Rollout 到 LLM Message 的统一投影，包括 Assistant reasoning 和多工具调用历史。
- 实现 Provider Usage、估算 Token、Context Window、Auto Compact 和 Replacement History。
- 完成上下文截断、工具结果归一化、历史压缩和中断 Turn 的上下文重建。

### 出口

- [x] ContextManager 是唯一 Prompt 历史入口。
- [x] Prompt、Token 和 Compaction 使用同一套 ModelInfo 预算。
- [x] 历史可从 Rollout 重建，不依赖内存残留。
- [x] Provider reasoning、Tool Call 和 Tool Result 的顺序可正确回放。

## 5. C. Tool + Approval — `DOING`

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

### C-T：Claude-style Tool Domain Refactor — `DOING`

#### C-T-01：Tool Runtime Contract

- [ ] 将 `Tool` 目标概念统一为 `ToolDefinition`、`ToolUseContext`、`PreparedToolUse`、typed `ToolResult` 和 `ToolDisplayResult`。
- [ ] 将调用链拆成 `Normalize`、`ValidateInput`、`Prepare`、Permission evaluation、Approval、`Execute(prepared)` 六个显式阶段。
- [ ] 以显式 `PreparedToolUse.State` 传递快照和准备态，移除核心执行链对 `context.WithValue` 的依赖。
- [ ] 为现有 Tool 提供短期 Adapter，迁移完成后删除旧 `Tool.Call`/`PermissionCheck.Prepared any` 主链，不长期保留双协议。

#### C-T-02：Claude-style Read-only Tools

- [ ] 按 Claude Code `FileReadTool` 语义重构 `read`：canonical path、offset/limit、文本/图片分流、完整读取后记录 `FileReadState`。
- [ ] 按 Claude Code `GlobTool/GrepTool` 语义重构 `glob/grep`：read-only、并发安全、稳定排序、数量/字节/Token 上限、typed `truncated` 和截断原因。
- [ ] 收敛 `view_image` 的路径、MIME、大小、权限和 typed media result；工作目录外读取走独立 read-directory Approval。

#### C-T-03：Claude-style File Mutation Tools

- [ ] 增加 `FileReadStateStore`，记录 canonical path、存在性、内容 Hash、mode 和 symlink 状态，严格执行 Read-before-write。
- [ ] 按 Claude Code `FileEditTool` 语义重构 `edit`：唯一匹配、`replace_all`、Prepare Diff、stale check、原子写入和 typed result。
- [ ] 按 Claude Code `FileWriteTool` 语义重构 `write`：create/update 区分、已有文件完整 Read、覆盖 Diff、重新校验和 typed result。
- [ ] 增加 typed `FileChangePreview`、`DiffHunk`、`DiffStats` 和 `FileChangeResult`，保证 Deny/Decline 零副作用、Apply 与 Preview 一致。

#### C-T-04：Permission、Approval 与 TUI Diff

- [ ] 将权限 Grant 拆分为 read directory、edit directory、exact command、external host/tool，禁止跨能力复用授权。
- [ ] 将结构化 Diff 原样贯通 `ApprovalRequest` → `InteractiveRequest` → TUI，禁止桥接为 `string` 或扁平 `any`。
- [ ] 将通用 selection overlay 改为专用 Approval Dialog，提供有界、可滚动 Diff viewport 和 Claude Code 风格的清晰 Question/Options。
- [ ] ApprovalCoordinator 只等待决定，Session 只在内存应用 grant；Session Close、进程退出或 Resume 后清空。

#### C-T-05：Codex-style Process Tools

- [ ] 为 `execute_command` 增加 typed `ProcessResult`、ProcessManager、process/session ID、输出预算、exit code、truncation、取消和 lifecycle event。
- [ ] 按 Codex unified exec 重构 `write_stdin`：绑定 `OriginCallID`，继承原命令 Approval，不重复 Permission/PreToolUse。
- [ ] `write_stdin` 在 Registry 层声明可并行；ProcessManager 保证同一 `process_id` 串行、不同进程并行。
- [ ] 保持宿主执行边界，不把 Codex Sandbox、Guardian、Remote Environment 或 Network Approval 引入主链。

#### C-T-06：Codex-style Runtime Tools

- [ ] 将 `update_plan` 输入统一为 Codex 风格 `plan` + optional `explanation`，校验至多一个 `in_progress`。
- [ ] `update_plan` 只调用 Session `UpdatePlan` capability、发布 `PlanUpdated` 并返回简短 `Plan updated`；完整计划不塞入通用 ToolResult 文本。
- [ ] 将 `request_user_input` 保持为独立 Interactive Request，不复用 Permission Approval 或修改权限上下文。
- [ ] C-T 只完成 Tool 层 Contract；Plan State、Resume、Plan Mode 与 Reactor 的端到端实现继续由 F-03/F-04 完成。

#### C-T-07：Result Projection 与外部 Tool

- [ ] 分离 Execution Result、模型侧 ToolResult、展示侧 ToolDisplayResult 和 canonical TurnItem，禁止 TUI 从模型文本反解析结果。
- [ ] Web/MCP/Skill 迁移到同一 ToolUse 生命周期，统一 bounded output、typed error 和最小 host/tool grant。
- [ ] Runtime、TUI、Rollout 和 ContextManager 对同一 Tool 结果使用一致 typed semantics。

#### C-T-08：遗留隔离与验证

- [ ] 明确 `apply_patch` 与 sandbox 仅为遗留代码：不注册、不暴露给模型、不接入 ToolExecutionService/Approval/Event/Rollout/TUI 主链。
- [ ] 增加 Tool/Approval/TUI Contract 测试：请求文案、目标范围、Grant 作用域、拒绝、stale conflict、Diff 截断/滚动和 Resume 后权限清空。
- [ ] 增加只读截断、`write_stdin` Approval 复用与并发、`update_plan` concise result/Event、默认 Catalog 无 `apply_patch` 的测试。
- [ ] 完成 C-T 后重新核对 D 的 Event/TUI 结果模型和当前进度断言，删除“已完成 Diff/Approval”一类过度承诺。

### C-T 出口

- [ ] 所有内置 Tool 使用同一 ToolUse 生命周期，Tool 定义不直接访问 TUI 或更新 Session 权限。
- [ ] 文件修改遵循 `Read → Prepare Diff → Approval → Revalidate → Atomic Apply → Verify`。
- [ ] Approval 文案、范围和选项对齐 Claude Code；grant 仅存在于当前 Session 内存。
- [ ] TUI 可以在写入前展示结构化、可滚动 Diff，并只返回用户决定。
- [ ] Typed ToolResult、ToolDisplayResult、TurnItem、Rollout 和 ContextManager 对同一 Tool 结果保持一致语义。
- [ ] `write_stdin` 正确续接原命令且不重复 Approval；`update_plan` 通过 Event 展示完整计划并只返回简短 ToolResult。
- [ ] `apply_patch` 与 sandbox 不出现在默认 Tool Catalog、Registry、Prompt、Event、Rollout 或 TUI 主链。
- [ ] 针对性测试和构建通过后，C 才能恢复为 `DONE`。
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
- [x] Approval、Diff、Plan、Compaction、Tool 和 Turn 终态已有统一事件承载与基础投影；typed payload、Approval Dialog 和可滚动 Diff 由 C-T-04/C-T-07 收敛。

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

## 8. F. Plan-guided ReAct — `TODO`

### F-01：唯一 RegularTask/Reactor

- `TODO`：以 Codex 风格的 Plan-guided ReAct 作为默认 Agent Engine 主链。
- `TODO`：统一 RegularTask、Reactor、TurnContext、Tool Loop 和取消传播。

### F-02：Plan 与 TurnItem

- `TODO`：将 Plan、Plan Step、Plan Update 纳入 Event/Rollout/HistoryCell 主链。
- `TODO`：确保普通 ReAct、`/plan` 和 Tool 执行使用同一 Turn Runtime。

### F-03：`update_plan`

- `TODO`：在 C-T-06 已完成 `update_plan` Tool Contract 的前提下，实现由 Session 持有的 Plan State、canonical persistence、Resume 和 Plan Update 展示。
- `TODO`：补齐 Plan 更新、恢复、压缩、Plan Mode 和 TUI 端到端测试。

### F-04：Plan Mode、终态与中断

- `TODO`：实现 `/plan` 的计划展示/审核流程，不创建第二套 Agent 模式。
- `TODO`：统一 completed、blocked、failed、aborted 和用户中断后的重新规划。

### F 出口

- [ ] 默认 Agent Engine 为 Plan-guided ReAct。
- [ ] `/plan` 只是显式计划入口，不产生独立 Runtime 主链。
- [ ] Agent、Tool、Plan、Event、Rollout、Context 和 TUI 可端到端运行。

## 9. G. Extensions + Release — `TODO`

- `G-01 MCP`：按当前 ToolDefinition、Approval 和 Event Contract 接入 MCP 工具。
- `G-02 Skill`：实现 Skill 发现、说明、调用和脚本执行边界。
- `G-03 Web`：完善可配置 Web Search Provider、超时、重试和结果归一化。
- `G-04 Persistence Cleanup`：清理 F 阶段确认不再需要的旧持久化和兼容代码。
- `G-05 Release Validation`：跨平台构建、端到端测试、文档同步和发布验收。

## 10. 当前保留能力

- 默认启动：`amadeus` 或 `amadeus "<task>"`。
- 当前配置链和 Provider Adapter 已可使用 OpenAI Responses/Chat Completions 及兼容 Provider。
- JSONL Canonical Rollout + SQLite Metadata Index 已可支持 Session 恢复。
- TUI 和 Inline 输出以当前代码和 `docs/design.md` 为准。
- 内置 Tool、Approval 和 Diff 已进入 A-E 基础主链，但 Tool 内层协议、typed result、Approval Dialog 与 Diff viewport 仍以 C-T 为当前重构任务；AGENTS.md、Web Search 和 Slash Command 保持现有能力。

## 11. 当前执行规则

1. 每次只推进一个 `TODO`/`DOING` 主任务。
2. 先修改 `docs/design.md`，再修改代码；实现发现设计问题时暂停并同步 Contract。
3. 新主链验收后立即删除对应旧主链，不保留长期双实现。
4. 任务完成必须运行针对性测试和构建；环境限制导致的测试失败要单独记录。
5. 本文只更新任务状态和出口，不复制架构设计、源码审计或长篇讨论。
