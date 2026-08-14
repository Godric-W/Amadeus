# Amadeus 架构设计

> 状态：Target Architecture v1
> 最近修订：2026-08-14
> 目标语言：Go
> 产品形态：面向真实软件工程任务的本地 Coding Agent CLI
> 架构骨架：`../codex-main`
> Tool 与 Approval 行为参考：`../claude-code-main`

## 1. 文档定位

本文只描述 Amadeus 的最终目标架构、稳定 Contract 和验收标准。实施顺序、任务状态和旧代码清理由 `docs/development-progress.md` 维护。

设计遵循以下约束：

1. Codex 作为 Session、Turn、Runtime、Rollout、Context、Plan-guided ReAct、Slash Command 和 TUI 的主要架构骨架。
2. Claude Code 作为 Tool 内层协议、ToolUseContext、文件工具、文件修改确认、Diff Preview、Read-before-write、权限评估和 Approval 交互的主要行为参考。
3. Amadeus 保留多 Provider、本地配置、Go 实现和跨模型安全默认值等自身产品要求。
4. 外层 Runtime 不照搬 Claude Code；内层 Tool 的数据模型和阶段划分尽量与 Claude Code 对齐，再通过 Go 类型和 Codex Event/InteractiveRequest 边界适配。
5. 不引入 Claude Code 的用户级、项目级或本地权限持久化；Approval grant 只保存在当前 Session 内存中，Session 结束或 Resume 后清空。

`../claude-code-main` 是源码还原项目，因此用于理解产品行为和局部机制，不作为未经验证的实现权威。涉及关键安全行为时，必须用 Amadeus 自身测试固化契约。

核心设计域：

| 设计域 | 主要章节 |
|---|---|
| A. Runtime + Persistence | 分层架构、核心语义、Canonical Runtime、Session/Resume、Persistence |
| B. Context + Prompt | Prompt/Context、Token、Compaction、Provider Adapter |
| C. Tool + Approval | Tool 架构、文件修改、Permission、Command、并发 |
| D. Event + TUI | Session Event、Interactive Request、TurnItem、HistoryCell、Rich Inline Projection |
| E. Slash Command | Codex 风格 SlashCommand、InputResult、单一 TUI 分发与 Application/Session 操作 |
| F. Agent Engine | Plan-guided ReAct、Plan Tool、Plan Mode、中断与终态 |
| G. Extensions + Release | MCP、Skill、Web、迁移清理与发布验收 |

## 2. 产品目标

Amadeus 的目标是成为一个真正可用于日常软件开发的通用 Coding Agent：

- 用户直接运行 `amadeus` 进入交互会话。
- 用户也可以运行 `amadeus "<task>"` 执行首个任务。
- Agent 能探索项目、编辑文件、执行命令、验证结果并解释修改。
- 默认使用 Plan-guided ReAct，而不是 DAG Planner 或两套 Agent Engine。
- 简单任务可以直接执行；复杂任务可以通过 `update_plan` 维护可见计划。
- `/plan` 进入与 Codex 对齐的显式 Plan Mode，用于分析和规划，不实施文件或命令副作用。
- `/compact` 调用正式的上下文压缩服务，而不是仅清空 TUI 文本。
- 文件修改默认对能力较弱或不稳定的模型保持安全：先生成 Diff，再由用户确认，最后写入。
- Runtime 不依赖 TUI，未来可以被其他界面复用。

## 3. 参考职责矩阵

| 能力域 | 主要参考 | Amadeus 取舍 |
|---|---|---|
| Thread、Session、Turn | Codex | AmadeusThread、internal Session、ActiveTurn 与 SessionTask 使用同构生命周期 |
| Agent Runtime | Codex | 单一 Plan-guided ReAct 内核 |
| Plan | Codex | `update_plan` 是软状态；`/plan` 是显式 Plan Mode |
| Context Manager | Codex 为骨架 | 统一历史投影、Token Accounting 和 Compaction 生命周期 |
| Prompt Assembly | Codex 与 Claude Code | 稳定规则分层，动态事实不写入静态模板 |
| Tool 内层协议 | Claude Code | ToolUseContext、Validate、Prepare、Permission、Approval、Execute 和 Typed ToolResult |
| 文件 Tool | Claude Code | Read-before-write、staleness 校验、Structured Diff、确认后落盘 |
| Command Tool | Claude Code 行为，按 Amadeus 跨平台目标收敛 | 默认 Ask、Session 精确规则、应用层保护与宿主执行 |
| Approval TUI | Claude Code | 展示操作和结构化 Diff，使用范围明确的动态选项与键盘交互；不直接修改权限状态 |
| Event Protocol | Codex | Submission、SessionEvent、InteractiveRequest、TurnItem 生命周期与 Delta |
| TUI 数据模型与视觉 | Codex | EventReducer、HistoryCell、ActiveHistoryCell、Working、Slash Popup、状态栏 |
| Slash Command | Codex | 命令分为 TUI Local、Application Action 与 Core Op，不拥有业务状态 |
| MCP、Skill | Codex 为主 | 统一进入 Tool Registry、TurnItem 和 InteractiveRequest |
| Provider Adapter | Amadeus | 支持 Responses、Chat Completions 和 Provider Dialect |
| Persistence | Codex | ThreadManager、LiveThread、ThreadStore；JSONL canonical rollout + SQLite metadata index |

## 4. 明确非目标

Amadeus 不引入以下主链：

- DAG、ExecutionGraph、Planner、Replanner、Scheduler 或 Plan Task Executor。
- 默认自动长期记忆、用户偏好推断或 RAG 记忆库。
- 内置 LSP 核心依赖。
- 文件 Snapshot/Revert 或项目级时间旅行。
- OS Sandbox、容器沙箱或仅 Linux 生效的隔离主链。
- 企业策略下发、遥测平台、Feature Flag 平台或 IDE 专属协议。
- 为所有 Tool 强行设计完全相同的权限算法。
- 从任意 Shell 字符串中推断所有文件读写路径。

## 5. 核心架构原则

1. **Runtime 是事实所有者**：TUI、CLI、Context 和 Tool 都消费 Runtime 状态，不各自维护 Agent 真相。
2. **Rollout 是唯一历史源**：模型上下文、Resume、Compaction 和 TUI 历史都从 canonical rollout 投影。
3. **Plan 是软状态**：计划辅助执行和沟通，不编译成 DAG。
4. **Tool 按能力分类**：只读、结构化修改和进程执行拥有不同安全流程。
5. **修改先预览后落盘**：结构化文件修改必须先生成 Structured Diff。
6. **权限靠近 Tool**：Tool 根据真实输入返回 Allow、Ask 或 Deny；Ask 统一进入 Approval Runtime。
7. **Prompt 只描述真实能力**：未实现的 Tool、权限、MCP、Skill 或 Runtime 行为不得写进系统提示词。
8. **错误必须可见**：任何停止工作、上下文超限、Tool 失败或 Provider 错误都必须产生明确的 StreamError、Completed Item 或 Turn 终态。
9. **默认安全但不过度抽象**：优先采用简单、明确、可测试的规则，不提前建立企业级策略系统。

## 6. 分层架构

```text
┌──────────────────────────────────────────────────────────┐
│ Interface                                                │
│ CLI · Rich Inline TUI                                    │
├──────────────────────────────────────────────────────────┤
│ Application                                              │
│ Bootstrap · ThreadManager · SlashCommandService          │
│ CompactService · ApprovalService · StatusService         │
├──────────────────────────────────────────────────────────┤
│ Agent Runtime                                            │
│ AmadeusThread · SessionIo · internal Session              │
│ SessionState · ActiveTurn · RunningTask · SessionTask     │
│ TurnContext · TurnState · ContextManager · Reactor        │
├──────────────────────────────────────────────────────────┤
│ Capabilities                                             │
│ LLM · Tool Registry · Approval · Command Runner           │
│ MCP · Skill · Web · Structured Diff                       │
├──────────────────────────────────────────────────────────┤
│ Infrastructure                                           │
│ LocalThreadStore · JSONL Rollout · SQLite State DB       │
│ Filesystem · Process · HTTP · Provider SDK               │
└──────────────────────────────────────────────────────────┘
```

### 6.1 Interface

Interface 只负责：

- 收集用户输入。
- 展示 HistoryCell 和 ActiveHistoryCell。
- 展示 Slash Command Popup、Selection Overlay 和 Approval Dialog。
- 将键盘、鼠标和终端 Resize 转换为 UI Action。
- 将 Application Result、SessionEvent 和 InteractiveRequest 渲染为用户可见状态。

Interface 不负责：

- 创建第二份对话历史。
- 判断 Turn 是否完成。
- 直接执行 Tool。
- 自行压缩上下文。
- 自行修改 PlanState。

### 6.2 Application

Application 负责用例编排：

- 通过 ThreadManager 创建、恢复、重命名和删除持久化 Thread。
- 创建和关闭 `AmadeusThread`。
- 将用户输入、Interrupt、Approval、Compact 和 Shutdown 转换为 `Op`。
- 接收 TUI 分发后的 Application Command，并映射到明确服务。
- 调用 Thread、Compact 和 Status 等 Application Service。

Application 不包含模型循环，也不复制 Tool 权限逻辑。

### 6.3 Thread 边界

`AmadeusThread` 是 Interface/Application 持有的活动会话句柄，对齐 CodexThread：

- 通过 `SessionIo` 向内部 Session 提交 `Submission/Op`。
- 接收 `SessionEvent`、`InteractiveRequest` 与 AgentStatus。
- 提供 shutdown、wait、flush 和 resume 生命周期入口。
- 不直接拥有 Context History、ActiveTurn 或 Tool 状态。

TUI 不直接调用内部 Session、RunningTask、ContextManager 或 Store。

### 6.4 Agent Runtime

Agent Runtime 负责：

- 由内部 `Session` 运行长期 Submission Loop。
- 使用 `SessionState` 保存跨 Turn 的内存状态。
- 使用 `SessionServices` 持有 Session 级可复用服务。
- 通过 `LiveThread` 追加 canonical RolloutItem，不直接操作 JSONL 或 SQLite。
- 创建 TurnContext、ActiveTurn、RunningTask 和 SessionTask。
- 由 RegularTask 执行 Plan-guided ReAct。
- 基于 SessionState.Context、TurnContext 和可见 Tool Specs 构建每次模型采样的 Prompt。
- 接收 ToolResult 并继续推理。
- 路由 Interrupt、Approval Decision、User Input Response 和待处理输入。
- 完成持久化后发布 Turn 终态事件。
- 发布稳定的 SessionEvent，不把 Reactor、Provider 或 TUI 内部细节暴露为公共协议。

### 6.5 Capabilities

Capabilities 是 Runtime 可调用的外部能力：

- LLM Provider Adapter。
- Tool Registry、ToolDefinition、ToolUseContext 与 ToolExecutionService。
- Validate/Prepare/Permission/Approval/Execute 生命周期。
- 结构化文件 Tool、Diff Preview 与 Approval。
- Host Process Runner。
- MCP、Skill、Web Search、Web Fetch。
- Structured Diff。

### 6.6 Infrastructure

Infrastructure 负责本地 ThreadStore、JSONL Rollout、SQLite State DB、文件系统、进程、网络和 SDK 等具体实现，不反向依赖 TUI。JSONL 保存 canonical history；SQLite 只保存可重建的 Thread metadata/index。

## 7. 目标目录结构

目标目录按职责组织：

```text
cmd/amadeus/                 CLI 入口与依赖装配
internal/app/                Application Services
internal/thread/             LiveThread、ThreadStore、InitialHistory 与恢复逻辑
internal/thread/local/       LocalThreadStore 组合与本地路径布局
internal/thread/manager/     ThreadManager 与 AmadeusThread
internal/agent/session/      internal Session、SessionState、SessionServices
internal/agent/turn/         TurnContext 与 TurnState
internal/agent/task/         RunningTask、SessionTask、RegularTask、CompactTask
internal/agent/react/        单一 Reactor
internal/agent/plan/         Session Plan 投影、update_plan 数据模型与校验
internal/agent/protocol/     Submission、SessionEvent、InteractiveRequest 与 TurnItem
internal/context/            ContextManager、Token Accounting、Projection 与 Compaction
internal/prompt/             BaseInstructions、Prompt 与内置 Prompt 资产
internal/llm/                LLM Domain Port
internal/llm/openai/         OpenAI-compatible Adapter
internal/tool/               ToolDefinition、Registry、ToolUseContext、ToolExecutionService 与 PermissionService
internal/policy/             ApprovalPort、ApprovalCoordinator、SessionPermissionContext 与静态策略
internal/tool/builtin/       Read、Edit、Write、Glob、Grep、Command 等内置 Tool
internal/process/            Host Process Runner
internal/rollout/            RolloutLine、RolloutItem、JSONL 编解码与重放
internal/state/              StoredThread、Metadata Update 与 State DB Port
internal/state/sqlite/       SQLite State DB 与历史数据 Migration
internal/interface/tui/      Codex 风格 Rich Inline TUI
internal/interface/cli/      CLI 参数和非交互输出
internal/instruction/        AGENTS.md 发现与合并
internal/mcp/                MCP Client 与 Tool Binding
internal/skill/              Skill 发现与 Prompt/Script 元数据
internal/diff/               Structured Diff
internal/config/             配置加载、校验、脱敏
```

## 8. 核心语义

### 8.1 Thread Identity 与 Project Context

`ThreadID` 是持久化对话的唯一身份；`AmadeusThread` 是该 Thread 在当前进程中的活动 Runtime 句柄。用户界面继续使用 Session 语义，例如 `amadeus sessions list`、`/resume` 和 `amadeus --resume <id>`。

Project Context 由启动目录或 `--project` 指定目录形成 canonical CWD：

- canonical CWD 进入 Thread metadata、SessionConfiguration 和 TurnContext。
- `/resume` 默认按 canonical CWD 过滤 StoredThread。
- Project Root 是工作上下文，不自动等同于完整安全策略。
- 相对路径始终基于 TurnContext 的 canonical CWD 解析。
- 项目过滤直接使用 Thread metadata 中的 CWD，不建立独立 `ProjectRecord/projects` 表。

### 8.2 Thread Persistence Boundary

Amadeus 对齐 Codex，采用以下持久化边界：

```text
ThreadManager
→ LiveThread
→ ThreadStore
→ LocalThreadStore
   ├─ RolloutRecorder → JSONL canonical rollout
   └─ StateDB         → SQLite metadata/index
```

- `RolloutItem` 是按真实发生顺序追加的 canonical history protocol。
- `RolloutLine` 为每个 RolloutItem 增加 sequence 和 timestamp，并编码为单行 JSON。
- `StoredThread` 是 SQLite 中可重建的 Thread metadata/index，不是 Runtime Session。
- `InitialHistory` 表示 `New` 或 `Resumed` 的启动历史输入。
- JSONL 是 Resume、Context 重建、Compaction 和历史投影的唯一事实来源。
- SQLite 只保存 rollout path、CWD、标题、预览、模型、Token、时间和归档状态等可查询元数据。
- SQLite 可以落后 JSONL，但不能领先 JSONL；SQLite 丢失后必须可由 Rollout 重建。

`amadeus` 启动时可以先生成 ThreadID 并持有未物化的 AmadeusThread；第一次有效输入时才创建 Thread persistence 并写入 SessionMeta。`/resume` 和 `amadeus --resume <id>` 先读取 StoredThread 定位 Rollout，再以 `InitialHistory::Resumed` 重建运行时 SessionState。

### 8.3 ThreadManager

`ThreadManager` 对齐 Codex ThreadManager，是进程级 Thread 创建、恢复和已加载实例管理入口：

```go
type ThreadManager struct {
    store    ThreadStore
    services SharedServices
    threads  map[ThreadID]*AmadeusThread
}
```

它负责：

- `StartThread`、`ResumeThread`、`GetThread` 和 `ShutdownThread`。
- 创建 `SessionSpawnArgs` 并调用 internal Session 的 spawn 流程。
- 将 `Session + SessionIo` 包装为 AmadeusThread。
- 持有进程级共享依赖，不执行 Reactor，不持有 ActiveTurn。

CLI/TUI 只通过 ThreadManager 和 AmadeusThread 使用 Runtime，不直接装配 Session 级依赖或 Rollout Writer。

Go 为避免 `thread → agent/session → thread` 包循环，将持久化边界放在 `internal/thread`，将需要 spawn internal Session 的管理层放在 `internal/thread/manager`。这是同一个 Thread Runtime 边界的无环包拆分，不建立第二套 ThreadManager。

### 8.4 AmadeusThread

`AmadeusThread` 对齐 CodexThread，是 TUI、CLI 或未来 Runtime API 持有的活动会话句柄：

```go
type AmadeusThread struct {
    session *Session
    io      SessionIo
    threadID ThreadID
}
```

它负责：

- 提交 `Op`。
- 接收 SessionEvent、InteractiveRequest 和 AgentStatus。
- shutdown、wait 和 flush 生命周期。
- 隔离 Interface 与内部 Session 实现。

它不直接执行 Reactor，不持有 SessionState、ActiveTurn 或 Context History。

### 8.5 SessionIo、Submission、Event 与 Request

`SessionIo` 是 AmadeusThread 与内部 Session Loop 的双向协议边界：

```go
type SessionIo struct {
    Submissions chan<- Submission
    Events      <-chan SessionEvent
    Requests    <-chan InteractiveRequest
    Status      <-chan AgentStatus
    Terminated  <-chan struct{}
}
```

通道语义：

- `Submission`：Interface/Application 发往 Session 的命令。
- `SessionEvent`：Session 发出的单向生命周期、业务项和增量通知。
- `InteractiveRequest`：Session 发出的、必须由用户或调用方回答的 Approval/User Input 请求。
- `AgentStatus`：面向状态栏和外部观察者的轻量 Thread 状态快照，不替代 Turn 终态事件。
- `Terminated`：Session Loop 已停止且清理完成。

`Submission` 与输出协议严格分离。普通 Runtime Event 不承担请求—响应职责；Approval 和模型主动询问用户必须通过 `InteractiveRequest`，回答再作为新的 `Submission/Op` 返回 Session。

首批 Op：

- `UserInputOp`
- `InterruptOp`
- `ApprovalDecisionOp`
- `UserInputResponseOp`
- `CompactOp`
- `ThreadSettingsOp`
- `ShutdownOp`

Interface 不直接保存 cancel function，也不直接调用 ActiveTurn 或 Store。

### 8.6 internal Session

内部 `Session` 对齐 Codex internal Session，是 Agent 会话真正的 Runtime 所有者：

```go
type Session struct {
    threadID   ThreadID
    state      SessionState
    services   SessionServices
    activeTurn *ActiveTurn
    inputQueue InputQueue
}
```

它负责：

- 运行长期 Submission Loop。
- 接纳用户输入和其他 Op。
- 创建 TurnContext，并记录 TurnStarted、TurnCompleted 或 TurnAborted canonical RolloutItem。
- 创建、持有、调度和取消 SessionTask。
- 路由 Approval Decision 与 Interrupt。
- 决定需要记录的 Runtime 事实，通过 LiveThread 追加 canonical RolloutItem；瞬时 Delta、Working 和未决交互请求不进入 canonical Rollout。
- 使用 InitialHistory 重建 SessionState 与 ContextManager 投影。
- 在持久化和 flush 后发布 Turn 终态事件。
- 对尚未写入 `turn_started` 的启动失败发布 `TurnRejected`，避免 Interface 永久等待一个从未被接纳的 Turn。

同一 Session 最多只有一个前台 ActiveTurn。

### 8.7 SessionState

`SessionState` 是跨多个 Turn 持续存在的会话级可变内存状态，不是数据库记录：

```go
type SessionState struct {
    Configuration        SessionConfiguration
    CanonicalHistory     []RolloutLine
    PreviousTurnSettings *PreviousTurnSettings
    Permissions          SessionPermissionContext
}
```

Runtime + Persistence 阶段先保存经过深拷贝和并发保护的 canonical history，并从 `InitialHistory` 恢复 `PreviousTurnSettings`；B 工作流再以这份 canonical history 构建正式 `ContextManager`。SessionPermissionContext 每次启动都重新初始化。ContextManager 只能是 Rollout 的派生投影，不能成为第二历史源；StoredThread 只用于定位 Rollout 和展示索引元数据。

### 8.8 SessionServices

`SessionServices` 保存 Runtime 核心在 Session 生命周期内复用的能力：

```go
type SessionServices struct {
    LiveThread  *LiveThread
    TaskFactory SessionTaskFactory
    Clock       Clock
    NextID      IDFactory
}
```

具体 `SessionTaskFactory` 是 Session 级 capability owner。当前 `RegularTask` 工厂持有 Provider、Prompt/Context、Tool、Approval、MCP、Skill 和 Web 的组合依赖，并由 Session 统一关闭；B、C、D、F 工作流分别收敛这些 capability 的 typed contract，但不会把生命周期重新交还给 CLI/TUI。这样 internal Session 只调度 SessionTask，不直接耦合每一种能力。

SessionServices 不直接持有 SQLite Repository、JSONL 文件句柄或 Rollout Path；这些细节封装在 LiveThread → ThreadStore → LocalThreadStore 中。

### 8.9 LiveThread、ThreadStore 与 LocalThreadStore

`LiveThread` 对齐 Codex LiveThread，是活动 Session 使用的持久化句柄：

```text
SessionServices.LiveThread
→ LiveThread.AppendItems
→ ThreadStore.AppendItems
→ LocalThreadStore
```

`ThreadStore` 是存储无关边界，第一版只定义创建、恢复、追加、flush、shutdown、读取历史、读取/列出/更新 Thread metadata 和删除 Thread 所需方法。

`LocalThreadStore` 是第一版生产实现：

- `RolloutRecorder` 负责 JSONL 创建、append、flush、resume 和 shutdown。
- `StateDB` 负责 StoredThread 的 SQLite 查询索引。
- 每个 Thread 只有一个活动 Writer，并使用 Thread 级锁串行化 append/flush/shutdown。
- Append 必须先 durable write + flush JSONL，再由 MetadataSync 更新 SQLite。
- Metadata 更新失败不能让 SQLite 超前于 Rollout；后续通过 backfill/reconciliation 补齐。

Session 不知道 ThreadStore 的具体实现，RolloutRecorder 也不推导 Runtime metadata。

### 8.10 Turn 与 TurnContext

Turn 是 Session 内一次具有明确开始、完成、失败或中断生命周期的工作。普通用户输入创建 regular Turn，`/compact` 创建 compact Turn。

TurnContext 是 Turn 启动时冻结的单本地环境纯数据快照：

```go
type TurnContext struct {
    ThreadID ThreadID `json:"thread_id"`
    TurnID   TurnID   `json:"turn_id"`

    Provider string `json:"provider"`
    Model    string `json:"model"`

    CWD   string `json:"cwd"`
    Shell string `json:"shell,omitempty"`

    CurrentDate string `json:"current_date,omitempty"`
    Timezone    string `json:"timezone,omitempty"`

    InitialPermissionMode PermissionMode `json:"initial_permission_mode"`
    Personality            Personality    `json:"personality,omitempty"`

    ToolNames    []string        `json:"tool_names,omitempty"`
    OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}
```

TurnContext 创建后不再修改。它不包含 Client、API Key、Mutex、Cancellation、Telemetry 或其他进程对象，因此可以直接作为 `turn_context` RolloutItem payload 持久化。

以下内容明确不属于 TurnContext：

- BaseInstructions 和 ContextManager 属于 SessionConfiguration/SessionState。
- Provider Client、Tool Registry、Instruction Resolver、MCP 与 Skill Runtime 属于 SessionServices。
- Cancellation、done、execution handle、timing 和 terminal error 属于 RunningTask/TurnState。
- 完整 Tool Specs 由 Tool Registry 提供；TurnContext 只记录该 Turn 暴露的 Tool 名称快照。

### 8.11 ActiveTurn 与 TurnState

`ActiveTurn` 表示 Session 当前正在执行的 Turn：

```go
type ActiveTurn struct {
    State *TurnState
    Task  *RunningTask
}
```

TurnState 保存 Usage、Tool Call 计数、Pending Approval、Pending User Input 和终态标记；当前可见 Plan 由 SessionState 持有。SessionPermissionContext 属于 SessionState，不进入单个 TurnState；Turn 完成后，Session 清除 ActiveTurn，历史 Turn 生命周期继续保存在 canonical Rollout 中。

### 8.12 RunningTask 与 SessionTask

`SessionTask` 对齐 Codex SessionTask：它由 Session 创建、持有、调度和取消，并通常驱动一个 Turn。

```go
type SessionTask interface {
    Kind() SessionTaskKind
    Run(context.Context, TaskHost, *TurnContext, []TurnInput) (TaskResult, error)
    Abort(context.Context, TaskHost, *TurnContext) error
}
```

`TaskHost` 只暴露 canonical append 与 history snapshot，不暴露 SessionState 或 Channel 所有权。`RunningTask` 是 SessionTask 的一次真实运行实例，持有 SessionTask、TurnContext、CancelCause、单次 buffered done channel 和 cleanup；Task panic 也必须转换为唯一 Completion，不能让 Session 永久停留在 Working。

首批 SessionTask：

- `RegularTask`：执行 Plan-guided ReAct。
- `CompactTask`：执行上下文压缩。

当 TurnContext.InitialPermissionMode 为 `plan` 时，RegularTask 进入 Plan Mode；它不是独立 SessionTask。SessionTask 也不是用户计划中的 Task，不进入 DAG。

### 8.13 RolloutLine 与 RolloutItem

`RolloutItem` 是 Thread 中按序持久化的 canonical 事实；`RolloutLine` 为其增加 sequence 和 timestamp，并编码为一行独立 JSON：

```go
type RolloutLine struct {
    Version   int
    Sequence  uint64
    Timestamp time.Time
    ThreadID  ThreadID
    TurnID    TurnID
    Item      RolloutItem
}
```

RolloutItem 至少包括：

- `session_meta`
- `turn_context`
- `turn_started`
- `response_item`
- `turn_item_completed`
- `plan_update`
- `compaction`
- `token_usage`
- `turn_completed`
- `turn_aborted`
- `context_update`

`response_item` 保存模型可见的 User/Assistant/Reasoning/Tool Call/Tool Result 事实；`turn_item_completed` 保存可独立 Replay 的完成态 TurnItem。未决 Approval/User Input Request、ItemStarted、Delta 和 Working 不进入 canonical Rollout。TUI 可以显示更丰富的瞬时 Event，但恢复上下文和历史展示只依赖持久化 RolloutItem。

### 8.14 Iteration

Iteration 是 Reactor 的一次逻辑循环：

```text
ContextManager.ForPrompt
→ Build Prompt
→ Think
→ Tool Calls or Final Response
→ Execute Tools
→ Observe Tool Results
→ Continue or Complete
```

Iteration 是运行时术语，不建表，也不作为 TUI 强制分隔边界。

## 9. Canonical Runtime 流程

### 9.1 Canonical Turn Flow

```text
User Input
→ AmadeusThread.Submit(UserInputOp)
→ SessionIo.Submissions
→ internal Session 接纳输入
→ 构建 TurnContext
→ 创建 RegularTask
→ LiveThread 物化 SessionMeta（仅首次）
→ 追加 turn_context、用户 response_item 与 turn_started
→ flush JSONL canonical rollout
→ 创建 RunningTask 并设置 ActiveTurn
→ 发布 TurnStarted
→ 构建必要的动态 Context Update
→ SessionState.Context.ForPrompt 生成标准化历史
→ Session BaseInstructions + 历史 + 可见 Tool Specs 构建 Prompt
→ LLM Stream
→ ItemStarted + Assistant/Reasoning Delta 或 Tool Call
→ Tool + InteractiveRequest/Approval Runtime
→ LiveThread 追加模型 response_item、Tool Result 与 completed TurnItem
→ 发布 ItemCompleted
→ Reactor 继续
→ Final Response
→ LiveThread 追加 Assistant response_item、completed AssistantMessageItem 与 turn_completed
→ flush JSONL canonical rollout
→ MetadataSync 更新 SQLite StoredThread
→ Session 清除 ActiveTurn
→ 发布 TurnCompleted
→ TUI 显示 Worked for ...
```

关键不变量：

1. 任何模型调用和文件副作用前必须已将 TurnContext、用户输入和 TurnStarted 写入并 flush canonical rollout。
2. ToolCall 与 ToolResult 必须可配对；Completed TurnItem 必须可以独立 Replay。
3. Turn 终止只能记录 TurnCompleted 或 TurnAborted；失败由 TurnCompleted 的 failed 状态表达。
4. Turn 终态 Event 必须在持久化、rollout flush 和 ActiveTurn 清理后发布。
5. TUI 消失或动画停止不能代替 Turn 终态。
6. Resume 通过 StoredThread 定位 Rollout，并由 InitialHistory 重建语义，不恢复 Go goroutine 或旧 RunningTask。
7. `TurnRejected` 只表示 Turn 尚未进入 canonical started 状态；已写入 `turn_started` 的 Turn 必须以 `turn_completed` 或 `turn_aborted` 收尾。

A 阶段的 `SessionIo` 是唯一 canonical 生命周期协议。D 阶段完成后，Reactor、Tool 和 Application 只通过 `Session.Publish` 进入统一 `SessionEvent` 主链；不存在第二套 Event Hub、Metadata 注入或兼容终态通道。

### 9.2 Go Runtime Concurrency Model

Amadeus 学习 Codex 和 Claude Code 的架构语义，但 Runtime 必须使用符合 Go 习惯的所有权、取消和并发模型，而不是逐类或逐文件翻译其他语言实现。

#### Session Loop

每个活动 internal Session 拥有一个长期 Session Loop goroutine：

```text
Session Loop
├── 接收 Submission
├── 路由 Interactive Response
├── 接收 RunningTask Result
├── 管理 ActiveTurn
├── 发布 SessionEvent / InteractiveRequest
└── 处理 Shutdown
```

Session Loop 是 SessionState、ActiveTurn、InputQueue 和 Turn 接纳顺序的主要单一所有者。优先通过单 goroutine 所有权避免为每个字段增加 Mutex；跨 goroutine 共享的只读 Snapshot、缓存或进程状态才使用短临界区 Mutex/RWMutex。

#### RunningTask

前台 `SessionTask` 以独立 RunningTask goroutine 执行，使 Session Loop 在模型、Tool 或 Approval 等待期间仍可处理 Interrupt、ApprovalDecisionOp、UserInputResponseOp 和 Shutdown。

- 同一 Session 最多只有一个前台 ActiveTurn/RunningTask。
- RunningTask 必须有明确 parent Context、CancelCause、Done Result 和 cleanup owner。
- RunningTask goroutine 无论正常返回、Context 取消还是 panic，都只能发送一次 Completion 并关闭自己的 done channel。
- RunningTask 不能直接关闭 SessionIo Channel，也不能在返回后继续修改 SessionState。
- Bubble Tea `tea.Cmd` 或其他 Interface 后台任务返回只表示界面 goroutine 完成，不构成第二套 Turn 终态。

#### Context 取消树

取消关系固定为：

```text
Application Context
└── AmadeusThread Context
    └── Session Context
        └── Turn Context
            ├── LLM Request Context
            ├── Tool Context
            ├── Approval Wait Context
            └── Process Context
```

- 优先使用带 cause 的取消，使用户中断、Session Shutdown、Provider Timeout、Tool Timeout 和 Approval Cancel 可区分。
- Provider、Tool、HTTP 和 Process API 必须接受 `context.Context`，不能创建脱离 Turn 的无主后台任务。
- `context.WithoutCancel` 只用于 Rollout terminal append、flush、MetadataSync 和必要审计等终态收尾，并必须再包一层有限超时。
- 所有 goroutine 都必须有明确 Owner、退出条件和测试覆盖；不允许 fire-and-forget goroutine。

#### Channel 所有权

Session 创建并拥有 SessionIo 的内部双向 Channel；Interface 只持有方向受限的端点：

```go
type SessionIo struct {
    Submissions chan<- Submission
    Events      <-chan SessionEvent
    Requests    <-chan InteractiveRequest
    Status      <-chan AgentStatus
    Terminated  <-chan struct{}
}
```

- Channel 只用于生命周期和所有权边界，不替代普通同步函数调用。
- 创建并发送数据的一方负责关闭 Channel；消费者不得关闭接收端。
- Session 退出时按固定顺序停止接纳 Submission、取消 RunningTask、完成持久化、关闭输出并通知 Terminated。
- Event/Request Channel 使用有界缓冲；高频 Delta 可以在投影层合并，Turn/Item 终态不得静默丢失。
- 不建立支持任意 Subscriber、Topic、Priority 和 Critical Backpressure 的通用 Event Bus。

#### Interactive Request Waiter

公开协议保持 `InteractiveRequest → Response Op`，内部 Approval/User Input Runtime 可以按 RequestID 使用一次性 buffered response channel 等待结果。Session Loop 收到 Response Op 后定位 waiter 并交付 Decision；Turn Context 取消时 waiter 必须同步释放。

#### 有界 Tool 并发

Reactor Iteration 保持串行；只有同一次模型响应中由 Tool 的 `SupportsParallelToolCalls` 声明为安全的独立 Tool Call 可以并行：

- 使用带 Context 的有界 task group，限制并发数并在错误或取消时停止剩余任务。
- 结果按原 Tool Call 顺序回灌模型，不按 goroutine 完成顺序改变协议。
- `read/glob/grep/view_image/web_search/web_fetch` 可以有界并行。
- `write_stdin` 在 Tool Registry 层声明可并行，但 ProcessManager 必须按 `process_id` 串行化同一进程的输入与轮询；不同进程可以并行。
- `edit/write/execute_command/update_plan/MCP Call` 串行。
- 不建立长期 Worker Pool、DAG Scheduler、资源级读写图或自定义共享/独占 Gate。

#### 同步持久化与进程 goroutine

- Canonical JSONL append/flush 第一版保持同步单 Writer，不为展示并发能力而增加后台 Writer goroutine。
- SQLite MetadataSync 发生在 durable JSONL 之后；SQLite 失败不得回滚已写入的 canonical history，后续通过 reconciliation 修复。
- 长期子进程可以拥有独立 waiter goroutine 和 done channel，因为其生命周期天然独立；Owner 使用 TurnID/RunningTask ID，并受 Turn Context 取消。
- 单次 Web/Provider HTTP 调用保持同步 Context API；HTTP Client 负责连接池，多次独立调用由 Tool Executor 做有界并发。

## 10. Agent Engine：Plan-guided ReAct

### 10.1 单一 Reactor

Amadeus 只有一套 Agent 执行循环：

```text
Think → Act → Observe → Continue/Complete
```

- Think：基于标准化 Prompt 调用模型。
- Act：模型选择 Tool 或给出最终回答。
- Observe：将 ToolResult 写入 Rollout，并提供给下一次采样。
- Continue：任务未完成时继续下一 Iteration。
- Complete：模型给出最终回答并结束 Turn。

不建立 Direct Engine、Planned Engine 两套实现。

### 10.2 默认 Plan-guided

默认执行模式与 Codex 对齐：

- `update_plan` Tool 默认可用。
- 简单任务不要求创建计划。
- 复杂、多阶段或长时间任务可以创建计划。
- 模型在执行过程中可以更新计划状态。
- SessionState.Plan 只用于方向、进度和用户可见性，不决定 Tool 调度。
- Runtime 不把计划编译为 DAG。

计划项状态保持简单：

```text
pending → in_progress → completed
```

同一时刻最多一个 `in_progress` 项。计划必须反映真实进度，不能在任务结束时一次性伪造全部完成。

### 10.3 `/plan` Plan Mode

`/plan` 与默认 Plan-guided 执行不是两套 Agent Engine。

Plan Mode 的语义：

- 允许读取、搜索、分析项目。
- 禁止 `edit`、`write` 和 `execute_command` 等副作用 Tool。
- 模型可以提出澄清问题。
- 模型输出可执行计划，而不是实施修改。
- 计划作为 RolloutItem 保留在 Session 中。
- 用户确认开始实施后，由后续普通 Turn 使用同一 Reactor 执行。

`/plan` 的重点是**强制只规划不实施**，而不是强制生成 DAG。

### 10.4 Completion、Failure 与 Interruption

- 模型返回最终回答时 Turn 完成。
- Provider、Context 或 Tool 的不可恢复错误使 Turn 以 `TurnCompleted{status: failed}` 结束。
- 用户按 Esc 取消时 Turn 以 `TurnAborted` 结束。
- 中断时正在执行的 Tool 应尽力取消，并补齐 ToolResult/Marker 协议。
- 用户随后输入“继续”时创建新 Turn；SessionState.Context 提供上次中断的事实，由模型重新评估和重新计划，不精确恢复旧执行位置。

## 11. Plan Tool

`update_plan` 是 Runtime Tool，不是普通外部副作用 Tool。

```go
type PlanItem struct {
    Step   string
    Status PlanStatus
}

type PlanState struct {
    Explanation string
    Items       []PlanItem
    UpdatedAt   time.Time
}
```

`update_plan`：

- 输入 Schema 使用 `plan` 条目和可选 `explanation`；Handler 校验状态值、空步骤和“至多一个 `in_progress`”，不接受多套等价字段。
- Handler 只解析参数并调用 Session 提供的 `UpdatePlan` capability，不持有独立 PlanState 或 Recorder。
- Session 先将 `plan_update` 写入 canonical JSONL Rollout，持久化成功后再提交 `SessionState.Plan` 内存投影。
- 不需要用户 Approval，也不经过文件权限检查。
- 持久化完成后产生 `PlanUpdated` Event，TUI 使用 Codex 风格 `Updated Plan` HistoryCell 展示。
- ToolResult 只返回类似 `Plan updated` 的简短确认；完整计划只通过 `PlanUpdated` Event、TurnItem 和 SessionState 投影传播，避免模型结果与 Runtime 状态形成双事实源。
- Resume 时 Session 从最近的 `plan_update` 恢复投影，并从已有 revision 继续递增。

C-T 只负责 `update_plan` 的 ToolDefinition、输入校验、Session capability 调用、Event handoff 和 concise result；Plan State、Resume、Plan Mode 与 Reactor 的端到端完成仍属于 F 阶段。

## 12. Prompt 与 Context

### 12.1 Prompt 分层

单次模型请求使用明确的 Prompt 数据模型：

```go
type BaseInstructions struct {
    Text string
}

type Prompt struct {
    BaseInstructions  BaseInstructions
    Input             []llm.ResponseItem
    Tools             []tool.Spec
    ParallelToolCalls bool
    OutputSchema      json.RawMessage
}
```

Prompt 构建主链固定为：

```text
SessionConfiguration.BaseInstructions
+ SessionState.Context.ForPrompt()
+ ToolRegistry.ModelVisibleSpecs()
+ TurnContext.OutputSchema
→ Prompt
→ ModelClient
```

各类内容只有一个所有者：

- BaseInstructions：稳定的 Coding Agent 身份、完成标准、沟通方式和跨 Tool 行为纪律，属于 SessionConfiguration。
- Dynamic Context Updates：Developer Instructions、AGENTS.md、Environment、Permission Mode、Skill 和 MCP 的当前事实，转换为 ResponseItem 后进入 ContextManager。
- Conversation：User、Assistant、ToolCall 和 ToolResult，由 ContextManager 从 canonical Rollout 投影和维护。
- Tool Guidance：Tool 名称、描述和 Input Schema 位于 Tool Spec；只有跨 Tool 纪律保留在 BaseInstructions。
- Output Schema：只由当前 TurnContext 提供，不写入静态 Prompt。

禁止把动态路径、当前权限、模型名或 Tool 列表硬编码进静态模板。

Session 使用固定的 `buildContextUpdates` 流程，按 Developer Instructions、AGENTS.md、Environment、Permission Mode、Skills 和 MCP 的稳定顺序生成 ResponseItem。

动态 Context Update 使用稳定的 replace key。Session 初始化、Resume 或 SessionPermissionContext 变化时，下一次模型采样可以看到新的临时 Permission Context Update；该 Update 只描述当前 Session 能力，不把 grant 变成持久化权限。历史 Approval Decision 可以作为事实保留，但 Resume 时不会重新授予权限。

Prompt 资产位于 `internal/prompt`，按 BaseInstructions、Context Update、Tool Spec 和 Compaction Prompt 分层。

### 12.2 AGENTS.md

指令优先级：

```text
更深目录 AGENTS.md
> 项目根 AGENTS.md
> AMADEUS_HOME/AGENTS.md
```

- 用户级指令位于 `$AMADEUS_HOME/AGENTS.md`。
- 项目级指令从 Project Root 向目标文件目录逐级发现。
- 更深目录只覆盖其目录子树。
- Context 中注入生效内容，不只提供文件路径让模型自行读取。
- 指令文件来源、作用域和 Hash 可进入诊断信息，但 Hash 不是加密或隐藏内容。

### 12.3 ContextManager

ContextManager 属于 SessionState，是当前模型可见历史的唯一所有者：

```text
Canonical Rollout
→ SessionState.ContextManager
→ Record / Replace / Atomic Rebuild / UpdateUsage
→ ForPrompt(ModelInfo)
→ []ResponseItem
```

它负责：

- 记录模型可见的 User、Assistant、ToolCall、ToolResult 和 Context Update。
- 保持 ToolCall/ToolResult 原子配对。
- 删除孤立 ToolResult，并为中断或缺失结果补显式 synthetic ToolResult。
- 按 ModelInfo 的限制投影超大 Tool Result。
- 根据模型输入能力移除不支持的内容类型。
- 应用 Compaction Replacement History。
- 保存 Provider Usage、估算值和 history version。
- 返回不可变的 Prompt 输入快照。

ContextManager 是 canonical Rollout 的派生投影，不是第二事实源。Reactor、TUI、CLI 和 CompactTask 不得各自实现第二套历史裁剪或消息投影。

### 12.4 Token Accounting

Token 预算来源按优先级处理：

1. Provider 返回的真实 usage，是已完成请求的权威统计。
2. 发送请求前使用模型 tokenizer；未实现专属 tokenizer 时使用保守 TokenEstimator。

TokenEstimator 只用于发送前容量预估，不得覆盖 Provider 已返回的真实 Usage。UI 展示估算值时必须标记为 estimated。

ModelInfo 至少保存：

```text
context_window
auto_compact_token_limit
max_output_tokens
supports_parallel_tool_calls
tool_output_max_tokens
```

- `provider.max_output_tokens` 只表示单次模型请求最大输出。
- `max_output_tokens` 只由 ModelInfo/Provider Request 定义。
- `context_window` 是模型硬上限；`auto_compact_token_limit` 默认取 context window 的 90%，显式配置只能进一步收紧。
- Provider Usage 只作为已完成请求的权威统计；下一次请求容量判断始终重新估算当前完整 Prompt，避免把上次请求 Usage 当成不同 Prompt 的容量值。
- 完整 Prompt 估算包含 BaseInstructions、ContextManager 输入、模型可见 Tool Specs 与 OutputSchema。
- `base_tokens_remaining = min(auto_compact_token_limit - active_context_tokens, context_window - active_context_tokens)`。
- 单次请求还必须满足 estimated input + max output 不超过 context window。
- Token Budget 不按 System、Instructions、History、Tools 或 Resources 设置固定百分比分区。

### 12.5 Tool Result Projection

大输出不能原样无限进入模型：

- 保留 Tool 名、状态、关键摘要和输出边界。
- 文件读取保留相关片段、行号和截断信息。
- 文件修改保留 Typed `FileChangeResult` 的操作、路径、统计、Diff 摘要和最终状态；模型投影不能丢失 declined、stale 或 partial failure。
- Shell 保留命令、退出码、关键 stdout/stderr 和截断信息。
- 搜索保留匹配路径、行号和总匹配数。
- canonical Rollout 保存完整原始 Tool Result；`ContextManager.ForPrompt` 只返回模型安全投影。
- 投影失败必须产生显式 Context Error，不允许静默丢失。

### 12.6 Compaction

Compaction 是 Runtime 用例，不是 TUI 文本操作：

```text
CompactOp / Auto Compact Trigger
→ CompactTask
→ Canonical history projection + History Normalization
→ Build Compaction Prompt
→ 无 Tool 的 LLM Summary
→ Build Replacement History
→ Append Compaction + TokenUsage RolloutItem
→ Session 原位 Atomic Rebuild ContextManager
```

- `/compact` 触发手动压缩。
- 新 Turn 首次采样前，以及 ReAct 中 Tool Result 后准备再次采样前，都检查自动压缩阈值。
- 原始 RolloutItem 不删除。
- Replacement History 只改变后续 ContextManager 模型投影。
- Summary 必须保留用户目标、已修改文件、Tool 结果、失败、未完成事项和关键决策。
- ToolCall/ToolResult 配对不能被压缩成协议非法历史。
- D 工作流通过统一 SessionEvent/TurnItem 投影压缩开始、完成和失败；CompactTask 本身只返回 canonical 结果与错误，不另建第二事件通道。
- LLM Compaction 失败时保持原 ContextManager 不变并返回明确错误，不使用本地摘要替代。
- Replacement History 至少包含初始用户目标和带明确 checkpoint 前缀的 Summary；最近用户消息保留在压缩覆盖范围之外。

## 13. LLM Domain 与 Provider Adapter

### 13.1 Domain Port

Agent Runtime 依赖 Amadeus LLM Domain，而不直接依赖 SDK 类型：

```go
type Client interface {
    Stream(context.Context, Request) (Stream, error)
}
```

Domain Request 统一表达：

- system/developer/user/assistant/tool 语义。
- Tool definitions。
- Tool call 与 Tool result 配对。
- model、temperature、max output tokens。
- reasoning 与 usage。

### 13.2 OpenAI Adapter

`internal/llm/openai` 是 Adapter，不称为 Domain Client。

它负责：

- Responses API 请求转换和流聚合。
- Chat Completions 请求转换和流聚合。
- Provider Dialect 的最小兼容差异。
- Developer role 降级。
- Tool call argument 增量聚合。
- Provider Error 和 Usage 归一化。

它不负责：

- Agent Loop。
- Prompt 业务规则。
- Tool 参数业务校验。
- Approval。
- Context Compaction。

### 13.3 API Mode

支持：

| 值 | 用途 |
|---|---|
| `responses` | OpenAI Responses API，默认优先 |
| `chat_completions` | OpenAI-compatible Chat Completions |

Provider 预设可以提供默认 Base URL、API Mode 和 Dialect，但用户仍可以显式覆盖 Base URL。

### 13.4 Provider Dialect

Dialect 只处理经过验证的协议差异，不根据域名猜测：

- `openai`
- `deepseek`
- `glm`
- `qwen`
- `generic_openai`

具体差异必须由契约测试覆盖，包括 role 支持、reasoning 字段、tool call delta 和 usage。

## 14. Tool 架构

### 14.1 目标模型

Amadeus 保留 Codex 的 Registry、Session、Turn、Event 和 Rollout 边界，但将 Tool 内层协议改为 Claude Code 风格的显式阶段。`ToolExecutionService` 仍是唯一编排入口，不新增第二套 Router、Handler 或 Permission Engine。

```text
Model Tool Call
→ Tool Registry.Lookup
→ NormalizeInput
→ ToolDefinition.ValidateInput
→ ToolDefinition.Prepare
→ PermissionService.Evaluate
→ Allow / Ask / Deny
→ ApprovalCoordinator（仅 Ask）
→ ToolDefinition.Execute(prepared)
→ Typed ToolResult
→ ToolDisplayResult / FileChangeItem
→ SessionEvent / Canonical Rollout / ContextManager / TUI
```

每个 Tool 必须表达同一组概念：

```go
type ToolDefinition interface {
    Name() string
    Spec() ToolSpec
    ValidateInput(ToolUseContext, any) error
    Prepare(ToolUseContext, any) (PreparedToolUse, error)
    Execute(ToolUseContext, PreparedToolUse) (ToolResult, error)
    SupportsParallelToolCalls() bool
}
```

`ValidateInput` 只负责 Schema、类型、字段关系和工具输入的客观合法性；`Prepare` 负责解析路径、读取必要快照、计算副作用、生成 Approval 所需的结构化预览，但不得产生最终文件副作用；`Execute` 只能执行已经通过权限和 Approval 的 `PreparedToolUse`。工具不得在 `Execute` 中重新解释原始模型输入，也不得通过隐式全局状态恢复准备数据。

```go
type ToolUseContext struct {
    SessionID     string
    TurnID        string
    CWD           string
    Permission    *SessionPermissionContext
    FileReadState *FileReadStateStore
    FileSystem    FileSystemPolicy
    Abort         <-chan struct{}
    Events        EventSink
}

type PreparedToolUse struct {
    Invocation Invocation
    Input      any
    State      any
    Approval   *policy.ApprovalRequest
}

type ToolResult struct {
    CallID      ToolCallID
    Status      ToolResultStatus
    Data        any
    Error       *ToolError
    Display     ToolDisplayResult
}

type FileChangeResult struct {
    Path           string
    Operation      FileOperation
    OriginalFile   *string
    UpdatedFile    *string
    Diff           *FileChangePreview
    UserModified   bool
}
```

`PreparedToolUse.State` 是 Tool 私有的、显式传递的准备态；核心链不得依赖 `context.WithValue` 注入准备数据。`ToolUseContext` 是一次 Tool Use 的运行时上下文，不等同于可持久化的 `TurnContext`；它可以引用 Session 内存状态，但不能把权限 grant 或未决 Approval 写入 Rollout。

`ToolExecutionService` 固定执行以下顺序：

```text
Lookup
→ NormalizeInput
→ ValidateInput
→ Prepare
→ PermissionService.Evaluate(prepared)
→ ApprovalCoordinator.Decide（ask 时）
→ Apply in-memory grant（如有）
→ Execute(prepared)
→ Build typed result and display projection
```

以下状态必须明确区分：

```text
Validation Failure   参数或客观输入无效，不进入审批、不执行
Preparation Failure  无法读取快照、计算 Diff 或建立安全执行态，不执行
Permission Deny      客观边界或 Session 规则明确拒绝，不执行
Approval Ask         需要用户选择，等待 InteractiveRequest
Approval Decline     用户拒绝，零副作用并返回 declined ToolResult
Execution Failure    已获准执行但 Execute 失败
```

文件 Diff 由 `Prepare` 生成，`ApprovalCoordinator` 原样传递，InteractiveRequest 保留其结构化数据，TUI 只展示并返回决定；Typed ToolResult 和 Rollout 保存最终事实。Tool 定义不能读取 TUI、解析键盘输入、直接更新 Session 权限上下文或自行发布审批交互。

### 14.2 SessionPermissionContext 与 FileSystemPolicy

`FileSystemPolicy` 只表示客观文件系统边界，不承担用户授权状态：

```go
type FileSystemPolicy struct {
    CWD           string
    ReadHost      bool
    WorkspaceRoots []string
    ReadOnlyRoots []string
    DeniedRoots   []string
}
```

它负责 canonical path 是否越界、是否命中 DeniedRoots、是否允许读取，以及是否具备客观写入条件。它不保存 `allow once`、`allow for session`、Run grant 或 Session grant。

路径与用户授权分成三层：

```text
PathResolver
→ 相对路径、~、分隔符和 symlink 解析为 canonical path

FileSystemPolicy
→ 客观 workspace/read-only/denied 边界检查

PermissionService + SessionPermissionContext
→ 当前 Tool Use 的权限评估与 Session 内存 grant 匹配
```

权限数据模型必须区分授权能力，不能用一个宽泛的“文件权限”覆盖所有操作：

```go
type SessionPermissionContext struct {
    Mode             PermissionMode
    ReadDirectories  []string
    EditDirectories  []string
    CommandGrants    map[CommandApprovalKey]struct{}
    ExternalGrants   map[string]struct{}
}

type PermissionGrant struct {
    Kind      GrantKind
    Scope     GrantScope
    ExpiresAt *time.Time // 第一版为 nil；Session 结束即失效
}
```

至少区分 `read directory`、`edit directory`、`exact command` 和 `external host/tool` 四种 Grant。Read grant 不能隐式允许 edit；edit grant 不能隐式允许 command；每种 Grant 只能由对应 Tool 的 `PermissionService` 匹配。

`SessionPermissionContext` 由 internal Session 持有；`PermissionService` 只读取它并返回 `PermissionDecision`，`ApprovalCoordinator` 只负责等待用户决定，最终由 Session 统一应用内存中的 `PermissionGrant`。Amadeus 不实现 Claude Code 的用户级、项目级或本地权限持久化；Session Close、进程退出和 Resume 后都恢复默认权限状态。
### 14.3 Tool 分类

```text
Read-only Tools
Structured Mutation Tools
Process Tools
Runtime Tools
Remote Tools
```

工具分类只描述默认的验证、权限和并发倾向，不创建多套执行器。每个 Tool 仍然通过同一个 `ToolExecutionService` 执行；并发能力由 `SupportsParallelToolCalls` 声明，输入校验和准备由 ToolDefinition 显式完成，权限由 `PermissionService` 统一评估。

### 14.4 初始内置 Tool 集


默认向模型暴露：

```text
read
edit
write
glob
grep
execute_command
update_plan
```

按环境和能力条件暴露：

```text
write_stdin
view_image
skill
web_search
web_fetch
MCP Tools
request_user_input
```

模型可见 Tool Catalog 和默认 Core Registry 只包含当前公开工具：`read`、`edit`、`write`、`glob`、`grep`、`execute_command`、`write_stdin`、`update_plan`，以及按配置启用的条件工具。旧 `read_file`、`list_dir`、`glob_files`、`grep_code`、`request_permissions` 已物理删除，不再提供兼容注册。

保留 `execute_command` 而不命名为 `Bash`，因为 Amadeus 面向多平台；Skill Script 统一通过它执行。

### 14.5 内置 Tool 的源码参考与优化边界

Amadeus 不按单一项目整套复制 Tool，而是按 Tool 的真实职责选择行为来源：

| Tool/能力 | 主要源码参考 | Amadeus 应复刻的核心语义 | 不复制的产品耦合 |
|---|---|---|---|
| `read` | Claude Code `FileReadTool` | canonical path、offset/limit、文本/图片分流、完整读取后记录 FileReadState、bounded output、typed truncation | LSP、IDE 通知、Analytics、文件历史 |
| `edit` | Claude Code `FileEditTool` | old/new string 校验、唯一匹配、`replace_all`、Read-before-write、Prepare Diff、Approval 后 stale check、原子写入、structured patch/result | React 展示组件、VS Code 集成、产品遥测 |
| `write` | Claude Code `FileWriteTool` | create/update 区分、已有文件完整 Read、覆盖 Diff、Approval 后重新校验、typed create/update result | 文件历史服务、IDE 刷新、持久化权限来源 |
| `glob/grep` | Claude Code `GlobTool` / `GrepTool` | read-only、concurrency safe、稳定排序、数量/字节/Token 上限、`truncated` 与截断原因 | Claude Code 特有搜索服务和 UI 组件 |
| `view_image` | Claude Code 只读 Tool 约束 | MIME/尺寸/路径校验、工作目录内默认 Allow、typed media result | IDE 图片预览与外部产品通知 |
| `execute_command` | Claude Code Permission UX + Codex Process Lifecycle | 默认 Ask、exact command grant、进程 ID、输出/退出码/截断、取消和 lifecycle event | Codex OS Sandbox、Guardian、Remote Environment、网络审批 |
| `write_stdin` | Codex unified exec `write_stdin` | 续接已有进程、不重复 Approval/PreToolUse、绑定 OriginCallID、同进程串行、不同进程可并行、typed process result | Codex Remote Shell 与 sandbox orchestration |
| `update_plan` | Codex `plan` handler/spec | 参数校验、至多一个 `in_progress`、更新 Session Plan、发布 `PlanUpdated`、Tool 返回简短确认 | 独立 Planner、DAG 调度器、Tool 自持 PlanState |
| `request_user_input` | Codex Interactive Request | 业务输入请求与 Permission Approval 分离、结构化问题/选项、通过 Response Op 返回 | 将普通问答写入权限上下文 |
| Web/MCP/Skill | Codex Registry + Amadeus Provider | 统一 ToolUse 生命周期、typed result、host/tool 最小 Grant、bounded output | 完整远端执行环境和持久化授权 |

“复刻核心语义”指按 Go 和 Amadeus Runtime 重写相同行为契约，而不是把 TypeScript/Rust 类型、UI 组件、Sandbox 或产品服务直接搬入。每个内置 Tool 必须使用同一 `ToolExecutionService`，但可以拥有不同的输入、准备态、权限策略和 typed result；不得为了形式统一而把所有 Tool 强塞进文件 Diff 或 Approval 流程。

内置 Tool 的结果至少分成三层：

```text
Execution Result
→ ToolResult.Data            给 Runtime 和模型的稳定事实
→ ToolDisplayResult          给 Event/TUI 的展示投影
→ Canonical TurnItem         给 Rollout/Resume 的完成事实
```

`ToolDisplayResult` 不能反向成为执行事实，模型侧文本也不能作为 TUI 重新解析结构化结果的来源。只读 Tool 必须显式返回截断状态；文件 Tool 必须返回 `FileChangeResult`；进程 Tool 必须返回 `ProcessResult`；Runtime Tool 必须通过 Session capability 修改状态并发布对应 Event。

### 14.6 遗留 `apply_patch` 与 sandbox 隔离

`apply_patch` 只是遗留代码，不是 Amadeus 内置 Tool；sandbox 也不是正式执行主链。两者可以暂时保留用于独立实验、兼容性研究或测试，但必须满足：

- 不注册到默认 Core Registry，也不进入模型可见 Tool Catalog、Prompt Tool Snapshot 或 Skill/MCP Tool Projection。
- 不接入 `ToolExecutionService`、PermissionService、ApprovalCoordinator、SessionEvent、Rollout 或 TUI 的正式链路。
- 不作为 `edit/write` 的底层执行器；结构化文件修改只通过 Claude Code 风格的 `Prepare → Approval → Revalidate → Atomic Apply` 实现。
- 不以 Codex `apply_patch` 的 Sandbox、Guardian、Remote Environment、Network Approval 或 Diff Tracker 反向塑造 Amadeus 当前 Tool Contract。
- 遗留代码能否编译可单独维护，但不作为 C-T 完成条件；若其共享类型阻碍正式主链，应先解除依赖，而不是把遗留能力重新接回主链。

## 15. Claude Code 风格文件修改链

文件修改 Tool 是 ToolUse 协议的第一个完整实现：`ValidateInput` 不产生副作用，`Prepare` 建立可审阅的快照和 Diff，Approval 只决定是否继续，`Execute` 只执行已经重新验证的准备态。

```go
type FileReadState struct {
    CanonicalPath string
    Exists        bool
    ContentHash   string
    Mode          fs.FileMode
    IsSymlink     bool
}

type FileReadStateStore interface {
    Record(FileReadState)
    Lookup(canonicalPath string) (FileReadState, bool)
}

type FileChangePreview struct {
    Operation    FileOperation
    Path         string
    DisplayPath  string
    BeforeExists bool
    BeforeHash   string
    AfterHash    string
    Stats        DiffStats
    Hunks        []DiffHunk
    Truncated    bool
}

type DiffHunk struct {
    OldStart int
    OldLines int
    NewStart int
    NewLines int
    Lines    []DiffLine
}
```

`FileChangePreview` 是 Approval、InteractiveRequest、TUI 和 FileChangeItem 之间的唯一结构化 Diff Contract。它不是最终写入结果，也不能由 TUI 根据文本重新解析；展示可以截断，但底层 Preview 必须保留完整的 Hash、统计和可滚动 Hunks。

### 15.1 Read-before-write

已有文件的结构化修改必须建立在完整 `read` 得到的明确文件状态上：

```text
Read File
→ Record File State
→ Build Structured Diff
→ Approval
→ Re-read + Stale Check
→ Atomic Apply
→ Verify
```

文件状态至少包含：

- canonical path。
- 文件是否存在。
- 原始内容 Hash。

Hash 用于判断内容是否变化，不用于加密文件。

若文件在读取后被外部修改：

- 不允许静默覆盖。
- ToolResult 返回 stale/conflict。
- 模型必须重新读取并重新提出修改。

### 15.2 `edit`

`edit` 用于已有文件的局部字符串替换：

```json
{
  "path": "...",
  "old_string": "...",
  "new_string": "...",
  "replace_all": false
}
```

- 修改已有文件时优先使用 `edit`。
- `old_string` 默认必须唯一存在；零匹配或多匹配返回明确错误。
- Tool Input 本身就是待确认的修改内容，`Prepare` 生成完整的 `FileChangePreview`。
- Preview 包含操作类型、canonical/display path、前后存在性与 Hash、DiffStats、DiffHunk 和截断标记；默认限制 TUI 展示大小，但不丢失 stale check 所需事实。
- Approval 后重新读取文件并执行 stale check，成功后再原子写入。

### 15.3 `write`

`write` 用于创建新文件或完整覆盖文件：

```json
{
  "path": "...",
  "content": "..."
}
```

- 新文件展示全新增 Diff。
- 覆盖已有文件前必须完整 `read`，并展示完整覆盖 Diff。
- Approval 后重新读取并执行 stale check。
- 覆盖风险高于 `edit`；已有文件的小范围修改不得默认使用 `write`。

### 15.4 Approval Contract

权限结果只有三种：

```go
type PermissionBehavior string

const (
    PermissionAllow PermissionBehavior = "allow"
    PermissionAsk  PermissionBehavior = "ask"
    PermissionDeny PermissionBehavior = "deny"
)
```

Approval 请求携带完成交互所需的信息：

```go
type ApprovalRequest struct {
    ID                 ApprovalID
    ToolCallID         ToolCallID
    ToolName           string
    Kind               ApprovalKind
    Title              string
    Question           string
    Path               string
    Command            string
    CWD                string
    Operation          FileOperation
    Diff               *FileChangePreview
    Presentation       ApprovalPresentation
}

type ApprovalPresentation struct {
    Title    string
    Subtitle string
    Question string
    Path     string
    Details  []DisplayLine
    Diff     *FileChangePreview
    Options  []ApprovalOption
}

type ApprovalOption struct {
    ID          string
    Label       string
    Description string
    Outcome     ApprovalOutcome
    Scope       ApprovalScope
}

type ApprovalDecision struct {
    OptionID string
    Outcome  ApprovalOutcome
    Scope    ApprovalScope
    Source   ApprovalSource
    Reason   string
}
```

文件修改 Approval TUI 必须展示：

- Tool 名称。
- 文件路径。
- Structured Diff。
- Insertions/Deletions。
- 选项说明。

Approval TUI 不得把所有 Tool 硬编码为 `Allow / Allow for this session / Deny`。具体标题、问题和选项文本由 Tool 生成的 `ApprovalRequest` 与目标范围生成，TUI 只负责统一的选择、反馈输入和键盘交互。

- `Yes` 只批准当前 Tool Call，不应用 Session grant。
- 带有具体范围的第二选项批准当前调用，并由 ApprovalCoordinator 将对应的 `PermissionGrant` 写入 SessionPermissionContext。
- `No` 不执行 Tool，并把拒绝及可选反馈作为 ToolResult 返回模型。
- Session Permission State 只存在于当前进程内存，Resume 后恢复为默认权限状态。

ApprovalDecision 由 TUI 作为 `ApprovalDecisionOp` 返回 Session；Session 将其交给 `ApprovalCoordinator`，由 Coordinator 应用内存 grant 并唤醒等待中的 Tool。Approval 请求、决定和结果使用同一 typed Contract，但未决请求不进入 canonical history。

#### Claude Code 对齐的 Approval 文案

Amadeus 保留自身 Tool 名称，但直接采用 Claude Code 的文案结构、范围表达和反馈语义。

| 场景 | Title / Question | Options |
|---|---|---|
| `edit` | `Edit file` / `Do you want to make this edit to <file>?` | `Yes`；工作目录内：`Yes, allow all edits during this session`；工作目录外：`Yes, allow all edits in <directory>/ during this session`；`No` |
| `write` 新建 | `Create file` / `Do you want to create <file>?` | 与 `edit` 使用相同的 Session Edit 文案；`No` |
| `write` 覆盖 | `Overwrite file` / `Do you want to overwrite <file>?` | 与 `edit` 使用相同的 Session Edit 文案；`No` |
| 工作目录外 `read/glob/grep/view_image` | `Read file` / `Do you want to proceed?` | `Yes`；`Yes, allow reading from <directory>/ during this session`；`No` |
| 因显式 Ask Rule 触发的工作目录内读取 | `Read file` / `Do you want to proceed?` | `Yes`；`Yes, during this session`；`No` |
| `execute_command` | Bash 环境使用 `Bash command`，PowerShell 环境使用 `PowerShell command`，其他 Shell 使用 `Command`；问题为 `Do you want to proceed?` | `Yes`；`Yes, and don't ask again for <command> commands in <cwd>`；`No` |
| Skill | `Skill` | `Yes`；`Yes, and don't ask again for <skill> in <cwd>`；存在参数前缀建议时可增加 `Yes, and don't ask again for <prefix>:* commands in <cwd>`；`No` |
| `web_fetch` 需要域名确认时 | `Fetch` | `Yes`；`Yes, and don't ask again for <hostname>`；`No, and tell Amadeus what to do differently (esc)` |
| MCP 或通用远端 Tool | `Tool use` | `Yes`；`Yes, and don't ask again for <tool-name> commands in <cwd>`；`No` |

受保护的 `$AMADEUS_HOME` 配置目录使用专门选项：

```text
Yes, allow edits to Amadeus config for this session
```

命令的第二选项必须展示实际将写入 Session Rule 的 exact command 或 prefix，不能只显示泛化的 `Allow for this session`。若 Tool 无法产生安全、明确且可向用户解释的 Permission Update，则不显示第二选项，只显示 `Yes` 与 `No`。

以下 Tool 不创建独立 Approval Dialog：

- `update_plan` 默认 Allow。
- 工作目录内的 `read/glob/grep/view_image` 默认 Allow。
- `write_stdin` 继承原 `execute_command` 的批准状态和 Presentation，不重复询问。
- `request_user_input` 是业务输入请求，不属于 Permission Approval。
- `web_search` 默认 Allow。
- MCP List 与 Resource Read 默认 Allow。

所有拒绝选项支持通过 Tab 添加反馈，底部统一显示：

```text
Esc to reject · Tab to add feedback
```

### 15.5 SessionPermissionContext 与默认规则

当前 Session 只持有一个 `SessionPermissionContext`。它是进程内存中的运行时权限状态，不是数据库表，也不是 Rollout 历史的一部分。Session 创建时初始化，Session Close、进程退出或 Resume 一个历史 Session 时清空；Approval Request、文件内容、Diff 和用户反馈都不写入权限 Context。

Amadeus 不实现 Claude Code 的 `userSettings`、`projectSettings`、`localSettings` 或其他权限持久化来源，也不建立 Approval Persistence Store。`SessionPermissionContext` 只保存当前 Session 已获准的最小 Grant：

```go
type SessionPermissionContext struct {
    Mode             PermissionMode
    ReadDirectories  []string
    EditDirectories  []string
    CommandGrants    map[CommandApprovalKey]struct{}
    ExternalGrants   map[string]struct{}
}
```

Grant 的应用规则固定为：

- `allow once`：只允许当前 PreparedToolUse，不写入 Context。
- `allow for this session`：只写入与 Tool 能力匹配的最小 Grant。
- `deny`：默认只拒绝当前调用，不写入 Context。
- 确定性危险检查：由 `FileSystemPolicy`、`CommandGuard` 等直接 deny，不可被用户 grant 绕过。
- Read grant 不能允许 edit；edit grant 不能允许 command；external host/tool grant 不能扩大到其他 Host 或 Tool。

匹配粒度由 PermissionService 决定，但生命周期统一由 Session 持有：

- `read/glob/grep/view_image`：canonical read directory。
- `edit/write`：canonical edit directory。
- `execute_command`：canonical CWD + 完整规范化 command 或明确安全 prefix。
- `web_fetch` / MCP：精确 hostname、resource 或 tool key。

`FileSystemPolicy` 仍只负责 workspace/read-only/denied roots、canonicalization 和符号链接安全检查。Approval 通过后，`Execute` 前必须重新执行这些客观检查、stale check 和必要的命令危险检查。

#### Claude Code 对齐的文件 Session Approval

`edit` 与 `write` 的 Approval 文案可以相同，但其 Grant 必须按能力区分：

- 默认模式下，结构化文件修改返回 Ask。
- `Yes, allow all edits during this session` 写入当前工作目录的 `edit directory` Grant。
- `Yes, allow all edits in <directory>/ during this session` 写入指定 canonical directory 的 `edit directory` Grant。
- 后续位于已授权目录内的 `edit/write` 可以自动 Allow，但仍执行路径 canonicalization、Denied/ReadOnly roots、符号链接和 stale check。
- 对工作目录外的 `read`，使用独立的 `read directory` Grant；该 Grant 不能让文件修改静默通过。
- 对 `$AMADEUS_HOME`、版本控制元数据和 Shell 启动文件等受保护位置，只提供窄范围 Grant，不扩大整个工作目录。

默认规则：

| Tool | 默认行为 |
|---|---|
| `update_plan` | Allow |
| 工作目录内 `read/glob/grep` | Allow |
| 工作目录外读取 | Ask |
| `edit/write` 默认模式 | Ask |
| `edit/write` accept_edits 且位于 Working Directories | Allow，受安全检查约束 |
| `execute_command` | Ask |
| `write_stdin` | 继承原命令 Approval |
| 工作目录内 `view_image` | Allow |
| Skill Markdown | Allow |
| Skill Script | 走 `execute_command` |
| MCP List/Resource Read | Allow |
| MCP Call | Ask |
| Web Search | Allow |
| Web Fetch 已授权域名 | Allow |
| Web Fetch 未授权域名 | Ask |

Plan Mode 由当前 Agent 的可见 Tool 投影禁止实施副作用；它不修改 Session grant，也不自动批准任意 `execute_command`。

### 15.6 Apply 与 Verify

用户批准后仍必须重新验证 FileVersion：

- Approval 等待期间文件可能变化。
- Revalidation 失败时不能继续写入。
- 写入尽量采用同目录临时文件和原子替换。
- 不允许静默 partial success；若发生部分修改，ToolResult 必须明确列出。

`edit/write` 返回 Typed `FileChangeResult`；`ToolDisplayResult` 再生成模型和 TUI 所需的安全摘要。`execute_command` 只返回命令、输出和退出状态，不提供文件修改归因。

## 16. Command Execution 与 Approval

### 16.1 `execute_command`

```text
Parse Invocation
→ Validate cwd/timeout/tty
→ Allow / Ask / Deny
→ Approval Runtime（仅 Ask）
→ Host Execute
→ Capture Output
→ ToolResult
```

目标行为：

- 所有新命令默认 Ask；命中 Session Command Rule 时 Allow。
- 空命令、NUL、非法 CWD 和少量灾难性命令直接 Deny。
- 校验 timeout、tty、输出上限和 process group cancellation。
- stdout/stderr、exit code 和截断信息进入 ToolResult，并在日志与 TUI 中脱敏。
- 不从任意 Shell 字符串推断完整文件读写集合，也不承诺执行前或执行后生成精确文件 Diff。

### 16.2 Session Command Rule

`Yes, and don't ask again for <command> commands in <cwd>` 对命令产生最小精确规则：

```go
type SessionCommandRule struct {
    CWD     string
    Command string
}
```

匹配键为：

```text
canonical cwd
+ minimally normalized exact command
```

命令规范化只执行 CRLF → LF、`TrimSpace` 和 NUL 拒绝；内部空格、引号、换行、变量与重定向保持原样。不做 Shell AST、参数重排、变量展开或 Quote 归一化，也不把 Shell、TTY 或平台模式加入 Key。

### 16.3 Host Execution Boundary

命令统一通过应用层 Permission 与 Approval 后在宿主系统执行：

```text
execute_command
→ Application Permission
→ Host Execute
```

安全边界由默认 Ask、Session Rule、灾难性命令硬拒绝、进程取消、超时、输出限制和显式用户 Approval 共同提供。Amadeus 不宣称提供 OS Sandbox 隔离。

### 16.4 `write_stdin` 与 ProcessManager

`write_stdin` 不是新的 Shell 执行，也不是独立的权限请求；它是对已有 `execute_command` 进程的输入、轮询或关闭操作：

```text
write_stdin(process_id, input, poll/close options)
→ ProcessManager.Lookup
→ Reuse OriginCallID and approval state
→ Serialize per process_id
→ Write / Poll / Close
→ Typed ProcessResult
```

- `PreparedToolUse` 必须包含 `process_id`、`OriginCallID`、操作类型和输出预算，不重新解析或批准原始命令。
- 找不到进程、进程已退出、输入已关闭或操作非法时返回稳定的 typed error，不创建新的 Approval Request。
- 不重复执行原 `execute_command` 的 PreToolUse/Permission 流程；Post Tool lifecycle 仍归属于原始 CommandExecutionItem，`write_stdin` 只产生续接事实。
- ProcessManager 对同一 `process_id` 的写入、轮询和关闭串行化；不同进程可以并行，因此 `write_stdin.SupportsParallelToolCalls()` 可以为 `true`。
- 结果包含 process/session ID、增量输出、exit code、running/completed 状态和 truncation；不得只返回无归属的字符串输出。

## 17. Tool 并发

- 并发能力由 `SupportsParallelToolCalls` 声明，不使用额外的资源级锁模型或旧的 `IsConcurrencySafe(input)` 抽象。
- `read/glob/grep/view_image/web_search/web_fetch` 可以按 9.2 定义的 task group 有界并行。
- `write_stdin` 在 Registry 层可并行，ProcessManager 按 `process_id` 保证同一进程串行；不同进程的续接操作可以并行。
- `edit/write/execute_command/update_plan/MCP Call` 串行。
- 并行结果必须恢复为原 Tool Call 顺序后再写入 Rollout 和回灌模型。
- 不建立资源级读写锁、路径依赖图或共享/独占 Permission Gate。

## 18. Slash Command

Slash Command 完全采用 Codex 风格的轻量模型，不建立独立的 `SlashCommandCatalog`、通用命令注册器或每个 TUI 模式各自的命令路由。

### 18.1 命令数据模型

`SlashCommand` 是命令的唯一身份，命令的展示顺序、名称、描述、参数能力和运行期间可用性通过类型方法提供：

```go
type SlashCommand string

func (command SlashCommand) Name() string
func (command SlashCommand) Description() string
func (command SlashCommand) SupportsInlineArgs() bool
func (command SlashCommand) AvailableDuringTask() bool

func BuiltinSlashCommands() []SlashCommand
```

`BuiltinSlashCommands` 只提供 Codex 风格的固定展示顺序，不是包含业务 Handler 的 Registry。初始命令为：`/resume`、`/skills`、`/rename`、`/delete`、`/compact`、`/plan`、`/copy`、`/status`、`/mcp`、`/clear` 和 `/exit`。

### 18.2 Composer 与输入结果

输入框负责补全、过滤、Popup 选择和解析，不执行命令业务。解析结果必须在输入层区分普通文本与命令：

```go
type SlashInvocation struct {
    Command SlashCommand
    Args    string
}

type InputResult struct {
    Text    string
    Command *SlashInvocation
}
```

```text
普通文本
→ InputResult.Text

/plan
→ InputResult.Command(Command: plan)

/plan <task>
→ InputResult.Command(Command: plan, Args: <task>)
```

Popup 只负责过滤和选择 `BuiltinSlashCommands`；Enter 后返回类型化 `InputResult`，Esc 只关闭 Popup。命令历史记录只有在分发成功后才提交，失败的输入不污染本地输入回忆。

### 18.3 单一分发中心

Fullscreen TUI 的活动模型承担类似 Codex `ChatWidget` 的统一分发职责：

```text
Composer
→ InputResult
→ fullscreenModel.dispatchCommand
→ TUI Local Action / Application Command / Session Op
→ Runtime
→ SessionEvent 或 Application Result
→ HistoryCell / TUI Projection
```

`cmd/amadeus` 只负责 CLI 参数、依赖装配和启动 TUI，不包含 Slash Command `switch`。不保留 Plain Controller、`CommandHandler`、`TaskHandler` 或第二套 Slash Command 执行路径；`--plain` 删除，不作为另一套交互运行时维护。

命令按最终动作分为三类，但分类只服务于分发实现，不引入额外的路由抽象：

| 类型 | 命令 | 执行方式 |
|---|---|---|
| TUI Local | `/copy` | 复制最近 Assistant 回复，不创建 Turn |
| Application Command/Query | `/resume`、`/skills`、`/rename`、`/delete`、`/status`、`/mcp`、`/clear`、`/exit` | 由当前 TUI 分发到 Application/Thread 服务 |
| Session/Turn Operation | `/compact`、`/plan` | 提交 `CompactOp` 或 `ThreadSettingsOp`；设置成功后 `/plan <task>` 再提交用户输入 |

`/plan` 不直接修改 TUI 的本地模式变量。Fullscreen TUI 通过一个明确的 `SetPermissionMode` 回调向当前 `AmadeusThread` 提交 `ThreadSettingsOp`；Session 接受设置后，TUI 才更新模式投影。带参数的 `/plan <task>` 严格遵循：

```text
SetPermissionMode(plan)
→ ThreadSettingsOp
→ Session 更新 PermissionMode
→ TUI 收到设置成功结果
→ UserInputOp(task)
```

快捷模式切换复用同一回调；设置失败时保留原模式，不启动任务。

Slash Command 不是 SessionEvent。命令执行引发的状态变化才通过 SessionEvent、Rollout 和 TUI Projection 传播；纯 TUI 操作不写入 canonical history。

## 19. TUI

### 19.1 产品形态

默认使用 Bubble Tea + Lip Gloss 实现 Codex 风格 Rich Inline TUI：

- 保留 Amadeus Logo 和 `>_` 启动视觉。
- 历史内容尽量进入终端原生 scrollback。
- 鼠标默认保留终端选择文本能力。
- 输入运行期间仍可编辑；Enter 是否提交由 Runtime 状态决定。
- Amadeus 只维护这一套 Rich Inline 交互运行时；不提供 `--plain` 第二套输入、状态和事件路径。

### 19.2 HistoryCell

TUI 使用结构化 HistoryCell，而不是拼接 ANSI 字符串。Runtime 业务项先经 `EventReducer` 投影为 TranscriptState，再由 Cell 渲染：

```text
SessionEvent
→ EventReducer
→ TranscriptState
→ ActiveHistoryCell / HistoryCell
→ Renderer
```

首批 Cell：

- UserMessageCell
- AssistantMessageCell
- ReasoningCell
- ExecCell
- ExploreCell
- WebSearchCell
- PlanCell
- ApprovalCell
- DiffCell
- ErrorCell
- WorkedSeparatorCell

`ActiveHistoryCell` 按 `ItemID` 保存进行中状态；Delta 只能更新对应 Item，不能依赖“当前最后一个 Cell”猜测归属。`ItemCompleted` 完成对应 Active Cell 并只提交一次正式 HistoryCell；如果 Resume 只有 Completed Item，则 Reducer 直接合成正式 Cell。

实时与恢复使用同一 `TurnItem → HistoryCell` 映射：

```text
Live:   ItemStarted → Delta → ItemCompleted → HistoryCell
Replay: Completed TurnItem                  → HistoryCell
```

Replay Mode 不播放 Working、Shimmer 或流式动画，但必须产生与实时完成态一致的历史结构。

### 19.3 Visual Runtime

与 Codex 对齐的关键行为：

- `◦ Working (1m 32s • esc to interrupt)` 使用单调时钟。
- Working shimmer 使用终端主题感知的 foreground/dim，而不是固定彩虹色。
- 完成后显示 `─ Worked for 7m 18s ─────`。
- User、Working、Assistant、Separator 和 Composer 的空行由结构化布局决定。
- Tool Start/Delta/Complete 原位更新，不重复打印多个树枝。
- Ran/Explored/Search 等标签使用 TerminalPalette 的强调色。
- Markdown 代码、路径和命令采用终端主题感知高亮。
- 终端不支持颜色或 `NO_COLOR` 时正确降级。

Working 是 Turn 生命周期的派生 UI 状态：

- `TurnStarted` 进入 Working。
- `TurnCompleted` 或 `TurnAborted` 离开 Working。
- LLM Call、Reasoning、Tool 或任意通用 Status Event 不单独决定 Turn 是否运行。
- Bubble Tea 后台命令返回只释放 goroutine，不再作为第二套 Turn 终态真相。

### 19.4 Approval 与 Diff

Approval Dialog 是 Rich Inline TUI 的专用交互状态，不复用只显示文字的通用 selection overlay：

```text
Approval Dialog
├── Title / Question / Path or Command
├── Details
├── scrollable structured Diff viewport（文件 Tool）
├── selectable Options
└── feedback / keyboard hints
```

- 上方展示操作摘要、目标和 Claude Code 风格 Question。
- 文件修改展示结构化 Diff、增删统计和必要的截断提示；Diff viewport 有界且可滚动。
- 下方展示由 ApprovalPresentation 提供的完整选项。
- 上下方向键移动选择，Enter 确认，Esc 不隐式 Allow，Tab 添加反馈。
- 选项文本、范围和 Grant 类型完全来自 ApprovalPresentation，不由 TUI 改写或泛化。
- TUI 只返回 `ApprovalDecisionOp`，不直接更新 SessionPermissionContext。
- 等待 Approval 时 ActiveTurn 与 RunningTask 保持活动，Working 状态不能错误消失。

Approval 与模型主动询问用户通过 `InteractiveRequest` 进入 TUI，不作为普通 `SessionEvent`。TUI 将选择结果返回 Session；Session 由 ApprovalCoordinator 应用内存 grant，Tool 的 Completed Item 记录最终 completed/declined/failed/stale 状态。

## 20. Session、Resume 与中断

### 20.1 Thread 创建与持久化物化

- 仅启动 `amadeus` 时可以先生成 ThreadID 并创建未持久化的 AmadeusThread。
- 第一次提交真实用户输入时由 ThreadManager 通过 LiveThread 物化 SessionMeta RolloutLine。
- LocalThreadStore 创建 JSONL Rollout；SQLite StoredThread 由已持久化 SessionMeta 和后续 RolloutItem 派生。
- internal Session 只持有 ThreadID 和 LiveThread，不持有 SQLite Row。

### 20.2 Resume

- `/resume` 在 TUI 内选择当前项目的 Session。
- `amadeus --resume <id>` 从终端直接恢复。
- ThreadManager 先读取 StoredThread 定位 Rollout，再通过 ThreadStore.LoadHistory 构造 `InitialHistory::Resumed`。
- Session spawn 使用 InitialHistory 恢复 canonical Rollout、Replacement History 和最近的 Session Plan 投影。
- SessionPermissionContext 在 Resume 时重置为 `default`、空 Read/Edit Directories 和空 Command/External Grants，并生成新的临时 Permission Context Update。
- 不恢复旧 goroutine、文件句柄或进行中的进程。

### 20.3 中断后继续

用户取消 Turn 时：

1. TUI 通过 AmadeusThread 提交 `InterruptOp`。
2. internal Session 找到 ActiveTurn 并取消 RunningTask context。
3. SessionTask.Abort 尽力终止活动 Tool。
4. 为未完成 ToolCall 写入 cancelled ToolResult。
5. 追加未完成 ToolCall 的 cancelled ToolResult 和 `TurnAborted` RolloutItem，并 flush。
6. 清除 ActiveTurn。
7. 发布 `TurnAborted`，TUI 回到可输入状态。

下一次用户输入始终创建新 Turn。SessionState.Context 注入最近中断事实；模型根据新输入决定重新规划或开始新任务。

## 21. Persistence

Amadeus 使用 JSONL canonical rollout + SQLite metadata index：

```text
$AMADEUS_HOME/
├── sessions/YYYY/MM/DD/rollout-<timestamp>-<thread-id>.jsonl
└── data/amadeus.db
```

### 21.1 JSONL Canonical Rollout

- 每个 Thread 一个 Rollout 文件。
- 每行是独立的 RolloutLine JSON 对象。
- sequence 从 1 开始严格递增，不重复、不倒退。
- ThreadID/TurnID 使用路径安全的稳定标识；`turn_context` payload 的身份必须与 RolloutLine envelope 一致。
- RolloutRecorder 是唯一文件 Writer；Session、Tool 和 TUI 不直接打开文件追加。
- 恢复时逐行解析；只有最后一条不完整记录可以被安全忽略或截断，完整但缺少末尾换行的最后一条记录会被补齐后再追加，其他解析错误必须显式报告。
- append batch 必须先完整编码再写入；部分写失败时回滚到 batch 起始 offset，flush 使用文件同步保证 durable 顺序。
- ToolCall/ToolResult 使用稳定 CallID 配对。
- Compaction、Plan Update、Approval 结果、TurnContext 和 Turn 终态均进入 Rollout；未决 Approval Request 与 ApprovalDecision 不作为独立 canonical history。
- Runtime 临时状态、goroutine、进程句柄和 Pending Future 不持久化。
- `session_meta` 保存重建索引所需的 CWD、标题、模型、Git metadata、归档初态和创建时间；后续标题/归档变化使用 `context_update`。

### 21.2 SQLite State DB

SQLite 位于 `$AMADEUS_HOME/data/amadeus.db`，核心表保持最小：

#### `schema_migrations`

- version
- applied_at

#### `threads`

- id
- rollout_path
- cwd
- title
- preview
- model_provider
- model
- tokens_used
- created_at
- updated_at
- archived
- git_sha
- git_branch
- git_origin_url

不建立 `projects`、`turns`、`messages`、`summaries` 或 SQLite `rollout_items` 表。Turn、消息、Tool 和 Compaction 历史只存在于 canonical Rollout。

### 21.3 Metadata Sync 与 Backfill

```text
LiveThread.AppendItems
→ LocalThreadStore durable write + flush JSONL
→ 从活动 Recorder 的 canonical path 重放 metadata projection
→ StateDB.UpsertThread
```

- SQLite 可以暂时落后 JSONL，但不能包含尚未 durable 的 Rollout 事实。
- Metadata 更新失败必须记录警告并保留可重建状态，不能回滚已经 durable 的 canonical history。
- 启动时执行 reconciliation/backfill，从 SessionMeta、ContextUpdate、UserMessage 和 TokenUsage 重建 StoredThread；活动 Thread 即使索引被清空，下一次 canonical append 也能直接重新 upsert。
- `/resume`、列表和搜索优先查询 SQLite；索引缺失或漂移时可扫描 Rollout 修复。
- `tokens_used` 是各 canonical `token_usage` 记录的 Thread 累计值，不是仅保存最后一个 Turn 的 usage。
- Session Permission State（Mode、Additional Working Directories 和 Session Rules）只存在于活动 internal Session，不写入 Thread metadata，也不从历史 Approval Decision 恢复。

旧 SQLite canonical history 迁移必须可重复执行：若进程已写完 JSONL、但尚未 upsert Thread index 或删除旧表，下一次启动复用并校验现有 Rollout 后继续迁移；只有所有 Session 导出和索引写入成功后才删除旧 `projects/sessions/runs/rollout_items` 表。

只有当真实产品需求证明需要分页历史或全文搜索时，才增加可重建的 SQLite History Projection。

## 22. MCP 与 Skill

### 22.1 MCP

- MCP Tool 进入统一 Tool Registry。
- MCP 不建立第二套 Agent Loop、历史或 Approval UI。
- MCP Server 生命周期属于 internal Session 的 SessionServices。
- Tool Catalog 使用 lazy discovery 和 snapshot，单次模型请求看到稳定集合。
- MCP List 与 Resource Read 默认 Allow；MCP Call 默认 Ask，无法确认安全性时保持 Ask。
- MCP Server 不可信输出按 ToolResult 处理，不能注入系统级指令。

### 22.2 Skill

- 用户级 Skill 位于 `$AMADEUS_HOME/skills`。
- 项目级 Skill 位于项目约定目录。
- Skill 的 `SKILL.md` 提供显式工作流和说明。
- Skill 可以引用脚本，但脚本统一通过 `execute_command` 执行，复用命令 Permission、Approval 和 Host Runner。
- Skill 不拥有独立进程执行器。
- Skill 只在被选择或明确触发时注入完整内容，避免污染 Context。

## 23. Web 与网络

- `web_search` 是 Amadeus 必须保留的内置 Tool；`web_fetch` 是独立 Tool，二者不能因为 Shell 或 MCP 能力存在而删除。
- 模型始终只看到稳定的单一 `web_search` Tool，不暴露 `brave_search`、`tavily_search` 等 Provider 专用 Tool 名。
- 用户通过配置选择 Search Provider；首批正式支持 `duckduckgo`、`tavily`、`searxng` 和 `brave`。
- `duckduckgo` 默认不要求 API Key；`tavily` 与 `brave` 要求 API Key；`searxng` 要求用户提供实例 Base URL。
- Search Provider 在 Application Bootstrap 时根据配置构造并注入 WebSearch Service；Agent Runtime、Tool Contract、TurnItem 和 TUI 不感知具体搜索引擎。
- Provider 必须实现统一的 `Search(context.Context, query, limit)` 边界，并返回标准化 `title/url/snippet` Result。
- Provider 超时、认证、限流、网络和协议错误必须转换为可见 ToolResult，并保留稳定 Provider/Error Kind 供诊断。
- 单次 Search 调用使用 Turn 派生 Context 和配置超时，保持同步 API；多个独立 `web_search` 调用只由 Tool Executor 做有界并发。
- 搜索结果进入 Context 前进行 URL 校验、去重、数量限制、文本长度限制和来源标注。
- `web.search.enabled=false` 时不注册 `web_search`，模型不可见；启用后 `web_search` 默认 Allow。
- `web_fetch` 对未授权 Hostname 返回 Ask；用户选择 `Yes, and don't ask again for <hostname>` 后写入 Session Domain Rule。
- 不构建通用 Network Permission Store；Provider 配置缺失或请求失败时返回可见 ToolResult。
- Browser 自动化不进入内置核心能力，只通过 MCP 或 Extension Tool 接入。

目标配置形态：

```yaml
web:
  search:
    enabled: true
    provider: brave # duckduckgo | tavily | searxng | brave
    api_key: "${BRAVE_API_KEY}"
    base_url: ""
    timeout: 15s
    max_results: 5
```

未来可以在不改变 `web_search` Tool Schema 的前提下增加其他 Search Provider；Provider 扩展属于 Infrastructure，不进入 Agent Engine 分支逻辑。

## 24. 配置

### 24.1 配置位置

Amadeus 配置固定属于 `$AMADEUS_HOME`：

```text
$AMADEUS_HOME/config.yaml
```

如果未设置 `AMADEUS_HOME`，由 Bootstrap 使用二进制所在目录作为默认 Home。不得回退到任意当前工作目录寻找配置。

### 24.2 配置优先级

```text
CLI Flags
> Environment Variables
> $AMADEUS_HOME/config.yaml
> Built-in Defaults
```

API Key 默认通过环境变量或配置文件提供，不要求暴露 CLI Flag，避免进入 Shell History。

### 24.3 核心配置域

- provider
- agent runtime limits
- context window/compaction
- tool process defaults
- MCP
- Skill
- Web Search
- logging

Approval 不暴露配置规则 DSL。文件 Tool 的 Session Allow 更新内存中的 Permission Mode 与 Additional Working Directories；命令和 MCP 等 Tool 可以增加各自的 Session Rule；Plan Mode 自动禁止实施副作用。

敏感字段在 `config show`、日志和错误中脱敏。

## 25. Event Protocol

### 25.1 协议分层

Amadeus 不使用单一 Event Bus 混合命令、交互请求、持久化事实、Telemetry 和 TUI 消息。边界固定为：

```text
Submission / Op       Interface/Application → internal Session
SessionEvent          internal Session → Interface/Application
InteractiveRequest    internal Session → Interface/Application，必须回答
RolloutItem           internal Session → LiveThread，canonical persistence
TUI Message           Bubble Tea 内部按键、动画、Popup 与异步结果
Trace / Telemetry     Runtime 内部诊断，不进入产品 Event Protocol
```

普通 Event 是单向通知，不承担请求—响应职责。Approval、模型主动询问用户和未来 MCP Elicitation 使用 `InteractiveRequest`；回答作为新的 `Submission/Op` 返回 Session。

### 25.2 SessionEvent 作用域

Amadeus 不定义通用 Envelope，也不通过 `context.Context` 注入动态 Event Metadata。`SessionEvent` 直接携带固定的 Thread/Turn 作用域和一个明确的消息类型：

```go
type SessionEvent struct {
    ThreadID ThreadID
    TurnID   TurnID
    Message  EventMessage
}
```

- Thread 级事件可以没有 TurnID。
- Item 生命周期和 Delta 在 payload 中携带稳定 `ItemID`。
- SessionIo 的单一 Event Channel 保证发送顺序；第一版不增加 Event Priority、Topic DSL 或独立 Sequence。
- JSONL RolloutLine 自己维护持久化 ordinal，不能用 TUI Event 顺序替代 Rollout 顺序。

### 25.3 最小 EventMessage

首批公共 Event：

```text
ThreadConfigured

TurnStarted
TurnCompleted
TurnAborted

ItemStarted
ItemCompleted

AssistantMessageDelta
ReasoningDelta
CommandOutputDelta

PlanUpdated
ThreadTokenUsageUpdated
ContextCompacted

Warning
StreamError
```

明确不进入公共 Event Protocol（内部仍可作为 Trace/Telemetry）：

- LLMCallStarted/Completed。
- Iteration 生命周期事件。
- 通用 StatusChanged 生命周期事件。
- ContextBuildStarted/Completed。
- TUI Working/Shimmer Tick。
- Provider Trace、HTTP Attempt、Retry Backoff 等遥测细节。

这些信息可以保留在日志、Trace 或测试探针中，但 TUI 不应依赖它们判断 Turn 生命周期。

### 25.4 TurnItem

`TurnItem` 是 Event、Rollout Replay 和 TUI History 的稳定业务项。首批类型：

```text
UserMessageItem
AssistantMessageItem
ReasoningItem
ToolCallItem
CommandExecutionItem
FileChangeItem
PlanItem
ContextCompactionItem
```

职责：

- `ToolCallItem`：read、glob、grep、MCP、Skill、Web、view_image 等通用 Tool。
- `CommandExecutionItem`：命令、进程、stdout/stderr、退出码和 duration。
- `FileChangeItem`：edit/write、Structured Diff、Approval 结果和最终修改状态。
- `PlanItem`：模型在显式 Plan Mode 中输出的正式计划内容。
- `PlanUpdated`：`update_plan` 修改的 TurnState 软计划快照，不驱动 DAG。

Item 状态至少包括：

```text
in_progress
completed
failed
declined
stale
```

### 25.5 Item 生命周期与 Delta

统一生命周期：

```text
ItemStarted{Item: in_progress}
→ zero or more Delta{ItemID}
→ ItemCompleted{Item: completed|failed|declined}
```

要求：

- Delta 必须携带 ItemID，只更新对应 Active Item。
- Completed Item 必须包含恢复和最终展示所需的完整事实，不能要求 Replay 重新拼接历史 Delta。
- TUI 收到没有 Started 的 Completed Item 时必须能够直接渲染，支持 Resume、旧历史迁移和晚订阅。
- 同一 ItemID 的 Completed 只能提交一个正式 HistoryCell；重复或迟到 Delta 必须忽略并记录诊断。

### 25.6 InteractiveRequest

首批交互请求：

```text
ApprovalRequest
UserInputRequest
```

每个 Request 带稳定 RequestID、ThreadID、TurnID 和完整 typed Presentation。Approval Request 至少包含 Tool 名、操作类型、Title、Subtitle、Question、目标路径/命令/CWD、Details、`*FileChangePreview` 和 typed Options；Diff 不得在协议桥接时转换为 `string` 或 `any` 的扁平文本。TUI 只渲染 Presentation，不重新解释权限语义；回答通过 `ApprovalDecisionOp` 或 `UserInputResponseOp` 返回。

```go
type InteractiveApprovalRequest struct {
    RequestID    RequestID
    Approval     ApprovalRequest
    Presentation ApprovalPresentation
}
```

未决 Request 不作为 canonical history。最终 completed/declined/failed 结果进入对应 Tool/File/Command Completed Item；安全审计可以单独记录，但不与 TUI Event 混为一体。

### 25.7 Event、Rollout 与 Delivery

持久化策略：

- 持久化 TurnStarted、TurnCompleted/TurnAborted、Completed TurnItem、`plan_update`、Token Usage、Compaction 和恢复所需 Context Facts。
- 不持久化 ItemStarted、Delta、Working、未决 InteractiveRequest、Popup 和动画 Tick。
- Resume 从 canonical Completed Item 重建 HistoryCell，不重放旧 Delta。
- 高频 Completed Item 先由 Session 串行追加到 JSONL，并更新内存/SQLite 索引；Session 在 TurnStarted 和 TurnCompleted/TurnAborted 前执行强制 flush，确保终态 Event 只在 canonical facts durable 后发布。这样不会让每个工具事件都独占一次 `fsync`，但不改变持久化顺序和终态不变量。

Turn 终态顺序固定为：

```text
RunningTask 返回
→ append canonical terminal facts
→ flush Rollout
→ 清除 ActiveTurn
→ 发布 TurnCompleted / TurnAborted
```

Runtime 正确性不能依赖 TUI 消费速度：

- Rollout append/flush 失败可以使 Turn 失败。
- TUI Channel 关闭或 Renderer 退出不能反向把已完成 Tool/模型调用改判为失败。
- 高频 Delta 可以合并或在重绘层丢弃；ItemCompleted、TurnCompleted 和 TurnAborted 不得静默丢失。
- 不保留会因调试 Subscriber backpressure 而让 Agent 执行失败的通用 Event Hub 主链。

## 26. 错误模型

错误至少分为：

- config_error
- provider_error
- context_error
- compaction_error
- tool_validation_error
- permission_denied
- approval_denied
- stale_file
- process_error
- persistence_error
- interrupted

所有错误必须：

- 带稳定错误码。
- 保留底层错误链。
- 不泄露 API Key。
- 在 TUI 显示可行动说明。
- 在 Turn 终态中可恢复或可诊断。

禁止出现“Working 动画停止但没有 Error/Completed Event”的静默失败。

## 27. 测试策略

### 27.1 Runtime Contract

- 一个用户输入只创建一个 Turn。
- Turn 在模型调用前持久化。
- ItemStarted/ItemCompleted 使用相同 ItemID；Completed Item 足以独立 Replay。
- Completed/Aborted 终态唯一，失败信息进入唯一终态。
- Rollout append/flush 和 ActiveTurn 清理先于终态 Event。
- Resume 后 Rollout 顺序稳定。

### 27.2 Event Protocol

- SessionEvent 统一携带 ThreadID/TurnID，具体 payload 不重复公共 Metadata。
- Delta 只能更新相同 ItemID 的 Active Item；迟到 Delta 不改变 Completed Item。
- 没有 ItemStarted 的 Completed Item 仍可直接渲染。
- InteractiveRequest 必须通过对应 Response Op 完成，不与普通 Event 混用。
- 慢 Renderer、已关闭 TUI 或调试 Subscriber 不导致 Agent Turn 失败。
- Live 和 Replay 对相同 Completed TurnItem 生成一致 HistoryCell。

### 27.3 Context

- 超大 `docs/design.md` 不导致静默停止。
- 读取整个 `docs` 目录后仍可继续对话。
- Tool Result 被安全投影。
- Compaction 保留目标、修改、失败和待办。
- Compaction 后 Tool 协议合法。
- Provider usage 可以校准 estimator。

### 27.4 Tool 与 Approval

- `read/glob/grep` 的输出上限、稳定排序、截断标记和截断原因可预测。
- 未完整 Read 的已有文件 Edit/Write 被拒绝或要求读取。
- 文件外部变化触发 stale_file。
- Diff Preview 在落盘前产生。
- Deny 后文件不变化。
- Allow 后修改与 Preview 一致。
- Approval 等待期间发生外部变化时拒绝落盘。
- `edit` 零匹配或多匹配时零写入。
- `write` 覆盖已有文件前展示完整覆盖 Diff。
- Session Command Rule 只匹配相同 canonical CWD 和最小规范化后的完整命令。
- `execute_command` 不伪造结构化文件 Diff 或修改归因。
- `write_stdin` 复用原命令 Approval 和 OriginCallID，不重复 Permission/PreToolUse；同一进程串行、不同进程可并行。
- `update_plan` 最多一个 `in_progress`，通过 `PlanUpdated` 展示完整状态，ToolResult 只返回简短确认。
- Approval Presentation 快照覆盖工作目录内/外 Edit、Create、Overwrite、External Read、Command、Skill、Web Fetch 和 MCP。
- 默认 Tool Catalog、Prompt Snapshot、Registry、Event 和 Rollout 中均不存在 `apply_patch` 或 sandbox Tool。

### 27.5 TUI

- Slash Popup 键盘交互。
- Approval 上下键与 Enter。
- 中文输入和 Backspace。
- Working/Worked 计时和间距。
- ActiveHistoryCell 只提交一次。
- Working 只由 TurnStarted/TurnCompleted/TurnAborted 控制。
- Bubble Tea Task 返回不作为第二套 Turn 终态。
- Terminal 无颜色和窄宽度降级。

### 27.6 Provider

- Responses 与 Chat Completions Tool Call。
- 流式增量聚合。
- Developer role 降级。
- DeepSeek、GLM、Qwen 方言 fixture。
- timeout、retry、取消和错误脱敏。

## 28. 架构验收场景

Amadeus 至少通过以下真实场景：

1. 用户输入“你好”，不创建无关计划，不调用 Tool，快速返回。
2. 用户要求查看 `docs`，Agent 能读取大文件、控制 Context，并持续工作。
3. 用户要求修改文件，TUI 在写入前展示 Diff，选择 `No` 时零修改，选择 `Yes` 时精确写入。
4. 文件在 Approval 等待期间被外部修改，Agent 拒绝覆盖并重新读取。
5. 用户运行 `/plan`，Agent 只分析和规划，不执行副作用。
6. 普通复杂任务中模型按需调用 `update_plan`，TUI 显示 Updated Plan。
7. 用户运行 `/compact`，历史被压缩但原始 Rollout 保留，后续对话不丢目标。
8. 用户中断 Turn 后输入“继续”，新 Turn 能看到中断事实并重新规划。
9. `execute_command` 完成后展示命令、输出和退出码，不伪造文件 Diff 或修改归因。
10. Provider、Context 或 Tool 失败时 TUI 明确显示错误，不静默卡死。
11. `/resume` 恢复 Session 后 Plan、Compaction 和 Tool 历史语义一致。
12. Rich Inline TUI 在正常终端中完成完整 Runtime 流程。

## 29. 最终架构结论

1. Codex 是 Amadeus 的 Thread、Session、Turn、Runtime、Context、Plan-guided ReAct、Slash Command 和 TUI 架构骨架。
2. Claude Code 是文件 Tool、修改确认、Diff Preview 和 Permission UX 的主要行为参考。
3. 默认 Agent 是单一 Plan-guided ReAct；`update_plan` 按需使用，计划不驱动 DAG。
4. `/plan` 是显式只规划不实施的 Plan Mode，复用同一 Reactor。
5. `/compact` 提交 CompactOp，由 CompactTask 使用 SessionState.Context 完成压缩并保留 canonical rollout。
6. 结构化文件修改遵循 Read → Diff Preview → Approval → Revalidate → Atomic Apply → Verify。
7. `execute_command` 默认 Ask，经 Session 精确规则复用授权后直接在宿主执行；不解析任意命令的完整路径副作用。
8. JSONL RolloutItem 是完整历史的唯一事实；SQLite StoredThread 只保存可重建 metadata/index。
9. ThreadManager 是 Thread 创建和恢复入口；LiveThread → ThreadStore → LocalThreadStore 是唯一持久化链。
10. AmadeusThread 是 Interface 唯一 Runtime 句柄；TUI 只提交 Op、消费 SessionEvent 并回答 InteractiveRequest。
11. TurnItem 是 Event、Rollout Replay 和 HistoryCell 的稳定业务项；Delta 只服务实时更新，Completed Item 才是恢复事实。
12. Slash Command 分为 TUI Local、Application Action 与 Core Op，不直接拥有 Runtime 或持久化状态。
13. internal Session 是 SessionTask、ActiveTurn、Context History、Event Delivery 和终态收尾的唯一所有者。
