# Amadeus 架构设计

> 状态：Target Architecture v2
> 最近修订：2026-08-21
> 目标语言：Go
> 产品形态：面向真实软件工程任务的本地 Coding Agent CLI
> 架构骨架：`../codex-main`
> Tool 与 Approval 行为参考：`../claude-code-main`

## 1. 文档定位

本文只描述 Amadeus 的最终目标架构、稳定 Contract 和验收标准。实施顺序、任务状态和旧代码清理由 `docs/development-progress.md` 维护。

设计遵循以下约束：

1. Codex 作为 Thread、Session、SessionServices、Turn、Rollout、Context、SessionTask、`run_turn`、Prompt 构造、Slash Command 和 TUI 的主要架构骨架。
2. Codex 的 StepContext/ToolRouter 与 Claude Code 的 Tool 内层协议、ToolUseContext、文件工具、文件修改确认、Diff Preview、Read-before-write、权限评估和 Approval 交互共同构成 Tool 调用链。
3. Amadeus 保留多 Provider、本地配置、Go 实现和跨模型安全默认值等自身产品要求。
4. 外层 Runtime 不照搬 Claude Code；内层 Tool 的数据模型和阶段划分尽量与 Claude Code 对齐，再通过 Go 类型和 Codex `Submission/Event/EventMsg` 边界接入 Session。
5. 不引入 Claude Code 的用户级、项目级或本地权限持久化；Approval grant 只保存在当前 Session 内存中，Session 结束或 Resume 后清空。
6. Prompt 的架构、数据模型、命名和所有权以 Codex 为准；普通模式、Plan Mode 和 Compaction Prompt 使用 Codex 对应机制与资产。Prompt 迁移不得为旧实现增加兼容适配层，旧 Prompt owner、旧字段和旧装配入口必须在切换后删除。
7. 本文所称“向 Codex/Claude Code 看齐”均指架构级对齐：对齐职责划分、数据模型、概念术语、命名、所有权、依赖方向、状态生命周期、事件顺序和失败顺序，而不是保留 Amadeus 旧实现后仅增加同名类型、回调、Adapter、Facade 或展示层包装。

架构对齐必须遵守以下替换原则：

- 先确认参考实现中业务事实的 owner、输入输出 Contract、状态机边界和完成协议，再设计 Amadeus 的 Go 等价实现；不得从当前旧调用链反推一个“最小改名方案”。
- 当前类型或调用链无法表达目标 Contract 时，必须替换数据模型和职责边界，迁移全部生产调用方，并删除旧 callback、旧 message、旧状态字段和旧完成路径。
- 不允许新主链调用旧 executor/query/controller 后再把字符串结果包装成 Codex 风格 Event；不允许 TUI、Application 和 Session 同时维护同一操作的 running、completed 或 history 真相。
- UI 文案、视觉样式或 symbol 名称相似不构成对齐完成；只有所有权、生命周期、事件时序、恢复语义和失败行为一致，并有 Contract test 固化后，才视为完成。
- Amadeus 可以因 Go、Bubble Tea、多 Provider 和本地运行边界做语言与平台适配，但适配不得改变参考架构中的核心概念关系，也不得成为保留旧实现的理由。

Amadeus 当前处于未发布开发阶段，不承诺自身旧实现的任何兼容性：

- 不兼容旧数据、旧配置、旧 Protocol、旧 Event、旧 Rollout、旧 SQLite schema、旧 package API 或旧测试 fixture。
- 架构或 schema 替换时直接删除旧 reader、writer、decoder、migration、alias、wrapper、Adapter、Facade 和兼容测试，不保留 `migration-only` 生产代码。
- 旧本地开发数据和 fixture 随当前 schema 变化直接删除并重建；程序不自动识别、升级或导出旧格式。
- 当前格式的崩溃恢复、尾行截断和可重建 SQLite index 属于同一版本内的可靠性能力，不属于旧格式兼容。
- Provider wire compatibility、OpenAI-compatible API 等是产品能力，不代表 Amadeus 自身历史实现需要兼容。

架构迁移以所有权、依赖方向和运行时不变量为完成标准，不以 package、类型或字段改名为完成标准：

- `cmd/amadeus` 只作为 Composition Root 和 Interface 启动入口，不拥有 Turn executor、Context history、Session capability 或 Turn 终态协调逻辑。
- 生产 `SessionTask` 必须是可独立运行的 Agent Runtime 任务，不得回调 CLI/Application controller，不得通过 invocation/request/result channel 从旧执行器临时取得依赖。
- 同一业务事实只能有一个 owner 和一套完成协议；新增 Session、Task、Event 或 Context 外壳后，旧 side channel、旧历史源和旧执行入口必须删除，而不是继续包裹。
- 架构验收必须验证依赖方向和失败顺序，不能只扫描旧 symbol、旧 package 名称或目录结构。

`../claude-code-main` 是源码还原项目，因此用于理解产品行为和局部机制，不作为未经验证的实现权威。涉及关键安全行为时，必须用 Amadeus 自身测试固化契约。

核心设计域：

| 设计域 | 主要章节 |
|---|---|
| A. Runtime + Persistence | 分层架构、核心语义、Canonical Runtime、Session/Resume、Persistence |
| B. Context + Prompt | Prompt/Context、Token、Compaction、Provider Adapter |
| C. Tool + Approval | Tool 架构、文件修改、Permission、Command、并发 |
| D. Event + TUI | Submission、Event/EventMsg、TurnItem、HistoryCell、Rich Inline Projection |
| E. Slash Command | Codex 风格 SlashCommand、InputResult、单一 TUI 分发与 Application/Session 操作 |
| F. Agent Engine | Codex 风格 Turn continuation loop、StepContext、Plan Tool、Plan Mode、中断与终态 |
| G. Runtime Architecture Convergence | SessionServices 所有权归位、Codex 术语收敛、Task/run_turn 主链与混合 Tool 边界 |
| H. Extensions + Release | MCP、Skill、Web、迁移清理与发布验收 |
| I. Prompt Construction + Optimization | Codex Prompt 数据模型、ModelMessages、WorldState/Collaboration Mode、Prompt 资产迁移与缓存/Token/Contract 验证 |
| J. Slash Command + TUI Application Lifecycle Alignment | Active Thread Attachment、canonical replay、typed command lifecycle 与专用 HistoryCell |
| K. Response Stream Reconnect Lifecycle Alignment | Provider retry 分层、ModelClientSession 重连、typed transient error 与 TUI 状态恢复 |
| L. Model + Provider Configuration Ownership Alignment | Config v2、Provider transport、ModelInfo、runtime override 与 token policy |
| M. Codex Architecture Realignment | Protocol identity、typed Rollout、SessionState/Services、SessionTask、StepContext 与 legacy cleanup |
| N. Runtime Coordination Tools + Plan Mode Alignment | `update_plan`、`request_user_input`、Collaboration Mode 与 Proposed Plan lifecycle |
| O. Same-Turn User Input + Turn Steer Alignment | UserMessageAdmission、TurnInputQueue、same-Turn continuation、client message identity 与 TUI steer UX |

## 2. 产品目标

Amadeus 的目标是成为一个真正可用于日常软件开发的通用 Coding Agent：

- 用户直接运行 `amadeus` 进入交互会话。
- 用户也可以运行 `amadeus "<task>"` 执行首个任务。
- Agent 能探索项目、编辑文件、执行命令、验证结果并解释修改。
- 默认使用 Codex 风格的单一 Turn continuation loop。
- 简单任务可以直接执行；复杂任务可以通过 `update_plan` 维护可见计划。
- `/plan` 进入与 Codex 对齐的显式 Plan Mode，用于分析和规划，不实施文件或命令副作用。
- Default 与 Plan Mode 都可以通过 `request_user_input` 在当前 Turn 内请求结构化用户输入并继续执行；用户提问是独立交互能力，不属于 Approval。
- 用户可以在 Regular Turn 运行期间继续提交普通消息；Runtime 将其作为 steer input 接纳到同一 Turn，并在当前 Model Step 后继续，而不是静默排队成下一 Turn。
- `/compact` 调用正式的上下文压缩服务，而不是仅清空 TUI 文本。
- 文件修改默认对能力较弱或不稳定的模型保持安全：先生成 Diff，再由用户确认，最后写入。
- Runtime 不依赖 TUI，未来可以被其他界面复用。

## 3. 参考职责矩阵

| 能力域 | 主要参考 | Amadeus 取舍 |
|---|---|---|
| Thread、Session、Turn | Codex | AmadeusThread、internal Session、ActiveTurn 与 SessionTask 使用同构生命周期 |
| Agent Runtime | Codex | Session、SessionState、SessionServices、TurnContext、StepContext、SessionTask 与单一 `run_turn` continuation loop |
| Plan | Codex | `update_plan` 是 transient 软 checklist Event；`/plan` 是 Collaboration Mode；最终方案使用 `<proposed_plan>`、`PlanDeltaEvent` 与 completed Plan TurnItem |
| Context Manager | Codex 为骨架 | 统一历史投影、Token Accounting 和 Compaction 生命周期 |
| Prompt Assembly | Codex | Prompt、BaseInstructions、ResponseItem、ToolSpec、ModelMessages、WorldState、CollaborationModeState 和 ContextualUserFragment 使用 Codex 同构职责；不保留旧 Prompt 构造双轨 |
| 普通/Plan/Compact Prompt | Codex | 普通 Turn 复用 ModelInstructions + WorldState + Conversation + ToolSpec；Plan 使用 CollaborationModeMessages.plan；Compact 使用 Codex summarization prompt 与 summary prefix |
| Tool 路由与内层协议 | Codex + Claude Code | StepContext 捕获本次 ToolRouter/ToolSet；ToolExecutionService 执行 Validate、Prepare、Permission、Approval、Execute 和 Typed ToolResult |
| Claude-style 文件 Tool | Claude Code | `read`、`edit`、`write`、`glob`、`grep` 的模型指引和使用边界以 Claude Code 对应 Tool Prompt 为准 |
| Codex-style Runtime Tool | Codex | `update_plan`、`write_stdin` 和命令续接提示以 Codex 对应 Tool Prompt/Contract 为准 |
| 用户输入 Tool | Codex 为架构、Claude Code 为 UX 参考 | `request_user_input` 使用独立 Request Event/Answer Op/Session waiter；稳定 Question ID、Default/Plan 通用，并选择性吸收多选与 Other 体验，不复用 Approval `updatedInput` |
| Turn Steer | Codex | 普通 `UserInputOp` 通过 Started/Steered admission 接纳；ActiveTurn 持有 TurnInputQueue，`run_turn` 在 Model Step 边界 drain 并继续同一 Turn |
| Command Tool | Codex + Amadeus | `execute_command`、ProcessManager、Approval 复用和宿主执行边界遵循 Amadeus 已有 Contract 与 Codex unified exec 语义 |
| Approval TUI | Claude Code | 展示操作和结构化 Diff，使用范围明确的动态选项与键盘交互；不直接修改权限状态 |
| Event Protocol | Codex | Submission、UserMessageAdmission request/response、Event、EventMsg、TurnItem 生命周期、Approval/User Input request 与 Delta |
| TUI 数据模型与视觉 | Codex | EventReducer、HistoryCell、ActiveHistoryCell、Working、Slash Popup、状态栏 |
| Slash Command | Codex | 命令分为 TUI Local、Application Action 与 Core Op，不拥有业务状态 |
| MCP、Skill | Codex 为主 | 统一进入 SessionServices、ToolRouter、TurnItem 和 EventMsg |
| Provider Adapter | Amadeus | 支持 Responses、Chat Completions 和 Provider Dialect |
| Persistence | Codex | ThreadManager、LiveThread、ThreadStore；JSONL canonical rollout + SQLite metadata index |

## 4. 明确非目标

Amadeus 不引入以下主链：

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
3. **Plan 服务于协作**：`update_plan` 展示当前执行 checklist，Plan Mode 产出可恢复的 Proposed Plan。
4. **Tool 按能力分类**：只读、结构化修改和进程执行拥有不同安全流程。
5. **修改先预览后落盘**：结构化文件修改必须先生成 Structured Diff。
6. **权限靠近 Tool**：Tool 根据真实输入返回 Allow、Ask 或 Deny；Ask 统一进入 Approval Runtime。
7. **Prompt 只描述真实能力**：未实现的 Tool、权限、MCP、Skill 或 Runtime 行为不得写进系统提示词。
8. **错误必须可见且区分瞬态与终态**：任何停止工作、上下文超限、Tool 失败或 Provider 错误都必须产生明确的 StreamError、Completed Item 或 Turn 终态；正在自动恢复的 response stream error 必须显式标记为 retrying，不得被 TUI 提前投影成永久错误或 Turn 终态。
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
│ TurnContext · StepContext · ContextManager                │
│ SessionServices · ModelClient · ModelClientSession        │
│ run_turn · ToolRouter                                     │
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
- 展示 Slash Command Popup、Selection Overlay、Approval Dialog 和 Request User Input Overlay。
- 将键盘、鼠标和终端 Resize 转换为 UI Action。
- 将 Application Result 和 `Event/EventMsg` 渲染为用户可见状态，并将 Approval response 提交为 correlated Op。

Interface 不负责：

- 创建第二份对话历史。
- 判断 Turn 是否完成。
- 直接执行 Tool。
- 自行压缩上下文。
- 自行维护或恢复 `update_plan` checklist 状态。

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

- 通过 `SessionIo` 向内部 Session 提交带 ID 的 `Submission/Op`。
- 接收统一的 `Event/EventMsg`；AgentStatus 仅由 Event reducer 派生为内部 watch/read model，不构成第二套公开协议。
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
- 由 `RegularTask` 调用 Session 模块内唯一 `run_turn` continuation loop，不创建独立 Runtime/Engine aggregate。
- 基于 SessionState.History、TurnContext 和每次请求冻结的 StepContext 构建模型采样 Prompt。
- 接收 ToolResult 并继续推理。
- 路由 Interrupt、Approval Decision 和待处理输入。
- 完成持久化后发布 Turn 终态事件。
- 发布稳定的 `Event/EventMsg`，不把 Model Step、单次 HTTP attempt、backoff tick 或 TUI 动画帧暴露为公共协议；但会影响用户等待体验和 Turn 是否继续运行的 response stream retry lifecycle 必须通过 typed transient EventMsg 明确公开。

### 6.5 Capabilities

Capabilities 是 Runtime 可调用的外部能力：

- LLM Provider Adapter。
- ToolRegistry、request-scoped ToolRouter/ToolSet、ToolDefinition、ToolUseContext 与 ToolExecutionService。
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
internal/agent/session/      Session、SessionState、SessionServices、ActiveTurn、RunningTask、SessionTask、StepContext 与 run_turn
internal/agent/turn/         TurnContext、ModeKind 与 Personality
internal/agent/protocol/     Identity、Submission、Event、EventMsg 与 TurnItem DTO
internal/context/            ContextManager、Token Accounting、Projection 与 Compaction
internal/prompt/             BaseInstructions、Prompt 与内置 Prompt 资产
internal/llm/                LLM Domain Port
internal/llm/openai/         OpenAI-compatible Adapter
internal/tool/               ToolDefinition、ToolRegistry、ToolRouter、ToolUseContext、ToolExecutionService 与 PermissionService
internal/policy/             ApprovalPort、ApprovalCoordinator、SessionPermissionContext 与静态策略
internal/tool/builtin/       Read、Edit、Write、Glob、Grep、Command 等内置 Tool
internal/process/            Host Process Runner
internal/rollout/            typed RolloutItem、JSONL 编解码与当前格式加载
internal/state/              StoredThread、Metadata Update 与 State DB Port
internal/state/sqlite/       当前 schema 的 SQLite State DB 与可重建 index
internal/interface/tui/      Codex 风格 Rich Inline TUI
internal/interface/cli/      CLI 参数和非交互输出
internal/agentsmd/           AgentsMdManager、LoadedAgentsMd、发现与作用域合并
internal/mcp/                MCP Config、MCPRuntime、Binding 与 Tool/Resource Catalog
internal/skill/              Skill Catalog、Metadata、Injection 与 Resource Boundary
internal/config/             配置加载、校验、脱敏
```

### 7.1 Package 内源码组织规则

目录负责表达稳定的领域或适配器边界，文件负责表达该目录内的一项内聚职责。为避免持续重构后重新形成“历史聚合文件”，源码遵循以下约束：

- 不为单纯缩短文件而新增 package；只有出现独立依赖方向、生命周期或可替换适配器边界时才拆目录。
- 同一 package 内优先按行为拆文件，文件名直接表达职责，例如 `approval_request.go`、`approval_decision.go`、`application_events.go`。
- Tool 实现按用户可见 Tool 拆分；多个 Tool 共用的安全写入、Diff、Approval 与 revalidate 流水线放入明确命名的共享文件。
- TUI Application 的状态与生命周期、Bubble Tea 更新、Runtime Event 投影和 View 渲染分别组织，不把业务事件归约与字符串渲染重新合并。
- 测试与被测 package 共置；架构约束测试必须扫描同一主链的全部拆分文件，不能只检查历史入口文件。
- 约 400–500 行是需要重新审视职责的软阈值，不作为机械拆分标准；Runtime 状态机或协议编解码在保持单一职责时可以超过该阈值。

生产依赖方向固定为：

```text
cmd/amadeus (Composition Root)
├─→ internal/interface + internal/app + internal/thread/manager
│   → internal/agent/session + internal/agent/turn
│   → internal/context + internal/tool + internal/llm domain ports
└─→ infrastructure adapters
    → internal/thread + internal/state + internal/tool + internal/llm domain ports
```

Domain/Runtime 不依赖 infrastructure adapter 或上层具体 controller/model；adapter 依赖并实现 domain port，再由 Composition Root 注入。Composition Root 可以引用所有装配对象，但不得因此成为执行 owner；若 Runtime 只能通过回调 `cmd/amadeus` 才能完成 Turn，即使类型位于目标目录，也视为架构未迁移完成。

当前关键文件布局：

```text
cmd/amadeus/
  composition.go             Composition Root：配置、Adapter、Session spawn args 与 ThreadManager 装配
  turn_interface.go          CLI/TUI 对 SessionIo 的提交、等待与终态适配
  agent_interactive.go       Fullscreen TUI 启动与 active attachment wiring
  interactive_commands.go    Slash Command 对 Application/Session typed API 的调用适配
  interactive_history.go     persisted ResponseItem/EventMsgItem 的界面恢复投影

internal/app/
  thread_workspace.go        当前 Thread 选择、切换、恢复、重命名和删除生命周期

internal/agent/session/
  session.go                 Session 状态机、submission loop 与 Turn 生命周期
  history.go                 ContextManager、Plan、canonical append 与增量 record
  interaction.go             Event/EventMsg 发布与 correlated approval waiter

internal/interface/tui/
  application.go             Fullscreen Application 类型、生命周期与模型装配
  application_update.go      Bubble Tea Update 与输入状态转换
  application_events.go      EventMsg / TurnItem 投影与历史状态更新
  application_view.go        View、状态栏、输入框与终端内容清理
  application_commands.go    Slash Command 分发
  application_selection.go   Session / Skill 选择流程

internal/policy/
  approval_types.go          Approval 公共枚举、Cause 与 Presentation 类型
  approval_request.go        Request 构造、校验、克隆与参数完整性
  approval_decision.go       Decision 校验与终端输入解析
  approval_port.go           ApprovalPort 边界
  approval_coordinator.go    并发请求协调
  presentation.go            用户可见 Approval 文案构建

internal/tool/builtin/
  file_tools.go              FileTools 配置、工厂与 ToolDefinition 暴露
  read_file.go               read Tool
  edit_file.go               edit Tool
  write_file.go              write Tool
  file_change.go             共享 Diff、Approval、revalidate 与原子写入流水线
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
   ├─ LiveThread → ThreadStore → JSONL canonical rollout
   └─ StateDB         → SQLite metadata/index
```

- `RolloutItem` 是按真实发生顺序追加的 canonical history protocol。
- `RolloutLine` 为每个 RolloutItem 增加 sequence 和 timestamp，并编码为单行 JSON。
- `StoredThread` 是 SQLite 中可重建的 Thread metadata/index，不是 Runtime Session。
- `InitialHistory` 表示 `New` 或 `Resumed` 的启动历史输入。
- JSONL 是 Resume、Context 重建、Compaction 和历史投影的唯一事实来源。
- SQLite 只保存 rollout path、CWD、标题、预览、模型、Token、时间和归档状态等可查询元数据。
- SQLite 可以落后 JSONL，但不能领先 JSONL；SQLite 丢失后必须可由 Rollout 重建。

`amadeus` 启动时可以先生成 ThreadID 并持有未物化的 AmadeusThread；第一次有效输入时才创建 Thread persistence 并写入 SessionMetaItem。`/resume` 和 `amadeus --resume <id>` 先读取 StoredThread 定位 Rollout，再以 `InitialHistory::Resumed` 重建运行时 SessionState。

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
- 持有进程级共享依赖，不执行 `run_turn`，不持有 ActiveTurn。

CLI/TUI 只通过 ThreadManager 和 AmadeusThread 使用 Runtime，不直接装配 Session 级依赖或 Rollout Writer。

Go 为避免 `thread → agent/session → thread` 包循环，将持久化边界放在 `internal/thread`，将需要 spawn internal Session 的管理层放在 `internal/thread/manager`。这是同一个 Thread Runtime 边界的无环包拆分，不建立第二套 ThreadManager。

#### Application ThreadWorkspace

`internal/app.ThreadWorkspace` 是界面无关的 Application Service，拥有“当前选中的 AmadeusThread”及其切换生命周期。ThreadManager 继续拥有进程内全部已加载 Thread；ThreadWorkspace 只负责 `EnsureCurrent`、`Resume`、`NewDraft`、`RenameCurrent`、`DeleteCurrent` 和当前 metadata 查询。

`cmd/amadeus` 只持有一个 ThreadWorkspace，不再分别保存 `ThreadManager`、`currentThread` 和对应 mutex。切换或清空当前 Thread 必须通过 ThreadWorkspace，并由 ThreadManager 同步完成 shutdown 与已加载实例移除，避免 Resume 重新取得已经终止的 Runtime 对象。

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
- 接收 `Event/EventMsg`，并从事件投影内部 AgentStatus。
- shutdown、wait 和 flush 生命周期。
- 隔离 Interface 与内部 Session 实现。

它不直接执行 `run_turn`，不持有 SessionState、ActiveTurn 或 Context History。

### 8.5 SessionIo、Submission、Event 与 EventMsg

`SessionIo` 是 AmadeusThread 与内部 Session Loop 的双向协议边界：

```go
type SessionIo struct {
    Submissions chan<- Submission
    Events      <-chan Event
    Terminated  <-chan struct{}
}
```

通道语义：

- `Submission`：Interface/Application 发往 Session 的命令，包含稳定 correlation ID 和 `Op`。
- `Event`：Session 发出的统一 envelope，包含与 Submission 对应的 ID 和一个 typed `EventMsg`。
- `EventMsg`：生命周期、业务项、增量、Approval request、User Input request、Warning 与 Error 的 tagged union。
- `Terminated`：Session Loop 已停止且清理完成。

Approval request 与 `RequestUserInputEvent` 都作为 `EventMsg` 进入同一 Event 流；回答分别作为带 RequestID 的 `ApprovalDecisionOp` 或 `UserInputAnswerOp` 和新的 `Submission` 返回 Session。两类交互共享 Session-owned request/response 边界，但拥有不同 typed payload、UI 和业务语义。Event 流是唯一公开输出主链；AgentStatus、Working 和其他界面状态只能由 Event reducer/watch 派生，不得建立 `Requests` 或 `Status` 第二通道。

首批 Op：

- `UserInputOp`
- `InterruptOp`
- `ApprovalDecisionOp`
- `UserInputAnswerOp`
- `CompactOp`
- `ThreadSettingsOp`
- `ShutdownOp`

Interface 不直接保存 cancel function，也不直接调用 ActiveTurn 或 Store。

`UserInputOp` 和独立 `ThreadSettingsOp` 共享同一个 typed settings contract：

```go
type ThreadSettingsOverrides struct {
    CollaborationMode *CollaborationMode
}

type UserInputOp struct {
    Content             string
    ClientUserMessageID string
    ThreadSettings      ThreadSettingsOverrides
}

type ThreadSettingsOp struct {
    ThreadSettings ThreadSettingsOverrides
}
```

`UserInputOp.ThreadSettings` 在接纳用户消息前原子应用；因此 `/plan <task>` 不需要 TUI 保存 `pendingModeTask`、等待 settings acknowledgement 后再提交第二个业务请求。若消息启动新 Turn，更新后的 settings 用于冻结新 TurnContext；若消息 steer 当前 Turn，当前已冻结 TurnContext 保持不变，更新后的 SessionConfiguration 只影响后续 Turn。独立 `ThreadSettingsOp` 只用于不提交用户消息的 `/plan` 和快捷模式切换，不能在 ActiveTurn 运行期间原地改变已冻结的 TurnContext。

普通用户消息只有一个 `UserInputOp`；steer 是该消息被 Runtime 接纳到当前 Turn 的方式，不是第二种用户意图，也不新增 `SteerOp`。Session 必须为用户消息返回 typed admission：

```go
type UserMessageAdmissionKind string

const (
    UserMessageAdmissionStarted UserMessageAdmissionKind = "started"
    UserMessageAdmissionSteered UserMessageAdmissionKind = "steered"
)

type UserMessageAdmission struct {
    Kind   UserMessageAdmissionKind
    TurnID TurnID
}
```

`UserMessageAdmission` 是 submission request 的同步接纳结果，不是 `EventMsg`、TurnItem 或 canonical RolloutItem。`Started` 表示消息创建并启动新 Regular Turn；`Steered` 表示消息已进入现有 Regular ActiveTurn 的 pending input。`AmadeusThread.SubmitUserInputAndWaitForAdmission` 对齐 CodexThread 的 admission API：先按 SubmissionID 注册一次性 waiter，再通过既有 SessionIo Submission 边界提交，Session 完成接纳后返回 admission。普通 `Submit` 只保证 Submission 已进入 Runtime channel，不能被 TUI 用来推断 Started/Steered。

对需要基于缓存 ActiveTurn 做严格路由的未来远程/API caller，`AmadeusThread.SteerInput` 接受 required `ExpectedTurnID`；实际 ActiveTurn 不存在、TurnID 不匹配或当前 Task 不可 steer 时必须返回 typed error，不能静默注入另一个 Turn。基础 in-process TUI 优先使用 admission API，避免只依据本地 `running` 状态猜测 Core 状态。

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
- 创建 TurnContext，并记录 TurnStartedEvent、TurnCompleteEvent 或 TurnAbortedEvent canonical RolloutItem。
- 创建、持有、调度和取消 SessionTask。
- 路由 Approval Decision、User Input Answer 与 Interrupt。
- 对 `UserInputOp` 执行 Started/Steered admission，并完成按 SubmissionID 注册的 user message admission waiter。
- 将 steer input 放入当前 Turn 的 `TurnInputQueue`，而不是 Session deferred submission queue。
- 决定需要记录的 Runtime 事实，通过 LiveThread 追加 canonical RolloutItem；瞬时 Delta、Working 和未决交互请求不进入 canonical Rollout。
- 使用 InitialHistory 重建 SessionState 与 ContextManager 投影。
- 在持久化和 flush 后发布 Turn 终态事件。
- 对尚未接纳的启动失败发布 correlated `ErrorEvent`；不存在独立 `TurnRejected` 协议。

同一 Session 最多只有一个前台 ActiveTurn。

Session 是 Turn 接纳、运行和终态的唯一协调者。`SessionTask.Run` 的返回值只回到 Session，由 Session 完成 canonical terminal append、flush、ActiveTurn 清理和终态 Event 发布；CLI、TUI 或 Application 不得建立第二套 `result chan`、done callback 或终态等待协议。Interface 只等待 SessionIo 中的 Event/Terminated，不同时等待私有 Task completion。

### 8.7 SessionState

`SessionState` 是跨多个 Turn 持续存在的会话级可变内存状态，不是数据库记录：

```go
type SessionState struct {
    Configuration        SessionConfiguration
    History              ContextManager
    PreviousTurnSettings *PreviousTurnSettings
}
```

`SessionConfiguration` 是 Collaboration Mode 的唯一 Session 级 owner：

```go
type ModeKind string

const (
    ModeKindDefault ModeKind = "default"
    ModeKindPlan    ModeKind = "plan"
)

type CollaborationMode struct {
    Mode ModeKind
}
```

基础版只要求 `ModeKind`；顶层 `model_reasoning_effort` 是全局 Model/Runtime override，不属于按模式设置。当 Amadeus 真正支持按模式覆盖模型、reasoning effort 或 developer instructions 时，再按 Codex 扩展 `CollaborationMode.Settings`，不得提前建立空壳配置层。TUI 不定义 `Execute/Plan` 第二套模式 enum；UI、Protocol、Session、TurnContext 和 Rollout 统一使用 `Default/Plan`。

Session 从 `InitialHistory` 恢复 ContextManager、PreviousTurnSettings 和当前 SessionConfiguration；canonical Rollout 由 LiveThread/ThreadStore 持有，SessionState 不再并列保存第二份 `[]RolloutLine`。`update_plan` checklist 不是 SessionState，也不从 Rollout 恢复；它只作为当前运行期间的 transient `PlanUpdateEvent` 交给 Interface 展示。Proposed Plan 也不是 SessionState：模型原始 ResponseItem 与 completed Plan TurnItem 进入 canonical history，未决 parser、stream buffer 和实施 Popup 不恢复。StoredThread 只用于定位 Rollout 和展示索引元数据。SessionPermissionContext 是 SessionServices 中的瞬时服务，Session spawn 时重新初始化，Resume 不恢复历史 grant。

### 8.8 SessionServices

`SessionServices` 对齐 Codex 的同名职责，是 Session-scoped capability aggregate，也是所有跨 Turn 可复用 Agent 能力的唯一 owner：

```go
type SessionServices struct {
    LiveThread   *LiveThread
    ModelClient  ModelClient
    Tools        *ToolRegistry
    ToolExecutor *ToolExecutionService
    Processes    *ProcessManager
    AgentsMd     *AgentsMdManager
    Skills       *SkillCatalog
    MCP          *MCPRuntime
    Web          WebServices
    Approvals    ApprovalService
    UserInput    UserInputRequester
    Permissions  *SessionPermissionContext
    Compactor    Compactor
    TimeProvider TimeProvider
    NextID       IDFactory
}
```

Composition Root 只构造外部 Adapter 和创建 Session 所需参数；Session spawn 直接构造完整 `SessionServices`，Session shutdown 直接关闭其中由本 Session 拥有的资源。

依赖与生命周期规则：

- CLI flags 和 invocation 必须先归一化为 typed SessionConfiguration 或 `Submission{ID, Op}`，再跨越 Runtime 边界。
- `SessionState.Configuration` 是当前 Session 配置的唯一事实源；SessionServices 或 closure 不得保存另一份 configured snapshot。
- Provider client、ToolRegistry、ToolExecutionService、ProcessManager、AgentsMdManager、Approval、Permission、MCP、Skill、Web 和 Compactor 不得在每个 Turn 中重新创建。
- Session 根据 Op 和 Turn 类型直接创建 `RegularTask`、`CompactTask` 或后续 ReviewTask。
- `RegularTask` 持有从 `UserInputOp` 接纳的目标、scoped EventSink 和 SessionServices 引用，并以 Session、TurnContext 和 cancellation 调用 Session 模块内 `run_turn`；Task 不持有或关闭 SessionServices。
- SessionTask 不反向调用 Application 的 `execute*Turn` 方法，也不持有 `cmd/amadeus` controller、Cobra command、TUI model 或完整 CLI invocation。
- AmadeusThread/Application 需要的查询由 Session/Thread 提供明确的 typed API。

SessionServices 不直接持有 SQLite Repository、JSONL 文件句柄或 Rollout Path；这些细节封装在 LiveThread → ThreadStore → LocalThreadStore 中。

#### 8.8.1 MCP 与 Skill Capability Lifecycle

MCP 与 Skill 都是 SessionServices 持有的 Session-scoped capability，但二者的生命周期和数据所有权不同：

```text
MCP:
SessionConfiguration
→ MCP Config
→ MCPRuntime
→ MCPBinding + lazy ToolCatalog/ResourceCatalog snapshot
→ StepContext
→ ToolExecutionService

Skill:
Skill Roots + Settings
→ SkillCatalog
→ SkillMetadata snapshot
→ explicit Skill selection / read_skill
→ ContextManager Skill update
→ StepContext
```

- `MCPRuntime` 是当前 Session 的 Server 生命周期、连接、发现结果、Resource Catalog 和 Binding Revision 的唯一 owner；MCP Client 不直接暴露给 Tool、Task 或 TUI。
- `SkillCatalog` 是当前 Session 的 Skill Root 扫描、metadata、启用状态、资源路径和 Catalog Revision 的唯一 owner；Skill 正文不作为常驻 Catalog 历史保存。
- MCPRuntime 和 SkillCatalog 在 Session spawn 时初始化并在 Session 内复用；MCP Server 连接、Tool Catalog、Resource Catalog 仍按需 lazy startup/discovery。Session shutdown 关闭 MCP Client、Process 和其他由 Session 拥有的资源。
- 配置、Server Catalog、Skill Metadata 或 Skill Resource 发生变化时，Runtime 生成新的 Revision；下一次 Model Step 重新 capture StepContext，旧 Deferred/Lazy Tool Call 必须返回 typed stale result，而不能继续使用旧 binding。
- MCP/Skill 不拥有第二套 Prompt、Agent Loop、Process Runner、Approval UI 或持久化历史。它们只向统一 Tool Registry、ContextManager、ToolExecutionService、Event/Rollout 和 TUI 提供 typed capability。

MCP 与 Skill 的命名以 Codex 领域术语为准：使用 `MCPRuntime`、`MCPBinding`、`ToolCatalog`、`ResourceCatalog`、`SkillCatalog`、`SkillMetadata`、`SkillInjection` 和 `SkillResource`；不得继续以 `Extension`、`Assembly`、`Capability` 或 `GenericProvider` 作为这些对象的生产职责名称。

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

- `LiveThread` 与 `ThreadStore` 负责 JSONL 创建、append、flush、resume 和 shutdown。
- `StateDB` 负责 StoredThread 的 SQLite 查询索引。
- 每个 Thread 只有一个活动 Writer，并使用 Thread 级锁串行化 append/flush/shutdown。
- Durable Append 必须先 write + flush JSONL，再由 MetadataSync 更新 SQLite。
- Buffered Append 可以只推进 Recorder 的内存/文件缓冲区和 Session 内存投影，但不得把未 flush 的 Rollout 事实写入 SQLite；Flush 成功后才允许按 durable watermark 执行 MetadataSync。
- Metadata 更新失败不能让 SQLite 超前于 Rollout；后续通过 backfill/reconciliation 补齐。

Session 不知道 ThreadStore 的具体实现；Runtime metadata 由 typed append receipt 和可重建索引维护。

### 8.10 Turn 与 TurnContext

Turn 是 Session 内一次具有明确开始、完成、失败或中断生命周期的工作。普通用户输入创建 regular Turn，`/compact` 创建 compact Turn。

运行时 `TurnContext` 是 Turn 启动时冻结的执行上下文；持久化使用独立的 `TurnContextItem` DTO，不能把 runtime object 直接序列化：

```go
type TurnContextItem struct {
    ThreadID ThreadID `json:"thread_id"`
    TurnID   TurnID   `json:"turn_id"`

    Provider        string           `json:"provider"`
    Model           string           `json:"model"`
    ReasoningEffort *ReasoningEffort `json:"reasoning_effort,omitempty"`

    CWD   string `json:"cwd"`
    Shell string `json:"shell,omitempty"`

    CurrentDate string `json:"current_date,omitempty"`
    Timezone    string `json:"timezone,omitempty"`

    Mode        ModeKind    `json:"mode"`
    Personality Personality `json:"personality,omitempty"`

    OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}
```

`TurnContext` 创建后不再修改，但可以持有运行所需的 typed 引用和取消关系；`TurnContextItem` 只保存恢复和诊断所需的稳定纯数据。二者必须通过显式 projector 转换，durable DTO 不引用 Client、API Key、Mutex、Cancellation、Telemetry 或其他进程对象。

TurnContext 必须在本 Turn 的 Provider、ModelInfo、ModelReasoningEffort、Collaboration Mode、Approval Policy、Permission Profile、OutputSchema 和稳定环境事实解析完成后创建。`Default/Plan` 属于 `ModeKind`/CollaborationMode，不得再命名为 PermissionMode；Approval Policy 与文件/网络 Permission Profile 是彼此独立的安全概念。CurrentDate、Timezone、Personality、ReasoningEffort 和 OutputSchema 要么记录真实生效值，要么明确为空，不能为了贴合结构而填充未接线占位值。Resume 恢复的 `PreviousTurnSettings` 必须存在明确消费点，否则不得作为已完成 capability 保留。

`ToolNames` 不属于最终 TurnContext 或 TurnContextItem。Tool Catalog、MCP binding、Skill revision、AGENTS.md 和执行环境可能在同一 Turn 的两次模型采样之间变化；这些请求级事实由 StepContext 冻结。旧 Rollout 中的 `ToolNames` 不读取、不迁移，也不作为生产请求的工具事实源。

以下内容明确不属于 TurnContext：

- BaseInstructions 和 ContextManager 属于 SessionConfiguration/SessionState。
- Provider Client、Tool Registry、AgentsMdManager、MCP 与 Skill Runtime 属于 SessionServices。
- Cancellation、done、execution handle 和 panic/error capture 属于 RunningTask；终态映射与 ActiveTurn 清理由 Session 完成。
- 完整 Tool Specs 与本次采样的可见工具集合属于 StepContext。

### 8.11 StepContext

`StepContext` 对齐 Codex request-scoped StepContext，表示一次模型采样以及紧随其后的 Tool Call 使用的精确动态能力快照：

```go
type StepContext struct {
    Turn                  TurnContext
    Prompt                PromptSnapshot
    Model                 ModelInfo
    BaseInstructions      BaseInstructions
    ToolRouter            ToolRouter
    ModelMessagesRevision string
    WorldStateRevision    string
}
```

要求：

- 每次模型采样前重新 capture；同一次采样构建 Prompt、向模型声明 Tool 和执行模型返回的 Tool Call 必须使用同一个 StepContext/ToolRouter。
- StepContext 是进程内 immutable value，不作为 TurnContextItem 持久化；其中需要恢复的变化通过 typed ResponseItem 或对应 EventMsgItem 记录。
- ToolRouter 是当前 Step 最终广告和允许执行的工具计划；它冻结 exact ToolDefinition binding、visibility、parallel capability，以及 MCP、Skill、AgentsMd 和 router revision，由 ToolExecutionService 用于拒绝 stale deferred/lazy capability。
- capture 顺序必须先刷新 AgentsMd、Skill 与 MCP snapshot，再构造 ToolRouter 和 PromptSnapshot；Prompt 在 sampling request 构建阶段从 ContextManager、TurnContext 与 StepContext 统一生成，不能把可变 Prompt、EventSink 或 mutable resolver handle 塞进 StepContext。
- Plan Mode 通过 TurnContext.CollaborationMode 驱动 Prompt assembly，并在 StepContext.ToolRouter 中应用 request-scoped Tool policy；不创建另一种 SessionTask 或另一条 Engine 主链。
- ToolRouter snapshot 是模式 Tool 可见性的唯一事实源；Prompt guidance、模型 ToolSpec 与实际 dispatch 必须读取同一 snapshot。一次性的 `prepareStaticTurnContext` 与逐批输入执行的 `prepareInputContext` 都不得再次调用 mutable Registry 计算第二份 Plan Tool 列表。

### 8.12 ActiveTurn

`ActiveTurn` 表示 Session 当前正在执行的 Turn：

```go
type ActiveTurn struct {
    SubmissionID SubmissionID
    Task         *RunningTask
    State        *TurnState
}

type TurnState struct {
    pendingRequests map[RequestID]chan Op
    pendingInput    TurnInputQueue
}
```

ActiveTurn 只保存当前 Submission correlation、RunningTask 和 TurnState。TurnState 是当前 Turn 的可变协调状态：Approval 与 `request_user_input` 使用不同 Request/Response 类型，但都由 Session Loop 按 RequestID 注册、交付和清理；`pendingInput` 保存已接纳但尚未进入模型 history 的同 Turn 输入，两者不得复用 channel、RequestID 或 payload。Usage 与 Tool Call 计数由 `run_turn` 累计并通过最小 `TaskOutput` 返回；SessionPermissionContext 属于 SessionServices。Turn 完成后，Session 清除 pending waiter、TurnInputQueue 与 ActiveTurn，历史 Turn 生命周期继续保存在 canonical Rollout 中。

### 8.13 RunningTask 与 SessionTask

`SessionTask` 对齐 Codex SessionTask：它由 Session 创建、持有、调度和取消，并通常驱动一个 Turn。

```go
type SessionTask interface {
    Run(context.Context, *Session, *TurnContext) (TaskOutput, error)
}

type TaskKind string

const (
    TaskKindRegular TaskKind = "regular"
    TaskKindCompact TaskKind = "compact"
)
```

SessionTask 直接使用 Session 提供的 typed 方法。ContextManager 的 mutation 只能由 Session 对已接纳的 canonical facts 执行。`TaskOutput` 只表达任务产生的最小业务输出。TurnCompleteEvent、TurnAbortedEvent、ErrorEvent、Usage、Tool count、canonical append 和 Event 发布由 Session 统一创建。`RunningTask` 是 Session 保存的运行记录，持有具体 Task、TaskKind、TurnContext、cancellation、execution handle 和 completion notification；启动 goroutine、取消、清理 ActiveTurn 与终态收尾属于 Session。基础版只有 Regular 可 steer，Compact 明确返回 `ActiveTurnNotSteerable`；不得继续使用 `compact bool` 作为 Task identity 或 steerability 判断。

首批 SessionTask：

- `RegularTask`：调用单一 `run_turn` 执行普通或 Plan Mode Turn。
- `CompactTask`：执行上下文压缩。

当 TurnContext.Mode 为 `plan` 时，RegularTask 使用 Plan Collaboration Mode，并继续复用相同的 SessionTask 与 `run_turn` 生命周期。

### 8.14 RolloutLine 与 RolloutItem

`RolloutItem` 是 Thread 中按序持久化的 canonical typed sum；`RolloutLine` 只增加当前格式需要的 version、sequence 和 timestamp。ThreadID、TurnID、SubmissionID 和 RequestID 由 Protocol/Identity domain 拥有，并出现在相应 typed item 中，不由 persistence package 定义或别名化：

```go
type RolloutLine struct {
    Version   int
    Sequence  uint64
    Timestamp time.Time
    Item      RolloutItem
}

type RolloutItem interface {
    isRolloutItem()
}
```

首批 `RolloutItem` variant：

- `SessionMetaItem`
- `ResponseItem`
- `CompactedItem`
- `TurnContextItem`
- `EventMsgItem`

`ResponseItem` 保存模型可见的 User/Assistant/Reasoning/Tool Call/Tool Result 事实；需要恢复的 Turn/Item 生命周期、Plan、Token、Context、Approval 结果和终态使用对应的 typed `EventMsg` 原样持久化。

ItemStartedEvent、ItemCompletedEvent、TurnCompleteEvent、TurnAbortedEvent 等是否持久化由统一 store policy 决定；高频 Delta、Working、未决 ApprovalRequestEvent、未决 RequestUserInputEvent 和 retrying StreamErrorEvent 默认不持久化。Resume 只读取当前版本 typed RolloutItem，并由同一 Event/ResponseItem projector 重建 Context 与 UI。

JSON codec 可以为 tagged union 编码 discriminant，但生产代码不得把 `kind + json.RawMessage`、`map[string]any` 或本地 ad-hoc struct 作为领域主链。当前版本的每个 variant 必须有唯一 typed Go contract 和 round-trip test。未知 kind/version 或任何旧 Rollout 格式直接返回明确的 unsupported-format 错误；不提供旧 reader、fallback decoder 或 migration projector。

### 8.15 Model Step

Model Step 是 `run_turn` 中的一次模型 continuation：

```text
Capture StepContext
→ Maybe Compact
→ Sample Model Stream
→ Persist Response Items
→ Execute And Persist Tool Results when present
→ Continue or Complete
```

Model Step 只是内部运行和遥测术语，不建表、不进入 canonical 产品协议，也不作为 TUI 强制分隔边界。恢复只依赖 canonical Rollout；不会恢复旧 Model Step、Provider stream 或 Go 调用栈。

### 8.16 Turn Steer、TurnInput 与 InputQueue

Turn steer 表示用户在一个 Regular Turn 运行期间补充信息。补充消息属于当前 Turn，在当前模型步骤结束后进入同一 `run_turn` 的下一次 continuation；它不创建第二个 TurnStartedEvent，不独立产生 Turn terminal，也不复用 `request_user_input` 或 Approval lifecycle。

内部数据模型固定为：

```go
type TurnInput interface {
    isTurnInput()
}

type UserTurnInput struct {
    Content  string
    ClientID string
}

type TurnInputQueue struct {
    // Turn-local FIFO storage; implementation owns synchronization/sealing.
}

type InputQueue struct {
    // Session-scoped coordinator for enqueue/has/drain and future activity notification.
}
```

基础版只实现文字 `UserTurnInput`；未来在真实需要 additional context、multi-agent mailbox 或多模态输入时再增加新的 TurnInput variant，不提前复制 Codex 的完整 InputQueue 功能。`TurnInputQueue` 是 Turn-scoped storage，随 ActiveTurn 创建和销毁；`InputQueue` 是 Session-scoped coordinator，本身不得成为第二份 conversation history。

普通用户消息接纳顺序固定为：

```text
Receive UserInputOp
→ validate input and atomically apply ThreadSettingsOverrides
→ try steer current ActiveTurn
   → active Regular Task: enqueue UserTurnInput, return Steered{TurnID}
   → no ActiveTurn: create RegularTask, return Started{TurnID}
   → active Compact Task: return ActiveTurnNotSteerable
→ complete UserMessageAdmission waiter
```

显式 steer 的错误模型至少包括：

- `NoActiveTurn`
- `ExpectedTurnMismatch{Expected, Actual}`
- `ActiveTurnNotSteerable{TaskKind}`
- `EmptyInput`

Session 的 deferred submission queue 只保存明确允许延后执行的 Session operation；`UserInputOp` 在 ActiveTurn 期间不得进入该 queue。Core 不静默把 rejected steer 变成下一 Turn；如未来需要“压缩结束后自动发送”，由 Interface/Application 维护用户可见的 rejected-steer queue，并在重新提交时获得新的 admission。

Steer 默认不取消正在进行的普通模型 stream、Tool、Approval wait 或 `request_user_input` wait。特殊等待 Tool 若未来需要被新输入唤醒，必须订阅 typed InputQueue activity，不得让所有 Tool 隐式观察全局输入 channel。

`ClientUserMessageID` 用于 Interface optimistic rendering 与 Runtime UserMessage Item 的确认/去重。初始输入和 steered input 都必须形成同一 canonical/live 生命周期：先追加当前 Turn 的 `ResponseUserMessage` 和需要持久化的 completed UserMessage TurnItem，再发布 Item lifecycle Event；TUI 不得长期依赖“初始消息只由本地插入、steer 消息只由 Runtime 插入”的双来源规则。

## 9. Canonical Runtime 流程

### 9.1 Canonical Turn Flow

```text
User Input
→ AmadeusThread.SubmitUserInputAndWaitForAdmission(UserInputOp)
→ SessionIo.Submissions
→ internal Session 接纳输入
→ 无 ActiveTurn 时返回 Started，创建新 Turn
→ 构建 TurnContext
→ 创建 RegularTask
→ LiveThread 物化 SessionMetaItem（仅首次）
→ 追加 TurnContextItem、用户 ResponseItem 与 TurnStartedEvent
→ flush JSONL canonical rollout
→ 创建 RunningTask 并设置 ActiveTurn
→ 发布 TurnStartedEvent
→ RegularTask 调用 Session 模块内 run_turn
→ capture StepContext：AgentsMd、环境、MCP binding、Skill snapshot 与 ToolRouter snapshot
→ 必要时通过 Compactor 追加 CompactedItem/ContextCompactedEvent 并重新 capture StepContext
→ Turn-scoped ModelClientSession 发起 LLM Stream
→ 发布 ItemStartedEvent 与 Agent/Reasoning Delta Event
→ 模型完成一个 ResponseItem 后 canonical append ResponseItem 与需要持久化的 ItemCompletedEvent
→ 发布 ItemCompletedEvent
→ 若存在 Tool Call，使用同一 StepContext 的 Tool Router 执行
→ Tool + Approval Event/Response Runtime
→ canonical append Tool Result 与需要持久化的 ItemCompletedEvent
→ 下一 Model Step 重新 capture StepContext
→ 模型返回 Final Response 后结束 run_turn
→ Session 追加 TokenCount 与 TurnComplete EventMsg
→ flush JSONL canonical rollout
→ MetadataSync 更新 SQLite StoredThread
→ Session 清除 ActiveTurn
→ 发布 TurnCompleteEvent
→ TUI 显示 Worked for ...
```

关键不变量：

1. 任何模型调用和文件副作用前必须已将 TurnContextItem、用户 ResponseItem 和 TurnStartedEvent 写入并 flush canonical rollout。
2. ToolCall 与 ToolResult 必须可配对；ResponseItem 与按 store policy 持久化的 ItemCompletedEvent 必须在 live ItemCompletedEvent 前进入 Session-owned canonical append 顺序。
3. Turn 终止只能记录 TurnCompleteEvent 或 TurnAbortedEvent；启动失败使用 correlated ErrorEvent。
4. Turn 终态 Event 必须在持久化、rollout flush 和 ActiveTurn 清理后发布。
5. TUI 消失或动画停止不能代替 Turn 终态。
6. Resume 通过 StoredThread 定位 Rollout，并由 InitialHistory 重建语义，不恢复 Go goroutine 或旧 RunningTask。
7. 尚未接纳的 Turn 失败使用 correlated ErrorEvent；已经发出 TurnStartedEvent 的 Turn 必须以 TurnCompleteEvent 或 TurnAbortedEvent 收尾。

`SessionIo` 是唯一 canonical 生命周期协议。`run_turn`、Tool 和 Application 只通过 Session-owned typed methods 与 `Submission/Event/EventMsg` 边界进入统一主链。

#### 9.1.1 Same-Turn Steer Flow

Active Regular Turn 期间的补充输入使用同一 Submission 入口，但返回 `Steered` admission：

```text
User Input while Regular Turn is active
→ AmadeusThread.SubmitUserInputAndWaitForAdmission(UserInputOp)
→ Session Loop validates settings and current ActiveTurn
→ InputQueue appends UserTurnInput to ActiveTurn.TurnState.pendingInput
→ return UserMessageAdmission{Steered, current TurnID}
→ current Model Step / Tool continues without ordinary preemption
→ persist current model/tool completion
→ run_turn observes pending input and keeps the same Turn alive
→ FIFO drain pending input
→ canonical append ResponseUserMessage + completed UserMessage TurnItem
→ publish UserMessage Item lifecycle
→ refresh input-dependent Skill/MCP context
→ capture a new StepContext
→ sample the next continuation in the same Turn
→ eventually publish the single Turn terminal
```

Same-turn steer 的关键不变量：

1. Steered admission 返回的 TurnID 必须等于当前 ActiveTurn TurnID。
2. 初始用户输入必须先进入第一次模型请求；刚启动 Turn 时到达的 steer 不能越过初始输入。
3. pending input 在下一次 sampling request 构建前进入 canonical Rollout 和 ContextManager；不得只存在于 TUI 或 `regularTask.goal`。
4. 一个或多个 steer 不产生额外 TurnStartedEvent、TurnContextItem 或 Turn terminal。
5. 多个 steer 按接纳顺序 FIFO 记录和采样；不得按 goroutine 完成顺序重排。
6. Final model response 与 pending input 同时存在时，Final Response 先完成其 canonical Item lifecycle，然后 pending input 触发同 Turn follow-up。
7. Interrupt 或终态竞态不得静默丢弃已经返回 `Steered` 的输入；无法继续采样时至少应在 terminal 前记录已接纳 UserMessage，或通过原子 sealing 让该提交退化为新 Turn admission。
8. ApprovalDecisionOp、UserInputAnswerOp 和 steer UserInputOp 使用不同路由；普通用户消息不得满足 interactive waiter。

### 9.2 Go Runtime Concurrency Model

Amadeus 学习 Codex 和 Claude Code 的架构语义，但 Runtime 必须使用符合 Go 习惯的所有权、取消和并发模型，而不是逐类或逐文件翻译其他语言实现。

#### Session Loop

每个活动 internal Session 拥有一个长期 Session Loop goroutine：

```text
Session Loop
├── 接收 Submission
├── 完成 UserMessage Started/Steered admission
├── 向 ActiveTurn TurnInputQueue 接纳 steer input
├── 路由 Approval Response
├── 接收 RunningTask Result
├── 管理 ActiveTurn
├── 发布 Event / EventMsg
└── 处理 Shutdown
```

Session Loop 是 SessionState、ActiveTurn、InputQueue 和 Turn 接纳顺序的主要单一所有者。优先通过单 goroutine 所有权避免为每个字段增加 Mutex；但 TurnInputQueue 同时由 Session Loop enqueue、RunningTask/run_turn inspect/drain，因此必须通过 TurnState 内部短临界区或等价原子协议保护，不能依赖 Go `select` 在 Submission 与 Completion 同时 ready 时的非确定顺序。

#### RunningTask

前台 `SessionTask` 以独立 RunningTask goroutine 执行，使 Session Loop 在模型、Tool 或 Approval 等待期间仍可处理 Interrupt、ApprovalDecisionOp 和 Shutdown。

- 同一 Session 最多只有一个前台 ActiveTurn/RunningTask。
- RunningTask 必须有明确 parent Context、CancelCause、Done Result 和 cleanup owner。
- RunningTask goroutine 无论正常返回、Context 取消还是 panic，都只能发送一次 Completion 并关闭自己的 done channel。
- RunningTask 返回前必须与 TurnInputQueue 完成原子 completion handshake：有 pending input 时继续 Turn；无 pending input 时 seal 当前 queue 后才允许返回。seal 后到达的普通用户消息不能再得到当前 Turn 的 `Steered` admission。
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
    Events      <-chan Event
    Terminated  <-chan struct{}
}
```

- Channel 只用于生命周期和所有权边界，不替代普通同步函数调用。
- UserMessageAdmission 使用按 SubmissionID 注册的一次性 waiter；它是提交 request/response，不增加第二条长期公开 Event channel，也不进入 Rollout。
- 创建并发送数据的一方负责关闭 Channel；消费者不得关闭接收端。
- Session 退出时按固定顺序停止接纳 Submission、取消 RunningTask、完成持久化、关闭输出并通知 Terminated。
- Event Channel 使用有界缓冲；高频 Delta 可以在投影层合并，Turn/Item 终态、Approval request 与 User Input request 不得静默丢失。
- 不建立支持任意 Subscriber、Topic、Priority 和 Critical Backpressure 的通用 Event Bus。

#### Interactive Waiter

公开协议分别保持 `ApprovalRequestEvent → ApprovalDecisionOp` 与 `RequestUserInputEvent → UserInputAnswerOp`。Session 内部使用按 RequestID 路由的一次性 buffered response channel 机制承载两类等待，并通过 tagged request/response 类型保持领域边界；Approval 只返回授权决定，User Input 只返回问题答案。Session Loop 收到 Response Op 后定位 waiter 并交付 typed response；Turn Context 取消、用户拒绝回答或 Session shutdown 时 waiter 必须同步释放。所有请求通过统一 Event 流输出、通过 Submission/Op 返回。

#### 有界 Tool 并发

Model Step 保持串行；只有同一次模型响应中由 Tool 的 `SupportsParallelToolCalls` 声明为安全的独立 Tool Call 可以并行：

- 使用带 Context 的有界 task group，限制并发数并在错误或取消时停止剩余任务。
- 结果按原 Tool Call 顺序回灌模型，不按 goroutine 完成顺序改变协议。
- `read/glob/grep/view_image/web_search/web_fetch` 可以有界并行。
- `write_stdin` 在 Tool Registry 层声明可并行，但 ProcessManager 必须按 `process_id` 串行化同一进程的输入与轮询；不同进程可以并行。
- `edit/write/execute_command/update_plan/request_user_input/MCP Call` 串行。

#### 同步持久化与进程 goroutine

- Canonical JSONL append/flush 第一版保持同步单 Writer，不为展示并发能力而增加后台 Writer goroutine。
- SQLite MetadataSync 发生在 durable JSONL 之后；SQLite 失败不得回滚已写入的 canonical history，后续通过 reconciliation 修复。
- 长期子进程可以拥有独立 waiter goroutine 和 done channel，因为其生命周期天然独立；Owner 使用 TurnID/RunningTask ID，并受 Turn Context 取消。
- 单次 Web/Provider HTTP 调用保持同步 Context API；HTTP Client 负责连接池，多次独立调用由 Tool Executor 做有界并发。

## 10. Codex 风格 Turn Continuation Loop

### 10.1 SessionServices 与直接 Task 创建

Codex 对应职责直接落在 `Session`、`SessionState` 与 `SessionServices`：

```text
ThreadManager
→ Session::spawn(SessionSpawnArgs)
→ Session { state, services, activeTurn }
→ Session 根据 Op 创建 RegularTask / CompactTask
→ Session::spawn_task
```

所有权必须满足：

- Session spawn 时构造完整 SessionServices；不得通过首次 `Prepare` 惰性创建第二层 capability aggregate。
- Provider client、ToolRegistry、ToolExecutionService、ProcessManager、MCP/Skill/Web、Approval、Permission 和 Compactor 不在每 Turn 重建。
- Session 根据 Op 直接创建具体 Task。
- RegularTask 只把 Session、TurnContext、目标和 cancellation 交给 `run_turn`。
- CompactTask 与自动压缩共享 SessionServices.Compactor；自动压缩是 `run_turn` 操作，不是嵌套 SessionTask。
- Session shutdown 直接关闭 SessionServices 中由本 Session 拥有的资源，不通过 Factory Close 间接释放。

### 10.2 `run_turn` Contract

`run_turn` 是 Session 模块中的唯一 regular continuation loop，不是可替换的 Engine 对象：

```go
func runTurn(
    context.Context,
    *Session,
    *SessionServices,
    TurnContext,
    EventSink,
) (TaskOutput, error)
```

`run_turn` 通过 Session 的明确方法完成：

- capture immutable StepContext；
- append typed canonical facts；
- 在 canonical append 成功后发布 Item lifecycle Event；
- 发布 ApprovalRequestEvent 并等待 correlated ApprovalDecisionOp；
- 通过 Session-owned Approval port 等待 ActiveTurn 中 correlated pending decision；
- 发布 RequestUserInputEvent 并通过 Session-owned interaction port 等待 correlated UserInputAnswerOp；
- 在 Model Step 边界检查、drain 并 canonical record 当前 ActiveTurn 的 pending TurnInput；
- 更新 Turn usage/tool-call counters，但不直接完成或清除 ActiveTurn。

Session 是 Task completion、Turn terminal、ActiveTurn cleanup 和 durable terminal flush 的唯一 owner。

### 10.3 Canonical Continuation Loop

单一执行循环固定为：

```text
Capture StepContext
→ Check Context Budget / Maybe Compact
→ Sample Model Stream
→ Persist completed model ResponseItems
→ If Tool Calls: Execute with the same StepContext
→ Persist one Tool Result for every Tool Call
→ Inspect model continuation and pending TurnInput
→ If neither requires follow-up: seal TurnInputQueue and return
→ Drain allowed pending TurnInput into canonical history
→ Refresh input-dependent context and capture a new StepContext
→ Continue in the same Turn
```

关键规则：

- 单次 sampling request 的 Prompt input、模型可见 Tool Specs 和 Tool 执行路由必须来自同一 ContextManager/TurnContext/StepContext 组合；Tool Specs 与执行路由必须由同一个 ToolRouter 提供。
- 每个 Tool Call 都必须产生 Tool Result，包括 denied、failed、stale、cancelled 和 argument error；普通 Tool 错误作为模型可见结果继续循环，不直接使 Turn failed。
- 只有 Provider fatal error、Context/persistence invariant 破坏、无法补齐 Tool 协议或 Session 内部错误才以 failed 结束。
- Final Response 不由独立 Analyze 阶段判定；Provider Adapter 输出的标准化 finish reason 与 ResponseItem 决定 continuation。
- Final Response 只表示当前 sampling request 不再要求模型自身 continuation；当 ActiveTurn 仍有 pending TurnInput 时，`run_turn` 必须在同一 Turn 内继续。
- `run_turn` 不保存可恢复执行位置；Resume 从 canonical Rollout 重建 Context，再由新 Turn 重新采样。

#### 10.3.1 Pending Input Drain 与 Compaction 顺序

`run_turn` 使用内部 `canDrainPendingInput` 或等价状态表达当前 continuation 是否允许消费 steer：

- 新 Regular Turn 的第一次 sampling request 前为 false，确保初始 UserInput 先被采样。
- 普通 sampling request 完成并持久化结果后为 true。
- Tool Calls 先使用产生这些调用的同一 StepContext 执行并持久化 Tool Result，再允许 pending input 进入后续请求。
- auto-compaction request 不包含尚未 drain 的 steer input。
- 若 compaction 后仍需恢复压缩前已有的 model/tool continuation，steer 继续 pending，直到该 continuation 完成。
- 若模型已经 Final，只有 pending input 要求 follow-up，则 compact 完成后可以直接 drain steer，不发送无业务输入的空恢复请求。

每批 drain 的顺序固定为：

```text
Take pending TurnInput in FIFO order
→ run input inspection/hooks
→ durable append current-Turn ResponseUserMessage
→ append completed UserMessage TurnItem according to store policy
→ update ContextManager through the same Session append boundary
→ publish live UserMessage Item lifecycle
→ refresh input-dependent explicit Skill context; recapture current MCP binding in StepContext
→ capture StepContext from the updated canonical history
```

Steer 接纳时只进入 TurnInputQueue，不立即修改正在采样请求所使用的 PromptSnapshot。ContextManager 仍是唯一模型历史 owner；不得在 InputQueue、TUI 或 ModelClientSession 中维护第二份已消费 steer history。

Tool 调用链采用明确的混合边界，而不是强行复制任一参考项目：

```text
Codex-style StepContext / ToolRouter snapshot
→ Amadeus ToolExecutionService
→ Claude-style Validate / Prepare / Permission / Approval
→ Execute / Typed ToolResult
→ canonical append / continuation
```

- Codex 决定 Session、Turn、StepContext、ToolRouter snapshot 与 continuation owner。
- Claude Code 决定 Tool 内层的 Validate、Prepare、Permission、Approval、Revalidate 与 Execute 行为。
- ToolExecutionService、ApprovalCoordinator、ApprovalPort、PreparedToolUse、RequestSnapshot 和 SessionPermissionContext 可以保留，但不得承担 Session、Turn terminal 或 Tool catalog owner 的职责。

### 10.4 Model Stream 与 Item 顺序

Session-scoped `ModelClient` 创建 Turn-scoped `ModelClientSession`；sampling request 处理负责 Provider request、retry、stream aggregation 和 Delta Event，但不拥有 Turn terminal。一个 ModelClientSession 在同一 Turn 的重试和多次 sampling request 间复用，不跨 Turn 复用。完成态顺序固定为：

```text
ItemStartedEvent
→ zero or more Delta
→ canonical append ResponseItem
→ canonical append EventMsgItem(ItemCompletedEvent)
→ ItemCompletedEvent
```

Tool Call/Result 也遵守同样顺序。允许多个 completed facts 先 buffered append、在 Turn durability boundary 统一 flush，但不能先向 TUI 宣布 Completed 再只保存在 `run_turn` 私有内存队列中。

#### 10.4.1 Request Retry 与 Response Stream Reconnect

Provider request retry 与 response stream reconnect 是两个不同生命周期，必须使用不同配置、计数和 owner：

- `request_max_retries` 只处理请求建立前后可安全重试的 HTTP/transport failure，由 Provider client/adapter 按统一配置执行；它不产生 Turn-visible `Reconnecting...` 状态。
- `stream_max_retries` 处理已经建立的 response stream 在完成前断开、idle timeout 或其他可恢复读取错误，由 Turn-scoped `ModelClientSession` 或 Session 模块内专用 response retry helper 负责。
- `stream_idle_timeout` 定义 response stream 多久无活动后视为连接丢失；Provider Adapter 负责把 transport timeout、Retry-After 和底层错误归一化为 typed `ProviderError`，不拥有 Turn 状态或 TUI 文案。
- retryability 判定、retry counter、delay、取消、瞬态 Event 和重试耗尽后的最终错误必须由同一 Core owner 串行协调；不得由 OpenAI SDK、TUI 和 Compactor 分别维护三套重试事实。

当前 Amadeus 只支持 HTTP/SSE Provider transport，因此 K 阶段的连接恢复配置固定为上述 **2 个 retry count + 1 个 stream idle timeout**。Codex 另有 `websocket_connect_timeout_ms`，但 Amadeus 在真正实现 WebSocket transport、capability detection 和 transport fallback 前不得增加无效的 `websocket_connect_timeout` 配置或伪 fallback 分支。

可恢复 stream failure 的顺序固定为：

```text
Sample Model Stream
→ classify retryable ProviderError
→ publish StreamErrorEvent{WillRetry: true, Message: "Reconnecting... n/m"}
→ cancellable backoff
→ reuse the same Turn-scoped ModelClientSession and sampling request semantics
→ reopen response stream
→ next normal live Event restores the previous TUI status
→ success continues the same Turn
```

重试耗尽或错误不可恢复时，Core 发布 `WillRetry: false` 的最终错误语义并让 `run_turn` 返回 failed result；Session 仍按唯一 Turn terminal protocol 完成 durable terminal append、ActiveTurn cleanup 和 `TurnCompleteEvent`。TUI 不得根据计数或错误字符串自行决定 Turn 是否结束。

普通 sampling、手动 `/compact` 的 CompactTask 和 `run_turn` 内自动 compaction 必须复用同一 response-stream retry policy；Compactor 可以使用不同请求类型和 Prompt，但不得拥有独立、不可观察的 retry loop。

#### 10.4.2 Partial Delta 与恢复边界

Stream 在已发布部分 Delta 后断开时，不能简单重新请求并把新 Delta 继续追加到旧 draft。实现必须明确 attempt-local aggregation 与用户可见 draft 的关系，并满足：

- 未形成 completed ResponseItem 的 Delta 不是 canonical fact，不写入 Rollout，也不参与 Resume replay。
- 每次 retry attempt 使用独立 aggregation state；只有成功完成的 ResponseItem 才进入 canonical append 与 ItemCompletedEvent 顺序。
- 新 attempt 如果从头返回内容，TUI/stream projector 必须替换或按稳定 item identity 去重旧 attempt 的未完成 draft，不能产生重复文本、重复 Tool Call 或重复 reasoning。
- `ItemStartedEvent.Item.Kind` 是 live projector 的稳定分流依据：Assistant/Reasoning start 只建立 draft identity，不得创建 Tool/Explored HistoryCell；只有 Tool/Command/File activity 才进入 Tool activity projector。TUI 不得用空 `ToolName`、事件到达顺序或展示字符串猜测 item 类型。
- 已完成并 canonical append 的 ResponseItem 不因后续 sampling request 重试而回滚；retry 只作用于当前未完成 sampling request。
- cancellation 在 stream read 和 backoff 期间都必须立即生效，并最终走 `TurnAbortedEvent` 或既定 interruption contract，不能被下一次 retry 吞掉。

Response stream retry Event 是 live、transient、non-canonical notification。Resume/replay 不重放历史上的 `Reconnecting...` 状态，也不尝试恢复旧 Provider stream、retry counter、backoff timer 或 attempt-local draft。

### 10.5 Progress、Budget 与停止条件

`run_turn` 不使用“相同调用两次”或“相同错误两次”之类通用启发式判定 stalled。重复错误可以形成 model-visible reminder 或 telemetry，但不拥有 Turn 终止权。

基础预算只包括明确可解释的限制：

- 最大模型采样次数；
- 最大 Tool Call 次数；
- 最大 Turn wall-clock；
- Model context/token limit。

这些限制是 Amadeus 明确保留的高阈值 runaway safety，并非声称与 Codex 一比一同构。它们不再暴露旧 `agent.max_iterations`、`agent.max_tool_calls` 或 `agent.max_duration` 配置，避免形成低阈值且行为不稳定的产品契约。接近预算时只向模型注入一次明确的完成提醒，使其总结当前结果；真正耗尽后返回 typed blocked outcome，而不是伪装成 Provider 或 Tool infrastructure error。模型采样、Tool Call、token 与 elapsed time 在 Turn 执行期间累计，最终 Usage/Tool count 进入 TaskOutput，canonical TokenCountEvent 支持 Resume 后重建累计事实。

### 10.6 默认执行与软计划

默认模式中 `update_plan` Tool 可用，但计划不是 `run_turn` 的前置阶段：

- 简单任务直接完成，不要求创建计划。
- 复杂、多阶段或长时间任务由模型按需调用 `update_plan`。
- checklist 只用于方向、进度沟通和当前运行期间的用户可见性，不属于 SessionState，也不决定 Tool 调度。
- 计划项状态保持 `pending → in_progress → completed`，同一时刻最多一个 `in_progress`。

### 10.7 `/plan` Plan Mode

`/plan` 仍使用 RegularTask、`run_turn`、ModelClientSession、ContextManager 和 Event/Rollout 主链，只改变 Collaboration Mode，并在创建 TurnContext 时冻结该模式；随后每次 capture StepContext 时使用同一 frozen mode 选择 Plan instructions 与 Tool policy。

Plan Mode 是持续的 Thread/Session collaboration setting，不由用户自然语言、Tool Call 或最终方案自动退出。用户在 Plan Mode 中要求“直接实现”时，模型仍只规划如何实现；只有显式 Default mode settings update 才会让后续 Turn 进入执行模式。`/plan` 不启动 Turn，`/plan <task>` 使用携带 Plan mode override 的单个 `UserInputOp` 原子应用设置并启动 Turn。

Plan Mode 的语义：

- 允许读取、搜索、Web/MCP 只读发现和分析项目。
- 禁止 `edit`、`write`、`write_stdin` 和有副作用 MCP Tool。基础版继续隐藏 `execute_command`，直到能够可靠约束非修改性命令；这是 Amadeus 安全能力差异，不改变 Collaboration Mode 架构。
- `update_plan` 是普通执行模式的 checklist Tool，在 Plan Mode 中不暴露，避免把显式方案与执行进度软计划混为同一事实。
- `request_user_input` 在 Plan Mode 中与 Default Mode 一样可用；模型应先探索可发现事实，再用它澄清无法从环境推导的需求、偏好和关键取舍，并在收到答案后于同一 Turn 继续规划。
- 普通 Assistant 文本不承担结构化多选提问协议；需要等待结构化回答时必须调用 `request_user_input`，避免结束当前 Turn 后再以自由文本猜测回答映射。
- Plan Prompt 使用 Codex 的 conversational 三阶段：先通过非修改性探索 Ground in the environment，再完成 Intent chat，最后完成 Implementation chat；只有目标、范围、约束、接口、数据流、失败行为、测试和必要迁移均 decision-complete 后才能正式给出方案。
- 正式方案必须放在且每 Turn 最多一个 `<proposed_plan>...</proposed_plan>` block 中。普通说明文本仍是 AgentMessage；block 内容由 Runtime stream parser 分流成 `PlanDeltaEvent` 与 completed Plan TurnItem。
- 后续修订必须输出完整替换方案，不维护 Plan revision 或增量 Patch；未决 parser 和 streamed Delta 是 transient，completed Plan TurnItem 才是 Replay 事实。
- 用户开始实施时创建后续 Default Turn，由同一 `run_turn` 根据 canonical history 执行；不得在 Plan Turn 尾部直接实施，也不得增加 `EnterPlanMode`/`ExitPlanMode` Tool。

Plan 输出生命周期：

```text
assistant output in Plan Mode
→ ProposedPlanStreamParser
→ normal text: AgentMessageContentDeltaEvent / AssistantMessageItem
→ <proposed_plan>: ItemStartedEvent(Plan)
→ PlanDeltaEvent*
→ ItemCompletedEvent(Plan{text})
→ TurnCompleteEvent
→ optional TUI implementation prompt
```

原始 Assistant ResponseItem 仍按正常模型历史进入 canonical Rollout；completed Plan TurnItem 用于 Event/TUI/Replay。二者分别承担模型 continuation history 与完成态界面投影职责。

### 10.8 Completion、Blocked、Failure 与 Interruption

- 模型返回最终回答时 TaskOutcome 为 completed。
- 明确预算耗尽、必需外部输入不可获得或模型确认无法继续时可以返回 typed blocked；blocked 是正常 Turn outcome，不通过 Go error 表达。
- Provider、Context、Persistence 或 Session 不可恢复错误使 Turn 以 `TurnCompleteEvent{status: failed}` 结束。
- 用户中断或 Session shutdown 取消 Turn Context，Session 以 `TurnAbortedEvent` 收尾。
- 中断时正在执行的 Tool 应尽力取消，并为已经 canonical 记录的 Tool Call 补齐 cancelled Tool Result 或 interruption marker。
- 用户随后输入“继续”时创建新 Turn；Context 提供上次中断事实，由模型重新评估，不恢复旧 Model Step、goroutine 或 Tool future。

canonical terminal 映射固定为：

- `TaskOutcomeCompleted` → `TurnCompleteEvent{status: completed, outcome: completed}`。
- `TaskOutcomeBlocked` → `TurnCompleteEvent{status: completed, outcome: blocked, summary/reason}`。
- `run_turn` error → `TurnCompleteEvent{status: failed, outcome: failed, error}`。
- Turn Context cancellation with abort cause → `TurnAbortedEvent`。

`TurnCompleteEvent` 的 typed payload 包含 `outcome` 与可选 `reason`；不得再用自定义 `OutcomeError` 把 blocked、limit 或正常 partial completion 伪装成 failed。

## 11. Runtime Coordination Tools

### 11.1 `update_plan`

`update_plan` 是 Codex 风格的 TODO/checklist Runtime Tool。Codex 中对应实现负责参数解析、Plan Mode 可用性约束、发送 `PlanUpdate` Event 和返回 concise Tool Output；Amadeus 由现有 `ToolDefinition` 的 `ValidateInput`、`Prepare` 和 `Execute` 承担这些职责。

```go
type StepStatus string

const (
    StepStatusPending    StepStatus = "pending"
    StepStatusInProgress StepStatus = "in_progress"
    StepStatusCompleted  StepStatus = "completed"
)

type PlanItemArg struct {
    Step   string     `json:"step"`
    Status StepStatus `json:"status"`
}

type UpdatePlanArgs struct {
    Explanation *string       `json:"explanation,omitempty"`
    Plan        []PlanItemArg `json:"plan"`
}

type PlanUpdateEvent struct {
    ThreadID ThreadID
    TurnID   TurnID
    UpdatePlanArgs
}
```

`PlanUpdateEvent` 与 Codex 的 `EventMsg::PlanUpdate(UpdatePlanArgs)` 同构表达 checklist 内容；`ThreadID` 和 `TurnID` 是 Amadeus Event scope 的统一字段。它不携带 `ItemID`、revision、更新时间或 Session snapshot，也不转换成 `ItemPlan`。

目标调用链固定为：

```text
Model Function Call(update_plan)
→ PersistAssistantResponse 持久化 Tool Call
→ ToolExecutionService Normalize / Validate / Prepare
→ UpdatePlan.Execute
→ EventSink.Publish(transient PlanUpdateEvent)
→ ToolResult("Plan updated")
→ 持久化 ResponseToolResult
→ 下一 Model Step
```

`update_plan` Contract：

- 输入 Schema 只使用 `plan` 和可选 `explanation`，不接受 `items`、`todos` 或其他等价字段。
- `ValidateInput` 校验状态值、trim 后的空步骤和“至多一个 `in_progress`”；空 `plan` 合法，可用于清空当前 checklist。基础 Contract 不增加 Codex 没有的固定最大条目数。
- `Prepare` 只生成 immutable `UpdatePlanArgs` prepared state 和 Allow permission，不创建 Snapshot、revision 或持久化回调。
- `Execute` 只校验 Turn scope、发布一次 `PlanUpdateEvent` 并返回严格的 `Plan updated`；不调用 Session capability。
- 不需要用户 Approval，也不经过文件权限检查。
- `PlanUpdateEvent` 是 transient Event，不写入 canonical JSONL Rollout、不进入 ContextManager、不投影 Developer Message，也不在 Resume 时恢复或重放。
- 原始 Function Call 参数和 Function Call Output `Plan updated` 按普通 ResponseItem 持久化，作为模型后续步骤看到该次计划调用的唯一历史来源；不额外注入“当前计划”消息。
- `update_plan` 不生成普通 Tool `ItemStartedEvent` 或 `ItemCompletedEvent`；Tool lifecycle 仍必须持久化模型可见的 `ResponseToolResult`，用户展示完全由 `PlanUpdateEvent` 驱动。
- Plan Mode 继续在 request-scoped ToolRouter 中隐藏 `update_plan`。基础版不为复制 Codex Handler 内的二次检查而把 Mode 注入整个 ToolUseContext；如未来增加其他非模型直接调用入口，再在统一执行策略层补充防御。
- Tool 默认注册并可用；基础版不以 `PlanUpdater != nil` 作为隐式 feature gate。Codex 风格的独立配置开关可以后置，不阻塞 lifecycle 对齐。

通用 Tool lifecycle observer 对 `update_plan` 只提交 `ResponseToolResult`，不提交普通 Tool TurnItem。基础阶段允许在 observer 的单一策略函数中显式识别 dedicated-event Tool，避免把 UI lifecycle 字段加入模型可见 `ToolSpec`；当第二个同类 Tool 出现时，再将该策略提升为 Registry metadata。TUI、Inline 和 Replay 不得继续通过多处分散的 Tool 名称特判隐藏重复 activity。

### 11.2 `request_user_input`

`request_user_input` 是 Codex 风格的结构化用户输入 Tool，在 Default 与 Plan Mode 中都直接暴露。它用于收集无法从仓库、运行环境或既有对话推导的需求、偏好和实现选择；它不是 Approval、Permission、计划批准或普通 User Message 的别名。架构、命名和生命周期以 Codex 为准，问题选择、多选和 Other 的基础交互体验可以参考 Claude Code `AskUserQuestion`，但不复制其 permission `updatedInput` 注入方式。

基础输入 Contract：

```go
type RequestUserInputOption struct {
    Label       string `json:"label"`
    Description string `json:"description"`
}

type RequestUserInputQuestion struct {
    ID          string                   `json:"id"`
    Header      string                   `json:"header"`
    Question    string                   `json:"question"`
    MultiSelect bool                     `json:"multi_select,omitempty"`
    Options     []RequestUserInputOption `json:"options"`
}

type RequestUserInputArgs struct {
    Questions []RequestUserInputQuestion `json:"questions"`
}

type RequestUserInputAnswer struct {
    Values []string `json:"values"`
    Other  string   `json:"other,omitempty"`
}

type RequestUserInputResponse struct {
    Answers map[string]RequestUserInputAnswer `json:"answers"`
}
```

Contract 约束：

- 一次调用包含 1–3 个问题；每题包含稳定的 `snake_case` ID、简短 Header、单句 Question 和 2–3 个选项。
- 回答按稳定 Question ID 映射，不使用可变化、可本地化的 Question 文本作为 key。
- `multi_select` 默认 false；启用时 `Values` 可以包含多个 option label。TUI 自动提供 Other/free-form，不要求模型伪造 Other option。
- 推荐项放在第一项，并在 Label 中标记 `(Recommended)`；Description 只解释影响或取舍，不重复 Label。
- 第一版不增加 Claude Code 的 Markdown/HTML Preview、图片答案、annotations 或 Plan interview 产品逻辑。
- Tool 在 Default 与 Plan Mode 中使用同一 Schema 和 ToolResult；Mode 只影响 Prompt 使用建议，不改变回答协议。
- Default Prompt 要求优先探索与合理假设，只有无法发现且错误假设风险较高时才询问；Plan Prompt 强烈优先使用该 Tool 锁定会实质改变方案的意图、取舍和实现决策。
- Tool 调用总是阻塞当前 Tool future，直到用户回答、拒绝、Turn 取消或 Session shutdown；基础版不向模型暴露可控的 `is_blocking` 或 timeout 字段。
- Tool 仅允许 root Session/主 Agent 调用；未来引入 Sub-agent 后由根 Agent 汇总问题，避免多个 Agent 并发占用交互界面。
- `SupportsParallelToolCalls` 返回 false，同一 Model Step 中不得与其他用户交互或副作用 Tool 并发执行。

协议对象保持请求与回答来源分离：

```go
type RequestUserInputEvent struct {
    ThreadID  ThreadID
    TurnID    TurnID
    RequestID RequestID
    CallID    ToolCallID
    Questions []RequestUserInputQuestion
}

type UserInputAnswerOp struct {
    RequestID RequestID
    Response  RequestUserInputResponse
}
```

目标调用链固定为：

```text
Model Function Call(request_user_input)
→ PersistAssistantResponse 持久化 Tool Call
→ ToolExecutionService Normalize / Validate / Prepare
→ PermissionService Allow（不进入 Approval）
→ RequestUserInput.Execute
→ UserInputRequester.Request(RequestUserInputEvent)
→ Session 注册 ActiveTurn pending waiter 并发布 transient Event
→ Application/TUI 展示专用问题 Overlay
→ UserInputAnswerOp 作为 correlated Submission 返回 Session
→ Session 交付 RequestUserInputResponse 并唤醒 Tool
→ ToolResult(structured answers)
→ 持久化 ResponseToolResult
→ 同一 Turn 的下一 Model Step
```

职责边界：

- `ValidateInput` 校验问题数量、ID 唯一性、字段长度、选项数量、选项 Label 唯一性和 `multi_select` 字段；不接触 TUI。
- `Prepare` 只构造 immutable `RequestUserInputArgs` 和 Allow permission，不创建 ApprovalRequest，也不把答案字段预留到模型原始输入中。
- `Execute` 通过 `ToolUseContext.Interactions` 暴露的窄 `UserInputRequester` 接口发起请求；不得直接引用 Session、Application 或 TUI。
- Session 是 pending waiter 的唯一 owner；Application/TUI 只展示 Event 并提交 Answer Op，不持有 Tool future。
- 用户拒绝回答或取消 Dialog 时，Session 以 typed cancellation/decline 结束 waiter，Tool 返回模型可见错误；不得转换成 Approval decline、Permission deny 或 grant。
- `RequestUserInputEvent` 和未决 waiter 是 transient runtime 状态，不写入 canonical Rollout；模型 Function Call 与最终 ResponseToolResult 按普通 Tool lifecycle 持久化，回答因此进入后续 Context。
- CLI/headless/SDK 必须提供明确的 UserInputRequester adapter；没有交互能力时立即返回 typed unavailable error，不得永久等待 stdin 或静默选择默认项。

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
    Tools             []ToolSpec
    ParallelToolCalls bool
    OutputSchema      json.RawMessage
}
```

Prompt 构建主链固定为：

```text
ModelInfo.ModelMessages
→ BaseInstructions (resolved for current Personality)
+ ContextManager PromptSnapshot
+ StepContext.ToolRouter.Specs
+ TurnContext.OutputSchema
→ Prompt
→ ModelClientSession
```

各类内容只有一个所有者：

- BaseInstructions：稳定的 Coding Agent 身份、完成标准、沟通方式和跨 Tool 行为纪律，由当前 `ModelInfo.ModelMessages` 在 StepContext capture 时解析；不属于 Task 或 CLI Composition。
- Dynamic Context Updates：Developer Instructions、AGENTS.md、Environment、Collaboration Mode、Permission Profile、Skill 和 MCP 的当前事实，转换为 ResponseItem 后进入 ContextManager。
- Conversation：User、Assistant、ToolCall 和 ToolResult，由 ContextManager 从 canonical Rollout 投影和维护。
- Tool Guidance：Tool 名称、描述和 Input Schema 位于 Tool Spec；只有跨 Tool 纪律保留在 BaseInstructions。
- Output Schema：只由当前 TurnContext 提供，不写入静态 Prompt。

禁止把动态路径、当前权限、模型名或 Tool 列表硬编码进静态模板。

Session 在 capture StepContext 时解析动态环境、AGENTS.md、MCP 和 Tool capability，并把需要进入模型历史的变化转换为 typed Context Update EventMsg。Session 在 durable append 后将 canonical fact 增量 record 到 ContextManager；sampling request 再从 ContextManager、TurnContext 与 StepContext 生成同一次请求使用的 PromptSnapshot。Developer Instructions、AGENTS.md、Environment、Collaboration Mode、Permission Profile、Skills 和 MCP 在 immutable Prompt snapshot 中按稳定顺序投影。

动态 Context Update 使用稳定的 replace key。Session 初始化、Resume 或 SessionPermissionContext 变化时，下一次 capture StepContext 可以提交新的临时 Permission Context Update；该 Update 只描述当前 Session 能力，不把 grant 变成持久化权限。历史 Approval Decision 可以作为事实保留，但 Resume 时不会重新授予权限。

Prompt 资产位于 `internal/prompt`，按 ModelMessages Catalog、Context/WorldState、Tool Guidance 和 Compaction Prompt 分层；解析后的 `llm.ModelMessages` 属于 LLM domain contract。

### 12.1.1 Codex Prompt 数据模型与所有权

Prompt 构造必须与 Codex 的数据模型和职责划分同构，不为旧 Amadeus Prompt 实现保留兼容入口：

```go
type BaseInstructions struct {
    Text string
}

type Prompt struct {
    Input              []ResponseItem
    Tools              []ToolSpec
    ParallelToolCalls  bool
    BaseInstructions   BaseInstructions
    OutputSchema       json.RawMessage
    OutputSchemaStrict bool
}
```

对应 Codex 参考实现为 `codex-rs/core/src/client_common.rs` 的 `Prompt`、`codex-rs/protocol/src/models.rs` 的 `BaseInstructions` 和 `ResponseItem`。Amadeus 的 Go 类型可以位于 `internal/llm`，但必须保持以下职责边界：

- `BaseInstructions` 只承载模型级稳定指令，不承载当前目录、当前权限、当前 Tool 列表或当前 Turn 事实。
- `ModelMessages` 负责模型 instruction template、模板变量、模式消息、Compaction message、来源和 revision；默认 Prompt 必须由当前 ModelInfo 解析，而不是由 `RegularTask` 选择字符串。
- `WorldState` 负责可变运行事实的 Section 状态；每个 Section 通过 Codex 风格的 `ContextualUserFragment` 生成带稳定 marker 的 Context Update。
- `CollaborationModeState` 负责当前 `ModeKind`、模型和该模式的 Developer Instructions；Default 与 Plan 的模式文本由 `CollaborationModeMessages` 选择。
- `ContextManager` 只拥有 canonical `ResponseItem` 历史和 WorldState/Context Update 投影，不拥有模型 instruction template 或 Tool 执行状态；`PromptSnapshot` 同时记录 WorldState revision 和 Prompt revision。
- `StepContext` 只捕获本次请求的 immutable ToolSpec、ToolRouter snapshot、ModelInfo、PromptSnapshot 和 revision，不拼接第二套历史。
- `ModelClientSession` 只消费已完成的 `Prompt`，不负责决定 Prompt 内容或注入模式逻辑。

`llm.Message`、`llm.ToolDefinition`、`prompt.Assets`、按 `mode/toolNames` 拼接字符串的旧入口，以及由 `RegularTask`、`CompactTask` 或 Provider adapter 私自构造 Prompt 的路径都属于待删除的旧实现。迁移完成后不得通过 wrapper、fallback 或双写继续保留这些 owner；调用方必须一次性切换到 Codex 同构模型。

### 12.1.2 Codex 风格 Prompt 装配

普通 Turn 不拥有独立的 `RegularTaskPrompt`。它使用 Codex 风格的统一装配：

```text
ModelInfo.ModelMessages.instructions_template
→ BaseInstructions

WorldState Sections
├── collaboration_mode(default/execute)
├── permissions
├── environment
├── AGENTS.md
├── skills
└── MCP

Canonical ResponseItem History
+ Current ToolRouter ToolSpec Snapshot
+ TurnContext.OutputSchema
→ Prompt
```

`RegularTask` 只负责创建 Task、提交用户输入和调用 Session 内唯一 `run_turn`；它不得选择、拼接或缓存 Prompt。每次 Model Step 都必须重新 capture `StepContext`，由当前 WorldState、ContextManager 和 ToolRouter snapshot 生成 immutable Prompt。

Plan Mode 必须沿用同一条 Prompt/Context/`run_turn` 主链：

```text
CollaborationMode{Mode: Plan}
→ CollaborationModeState
→ CollaborationModeMessages.plan
→ StepContext ToolRouter policy
→ Prompt
```

Plan Prompt 由 Collaboration Mode 注入，ToolRouter policy 和 ToolExecutionService 仍然是最终安全边界；Prompt 只表达模型工作模式。Plan Prompt 必须包含 Codex 同义的模式持续规则、探索优先、Intent/Implementation chat、`request_user_input` 使用边界和 `<proposed_plan>` finalization contract。

### 12.1.3 Codex 风格 Compaction Prompt

Compaction 使用独立于普通 BaseInstructions 的 Codex Prompt Asset：

```text
codex-rs/prompts/templates/compact/prompt.md
+ Covered Canonical ResponseItems
→ Summary

codex-rs/prompts/templates/compact/summary_prefix.md
+ Summary
→ Replacement History
```

Amadeus 必须迁移 Codex 的 `SUMMARIZATION_PROMPT` 和 `SUMMARY_PREFIX` 语义与结构，保留自身 `rollout.Compaction`、SourceHash、CoveredThroughSequence 和原位 ContextManager rebuild Contract。Compact 请求不得调用 Tool，不得使用普通 Turn 的 CollaborationMode/Tool Guidance 代替 Compaction Prompt，也不得把摘要当成任务完成证明。

Compaction Summary 至少保留：当前目标、关键决策、约束和用户偏好、已完成进度、重要文件/数据/结果、未完成事项、下一步和继续任务所需的关键引用。`CompactTask` 只调用 SessionServices 的 Compactor，不拥有第二套历史投影或 Prompt 装配逻辑。

### 12.1.4 Tool Prompt 来源与装配边界

Tool Prompt 的来源按 Tool 所属 Contract 固定，不在迁移时混合来源：

| Tool | Prompt 主要来源 | Amadeus 边界 |
|---|---|---|
| `read` | Claude Code `FileReadTool` | 只声明 Amadeus 实际支持的路径、媒体和输出能力 |
| `edit` | Claude Code `FileEditTool` | 保留 Read-before-write、唯一匹配、`replace_all`、缩进和路径规则 |
| `write` | Claude Code `FileWriteTool` | 保留已有文件先读、创建/完整重写与 Edit 优先规则 |
| `glob` | Claude Code `GlobTool` | 文件发现与实际排序/路径 Contract 一致 |
| `grep` | Claude Code `GrepTool` | 专用搜索、正则、过滤、输出模式和多行规则一致 |
| `update_plan` | Codex Plan Tool | `Plan updated` concise result、transient PlanUpdate Event 和软 checklist 语义一致 |
| `write_stdin` | Codex unified exec | `process_id`、`origin_call_id`、轮询、取消、输出预算和 Approval 复用一致 |
| `execute_command` | Codex unified exec + Amadeus | 以 Amadeus ProcessManager、宿主执行和 Approval Contract 为准 |

Tool Spec 负责模型可见的名称、参数 Schema 和短描述；较长的使用指导作为当前可见 Tool 对应的 Prompt Fragment/Developer Context 注入。Tool Prompt 不得声明 Amadeus 没有实现的能力，也不得取代 ToolExecutionService 的 Validate、Prepare、Permission、Approval 和 Execute 约束。

Prompt 装配必须只存在一条生产主链：`ModelMessages/WorldState/ContextManager/StepContext → Prompt`。旧 `Assets.Base`、`Assets.Compaction`、`DeveloperInstructions(mode, toolNames)` 或相似的字符串拼接接口必须在迁移时删除，而不是继续作为兼容层包裹新实现。

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

目录作用域必须接入真实 Tool target，而不只在 Turn 开始时对初始 CWD 解析一次：

- `AgentsMdManager` 是 AGENTS.md discovery、作用域解析、缓存和 revision 的唯一 owner；输出使用 `LoadedAgentsMd`。
- 每次 capture StepContext 时，Session 根据当前 CWD、已知目标路径和 AgentsMd revision 得到完整 `LoadedAgentsMd`，并把实际新增的指令作为 typed Context Update 增量记录。
- Read/Search 发现新的目录作用域后，可以让下一 Model Step 重新 capture；Edit/Write 和带目标 CWD 的 Command 在 Prepare 阶段校验其目标仍受 StepContext 中已加载的 AgentsMd snapshot 约束。
- snapshot 过期时返回 typed stale result，由下一 Model Step 重新 capture；不使用 `MarkSampled`、`context_refresh_required` 或旧 Resolver callback 驱动第二条 instruction 主链。
- Tool 不直接修改 ContextManager；它只返回目标与 stale 事实，Session 决定是否记录新的 AgentsMd Context Update。

### 12.3 ContextManager

ContextManager 属于 SessionState，是当前模型可见历史的唯一所有者：

```text
Initial Rollout Replay
→ SessionState.History (ContextManager)
→ Runtime Incremental Record
→ Snapshot(ModelInfo, PromptShape)
→ immutable PromptSnapshot
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

ContextManager 是活动 Session 的内存 history owner；canonical Rollout 是 durable source。Resume 时从 Rollout 重建一次，运行期间由 Session 在 durable append 成功后对同一 typed fact 执行增量 record。`run_turn`、TUI、CLI 和 CompactTask 不得各自实现第二套历史裁剪或消息投影。

只有 Session 可以提交 ContextManager mutation。Task/`run_turn` 通过 Session typed methods 请求 canonical append，并在 sampling request 构建时取得 immutable `PromptSnapshot`；不得持有 `*ContextManager` 或调用无 Rollout 对应事实的 Record/Replace fallback。正常 append 不得读取全部 `[]RolloutLine` 再 rebuild；全量 rebuild 只用于 Resume、显式 Compaction replacement 和测试校验。增量 record 与 Resume projector 必须共享同一 typed normalization 规则并通过 semantic-equivalence 测试。

### 12.4 Token Accounting

Token 预算来源按优先级处理：

1. Provider 返回的真实 usage，是已完成请求的权威统计。
2. 发送请求前使用模型 tokenizer；未实现专属 tokenizer 时使用保守 TokenEstimator。

TokenEstimator 只用于发送前容量预估，不得覆盖 Provider 已返回的真实 Usage。UI 展示估算值时必须标记为 estimated。

ModelInfo 至少保存：

```text
context_window
auto_compact_token_limit
truncation_policy
supports_parallel_tool_calls
input_modalities
```

- `context_window` 是当前 ModelInfo 的模型硬上限；用户配置 `model_context_window` 是 Runtime/Model override，不属于 Provider transport。
- `auto_compact_token_limit` 默认取 context window 的 90%；用户配置 `model_auto_compact_token_limit` 只能进一步收紧，不得扩大默认安全窗口。
- `truncation_policy` 是模型可见 Tool/Function Output 的投影策略；顶层 `tool_output_token_limit` 覆盖其 token limit，默认 `10000`。
- 普通 sampling 和 Compaction Request 均不提供用户可配置的 `temperature` 或 `max_output_tokens`，Adapter 省略对应 wire 参数并使用模型厂商默认值。
- Provider Usage 只作为已完成请求的权威统计；下一次请求容量判断始终重新估算当前完整 Prompt，避免把上次请求 Usage 当成不同 Prompt 的容量值。
- ContextManager 中的 live Usage、canonical TokenCountEvent 和 Resume 重建使用同一累计语义；单次 Model Step usage 只能先累加到 Turn/Thread usage，再更新投影，不能覆盖前序 Model Step。
- 完整 Prompt 估算包含 BaseInstructions、ContextManager 输入、模型可见 Tool Specs 与 OutputSchema。
- `base_tokens_remaining = min(auto_compact_token_limit - active_context_tokens, context_window - active_context_tokens)`。
- 自动压缩阈值负责为模型输出、系统开销和协议开销预留 headroom；硬 Context Window 判断只比较当前 active/estimated input 与 `context_window`，不得重新引入用户配置的最大模型输出预算。
- Token Budget 不按 System、Instructions、History、Tools 或 Resources 设置固定百分比分区。

### 12.5 Tool Result Projection

大输出不能原样无限进入模型：

- 保留 Tool 名、状态、关键摘要和输出边界。
- 文件读取保留相关片段、行号和截断信息。
- 文件修改保留 Typed `FileChangeResult` 的操作、路径、统计、Diff 摘要和最终状态；模型投影不能丢失 declined、stale 或 partial failure。
- Shell 保留命令、退出码、关键 stdout/stderr 和截断信息。
- 搜索保留匹配路径、行号和总匹配数。
- canonical Rollout 保存完整原始 Tool Result；`ContextManager.Snapshot` 只返回模型安全投影。
- `tool_output_token_limit` 只限制进入模型上下文的 Tool/Function Output，默认 `10000`；它不限制终端展示、canonical Rollout 原始结果、模型输出 token，也不等同于 `execute_command` 调用参数中的 `max_output_tokens`。
- 投影失败必须产生显式 Context Error，不允许静默丢失。

Tool Result 不保留独立的即时 replay 历史：Tool Call/Result 先 canonical append，Session 随后增量 record 到 ContextManager；`run_turn` 的下一次 sampling request 与 Resume 使用同一 normalization/projector 语义生成 `PromptSnapshot`。唯一 typed projector 的模型可见 payload 至少稳定表达 `ok/status`、文本或 parts、error、partial/truncated 和允许暴露的 metadata；任何阶段不得只取 `Text/Parts` 而静默丢失 declined、failed、cancelled、stale 或 partial 语义。完整 canonical result 与受预算约束的模型投影可以不同，但差异必须由同一 projector 显式产生并有 round-trip/semantic-equivalence 测试。

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
- 新 Turn 首次采样前，以及 Tool Result 后准备 capture 下一 StepContext 前，都检查自动压缩阈值。
- 原始 RolloutItem 不删除。
- Replacement History 只改变后续 ContextManager 模型投影。
- Summary 必须保留用户目标、已修改文件、Tool 结果、失败、未完成事项和关键决策。
- ToolCall/ToolResult 配对不能被压缩成协议非法历史。
- D 工作流通过统一 EventMsg/TurnItem 投影压缩开始、完成和失败；CompactTask 本身只返回 canonical 结果与错误，不另建第二事件通道。
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
- model selection 与 Provider 支持的显式请求控制。
- reasoning 与 usage。

通用 reasoning request control 只表达模型推理强度，不抽象厂商特有的 thinking 开关或历史清理策略：

```go
type ReasoningEffort string

const (
    ReasoningEffortNone    ReasoningEffort = "none"
    ReasoningEffortMinimal ReasoningEffort = "minimal"
    ReasoningEffortLow     ReasoningEffort = "low"
    ReasoningEffortMedium  ReasoningEffort = "medium"
    ReasoningEffortHigh    ReasoningEffort = "high"
    ReasoningEffortXHigh   ReasoningEffort = "xhigh"
    ReasoningEffortMax     ReasoningEffort = "max"
)

type ReasoningConfig struct {
    Effort *ReasoningEffort
}
```

`ReasoningConfig` 不保留通用 `Enabled` 或 `Preserve`。省略 `Effort` 表示不发送 reasoning control 并使用模型/Provider 默认值；`none` 是显式关闭 reasoning 的唯一通用语义。Reasoning history 是否回传、Provider 是否需要 `reasoning_content`、`thinking`、`enable_thinking` 或其他字段，属于 Dialect 的协议正确性，不是用户可配置的通用模型开关。

普通 Domain Request 不保存稳定的 `temperature` 或 `max_output_tokens` 字段。Amadeus 不用内部默认值伪装模型厂商默认值；Responses 与 Chat Completions Adapter 都必须在普通 sampling 和 Compaction 中省略对应 wire 参数。未来确有 Provider 必需扩展时，只能由经过契约测试的 Dialect/request extension 显式提供，不能重新变成所有 Provider 共用的用户配置。

### 13.2 OpenAI Adapter

`internal/llm/openai` 是 Adapter，不称为 Domain Client。

它负责：

- Responses API 请求转换和流聚合。
- Chat Completions 请求转换和流聚合。
- Provider Dialect 的最小兼容差异。
- Developer role 降级。
- Tool call argument 增量聚合。
- Provider Error、retryability、Retry-After、transport/idle timeout 和 Usage 归一化。

它不负责：

- Agent Loop。
- Prompt 业务规则。
- Tool 参数业务校验。
- Approval。
- Context Compaction。
- Turn-visible response stream retry counter、backoff lifecycle 或 TUI 状态。

### 13.3 Wire API

支持：

| 值 | 用途 |
|---|---|
| `responses` | OpenAI Responses API，默认优先 |
| `chat_completions` | OpenAI-compatible Chat Completions |

Provider transport 配置字段统一命名为 `wire_api`，内部类型与常量使用 `WireAPI` 术语，不保留 `api`/`APIMode` 作为新名称外壳下的旧主链。Amadeus 仍支持 Chat Completions，因此只对齐 Codex 的字段职责和命名，不照搬其当前仅保留 `responses` 的枚举范围。

Amadeus 没有内置 Provider preset。每个用户定义 Provider 省略 `wire_api` 时由配置归一化层得到默认值 `responses`；Base URL、Dialect 和认证仍由用户 Provider 定义。

### 13.4 Provider Dialect

Dialect 只处理经过验证的协议差异，不根据域名猜测：

- `openai`
- `deepseek`
- `glm`
- `qwen`
- `standard`

`standard` 表示未知的 OpenAI-compatible Provider，而不是明确缺少某项能力。用户显式配置标准 wire 参数时，Adapter 按所选 Wire API 透传，由上游 endpoint/model 判断是否支持；未配置时不得主动发送实验字段。`openai`、`deepseek`、`glm` 和 `qwen` 只处理已经由官方协议或契约 fixture 验证的差异，不通过模型名或域名猜测。

具体差异必须由契约测试覆盖，包括 role 支持、reasoning/effort 字段、thinking history、tool call delta 和 usage。单一 `SupportsReasoningEffort bool` 无法表达 `standard` 的 unknown 状态；基础版不增加该布尔 capability。未来 UI 或 Model Catalog 必须展示 effort capability 时，使用 supported/unsupported/unknown 三态或模型声明的 supported levels。

### 13.5 Model 与 Provider 配置所有权

配置模型向 Codex 的概念、命名和职责划分收敛，目标稳定形态为：

```yaml
version: 2

model: provider-model
model_provider: compatible
model_context_window: 128000
model_reasoning_effort: high
# 省略时从 model_context_window 推导 90%
# model_auto_compact_token_limit: 115200
tool_output_token_limit: 10000

model_providers:
  compatible:
    wire_api: chat_completions
    dialect: standard
    api_key: "${COMPATIBLE_API_KEY}"
    base_url: https://provider.example/v1
    timeout: 120s
    request_max_retries: 4
    stream_max_retries: 5
    stream_idle_timeout: 5m
```

所有权固定如下：

- `model`、`model_context_window`、`model_reasoning_effort`、`model_auto_compact_token_limit` 和 `tool_output_token_limit` 属于当前 Model/Runtime 配置，不进入 `ModelProviderInfo`。
- `model_provider` 选择 `model_providers` 中的用户定义 Provider；Provider 只保存 transport、auth、wire API、Dialect、timeout、retry 和 capability。
- `model_context_window` 在 Amadeus 尚无可信 Model Catalog 时必须显式为正数；不得为任意未知模型伪造统一的 128K Context Window 默认值。
- `model_auto_compact_token_limit` 可省略，省略时取 Context Window 的 90%；显式值必须大于 0 且不超过该派生上限。
- `tool_output_token_limit` 是顶层可配置项，默认 `10000`，覆盖 ModelInfo 的 Tool Output truncation policy；不得放回单个 Provider。
- Codex 的 `model_auto_compact_token_limit_scope` 依赖 carried-prefix/body-after-prefix 计数模型。Amadeus 在实现对应 Context Window 生命周期前不暴露未接线的 scope 配置，当前固定采用 total active context 语义。
- `temperature` 和 `max_output_tokens` 从稳定配置、ProviderConfig、ModelInfo、SampleRequest 与普通 LLM Request 中删除；普通 sampling 和 Compaction 使用模型厂商默认参数。

#### 13.5.1 Model Reasoning Effort

`model_reasoning_effort` 对齐 Codex 的模型级 request control，但 Amadeus 在没有可信 Model Catalog 的基础版中不伪造模型默认值：

- 可配置值为 `none`、`minimal`、`low`、`medium`、`high`、`xhigh` 和 `max`；基础版不增加 Codex 内部的 `ultra` 或任意 custom effort。
- 省略字段表示使用模型/Provider 默认值，普通 sampling、手动 Compaction 和自动 Compaction 均不得补写 `medium`、`high` 或其他客户端默认值。
- `none` 表示显式关闭 reasoning；不增加 `model_reasoning_enabled`，也不允许 `enabled=false` 与非 `none` effort 形成冲突配置。
- Config 的 effective effort 在 Turn 启动时冻结到 `TurnContext`，再进入每次 `SampleRequest`、`llm.Request.Reasoning.Effort` 和 Compaction request；同一 Turn 的 continuation 不从可变全局配置重新读取。
- `TurnContextItem` 持久化该 Turn 的 effective effort，用于诊断、Replay 和恢复语义；未配置时省略，不把 Provider 默认值伪装成已知事实。
- 当前字段是全局 Model/Runtime override，不是 per-mode override；因此不提前扩展 `CollaborationMode.Settings`。未来增加 `/effort`、Plan-specific effort 或动态模型设置时，再按 Codex 将 model/effort 纳入 CollaborationMode，并保持 TurnContext 冻结边界。

Effort 的 OpenAI-compatible 主线 wire shape 固定为：Responses 使用嵌套的 `reasoning.effort`，Chat Completions 使用顶层 `reasoning_effort`。这只是按 Wire API 选择的默认序列化形状，不代表任意 Provider 只要选择该 API 就必然支持对应字段；request builder 必须先按 Wire API 形成候选字段，再由 Dialect 按官方契约确认、覆写或拒绝。`standard` 允许显式 opt-in passthrough，`openai` 使用 typed field，其他已知 Dialect 不得绕过自身契约直接发送。

具体映射固定如下：

| Dialect / Wire API | 未配置 | 显式 effort |
|---|---|---|
| `standard` / Responses | 省略 | 透传 `reasoning.effort` |
| `standard` / Chat Completions | 省略 | 透传 `reasoning_effort` |
| `openai` / Responses | 省略 | 发送 `reasoning.effort` |
| `openai` / Chat Completions | 省略 | 发送 `reasoning_effort` |
| `deepseek` / Responses | 使用 Provider 默认 | 发送 Responses-compatible `reasoning.effort` |
| `deepseek` / Chat Completions | 使用 Provider 默认 | 发送 `reasoning_effort`；`none` 按官方 thinking toggle 映射为 disabled |
| `qwen` / Responses | 使用 Provider 默认 | 发送 `reasoning.effort` |
| `qwen` / Chat Completions | 使用 Provider 默认 | 非 `none` 发送 `reasoning_effort`；`none` 映射为 `enable_thinking=false` |
| `glm` / Chat Completions | 使用 Provider 默认 | 非 `none` 发送 `reasoning_effort`；`none` 映射为 `thinking.type=disabled` |

DeepSeek Dialect 不再固定为 Chat-only；在官方 Responses API 与 effort contract 已进入契约测试后支持 Responses。不得照搬 Claude Code 为未知自托管 endpoint 同时发送 `thinking`、`enable_thinking` 和 `chat_template_kwargs` 的广域兼容策略；Amadeus 已有 Dialect，必须只发送当前 Dialect 的官方字段。

Qwen 与 GLM 的当前官方 API 已定义 `reasoning_effort`；因此非 `none` 显式等级必须原样进入对应 wire field，不能再拒绝为 unsupported，也不能退化为只发送 `enable_thinking=true` 或 `thinking.type=enabled`。`none` 是通用领域中的显式关闭语义；Chat Completions 下，DeepSeek/GLM 将其映射为各自的 `thinking.type=disabled`，Qwen 映射为 `enable_thinking=false`，其余无需特殊关闭映射的 Dialect 仍发送 `reasoning_effort=none`；Responses 下统一发送 `reasoning.effort=none`。具体模型支持哪些非 `none` 等级属于 endpoint/model contract：Amadeus 基础版不维护按模型名分支的 support matrix，显式配置时发送官方字段，并保留上游对不支持模型或取值返回的 4xx。Reasoning history 与 `clear_thinking` 仍是独立的 Dialect 协议行为。

错误边界如下：

- 配置层只校验 effort 非空且属于稳定枚举，不根据模型名猜测 supported levels。
- `standard` 对显式 effort 采用 opt-in passthrough；上游不支持时保留 Provider 4xx 诊断，不在客户端静默忽略。
- 已知 Dialect/Wire API 只有在官方协议没有任何 effort 表达方式时才返回明确 unsupported error；只要协议定义了 effort 字段，就原样发送并由上游 endpoint/model 校验 supported levels，不能由客户端静默忽略或退化为“仅开启 thinking”。
- Reasoning history 的 `reasoning_content` 回传、GLM clear-thinking 等行为由 Dialect 自动维护，不暴露通用 `Preserve` 配置。

配置版本升级为 `2`。迁移规则只自动处理无歧义转换：`default_provider → model_provider`、`providers → model_providers`、Provider 内 `api → wire_api` 和旧 `max_retries → request_max_retries`。旧 Provider-local `model`、`context_window`、`auto_compact_token_limit`、`tool_output_max_tokens` 需要提升到顶层；存在多个 Provider 值或新旧字段同时存在时必须返回带准确路径的迁移错误，不能猜测、覆盖或静默丢弃。旧 `temperature` 与 `max_output_tokens` 返回明确 removed-field 诊断，提示其已改为模型厂商默认行为。

CLI、Environment、provenance、`config check`、`config explain/show`、redaction、example config 和 README 使用同一字段集合。目标命名至少包括 `model_provider`、`model_providers` 和 `wire_api`；不得只修改 YAML tag 而保留 `DefaultProvider`、`Providers`、`API` 等旧概念作为生产主模型。

### 13.6 Provider Retry 配置与错误 Contract

Amadeus 没有内置 Provider；每个 Provider 都由用户定义。因此这三个字段属于所有用户 Provider 共用的稳定配置 Contract，并由配置归一化层在用户省略时填入默认值，而不是依赖某个内置 Provider preset：

| 字段 | 默认值 | 校验与含义 |
|---|---:|---|
| `request_max_retries` | `4` | `0..100`；失败 HTTP request 的重试次数，不显示 `Reconnecting...` |
| `stream_max_retries` | `5` | `0..100`；已建立 response stream 中断后的重连次数 |
| `stream_idle_timeout` | `5m` | 必须大于 `0`；stream 连续无活动达到该时长后按可恢复断线处理 |

这三个字段就是当前 HTTP/SSE 基础版的完整连接恢复配置：严格按“次数”统计是 2 个 retry 字段，连同断流检测是 3 个字段。Codex 的第 4 个相关字段 `websocket_connect_timeout_ms` 只服务 WebSocket transport，不进入当前 Amadeus 配置、Prompt、`config show` 或 K 阶段验收。

不得继续用单一 `max_retries` 同时表达 SDK HTTP request retry 和 response stream reconnect。旧 `max_retries` 当前只接入 SDK request retry，因此配置迁移只能将显式旧值映射到 `request_max_retries`；`stream_max_retries` 始终使用自己的显式值或默认值 `5`，不能继承旧字段。迁移期可以给出 deprecation error/warning，但最终生产 schema、`config show`、patch/merge、validation 和 Adapter wiring 必须删除旧字段。

现有 `provider.timeout` 不属于 Codex 上述三个 retry/reconnect 字段。K 阶段必须审计并明确其 request timeout 语义；它不得作为整个活跃 streaming response 的固定 wall-clock deadline，从而在持续有 Delta 时抢先于 `stream_idle_timeout` 终止长响应。Composition Root 只负责装配，不把 retry policy 写进 TUI 或 Task。

`ProviderError` 至少稳定表达 error kind/code、用户安全 message、可选 additional details、retryable、可选 retry delay 和底层 request/provider identity。Adapter 负责归一化这些事实；Core 根据这些事实决定是否重连。错误字符串不能作为 retryability、Turn terminal 或 TUI 状态切换的判断依据。

## 14. Tool 架构

### 14.1 目标模型

Amadeus 保留 Codex 的 Session、Turn、StepContext、ToolRegistry、ToolRouter、Event 和 Rollout 边界，并将 Tool 内层执行协议实现为 Claude Code 风格的显式阶段。`ToolRouter` 是一次 Step 的 immutable 广告与路由计划；`ToolExecutionService` 是该 Router 选中 Tool 后唯一的内层执行编排入口，不再新增第二套执行 Service、Handler 总线或 Permission Engine。

```text
Model Tool Call
→ StepContext.ToolRouter.Route
→ Frozen Tool Handler / MCP Binding
→ NormalizeInput
→ ToolDefinition.ValidateInput
→ ToolDefinition.Prepare
→ PermissionService.Evaluate
→ Allow / Ask / Deny
→ ApprovalCoordinator（仅 Ask）
→ ToolDefinition.Execute(prepared)
→ Typed ToolResult
→ ToolDisplayResult / FileChangeItem
→ EventMsg / Canonical Rollout / ContextManager / TUI
```

该固定链描述普通 Permission/Approval Tool。`request_user_input` 的 Permission 结果始终为 Allow，并在 `Execute` 内通过窄 `UserInputRequester` 发起独立 interactive request；用户回答不是 ApprovalDecision，也不允许 Permission 阶段通过 `updatedInput` 重写模型原始参数。

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
    Interactions  UserInputRequester
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

`PreparedToolUse.State` 是 Tool 私有的、显式传递的准备态；核心链不得依赖 `context.WithValue` 注入准备数据。`ToolUseContext` 是一次 Tool Use 的运行时上下文，不等同于可持久化的 `TurnContext`；它可以引用 Session 内存状态，但不能把权限 grant、未决 Approval 或未决用户输入请求写入 Rollout。`Interactions` 只暴露 `RequestUserInput` 所需的窄接口，不暴露 Session、Application 或任意 Event 发布能力。

`ToolRouter` snapshot 同时保存模型可见 ToolSpec、确切 ToolDefinition handler、MCP binding、visibility 和 parallel flag；模型看到的 spec 与随后 dispatch 的 handler 必须来自同一 snapshot。执行阶段不得按工具名重新查询当前 mutable registry，也不得只用 `AllowedTools`/revision 字符串假装冻结 handler identity。

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
Approval Ask         需要用户选择，等待 ApprovalRequestEvent
Approval Decline     用户拒绝，零副作用并返回 declined ToolResult
Execution Failure    已获准执行但 Execute 失败
```

文件 Diff 由 `Prepare` 生成，`ApprovalCoordinator` 原样传递，ApprovalRequestEvent 保留其结构化数据，TUI 只展示并返回决定；Typed ToolResult 和 Rollout 保存最终事实。Tool 定义不能读取 TUI、解析键盘输入、直接更新 Session 权限上下文或自行发布审批交互。

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

`SessionPermissionContext` 由 SessionServices 持有并由 internal Session 管理；`PermissionService` 只读取它并返回 `PermissionDecision`，`ApprovalCoordinator` 只负责等待用户决定，最终由 Session 统一应用内存中的 `PermissionGrant`。Amadeus 不实现 Claude Code 的用户级、项目级或本地权限持久化；Session Close、进程退出和 Resume 后都恢复默认权限状态。
### 14.3 Tool 分类

```text
Read-only Tools
Structured Mutation Tools
Process Tools
Runtime Tools
Interactive Tools
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
request_user_input
```

按环境和能力条件暴露：

```text
write_stdin
view_image
read_skill
web_search
web_fetch
mcp_list_tools
mcp_call
mcp_list_resources
mcp_read_resource
```

模型可见 Tool Catalog 和默认 Core Registry 只包含当前公开工具：`read`、`edit`、`write`、`glob`、`grep`、`execute_command`、`write_stdin`、`update_plan`、`request_user_input`，以及按配置启用的条件工具。`request_user_input` 在 Default 与 Plan Mode 中都属于 direct core Tool，不以 Plan Mode 作为可见性 gate。旧 `read_file`、`list_dir`、`glob_files`、`grep_code`、`request_permissions` 已物理删除，不再提供兼容注册。

保留 `execute_command` 而不命名为 `Bash`，因为 Amadeus 面向多平台；Skill Script 统一通过它执行。MCP 初期保留 Codex 对齐的统一 Lazy Tool 边界 `mcp_list_tools/mcp_call`，不在每次模型请求中复制一套动态 Tool Registry；后续可在不改变 MCPRuntime 和 Tool Contract 的前提下增加动态 Tool Search/直接 ToolSpec 投影。

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
| `update_plan` | Codex `plan` tool/spec | `UpdatePlanArgs`/`PlanItemArg`/`StepStatus` 术语、至多一个 `in_progress`、发布 transient `PlanUpdateEvent`、持久化普通 Tool Call/Result、Tool 返回 `Plan updated` | Session 持久状态、revision 与 Resume checklist restore |
| `request_user_input` | Codex protocol/session lifecycle + Claude Code question UX | 独立 Request Event/Answer Op、Session pending waiter、稳定 Question ID、Default/Plan 通用、Other 与基础 multi-select、回答后同 Turn 继续 | Handler 抽象、Approval `updatedInput`、Question 文本 key、Preview/图片/annotations、ExitPlanMode 产品逻辑 |
| Web/MCP/Skill | Codex Registry + Amadeus Provider | 统一 ToolUse 生命周期、typed result、host/tool 最小 Grant、bounded output | 完整远端执行环境和持久化授权 |

“复刻核心语义”指按 Go 和 Amadeus Runtime 重写相同行为契约，而不是把 TypeScript/Rust 类型、UI 组件、Sandbox 或产品服务直接搬入。每个内置 Tool 必须使用同一 `ToolExecutionService`，但可以拥有不同的输入、准备态、权限策略和 typed result；不得为了形式统一而把所有 Tool 强塞进文件 Diff 或 Approval 流程。

内置 Tool 的结果至少分成三层：

```text
Execution Result
→ ToolResult.Data            给 Runtime 和模型的稳定事实
→ ToolDisplayResult          给 Event/TUI 的展示投影
→ ResponseItem/EventMsgItem  给 Rollout/Resume 的完成事实
```

`ToolDisplayResult` 不能反向成为执行事实，模型侧文本也不能作为 TUI 重新解析结构化结果的来源。只读 Tool 必须显式返回截断状态；文件 Tool 必须返回 `FileChangeResult`；进程 Tool 必须返回 `ProcessResult`；Runtime Tool 必须通过 Session capability 修改状态并发布对应 Event。

### 14.6 遗留 `apply_patch` 与 sandbox 隔离

`apply_patch` 的旧实现不是 Amadeus 内置 Tool，sandbox 也不是正式执行主链。生产代码中的旧注册、协议、handler、adapter、fixture 和兼容分支直接删除，仅允许本文保留历史说明：

- 不注册到默认 Core Registry，也不进入模型可见 Tool Catalog、Prompt Tool Snapshot 或 Skill/MCP Tool Projection。
- 不接入 `ToolExecutionService`、PermissionService、ApprovalCoordinator、EventMsg、Rollout 或 TUI 的正式链路。
- 不作为 `edit/write` 的底层执行器；结构化文件修改只通过 Claude Code 风格的 `Prepare → Approval → Revalidate → Atomic Apply` 实现。
- 不以 Codex `apply_patch` 的 Sandbox、Guardian、Remote Environment、Network Approval 或 Diff Tracker 反向塑造 Amadeus 当前 Tool Contract。
- 不维护遗留代码的可编译性；任何残留类型阻碍正式主链时直接删除。

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

`FileChangePreview` 是 ApprovalRequestEvent、TUI 和 FileChangeItem 之间的唯一结构化 Diff Contract。它不是最终写入结果，也不能由 TUI 根据文本重新解析；展示可以截断，但底层 Preview 必须保留完整的 Hash、统计和可滚动 Hunks。

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
- `web_search` 默认 Allow。
- MCP List 与 Resource Read 默认 Allow。

所有拒绝选项支持通过 Tab 添加反馈，底部统一显示：

```text
Esc to reject · Tab to add feedback
```

### 15.5 SessionPermissionContext 与默认规则

每个 SessionServices 只持有一个 `SessionPermissionContext`。它是进程内存中的运行时权限状态，不是数据库表，也不是 Rollout 历史的一部分。Session 创建时初始化，Session Close、进程退出或 Resume 一个历史 Session 时清空；Approval Request、文件内容、Diff 和用户反馈都不写入权限 Context。

Amadeus 不实现 Claude Code 的 `userSettings`、`projectSettings`、`localSettings` 或其他权限持久化来源，也不建立 Approval Persistence Store。`SessionPermissionContext` 只保存当前 Session 已获准的最小 Grant：

```go
type SessionPermissionContext struct {
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
→ TUI Local Action / AppEvent / Session Op
→ Application 或 Runtime owner
→ Event/EventMsg / typed AppEvent result
→ HistoryCell / TUI Projection
```

`cmd/amadeus` 只负责 CLI 参数、依赖装配和启动 TUI，不包含 Slash Command `switch`。不保留 Plain Controller、`CommandHandler`、`TaskHandler` 或第二套 Slash Command 执行路径；`--plain` 删除，不作为另一套交互运行时维护。

命令按最终动作分为三类，但分类只服务于分发实现，不引入额外的路由抽象：

| 类型 | 命令 | 执行方式 |
|---|---|---|
| TUI Local | `/copy` | 复制最近 Assistant 回复，不创建 Turn |
| Application Command/Query | `/resume`、`/skills`、`/rename`、`/delete`、`/status`、`/mcp`、`/clear`、`/exit` | 转换为 typed AppEvent，由 Application/Thread owner 执行并返回结构化结果 |
| Session/Turn Operation | `/compact`、`/plan` | `/compact` 提交 `CompactOp`；`/plan` 提交独立 settings update；`/plan <task>` 提交携带 Plan mode override 的单个 `UserInputOp` |

`/plan` 不直接修改 TUI 的本地模式变量，也不通过 callback 返回模拟 Session 已接受设置。Fullscreen TUI 通过 active Thread attachment 提交 `ThreadSettingsOp{CollaborationMode: Plan}`；Session 发布 typed `ThreadSettingsAppliedEvent` 后，TUI 才更新模式投影。带参数的 `/plan <task>` 不使用 `pendingModeTask`，严格遵循：

```text
SlashCommand::Plan(task)
→ UserInputOp{Content: task, ThreadSettings.CollaborationMode: Plan}
→ Session 串行接纳 Submission
→ apply settings
→ freeze TurnContext.CollaborationMode
→ optional ThreadSettingsAppliedEvent
→ start RegularTask/run_turn
```

快捷模式切换复用独立 `ThreadSettingsOp`/Event 生命周期；设置失败时保留原模式，不启动任务。ActiveTurn 运行期间的独立 settings update 必须拒绝或排队，不能改变当前 Turn。TUI 不保存 `pendingModeTask`，也不定义 `CollaborationExecute` 第二套 enum。旧 `FullscreenPermissionModeSetter` 和 `fullscreenPermissionModeDoneMsg` 不作为最终目标保留。

Slash Command 不是 EventMsg。命令执行引发的状态变化才通过 EventMsg、Rollout 和 TUI Projection 传播；纯 TUI 操作不写入 canonical history。

### 18.4 AppEvent 与命令生命周期

Slash Command 分发后的异步工作使用 Codex 同构的 typed AppEvent，不使用 `func(context.Context) (string, error)` 作为通用命令边界。字符串只允许存在于最终 HistoryCell 的展示字段中，不能承担 Thread attach、history replay、running state、empty state 或 typed inventory 的业务语义。

Fullscreen interactive mode 必须像 Codex `App` 一样持续拥有当前 Thread attachment 和事件路由，而不是让每次 `runTask` 或 Slash Command callback 临时调用 `waitTurn` 消费 `SessionIo`：

- 同一时刻只有一个 active Thread attachment；它是 `SessionIo.Events` 和 termination 的唯一消费者，并在内部从 EventMsg 派生 transcript/status read model。
- 普通用户输入与 `/compact` 只负责向 active `AmadeusThread` 提交 typed Op；Turn running、approval、completion 和 history 更新全部由 attachment event pump 送回 Bubble Tea AppEvent。
- Resume 成功后先停止旧 attachment 的转发，再原子安装新 attachment；带旧 ThreadID 或旧 attachment generation 的迟到消息必须被丢弃。
- `waitTurn` 只保留给 one-shot/非 Fullscreen CLI；Fullscreen 主链不得通过 `runOnce → waitTurn → EventSink` 形成第二套事件消费和 Turn 完成协议。
- Bubble Tea 后台 command 的完成只表示 Application request goroutine 已返回，不能表示 Turn 已完成；Turn 终态仍唯一来自 `TurnCompleteEvent`/`TurnAbortedEvent`。

目标 AppEvent 至少覆盖：

```text
OpenResumePicker
ResumeThread(thread_id)
ThreadAttached(ThreadViewSnapshot)
ThreadAttachFailed(error)

ClearUI(name)
SetThreadName(name)
ThreadNameUpdated(thread_id, name)
DeleteCurrentThread

FetchMCPInventory(detail, origin_thread_id)
MCPInventoryLoaded(inventory, detail, origin_thread_id)
MCPInventoryFailed(error, origin_thread_id)

RefreshStatusData(StatusRefreshOrigin)
StatusDataLoaded(origin, result)

OpenSkillsList
OpenManageSkills
SetSkillEnabled(path, enabled)
ManageSkillsClosed
ListSkills(force_reload)
SkillsLoaded(catalog)

Exit(ShutdownFirst | Immediate)
ShutdownComplete
```

这些名称表达 Codex 的职责和生命周期；在 Go/Bubble Tea 中可以按语言习惯拆成 request/result message，但不得重新退化为一个携带 `command/output/error` 的通用完成消息。Thread-scoped completion 必须携带 ThreadID，后台 query completion 必须携带 request ID 或 origin generation。

`ThreadViewSnapshot` 是 Application 向 TUI 提供的只读线程视图，不成为第二份历史 owner：

```go
type ThreadViewSnapshot struct {
    ThreadID      rollout.ThreadID
    Title         string
    Mode          turn.ModeKind
    Items         []protocol.TurnItem
    Usage         llm.Usage
    ContextWindow int64
}
```

`Items` 必须由目标 Thread 的 canonical rollout 投影得到；TUI 只能通过既有 `TurnItem → HistoryCell` replay 链渲染，不能直接解析 rollout，也不能复用切换前 Thread 的 `TranscriptState`。

#### `/resume`

`/resume` 是 Application 级 Thread attach/switch 生命周期，不是“修改当前 Session ID 后返回一条成功字符串”：

```text
SlashCommand::Resume
→ OpenResumePicker / ResumeThread
→ ThreadWorkspace 准备并恢复目标 AmadeusThread
→ 从目标 canonical rollout 构造 ThreadViewSnapshot
→ 原子替换 Application current Thread
→ BeginThreadSwitchHistoryReplay
→ TUI 重置旧 TranscriptState、HistoryCell、active item、usage 和 mode 投影
→ replay ThreadViewSnapshot.Items
→ EndThreadSwitchHistoryReplay
→ 重新接收目标 Thread 的 live Event
```

- 目标 Thread 恢复、history projection 或 TUI attach 失败时，当前 Thread 与当前 transcript 必须保持不变；不得先关闭当前 Thread 再尝试恢复目标 Thread。
- replay 必须按 canonical 顺序一次性缓冲并刷新，避免用户看到只切换 Session 标识、半段历史或逐条闪烁。
- Resume 只允许存在一个 application-owned `ProjectRolloutItems` projector，按 rollout sequence 投影 ResponseItem 和 EventMsgItem；不得把 completed-item projection、response items 等多个列表合并后按时间重新排序。
- canonical User ResponseItem 必须投影为 UserMessageItem，CompactedItem/ContextCompactedEvent 必须投影为 ContextCompactionItem；用户消息和压缩边界不能只存在于首次 live TUI 的本地插入中。
- replay 不重放 Working、Approval wait 或 Delta 动画；已完成项必须与首次启动时的 Resume projection 完全一致。
- 成功后可以追加轻量 Session lineage/notice，但 notice 不能替代历史恢复。
- 旧 `FullscreenSessionResumer func(...) (string, error)`、`fullscreenResumeMsg.message`、`replayTurnItems` 的 merge/sort 路径和只调用 `refreshCurrentSession()` 的完成路径必须删除。

#### `/compact`

`/compact` 是正式的 Session Op 和 `CompactTask`，其运行状态与终态只来自 Runtime Event：

```text
SlashCommand::Compact
→ TUI 立即进入 pending task UI
→ AmadeusThread.Submit(CompactOp)
→ TurnStartedEvent(kind=compact)
→ CompactTask
→ durable Compaction rollout item
→ ContextCompacted
→ Warning("Heads up: Long threads...")
→ TurnCompleteEvent / TurnAbortedEvent
→ TUI 离开 task running state
```

- `TurnStartedEvent` 必须携带稳定的 Turn/Task kind，使 TUI 显示 `Compacting context`，而不是把压缩伪装成普通用户 Turn 或仅修改不可见的 status 字符串。
- 提交 `CompactOp` 后 TUI 可以像 Codex 一样先设置 pending/running projection，消除事件往返前的空白；Runtime 的 `TurnStartedEvent`、`TurnCompleteEvent` 和 `TurnAbortedEvent` 仍是最终真相。
- `ContextCompacted` 必须在 compaction durable append 成功后发布，并固定投影为 Codex 同义的 `• Context compacted`；生成的 summary 只属于 durable replacement history/compaction payload，不得作为 Assistant 消息、Reason 文本或 TUI 详情泄露。
- `ContextCompactedEvent` 后必须立即发布独立的 typed `WarningEvent`，由黄色 `WarningHistoryCell` 显示 `⚠ Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted.`；该提示不是 compaction summary，也不并入 `TurnCompleteEvent` 文本。
- `/compact` 不通过 TUI callback 内部调用 `waitTurn`，不返回 `"Conversation compacted"` 字符串作为完成协议，也不维护独立 `fullscreenCompactMsg` 终态。
- Compact 期间的新输入遵循普通 task queue 规则，不创建仅对 Compact 生效的第二套输入状态机。

#### `/mcp`

`/mcp` 是 Application 级结构化 inventory query，不是拼接文本的同步 reader：

```text
SlashCommand::MCP
→ 立即提交洋红色 MCPCommandHistoryCell("/mcp")
→ status = loading MCP inventory
→ FetchMCPInventory(detail, origin_thread_id)
→ MCPInventoryLoaded / MCPInventoryFailed
→ MCPInventoryCell / EmptyMCPInventoryCell / ErrorCell
→ status = idle
```

- 主屏 terminal history 一经 `tea.Println` flush 即不可变，因此不得先打印一个 loading HistoryCell 再假装原位替换；in-flight 状态属于 status projection，正式 history 只记录 `/mcp` 命令与最终结果。
- MCP query 使用 `MCPServerStatus`、`MCPAuthStatus`、`MCPToolMetadata`、`MCPResourceMetadata` 和 detail enum 等 typed 数据；Application 通过 Session/Thread typed query 获取 redacted configuration/tool/resource catalog，排序和最终布局由 MCP HistoryCell renderer 负责，Application 不拼接终端文本。
- 默认输出必须采用 Codex 术语与层级：`🔌  MCP Tools`、Server、`Auth`、`Tools`；`/mcp verbose` 在同一 renderer 中增加 `Resources` 与 `Resource templates`，不得建立第二套字符串输出路径。
- 即使没有配置 Server 或没有可用 Tool，也必须提交明确结果：`No MCP servers configured.` 或 `No MCP tools available.`，不能静默结束。
- query/result 必须携带发起时的 `origin_thread_id`、attachment generation 和 request ID；用户在请求期间 Resume 到其他 Thread 时，迟到结果不得污染新 transcript。
- 旧 `FullscreenMCPReader func(...) (string, error)`、`writeInteractiveMCP` 的 TUI 主链用途以及通过通用 `fullscreenCommandDoneMsg` 展示 MCP 的路径必须删除。
- `/mcp verbose` 只改变 typed detail level，不建立第二套查询或 renderer。

#### `/clear`

`/clear` 与 Codex `ClearUi` 一样，是 Application-owned 的“清空当前 UI 并启动 fresh Thread”操作，不是清空一段本地 slice 后返回 `Started a new chat`：

```text
SlashCommand::Clear
→ ClearUI(name?)
→ 清除 pending history insertion
→ 清空 terminal viewport/scrollback
→ reset Transcript/App UI state
→ detach/shutdown 当前 live Thread attachment
→ StartFreshThread(source=clear)
→ 复用正常 Thread attach/configure lifecycle
→ 显示旧 Thread 的 resume hint/lineage（如适用）
```

- 清屏前必须先丢弃尚未 flush 的旧 history insertion，防止 `/clear` 后旧 transcript 行再次写回终端。
- reset 范围至少包括 HistoryCell、ActiveHistoryCell、deferred history、replay buffer、details store、pending status refresh、usage/mode projection 和旧 attachment generation；不能只调用 `resetHistory()`。
- 旧 Thread 不被删除或归档，canonical rollout 继续可 Resume；Application 只停止其 live event forwarding。
- fresh Thread 启动或 attach 失败时，在已清空的 UI 中显示 ErrorCell，并保留旧 Thread 可恢复性；不得返回或显示伪造的成功文案。
- 旧 `FullscreenClearer`、`fullscreenCommandDoneMsg` 中通过 `strings.HasPrefix(command, "/clear")` 分支执行重置的路径必须删除。

#### `/rename`

`/rename` 分为 TUI-local prompt 和 Thread-scoped typed command 两段，完成事实来自 Thread metadata notification：

```text
SlashCommand::Rename
→ Rename prompt / inline name validation
→ SetThreadName(name)
→ Application 路由到 active Thread metadata owner
→ durable metadata update
→ ThreadNameUpdated(thread_id, name)
→ matching ChatWidget/Header 更新名称
```

- prompt、空名称校验和 normalize 属于 TUI；持久化与当前 Thread 身份校验属于 Application/ThreadWorkspace。
- `ThreadNameUpdated` 必须携带 ThreadID；旧 Thread 或迟到 attachment 的更新不得修改当前 Header。
- 提交失败显示 ErrorCell，成功不依赖返回字符串或额外 Notice 才能更新 UI；Header/metadata 只根据 typed completion 更新。
- 旧 `FullscreenSessionRenamer func(...) (string, error)`、`fullscreenRenameMsg.message` 和直接调用 `refreshCurrentSession()` 的完成路径必须删除。

#### `/delete`

`/delete` 使用 TUI-local confirmation + Application-owned destructive lifecycle：

```text
SlashCommand::Delete
→ confirmation overlay
→ DeleteCurrentThread
→ ThreadWorkspace/ThreadStore durable delete
→ success: Exit(UserRequested)
→ failure: ErrorCell + Continue
```

- 取消 confirmation 不产生 AppEvent；confirmation overlay 绑定创建时的 attachment generation，Thread switch 必须关闭或失效旧 overlay，避免对错误 Thread 执行确认动作。
- `DeleteCurrentThread` 与 Codex 一样由串行 Application event loop 在处理时解析 active Thread；Application 在删除前验证目标仍允许删除，并协调 attachment shutdown、rollout close、metadata/rollout 删除顺序，删除只有一个 owner。
- durable delete 成功后直接进入 Application exit control，不先插入成功字符串再 `tea.Quit`；删除失败必须保留 TUI 可用并显示错误。
- 旧 `FullscreenSessionDeleter func(...) (string, error)`、`fullscreenDeleteMsg.message` 和 `tea.Sequence(flushHistory, tea.Quit)` 完成路径必须删除。

#### `/status`

`/status` 首先从 TUI/Application 已持有的 typed state 立即构造 Status HistoryCell；只有确实需要远程或延迟数据时才发起关联请求：

```text
SlashCommand::Status
→ Build StatusSnapshot from cached typed state
→ StatusHistoryCell(refreshing?, request_id?)
→ optional RefreshStatusData(StatusCommand(request_id))
→ StatusDataLoaded(StatusCommand(request_id), result)
→ 原位完成 matching StatusHistoryCell
```

`StatusSnapshot` 至少包含当前 ThreadID/名称、Model、Collaboration Mode、Token/Context usage、Turn phase 和 Provider identity；可选扩展数据保持 typed field，不拼接成 CLI 文本后再交给 TUI 解析。

- 本地状态卡必须立即可见，不能为了等待可选 Provider/account 数据让回车后无反馈。
- 每次 `/status` 使用独立 request ID 和 HistoryCell handle；并发请求只更新自己的卡片，迟到或未知 request ID 直接忽略。
- 异步刷新失败也必须结束该卡片的 refreshing 状态，并保留已显示的本地 snapshot；不能永久显示 loading，也不能用通用 command done message 追加第二张状态卡。
- 旧 `FullscreenStatusReader func(...) (string, error)`、`commandStatus() + output string` 拼接和 `/status` 对 `fullscreenCommandDoneMsg` 的依赖必须删除。

#### `/skills`

`/skills` 的菜单与列表交互属于 TUI，Skill catalog、配置写入和 refresh 属于 Application/Skill owner：

```text
SlashCommand::Skills
→ local Skills menu
→ OpenSkillsList / OpenManageSkills
→ list: 打开既有 Skill mention/selection surface
→ manage: render cached typed Skill catalog
→ SetSkillEnabled(path, enabled)
→ config owner durable write
→ success: update cached Skill state
→ failure: ErrorCell
→ ManageSkillsClosed
→ ListSkills(force_reload=true)
→ SkillsLoaded(catalog)
```

- Codex 的 `OpenSkillsList` 复用现有 mention selector，而不是生成一段列表文本；Amadeus 若尚无 mention surface，基础版可复用唯一 typed searchable Skill picker，但不得为 list 再建立字符串输出或第二份 catalog UI。
- Menu、搜索、选中和 toggle view 不进入 Runtime/Rollout；Skill identity 使用稳定 path/ID，不能只用可能重复的 display name。
- 空 catalog 必须显示明确的 `No skills available.`；list path 不得把 `[]SkillOption` 转成多行字符串交给通用 NoticeCell。
- enable/disable 只有 Application 配置 owner 可以持久化；成功后更新内存 catalog，失败显示 ErrorCell，关闭管理界面后执行一次 typed force refresh 消除 optimistic view 与真实配置的偏差。
- startup refresh 与用户触发 refresh 可以共享 `SkillsLoaded` 数据模型，但 origin/错误展示不同；迟到 refresh 必须按 cwd/catalog generation 校验。
- 旧 `FullscreenSkillLister`、`FullscreenSkillSetter`、`fullscreenSkillsMsg`、`fullscreenSkillSetMsg` 及 `/skills` 的字符串 builder 路径必须删除。

#### `/exit`

`/exit` 是 Application shutdown request，不是 ChatWidget 直接返回 `tea.Quit`：

```text
SlashCommand::Exit
→ Exit(ShutdownFirst)
→ 显示 shutdown in progress
→ 标记 pending shutdown target
→ shutdown/detach active Thread 与后台资源
→ rollout flush / child process cleanup
→ ShutdownComplete 或 bounded timeout
→ Application Exit(UserRequested)
→ tea.Quit
```

- `ShutdownFirst` 是用户主动退出的默认模式；pending shutdown target 用于阻止正常 Thread termination/failover 逻辑把退出误判为异常切换。
- shutdown 必须有 UI escape-hatch timeout，避免损坏的 Runtime 让退出永久卡住；超时可以记录 warning 后退出，但不能把 `Immediate` 当常规路径。
- `Immediate` 只用于 fatal error、shutdown 已完成后的最终跳出或明确的紧急逃生路径，允许跳过 flush 的风险必须在类型命名中可见。
- 旧 `/exit → tea.Quit` 直接路径必须删除；最终 `tea.Quit` 只能由 Application shutdown lifecycle 的终态触发。

#### `/copy`

`/copy` 与 Codex 一样保留为纯 TUI-local action：读取 Transcript 中最后一条 Assistant raw markdown，调用可注入的 clipboard backend，并插入 Info/Error HistoryCell 后 redraw。

- 没有 Assistant markdown 时显示 `No agent response to copy`；clipboard 失败显示具体 ErrorCell，成功显示轻量 InfoCell。
- `/copy` 不创建 AppEvent、Session Op、Turn、Rollout item 或 Application callback。为了形式统一而把它路由到 Runtime 属于错误的架构对齐。
- clipboard lease 等平台资源由 TUI/clipboard adapter 持有；Transcript 只保存 raw markdown，不从已渲染 ANSI 文本反向提取内容。

## 19. TUI

### 19.1 产品形态

默认使用 Bubble Tea + Lip Gloss 实现 Codex 风格 Rich Inline TUI：

- 保留 Amadeus Logo 和 `>_` 启动视觉。
- 历史内容尽量进入终端原生 scrollback。
- 鼠标默认保留终端选择文本能力。
- 输入运行期间仍可编辑；普通文本 Enter 始终提交 `UserInputOp`，由 Runtime admission 决定 Started、Steered 或 typed rejection，TUI 不依据 `running bool` 自行改写为下一 Turn。
- Amadeus 只维护这一套 Rich Inline 交互运行时；不提供 `--plain` 第二套输入、状态和事件路径。

运行中提交普通文本并获得 `Steered` 时，TUI 保持当前 Turn 的 elapsed timer、Working/activity state、details store 和 active item，不重新执行新 Turn 初始化，也不插入第二个 Worked boundary。获得 `Started` 时由后续 `TurnStartedEvent` 初始化新 Turn；获得 `ActiveTurnNotSteerable` 等 rejection 时恢复或保留 composer 内容并显示明确错误，不能假装提交成功后静默排队。

### 19.2 HistoryCell

TUI 使用结构化 HistoryCell，而不是拼接 ANSI 字符串。Runtime 业务项先经 `EventReducer` 投影为 TranscriptState，再由 Cell 渲染：

```text
Event/EventMsg
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
- ProposedPlanCell
- UpdatedPlanCell
- ApprovalCell
- RequestUserInputCell
- DiffCell
- ContextCompactedCell
- WarningHistoryCell
- MCPCommandHistoryCell
- MCPInventoryCell
- EmptyMCPInventoryCell
- StatusHistoryCell
- ErrorCell
- WorkedSeparatorCell

`ActiveHistoryCell` 按 `ItemID` 保存进行中状态；Delta 只能更新对应 Item，不能依赖“当前最后一个 Cell”猜测归属。`ItemCompletedEvent` 完成对应 Active Cell 并只提交一次正式 HistoryCell；如果 Resume 只有持久化的 ItemCompletedEvent，则 Reducer 直接合成正式 Cell。

UserMessage 使用 `ClientUserMessageID` 支持 optimistic projection：TUI 可在提交时立即插入 pending UserMessageCell；Runtime canonical record 后发布带同一 client ID 的 completed UserMessage Item，Reducer 将其确认或去重。初始输入与 steered input 使用相同映射；未知 client ID 的 Runtime UserMessage 仍按正常 completed Item 渲染，以支持其他 caller、Resume 和未来远程输入。

实时与恢复使用同一 `TurnItem → HistoryCell` 映射：

```text
Live:   ItemStartedEvent → Delta → ItemCompletedEvent → HistoryCell
Replay: EventMsgItem(ItemCompletedEvent)               → HistoryCell
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

- `TurnStartedEvent` 进入 Working。
- `TurnCompleteEvent` 或 `TurnAbortedEvent` 离开 Working。
- LLM Call、Reasoning、Tool 或任意通用 Status Event 不单独决定 Turn 是否运行。
- Bubble Tea 后台命令返回只释放 goroutine，不再作为第二套 Turn 终态真相。

Response stream reconnect 复用同一个 status indicator、activity marker、shimmer、elapsed time 和 `esc to interrupt` 交互，不新增 reconnect 专用动画组件：

- status renderer 必须读取当前 status header/details，不能硬编码只渲染 `Working`。
- 收到 `StreamErrorEvent{WillRetry:true}` 时先保存当前 status header，确保 status indicator 可见，再显示 `Reconnecting... n/m` 和可选底层 details。
- retrying StreamErrorEvent 不 `finishDraft`、不提交 ActiveHistoryCell、不插入 ErrorCell，也不改变 Turn running 状态。
- 收到下一条非 retry 的 live Event 时恢复此前保存的 status header；连续 retry event 只保存一次原 header。
- retrying 状态使用 TerminalPalette 的既有 status accent，不以硬编码 ANSI 颜色实现；无颜色终端仍保留文本和 details。
- Replay/Resume initial history 忽略 retrying StreamErrorEvent，不能恢复旧 retry status 或在历史底部生成永久 `Reconnecting...` Cell。
- `WillRetry=false` 使用最终错误投影，并等待唯一 `TurnCompleteEvent`/`TurnAbortedEvent` 结束 Working；TUI 不自行合成 Turn terminal。

### 19.4 Interactive Request 与 Diff

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

Approval request 通过统一 `EventMsg` 进入 TUI，TUI 将 `ApprovalDecisionOp` 返回 Session；Session 由 ApprovalCoordinator 应用内存 grant，Tool 的 ItemCompletedEvent 记录最终 completed/declined/failed/stale 状态。Approval 与用户提问只共享 Session-owned request/waiter 基础设施，不共享 payload、决定类型或业务语义。

`request_user_input` 使用独立的 Request User Input Overlay，不复用 Approval Dialog，也不解释权限语义：

```text
Request User Input Overlay
├── Header / Question
├── selectable Options + descriptions
├── automatic Other / free-form input
├── optional multi-select state
└── submit / cancel / keyboard hints
```

- `RequestUserInputEvent` 通过统一 `EventMsg` 进入 TUI；TUI 仅收集结构化答案并以 correlated `UserInputAnswerOp` 返回 Session。
- Overlay 使用稳定 Question ID 组装答案，不以 Question 文本作为 key；单选、多选和 Other/free-form 均归一化为 typed answer。
- 等待回答时 ActiveTurn 与 RunningTask 保持活动；Default 与 Plan Mode 使用完全相同的交互生命周期。
- 用户取消或 TUI/Session 关闭必须释放 waiter，并形成可判定的 cancelled/unavailable Tool Result；不得转换成 Approval denial。
- Request/answer 是 transient interactive protocol；TUI 可以在 live transcript 中投影 `RequestUserInputCell` 或回答摘要，但不伪造可 Replay 的历史提问项。

Plan Mode 的正式方案使用独立 `ProposedPlanCell`，不复用 `UpdatedPlanCell`：

- `PlanDeltaEvent` 只更新当前 Plan Item 对应的 ActiveHistoryCell；completed Plan TurnItem 提交可 Replay 的 `ProposedPlanCell`。
- `PlanUpdateEvent` 仍只生成 transient `UpdatedPlanCell`，表示 Default Mode 中 `update_plan` 的 checklist；两者不得共享 payload、revision 或恢复规则。
- TUI 仅在当前模式为 Plan、当前 live Turn 产生 completed Plan Item、没有 queued follow-up 且没有其他 modal 时显示 `Implement this plan?`。
- 基础选项为 `Implement this plan` 与 `Stay in Plan mode`。前者提交携带 Default mode override 的新 `UserInputOp("Implement the plan.")`；后者只关闭 Popup，不改变模式或创建 Turn。
- implementation Popup 是 transient UI state，Replay/Resume 不恢复旧 Popup；Resume 只恢复 completed `ProposedPlanCell`。清空上下文后实施属于后续产品能力，不阻塞基础生命周期对齐。

### 19.5 Tool Projection 与展示 Contract

TUI 的 Tool 展示必须同时吸收 Codex 的 HistoryCell/树状 activity 表现和 Claude Code 的 Tool-specific UI projection。TUI 不根据模型生成的自然语言标题、`action_summary` 或 Tool Result 文本反推工具身份；`TurnItem.ToolName` 是唯一的工具身份来源，结构化 `ToolDisplayResult` 是结果展示来源。

Tool 展示遵循以下链路：

```text
TurnItem.ToolName + typed payload
→ ToolDisplaySpec
→ ActiveToolCell / HistoryCell
→ Rich / Raw Renderer
```

`ToolDisplaySpec` 至少表达：

```go
type ToolDisplaySpec struct {
    ToolName       string
    UserFacingName string
    Category       ToolDisplayCategory
    Summary        string
    Detail         string
    ResultSummary  string
    Status         ToolDisplayStatus
    Metadata       map[string]any
}
```

具体的 `Diff`、文件变更统计、进程输出和媒体数据继续通过 `ToolDisplayResult` 的 typed 数据承载，不在 TUI 中从文本重新解析。`apply_patch` 是遗留设计，其生产实现已删除，也不进入本展示 Contract。

首批内置 Tool 的展示规则如下：

- `read`、`grep`、`glob` 等只读探索 Tool 统一进入 Codex 风格的 `Exploring`/`Explored` 树中，但每个叶节点必须显示真实 Tool 名和必要参数；不得把未知探索 Tool 默认显示为 `Read`。
- `execute_command` 统一进入 Codex 风格的 `Running`/`Ran` 树中，摘要包含命令，结果包含截断后的 stdout/stderr、退出状态和错误状态；不得把命令执行伪装成文件修改。
- `update_plan` 直接沿用 Codex 的 `Updated Plan` 展示逻辑，由 live `PlanUpdateEvent` 生成独立 HistoryCell，不进入 `Explored`、`Ran` 或普通 `ToolHistoryCell`。
- `write` 使用 Claude Code 风格的 `Write`/`Create` 展示目标路径和必要的行数/大小摘要；覆盖已有文件时沿用结构化 Diff 和 Approval 展示。
- `edit` 使用 Claude Code 风格的 `Edit`/`Update` 展示目标路径和变更统计；完成态可以提供可进入 Detail View 的结构化 Diff。
- `web_search` 使用搜索专用摘要，显示查询和结果统计，不并入本地探索树。
- 未知或扩展 Tool 使用真实 Tool 名和安全的通用摘要，不能因为缺少专用 renderer 而丢失 Tool 身份。

每个 Tool 的 renderer 应分别覆盖调用摘要、运行中状态、等待 Approval、完成结果、失败结果和拒绝结果，职责对应 Claude Code 的 `renderToolUseMessage`、progress、result 和 error projection。主 transcript 保持摘要，长结果和结构化 Diff 进入 Detail View。

工具状态至少区分：`queued`、`running`、`waiting_for_approval`、`completed`、`failed`、`denied` 和 `partial`。等待 Approval 时，对应 Tool 行和 Approval Dialog 都必须可见；Approval 完成后，Completed Item 保留 approved/denied 的最终事实。

目标展示示例：

```text
• Explored
  ├─ Read internal/agent/session/session.go
  ├─ Grep "Approval" internal/policy
  └─ Glob internal/**/*.go

• Ran go test ./...
  └ ok

• Updated internal/policy/presentation.go
  └ +8 -2 lines
```

## 20. Session、Resume 与中断

### 20.1 Thread 创建与持久化物化

- 仅启动 `amadeus` 时可以先生成 ThreadID 并创建未持久化的 AmadeusThread。
- 第一次提交真实用户输入时由 ThreadManager 通过 LiveThread 物化 SessionMetaItem RolloutLine。
- LocalThreadStore 创建 JSONL Rollout；SQLite StoredThread 由已持久化 SessionMetaItem 和后续 RolloutItem 派生。
- internal Session 只持有 ThreadID 和 LiveThread，不持有 SQLite Row。

### 20.2 Resume

- `/resume` 在 TUI 内选择当前项目的 Session。
- `amadeus --resume <id>` 从终端直接恢复。
- ThreadManager 先读取 StoredThread 定位 Rollout，再通过 ThreadStore.LoadHistory 构造 `InitialHistory::Resumed`。
- Session spawn 使用 InitialHistory 恢复 canonical conversation/Replacement History 和其他明确持久化的 Session 投影；不恢复 `update_plan` checklist 或 `PlanUpdateEvent`。
- SessionPermissionContext 在 Resume 时重置为空 Read/Edit Directories 和空 Command/External Grants，并生成新的临时 Permission Context Update；CollaborationMode 独立从 SessionConfiguration/Thread settings 恢复。
- 不恢复旧 goroutine、文件句柄或进行中的进程。

### 20.3 中断后继续

用户取消 Turn 时：

1. TUI 通过 AmadeusThread 提交 `InterruptOp`。
2. internal Session 找到 ActiveTurn 并取消 RunningTask context。
3. SessionTask.Abort 尽力终止活动 Tool。
4. 为未完成 ToolCall 写入 cancelled ToolResult。
5. 追加未完成 ToolCall 的 cancelled ToolResult 和 `EventMsgItem(TurnAbortedEvent)`，并 flush。
6. 清除 ActiveTurn。
7. 发布 `TurnAbortedEvent`，TUI 回到可输入状态。

下一次用户输入始终创建新 Turn。SessionState.History 注入最近中断事实；模型根据新输入决定重新规划或开始新任务。

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
- ThreadID/TurnID 使用 Protocol/Identity domain 的路径安全稳定标识，并由对应 SessionMetaItem、TurnContextItem、ResponseItem 或 EventMsg payload 携带；RolloutLine 不定义 ID owner。
- LiveThread/ThreadStore 是唯一文件写入边界；Session、Tool 和 TUI 不直接打开文件追加。
- 恢复时逐行解析；只有最后一条不完整记录可以被安全忽略或截断，完整但缺少末尾换行的最后一条记录会被补齐后再追加，其他解析错误必须显式报告。
- append batch 必须先完整编码再写入；部分写失败时回滚到 batch 起始 offset，flush 使用文件同步保证 durable 顺序。
- ToolCall/ToolResult 使用稳定 CallID 配对。
- CompactedItem、TurnContextItem、ResponseItem 以及 store policy 选中的 Plan/Token/Approval/Turn EventMsg 均进入 Rollout；未决 ApprovalRequestEvent、未决 RequestUserInputEvent、ApprovalDecisionOp 与 UserInputAnswerOp 不作为独立 canonical history。
- Runtime 临时状态、goroutine、进程句柄和 Pending Future 不持久化。
- SessionMetaItem 保存重建索引所需的 CWD、标题、模型、Git metadata、归档初态和创建时间；后续标题/归档变化使用对应 typed EventMsg。

### 21.2 SQLite State DB

SQLite 位于 `$AMADEUS_HOME/data/amadeus.db`，核心表保持最小：

#### `schema_info`

- version
- created_at

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

Amadeus 不提供旧 SQLite schema migration。启动时只接受当前 `schema_info.version`；版本不匹配时返回明确的 development-data-reset 诊断。SQLite 是可重建 index，可以删除后从当前格式 Rollout 重建；旧 schema、旧表和旧 Rollout 不自动读取、导出或升级。

### 21.3 Metadata Sync 与 Rebuild

```text
LiveThread.AppendItems
→ LocalThreadStore durable write + flush JSONL
→ 从活动 Recorder 的 canonical path 重放 metadata projection
→ StateDB.UpsertThread
```

- SQLite 可以暂时落后 JSONL，但不能包含尚未 durable 的 Rollout 事实。
- Recorder 必须维护 durable watermark；MetadataSync 只能读取不超过该 watermark 的 RolloutLine。Buffered Append 不触发 SQLite upsert，显式 Flush 或 Durable Append 成功后才能同步索引。
- `append → SQLite upsert → flush` 在任何路径都属于非法顺序；进程在 flush 前崩溃时，恢复结果允许缺少 buffered tail，但 SQLite 不能引用该 tail。
- Metadata 更新失败必须记录警告并保留可重建状态，不能回滚已经 durable 的 canonical history。
- 当前 schema 的 SQLite index 缺失或漂移时，从当前格式 `SessionMetaItem`、ResponseItem 和 EventMsg 重建 StoredThread；活动 Thread 即使索引被清空，下一次 canonical append 也能直接重新 upsert。
- `/resume`、列表和搜索优先查询 SQLite；索引缺失或漂移时可扫描 Rollout 修复。
- `tokens_used` 是各 canonical TokenCountEvent 的 Thread 累计值，不是仅保存最后一个 Turn 的 usage。
- Session Permission State（Mode、Additional Working Directories 和 Session Rules）只存在于活动 internal Session，不写入 Thread metadata，也不从历史 Approval Decision 恢复。

只有当真实产品需求证明需要分页历史或全文搜索时，才增加可重建的 SQLite History Projection。

旧 JSONL、旧 SQLite、旧 fixture 和旧本地开发数据不属于恢复输入。发生格式变更时直接删除旧 codec、旧表访问和兼容测试，并要求清理开发数据后重建；不得留下 migration decoder、legacy reader/writer 或双格式探测。

## 22. MCP 与 Skill

### 22.1 MCP

MCP 的最小稳定数据模型如下；真实 Client/SDK 类型只能在 `internal/mcp` 内部使用，不能越过 Tool/Session 边界：

```go
type MCPBinding struct {
    Revision string
    Servers  []MCPServerBinding
}

type MCPServerBinding struct {
    Name                    string
    ConnectionGeneration    uint64
    ToolCatalogRevision     uint64
    ResourceCatalogRevision uint64
    Connected               bool
    ToolsLoaded             bool
    ResourcesLoaded         bool
}

type ToolCatalog struct {
    Server  string
    Revision string
    Tools   []MCPToolMetadata
}

type ResourceCatalog struct {
    Server    string
    Revision  string
    Resources []MCPResourceMetadata
}

type MCPToolMetadata struct {
    Server                string
    Name                  string
    Description           string
    InputSchema           json.RawMessage
    ReadOnly              bool
    SupportsParallelCalls bool
    Revision              string
}

type MCPResourceMetadata struct {
    Server      string
    URI         string
    Name        string
    Description string
    MIMEType    string
    Revision    string
}
```

- MCP 以 Codex 的 `MCPRuntime`、`MCPBinding`、`ToolCatalog`、`ResourceCatalog` 和 per-step snapshot 为目标命名与职责模型；旧 `Manager` 直接删除，不作为迁移 wrapper 或第二套 owner 保留。
- `MCPRuntime` 是 Session 内唯一的 MCP Server 生命周期 owner，负责配置投影、Client 启停、连接状态、Server refresh、Tool/Resource discovery、Catalog Revision 和当前 Binding；Tool、Task、TUI 不直接持有 MCP Client。
- MCP Tool 通过统一 `ToolRegistry`/`ToolExecutionService` 执行，不建立第二套 Agent Loop、历史、Process Runner 或 Approval UI。
- Tool Discovery 初期采用 Codex 对齐的 lazy discovery：模型先看到 `mcp_list_tools`，按需获得某个 Server 的 `ToolCatalog`，再通过 `mcp_call` 调用具体 Tool。`MCPBinding` 只描述当前 Server 连接和 catalog 代次；工具/资源 metadata 必须留在各自 typed catalog，不把所有发现结果塞回 Binding。
- `ToolCatalog` 必须包含稳定的 server/tool identity、description、input schema、read-only/parallel capability 和 catalog revision；`ResourceCatalog` 必须包含 server、URI、name、description、MIME type 和 resource revision。两个 Catalog 都是 Runtime 发布的不可变 snapshot，Tool 和 TUI 不得持有可变 Client 引用。
- `mcp_call` 在 Prepare 阶段必须校验当前 StepContext 的 `MCPBinding`、Server Catalog 和具体 Tool schema；执行阶段只使用已验证的 server/tool identity，不接受任意未发现的远程名称。
- `mcp_list_resources` 与 `mcp_read_resource` 复用同一个 MCPRuntime、Binding、ResourceCatalog 和 Tool Contract；Resource 内容按不可信 ToolResult 处理。
- MCP List 和只读 Resource Read 默认 Allow；MCP Call 默认 Ask，无法确认安全性时保持 Ask。若实现需要对 Resource Read 采用 Ask，必须同步修改 Tool/Approval Contract，不得让代码与本文分叉。
- MCP Tool 的并发能力来自远程 annotation/capability，并保存在 `MCPToolMetadata`；明确只读的 Tool 才具备未来有界并行的资格，写入或未知能力必须串行。基础版仍通过统一的动态 `mcp_call` Tool 暴露远程调用，因 Tool Registry 无法按单次参数表达 capability，`mcp_call.SupportsParallelToolCalls()` 安全返回 `false`；不得据此删除 Catalog 中的真实 capability，也不得把未知 Tool 标为并发安全。若后续投影 direct MCP Tool，才按 metadata 选择有界并发。
- MCP Server 配置、连接代次或 Catalog 变化后生成新的 Binding Revision；下一 Model Step 使用新的 StepContext，旧 Lazy/Deferred Call 返回 typed `stale_mcp_binding`，不能误路由到新 Server/Tool。`Refresh` 必须使 ToolCatalog 和 ResourceCatalog 同时失效，不能只刷新其中一类。
- MCP Server 的 startup、refresh、disconnect、reconnect、shutdown、schema error 和 remote error 必须产生可观察的诊断或 ToolResult；不允许只写日志后让 Turn 永久等待。
- MCP Server 不可信输出只能进入 ToolResult/Contextual User Fragment，不能注入 BaseInstructions、Developer Instructions 或系统级 Prompt。
- 不在当前基础能力范围内实现 Codex 的完整 OAuth、Elicitation、Plugin/Remote Connector、MCP dependency installer 或独立 Tool Search 服务；这些能力不得以伪字段或未接线 Prompt 宣称已支持。

### 22.2 Skill

Skill 的最小稳定数据模型如下；Plugin、Remote 和 Dependency 字段可以先保持为空或不暴露，但不能让正文、执行器和 Catalog metadata 混为一个对象：

```go
type SkillMetadata struct {
    Name             string
    Description      string
    ShortDescription string
    PathToSkillMD    string
    Source           SkillSource
    Scope            SkillScope
    Enabled          bool
    Policy           SkillPolicy
    References       []SkillResource
    Scripts          []SkillResource
    Assets           []SkillResource
    Revision         string
}

type SkillResource struct {
    Path     string
    Kind     SkillResourceKind
    Size     int64
    Revision string
}

type SkillInjection struct {
    Name     string
    Path     string
    Revision string
    Content  string
}

type SkillResourceKind string // reference | script | asset
```

- Skill 以 Codex 的 `SkillCatalog`、`SkillMetadata`、`SkillInjection`、`SkillPolicy` 和 Resource/Invocation 边界为目标命名与职责模型；Amadeus 可暂不实现 Codex 的 Plugin/Remote/Dependency 全套能力，但不得用旧的通用 Extension 对象继续承担 Skill 领域职责。
- 用户级 Skill 位于 `$AMADEUS_HOME/skills/<name>/SKILL.md`；项目级 Skill 位于 `<project>/.amadeus/skills/<name>/SKILL.md`，项目同名 Skill 覆盖用户级 Skill。每个 Skill 的 root 是包含 `SKILL.md` 的目录，不能通过符号链接逃逸其所属 Skill Root。
- `SkillMetadata` 是 Catalog 常驻数据，至少包含 `name`、`description`、`short_description`、`path_to_skills_md`、`source/scope`、`enabled`、`policy`、`references`、`scripts` 和 `revision`；完整 `SKILL.md` 正文只在显式选择或 `read_skill` 时加载。
- `SkillCatalog` 负责 Root discovery、frontmatter 校验、同名覆盖、enabled/disabled settings、资源索引、增量 Revision 和 load warning；它不负责 Prompt 装配、Tool 执行、Approval 或进程生命周期。
- `SkillCatalog` 的常驻对象是 `SkillMetadata`/`SkillResource`；完整正文和资源内容属于按需加载的 read result，不得缓存为另一套 Skill owner。Composition Root 只提供 Skill roots/settings，SessionServices 直接创建并拥有 SkillCatalog。
- `SKILL.md` 是显式工作流和说明的唯一正文入口。普通 Turn 只注入 Skill Index/metadata；用户在输入中使用 `$skill-name` 后，Session 生成 `SkillInjection`，由 ContextManager 追加带 name/path/revision 的动态 Context Update；TUI 的 Skill 操作只改变 enabled policy，不伪造正文注入。
- `read_skill` 是唯一的 Skill 正文/资源读取边界：读取 `SKILL.md` 或受限的 `references/*`，支持 bounded bytes、line/limit、稳定路径和 path-escape rejection。Skill Resource 返回立即的不可信 Tool Observation，不成为系统 Prompt。
- Skill 资源至少分为 `SKILL.md`、`references/*`、`scripts/*` 和 `assets/*`：
  - `SKILL.md`：显式注入或按需完整读取；
  - `references/*`：按需分页读取，不默认注入；
  - `scripts/*`：不作为普通 reference 大量注入，只提供可验证脚本 metadata；
  - `assets/*`：不直接注入文本，按真实媒体/文件 Tool 能力读取。
- Skill Script 不拥有独立进程执行器。模型只能通过 `execute_command` 执行脚本；Command Prepare 必须解析目标脚本、确认它属于 enabled Skill 的 `scripts/` 目录、记录 Skill attribution，然后复用普通 Command Permission、Approval、Host Runner、ProcessManager、取消和 `write_stdin`。
- Skill Script 的归属识别不等于自动 Allow；脚本仍受工作目录、命令规则、文件系统策略、Approval 和 Session Grant 约束。脚本路径不属于 Skill `scripts/` 时按普通命令处理，不得伪装成 Skill Script。
- 基础版 Skill Script attribution 只解析直接脚本命令和常见解释器（含简单解释器 flags）；不解析任意 Shell AST、管道、重定向、`cd &&` 或 `sh -c` 内嵌脚本。无法确定唯一脚本时按普通命令处理，不阻断命令执行。
- 初期只实现 Codex 风格的隐式 Skill invocation detection/attribution：检测已执行的 Skill Script 或明确读取的 `SKILL.md` 并记录一次 invocation；不因隐式检测自动注入完整 Skill 正文，避免 Context 膨胀和执行前事实变化。
- Skill Catalog/Resource/Policy 变化后生成新的 Skill Revision；下一 Model Step 重新 capture Skill snapshot。旧 `read_skill` 或 Skill-aware Command 使用过期 snapshot 时返回 typed stale result。
- Skill 生命周期必须区分三种事实：discovery 产生 metadata/index，显式 `$skill-name` 产生一次 `SkillInjection`，`read_skill` 产生一次 bounded `ToolResult`。后两者都不能反向修改 Catalog，也不能把 references、scripts 或 assets 自动提升为系统 Prompt。
- 不在当前基础能力范围内实现 Codex 的 Plugin Skill、Remote Skill、MCP dependency installation、Product gating、图标/UI metadata 或独立 Skill package manager；未实现能力不得写入 Prompt 或 Tool Spec。

SessionServices 直接拥有 MCPRuntime 与 SkillCatalog；StepContext、Prompt、Event 和 Tool Contract 都从这两个明确 owner 获取能力快照与内容。

## 23. Web 与网络

- `web_search` 是 Amadeus 必须保留的内置 Tool；`web_fetch` 是独立 Tool，二者不能因为 Shell 或 MCP 能力存在而删除。
- 模型始终只看到稳定的单一 `web_search` Tool，不暴露 `brave_search`、`tavily_search` 等 Provider 专用 Tool 名。
- 用户通过配置选择 Search Provider；首批正式支持 `duckduckgo`、`tavily`、`searxng` 和 `brave`。
- `duckduckgo` 默认不要求 API Key；`tavily` 与 `brave` 要求 API Key；`searxng` 要求用户提供实例 Base URL。
- Search Provider 在 Application Bootstrap 时根据配置构造并注入 WebSearch Service；Agent Runtime、Tool Contract、TurnItem 和 TUI 不感知具体搜索引擎。
- Provider 必须实现统一的 `Search(context.Context, query, limit)` 边界，并返回标准化 `title/url/snippet` Result。
- Provider 返回的 `snippet` 是已经过搜索引擎排序和清理的 search-summary evidence，不等于目标页面完整正文；Tavily 的 `content` 在统一边界中同样只映射为 `snippet`，不得因为 Provider 面向 Agent 就将其标记为 full-page evidence。
- Provider 超时、认证、限流、网络和协议错误必须转换为可见 ToolResult，并保留稳定 Provider/Error Kind 供诊断。
- 单次 Search 调用使用 Turn 派生 Context 和配置超时，保持同步 API；多个独立 `web_search` 调用只由 Tool Executor 做有界并发。
- 搜索结果进入 Context 前进行 URL 校验、去重、数量限制、文本长度限制和来源标注。
- `web.search.enabled=false` 时不注册 `web_search`，模型不可见；启用后 `web_search` 默认 Allow。
- `web_fetch` 是精确 URL 的按需全文读取能力：用户直接提供 URL、搜索摘要不足、需要核对原文或验证动态事实时才使用；搜索摘要已经足够时不得为了“更完整”自动抓取页面。
- 基础版不在 `web_search` 后自动抓取 Top-N 结果，不建立 WeKnora 风格的 pipeline auto-fetch；是否调用 `web_fetch` 由模型根据证据充分性显式决定，抓取失败不应触发等价搜索或无限重试。
- `web_fetch` 保持基础 `{url}` Schema，抓取后返回有界、结构化且标记为不可信的页面内容，由当前主模型完成理解和回答；基础版不在 Tool 内部再发起 secondary-model summarization。
- `web_fetch` 输入必须在 Approval 前完成 absolute `http/https` URL、Hostname 和禁止 userinfo 校验；非法 Scheme、相对 URL 或凭据 URL 不得先展示网络 Approval 再在 Execute 阶段失败。
- `web_fetch` 对未授权 Hostname 返回 Ask；用户选择 `Yes, and don't ask again for <hostname>` 后只写入当前 Session 的精确 Hostname Domain Rule。
- Approval 只授权当前请求 Hostname，不授权任意重定向目标。同 Hostname 的受限重定向可以在重新执行 URL/SSRF 检查后跟随；跨 Hostname 重定向必须停止并返回 typed redirect result，由模型使用目标 URL 再次调用 `web_fetch`，从而触发目标 Hostname 的独立 Approval。
- URL 初检、每次重定向和实际 Dial 都必须执行 SSRF 防护；DNS 校验后的连接必须固定使用已验证的 public IP，同时保留原 Hostname 用于 HTTP Host 与 TLS SNI，不能在安全检查后对原 Hostname 进行第二次不受控解析。
- Fetcher 必须限制总超时、响应字节数和重定向次数；`max_redirects: 0` 明确表示拒绝所有重定向，不得被 Fetcher 重解释为默认值。超过字节限制返回 `Partial=true` 的可用结果，而不是无界读取。
- HTML 使用 DOM-based readable-content extraction 并投影为 bounded Markdown，至少保留标题、段落、列表、链接和代码块的基本结构；页面标题只出现一次，不使用正则剥标签后压成单行正文。
- 基础版只内建文本类 Content-Type：`text/html`、`text/plain`、`text/markdown` 和可安全展示的 `application/json`；其他二进制内容返回稳定的 unsupported-content result，不将 PDF、压缩包或任意字节直接按 UTF-8 注入 Context。
- `web_fetch` 的成功结果必须通过 typed Document/Data contract 提供 final URL、Content-Type、Title、Markdown、Partial 和实际读取字节数；不保留并行的 legacy `Text` 正文字段。失败结果必须区分 invalid URL、approval denial、SSRF rejection、redirect approval required、timeout、empty content、unsupported content 和 upstream HTTP failure。
- 页面正文与搜索结果始终属于 untrusted external content，不能覆盖 System/Developer Instructions、Permission、Approval、Tool Schema 或当前任务边界。
- 不构建通用 Network Permission Store；Provider 配置缺失或请求失败时返回可见 ToolResult。
- Browser 自动化不进入内置核心能力，只通过 MCP 或 Extension Tool 接入。
- Tavily Extract、自定义 fetch adapter、二进制文件持久化、15 分钟 LRU Cache 和 secondary-model query-focused extraction 均属于后续可选优化，不进入基础 `web_fetch` Contract；Browser/JavaScript rendering 继续只通过 MCP 或 Extension Tool 接入。未来引入其他优化时不得改变 Hostname Approval、跨域重定向和 untrusted-content 边界。

目标配置形态：

```yaml
web:
  fetch:
    enabled: true
    timeout: 30s
    max_bytes: 1048576
    max_redirects: 3 # 0 means reject all redirects
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
> Built-in Field Defaults
```

API Key 默认通过环境变量或配置文件提供，不要求暴露 CLI Flag，避免进入 Shell History。

Built-in Field Defaults 只表示 retry、timeout、Tool Output truncation 等字段级归一化默认值，不创建内置 Provider 或内置 Provider entry。`model_provider` 必须引用用户在 `model_providers` 中定义的条目。

### 24.3 核心配置域

- model selection/runtime overrides
- model reasoning effort
- model provider transport
- agent runtime limits
- context window/compaction
- tool process defaults
- MCP
- Skill
- Web Search
- logging

Approval 不暴露配置规则 DSL。文件 Tool 的 Session Allow 更新内存中的 SessionPermissionContext 与 Additional Working Directories；命令和 MCP 等 Tool 可以增加各自的 Session Rule；Plan Mode 由 CollaborationMode/ToolRouter 自动禁止实施副作用。

敏感字段在 `config show`、日志和错误中脱敏。

## 25. Event Protocol

### 25.1 协议分层

Amadeus 使用 Codex 风格的 `Submission/Event/EventMsg` 作为 Session 的唯一公开协议，但不建立支持任意 topic/subscriber 的通用 Event Bus。边界固定为：

```text
Submission {id, op}  Interface/Application → internal Session
Event {id, msg}      internal Session → Interface/Application
RolloutItem          internal Session → LiveThread，canonical persistence
TUI Message          Bubble Tea 内部按键、动画、Popup 与异步结果
Trace / Telemetry    Runtime 内部诊断，不进入产品 Event Protocol
```

Approval request 是 `EventMsg` variant，回答使用带 RequestID 的 `ApprovalDecisionOp` 作为新的 Submission。`request_user_input` 使用独立的 `RequestUserInputEvent → UserInputAnswerOp` pair，两种交互共享 Session waiter 基础设施但保持 payload 与语义分离。

### 25.2 Submission 与 Event Envelope

Protocol/Identity domain 拥有 ThreadID、TurnID、SubmissionID、RequestID 和 ItemID；rollout、thread、turn 或 persistence package 不得重新定义这些 ID。

```go
type Submission struct {
    ID SubmissionID
    Op Op
}

type Event struct {
    ID  SubmissionID
    Msg EventMsg
}
```

- `Event.ID` 将输出关联到触发它的 Submission；Session background event 使用明确的 generated ID，不用空字符串表达未知来源。
- ThreadID、TurnID、RequestID 和 ItemID 由具体 EventMsg 携带，只有需要该作用域的 variant 才包含对应字段。
- SessionIo 的单一 Event Channel 保证 live 发送顺序；不增加 Event Priority、Topic DSL 或第二套 Sequence。
- JSONL RolloutLine 维护持久化 sequence；不能用 TUI Event 到达顺序替代 Rollout 顺序。

### 25.3 最小 EventMsg

首批公共 Event：

```text
SessionConfiguredEvent
ThreadSettingsAppliedEvent

TurnStartedEvent
TurnCompleteEvent
TurnAbortedEvent

ItemStartedEvent
ItemCompletedEvent

AgentMessageContentDeltaEvent
PlanDeltaEvent
ReasoningContentDeltaEvent
CommandOutputDeltaEvent

PlanUpdateEvent
TokenCountEvent
ContextCompactedEvent
ApprovalRequestEvent
RequestUserInputEvent

WarningEvent
ErrorEvent
StreamErrorEvent
```

`SessionConfiguredEvent` 必须携带真实生效的 SessionConfiguration 摘要，不允许只发送无内容的 configured marker。`ThreadSettingsAppliedEvent` 表示设置已由 Session 接纳并应用；设置失败使用 correlated ErrorEvent。

`StreamErrorEvent` 是 response stream 生命周期的 typed notification，而不是普通永久 History 项。它至少表达：

```go
type StreamErrorEvent struct {
    Message           string
    AdditionalDetails *string
    ProviderError     *ProviderErrorInfo
    WillRetry         bool
}
```

- `ProviderErrorInfo` 是 Event Protocol 自己拥有的脱敏 DTO，不直接暴露或引用 Infrastructure/SDK error 类型。
- `WillRetry=true` 表示错误是瞬态的，Core 将自动恢复且当前 Turn 继续运行；`Message` 由 retry owner 生成 Codex 风格 `Reconnecting... n/m`，TUI 不解析字符串推导次数或状态。
- `AdditionalDetails` 展示底层安全诊断，例如 idle timeout；敏感 header、API Key 和未脱敏响应不得进入 Event。
- `WillRetry=false` 表示当前 stream 不再恢复；最终 Turn 是否 failed 仍由 SessionTask result 与 Session terminal protocol 决定。
- retrying StreamErrorEvent 不进入 canonical Rollout，不在 Resume/replay 中重放，不创建 ItemCompletedEvent。

明确不进入公共 Event Protocol（内部仍可作为 Trace/Telemetry）：

- LLMCallStarted/Completed。
- Model Step 生命周期事件。
- 通用 StatusChanged 生命周期事件。
- ContextBuildStarted/Completed。
- TUI Working/Shimmer Tick。
- Provider Trace、单次 HTTP Attempt、Retry Backoff tick、delay 采样等遥测细节。

这些信息可以保留在日志、Trace 或测试探针中，但 TUI 不应依赖它们判断 Turn 生命周期。`StreamErrorEvent{WillRetry:true}` 是对此规则的明确边界：它公开“Core 正在自动恢复且 Turn 未结束”的产品生命周期，不公开每一次 transport attempt 的内部遥测。

### 25.4 TurnItem

`TurnItem` 是 EventMsg 和 TUI History 的稳定业务项；需要恢复的完成态通过 `EventMsgItem(ItemCompletedEvent)` 原样持久化。首批类型：

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
- `PlanUpdateEvent` 不属于 TurnItem；它是 live transient checklist notification，不参与 canonical replay。
- `PlanItem`：显式 Plan Mode 中 `<proposed_plan>` 的完整 Markdown；它是可 Replay 的完成态业务投影。

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
ItemStartedEvent{Item: in_progress}
→ zero or more Delta{ItemID}
→ ItemCompletedEvent{Item: completed|failed|declined}
```

要求：

- Delta 必须携带 ItemID，只更新对应 Active Item。
- Completed Item 必须包含恢复和最终展示所需的完整事实，不能要求 Replay 重新拼接历史 Delta。
- TUI 收到没有 Started 的 Completed Item 时必须能够直接渲染，支持 Resume 和晚订阅。
- 同一 ItemID 的 Completed 只能提交一个正式 HistoryCell；重复或迟到 Delta 必须忽略并记录诊断。

### 25.6 Proposed Plan

`PlanDeltaEvent` 与 completed Plan TurnItem 是 Plan Mode 正式方案的 live/replay contract：

```go
type PlanDeltaEvent struct {
    ThreadID ThreadID
    TurnID   TurnID
    ItemID   ItemID
    Delta    string
}

type PlanItem struct {
    ID   ItemID
    Text string
}
```

Runtime 只在 frozen Collaboration Mode 为 Plan 时启用 `ProposedPlanStreamParser`。Parser 必须允许普通 Assistant 文本出现在 block 前后，并把 `<proposed_plan>` block 内文本从 AgentMessage stream 分流到单一 Plan Item。tag prefix 可以跨 Delta，stream completion 必须 flush parser；未闭合、重复或嵌套 block 形成明确 protocol/model-visible failure，不得把半个 tag 或重复方案静默写入 transcript。

原始 Assistant ResponseItem 是模型 continuation history；completed Plan TurnItem 是 UI/replay 业务投影。两者按同一次模型完成的 ordered canonical append 提交。Plan Delta 不持久化；completed Plan Item 必须携带完整 Markdown，允许没有 Started/Delta 的 Replay 直接渲染。

### 25.7 Approval Request

`ApprovalRequestEvent` 带稳定 RequestID、ThreadID、TurnID 和完整 typed Presentation。它至少包含 Tool 名、操作类型、Title、Subtitle、Question、目标路径/命令/CWD、Details、`*FileChangePreview` 和 typed Options；Diff 不得在协议桥接时转换为 `string` 或 `any` 的扁平文本。TUI 只渲染 Presentation，不重新解释权限语义；回答通过 `ApprovalDecisionOp` 返回。

```go
type ApprovalRequestEvent struct {
    RequestID    RequestID
    Approval     ApprovalRequest
    Presentation ApprovalPresentation
}
```

未决 ApprovalRequestEvent 不作为 canonical history。最终 completed/declined/failed 结果进入对应 Tool/File/Command ItemCompletedEvent；安全审计可以单独记录，但不成为第二输出协议。

### 25.8 Request User Input

`RequestUserInputEvent` 带稳定 RequestID、ThreadID、TurnID、CallID 和 typed Questions；每个 Question 使用稳定 `snake_case` ID，并包含 Header、Question、Options、`MultiSelect` 等展示与回答约束。TUI/adapter 不改变问题语义，回答通过 correlated `UserInputAnswerOp` 返回。

未决 `RequestUserInputEvent` 与 `UserInputAnswerOp` 不作为独立 canonical history。`request_user_input` 的普通 Function Call 与最终 Function Call Output 仍按 ResponseItem 持久化，因此模型上下文和 Resume 保留最终 Tool 事实，而不恢复已关闭的 Overlay 或 pending waiter。

### 25.9 Event、Rollout 与 Delivery

持久化策略：

- 持久化 store policy 指定的 TurnStartedEvent、TurnCompleteEvent/TurnAbortedEvent、ItemCompletedEvent（包含 completed Proposed Plan）、TokenCountEvent、ContextCompactedEvent 和恢复所需 Context facts。
- 不持久化高频 Agent/Reasoning/Command/Plan Delta、Working、未决 ApprovalRequestEvent、未决 `RequestUserInputEvent`、`PlanUpdateEvent`、Popup、动画 Tick 和 retrying StreamErrorEvent。`update_plan` 与 `request_user_input` 的 Function Call/Function Call Output 仍作为普通 ResponseItem 持久化。
- Resume 从 typed ResponseItem 和 EventMsgItem 重建 Context/History，不重放旧 Delta。
- ResponseItem 与对应 ItemCompletedEvent 必须由 Session 在同一 ordered append 主链中提交；live ItemCompletedEvent 只能在该 append 成功后发布，`run_turn` 不保留等待 Turn 尾部才写入的私有 completed-item queue。
- 高频 completed facts 先由 Session 串行 buffered append 到 JSONL，并增量更新 Session 内存投影；SQLite 只保持到最近 durable watermark。Session 在 TurnStartedEvent、TurnCompleteEvent/TurnAbortedEvent 和其他 durability boundary 执行 flush，随后 MetadataSync 才推进 SQLite。

Turn 终态顺序固定为：

```text
RunningTask 返回
→ append canonical terminal facts
→ flush Rollout
→ 清除 ActiveTurn
→ 发布 TurnCompleteEvent / TurnAbortedEvent
```

Runtime 正确性不能依赖 TUI 消费速度：

- Rollout append/flush 失败可以使 Turn 失败。
- TUI Channel 关闭或 Renderer 退出不能反向把已完成 Tool/模型调用改判为失败。
- 高频 Delta 可以合并或在重绘层丢弃；ItemCompletedEvent、TurnCompleteEvent、TurnAbortedEvent、ApprovalRequestEvent 和 RequestUserInputEvent 不得静默丢失。Plan Delta 即使丢失，也必须由 completed Plan Item 的完整文本纠正最终展示。
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

Provider/stream error 还必须区分：

- transient retrying：Core 已决定自动重试，Turn 保持 running，只更新 live status。
- terminal provider failure：不可恢复或重试耗尽，进入 failed SessionTask result 和唯一 Turn terminal protocol。

所有错误必须：

- 带稳定错误码。
- 保留底层错误链。
- 不泄露 API Key。
- 在 TUI 显示可行动说明。
- 在 Turn 终态中可恢复或可诊断。

禁止出现“Working 动画停止但没有 Error/Completed Event”的静默失败。
禁止把 transient retrying error 插入永久 ErrorHistoryCell、提前 finish draft 或停止 Turn；也禁止重试耗尽后只清除 `Reconnecting...` 状态而没有最终错误和 Turn 终态。

## 27. 测试策略

### 27.1 Runtime Contract

- 一个用户输入只创建一个 Turn。
- 生产 RegularTask/CompactTask 不持有或回调 CLI/Application controller，不通过 request/result side channel 取得执行依赖。
- CLI invocation 在进入 Session 前已归一化；Session 可以在没有 Cobra/TUI 对象的测试中独立运行完整 Turn。
- SessionServices 在 Session spawn 时只构造一次；连续两个 Turn 复用 ModelClient、ToolRegistry、ToolExecutionService、ProcessManager、MCP/Skill/Web、Approval/Permission 与 UserInputRequester 交互能力，Session 关闭后统一释放。
- architecture test 验证 SessionState.Configuration 是配置唯一事实源，SessionServices 是 Session capability owner。
- RegularTask 不创建或关闭完整 Agent Runtime，只调用 Session 模块内唯一 `run_turn`。
- Turn 在模型调用前持久化。
- ItemStartedEvent/ItemCompletedEvent 使用相同 ItemID；持久化的 ItemCompletedEvent 足以独立 Replay。
- ResponseItem 与 ItemCompletedEvent 在 live ItemCompletedEvent 前进入 ordered canonical append；进程在 Turn 中途退出时不会出现“UI 已 completed、Rollout 无事实”。
- Completed/Aborted 终态唯一，失败信息进入唯一终态。
- completed、blocked、failed 和 aborted 按 TaskOutcome/terminal contract 映射，不使用通用 Go error 表达 blocked 或 budget limit。
- Interface 只依赖 Event/EventMsg 识别 Turn terminal，不同时等待私有 Task completion。
- Rollout append/flush 和 ActiveTurn 清理先于终态 Event。
- Resume 后 Rollout 顺序稳定。
- crash/fault injection 验证 SQLite 永不超过 JSONL durable watermark，Buffered Append 不提前 upsert metadata。
- architecture test 验证 production SessionTask 不引用 `cmd/amadeus` controller、TUI model、Cobra command 或完整 invocation，并直接通过 Session/SessionServices 完成运行。

### 27.2 Event Protocol

- Submission/Event 使用 correlation ID；具体 EventMsg 按需携带 ThreadID/TurnID/RequestID/ItemID。
- Delta 只能更新相同 ItemID 的 Active Item；迟到 Delta 不改变 Completed Item。
- 没有 ItemStartedEvent 的 ItemCompletedEvent 仍可直接渲染。
- ApprovalRequestEvent 必须通过带 RequestID 的 ApprovalDecisionOp 完成，且不使用独立 Request channel。
- RequestUserInputEvent 必须通过带 RequestID 的 UserInputAnswerOp 完成；Default 与 Plan Mode 复用同一 Tool、Event、waiter 和回答协议，不复用 ApprovalDecision。
- Plan Mode 的 `<proposed_plan>` 必须产生独立 Plan Item；PlanDeltaEvent 只能更新相同 ItemID，completed Plan Item 必须足以独立 Replay，且不能与 PlanUpdateEvent checklist 混用。
- `/plan <task>` 的 mode override 与用户输入在同一个 UserInputOp 中应用；设置失败不得创建 Turn，ActiveTurn 期间不得改变已经冻结的 CollaborationMode。
- 慢 Renderer、已关闭 TUI 或调试 Subscriber 不导致 Agent Turn 失败。
- Live 和 Replay 对相同 ItemCompletedEvent 生成一致 HistoryCell。
- Protocol package 只包含 identity/DTO/contracts；TranscriptState、TUI reducer 和 rollout replay projector 位于各自 projection/application package。

### 27.3 Context

- 超大 `docs/design.md` 不导致静默停止。
- 读取整个 `docs` 目录后仍可继续对话。
- Tool Result 被安全投影，live replay 与 Rollout/Resume projection 对 status、error、partial、metadata 保持语义等价。
- Prompt 数据模型和装配职责与 Codex 同构：`ModelMessages`/`BaseInstructions`、`ResponseItem`、`ToolSpec`、`WorldState`、`CollaborationModeState`、`ContextualUserFragment`、`Prompt` 和 `PromptSnapshot` 各自只有一个 owner。
- 普通 Turn 不存在独立 `RegularTaskPrompt`；Plan 使用当前模型的 `CollaborationModeMessages.plan`，Compact 使用 Codex `SUMMARIZATION_PROMPT`/`SUMMARY_PREFIX` 语义，三者都通过同一 Prompt 主链生成。
- Prompt 资产、动态 Context Fragment、Tool Spec 和 ContextManager 历史不会通过旧 `Assets`、`DeveloperInstructions(mode, toolNames)` 或兼容 wrapper 双写；旧 Prompt 构造路径在迁移后不存在。
- Claude Code Tool Guidance 只进入对应的 `read`/`edit`/`write`/`glob`/`grep` Tool，Codex Tool Guidance 只进入 `update_plan`/`write_stdin`/`execute_command`；未暴露 Tool 的 Prompt 不得注入。
- 每个 Model Step 的 Prompt、Tool Specs 和 Tool execution router 来自同一 ContextManager/TurnContext/StepContext snapshot；Tool/MCP/Skill revision 变化后下一 Step 会重新 capture。
- 空或 stale RequestSnapshot 不会被注入 ToolExecutionService；deferred/lazy capability 使用精确 revision 校验。
- canonical Rollout payload 通过统一 typed encoder/decoder round-trip；writer 与 projector 不使用彼此独立的 ad-hoc schema，也不存在旧格式 decoder/fallback。
- ContextManager 只能由 Session 根据 canonical facts 增量更新；Resume 重建一次，普通 append 不触发全量 Rollout rebuild。
- 访问嵌套目录后应用对应 AGENTS.md；AgentsMdManager/LoadedAgentsMd 是唯一 owner，stale snapshot 触发下一 Step recapture。
- Compaction 保留目标、修改、失败和待办。
- Compaction 后 Tool 协议合法。
- Provider usage 可以校准 estimator；多 Model Step live usage 与 Resume 后累计 usage 一致。
- ModelInfo input modalities 控制不支持内容的投影，不把能力检查推迟到 Provider Adapter 报错。
- 自动压缩与 `/compact` 复用同一 SessionServices.Compactor；`run_turn` 不嵌套运行 CompactTask。

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
- `update_plan` 最多一个 `in_progress`，空 `plan` 可清空 checklist；通过 transient `PlanUpdateEvent` 展示完整状态，ToolResult 严格返回 `Plan updated`，Rollout 只保存普通 Tool Call/Result。
- denied、validation failed、stale、command non-zero 和普通 Tool failure 都形成模型可见 Tool Result 并允许下一 Model Step；只有协议、持久化或 Runtime invariant 失败终止 Turn。
- 重复 Tool Call/Tool Error 不因固定低阈值被 Runtime 自动判定 stalled；可选 reminder 不拥有 Turn 终止权。
- Approval Presentation 快照覆盖工作目录内/外 Edit、Create、Overwrite、External Read、Command、Skill、Web Fetch 和 MCP。
- MCP List/Resource Read 的默认 Allow、MCP Call 的默认 Ask、MCP binding stale rejection 和不可信结果投影形成同一 Approval/ToolResult Contract。
- MCP Tool Catalog 的 server/tool identity、schema、read-only/parallel capability 和 revision 在 Model Step、Tool Prepare、Event、Rollout 与 Resume projection 中保持一致。
- Skill Catalog 只常驻 metadata；显式 Skill Injection、`read_skill` reference read、Skill Script attribution 和 Skill revision 在 ContextManager、StepContext、ToolResult 与 Resume projection 中保持一致。
- Skill Script 只能经 `execute_command`，其 Permission、Approval、ProcessManager、取消和 `write_stdin` 语义与普通 Command 完全一致，不存在第二个 Skill Executor。
- 默认 Tool Catalog、Prompt Snapshot、Registry、Event 和 Rollout 中均不存在 `apply_patch` 或 sandbox Tool。

### 27.5 TUI

- Slash Popup 键盘交互。
- Approval 上下键与 Enter。
- 中文输入和 Backspace。
- Working/Worked 计时和间距。
- ActiveHistoryCell 只提交一次。
- Working 只由 TurnStartedEvent/TurnCompleteEvent/TurnAbortedEvent 控制。
- retrying StreamErrorEvent 复用 status indicator 显示 `Reconnecting... n/m` 与 details，不生成 HistoryCell、不结束 draft；下一条非 retry live Event 恢复此前 status header。
- Replay/Resume 忽略 transient retry status；无颜色、窄终端和隐藏 status indicator 场景仍有稳定降级。
- Bubble Tea Task 返回不作为第二套 Turn 终态。
- Terminal 无颜色和窄宽度降级。
- Tool 展示使用真实 `TurnItem.ToolName`，不从 `action_summary` 或自然语言标题猜测工具身份。
- `read`、`grep`、`glob` 的探索树叶节点显示对应 Tool 名和必要参数；`execute_command` 显示 Codex 风格的 `Running`/`Ran` 与命令结果。
- `update_plan` 使用 Codex 风格的 Plan 展示，仅由 live `PlanUpdateEvent` 驱动，不进入普通 ToolHistoryCell，不生成通用 Tool Started/Completed activity，Resume 不恢复旧 checklist。
- Plan Mode 的 Proposed Plan 使用 `PlanDeltaEvent → completed PlanItem → ProposedPlanCell`；Turn 完成后只对 live completed Plan Item显示基础 implementation prompt，Replay 不恢复旧 Popup。
- `write`、`edit` 分别使用 Claude Code 风格展示目标路径、操作名称、变更统计和结构化 Diff 入口，不归入通用 `Ran`。
- Tool 行覆盖 queued、running、waiting approval、completed、failed、denied 和 partial 状态。
- Rich/Raw、Live/Replay 对持久化的 Completed Tool Item 生成一致的 Tool-specific HistoryCell；`PlanUpdateEvent` 等明确标记为 transient 的领域事件只保证 live Rich/Raw 一致，不伪造 replay item。
- `apply_patch` 不出现在 Tool Catalog、Event、Rollout 或 TUI 主链；生产遗留实现、注册和兼容测试直接删除，仅在设计文档中标记为遗留能力。

### 27.6 Provider

- Responses 与 Chat Completions Tool Call。
- 流式增量聚合。
- Developer role 降级。
- DeepSeek、GLM、Qwen 方言 fixture。
- 顶层 `model`/`model_provider` 与 `model_providers.*.wire_api` 使用 Codex 对齐的命名和所有权；Provider 中不存在 model、temperature、max output、context window 或 Tool Output limit。
- `model_reasoning_effort` 省略时普通 sampling 与 Compaction 都不发送客户端默认值；显式值冻结到 TurnContext/TurnContextItem，并在同一 Turn 的所有 continuation 中保持一致。
- OpenAI-compatible effort 主线由 Wire API 选择候选形状：Responses 使用 `reasoning.effort`，Chat Completions 使用 `reasoning_effort`；随后由 Dialect 按官方契约确认、覆写或拒绝。`standard` 覆盖 opt-in passthrough，OpenAI/DeepSeek/Qwen/GLM 覆盖各自已验证的官方字段，未知 endpoint 或不支持模型的 Provider 4xx 不被静默吞掉。
- DeepSeek/Qwen/GLM 覆盖默认 thinking、effort levels、Chat/Responses wire fixture、Chat `none` 的厂商关闭映射和 reasoning history；非 `none` effort 原样进入官方 effort 字段，不降级为简单 thinking enable，也不由客户端根据模型名预判 supported levels。
- Domain 不保留通用 `ReasoningConfig.Enabled/Preserve`；reasoning history 与 clear-thinking 属于 Dialect 自动协议行为。
- `model_context_window` 必须显式有效，`model_auto_compact_token_limit` 省略时派生为 90%，`tool_output_token_limit` 默认 `10000` 并只约束模型可见 Tool/Function Output。
- 普通 Responses、Chat Completions 和 Compaction Request 不发送 `temperature`、`max_output_tokens`、`max_tokens` 等用户稳定配置参数，使用模型厂商默认值。
- 所有用户定义 Provider 在省略连接恢复字段时统一得到 `request_max_retries=4`、`stream_max_retries=5` 和 `stream_idle_timeout=5m`；显式 `0` 必须保留为禁用 retry，不能被默认值覆盖。
- 旧 `max_retries` 迁移只影响 `request_max_retries`，schema/patch/merge/`config show`/validation 一致；当前配置中不存在未接线的 `websocket_connect_timeout`。
- request retry 与 stream reconnect 使用独立配置、计数和测试 fixture。
- retryable disconnect/idle timeout 发布 `WillRetry=true` 后成功恢复；retry exhausted 只发布一次最终错误并完成 failed Turn terminal。
- backoff cancellation、Retry-After、不可恢复错误、部分 Delta 后重连和无重复 ResponseItem/Tool Call。
- 普通 sampling、手动 compact 与自动 compact 复用同一 stream retry policy。
- timeout、取消、错误脱敏和 request/stream retry 边界。

## 28. 架构验收场景

Amadeus 至少通过以下真实场景：

1. 用户输入“你好”，不创建无关计划，不调用 Tool，快速返回。
2. 用户要求查看 `docs`，Agent 能读取大文件、控制 Context，并持续工作。
3. 用户要求修改文件，TUI 在写入前展示 Diff，选择 `No` 时零修改，选择 `Yes` 时精确写入。
4. 文件在 Approval 等待期间被外部修改，Agent 拒绝覆盖并重新读取。
5. 用户运行 `/plan`，Agent 进入持续 Plan Collaboration Mode，只分析和规划，不执行副作用；用户在模式内要求实现时仍只规划。
6. `/plan <task>` 通过单个带 Plan mode override 的 UserInputOp 启动 Turn，不依赖 TUI pending task；最终 `<proposed_plan>` 流式显示并以 completed PlanItem 持久化。
7. Plan Mode 在探索后通过 `request_user_input` 询问关键取舍，用户回答后同一 Turn 继续并产生 decision-complete 方案；Default Mode 也能在必要时复用同一协议。
8. 用户在 Proposed Plan 完成后选择实施，Runtime 以 Default mode override 创建后续普通 Turn，不在 Plan Turn 内直接执行。
9. 普通复杂任务中模型按需调用 `update_plan`，TUI 显示 Updated Plan，且该 checklist 不成为 Proposed Plan 或 SessionState。
10. 用户运行 `/compact`，历史被压缩但原始 Rollout 保留，后续对话不丢目标。
11. 用户中断 Turn 后输入“继续”，新 Turn 能看到中断事实并重新规划。
12. `execute_command` 完成后展示命令、输出和退出码，不伪造文件 Diff 或修改归因。
13. Provider、Context 或 Tool 失败时 TUI 明确显示错误，不静默卡死。
14. `/resume` 恢复 Session 后 Proposed Plan、Compaction 和 Tool 历史语义一致，但不恢复旧 request overlay、checklist 或 implementation popup。
15. Rich Inline TUI 在正常终端中完成完整 Runtime 流程。
16. 无 TUI/Cobra controller 的 Runtime fixture 可以直接 spawn Session 并独立完成 regular Turn，且 Interface 只观察一套 Session 终态。
17. 在 buffered append 后、flush 前模拟崩溃，SQLite 不包含未 durable 的 preview、title、usage 或 terminal metadata；backfill 后与 JSONL 一致。
18. Tool Result 在即时 Model Step、下一 Model Step 和 Resume 后保持 status/error/partial/metadata 语义一致。
19. Agent 首次进入带更深层 AGENTS.md 的目录时，副作用操作在新指令进入 Prompt 前不会执行。
20. 同一 Turn 中 MCP/Skill/Tool revision 变化后，下一 Model Step 使用新的 StepContext；旧 Tool Call 按 stale snapshot 明确失败而不是误路由。
21. MCP Server 在 lazy startup、refresh、disconnect/reconnect 和 shutdown 时不会泄漏 goroutine、进程或 pending request；失败以可见诊断或 ToolResult 结束。
22. MCP Tool Catalog 的 schema 和 read-only/parallel capability 在 List、Prepare、Execute、Event、Rollout 和 Resume 中保持一致；未发现或 stale Tool 不会被远程调用。
23. Skill Index 不包含完整正文；显式 `$skill-name` 才生成 SkillInjection，`read_skill` 只能读取受限 `SKILL.md`/`references/*`，路径逃逸被拒绝。
24. Skill Script 只能通过 `execute_command`，enabled Skill `scripts/` attribution 不扩大命令权限；脚本仍经过普通 Permission/Approval/Process lifecycle。
25. 相同只读调用或相同可恢复错误出现两次不会被 `run_turn` 强制终止；模型仍可调整方案并继续。
26. 进程在 ItemCompletedEvent 后、TurnCompleteEvent 前退出，Resume 仍能从 canonical ResponseItem 与 EventMsgItem 恢复已完成工作。
27. 模型 response stream 在 Turn 中断开时，TUI 显示可取消的 `Reconnecting... n/m` 和安全 details，不写入永久错误历史；恢复后继续同一 Turn，重试耗尽后产生明确最终错误和唯一 Turn 终态。
28. response stream 在部分 Assistant/Reasoning/Tool Call/Plan Delta 后断开并恢复时，transcript、canonical Rollout 和后续 Resume 均不出现重复文本、重复 Tool Call、重复 Proposed Plan 或 attempt-local draft。

## 29. 最终架构结论

1. Codex 是 Amadeus 的 Thread、Session、SessionServices、Turn、Context、SessionTask、`run_turn`、Slash Command 和 TUI 架构骨架。
2. Codex 的 StepContext/ToolRouter 与 Claude Code 的 Validate/Prepare/Permission/Approval/Execute 共同构成 Amadeus Tool 调用链；Claude Code 仍是文件修改、Diff Preview 和 Permission UX 的主要行为参考。
3. 默认 Agent 使用单一 Codex 风格 Turn continuation loop；`update_plan` 按需发布 transient checklist Event，普通 Tool Call/Result 进入模型历史，checklist 不参与 Resume 恢复。
4. `/plan` 是显式只规划不实施的 Collaboration Mode，复用同一 RegularTask、`run_turn`、Context 和 Event/Rollout 主链；`/plan <task>` 原子应用 mode override，正式方案使用 `<proposed_plan>`、PlanDeltaEvent 和 completed PlanItem。
5. `request_user_input` 是 Default/Plan 通用的独立交互 Tool；两种模式共享 Tool/Event/waiter/answer lifecycle，只由 Prompt 决定提问倾向，不复用 Approval 或 checklist lifecycle。
6. `/compact` 提交 CompactOp，由 CompactTask 调用 SessionServices.Compactor；自动压缩由 `run_turn` 调用同一 Compactor，并保留 canonical rollout。
7. 结构化文件修改遵循 Read → Diff Preview → Approval → Revalidate → Atomic Apply → Verify。
8. `execute_command` 默认 Ask，经 Session 精确规则复用授权后直接在宿主执行；不解析任意命令的完整路径副作用。
9. Prompt 构造以 Codex 的 `Prompt`、`BaseInstructions`、`ResponseItem`、`ToolSpec`、`ModelMessages`、`WorldState` 和 Collaboration Mode 语义为唯一目标；不保留旧 Prompt 装配兼容层。
10. 普通/Plan/Compact Prompt 使用 Codex 对应机制与 Prompt 资产；Claude Code 只提供 `read`、`edit`、`write`、`glob`、`grep` 的 Tool Guidance，Codex 提供 `update_plan`、`write_stdin` 和命令续接 Guidance。
11. MCP 以 Session-owned `MCPRuntime`、稳定的 `MCPBinding`、lazy `ToolCatalog`/`ResourceCatalog` 和 StepContext snapshot 为唯一生产主链；基础版不复制 Codex 的 OAuth、Elicitation、Plugin 和 Remote Connector 复杂度。
12. Skill 以 `SkillCatalog`、`SkillMetadata`、`SkillInjection` 和 Resource Boundary 为唯一生产主链；正文渐进式披露，references 按需读取，scripts 统一经 `execute_command`，不建立独立 Skill Executor。
13. JSONL typed RolloutItem 是完整 durable history 的唯一事实；SQLite StoredThread 只保存可重建 metadata/index，旧格式数据直接删除重建。
14. ThreadManager 是 Thread 创建和恢复入口；LiveThread → ThreadStore → LocalThreadStore 是唯一持久化链。
15. AmadeusThread 是 Interface 唯一 Runtime 句柄；TUI 通过它提交带 ID 的 Submission、等待 UserMessageAdmission、消费 Event/EventMsg，并分别以 ApprovalDecisionOp、UserInputAnswerOp 回答 ApprovalRequestEvent、RequestUserInputEvent。
16. TurnItem 是 EventMsg 和 HistoryCell 的稳定业务项；需要恢复的完成态以 EventMsgItem(ItemCompletedEvent) 原样持久化，Delta 只服务实时更新。
17. Slash Command 分为 TUI Local、Application Action 与 Core Op，不直接拥有 Runtime 或持久化状态。
18. internal Session 是 SessionTask、ActiveTurn、ContextManager、Event Delivery 和终态收尾的唯一所有者；ContextManager 在运行期增量记录，Resume 时仅重建一次。
19. 生产 SessionTask 由 Session 直接创建和执行，不反向调用 CLI/Application executor；SessionServices 直接拥有全部 capability。
20. canonical Rollout 使用 SessionMetaItem、ResponseItem、CompactedItem、TurnContextItem 和 EventMsgItem 等 typed contract。
21. Provider request retry 与 response stream reconnect 是独立生命周期；Turn-scoped ModelClientSession/Core retry helper 拥有 retry 决策，TUI 只投影 typed transient StreamErrorEvent。
22. `Reconnecting... n/m` 复用 Codex 风格 status indicator，retrying 不进入 History/Rollout、不结束 Turn，下一条非 retry live Event 恢复先前 status，Resume 不重放瞬态状态。
23. StepContext 持有 immutable ToolRouter；同一 snapshot 同时提供模型 specs 与 exact ToolDefinition/MCP dispatch，不在执行时重新查询 mutable registry。
24. AgentsMdManager/LoadedAgentsMd、SkillCatalog 和 MCPRuntime 分别直接归 SessionServices 所有，不存在 generic Extension/Instruction assembly。
25. Amadeus 处于开发阶段，不提供旧配置、旧 Protocol、旧 Rollout、旧 SQLite schema 或旧 API 兼容；架构替换直接删除旧实现与兼容测试。
26. 普通用户消息只有一个 UserInputOp；无 ActiveTurn 时 admission 为 Started，Active Regular Turn 时为 Steered，显式 strict steer 使用 ExpectedTurnID 防止错误注入。
27. Steered input 由 ActiveTurn TurnState 中的 TurnInputQueue 按 FIFO 保存，在当前 Model Step、Tool 和必要 compaction continuation 后进入 canonical history 并触发同一 Turn follow-up；它不创建第二个 Turn lifecycle，也不复用 Approval 或 request_user_input。
