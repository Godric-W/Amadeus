# Amadeus 架构设计

> 状态：Target Architecture v2
> 最近修订：2026-08-28
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

- `cmd/amadeus` 只作为进程入口：建立 process context/标准流、调用 `internal/cli` 并映射进程退出码；它不定义 Cobra command tree、不装配 Runtime，也不拥有 Workspace、Turn executor、Event pump、Context history、Session capability 或 Turn 终态协调逻辑。
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
| D. Event + TUI | Submission、Event/EventMsg、TurnItem、HistoryCell、Rich TUI Projection |
| E. Slash Command | Codex 风格 SlashCommand、InputResult、单一 TUI 分发与 Application/Session 操作 |
| F. Agent Engine | Codex 风格 Turn continuation loop、StepContext、Plan Tool、Plan Mode、中断与终态 |
| G. Runtime Architecture Convergence | SessionServices 所有权归位、Codex 术语收敛、Task/run_turn 主链与混合 Tool 边界 |
| H. Extensions + Release | MCP、Skill、Web、迁移清理与发布验收 |
| I. Prompt Construction + Optimization | Codex Prompt 数据模型、ModelMessages、WorldState/Collaboration Mode、Prompt 资产迁移与缓存/Token/Contract 验证 |
| J. Slash Command + TUI Application Lifecycle Alignment | Active Thread Attachment、canonical replay、typed command lifecycle 与专用 HistoryCell |
| K. Response Stream Reconnect Lifecycle Alignment | Provider retry 分层、ModelClientSession 重连、typed transient error 与 TUI 状态恢复 |
| L. Model + Provider Configuration Ownership Alignment | Provider transport、ModelInfo、runtime override 与 token policy |
| M. Codex Architecture Realignment | Protocol identity、typed Rollout、SessionState/Services、SessionTask、StepContext 与 legacy cleanup |
| N. Runtime Coordination Tools + Plan Mode Alignment | `update_plan`、`request_user_input`、Collaboration Mode 与 Proposed Plan lifecycle |
| O. Same-Turn User Input + Turn Steer Alignment | UserMessageAdmission、TurnInputQueue、same-Turn continuation、client message identity 与 TUI steer UX |
| P. Model Reasoning Effort + Provider Thinking Contract | ReasoningEffort、Turn freeze、Provider Dialect 与 wire mapping |
| Q. Web + View Image Tool Contract Closure | pinned Web transport、typed Web result、image preparation、modality budget 与 ViewImageCell |
| R. Basic Multi-Agent Alignment | Codex V1 风格 AgentControl、SubAgent Thread、协作 Tool、Prompt、Event/Rollout 与 TUI projection |
| S. Codex-style TUI Architecture Alignment | Session snapshot、fixed statusline、Footer state、workspace metadata 与 exit lifecycle |
| T. Thread + Session UUID Identity Alignment | UUIDv7 ThreadID、SessionID/ThreadID 语义、创建/恢复生命周期、Persistence 与 Resume boundary |
| U. Next-Turn User Input Queue Alignment | Codex 风格 Composer queue、下一 Turn FIFO、terminal drain、失败恢复、attachment isolation 与 pre-enqueue Footer hint |
| V. Versionless Config Schema + Example Naming | 删除顶层 version gate、严格当前 schema、`config.yaml.example` 模板与旧配置零兼容 |
| W. Context Accounting + Compaction Realignment | TokenUsageInfo、active context、结构化 TokenEstimator、手动/自动 Compaction、atomic install 与 live/Resume 等价 |
| X. CLI + Bootstrap Package Architecture | thin process entry、multitool dispatch、bootstrap composition 与外层 package 拆分 |
| Y. Initial Prompt + Single TUI Frontend Alignment | Codex `PROMPT → initial_user_message → normal user-message submission`、单一 TUI frontend 与 startup/replay gating |
| Z. Internal Package + Source Layout Alignment | Codex-aligned Protocol、Session、ContextManager、ThreadStore、ThreadManager、TUI 与 Tool package ownership；Go 文件按内聚行为拆分 |
| AA. Prompt Ownership + Lifecycle Realignment | Codex model instructions、Session Base provenance、Default/Plan、source-specific ToolSpec、typed WorldState full/diff、Prompt wire mapping、Compact 与 Resume lifecycle |
| AB. Source-backed Markdown Streaming + TUI Render Lifecycle | Codex `MarkdownStreamCollector/StreamingRender/StreamController/AgentMarkdownCell` ownership、authoritative completion、Goldmark/Chroma structured render 与 Bubble Tea model-owned transcript viewport 适配 |
| AC. Basic Multi-Agent Terminal + Persistence Lifecycle Realignment | Codex V1 `last_agent_message`、AgentStatus/Wait、root-tree lifetime、durable spawn edge、budget finalization 与 live/Resume 等价 |

## 2. 产品目标

Amadeus 的目标是成为一个真正可用于日常软件开发的通用 Coding Agent：

- 用户直接运行 `amadeus` 进入交互会话。
- 用户也可以运行 `amadeus "<prompt>"` 启动同一交互 TUI；TUI 在 active Thread 已配置、初始历史已恢复并完成首次界面装配后，将 Prompt 作为 pending `UserMessage` 通过正常用户消息链提交，完成后继续停留在会话中。
- Agent 能探索项目、编辑文件、执行命令、验证结果并解释修改。
- 默认使用 Codex 风格的单一 Turn continuation loop。
- 简单任务可以直接执行；复杂任务可以通过 `update_plan` 维护可见计划。
- `/plan` 进入与 Codex 对齐的显式 Plan Mode，用于分析和规划，不实施文件或命令副作用。
- Default 与 Plan Mode 都可以通过 `request_user_input` 在当前 Turn 内请求结构化用户输入并继续执行；用户提问是独立交互能力，不属于 Approval。
- 用户可以在 Regular Turn 运行期间继续提交普通消息；Runtime 将其作为 steer input 接纳到同一 Turn，并在当前 Model Step 后继续，而不是静默排队成下一 Turn。
- TUI 运行期间，用户也可以用显式 Tab queue 动作把普通文字暂存为后续新 Turn；该输入在当前 Turn terminal 前不提交 Runtime，也不进入 canonical history。
- Root Agent 可以把边界清晰、可独立推进的探索任务交给 SubAgent；SubAgent 使用完整 Thread/Session/runtime 主链并与 Root 共享工作区，但拥有独立 Context、Turn、Tool 状态和 canonical Rollout。
- `/compact` 调用正式的上下文压缩服务，而不是仅清空 TUI 文本。
- 文件修改默认对能力较弱或不稳定的模型保持安全：先生成 Diff，再由用户确认，最后写入。
- Runtime 不依赖 TUI，未来可以被其他界面复用。

## 3. 参考职责矩阵

| 能力域 | 主要参考 | Amadeus 取舍 |
|---|---|---|
| Thread、Session、Turn | Codex | AmadeusThread、internal Session、ActiveTurn 与 SessionTask 使用同构生命周期 |
| Agent Runtime | Codex | Session、SessionState、SessionServices、TurnContext、StepContext、SessionTask 与单一 `run_turn` continuation loop |
| Multi-Agent | Codex 为骨架、Claude Code 为能力过滤参考 | SubAgent 是完整 AmadeusThread/Session；同一Root tree共享AgentControl，Basic实现采用Codex V1的`last_agent_message`、AgentStatus、wait、completion watcher与显式close edge lifecycle，并使用Claude Code风格child Tool allowlist、独立后台取消域、独立上下文与权限不升级原则 |
| Plan | Codex | `update_plan` 是 transient 软 checklist Event；`/plan` 是 Collaboration Mode；最终方案使用 `<proposed_plan>`、`PlanDeltaEvent` 与 completed Plan TurnItem |
| Context Manager | Codex 为骨架 | 统一历史投影；区分 Thread 累计 Token 消耗、最近请求 Usage、当前 active context 与 preflight estimate |
| Compaction | Codex | Session 拥有 trigger/reason/phase、Item lifecycle、durable replacement install 与 token recompute；CompactionService 只生成 typed output |
| Prompt Assembly | Codex | Prompt、BaseInstructions、ResponseItem、ToolSpec、ModelMessages、WorldState、CollaborationModeState 和 role-aware ContextFragment 使用 Codex 同构职责；不保留旧 Prompt 构造双轨 |
| Default/Plan/Compact Request Prompt | Codex | Default/Plan 是仅有的两个 Collaboration Mode，共享 Session BaseInstructions；Compact 是独立 request kind，使用 Codex summarization prompt 与 summary prefix |
| Tool 路由与内层协议 | Codex + Claude Code | StepContext 捕获本次 ToolRouter/ToolSet；ToolExecutionService 执行 Validate、Prepare、Permission、Approval、Execute 和 Typed ToolResult |
| Claude-style 文件 Tool | Claude Code | `read`、`edit`、`write`、`glob`、`grep` 的模型指引和使用边界以 Claude Code 对应 Tool Prompt 为准 |
| Codex-style Runtime Tool | Codex | `update_plan`、`request_user_input`、`execute_command`/`write_stdin`、`view_image`、Multi-Agent 和 MCP Resource Tool 以 Codex ToolSpec/Contract 为骨架，再适配 Amadeus 实际 schema |
| 用户输入 Tool | Codex 为架构、Claude Code 为 UX 参考 | `request_user_input` 使用独立 Request Event/Answer Op/Session waiter；稳定 Question ID、Default/Plan 通用，并选择性吸收多选与 Other 体验，不复用 Approval `updatedInput` |
| Turn Steer | Codex | 普通 `UserInputOp` 通过 Started/Steered admission 接纳；ActiveTurn 持有 TurnInputQueue，`run_turn` 在 Model Step 边界 drain 并继续同一 Turn |
| Next-Turn Queue | Codex | Tab queue 是 TUI/ChatWidget 等价层的 transient FIFO；terminal 后才通过普通 `UserInputOp` 启动下一 Turn，不新增 Core queue Op 或 durable history |
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
- Codex Multi-Agent V2及其AgentPath、task-name tree、mailbox、`send_message`/`followup_task`、residency/eviction和通用AgentGraphStore；Claude Code team/teammate、worktree/remote/background task体系；history fork、write-capable worker、child Approval/用户输入和完整agent picker。Amadeus只实现第21章的depth-one read-only Basic Multi-Agent闭环，这些能力不是后续路线图。

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
│ CLI · Rich TUI                                           │
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
cmd/amadeus/                 仅包含可执行程序进程入口
internal/cli/                Cobra 参数模型、顶层命令分发、CLI 输出与退出语义
internal/bootstrap/          环境/路径解析及 concrete Adapter、ThreadManager、ThreadWorkspace 装配
internal/app/                Application Services
internal/protocol/           Identity、Submission、Event、EventMsg、TurnItem DTO 与稳定 lifecycle enums
internal/protocol/identity/  SessionID、ThreadID、TurnID、SubmissionID、RequestID 与 ItemID
internal/threadstore/        ThreadStore、LiveThread、InitialHistory、StoredThread metadata 与恢复逻辑
internal/threadstore/local/  LocalThreadStore、JSONL writer、metadata projection 与 index coordination
internal/threadstore/local/sqlite/ 当前 schema 的 SQLite metadata index adapter
internal/threadmanager/      ThreadManager、AmadeusThread、child restore 与 capability query
internal/agent/session/      Session、SessionState、SessionServices、TurnContext、StepContext、ActiveTurn、RunningTask、SessionTask 与 run_turn
internal/agent/compact/      Compaction domain、Prompt request builder 与无状态摘要生成服务
internal/agent/modelclient/  Turn-scoped ModelClientSession、sampling、stream consume 与 reconnect policy
internal/agent/multiagent/   Root-scoped AgentControl、reservation、terminal reducer、wait、send、notification delivery 与 shutdown
internal/contextmanager/     ContextManager、Token Accounting、Prompt estimate、Projection 与 replacement validation
internal/prompt/             模型消息 profile、Collaboration Mode、Compaction 与内置 Prompt 资产
internal/llm/                LLM Domain Port
internal/llm/openai/         OpenAI-compatible Adapter
internal/tool/               ToolDefinition、ToolRegistry、ToolRouter、ToolUseContext、ToolExecutionService 与 PermissionService
internal/policy/             ApprovalPort、ApprovalCoordinator、SessionPermissionContext 与静态策略
internal/tool/builtin/       Read、Edit、Write、Glob、Grep、Command 等内置 Tool
internal/tool/textdiff/      unified text diff 与 changed-line statistics
internal/process/            Host Process Runner
internal/rollout/            typed RolloutItem、JSONL 编解码与当前格式加载
internal/tui/                Codex 风格单一 Rich TUI、pending initial UserMessage、active attachment、交互与 renderer 生命周期
internal/agentsmd/           AgentsMdManager、LoadedAgentsMd、发现与作用域合并
internal/mcp/                MCP Config、MCPRuntime、Binding 与 Tool/Resource Catalog
internal/skill/              Skill Catalog、Metadata、Injection 与 Resource Boundary
internal/config/             配置加载、校验、脱敏
internal/prompt/builtin/     embedded Prompt assets、revision 与 validation
internal/audit/              Tool/command audit domain port 与本地 sink
internal/buildinfo/          build/version metadata
internal/filechange/         structured file change preview/result DTO
internal/imageprep/          bounded image decode、resize、re-encode 与 prepared image model
internal/logging/            slog 初始化与配置映射
internal/project/            Project Root、PathResolver 与 FileSystemPolicy
internal/workspace/          bounded file read、enumeration、ignore/glob 与 text detection
internal/webfetch/           URL safety、pinned transport、redirect 与 Markdown projection
internal/websearch/          Provider-neutral search service 与 provider adapters
internal/architecture/       package dependency 与 ownership contract tests
internal/integration/        test-only end-to-end harness；不提供生产 runner
internal/testutil/           跨 package 测试 identity/helper；生产代码不得依赖
```

### 7.1 Package 内源码组织规则

目录负责表达稳定的领域或适配器边界，文件负责表达该目录内的一项内聚职责。为避免持续重构后重新形成“历史聚合文件”，源码遵循以下约束：

- 不为单纯缩短文件而新增 package；只有出现独立依赖方向、生命周期或可替换适配器边界时才拆目录。
- `cmd/<name>` 默认只保留 `package main` 的进程入口；可复用、需要独立测试或拥有启动/关闭生命周期的 CLI 行为进入 `internal` package，不能因为最终产物是一个二进制而堆在 `package main`。
- 同一 package 内优先按行为拆文件，文件名直接表达职责，例如 `approval_request.go`、`approval_decision.go`、`application_events.go`。
- 文件名不重复 package 已经表达的无效上下文：`internal/cli/config.go`、`sessions.go`、`tools.go` 优于统一追加 `_command.go`；只有 `config_flags.go`、`event_processor.go`、`config_output.go` 这类后缀确实区分同 package 内职责时才保留。
- Tool 实现按用户可见 Tool 拆分；多个 Tool 共用的安全写入、Diff、Approval 与 revalidate 流水线放入明确命名的共享文件。
- Tool 外层目录同时遵循 Codex 与 Claude Code：`internal/tool` 只拥有 schema normalization、registry/router、request-scoped snapshot、并发 orchestration 和 Validate/Prepare/Permission/Approval/Execute lifecycle；`internal/tool/builtin` 按用户可见 Tool 分文件；`internal/policy` 独立拥有权限与 Approval。generic Tool package 不枚举 `read`、MCP、Multi-Agent 等具体产品工具名，具体 presentation 归 builtin、Session Tool Event 或 TUI HistoryCell owner。
- 不照搬 Claude Code 的一 Tool 一目录布局。只有单个 Tool 已形成独立依赖、多个实现文件或可替换 adapter boundary 时才拆 subpackage；当前 `read_file.go`、`edit_file.go`、`write_file.go` 等 Go 文件是等价的内聚单元。
- TUI Application 的状态与生命周期、Bubble Tea 更新、Runtime Event 投影和 View 渲染分别组织，不把业务事件归约与字符串渲染重新合并。
- `application.go`、`manager.go`、`service.go`、`state.go`、`types.go` 等通用文件名只在内容确实覆盖 package 核心模型时使用；混合 constructor、event pump、query、render、persistence 或 adapter 行为时必须拆为责任名文件。
- 测试与被测 package 共置；架构约束测试必须扫描同一主链的全部拆分文件，不能只检查历史入口文件。
- 架构约束测试按 Runtime、Protocol/Persistence、Tool/Approval、TUI 和 CLI/Bootstrap 分文件；不得让单个 guard 文件成为记录历次迁移 symbol 的第二份进度文档。
- 约 400–500 行是需要重新审视职责的软阈值，不作为机械拆分标准；Runtime 状态机或协议编解码在保持单一职责时可以超过该阈值。

#### 7.1.1 Codex/Claude Code 目录对齐规则

目录对齐以参考源码的 owner 和依赖方向为准，不按 Rust crate 或 TypeScript workspace 逐字翻译：

| 参考边界 | Amadeus 目标边界 | 约束 |
|---|---|---|
| Codex `codex-protocol` | `internal/protocol` | 是 CLI、TUI、Runtime、Rollout 和 Adapter 共同使用的低层 contract，不嵌套在 `agent` 下 |
| Codex core `session` + private `state` | `internal/agent/session` | Go 使用同一 owner package 的责任文件表达 SessionState、Services、TurnContext、StepContext、ActiveTurn 和 RunningTask，不建立会反向依赖 Session 的 `agent/state` package |
| Codex core `client`/ModelClientSession | `internal/agent/modelclient` + `internal/llm` | modelclient 只拥有 Turn-scoped sampling/stream/reconnect；Provider-neutral request/response 留在 `llm`，Provider wire adapter 留在 `llm/openai` |
| Codex core `context_manager` | `internal/contextmanager` | package path 与 package name 一致；唯一拥有 canonical history projection、Prompt snapshot、token estimate 和 replacement install validation |
| Codex `thread-store` | `internal/threadstore` | Thread persistence port、LiveThread、StoredThread metadata、local writer 和 SQLite index 属于同一 store 边界 |
| Codex core `ThreadManager`/`CodexThread` | `internal/threadmanager` | 创建/恢复 internal Session 和 live registry，不嵌套在 persistence package 下 |
| Codex core `agent/control`、`agent/status`、V1 handlers | `internal/agent/multiagent` + `internal/protocol` + `internal/rollout` | AgentControl拥有root-tree control plane，Protocol拥有AgentStatus/LastTurn DTO，Root rollout拥有flat spawn-edge lifecycle；ThreadManager仍是runtime spawn/resume owner |
| Codex `tui` 的 App/ChatWidget/history_cell modules | `internal/tui` | 保持一个 Go package 共享 Bubble Tea model；用责任文件表达私有 module，不为缩短文件制造 renderer/widget 小 package |
| Codex core `tools` + Claude Code `Tool`/tool orchestration/permissions/builtin tools | `internal/tool` + `internal/policy` + `internal/tool/builtin` | Codex 决定 StepContext/ToolRouter/Event owner，Claude Code 决定 Validate/Prepare/Permission/Approval/Execute 与文件 Tool 行为 |
| Codex `rollout` | `internal/rollout` | 只拥有 canonical typed item 与 JSONL codec/recorder，不重新拥有 Protocol identity 或 Thread metadata query |

以下目录不属于目标架构：含义混杂的 `internal/agent/engine`、单类型 `internal/agent/turn`、嵌套公共协议 `internal/agent/protocol`、含义过宽的 `internal/state`、只有一个 frontend 的 `internal/interface`、无生产调用方的 `internal/sandbox`，以及空的 `internal/agent/plan`/`internal/agent/task`。迁移必须切换全部生产与测试调用方后直接删除旧目录，不保留 alias、wrapper 或 import-forwarding compatibility package。

`internal/app/transcript` 也不作为 Application domain：live Event reducer、active cell bookkeeping 和 replay-to-HistoryCell state 归 `internal/tui`。`internal/app` 只拥有界面无关的 ThreadWorkspace、attachment/event pump 和 Application use case orchestration。

生产依赖方向固定为：

```text
cmd/amadeus
└─→ internal/cli
    ├─→ internal/tui
    └─→ internal/bootstrap（sessions 等 command-only query）

internal/tui
└─→ internal/bootstrap + internal/app + internal/protocol

internal/bootstrap
├─→ internal/app + internal/threadmanager
└─→ infrastructure adapters
    → internal/threadstore + internal/llm/openai + internal/mcp

internal/threadmanager
├─→ internal/agent/session
└─→ internal/threadstore

internal/agent/session
→ internal/protocol + internal/contextmanager + internal/agent/modelclient
  + internal/tool + internal/policy + internal/threadstore + capability domain ports
```

Domain/Runtime 不依赖 infrastructure adapter 或上层具体 controller/model；adapter 依赖并实现 domain port，再由 `internal/bootstrap` 的窄构造函数装配。`bootstrap` 可以引用 concrete adapter，但不能成为通用 Service Locator 或执行 owner：不暴露 `Get/Resolve(kind)`、任意 callback map 或包含所有测试注入点的万能 Runtime bag。若 Runtime 只能通过回调 `internal/cli` 或 TUI 才能完成 Turn，即使类型位于目标目录，也视为架构未迁移完成。

目标关键文件布局：

```text
cmd/amadeus/
  main.go                    process context、标准流、cli.Run 与进程退出码

internal/cli/
  execute.go                 顶层 CLI 生命周期和错误/退出映射
  root.go                    Cobra root command 与 subcommand dispatch
  agent.go                   可选 PROMPT、Thread target、项目参数归一化并唯一分发到 TUI
  config.go                  config 子命令
  config_flags.go            config override 参数模型
  config_loader.go           effective/configured config 加载与 CLI override 合并
  config_output.go           config show/explain 呈现
  project_flags.go           CWD/add-dir 参数解析
  session_flags.go           new/latest/resume/select 参数解析
  sessions.go                Session catalog command 与输出
  tools.go                   Tool catalog command
  web.go                     Web capability check command
  version.go                 Version command
  exit.go                    CLI 错误呈现和 exit status

internal/bootstrap/
  environment.go             AMADEUS_HOME、启动 CWD 与平台路径解析
  workspace.go               ThreadStore → ThreadManager → ThreadWorkspace 装配及失败清理
  adapters.go                Provider/MCP/Web/Prompt/Audit 等 Session 外部 Adapter 装配
  audit.go                   audit sink 路径与打开
  thread_store.go            LocalThreadStore 构造
  thread_catalog.go          Session catalog 查询和临时 Manager 关闭

internal/app/
  interactive_application.go Application 类型、constructor、Start 与公共 port
  interactive_commands.go    Submit/Interrupt/Approval/UserInput 等 typed command API
  interactive_threads.go     Resume、Clear、Rename、Delete 与 transactional switch
  interactive_attachment.go  唯一 SessionIo event pump、snapshot install 与 generation gate
  interactive_capabilities.go Session/Status/MCP/Skill query orchestration
  interactive_history.go     persisted ResponseItem/EventMsgItem 的 canonical replay 投影
  thread_workspace.go        当前 Thread 选择、切换、恢复、重命名和删除生命周期

internal/agent/session/
  session.go                 Session、SessionIo、SessionState 与 constructor
  session_loop.go            submission/request/completion serial loop
  submission.go              Op validation、admission 与 dispatch
  turn_start.go              ActiveTurn 创建、RunningTask 启动与 watch
  turn_context.go            TurnContext、ModeKind 与 Personality
  step_context.go            request-scoped Model/ToolRouter/Skill/Permission/SubAgent capability capture
  step_capture.go            capability capture → WorldState sync sequencing
  world_state.go             StepContext-owned facts → typed WorldState sections/fragments
  prompt_assembly.go         WorldState record后组装Prompt shape与immutable PromptSnapshot
  initial_input.go           pre-turn compact与首次context/user/explicit Skill顺序
  skill_context.go           Skill index渲染与typed SkillInjection canonical append
  prompt_diagnostics.go      Base/WorldState/wire/asset revision只读诊断投影
  services.go                SessionServices 类型、capability fields 与 close state
  service_builder.go         Session capability construction 与失败逆序清理
  persistence.go             canonical append、durability receipt 与 ContextManager record
  context_snapshot.go        Prompt/Context/Token snapshot query
  interaction.go             Event/EventMsg 发布与 correlated Approval/User Input waiter
  context_window.go          active/preflight Token status、auto-compact policy 与 typed limit failure
  compaction.go              trigger/reason/phase、source capture、durable install 与 token recompute

internal/agent/modelclient/
  session.go                 ModelClientSession、Sample/Complete request 与 result
  response_stream.go         stream consume、idle timeout 与 chunk aggregation
  response_retry.go          reconnect classification、backoff 与 typed transient Event

internal/contextmanager/
  manager.go                 ContextManager state、constructor 与 public mutation boundary
  history.go                 canonical Rollout record、validation 与 projection state
  prompt_snapshot.go         model-visible Prompt snapshot 与 revision
  output_projection.go       modality filtering、Tool output bounds 与 token truncation
  world_state.go             role-aware ContextFragment 与 WorldState revision

internal/threadmanager/
  manager.go                 ThreadManager registry、start/resume/shutdown 与 close
  amadeus_thread.go          AmadeusThread handle、Submission/Event/History API
  capabilities.go            Skill/MCP/Permission/Prompt diagnostics capability query
  child_resume.go            persisted child restore 与 parent notification

internal/threadstore/local/
  store.go                   LocalThreadStore type、constructor 与 close
  writer.go                  materialize/open/append/flush/close writer lifecycle
  metadata.go                metadata projection、query、rename 与 archive
  index.go                   SQLite index reconciliation/rebuild

internal/agent/compact/
  contract.go                CompactionSource/Request/Output，复用 Protocol trigger/reason/phase
  service.go                 Codex compact Prompt request 和 typed summary/replacement 生成

internal/tui/
  run.go                     terminal preflight、Prompt→UserMessage、InteractiveApplication/TUI Application 启停与 AppExitInfo
  user_message.go            TUI UserMessage、initial message 构造与 submission DTO
  application.go             TUI Application、Bubble Tea model、pending initialUserMessage 与生命周期装配
  input_queue.go             QueuedUserMessage FIFO、in-flight admission 与 attachment scope
  application_update.go      Bubble Tea Update 与输入状态转换
  application_events.go      EventMsg/TurnItem reducer 与 Turn lifecycle
  history_state.go           active/completed HistoryCell、source-backed stream 与 recall state
  application_view.go        top-level View composition 与 terminal content cleanup
  composer_view.go           input box、textarea window、popup 与 Footer
  transcript_view.go         transcript viewport、active stream/cell 与 completed history flush
  application_commands.go    Slash Command 分发
  application_selection.go   Session / Skill 选择流程
  history_cell.go            HistoryCell/ActiveHistoryCell contracts
  history_messages.go        User、streaming Assistant、source-backed Assistant/Plan cells
  history_cell_session.go    source-backed SessionHeader/Logo/metadata HistoryCell
  history_notices.go         Notice/Info/Warning/Error cells
  history_render.go          Rich/Raw line projection 与 semantic style
  markdown_source.go         exact source、frozen CWD与临时parse source
  markdown_parse.go          Goldmark top-level source offset/reference analysis
  markdown_render.go         Goldmark AST writer、Chroma projection与typed lines/spans
  markdown_wrap.go           display-width word/grapheme wrap与span/link remap
  markdown_stream.go         newline-gated raw source collector
  markdown_stream_host.go    Assistant/Plan controller与deferred projection FIFO owner
  streaming_render.go        stable top-level blocks 与 mutable final block render state
  stream_controller.go       per-Item Assistant/Plan controller、StreamCore stable queue/tail、reset/finalize
  transcript_surface.go      HistoryCell、stream attachment range与final replacement
  transcript_viewport.go     mutable active-frame viewport、page scroll与anchor
  markdown_render_cache.go   finalized source render cache key 与失效边界
  markdown_tables.go         Goldmark table projection 与 output bounds
  markdown_links.go          CWD-aware local/web link typed span projection
  history_cell_tools.go           Tool activity reducer 与 generic fallback
  history_exec.go            execute_command projection
  history_explore.go         read/glob/grep projection

internal/architecture/
  runtime_guard_test.go      Session/Context/Compaction/ModelClient dependency guards
  protocol_guard_test.go     Protocol/Rollout/Identity/Persistence guards
  tool_guard_test.go         Tool/Approval/Permission lifecycle guards
  tui_guard_test.go          TUI/History/Queue/Status/initial Prompt guards
  legacy_guard_test.go        CLI/Bootstrap/thin main guards

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

`internal/cli` 对齐 Codex `cli` 的 multitool dispatcher，拥有顶层 command tree、共享 flags 和 command dispatch。根命令的可选 positional 参数命名为 `PROMPT`；无 Agent 子命令时，不论 Prompt 是否存在都只进入同一个 TUI frontend。Amadeus 当前基础范围没有独立的非交互 Agent frontend；未来若增加该产品形态，必须使用显式命令并单独定义输入、Approval、`request_user_input`、事件消费和完成 Contract。

`internal/tui` 对齐 Codex TUI/ChatWidget 边界，拥有 terminal preflight、active Thread attachment、pending initial `UserMessage`、Approval/UserInput overlay、normal user-message submission、renderer drain 和 `AppExitInfo`。`internal/app.InteractiveApplication` 继续拥有界面无关的 active Thread attachment 与唯一 SessionIo event pump；它不保存尚未提交的 initial message。CLI、bootstrap、Application、Session 和 Rollout 都不得并列保存 pending Prompt mirror。

外层数据模型固定为：

- CLI flag struct 只表示 Cobra 边界的原始用户输入；CLI 的 TUI dispatch helper 与 Codex `run_interactive_tui` 一样只执行 CRLF/CR→LF 归一化，再把根 positional `PROMPT` 放入 `tui.RunOptions.Prompt`。它不 trim/rewrite Prompt、不直接转换为 `UserInputOp`，也不跨入 Session Runtime。
- 不建立同时携带 `Mode`、Project、Task、`EventSink`、`ApprovalPort`、输入输出流和 Session 状态的通用 `agentInvocation`。根 Agent launch 只包含 Prompt、Project/WorkspaceRoots、typed Thread target、picker intent 和终端 streams。
- 不保留 `commandRuntime`、`agentController` 或等价的万能 dependency/controller aggregate。默认依赖由各 owner 的明确 constructor/options 注入；测试替身跟随 CLI、bootstrap、Application 或 TUI 的真实边界放置。
- Thread 当前选择继续唯一归 `internal/app.ThreadWorkspace`。bootstrap 只构造并交出它；TUI invocation 只持有一个 ThreadWorkspace；CLI dispatcher 和 `cmd/amadeus` 均不保存 current Thread mirror。
- CLI 的 `--continue`/显式 `--resume=<ThreadID>` 可以与 PROMPT 组合：先完成 latest/explicit Resume 和 canonical replay，再提交 initial UserMessage。无 ID 的 `--resume` 只打开 TUI Session picker且不同时接收 Prompt，避免在 target 未确定时把消息提交到临时 draft。
- positional Prompt 是普通 UserMessage，不经过 Slash Command parser；`amadeus "/compact"` 不等价于在 Composer 中执行 `/compact`。只有用户在活动 Composer 中提交的 Slash invocation 才进入 Slash Command dispatch。

启动和关闭顺序固定为：

```text
main process context/streams
→ cli parse + dispatch
→ tui.Run(Prompt, ThreadTarget)
→ bootstrap resolve config/environment + open Store/Manager/Workspace
→ new/latest/explicit-resume configured barrier
→ InteractiveApplication.Start: canonical history projection + active attachment
→ TUI appModel installs snapshot/replay + pending initial UserMessage
→ startup-ready → normal submitUserMessage/admission lifecycle
→ typed terminal/AppExitInfo
→ stop event pump/Application
→ bounded Workspace/ThreadManager/Store close
→ CLI summary/error/exit status
→ process exit
```

启动中任何一步失败都按已经取得资源的逆序关闭；不得把关闭责任留给 `main` 猜测。TUI Application 完成 renderer drain、terminal restore 和 shutdown，再把 `AppExitInfo` 交给 CLI 呈现；CLI 不在 Runtime 关闭后重新查询状态。初始 Prompt 尚未提交时属于 TUI transient intent，不写入 Rollout；提交成功后与 Composer 消息共享唯一 UserInputOp、admission、canonical UserMessage 和 terminal lifecycle。

测试随 owner 迁移：command/flag/output 单元测试位于 `internal/cli`，composition failure 和资源关闭测试位于 `internal/bootstrap`，initial UserMessage、optimistic/admission、picker、Resume replay 和 Approval/UserInput 测试位于 `internal/tui`。Provider/Tool 跨层 E2E 使用明确的 test-only integration fixture，通过 `InteractiveApplication` 驱动真实 Runtime，不访问 `package main` 私有 symbol，也不建立第二个生产 frontend。architecture guard 必须验证 `cmd/amadeus` 只有 thin entry、Domain 不反向依赖外层 package、生产代码只有一个 SessionIo event consumer 和一套交互完成协议。

### 7.2 Initial Prompt 与 TUI UserMessage

Codex 将根 positional `PROMPT` 定义为“启动交互 Session 的可选用户 Prompt”，没有 subcommand 时无论是否携带 Prompt 都进入 TUI；只有显式 `codex exec` 才属于 headless。Prompt 在 CLI/TUI 边界规范化 CRLF/CR 后，转换为 ChatWidget-owned `initial_user_message: Option<UserMessage>`。它不是 Task、Turn、Submission、Session configuration 或已持久化 history。

该 Contract 以当前 Codex 源码的 `codex-rs/tui/src/cli.rs`（`Cli.prompt`）、`codex-rs/cli/src/main.rs`（root dispatch 与换行归一化）、`codex-rs/tui/src/chatwidget/user_messages.rs`（`UserMessage`/`create_initial_user_message`）、`chatwidget/input_restore.rs` 与 `session_flow.rs`（pending submit）、`app/thread_routing.rs`（Resume replay suppression）及 `chatwidget/input_submission.rs`（正常提交链）为依据。实现验收必须保护这些概念关系和顺序，不能只验证“最终调用过一次 SubmitUser”。

Amadeus 采用相同概念关系和命名，并按当前只支持文字输入的产品范围定义最小 TUI 数据模型：

```go
type UserMessage struct {
    Text string
}

type UserMessageSubmission struct {
    Message             UserMessage
    ClientUserMessageID string
    Mode                protocol.ModeKind
    OverrideMode        bool
    FromNextTurnQueue   bool
    OriginThreadID      protocol.ThreadID
    OriginGeneration    uint64
}

type RunOptions struct {
    Prompt string
    // Bootstrap、Target、OpenSessions、streams 等现有字段
}

type ApplicationOptions struct {
    InitialUserMessage *UserMessage
    // Snapshot、Application、terminal options 等现有字段
}
```

- CLI 只拥有原始 `Prompt string` 并在 TUI dispatch 前规范化换行；`tui.Run` 只负责 `Prompt → *UserMessage` 转换。与 Codex `create_initial_user_message` 一样，exact empty Prompt 转换为 nil，非空内容除已完成的换行规范化外保持原样。当前不为尚未支持的 CLI image、TextElement 或 mention binding 增加占位字段。
- TUI `appModel` 是 Amadeus 中与 Codex ChatWidget 对应的 owner，保存唯一 `initialUserMessage *UserMessage`。TUI startup state 继续只保存 Version 等静态展示信息；Prompt 不进入 Startup、`InteractiveApplication`、ThreadWorkspace、SessionState、Protocol Event 或 Rollout。
- `InteractiveApplication.SubmitUser` 和 `Session.admitUserMessage` 不能再次 trim/rewrite 已由 TUI 构造的非空 UserMessage；它们只校验 exact empty、bounded bytes、active attachment 和 Runtime admission，再转换/接纳 Protocol UserInputOp。canonical ResponseUserMessage 与 completed UserMessage Item 保存同一 Text。Composer 是否 trim 属于 Composer parse policy，不得反向改变 CLI initial Prompt 的数据。
- 当前名为 `TaskSubmission` 的 TUI DTO 实际表示用户消息提交，必须直接替换为 `UserMessageSubmission`；`prepareTaskSubmission`/`submitTask` 对应改为 `prepareUserMessageSubmission`/`submitUserMessage`。不能保留旧类型 alias 或 wrapper。
- TUI pending queue 与 Codex 的用户消息术语统一：`QueuedUserInput` 改为 `QueuedUserMessage`，内部持有 `UserMessage`；Runtime boundary 继续使用 `UserInputOp`，因为它是跨 Interface/Session 的 Protocol operation。UI message 与 Protocol input 不使用同一个类型伪装 ownership。
- `MaxTaskBytes`/`maxTaskBytes` 改为 `MaxUserMessageBytes`/`maxUserMessageBytes`；错误文本使用 prompt/user message，不再把 UI 输入误称为 Agent Task。

Codex Fresh Session 在 Session configured 前 queue submit；Resume/Fork 则在安装 target 后 suppress initial submit，依次 replay persisted turns 和 buffered requests/events，再解除 suppress 并提交。Amadeus 使用已有 Go 同步 barrier 表达同一不变量：

```text
ThreadManager.Start/Resume
→ Session spawn
→ 等待 SessionIo.Configured 成功
→ InteractiveApplication.Start
→ 读取 canonical History 并构造 ThreadViewSnapshot
→ 安装唯一 active attachment/event pump
→ TUI appModel 恢复 snapshot Items
→ startup-ready lifecycle message
→ submitInitialUserMessageIfPending
→ normal submitUserMessage
```

`ThreadManager.spawn` 已在返回 AmadeusThread 前等待 `SessionIo.Configured`，`InteractiveApplication.Start` 已在构造 TUI appModel 前完成 history projection 和 attachment install。因此 Amadeus 不复制 Codex 为异步 app-server 准备的第二份 `queue_submissions_until_session_configured` 状态；configured barrier 和 snapshot/replay barrier 是 Go 中的等价实现。若这些 barrier 将来改为异步，必须重新引入 typed readiness state，不能靠 timeout 或 goroutine sleep 猜测。

Bubble Tea 的 `Init` 不能直接修改后续 model，因此 startup 使用无 payload 的 typed lifecycle message通知 `Update`“初始 frame 已建立”；Prompt 数据始终留在 `appModel.initialUserMessage`。`submitInitialUserMessageIfPending` 使用 take-once 语义并复用普通 Composer 提交链：

```text
pending initial UserMessage
→ validate active attachment / startup surface ready
→ create ClientUserMessageID for current attachment generation
→ append message recall history
→ optimistic UserMessageCell
→ InteractiveApplication.SubmitUser
→ UserMessageAdmission Started/Steered/rejection
→ canonical completed UserMessage Item confirms/deduplicates optimistic cell
```

- Initial message 不能拥有专用 `InitialPromptOp`、Turn 或 Event terminal；它与 Composer message 使用同一个 `UserInputOp` 和 admission contract。
- Initial message 不经过 Slash Command parser；CLI `amadeus "/compact"` 是普通用户 Prompt。Slash Command 只有从活动 Composer 的 `InputResult.Command` 才能执行。Amadeus 当前没有 Codex `!command` shell escape 产品能力，本次不为表面相似新增该功能。
- explicit Resume/latest continue 携带 Prompt 时，必须先完成目标 Thread history replay，再显示 optimistic initial message并提交。测试必须验证 replayed history 在 initial message 之前，且 initial message 只提交一次。
- picker、启动权限提示或其他 protected surface 存在时不得提交 pending initial message。当前 CLI 不允许无 ID picker 与 Prompt 组合；仍应由 `submitInitialUserMessageIfPending` 检查 active startup surface，避免未来新增入口后错误提交到临时 draft。
- submission 被拒绝、当前 model 不可用或 attachment ownership 阻止直接输入时，pending message 必须恢复到 Composer 并保持可编辑；不能丢失、自动改投 NextTurnQueue 或直接结束 TUI。
- initial message 一旦成功接纳便不再是 transient pending state；之后只由 Runtime canonical UserMessage 和现有 optimistic confirmation lifecycle 表达。Resume 不恢复未提交的 initial message。

## 8. 核心语义

### 8.1 Thread Identity 与 Project Context

`ThreadID` 是一条持久化对话 Thread 的 canonical identity；`AmadeusThread` 是该 Thread 在当前进程中的活动 Runtime 句柄。用户界面可以继续使用 Session 语义，例如 `amadeus sessions list`、`/resume` 和 `amadeus --resume <id>`，但这些恢复入口实际选择和恢复的是 `ThreadID`，内部字段、参数和 operation 不得将其误命名为 `SessionID`。

Amadeus 对齐 Codex，将 `ThreadID` 与 `SessionID` 建模为两个封装 UUID 的 Protocol/Identity value object，而不是允许任意安全字符的字符串 newtype，也不使用 alias 伪造两个领域类型：

```go
type ThreadID struct {
    uuid uuid.UUID
}

type SessionID struct {
    uuid uuid.UUID
}

func NewThreadID() (ThreadID, error)
func ParseThreadID(value string) (ThreadID, error)
func ParseSessionID(value string) (SessionID, error)
func SessionIDFromThreadID(id ThreadID) SessionID
func (id ThreadID) String() string
func (id ThreadID) IsZero() bool
```

- Amadeus 创建的新 Thread 一律使用 UUIDv7；生成职责属于 Protocol/Identity domain，不属于 bootstrap、CLI、ThreadStore 或通用 `NextID(kind)` factory。
- `ParseThreadID` 接受合法 UUID 并规范化为小写、带连字符的 canonical 表达；Resume 不要求输入 UUID 必须是 v7，但所有 Amadeus-generated ThreadID 必须是 v7。
- `ThreadID` 必须保持可比较，从而可以安全作为 ThreadManager、AgentControl 和 projector map 的 key；零值只表示未建立/缺失 identity，不能进入已创建 Thread、Event scope 或 Persistence。
- JSON、CLI、Tool argument、SQLite 和文件名边界统一使用 `ThreadID.String()`；反向进入 Domain 时必须调用 `ParseThreadID`，禁止 `ThreadID(value)` 强制转换、`strings.TrimPrefix("thread-")` 或 UI-only display ID。
- ThreadID 的 JSON/Text codec 属于 Protocol/Identity；SQLite adapter 显式扫描字符串并解析，不让 Identity domain 依赖 `database/sql`。

Codex 的 `SessionID` 与 `ThreadID` 是两个独立领域类型，即使 Root Thread 创建时二者具有相同 UUID：

```text
Root Thread:  SessionID 与 ThreadID 使用同一个 UUID value
Child Thread: SessionID == Root SessionID
              ThreadID  == Child 自己的 UUIDv7
```

`SessionID` 表示 root 与其 child threads 共享的 agent-tree/session-level identity；`ThreadID` 表示具体 Thread、Rollout、运行态 registry entry、Event scope 和 Resume target。Root 创建时由 Root ThreadID 派生 SessionID；child 创建时生成新的 ThreadID，并从共享 AgentControl 继承 SessionID。没有真实 session-level correlation/ownership 需求的接口不得仅为了用户文案引入或传递 `SessionID`。

身份所有权固定为：

| 领域 | Canonical identity |
|---|---|
| Root/child tree correlation、Provider metadata、Audit aggregation | SessionID |
| ThreadManager registry、AgentID、parent/child relation、Event routing、Resume、rename/delete | ThreadID |
| Turn、Tool execution owner、Approval/UserInput event scope | ThreadID + TurnID |
| `execute_command`/`write_stdin` 模型参数中的 `session_id` | Process session/process ID，不是 Agent SessionID |

共享 SessionID 不意味着共享全部可变状态。Root 与 child 的 Context、ActiveTurn、SessionPermissionContext、ProcessManager、Tool state 和 Rollout 继续隔离；只有 AgentControl、明确的 tree-level budget/correlation 和身份归属可以共享。

Project Context 由启动目录或 `-C, --cd` 指定目录形成 canonical CWD：

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
- SQLite 只保存 ThreadID、parent/source、rollout path、CWD、标题、预览、模型、Token、时间和归档状态等可重建查询元数据；不保存 SessionID。
- SQLite 可以落后 JSONL，但不能领先 JSONL；SQLite 丢失后必须可由 Rollout 重建。

`amadeus` 启动时可以先生成 UUIDv7 ThreadID、派生 Root SessionID 并持有未物化的 AmadeusThread；第一次有效输入时才创建 Thread persistence 并写入 SessionMetaItem。`/resume` 和 `amadeus --resume <id>` 在 CLI/TUI boundary 先解析 ThreadID，再读取 StoredThread 定位 Rollout，以 `InitialHistory::Resumed` 重建运行时 SessionState。Root 恢复必须校验 requested ThreadID、StoredThread.ID、Rollout filename identity 与 SessionMetaItem.ID 完全一致，并要求 `SessionMetaItem.SessionID == SessionIDFromThreadID(SessionMetaItem.ID)`。

`SessionMetaItem` 对齐 Codex，显式保存 session-level 与 thread-level identity：

```go
type SessionMetaItem struct {
    SessionID      SessionID `json:"session_id"`
    ID             ThreadID  `json:"id"`
    ParentThreadID *ThreadID `json:"parent_thread_id,omitempty"`
    // source、cwd、title、model、git metadata、created_at ...
}
```

SessionID 的 canonical durable source 是各 Thread Rollout 头部的 SessionMetaItem，不是 SQLite StoredThread。Root SessionMeta 的 ParentThreadID 为空；child SessionMeta 使用共享 SessionID、自己的 ID 和直接 parent ThreadID。Event scope、TurnContextItem、ResponseItem 和 collaboration payload 继续使用 `thread_id` 字段表达具体 Thread；只有 Session metadata 使用 `session_id + id`。不得在同一协议中同时保留旧 `SessionMetaItem.thread_id` 和新 `SessionMetaItem.id`。

### 8.3 ThreadManager

`ThreadManager` 对齐 Codex ThreadManager，是当前 Workspace runtime 的 Thread 创建、恢复和已加载实例管理入口；在未来存在长生命周期 App Server 时，才由上层将其提升为进程级共享 owner：

```go
type ThreadManager struct {
    store    ThreadStore
    services SharedServices
    threads  map[ThreadID]*AmadeusThread
}
```

它负责：

- `StartThread`、`ResumeThread`、`GetThread` 和 `ShutdownThread`。
- 对 New history 生成 UUIDv7 ThreadID，对 Resumed history 复用并校验 Rollout 中的 ThreadID；Root/child 创建都不得调用 `NextID("thread")`。
- Root 创建时由 Root ThreadID 派生 SessionID；Root Resume 从 canonical SessionMeta 恢复并校验 SessionID。Child 创建时从共享 AgentControl 取得 SessionID，不能从 child ThreadID 派生。
- Root Resume后从Root canonical AgentSpawnEdge projection选择state=open的persisted descendants，再用可重建SQLite parent/source/edge index定位child Rollout，并校验`SessionID == AgentControl.SessionID`、ID和ParentThreadID；SQLite不以SessionID查询tree，也不把任意存在的child metadata视为open membership。
- 对已持久化但尚未加载的 child 提供 ThreadManager 内部 child resume 路径；公开 `/resume`/`--resume` 仍只恢复 Root Thread，AgentControl 的 send/input lifecycle 按 child ThreadID 触发内部加载。
- 创建 `SessionSpawnArgs` 并调用 internal Session 的 spawn 流程。
- 在 Session configured 成功后将 `Session + SessionIo` 包装为 AmadeusThread 并注册进 live Thread registry；barrier 之前的失败路径必须 discard writer/runtime，正常关闭才使用 flush + shutdown，不留下半注册实例。
- 持有当前 Workspace 生命周期内的共享依赖，不执行 `run_turn`，不持有 ActiveTurn。只有真正跨多个 Workspace/Frontend 复用的 manager 才能提升到进程级；不得为了命名对齐建立宽泛 `Core` 或 `Runtime` aggregate。

CLI/TUI 只通过 ThreadManager 和 AmadeusThread 使用 Runtime，不直接装配 Session 级依赖或 Rollout Writer。

Go 为避免 `threadmanager → agent/session → threadmanager` 包循环，将持久化 port 与 LiveThread 放在 `internal/threadstore`，将需要 spawn internal Session 的管理层放在 `internal/threadmanager`。这对应 Codex 的 `thread-store` crate 与 core `ThreadManager/CodexThread` 边界，不建立第二套 ThreadManager，也不使用 `internal/state` 作为含义过宽的 metadata owner。

#### Application ThreadWorkspace

`internal/app.ThreadWorkspace` 是界面无关的 Application Service，拥有“当前选中的 AmadeusThread”及其切换生命周期。ThreadManager 继续拥有当前 Workspace runtime 内全部已加载 Thread；ThreadWorkspace 只负责 `EnsureCurrent`、`Resume`、`NewDraft`、`RenameCurrent`、`DeleteCurrent` 和当前 metadata 查询。若未来一个进程承载多个 Workspace，进程级共享服务必须由明确的上层 owner 持有，不能让 ThreadManager 隐式变成全局 registry。

每次 TUI invocation 只持有 bootstrap 构造的一个 ThreadWorkspace，不再分别保存 `ThreadManager`、`currentThread` 和对应 mutex；`cmd/amadeus` 与 CLI dispatcher 不持有 Workspace。切换或清空当前 Thread 必须通过 ThreadWorkspace，并由 ThreadManager 同步完成 shutdown 与已加载实例移除，避免 Resume 重新取得已经终止的 Runtime 对象。

### 8.4 AmadeusThread

`AmadeusThread` 对齐 CodexThread，是 TUI、CLI 或未来 Runtime API 持有的活动会话句柄：

```go
type AmadeusThread struct {
    session   *Session
    io        SessionIo
    sessionID SessionID
    threadID  ThreadID
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

`UserInputOp.ThreadSettings` 必须先转换为 immutable、validated settings update，但不能在 admission 结果确定前修改 Session。若消息 steer 当前 Regular Turn，只有 steer 成功后才应用 settings；若消息启动新 Turn，则在冻结新 TurnContext 前应用 settings。被拒绝或取消的消息不得改变 SessionConfiguration，也不得发布伪造的 `ThreadSettingsAppliedEvent`。因此 `/plan <task>` 仍只需要一个 `UserInputOp`，但其顺序是“校验 → 判断 Started/Steered → 在对应成功边界应用 → 冻结或继续”。独立 `ThreadSettingsOp` 只用于不提交用户消息的 `/plan` 和快捷模式切换；非法设置必须产生 correlated `ErrorEvent`，ActiveTurn 期间不能改变已冻结的 TurnContext。

提交到 Runtime 的普通用户消息只有一个 `UserInputOp`；steer 是该消息被 Runtime 接纳到当前 Turn 的方式，不是第二种 Core Op，也不新增 `SteerOp`。TUI Tab queue 在提交前只是 Interface 持有的未来输入，不改变这一 Protocol Contract。Session 必须为已提交的用户消息返回 typed admission：

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

`UserMessageAdmission` 是 submission request 的同步接纳结果，不是 `EventMsg`、TurnItem 或 canonical RolloutItem。`Started` 表示消息创建并启动新 Regular Turn；`Steered` 表示消息已进入现有 Regular ActiveTurn 的 pending input。没有 `Queued` admission：Tab queue 尚未跨越 Runtime submission boundary。`AmadeusThread.SubmitUserInputAndWaitForAdmission` 对齐 CodexThread 的 admission API：先按 SubmissionID 注册一次性 waiter，再通过既有 SessionIo Submission 边界提交，Session 完成接纳后返回 admission。普通 `Submit` 只保证 Submission 已进入 Runtime channel，不能被 TUI 用来推断 Started/Steered。

对需要基于缓存 ActiveTurn 做严格路由的未来远程/API caller，`AmadeusThread.SteerInput` 接受 required `ExpectedTurnID`；实际 ActiveTurn 不存在、TurnID 不匹配或当前 Task 不可 steer 时必须返回 typed error，不能静默注入另一个 Turn。基础 in-process TUI 优先使用 admission API，避免只依据本地 `running` 状态猜测 Core 状态。

### 8.6 internal Session

内部 `Session` 对齐 Codex internal Session，是 Agent 会话真正的 Runtime 所有者：

```go
type Session struct {
    sessionID  SessionID
    threadID   ThreadID
    state      SessionState
    services   SessionServices
    activeTurn *ActiveTurn
    inputQueue InputQueue
}
```

它负责：

- 持有当前 Thread 的 SessionID/ThreadID；Root SessionID 与 Root ThreadID 同 UUID value，child SessionID 从共享 AgentControl 继承。
- 运行长期 Submission Loop。
- 接纳用户输入和其他 Op。
- 创建 TurnContext，并记录 TurnStartedEvent、TurnCompleteEvent 或 TurnAbortedEvent canonical RolloutItem。
- 创建、持有、调度和取消 SessionTask。
- 路由 Approval Decision、User Input Answer 与 Interrupt。
- 对 `UserInputOp` 执行 Started/Steered admission，并完成按 SubmissionID 注册的 user message admission waiter。
- 将 steer input 放入当前 Turn 的 `TurnInputQueue`，而不是 Session deferred submission queue。
- 不拥有 TUI Tab queue；下一 Turn 输入只有在 Interface 从队列正式提交后才进入 Session。
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

Session 从 `InitialHistory` 恢复 ContextManager、ContextManager 内最后一个 TokenUsageInfo snapshot、PreviousTurnSettings 和当前 SessionConfiguration；恢复完成后根据 replacement history 与最后一个 TokenCount checkpoint sequence 后的 local suffix 重算 ActiveContextTokens。canonical Rollout 由 LiveThread/ThreadStore 持有，SessionState 不再并列保存第二份 `[]RolloutLine`。`update_plan` checklist 不是 SessionState，也不从 Rollout 恢复；它只作为当前运行期间的 transient `PlanUpdateEvent` 交给 Interface 展示。Proposed Plan 也不是 SessionState：模型原始 ResponseItem 与 completed Plan TurnItem 进入 canonical history，未决 parser、stream buffer 和实施 Popup 不恢复。StoredThread 只用于定位 Rollout 和展示索引元数据。SessionPermissionContext 是 SessionServices 中的瞬时服务，Session spawn 时重新初始化，Resume 不恢复历史 grant。

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
    Compaction   *CompactionService
    TimeProvider TimeProvider
    NextID       IDFactory
}
```

`internal/bootstrap` 只构造外部 Adapter 和创建 Session 所需参数；Session spawn 直接构造完整 `SessionServices`，Session shutdown 直接关闭其中由本 Session 拥有的资源。

依赖与生命周期规则：

- CLI flags 和 invocation 必须先归一化为 typed SessionConfiguration 或 `Submission{ID, Op}`，再跨越 Runtime 边界。
- `SessionState.Configuration` 是当前 Session 配置的唯一事实源；SessionServices 或 closure 不得保存另一份 configured snapshot。
- Provider client、ToolRegistry、ToolExecutionService、ProcessManager、AgentsMdManager、Approval、Permission、MCP、Skill、Web 和 CompactionService 不得在每个 Turn 中重新创建。
- CompactionService 只接收 immutable CompactionRequest 并返回 typed CompactionOutput；它不读取或修改 Session、ContextManager、Rollout、TUI、TokenUsageInfo 或 ActiveTurn，也不构造 canonical RolloutItem。
- Session 根据 Op 和 Turn 类型直接创建 `RegularTask`、`CompactTask` 或后续 ReviewTask。
- `RegularTask` 持有从 `UserInputOp` 接纳的目标、scoped EventSink 和 SessionServices 引用，并以 Session、TurnContext 和 cancellation 调用 Session 模块内 `run_turn`；Task 不持有或关闭 SessionServices。
- SessionTask 不反向调用 Application/TUI 的 `execute*Turn` 方法，也不持有 CLI controller、Cobra command、TUI appModel 或完整 CLI invocation。
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

`ThreadStore` 是存储无关边界，第一版定义创建、恢复、追加、flush、正常 shutdown、初始化失败 discard、读取历史、读取/列出/更新 Thread metadata 和删除 Thread 所需方法。`Shutdown` 与 `Discard` 是两个不同的生命周期操作：前者完成 durable flush 后关闭 writer，后者只释放初始化阶段的 writer/runtime，不强制把失败路径中的未决缓冲事实变成 durable history。

`LocalThreadStore` 是第一版生产实现：

- `LiveThread` 与 `ThreadStore` 负责 JSONL 创建、append、flush、resume 和 shutdown。
- `LiveThread` 必须区分正常 `Shutdown` 与初始化失败 `Discard`；Session 尚未完成 configured/ownership barrier 前发生的失败只能走 discard。
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

`TurnContext` 创建后不再修改，但可以持有运行所需的 typed 引用和取消关系；运行时 TurnContext 同时知道当前 SessionID、ThreadID 和 TurnID，以构造 Provider request metadata、Tool Invocation 和 Audit correlation。`TurnContextItem` 只保存恢复和诊断所需的稳定 ThreadID/TurnID 与纯数据，不为每个 Turn 重复持久化 SessionID；恢复后的 Session 从 canonical SessionMeta 重新注入 SessionID。二者必须通过显式 projector 转换，durable DTO 不引用 Client、API Key、Mutex、Cancellation、Telemetry 或其他进程对象。

普通 sampling 与 Compaction 的 Provider request metadata 必须来自同一 typed identity snapshot，至少表达 `session_id`、`thread_id`、`turn_id` 和可选 `parent_thread_id`。Root 与 child 请求使用相同 SessionID、不同 ThreadID；Provider Adapter 不得把 ThreadID 填入 `session_id`，也不得在 transport 层重新猜测 parent relation。该 metadata 必须被实际 Adapter 消费，不增加未接线的空扩展字段。

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
    Turn               TurnContext
    Model              ModelInfo
    ToolRouter         ToolRouter
    LoadedAgentsMd     LoadedAgentsMd
    Skills             []SkillMetadata
    PermissionProfile  PermissionProfile
    PermissionGrants   int
    Subagents          []AgentRecord
    MCPBindingRevision string
    SkillRevision      string
}
```

要求：

- 每次模型采样前重新 capture；同一次采样构建 WorldState、向模型声明 Tool 和执行模型返回的 Tool Call 必须使用同一个 StepContext/ToolRouter。
- StepContext 是进程内 immutable value，不作为 TurnContextItem 持久化；其中需要恢复的变化通过 typed ResponseItem、WorldStateItem 或 TurnContextItem 记录。
- ToolRouter 是当前 Step 最终广告和允许执行的工具计划；它冻结 exact ToolDefinition binding、visibility、parallel capability，以及 MCP、Skill、AgentsMd 和 router revision，由 ToolExecutionService 用于拒绝 stale deferred/lazy capability。
- capture 顺序必须先刷新 AgentsMd、Skill 与 MCP snapshot，并冻结 permission profile/grant count 与 active SubAgent snapshot，再构造 ToolRouter；Session 随后从同一个 StepContext 构造 typed WorldState，先记录需要追加的 full/diff fragment，再从更新后的 ContextManager history、Session-owned BaseInstructions、ToolRouter 和 TurnContext 生成 immutable PromptSnapshot。不能在 StepContext capture 内提前冻结尚未记录 WorldState diff 的 Prompt，也不能在 WorldState build 时二次读取 mutable Skill/Permission/AgentControl state。
- Plan Mode 通过 TurnContext.CollaborationMode 驱动 Prompt assembly，并在 StepContext.ToolRouter 中应用 request-scoped Tool policy；不创建另一种 SessionTask 或另一条 Engine 主链。
- ToolRouter snapshot 是模式 Tool 可见性的唯一事实源；模型 ToolSpec 与实际 dispatch 必须读取同一 snapshot。一次性的 `prepareStaticTurnContext`、按 `toolNames` 拼接 Developer Instructions 和逐批输入执行的第二次 mutable Registry 查询都不属于目标主链。

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

ActiveTurn 只保存当前 Submission correlation、RunningTask 和 TurnState。TurnState 是当前 Turn 的可变协调状态：Approval 与 `request_user_input` 使用不同 Request/Response 类型，但都由 Session Loop 按 RequestID 注册、交付和清理；`pendingInput` 保存已接纳但尚未进入模型 history 的同 Turn 输入，两者不得复用 channel、RequestID 或 payload。Tool Call count 和安全预算进度由 `run_turn` 累计；每个成功 Model/Compaction request 的 TokenUsage 立即交给 Session 更新 TokenUsageInfo，不等待 TaskOutput 或 Turn terminal 批量汇总。SessionPermissionContext 属于 SessionServices。Turn 完成后，Session 清除 pending waiter、TurnInputQueue 与 ActiveTurn，历史 Turn 生命周期继续保存在 canonical Rollout 中。

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

SessionTask 直接使用 Session 提供的 typed 方法。ContextManager 的 mutation 只能由 Session 对已接纳的 canonical facts 执行。`TaskOutput` 只表达 Turn outcome、summary/reason、Tool Call count 和由 `run_turn` 明确返回的 optional final agent message，不携带跨 Model Step 聚合 Usage，也不返回待 Session 在 Turn 尾部安装的 CompactedItem。Session 将该 final message 原样写入 `TurnCompleteEvent.last_agent_message`；普通 Assistant Item、stream tail、Tool Call 前导语和 TUI cell 都不得反向推断 Turn 的最终交付。TurnCompleteEvent、TurnAbortedEvent、ErrorEvent、TokenUsageInfo、canonical append 和 Event 发布由 Session 统一创建。`RunningTask` 是 Session 保存的运行记录，持有具体 Task、TaskKind、TurnContext、cancellation、execution handle 和 completion notification；启动 goroutine、取消、清理 ActiveTurn 与终态收尾属于 Session。基础版只有 Regular 可 steer，Compact 明确返回 `ActiveTurnNotSteerable`；不得继续使用 `compact bool` 作为 Task identity 或 steerability 判断。

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
- `AgentSpawnEdgeItem`
- `EventMsgItem`

`ResponseItem` 保存模型可见的 User/Assistant/Reasoning/Tool Call/Tool Result 事实；`AgentSpawnEdgeItem`只保存在Root rollout，拥有Basic Multi-Agent flat root→child open/closed membership；需要恢复的 Turn/Item 生命周期、Plan、Token、Context、Approval 结果和终态使用对应的 typed `EventMsg` 原样持久化。

`CompactedItem` 是 replacement install 的 canonical checkpoint，而不是摘要字符串 wrapper：

```go
type CompactedItem struct {
    ThreadID               ThreadID
    TurnID                 TurnID
    Trigger                CompactionTrigger
    Reason                 CompactionReason
    Phase                  CompactionPhase
    Summary                string
    ReplacementHistory     []ResponseItem
    CoveredThroughSequence int64
    SourceHash             string
    Provider               string
    Model                  string
}
```

ReplacementHistory 必须能无损表达当前 LLM domain 的 typed ResponseItem；Role/Content-only DTO、map payload 或从 summary 文本反解析角色均不允许进入当前格式。CompactedItem 与随后同批写入的 TokenCountEvent.Info 一起建立新 active context checkpoint；旧 RolloutLine 保留但不再进入后续模型投影。

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

基础版只实现文字 `UserTurnInput`；未来只有在真实产品需要additional context或多模态输入时才增加相应TurnInput variant，Multi-Agent mailbox仍是明确非目标。`TurnInputQueue`是Turn-scoped storage，随ActiveTurn创建和销毁；`InputQueue`是Session-scoped coordinator，本身不得成为第二份conversation history。

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

Session 的 deferred submission queue 只保存明确允许延后执行的 Session operation；`UserInputOp` 在 ActiveTurn 期间不得进入该 queue。Core 不静默把 Enter 提交或 rejected steer 变成下一 Turn；显式 Tab queue 与 rejected-steer recovery 都由 Interface/Application 持有，并在真正重新提交时获得新的 admission。

Steer 默认不取消正在进行的普通模型 stream、Tool、Approval wait 或 `request_user_input` wait。特殊等待 Tool 若未来需要被新输入唤醒，必须订阅 typed InputQueue activity，不得让所有 Tool 隐式观察全局输入 channel。

`ClientUserMessageID` 用于 Interface optimistic rendering 与 Runtime UserMessage Item 的确认/去重。初始输入和 steered input 都必须形成同一 canonical/live 生命周期：先追加当前 Turn 的 `ResponseUserMessage` 和需要持久化的 completed UserMessage TurnItem，再发布 Item lifecycle Event；TUI 不得长期依赖“初始消息只由本地插入、steer 消息只由 Runtime 插入”的双来源规则。

### 8.17 Next-Turn User Input Queue

Next-turn queue 表示用户在 TUI 的一个 Turn 运行期间显式按 Tab，把普通文字保留为后续独立 Turn。它与 same-turn steer 是两个不同的输入意图：Enter 立即提交 `UserInputOp` 并由 Runtime 返回 Started/Steered；Tab 在本地排队，当前 Turn terminal 前不得调用 Runtime。

该能力对齐 Codex `ChatComposer.InputResult::Queued`、`ChatWidget.InputQueueState` 与 terminal 后 `maybe_send_next_queued_input` 的职责关系，但只实现 Amadeus 当前需要的普通文字队列，不提前复制 queued Slash/Shell、图片附件、paste placeholder 或跨产品 thread-tab state。

Go 数据模型固定为：

```go
type QueuedUserMessage struct {
    Message              UserMessage
    Mode                 ModeKind
    ThreadID             ThreadID
    AttachmentGeneration uint64
}

type NextTurnQueue struct {
    Pending  []QueuedUserMessage
    InFlight *QueuedUserMessage
}
```

`NextTurnQueue` 由 TUI `appModel` 对应的 Input/ChatWidget 层拥有，并放在独立的 `internal/tui/input_queue.go`；它不是 `SessionState`、`Session.inputQueue`、Application Thread registry、Context history 或新的业务 Event reducer。队列只保存尚未提交的用户意图，因此：

- enqueue 不创建 Submission、UserMessageAdmission、TurnItem、EventMsg、RolloutItem、Context message 或 SQLite record。
- enqueue 不插入普通 `UserMessageCell`，只更新本地 input recall 与有界 queued preview；真正出队并提交时才生成 ClientUserMessageID 和 optimistic UserMessage projection。
- `Pending` 是 FIFO；`InFlight` 保存已经从 FIFO 取出但尚未收到 Started admission/TurnStarted 确认的唯一输入，防止 Bubble Tea `tea.Cmd` 与 terminal/key event 竞态重复启动 Turn。
- 每个 queued input 必须绑定 enqueue 时的 ThreadID、attachment generation 和 Mode；旧 attachment 的 queued/in-flight input 不得发送到新 Thread。基础版在 Resume、Clear、Delete 或 shutdown 替换 attachment 时清除旧 attachment queue，与未提交 composer draft 一样不持久化。
- dequeue 时当前 Session mode 必须仍与 queued Mode 一致；不一致时按提交前失败恢复 composer，不把 queued input 静默切换模式或发送到另一 Collaboration Mode。
- 初始版本只 queue `InputResult.Text`；Slash Popup 的 Tab completion 优先于 queue，Slash Command 不以原始字符串延迟解析。未来需要 queued command 时必须增加显式 action kind 和 dequeue parser，不得把 command 文本当普通 UserInputOp。

键盘与提交语义固定为：

```text
Active Turn + Enter + ordinary text
→ existing SubmitUser
→ Runtime admission Started / Steered / typed rejection

Active Turn + Tab + ordinary text
→ NextTurnQueue.Enqueue
→ clear composer
→ no Runtime call

Tab while Slash Popup has a selected item
→ complete selected Slash Command
→ do not enqueue
```

在用户已经输入可排队的普通文字、但尚未按 Tab 时，TUI Footer 必须对齐 Codex 显示 transient queue hint。`HasQueueableDraft` 或等价派生值只在以下条件全部成立时为 true：当前 Turn 正在运行、Composer trim 后非空、`ParseInput` 结果为普通 `Text`、没有活动 Slash/Selection/Approval/User Input overlay。Slash Command 或 invalid slash 不得显示会误导用户的 queue hint。

```text
Running + queueable ordinary draft
→ replace passive fixed statusline with "tab to queue message"
→ narrow fallback: "tab to queue"
→ preserve Plan indicator only when it fits
→ drop statusline/context and then Plan indicator before dropping queue hint
```

queue hint 是 Composer action guidance，不是 queued preview、StatusLineItem、Working header、HistoryCell 或 Runtime state。真正按 Tab enqueue 后 Composer 变空，hint 消失，已有 `Queued (n)` preview 继续显示；如果用户继续输入另一条普通文字，则 preview 与 queue hint 同时显示。Popup/overlay 继续占用整个 auxiliary/footer 区域并隐藏两者。

terminal drain 与恢复策略固定为：

- `TurnCompleteEvent` 且 Outcome 为 Completed 或 Failed：当前 Turn UI 先完成，再 FIFO 取出至多一条并通过现有 `SubmitUser` 路径启动下一 Turn；其 admission 必须为 Started，Steered 视为 attachment/ordering invariant violation，并产生可见诊断，不得静默当作成功对齐。
- `TurnCompleteEvent` 且 Outcome 为 Blocked，或 `TurnAbortedEvent`：不自动提交；将 InFlight 与 Pending 按原 FIFO 合并恢复到 composer，清空 queue，并保留用户重新编辑/提交的控制权。
- 普通 `ErrorEvent` 不单独触发 drain；已经开始的失败 Turn 仍等待唯一 `TurnCompleteEvent`，避免 Error + Complete 双提交。
- queued submission 在尚未被 Runtime 接纳前返回错误或 malformed admission 时，将 InFlight 恢复到 composer，保留剩余 Pending，不自动跳过失败输入继续发送。若 Runtime 已返回合法但错误的 Steered admission，消息已经进入当前 ActiveTurn，TUI 必须清除 InFlight、停止后续自动 drain 并显示 invariant violation；不得把同一内容恢复后再次提交。
- 一个 terminal 只允许启动一个 queued input；下一条必须等待新 Turn 自己的 terminal。`TurnStartedEvent` 清除匹配的 InFlight/start-pending gate。
- Plan Turn terminal 时如果存在 queued input，优先启动 queued Plan input，不显示 `Implement this plan?` overlay；没有 queued input 时保持现有 Proposed Plan transition。

queued preview 是 transient Composer state：显示有界数量、FIFO 顺序和总数，但不伪装成聊天历史、Tool activity、statusline metadata 或 Runtime Working 状态。窄终端必须截断而不能覆盖 Composer/Footer。Queue 不进入 Resume replay；进程退出、Thread attach 替换或显式 Clear 后不恢复。

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
→ 计算 ContextWindowTokenStatus，必要时由 Session inline runCompaction 并重新 capture StepContext
→ Turn-scoped ModelClientSession 发起 LLM Stream
→ 发布 ItemStartedEvent 与 Agent/Reasoning Delta Event
→ 模型完成一个 ResponseItem 后 canonical append ResponseItem 与需要持久化的 ItemCompletedEvent
→ Session 立即记录本次 TokenUsageInfo snapshot 并发布 TokenCountEvent
→ 发布 ItemCompletedEvent
→ 若存在 Tool Call，使用同一 StepContext 的 Tool Router 执行
→ Tool + Approval Event/Response Runtime
→ canonical append Tool Result 与需要持久化的 ItemCompletedEvent
→ 下一 Model Step 重新 capture StepContext
→ 模型返回 Final Response 后结束 run_turn
→ Session 追加 TurnComplete EventMsg
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
8. 每个成功 Model/Compaction request 的 TokenUsage 只记录一次；TokenCountEvent 是完整 TokenUsageInfo snapshot，不是等待 Turn 结束才写入的 usage delta。

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

#### 9.1.2 Next-Turn Queue Flow

运行中的 Tab queue 不进入上述 same-turn flow：

```text
Ordinary composer text + Tab while Turn is running
→ TUI Input layer validates active attachment and non-empty text
→ enqueue QueuedUserMessage(UserMessage, ThreadID, generation, Mode)
→ clear composer and refresh queued preview
→ current Turn continues unchanged
→ Session persists terminal and clears ActiveTurn
→ matching terminal Event reaches TUI reducer
→ finalize current Turn UI
→ move FIFO head to InFlight and synchronously close the local drain gate
→ submit through the normal UserInputOp/admission path
→ require Started{new TurnID}
→ TurnStarted clears matching InFlight and begins the next Turn UI
```

关键不变量：

1. enqueue 与 current Turn 的 Session、TurnInputQueue、ContextManager 和 Rollout 完全隔离；只有 dequeue submission 才成为 Runtime fact。
2. terminal Event 是自动 drain 的唯一触发源；`running=false`、spinner 停止、`tea.Cmd` 返回或 `ErrorEvent` 本身都不能代替 terminal。
3. Session 在发布 terminal 前已经完成 canonical append、flush 和 ActiveTurn 清理，因此正常 dequeue admission 必须为 Started，而不是 Steered。
4. FIFO head 从 Pending 移入 InFlight 与 drain gate 设置必须发生在创建异步 `tea.Cmd` 前；重复 terminal、resize、status refresh 或 admission callback 不得再次发送同一输入。
5. ThreadID 或 attachment generation 不匹配时不得 drain；旧 attachment queue 不迁移到新 Thread，也不进入 Resume replay。
6. TurnAborted、Blocked、pre-admission submit rejection 和 malformed admission 按 8.17 恢复用户输入；已经接纳的 unexpected Steered 按 invariant failure 停止自动 drain，不得重复恢复同一消息。

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
- 所有 goroutine 都必须有明确 Owner、退出条件和测试覆盖；不允许 fire-and-forget goroutine。Thread attachment replacement、child release、process waiter 等后台清理必须由 owner 跟踪，并在 owner shutdown 时等待或报告超时。

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
- Event Channel 使用有界缓冲；高频 Delta 可以在投影层合并，但生产者遇到背压时必须等待或通过取消退出，不能静默丢失 Turn/Item 终态、Approval request 或 User Input request。任何未来 transport 都必须保留单一顺序和 critical-event delivery 语义。
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
- Provider client、ToolRegistry、ToolExecutionService、ProcessManager、MCP/Skill/Web、Approval、Permission 和 CompactionService 不在每 Turn 重建。
- Session 根据 Op 直接创建具体 Task。
- RegularTask 只把 Session、TurnContext、目标和 cancellation 交给 `run_turn`。
- CompactTask 与自动压缩共享 SessionServices.Compaction，但 trigger/reason/phase、Item lifecycle、TokenUsageInfo 更新和 durable replacement install 都由 Session.runCompaction 负责；自动压缩是 `run_turn` 操作，不是嵌套 SessionTask。
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
→ Compute ContextWindowTokenStatus / Maybe Session.runCompaction
→ Sample Model Stream
→ Persist completed model ResponseItems
→ Record this request's TokenUsageInfo snapshot
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
- preflight budget 使用应用当前 WorldState diff 后、由 exact StepContext ToolRouter 组装出的 Prompt estimate；response 完成后的 continuation budget 优先使用 Provider-reported LastTokenUsage 加最近模型输出后新增本地 items。二者由同一 ContextWindowTokenStatus 汇合，不能由 PromptSnapshot、TUI 和 Compaction callback 分别判断。
- compact 成功后必须立即重新计算 active context；若 replacement 没有使 active tokens 显著下降或仍达到硬窗口，同一 history/window 不得再次无界 compact，而应返回 typed `compaction_insufficient`/`context_window_exceeded` failure。

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
- retryability 判定、retry counter、delay、取消、瞬态 Event 和重试耗尽后的最终错误必须由同一 Core owner 串行协调；不得由 OpenAI SDK、TUI 和 CompactionService 分别维护三套重试事实。

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

普通 sampling、手动 `/compact` 的 CompactTask 和 `run_turn` 内自动 compaction 必须复用同一 Turn-scoped ModelClientSession/response-stream retry policy；CompactionService 可以使用不同请求类型和 Prompt，但不得拥有独立、不可观察的 retry loop。

#### 10.4.2 Partial Delta 与恢复边界

Stream 在已发布部分 Delta 后断开时，不能简单重新请求并把新 Delta 继续追加到旧 attempt source。实现必须明确 attempt-local aggregation 与 Item-scoped StreamController/TranscriptSurface stable run/tail 的关系，并满足：

- 未形成 completed ResponseItem 的 Delta 不是 canonical fact，不写入 Rollout，也不参与 Resume replay。
- 每次 retry attempt 使用独立 aggregation state；只有成功完成的 ResponseItem 才进入 canonical append 与 ItemCompletedEvent 顺序。
- 新 attempt 如果从头返回内容，TUI/stream projector 必须通过`Reset=true`原子替换同一ItemID的旧attempt source/render/frame，不能产生重复文本、重复 Tool Call 或重复 reasoning。
- `ItemStartedEvent.Item.Kind` 是 live projector 的稳定分流依据：Assistant start只建立Item-scoped StreamController，Reasoning start只建立status/reasoning identity，不得创建Tool/Explored HistoryCell；只有Tool/Command/File activity才进入Tool activity projector。TUI不得用空`ToolName`、事件到达顺序或展示字符串猜测item类型。
- 已完成并 canonical append 的 ResponseItem 不因后续 sampling request 重试而回滚；retry 只作用于当前未完成 sampling request。
- cancellation 在 stream read 和 backoff 期间都必须立即生效，并最终走 `TurnAbortedEvent` 或既定 interruption contract，不能被下一次 retry 吞掉。

Response stream retry Event 是 live、transient、non-canonical notification。Resume/replay 不重放历史上的 `Reconnecting...` 状态，也不尝试恢复旧 Provider stream、retry counter、backoff timer、StreamController或attempt-local source。

### 10.5 Progress、Budget 与停止条件

`run_turn` 不使用“相同调用两次”或“相同错误两次”之类通用启发式判定 stalled。重复错误可以形成 model-visible reminder 或 telemetry，但不拥有 Turn 终止权。

基础预算只包括明确可解释的限制：

- 最大模型采样次数；
- 最大 Tool Call 次数；
- 最大 Turn wall-clock；
- Model context/token limit。

这些限制是 Amadeus 明确保留的高阈值 runaway safety，并非声称与 Codex 一比一同构。它们不再暴露旧 `agent.max_iterations`、`agent.max_tool_calls` 或 `agent.max_duration` 配置，避免形成低阈值且行为不稳定的产品契约。普通 Root Turn 接近预算时只注入一次明确的完成提醒；read-only SubAgent 还必须在 child budget 的 soft boundary 进入一次有界、Tools 为空的 finalization sample，使其停止探索并交付已验证事实、未决问题和限制。只有在 hard boundary 已被跨越、finalization request 失败或无法产生合法最终文本时才返回 typed blocked outcome，而不是伪装成 Provider 或 Tool infrastructure error。模型采样、Tool Call 和 elapsed time 在 Turn 执行期间累计；Tool count 和 optional final agent message进入 TaskOutput，每个 request 的 TokenUsage 则即时进入 Session-owned TokenUsageInfo/canonical TokenCountEvent，不在 Turn terminal 再生成第二份聚合 usage。

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

- `TaskOutcomeCompleted` → `TurnCompleteEvent{status: completed, outcome: completed, last_agent_message}`。
- `TaskOutcomeBlocked` → `TurnCompleteEvent{status: completed, outcome: blocked, summary/reason, optional last_agent_message}`。
- `run_turn` error → `TurnCompleteEvent{status: failed, outcome: failed, error}`。
- Turn Context cancellation with abort cause → `TurnAbortedEvent`。

`TurnCompleteEvent` 的 typed payload 包含 `outcome`、可选 `reason` 与可选 `last_agent_message`；`last_agent_message` 只能来自 `run_turn` 的最终模型完成，blocked 的 finalization sample 可以提供明确标注限制的 partial report。不得再用自定义 `OutcomeError` 把 blocked、limit 或正常 partial completion 伪装成 failed，也不得扫描普通 Assistant Item 猜测最终消息。

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

通用 Tool lifecycle observer 对 `update_plan` 只提交 `ResponseToolResult`，不提交普通 Tool TurnItem。基础阶段允许在 observer 的单一策略函数中显式识别 dedicated-event Tool，避免把 UI lifecycle 字段加入模型可见 `ToolSpec`；当第二个同类 Tool 出现时，再将该策略提升为 Registry metadata。live TUI 和 Replay 不得继续通过多处分散的 Tool 名称特判隐藏重复 activity。

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
- 当前基础产品只有单一 TUI frontend，`request_user_input` 始终由 TUI overlay 完成。未来若新增显式非交互或 SDK frontend，必须先定义独立 UserInputRequester capability 和 typed unavailable response；没有交互能力时不得永久等待输入或静默选择默认项。

## 12. Prompt 与 Context

### 12.1 Prompt 所有权与请求生命周期

单次模型请求使用 Codex 同构的数据模型：

```go
type BaseInstructionsProvenance struct {
    Kind  string // custom | model
    Model string // Kind == model
}

type BaseInstructions struct {
    Text       string
    Provenance BaseInstructionsProvenance
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

`BaseInstructions` 对应 Responses API 的 `instructions` 语义，而不是普通 conversation `system` item。Provider-neutral `Prompt` 保持这一边界：Responses Adapter 将 `BaseInstructions.Text` 写入 wire `instructions`；Chat Completions Adapter 根据 Dialect 映射为唯一且稳定的 system/developer 前缀。不得由 `Request.InputMessages()` 或其他通用 helper 先把 Base 转成 `ResponseItem`，再让所有 Adapter 共享错误的消息形状。

完整生产主链固定为：

```text
Session creation
→ resolve exact BaseInstructions + provenance
→ persist BaseInstructions in SessionMeta

Each model step
→ capture immutable StepContext
→ build typed WorldState from the same StepContext
→ render full/diff role-aware ContextFragments
→ record model-visible fragments into canonical history
→ persist WorldState full/patch and TurnContext baseline
→ normalize ContextManager history
→ Prompt{Session Base, history, Step ToolSpecs, Turn OutputSchema}
→ ModelClientSession
→ Provider-specific wire mapping
```

各类内容只有一个 owner：

- `BaseInstructions`：Session 级稳定模型指令；Session 创建时解析并持久化，不在每个 Step 从 mutable ModelMessages 重新解析。
- `ModelMessages`：模型 instruction template、personality variables、Approval/Permission messages、Collaboration Mode messages 和 Multi-Agent messages；不拥有 Compaction Prompt。Approval/Permission 使用 Codex 的模型消息槽位与动态注入边界，但具体判断、确认文案和交互继续遵循 Claude Code 内层 Contract。
- `WorldState`：当前可变运行事实及其 typed section snapshot、role、marker 和 diff 规则。
- `ContextManager`：已经进入模型历史的 canonical ResponseItem、durable ContextKind、WorldState baseline、TurnContext reference、normalization、replacement 和 token projection。
- `StepContext`：本次请求的 ModelInfo、ToolRouter、LoadedAgentsMd、atomic Skill metadata/revision、Permission profile/grants、active SubAgents 与 MCP binding snapshot；不提前缓存尚未记录 WorldState diff 的 Prompt。
- `ToolSpec`：Tool 名称、完整模型可见 description、参数 Schema 和 output schema；Collaboration Mode 不拼接 Tool guidance。
- `TurnContext`：当前模式、reasoning、output schema 和稳定 Turn 设置。
- `ModelClientSession`：消费完成的 Prompt 并管理 turn-scoped transport/retry，不决定 Prompt 文本。

禁止把动态路径、当前权限、模型名、Tool 列表或临时预算提醒硬编码进 Base；也禁止在 `continueTurn`、Task、Provider Adapter、TUI 或 projector 中为单次请求临时追加无 canonical typed fact 的 developer string。

### 12.1.1 ModelMessages、BaseInstructions 与资产来源

Session 创建时按以下优先级解析 Base：

```text
optional explicit custom Base override
> resumed SessionMeta.BaseInstructions
> selected ModelInfo.ModelMessages.instructions_template rendered with Personality
```

解析结果连同 `custom` 或 `model{slug}` provenance 写入 SessionState 和 SessionMeta。AA 不要求仅为此新增公开配置项；只有 composition/config 明确提供 override 时才使用 custom 分支。Resume 必须优先恢复 exact persisted text，因此升级二进制或调整内置 Prompt 不会静默改写已有 Thread 身份。未来支持模型切换时，custom Base 保持不变；model-derived Base 通过 typed `ModelInstructionsState`/`<model_switch>` developer fragment向既有 history 补充新模型指令，不在 transport 层悄悄替换历史语义。Personality 若可在 Thread 内改变，也必须通过 typed `PersonalityState` 追加当前 personality instructions，而不是改写 persisted Base。

第一版不必复制 Codex 的远程 ModelsManager/cache，但必须提供按模型 profile 选择的 pinned built-in catalog；未知模型使用明确的 neutral fallback，不得让所有模型共享声称 `based on GPT-5` 的模板。每个资产记录上游仓库 revision、源路径和内容 hash；当本地参考快照不含 `.git` metadata 时，使用明确 snapshot date + source path + SHA-256，不虚构 commit。Amadeus 版本采用“上游原文 + 可审计 patch manifest”生成。允许的 patch 仅包括：`Codex → Amadeus` 品牌替换、实际交互通道/Tool 名称差异、明确未实现能力删除和本设计规定的产品范围差异；不得人工压缩成语义不完整的短版。

资产 revision 按职责独立：model instructions、collaboration modes、每个 ToolSpec、compaction summarization prompt、summary prefix 和 multi-agent role 分别计算。不得再用一个包含全部 Markdown 的全局 hash，或一个合并 compact prompt/prefix 的 revision，让无关资产相互失效。

### 12.1.2 Typed WorldState、角色与持久化

WorldState section 必须拥有稳定 ID、typed snapshot 和相对旧状态的 render contract：

```go
type WorldStateSection interface {
    ID() string
    Snapshot() any
    RenderDiff(previous PreviousSectionState) *ContextFragment
}
```

基础版只实现当前产品真实使用的 section：`model`、可变时的 `personality`、`collaboration_mode`、`agents_md`、`environment`、`permissions`、`skills_catalog`、`multi_agent_role`/`multi_agent_mode`。MCP 可见能力主要由 Step ToolSpecs/Binding 表达；只有确实存在独立模型指令时才建立对应 section，不注入“请相信 ToolSpec”之类空泛文本。

每个 fragment 自己决定 role、marker 和是否独占 message，不能由统一 map 强制转换成 developer：

| Fragment | Role | 关键 marker/语义 |
|---|---|---|
| Collaboration Mode | developer | `<collaboration_mode>`；模式变化追加完整 reset/current instructions |
| AGENTS.md | user | `# AGENTS.md instructions for ...` + `<INSTRUCTIONS>` |
| Environment | user | `<environment_context>`；包含 cwd、shell、current date、timezone 和 subagents |
| Permissions/Approval policy | developer | 当前可执行边界和新增 grant/prefix 的最小 diff |
| Available Skills catalog | developer | `<skills_instructions>` |
| Explicit selected Skill | user | `<skill>`；作为本 Turn canonical input item，不是 replaceable catalog state |
| Runtime reminder | developer | 独立 typed marker；记录后才进入下一 request |

Durable `ResponseContextMessage` 使用 `ContextKind=world_state|explicit_skill|turn_budget` 标识来源类别；该字段用于 crash/Resume baseline recovery，不由 Provider 消费，也不把 runtime context误分类为真实 User。WorldState 内部的细粒度 content kind仍由各 `ContextFragment.Kind` 所有。

ContextManager 保存 `WorldStateSnapshot` baseline，并明确区分 `Absent/Unknown/Known`：没有 fragment/baseline 时为 Absent；带 `ContextKind=world_state` 的 canonical context fragment 已落盘而后续 full/patch 尚未完成时为 Unknown；`WorldStateItem{full|patch}` 成功记录后才为 Known。第一次真实 Turn、Absent/Unknown baseline 或需要新 context window 时生成 full snapshot；稳定 Known 状态只生成 section diff。模型可见 fragment 必须先 canonical append/record，成功后再持久化描述该状态的 `WorldStateItem{full|patch}`，最后持久化 `TurnContextItem` reference。任何一步失败都不能让 baseline 越过模型实际可见 history。Explicit Skill 与 runtime reminder 使用各自 durable ContextKind，不得误把 baseline 降为 Unknown。

Resume 从 Rollout 恢复 ResponseItem history、WorldState baseline 和 TurnContext reference。若 typed baseline 缺失，则基于 retained typed marker 判断 `Absent/Unknown` 并显式生成 replacement/removal fragment；不得简单用“当前值前置到所有历史之前”掩盖状态变化。

旧 `ContextUpdateEvent + map[replaceKey]renderedString + Snapshot 时统一前置` 模型不属于目标设计。迁移时直接以 `WorldStateItem` full/patch 和 canonical contextual ResponseItem 替换，删除旧 event、update map、universal marker switch、双写和兼容 decoder。

### 12.1.3 Step 装配、记录顺序与 Wire Contract

普通 Turn 不拥有独立 `RegularTaskPrompt`。首次用户输入的模型历史顺序必须是：

```text
full initial developer/contextual-user messages
→ persisted WorldState full snapshot
→ persisted TurnContext reference
→ real User input
→ input-scoped explicit Skill/contextual items
→ sampling
```

后续 Step 和后续 Turn 使用：

```text
capture StepContext
→ build current WorldState
→ append only required diff fragments
→ persist WorldState patch / TurnContext reference
→ record any newly drained real User input and its input-scoped contextual items
→ normalize full canonical history
→ build Prompt with the same StepContext.ToolRouter
```

同一次采样广告和执行 Tool 必须使用同一个 frozen ToolRouter。不得在 `prepareStaticTurnContext`、collaboration renderer 或 input preparation 中再取得第二份 Tool 列表。Prompt revision 必须覆盖 exact BaseInstructions、normalized input、ToolSpecs、OutputSchema 和当前 WorldState/TurnContext baseline；revision 是 cache/diagnostic identity，不替代 persisted exact text/snapshot。

Responses Provider 请求必须使用 wire `instructions = Prompt.BaseInstructions.Text`，`input` 只包含 ResponseItems；Chat Completions 的兼容映射由 Dialect 独占并有 golden tests。Provider Adapter 不得选择 Plan/Default 文本、加载 AGENTS.md、解析 Skill 或构造 runtime reminder。

### 12.1.4 Base 层与 Default/Plan 两种模式

Amadeus 只有 `Default` 和 `Plan` 两个 Collaboration Mode。`BaseInstructions` 不是第三种模式，而是两个模式共同使用的 Session/model-level 稳定指令层。单次普通采样的指令关系固定为：

```text
Session BaseInstructions
+ exactly one CollaborationMode fragment: Default | Plan
+ typed WorldState/contextual history
+ current ToolSpecs
```

因此，“Base、Default、Plan 文本”表示一个稳定 Base 层和两个互斥模式文本，不表示三种 mode。跨模式都成立的身份、沟通、工程纪律和 Tool 使用原则放在 Base；只有 Default 执行倾向、Plan 非修改约束、提问策略和 `<proposed_plan>` finalization 等模式差异分别放在 Default/Plan。Tool 的能力、参数和调用方法不属于模式策略，必须留在 ToolSpec。

Default 与 Plan 都由 `CollaborationModeState` 选择一个完整 Developer Instructions source。优先使用当前模型 catalog 的 `CollaborationModeMessages`；缺失时使用 pinned Codex collaboration-mode preset。每次只注入当前模式的一份完整文本，不得同时读取 Markdown 后再在 Go 中追加第二段 Plan 文本。

- Default 以 `codex-rs/collaboration-mode-templates/templates/default.md` 为基线，保留 mode reset、模式只由 developer update 改变、`request_user_input` availability 和 Default 提问边界。
- Plan 以 `codex-rs/collaboration-mode-templates/templates/plan.md` 为基线，保留严格 mode lifecycle、Plan Mode 与 `update_plan` 区分、non-mutating exploration、三阶段对话、问题策略和唯一 `<proposed_plan>` contract。
- Plan 的 Tool 可见性必须与文本一致。基础版若继续隐藏全部 command/write Tool，则只允许通过 manifest 修改 Codex 模板中对应的 allowed examples；不能用五行自定义文本替代完整模板。若未来暴露 command，则必须先有可执行的 non-mutating policy，而不是只依赖提示词承诺。
- Root Base 以选定 Codex model profile 为基线，仅将产品身份替换为 Amadeus。项目使用 Go、仓库结构或当前实现语言属于 workspace evidence，不属于 Agent 身份；Base 不注入“Go coding agent”。

SubAgent 的只读 explorer 是 Amadeus 明确产品差异，但所有权对齐 Codex：role text 位于 `ModelMessages.MultiAgent.Role.Subagent` 或等价 typed model-message field，并通过独立 developer fragment注入；只读能力限制由 ToolRouter/policy 强制。不得使用额外 `SubagentDeveloperInstructions` 顶层字段后再按 Tool 名追加 guidance。

### 12.1.5 Codex 风格 Compaction Prompt

Compaction Prompt 不属于 `ModelMessages`。`SUMMARIZATION_PROMPT`、`SUMMARY_PREFIX` 和可选 explicit compact override 是独立 Prompt asset/config；其上游文本 byte-for-byte 固定并分别 revision。

```text
Session BaseInstructions
+ exact normalized model-visible input
+ User(codex-rs/prompts/templates/compact/prompt.md)
+ Tools = none
→ Summary Response

codex-rs/prompts/templates/compact/summary_prefix.md
+ Summary
→ User(SUMMARY_PREFIX + Summary)
→ typed Replacement History
```

Amadeus 保留 `CompactedItem`、SourceHash、CoveredThroughSequence 和原位 ContextManager replacement Contract。CompactionService 输入必须来自 exact PromptSnapshot，因此摘要模型看到与当前 Model Step 一致的 BaseInstructions、WorldState、AGENTS.md、Skill 和 conversation projection。Compact 请求不得暴露 Tool、不得把 summarization prompt 包装成 `BaseInstructions` 类型，也不得把摘要当成普通 Assistant 完成证明。

pre-turn/manual compact 安装 summary replacement 后清空 TurnContext/WorldState reference baseline，由下一次正常 Turn full reinject 当前 context；mid-turn compact 必须把从 exact StepContext 重新渲染的 full initial context 插到最后真实 User 或 summary 之前，使 summary 保持最后一项，并同时安装新的 WorldState/TurnContext baseline。不能依赖一个永远前置的 side-map 假装两种 phase 相同。

Compaction Summary 至少保留当前目标、关键决策、约束和用户偏好、已完成进度、重要文件/数据/结果、未完成事项、下一步和关键引用。`CompactTask` 只请求 Session 执行 standalone compaction；Session 调用无状态 CompactionService 生成 typed output，再统一完成 canonical install。

### 12.1.6 Tool Prompt 来源与装配边界

Tool 模型指导是 ToolSpec description/schema 的一部分，不是 Collaboration Mode 的附加 developer block。这里的“Prompt”包含完整模型可见 Tool contract，而不只是 description：

```text
name
+ description
+ input property descriptions / required semantics
+ optional output schema
+ strictness when the Provider and exact schema support it
+ request-scoped visibility
```

Runtime-only 的 `SideEffect`、`Idempotent`、Permission/Approval policy 和 exact handler binding 不必原样发送给模型，但必须与模型可见 ToolSpec 语义一致。Amadeus 不因 Claude Code 使用 strict Zod schema 就全局开启 OpenAI strict function calling；`Strict` 与 `OutputSchema` 必须逐 Tool、逐 Provider 验证后启用。

#### 12.1.6.1 Claude Code 文件与搜索 Tool

以下 Tool 的行为原则、调用纪律和模型指导以 Claude Code 对应 Tool 为主，再按 Amadeus 真实能力裁剪：

| Tool | Claude Code 来源 | 必须保留 | 必须按 Amadeus 修改/删除 |
|---|---|---|---|
| `read` | `FileReadTool` | 文件读取、行号、targeted range、截断提示、文件非目录 | Amadeus 使用 workspace policy 和 `path/line/limit`；不宣称任意主机路径、图片、PDF、Notebook 或 screenshot 能力，图片由 `view_image` 负责 |
| `edit` | `FileEditTool` | read-before-write、精确字符串、缩进、唯一匹配、`replace_all`、文件非目录 | 明确 Amadeus 要求完整且未截断的 read snapshot，并保留 Diff→Approval→revalidate→atomic apply；不复制 Claude settings 专用规则 |
| `write` | `FileWriteTool` | 已有文件先读、Edit 优先、创建/完整重写、避免无请求文档 | 明确已有文件需要完整且未截断 read；Approval 和 atomic write 服从 Amadeus Contract |
| `glob` | `GlobTool` | 专用文件发现、pattern/path/limit、结果截断提示 | 结果按稳定路径排序而不是 Claude Code 的 mtime；不宣称 Agent Tool fallback；`include_hidden` 以实际实现为准 |
| `grep` | `GrepTool` | 优先使用专用搜索、path/glob/type/context/limit、ripgrep语义 | Amadeus 默认 literal，`regex=true` 才使用正则；不支持 multiline 或 Claude output modes/head offset，不得在 Prompt 中声明 |

`edit`/`write` Runtime 只接受完整 read snapshot；ToolSpec 必须直接写明“complete non-truncated read”，不能只写“read at least once”。大文件通过同一内容指纹下无 gap 的分页 line coverage 累积完整 snapshot，并在 ToolResult metadata 暴露 `complete_snapshot`；内容变化会清空旧 coverage，output/line truncation不计入coverage。单行超过配置 line limit 时当前基础协议明确无法建立complete snapshot，必须返回截断事实并拒绝edit/write，不能引导模型靠重复相同范围进入无效重试。

Claude Code 的长 usage instruction 最终进入对应 ToolSpec description/schema，并与 ToolDefinition、revision 和 handler 同属一个 owner。不得保留一份短 ToolSpec，再把另一份 Markdown 追加到 Collaboration Mode。

#### 12.1.6.2 Codex Runtime Tool

以下 Tool 以 Codex ToolSpec、schema guidance 和 lifecycle 为主要参考，但最终文本必须由 Amadeus 实际参数与 Runtime contract 生成：

| Tool | Codex 参考重点 | Amadeus 适配 |
|---|---|---|
| `update_plan` | 简洁 description、plan/explanation schema、至多一个 `in_progress`、`Plan updated` | 何时建立 checklist 属于 Base 的跨模式工作原则；Plan Mode 隐藏该 Tool，ToolSpec 不重复整段规划教程 |
| `request_user_input` | mode availability、1–3 questions、header/label/option guidance、recommended first、Other 自动添加 | ToolSpec 描述交互能力和当前可用模式；Default/Plan 的“何时询问”分别属于两种模式文本，不混入 Tool schema |
| `execute_command` | command/workdir/tty/yield/output budget、ongoing process、真实 exit结果 | 保留 Amadeus `command/cwd/timeout_ms/yield_time_ms/max_output_tokens/tty`，不改名照抄 Codex；Permission/Approval、风险和 session grant 继续服从 Claude Code 风格内层 Contract |
| `write_stdin` | 继续/轮询运行进程、yield/output、完成状态 | 保留 Amadeus `process_id + origin_call_id + chars/enter/eof`，不照抄 Codex numeric `session_id`；复用原 command Approval |
| `view_image` | 本地已有图片的视觉检查、model-aware visibility、`high/original` detail | 保留 Amadeus 支持的 PNG/JPEG/WebP/static GIF、bounded preparation 和单一环境，不增加 Remote/Multi Environment |
| Multi-Agent | critical path、bounded task、parallel delegation、sparse wait、close/status/notification | 采用 Codex V1 骨架，删除 worker write/fork/model override；加入 read-only explorer、Depth 1 和 Amadeus 实际参数 |
| MCP Resource | list/read resource、server/URI schema、模型上下文语义 | 保留 Amadeus 必填 server 和 lazy binding，不复制跨所有 server 的可选参数或未实现 template tool |

`request_user_input` 同时参考 Claude Code 的 question UX，但其 ToolSpec/Session lifecycle 以 Codex 为骨架。`execute_command` 同时参考 Claude Code Approval，但 Claude Code Bash Prompt 不是其外层 ToolSpec 的直接复制来源。

#### 12.1.6.3 Amadeus-specific Tool

以下 Tool 没有可以逐字复制的单一参考 Prompt，必须以 Amadeus 已实现 Contract 为权威，只选择性吸收参考项目的架构原则：

- `web_search`/`web_fetch`：保持 snippet evidence 与 full-page verification 分层、URL/redirect safety 和 untrusted-result标记；不因 Codex 有 native web search 就更换稳定 Tool schema。
- `read_skill`：描述 Skill catalog、main `SKILL.md`/受限 `references/*`、bounded `line/limit` 和 untrusted observation；它不是普通 `read` 的别名。
- `mcp_list_tools`/`mcp_call`：描述 lazy server discovery、sanitized schema、catalog revision、调用前先发现和 untrusted external result；它们是 Amadeus lazy wrapper，不伪装成 Codex direct dynamic Tool。
- 远端 MCP ToolSpec：server 返回的 description/schema 是不可信能力 metadata，必须经过 sanitize/binding revision 后才能进入 frozen ToolRouter，不得提升为 Base 或 Collaboration Mode instructions。

较长指导可以直接成为完整 ToolSpec description，或由 ToolDefinition 在构造 Spec 时组合；它必须和 Spec revision、模型可见 catalog 及实际 handler binding 同属一个 owner。删除 `ToolPromptOrder`、`toolGuidance(toolNames)`、按可见工具把 Markdown 附加到 mode/subagent instructions 的路径。

Prompt 装配只保留一条生产主链：`Session Base + typed WorldState/canonical ContextManager + StepContext ToolRouter + TurnContext → Prompt`。旧 `Assets.Base`、`Assets.Compaction`、`DeveloperInstructions(mode, toolNames)`、`ContextUpdateEvent` 和 request-only developer string 注入必须在迁移时物理删除。

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
- Context 中注入生效内容，不只提供文件路径让模型自行读取；模型可见文本使用 Codex 的 `# AGENTS.md instructions for <cwd>` 与 `<INSTRUCTIONS>` 结构。
- AGENTS.md fragment 使用 contextual user role。指令文件来源、作用域和 Hash 可以进入内部 snapshot/诊断，但不得把 Amadeus 自定义 metadata JSON 混入模型可见正文。

目录作用域必须接入真实 Tool target，而不只在 Turn 开始时对初始 CWD 解析一次：

- `AgentsMdManager` 是 AGENTS.md discovery、作用域解析、缓存和 revision 的唯一 owner；输出使用 `LoadedAgentsMd`。
- 每次 capture StepContext 时，Session 根据当前 CWD、已知目标路径和 AgentsMd revision 得到完整 `LoadedAgentsMd`；`AgentsMdState` 相对 WorldState baseline 生成 unchanged、replacement 或 removal fragment。
- 热路径可以按 canonical path 的 file fingerprint（存在性、大小、mtime 和内容 hash）复用已解析 Document，但 fingerprint cache 只能是 derived cache；写入/执行前仍必须对目标重新执行 stale snapshot 校验。
- Read/Search 发现新的目录作用域后，可以让下一 Model Step 重新 capture；Edit/Write 和带目标 CWD 的 Command 在 Prepare 阶段校验其目标仍受 StepContext 中已加载的 AgentsMd snapshot 约束。
- snapshot 过期时返回 typed stale result，由下一 Model Step 重新 capture；不使用 `MarkSampled`、`context_refresh_required` 或旧 Resolver callback 驱动第二条 instruction 主链。
- Tool 不直接修改 ContextManager；它只返回目标与 stale 事实，Session 决定是否记录新的 AGENTS.md contextual ResponseItem 和随后对应的 WorldState patch。

### 12.3 ContextManager

ContextManager 属于 SessionState，是当前模型可见历史的唯一所有者：

```text
Initial Rollout Replay
→ restore Session Base + ResponseItem history + WorldState/TurnContext baselines
→ Runtime Incremental Record
→ apply current WorldState full/diff before sampling
→ Snapshot(ModelInfo, PromptShape)
→ immutable PromptSnapshot
```

它负责：

- 记录模型可见的 User、Assistant、ToolCall、ToolResult 和 typed contextual fragment。
- 保持 ToolCall/ToolResult 原子配对。
- 删除孤立 ToolResult，并为中断或缺失结果补显式 synthetic ToolResult。
- 按 ModelInfo 的限制投影超大 Tool Result。
- 根据模型输入能力移除不支持的内容类型。
- 校验并应用 typed Compaction Replacement History。
- 保存 TokenUsageInfo、最新 ActiveContextTokens/estimated checkpoint、history version、TokenCount checkpoint sequence、rollout source sequence、WorldStateSnapshot baseline 和 TurnContext reference。
- 返回不可变的 Prompt 输入快照。

ContextManager 是活动 Session 的内存 history owner；canonical Rollout 是 durable source。Resume 时从 SessionMeta/ResponseItem/WorldStateItem/TurnContextItem/CompactedItem 重建一次，运行期间由 Session 在 durable append 成功后对同一 typed fact 执行增量 record。`run_turn`、TUI、CLI 和 CompactTask 不得各自实现第二套历史裁剪或消息投影，也不得在 Snapshot 阶段从 side map 合成一批未进入 canonical history 的当前前缀。

只有 Session 可以提交 ContextManager mutation。Task/`run_turn` 通过 Session typed methods 请求 canonical append，并在 sampling request 构建时取得 immutable `PromptSnapshot`；不得持有 `*ContextManager` 或调用无 Rollout 对应事实的 Record/Replace fallback。正常 append 不得读取全部 `[]RolloutLine` 再 rebuild；Resume 从 Rollout 重建，Compaction 通过 Session-owned durable install 原位替换模型历史。热路径应优先复用同一 history/version、ModelInfo、Tool revision、WorldState revision 下的 derived normalization、token estimate 和 Prompt revision，避免 `ActiveContextTokens` 与 `Snapshot` 对同一历史重复全量复制；任何缓存都不能成为第二份 history owner。增量 record、replacement preview/install 与 Resume projector 必须共享同一 typed normalization/estimation 规则并通过 semantic-equivalence 测试。

### 12.4 Token Accounting

Token accounting 必须区分四个不同业务事实：

1. `TotalTokenUsage`：Thread 内所有成功普通 sampling 与 Compaction request 的累计消耗，用于成本/统计和 SQLite `tokens_used` projection。
2. `LastTokenUsage`：最近一次成功 Provider request 的 usage，是 Provider 已观察到的 active context 基线。
3. `ActiveContextTokens`：使用 TokenCountEvent 保存的明确 active checkpoint，加该 canonical event sequence 之后尚未被 Provider 观察的 User/Tool/Context items；成功响应 checkpoint 通常为 LastTokenUsage.TotalTokens，未持久化输出的失败响应使用 InputTokens，Compaction replacement 后使用新 replacement 的完整本地估算 checkpoint。
4. `EstimatedInputTokens`：发送下一 request 前对 exact StepContext Prompt 的结构化估算，只用于 preflight、fallback、Compaction 后重算和诊断。

不得把累计消耗当成当前 context occupancy，也不得让 live TUI 使用单次 usage、Resume TUI 却累加全部历史 usage。Provider 返回的真实 usage 是完成请求后的权威基线；TokenEstimator 不覆盖该事实，只补充尚未被 Provider 观察的本地增长或完整 preflight。

稳定数据模型对齐 Codex：

```go
type TokenUsage struct {
    InputTokens       int64
    CachedInputTokens int64
    OutputTokens      int64
    ReasoningTokens   int64
    TotalTokens       int64
}

type TokenUsageInfo struct {
    TotalTokenUsage   TokenUsage
    LastTokenUsage    TokenUsage
    ModelContextWindow int64
}

type ContextWindowTokenStatus struct {
    ActiveContextTokens    int64
    EstimatedInputTokens   int64
    AutoCompactTokenLimit  int64
    FullContextWindowLimit int64
    BaseWindowTokensRemaining int64
    TokenLimitReached      bool
}
```

`TokenUsage` 属于 LLM domain 的 Provider-neutral response contract；`TokenUsageInfo` 属于 Event Protocol，并由 ContextManager 保存同构内存 snapshot；`ContextWindowTokenStatus` 是 Session 内部 policy/read model，不进入 Provider Adapter、Rollout schema 或 TUI 自行计算路径。

每个成功 Model/Compaction request 只在 Session 中调用一次 `RecordTokenUsage`：累加 TotalTokenUsage、替换 LastTokenUsage、写入完整 TokenUsageInfo snapshot，并发布同一 snapshot 的 TokenCountEvent。尚未被 Provider 观察的 canonical User/Tool/Context fact 在下一 Model Step 边界触发 `RefreshContextWindowStatus`，它可以发布 Total/Last 不变但 ActiveContextTokens 更新的完整 snapshot。TaskOutput、TUI 和 Application projector 不再二次累加 request usage。Compaction request 的真实 usage 仍进入 Total/Last；replacement 安装后 Session 只用本地估算设置新的 ActiveContextTokens checkpoint 和 estimated marker，不用估算值覆盖 LastTokenUsage。Resume 取最后一个 canonical TokenCountEvent snapshot，并在 Session spawn 时用恢复后的 history 校正 active suffix，而不是重放时逐条求和。

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
- Provider LastTokenUsage 与当前完整 Prompt estimate 都进入 Session-owned ContextWindowTokenStatus：post-response continuation 优先使用 active usage，pre-request 使用应用 WorldState diff 后的 exact assembled Prompt estimate；阈值判断不能散落在 PromptSnapshot、callback 或 TUI。
- 完整 Prompt 估算包含 BaseInstructions、ContextManager 输入、模型可见 Tool Specs、OutputSchema 和 modality cost。
- TokenEstimator 按结构估算 ResponseItem，而不是直接把 Go struct 的偶然 JSON 编码当成 wire contract。文本默认使用 provider-independent byte/rune heuristic；图片按 prepared dimensions/detail/patch cost 或稳定 fallback 估算，并排除 Base64 payload；Tool Call、Tool Result、Reasoning、ToolSpec 和 OutputSchema 分别计算明确开销。
- 未实现可信 tokenizer 时类型命名使用 `ApproxTokenEstimator`，不得用 `ConservativeEstimator` 声称对中文、图片或所有 Provider 必然保守；90% auto-compact limit 继续提供独立 headroom。
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

Compaction 是 Session-owned Runtime 用例，不是 TUI 文本操作，也不是返回 RolloutItem 的模型 helper。CompactionService 只负责纯粹的摘要 request/response；Session 负责 trigger/reason/phase、Item lifecycle、TokenUsageInfo、source validation、durable install、ContextManager replacement 和失败终态。

```go
// Stable lifecycle enums are owned by agent/protocol because Event and
// CompactedItem persist them.
type CompactionTrigger string // manual | auto
type CompactionReason string  // user_requested | context_limit
type CompactionPhase string   // standalone_turn | pre_turn | mid_turn

type CompactionSource struct {
    HistoryVersion         uint64
    CoveredThroughSequence int64
    SourceHash             string
    CanonicalHistory       []ResponseItem
    UserMessages           []ResponseItem
    PromptItems            []ResponseItem
}

type CompactionRequest struct {
    Trigger      CompactionTrigger
    Reason       CompactionReason
    Phase        CompactionPhase
    Source       CompactionSource
    Prompt       Prompt
    Model        ModelInfo
    Reasoning    *ReasoningConfig
}

type CompactionOutput struct {
    Message            ResponseItem
    FinishReason       FinishReason
    ReplacementHistory []ResponseItem
    TokenUsage          TokenUsage
}
```

CompactionTrigger/Reason/Phase 属于 Agent Protocol 的稳定枚举；`internal/agent/compact`、Session、Event 和 Rollout 共同引用它们。Rollout 不得依赖 `internal/agent/compact` runtime package，CompactionService 也不得重新定义同名 enum 后在边界转换字符串。

`ReplacementHistory` 使用完整 typed ResponseItem，不使用只有 `Role/Content` 的 `ReplacementMessage`。Amadeus 保留 SourceHash 与 CoveredThroughSequence：Compaction 异步执行期间若有合法 trailing canonical fact 到达，install 只替换已校验 prefix 并保留 trailing items；source hash/version 不匹配时返回 typed stale compaction failure，不覆盖新事实。

手动 `/compact` 生命周期：

```text
CompactOp
→ CompactTask（standalone, non-steerable）
→ Session.runCompaction{manual,user_requested,standalone_turn}
→ ItemStarted(ContextCompaction)
→ capture exact CompactionSource + StepContext
→ CompactionService.Generate
→ Session.RecordTokenUsage(compaction request)
→ validate summary/finish reason/no Tool Calls
→ Session.installCompaction durable append
→ ContextManager replacement + ActiveContextTokens recompute
→ TokenCountEvent
→ ItemCompleted(ContextCompaction)
→ WarningEvent
→ Turn terminal
```

自动 Compaction 是 regular `run_turn` 的 inline lifecycle，不创建嵌套 CompactTask：

- Turn 首次 sampling 前对 exact StepContext 执行 preflight estimate；超过 auto limit 时使用 `auto/context_limit/pre_turn`。
- 一个 sampling request 及其 Tool Result 已 canonical record 且确实需要 model/steer follow-up 时，按 Provider active usage 检查；超过 limit 时使用 `auto/context_limit/mid_turn`。
- auto-compaction request 不包含尚未 drain 的 steer。compact 后若必须恢复原 model/tool continuation，先继续原 continuation；若只有 steer 需要 follow-up，允许直接 drain。
- manual 与 auto 复用同一 CompactionService、Turn-scoped ModelClientSession retry policy、typed output 和 Session.installCompaction；差异只存在于 trigger/reason/phase 和是否拥有 standalone Task。

Replacement History 对齐 Codex 本地 compaction 语义：Context projector 以 typed MessageOrigin 区分真实 User、SubAgent notification 和 Runtime/contextual user fact，CompactionSource.UserMessages 只携带真实用户输入；服务从最新真实 User messages 向前选择有界总 token budget，保持原顺序，最后追加 `User(SUMMARY_PREFIX + summary)`。不得按 role 或 XML/string prefix 猜测真实用户来源；summary 不作为普通 Assistant 完成消息展示。pre-turn/manual replacement 不固化可能过期的 context，并清空 WorldState/TurnContext reference，让下一正常 Turn full reinject；mid-turn replacement 使用当前 exact StepContext 重新渲染 full initial context，插在最后真实 User/summary 之前，并与新 WorldState baseline 原子安装。

Provider 成功返回 Compaction response 后，Session 必须先通过普通 `RecordTokenUsage` 持久化本次真实消耗，再校验 summary、finish reason、Tool Call absence 和 source freshness；即使随后 output invalid、source stale 或 replacement persistence 失败，TotalTokenUsage 仍包含已经发生的请求。replacement install 本身是另一个原子 durability boundary：Session 用当前 ContextManager preview 验证 source/replacement 并计算新的 ActiveContextTokens/estimated checkpoint，再将 `CompactedItem + refreshed TokenCountEvent` 作为同一 durable append 写入。append/flush 成功后才更新 live ContextManager 并发布 completed Event。失败、取消、Provider error、stale source 或 persistence error 均保持原 ContextManager 不变，并以相同 ItemID 完成 failed/aborted lifecycle；不得使用本地摘要 fallback。

Context-window failure 必须 typed：Provider Adapter 将明确的 context length rejection 归一化为 `context_window_exceeded`。Compaction request 自身超限时，可以按完整 User/Tool-call group 从最旧端有界裁剪后重试，不能删除单边 ToolCall/ToolResult；裁剪到最小输入仍失败则终止。成功 install 后立即比较 before/after active tokens；没有实质下降或仍达到硬窗口时返回 `compaction_insufficient`，同一 history/window 不得重复无界 compact。

第一版固定采用 total active context，不实现 Remote Compaction、`body_after_prefix`、fallback compact prompt、window UUID 或 Codex TokenBudget feature；只有真实 Provider/runtime 需求出现时才扩展，不提前建立空状态。

## 13. LLM Domain 与 Provider Adapter

### 13.1 Domain Port

Agent Runtime 依赖 Amadeus LLM Domain，而不直接依赖 SDK 类型：

```go
type Client interface {
    Stream(context.Context, Request) (Stream, error)
}
```

Domain Request 统一表达：

- 独立 BaseInstructions 与 conversation developer/user/assistant/tool 语义；Base 不伪装成普通 system ResponseItem。
- Tool definitions。
- Tool call 与 Tool result 配对。
- model selection 与 Provider 支持的显式请求控制。
- reasoning 与 typed TokenUsage。

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

每个成功 Response 的 `TokenUsage` 是单次 request usage，不是 Turn aggregate；Adapter 不累加跨 request 状态。Session 在 canonical ResponseItem 已接纳后调用唯一 RecordTokenUsage，维护 TokenUsageInfo 与 ContextWindowTokenStatus。

普通 Domain Request 不保存稳定的 `temperature` 或 `max_output_tokens` 字段。Amadeus 不用内部默认值伪装模型厂商默认值；Responses 与 Chat Completions Adapter 都必须在普通 sampling 和 Compaction 中省略对应 wire 参数。未来确有 Provider 必需扩展时，只能由经过契约测试的 Dialect/request extension 显式提供，不能重新变成所有 Provider 共用的用户配置。

### 13.2 OpenAI Adapter

`internal/llm/openai` 是 Adapter，不称为 Domain Client。

它负责：

- Responses API 请求转换和流聚合。
- Chat Completions 请求转换和流聚合。
- Provider Dialect 的最小兼容差异。
- Developer role 降级。
- Tool call argument 增量聚合。
- Provider Error、retryability、Retry-After、transport/idle timeout 和 TokenUsage 归一化；明确 context length rejection 映射为 typed `context_window_exceeded`。

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

- 当前配置是唯一的 versionless strict schema，不包含顶层 `version` 字段、`CurrentVersion` 常量或 schema-version provenance。Loader 使用 `KnownFields(true)`；任何旧 `version:` 字段与其他删除字段一样直接返回 unknown-field error，不提供 decoder、migration、alias 或静默忽略。
- `model`、`model_context_window`、`model_reasoning_effort`、`model_input_modalities`、`model_supports_original_image_detail`、`model_auto_compact_token_limit` 和 `tool_output_token_limit` 属于当前 Model/Runtime 配置，不进入 `ModelProviderInfo`。
- `model_provider` 选择 `model_providers` 中的用户定义 Provider；Provider 只保存 transport、auth、wire API、Dialect、timeout、retry 和 capability。
- `model_context_window` 在 Amadeus 尚无可信 Model Catalog 时必须显式为正数；不得为任意未知模型伪造统一的 128K Context Window 默认值。
- `model_auto_compact_token_limit` 可省略，省略时取 Context Window 的 90%；显式值必须大于 0 且不超过该派生上限。
- `tool_output_token_limit` 是顶层可配置项，默认 `10000`，覆盖 ModelInfo 的 Tool Output truncation policy；不得放回单个 Provider。
- `model_input_modalities` 默认仅为 `[text]`，只接受 `text`/`image` 且必须包含 `text`；`model_supports_original_image_detail` 默认 `false`，为 `true` 时必须同时声明 `image`。两者由 Session 冻结进 ModelInfo，不能从 Provider/Dialect 推断。
- Codex 的 `model_auto_compact_token_limit_scope` 依赖 carried-prefix/body-after-prefix 和 window identity。Amadeus W 第一版明确固定采用 total active context 语义，不暴露 scope 配置；只有真实 Provider/runtime 需求出现时才同时引入完整 window lifecycle，不能先增加未接线字段。
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

现有 `provider.timeout` 不属于 Codex 上述三个 retry/reconnect 字段。K 阶段必须审计并明确其 request timeout 语义；它不得作为整个活跃 streaming response 的固定 wall-clock deadline，从而在持续有 Delta 时抢先于 `stream_idle_timeout` 终止长响应。bootstrap 只负责装配，不把 retry policy 写进 CLI、TUI 或 Task。

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

目录与所有权同时遵循两套参考：Codex 决定外层 `StepContext → ToolRouter → handler/event/rollout` 关系，Claude Code 决定内层 `ToolUseContext → ValidateInput → Prepare → Permission/Approval → Execute` 关系。对应到 Go：

- `internal/agent/session` capture StepContext、冻结 ToolRouter、创建 Tool lifecycle observer，并把 Tool Call/Result 转成 Protocol/Rollout；不再通过含义模糊的 `agent/engine` 中转。
- `internal/tool` 只定义通用 Tool contract、Registry/Router、argument normalization、batch concurrency、PermissionService 和执行结果；不得 import `tool/builtin`，也不得用工具名 switch 认识完整产品 catalog。
- `internal/tool/builtin` 拥有具体 Tool schema、Validate/Prepare/Execute 和具体调用摘要；文件修改共享链保留在同 package 的 `file_change.go`，不复制到 `edit`/`write`。
- `internal/policy` 拥有 Approval request/decision/port/coordinator、Session grant 和 command/file/network policy presentation，不执行 Tool。
- `internal/tui` 拥有 HistoryCell、Approval overlay 和用户可见完成态；TUI 可以按 typed ItemKind/ToolName 选择专用 renderer，但不得把展示判断反向写入 Tool 执行协议。

Claude Code 为每个 Tool 建独立目录，是因为对应实现同时包含大型 Tool、Prompt、UI、types 和 helpers。Amadeus 的 Prompt/TUI 已有独立 owner，基础 Tool 规模也更小，因此目标布局是一 Tool 一责任文件，而不是一 Tool 一 Go package。只有 `execute_command`、`grep` 等单文件出现可独立测试的 parser/executor/renderer 子职责时，才先在 `internal/tool/builtin` 内拆文件；不能为形式相似创建人工 subpackage。

每个 Tool 必须表达同一组概念：

```go
type ToolSpec struct {
    Name         string
    Description  string
    InputSchema  json.RawMessage
    OutputSchema json.RawMessage // optional
    Strict       bool            // Provider/schema verified only

    SideEffect SideEffect // runtime policy metadata
    Idempotent bool       // runtime scheduling metadata
}

type ToolDefinition interface {
    Name() string
    Spec() ToolSpec
    ValidateInput(ToolUseContext, any) error
    Prepare(ToolUseContext, any) (PreparedToolUse, error)
    Execute(ToolUseContext, PreparedToolUse) (ToolResult, error)
    SupportsParallelToolCalls() bool
}
```

`Name/Description/InputSchema/OutputSchema/Strict` 组成模型可见 Tool contract；`SideEffect/Idempotent` 服务 Runtime policy、并发和 Approval，不直接当作提示文本发送。Session 将 `tool.ToolSpec` 转换为 `llm.ToolSpec` 时必须完整保留模型可见字段，不能像当前路径一样只复制 Name/Description/InputSchema 后静默丢弃 Strict/OutputSchema。

`ValidateInput` 只负责 Schema、类型、字段关系和工具输入的客观合法性；`Prepare` 负责解析路径、读取必要快照、计算副作用、生成 Approval 所需的结构化预览，但不得产生最终文件副作用；`Execute` 只能执行已经通过权限和 Approval 的 `PreparedToolUse`。工具不得在 `Execute` 中重新解释原始模型输入，也不得通过隐式全局状态恢复准备数据。

```go
type InvocationMetadata struct {
    SessionID SessionID
    ThreadID  ThreadID
    TurnID    TurnID
    Source    ToolCallSource
}

type Invocation struct {
    SessionID SessionID
    ThreadID  ThreadID
    TurnID    TurnID
    Call      ToolCall
    Source    ToolCallSource
}

type ToolUseContext struct {
    Invocation    Invocation
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

Tool identity 使用固定规则：SessionID 只用于 tree-level correlation/audit，ThreadID 表示当前 Tool owner 和 Event scope，TurnID 表示本次 Turn。`spawn_agent` 必须从 Invocation.ThreadID 取得 parent ThreadID；`send_input`、`wait_agent` 和 `close_agent` 的目标参数在 Tool boundary 解析为 ThreadID。任何 Multi-Agent Tool 都不得从 Invocation.SessionID 推导 parent/target Thread。ApprovalRequestEvent 继续按 ThreadID + TurnID 路由；共享 SessionID 不自动共享 Root/child 的 SessionPermissionContext。

Audit record 至少携带 SessionID、ThreadID、TurnID、RequestID 和 ToolName：SessionID 用于聚合同一 agent tree，ThreadID/TurnID 用于定位实际执行者。command/file/network/MCP audit 都从 Invocation metadata 取得 identity，不能由 Tool 自行读取 UI current session 或把 ThreadID 写进名为 SessionID 的 string 字段。

`execute_command`/`write_stdin` Tool schema 中为兼容模型语料而存在的进程 `session_id` 表示 ProcessManager 分配的 process session/process ID，与 Agent SessionID 属于不同命名空间；内部必须保持 process ID 类型，不参与 Thread/Session UUID parsing。

`ToolRouter` snapshot 同时保存模型可见 ToolSpec、确切 ToolDefinition handler、MCP binding、visibility 和 parallel flag；模型看到的 spec 与随后 dispatch 的 handler 必须来自同一 snapshot。执行阶段不得按工具名重新查询当前 mutable registry，也不得只用 `AllowedTools`/revision 字符串假装冻结 handler identity。

Tool call presentation 不是 generic Router 事实。通用 fallback 可以按 SideEffect 生成安全摘要；`read`、`web_fetch`、`spawn_agent`、MCP 等具体 action summary 必须由 builtin registration、Session Tool Event policy 或 typed TUI projection 拥有，不保留 generic `internal/tool/presentation.go` 中枚举全部产品工具的反向依赖。

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
| `view_image` | Codex `view_image`/image preparation + Claude Code 只读权限边界 | model-aware visibility、canonical path、目录 Approval、bounded decode/resize、typed image part、单份持久化 | Remote/Multi Environment、Analytics、图片生成、终端图像协议 |
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

#### `view_image` 模型、图片准备与持久化 Contract

`view_image` 保留为独立只读 Tool，不重新并入 `read` 的文本结果，也不增加 Codex 的 Remote/Multi Environment 参数。基础 Schema 使用 `{path, detail?}`：`path` 必填；`detail` 默认 `high`，只有当前 ModelInfo 明确支持 original image detail 时才向模型暴露 `original` 枚举。Amadeus 当前只有单一 Project/Working Directories 文件环境，因此不增加 `environment_id`。

Tool 可见性必须由当前模型的冻结 `ModelInfo.InputModalities` 决定，不能继续从 OpenAI-compatible Dialect 或 Provider 类别推断所有模型都支持图片：

- 当前模型不支持 image input 时，不向该 Turn 的 Tool Snapshot 暴露 `view_image`。
- Execute 与 Provider Adapter 仍执行防御性 modality check，避免配置、Resume 或模型切换造成不可诊断的上游失败。
- `SupportsImages` 只能表达 transport/adapter 能否编码图片内容；具体模型是否支持图片由 ModelInfo 决定，二者不得合并为同一布尔事实源。

路径与 Permission 继续采用 Amadeus 现有约束：工作目录内默认 Allow，工作目录外使用 canonical read-directory Ask/Session Grant，Denied/ReadOnly/符号链接等客观文件系统规则不能被 Grant 绕过。Approval 后 Execute 必须在真实打开文件前重新执行路径和文件类型检查；不允许只信任 Prepare 阶段保存的字符串路径。

图片读取与模型输入准备拆成两个职责：

```text
view_image Tool
→ canonical path / Permission / bounded file read
→ internal image preparation service
→ PreparedImage
→ ToolResult image part + metadata
→ Provider Adapter
```

`view_image` 文件只负责 Tool Schema、Validate/Prepare/Permission/Execute orchestration；解码、格式判断、尺寸预算、缩放、重编码和可选缓存进入独立 image preparation package，不继续堆入 Tool 文件。`PreparedImage` 至少包含 source/prepared MIME、source/prepared width/height、source/prepared bytes、effective detail 和 Base64 payload。

输入安全限制与模型预算分开处理：

- source file 默认仍限制为 20 MiB、单边 16384、6400 万像素；这些是拒绝异常输入的安全上限，不是最终发送尺寸。
- `high` 是默认准备模式，保持纵横比并限制单边不超过 2048，同时使用至多 2500 个 32×32 patch 的等价像素预算。
- `original` 只在模型明确支持时可用，并仍受单边 6000、至多 10000 个 32×32 patch 的等价预算约束；“original”不表示无界原始字节直传。
- PNG、JPEG、WebP 与静态 GIF 为允许格式；格式由内容检测而不是扩展名决定。静态 GIF 规范化为 PNG，动态 GIF 保持明确拒绝，不静默只取第一帧。
- 需要缩放或格式规范化时重新编码；能够安全保留的 ICC/EXIF 元数据可以保留，但不得为了元数据兼容牺牲 bounded output。基础版不增加视频、SVG、PDF、远程图片 URL 或 Browser rendering。

ToolResult 必须只包含一个 prepared image Part；Text 只提供简短、可诊断的路径与准备结果，不嵌入 Base64。Typed Data/Metadata 至少包含 source path、effective detail、source/prepared dimensions、prepared media type 和字节数。Responses Adapter 将 image Part 编码为 FunctionCallOutput input image；Chat Completions 的 synthetic user image 仍属于 Adapter 兼容细节，不得改变 canonical ToolResult 或伪造成用户主动上传图片。

图片上下文不能视为零成本：Context Manager 使用 prepared dimensions/detail 估算图片预算，Compaction 与模型切换必须能够将不再受支持或超出预算的图片替换为稳定 omission marker。基础版不要求复刻 Codex Analytics，但不得只对文本 Tool output 应用 Token limit 而让图片完全绕过上下文预算。

Canonical Rollout 中图片 Base64 只保存一次：模型续接使用的 `ResponseToolResult.Parts` 保存 prepared image；`ItemCompletedEvent.ToolResult` 与 TUI projection 只能保留 display-safe metadata，不得再次序列化 `ContentPart.Data`。Resume 从 canonical ResponseToolResult 恢复模型上下文，从完成事件恢复界面摘要，二者不得互相解析。

TUI 继续复用统一 Tool lifecycle，不新增 Codex `ImageView` 第二 Event 或独立 Context owner。Rich TUI 增加轻量 `ViewImageCell`，显示 `Viewing/Viewed image`、路径、source→prepared dimensions、MIME 与失败状态；live 与 replay 使用同一 display metadata。图片像素预览、Kitty/iTerm 图像协议、IDE Preview 和系统通知不进入基础范围。

图片处理缓存属于后续性能优化。若真实 profiling 证明重复解码成为瓶颈，可以按内容 digest + preparation mode 增加有界内存缓存；Q 基础实施不得先引入跨 Session 持久化缓存、文件监听或图片资产数据库。

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
    Queue   bool
}
```

```text
普通文本
→ InputResult{Text: text}

运行中普通文本 + Tab
→ InputResult{Text: text, Queue: true}

/plan
→ InputResult.Command(Command: plan)

/plan <task>
→ InputResult.Command(Command: plan, Args: <task>)
```

Popup 只负责过滤和选择 `BuiltinSlashCommands`；Enter 后返回普通提交/命令结果，运行中的 Tab 对普通文字返回 queue result，Esc 只关闭 Popup。Slash Popup 存在 selection 时 Tab completion 优先，不产生 queue result。命令历史记录只有在分发成功后才提交；queued ordinary text 可以在 enqueue 时进入本地输入回忆，但不进入 transcript/canonical history。

`InputResult.Queue=true` 只允许与非空 `Text` 组合，`Command` 必须为空；命令、空输入、idle composer 和 popup completion 不产生 queue result。该字段表达 Composer action disposition，不是 Runtime admission。

### 18.3 单一分发中心

TUI 的活动 model 承担类似 Codex `ChatWidget` 的统一分发职责：

```text
Composer
→ InputResult
→ model.dispatchCommand
→ TUI Local Action / AppEvent / Session Op
→ Application 或 Runtime owner
→ Event/EventMsg / typed AppEvent result
→ HistoryCell / TUI Projection
```

`internal/cli` 只负责 CLI 参数和顶层分发，`internal/bootstrap` 只负责依赖装配，`internal/tui` 负责交互启动；三者都不在 TUI 外维护 Slash Command `switch`。不保留 Plain Controller、`CommandHandler`、`TaskHandler` 或第二套 Slash Command 执行路径；`--plain` 删除，不作为另一套交互运行时维护。

命令按最终动作分为三类，但分类只服务于分发实现，不引入额外的路由抽象：

| 类型 | 命令 | 执行方式 |
|---|---|---|
| TUI Local | `/copy` | 复制最近 Assistant 回复，不创建 Turn |
| Application Command/Query | `/resume`、`/skills`、`/rename`、`/delete`、`/status`、`/mcp`、`/clear`、`/exit` | 转换为 typed AppEvent，由 Application/Thread owner 执行并返回结构化结果 |
| Session/Turn Operation | `/compact`、`/plan` | `/compact` 提交 `CompactOp`；`/plan` 提交独立 settings update；`/plan <task>` 提交携带 Plan mode override 的单个 `UserInputOp` |

`/plan` 不直接修改 TUI 的本地模式变量，也不通过 callback 返回模拟 Session 已接受设置。TUI 通过 active Thread attachment 提交 `ThreadSettingsOp{CollaborationMode: Plan}`；Session 发布 typed `ThreadSettingsAppliedEvent` 后，TUI 才更新模式投影。带参数的 `/plan <task>` 不使用 `pendingModeTask`，严格遵循：

```text
SlashCommand::Plan(task)
→ UserInputOp{Content: task, ThreadSettings.CollaborationMode: Plan}
→ Session 串行接纳 Submission
→ validate immutable settings update
→ attempt Steer or Started admission
→ if Steered: apply settings after steer succeeds; current TurnContext remains frozen
→ if Started: apply settings before freezing TurnContext.CollaborationMode
→ optional ThreadSettingsAppliedEvent with the applied snapshot
→ start or continue RegularTask/run_turn
```

快捷模式切换复用独立 `ThreadSettingsOp`/Event 生命周期；设置失败时保留原模式并发布 correlated `ErrorEvent`，不启动任务。ActiveTurn 运行期间的独立 settings update 必须拒绝或按 Session 明确的 deferred-operation 规则处理，不能改变当前 Turn。TUI 不保存 `pendingModeTask`，也不定义 `CollaborationExecute` 第二套 enum。旧 `FullscreenPermissionModeSetter` 和 `fullscreenPermissionModeDoneMsg` 不作为最终目标保留。

Slash Command 不是 EventMsg。命令执行引发的状态变化才通过 EventMsg、Rollout 和 TUI Projection 传播；纯 TUI 操作不写入 canonical history。

### 18.4 AppEvent 与命令生命周期

Slash Command 分发后的异步工作使用 Codex 同构的 typed AppEvent，不使用 `func(context.Context) (string, error)` 作为通用命令边界。字符串只允许存在于最终 HistoryCell 的展示字段中，不能承担 Thread attach、history replay、running state、empty state 或 typed inventory 的业务语义。

TUI Application 必须像 Codex `App` 一样持续拥有当前 Thread attachment 和事件路由，而不是让每次 `runTask` 或 Slash Command callback 临时调用 `waitTurn` 消费 `SessionIo`：

- 同一时刻只有一个 active Thread attachment；它是 `SessionIo.Events` 和 termination 的唯一消费者，并在内部从 EventMsg 派生 transcript/status read model。
- 普通用户输入与 `/compact` 只负责向 active `AmadeusThread` 提交 typed Op；Turn running、approval、completion 和 history 更新全部由 attachment event pump 送回 Bubble Tea AppEvent。
- Tab queue 在提交前属于 TUI input state；它只消费 matching attachment 的 terminal Event 来触发下一次普通提交，不创建第二个 SessionIo consumer 或 TUI terminal truth。
- Resume 成功后先停止旧 attachment 的转发，再原子安装新 attachment；带旧 ThreadID 或旧 attachment generation 的迟到消息必须被丢弃。
- 生产代码只保留 TUI attachment event pump 这一套 SessionIo consumer；CLI 和其他外层 package 不得建立并行事件循环或第二套 Turn 完成协议。
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
    Generation    uint64
    SessionID     protocol.SessionID
    ThreadID      protocol.ThreadID
    Title         string
    Configuration protocol.SessionConfiguration
    Items         []protocol.TurnItem
    TokenInfo     *protocol.TokenUsageInfo
    ActiveContextTokens int64
    ActiveContextEstimated bool
}
```

`SessionID + ThreadID` 与 live `SessionConfiguredEvent` 使用相同 identity contract；`Configuration` 与 live `SessionConfiguredEvent`/`ThreadSettingsAppliedEvent` 使用同一个 typed `SessionConfiguration` 模型。Snapshot 不再分别保存 Mode、Provider、Model 或 CWD 的影子字段。`Items` 必须由目标 Thread 的 canonical rollout 投影得到；TUI 只能通过既有 `TurnItem → HistoryCell` replay 链渲染，不能直接解析 rollout，也不能复用切换前 Thread 的 `TranscriptState`。

TUI attachment、迟到 Event、MCP inventory、Git branch lookup 和 overlay matching 继续只使用 `attachment generation + ThreadID`；SessionID 不替代具体 Thread 路由。`SessionOption.ID`、`AppExitInfo.ThreadID`、resume picker 和 exit resume hint 始终保存 ThreadID。状态面板可以持有 SessionID 供诊断，但 Codex 风格用户可见 “session/thread id” 项仍显示当前可恢复的 ThreadID。

TUI startup state 只允许携带 Version 等真正属于进程启动且不会随 Thread attach 改变的静态展示信息。NoColor、初始终端尺寸等 TUI options 继续属于界面启动参数，不并入 Session 状态。CWD、Provider、Model、ReasoningEffort、Mode、Thread title、TokenUsageInfo 和 ActiveContextTokens 都属于 active Session/Thread read model，必须来自 `ThreadViewSnapshot` 或 live typed Event；不得在 Startup、Application Status 和 TUI appModel 中建立三份并列 owner。

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
- canonical User ResponseItem 必须投影为 UserMessageItem，CompactedItem 必须投影为 completed ContextCompactionItem；用户消息和压缩边界不能只存在于首次 live TUI 的本地插入中。
- replay 不重放 Working、Approval wait 或 Delta 动画；已完成项必须与首次启动时的 Resume projection 完全一致。
- `/resume` 对齐 Codex，任务运行期间仍在 Slash Command Popup 中可见并可执行；切换由 Application 的事务化 Thread attach 接管，旧 active Thread 在新 Thread 成功 attach 后再异步 shutdown，不能在 popup filter 阶段隐藏该命令。
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
→ ItemStartedEvent(ContextCompaction)
→ durable CompactedItem + refreshed TokenCountEvent snapshot
→ ItemCompletedEvent(ContextCompaction)
→ Warning("Heads up: Long threads...")
→ TurnCompleteEvent / TurnAbortedEvent
→ TUI 离开 task running state
```

- `TurnStartedEvent` 必须携带稳定的 Turn/Task kind，使 TUI 显示 `Compacting context`，而不是把压缩伪装成普通用户 Turn 或仅修改不可见的 status 字符串。
- 提交 `CompactOp` 后 TUI 可以像 Codex 一样先设置 pending/running projection，消除事件往返前的空白；Runtime 的 `TurnStartedEvent`、`TurnCompleteEvent` 和 `TurnAbortedEvent` 仍是最终真相。
- completed ContextCompaction Item 必须在 `CompactedItem + refreshed TokenCountEvent` durable append 和 ContextManager install 成功后发布，并固定投影为 Codex 同义的 `• Context compacted`；生成的 summary 只属于 durable replacement history/compaction payload，不得作为 Assistant 消息、Reason 文本或 TUI 详情泄露。
- completed ContextCompaction Item 后必须立即发布独立的 typed `WarningEvent`，由黄色 `WarningHistoryCell` 显示 `⚠ Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted.`；该提示不是 compaction summary，也不并入 `TurnCompleteEvent` 文本。
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
→ reset TranscriptSurface canonical cells / native-print watermark / active frame
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

`StatusSnapshot` 至少包含当前 SessionID、ThreadID/名称、Model、Collaboration Mode、Token/Context usage、Turn phase 和 Provider identity；可选扩展数据保持 typed field，不拼接成 CLI 文本后再交给 TUI 解析。Thread attachment 和状态刷新仍以 ThreadID 为 target，SessionID 只提供 tree-level correlation/diagnostics。

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

`/exit` 是 Application shutdown request，不是 ChatWidget 直接返回 `tea.Quit`。其目标生命周期对齐 Codex 的 `ExitMode::ShutdownFirst → AppExitInfo → CLI exit messages`，同时适配 Bubble Tea renderer：

```text
SlashCommand::Exit
→ Exit(ShutdownFirst)
→ 关闭 Slash Popup / modal，进入 transient shutdown presentation
→ 标记 pending shutdown target
→ shutdown/detach active Thread 与后台资源
→ rollout flush / child process cleanup
→ ShutdownFinished 或 bounded timeout
→ 清除 active Composer/Footer frame
→ Application Exit(UserRequested, AppExitInfo)
→ tea.Quit
→ 恢复终端并从 tui.Application.Run 返回 AppExitInfo
→ internal/cli exit presenter 在 TUI 结束后打印 token usage / resume hint
```

- `ShutdownFirst` 是用户主动退出的默认模式；pending shutdown target 用于阻止正常 Thread termination/failover 逻辑把退出误判为异常切换。
- shutdown 必须有 UI escape-hatch timeout，避免损坏的 Runtime 让退出永久卡住；超时可以记录 warning 后退出，但不能把 `Immediate` 当常规路径。
- `Immediate` 只用于 fatal error、shutdown 已完成后的最终跳出或明确的紧急逃生路径，允许跳过 flush 的风险必须在类型命名中可见。
- ThreadManager 关闭多个独立 Thread 时可以并发发起 bounded shutdown，但每个 Thread 必须有独立的 completion/timeout 结果；已完成实例从 registry 移除，超时实例不得被假装成已关闭或静默丢弃。
- SessionServices 关闭 ProcessManager、MCP、Audit 和其他资源时，必须等待可观察的 `done`/close 结果；ProcessManager 的 cancel-only 操作不能作为正常 shutdown 已完成的替代。
- ThreadStore 正常 shutdown 必须 flush durable Rollout 后关闭 writer；Session 初始化尚未越过 configured/ownership barrier 时只能 discard writer，不得复用正常 shutdown 的 flush 语义。
- `Shutting down…` 是 BottomPane/Composer 区域的 transient presentation，不是 HistoryCell。退出过程不得向永久 transcript 插入 `Shutting down…`，也不得把 Composer、placeholder、Footer 或 Popup 留在最终 active frame。
- 旧 `/exit → tea.Quit` 和 `ShutdownFinished → tea.Quit` 的单阶段路径必须删除；最终 `tea.Quit` 只能由 Application shutdown lifecycle 的终态及 renderer drain 完成后触发。

退出结果使用 typed model，而不是让 CLI 在 TUI 结束后重新查询已关闭的 Runtime：

```go
type ExitMode uint8

const (
    ExitModeShutdownFirst ExitMode = iota
    ExitModeImmediate
)

type ExitReason uint8

const (
    ExitReasonUserRequested ExitReason = iota
    ExitReasonFatal
)

type AppExitInfo struct {
    TokenUsage llm.TokenUsage
    ThreadID   protocol.ThreadID
    ThreadName string
    ResumeHint string
    ExitReason ExitReason
    Error      error
}
```

`AppExitInfo` 对齐 Codex 同名概念，是 TUI Application 的最终返回值，不是 EventMsg、HistoryCell 或 persisted RolloutItem。字段规则如下：

- `TokenUsage` 来自退出时最终 `sessionViewState.TokenInfo.TotalTokenUsage`；不能在 CLI 侧重新打开 Session、累加 TokenCountEvent 或解析 Rollout 统计。
- `ThreadID`/`ThreadName` 来自 active Thread attachment；没有已建立 Thread 时允许为空。
- `ResumeHint` 只在目标 Thread 已可恢复时生成。基础命令形式为 `amadeus --resume <thread-id>`；若后续 picker/name UX 与 Codex 对齐，可使用名称作为辅助展示，但 ThreadID 仍是稳定 identity。
- 正常 `/exit` 使用 `ExitReasonUserRequested`。Fatal shutdown、renderer 或 Application 错误使用 `ExitReasonFatal` 并携带 `Error`；不能把 fatal error 降级成普通 usage summary。
- `tui.Application.Run` 的目标签名为 `Run(context.Context) (AppExitInfo, error)`；`internal/cli` 的 exit presenter 只在 Run 返回、Bubble Tea renderer 停止且终端恢复后格式化输出，`cmd/amadeus` 不参与呈现。

Codex 的 token usage 和 resume hint 不属于 TUI 最后一帧，也不进入 History。CLI exit presentation 固定为：

```text
Token usage: total=<total> input=<input> output=<output>
To continue this session, run amadeus --resume <thread-id>
```

- Token usage 为零时省略 usage 行。
- 不可恢复时省略 resume hint；fatal exit 且没有 resume hint 时至少保留 ThreadID，便于诊断。
- 终端支持颜色时，仅将 resume command 使用 ANSI cyan 高亮并以 foreground reset 结束，说明文字保持默认前景；No Color 时输出完全相同的纯文本内容且不包含 ANSI。
- 输出写入正常 CLI Output；fatal error 写入 ErrorOutput，并由既有 exit-code policy 决定非零退出码。

Bubble Tea renderer 的退出清理必须显式建模为两阶段 UI 生命周期：

```text
ShutdownFinished
→ model.exitPhase = DrainingFrame
→ View() 不再渲染 Composer / Popup / Footer / shutdown indicator
→ Bubble Tea 完成至少一次空 active-frame redraw
→ ExitFrameDrained
→ tea.Quit
```

原因是 `tea.Quit` 不渲染最终 frame，直接在 `ShutdownFinished` 返回 Quit 只会终止 renderer，可能在终端保留多行 Composer 的 `› Ask Amadeus to do anything`。该 drain 只使用 Bubble Tea 正常 Update/View/render 周期；禁止重新引入 output writer wrapper、terminal-height padding、`CursorUp`/`CursorDown` 或手写 ANSI cursor reposition。

验收要求：

- `/exit`、空输入时双击退出快捷键及其他 user-requested exit 复用同一个 `ShutdownFirst` lifecycle。
- shutdown 等待期间最多存在一份 transient `Shutting down…`，Composer 不接受新输入，重复 `/exit` 不重复提交 shutdown。
- 正常退出后最终 active frame 不残留 Composer placeholder、Popup 或 Footer。
- Token usage/resume hint 只在 TUI 完全退出后各打印一次；零 usage、不可恢复、fatal 与 timeout 路径均有独立测试。
- renderer drain、Application shutdown 和 CLI summary 必须分别可测试，不能依赖真实终端中的人工观察或时间竞争作为唯一验收。

#### `/copy`

`/copy` 与 Codex 一样保留为纯 TUI-local action：读取 Transcript 中最后一条 Assistant raw markdown，调用可注入的 clipboard backend，并插入 Info/Error HistoryCell 后 redraw。

- 没有 Assistant markdown 时显示 `No agent response to copy`；clipboard 失败显示具体 ErrorCell，成功显示轻量 InfoCell。
- `/copy` 不创建 AppEvent、Session Op、Turn、Rollout item 或 Application callback。为了形式统一而把它路由到 Runtime 属于错误的架构对齐。
- clipboard lease 等平台资源由 TUI/clipboard adapter 持有；Transcript 只保存 raw markdown，不从已渲染 ANSI 文本反向提取内容。

## 19. TUI

### 19.1 产品形态

默认使用 Bubble Tea + Lip Gloss 实现 Codex 风格 Rich TUI：

- 保留 Amadeus Logo 和 `>_` 启动视觉。
- 已完成HistoryCell经`TranscriptSurface` print watermark只提交一次到终端原生scrollback；mutable stream/tool tail留在有界active frame。禁止把完整历史塞进超高`View()`后依赖stock Bubble Tea裁顶。
- 鼠标默认保留终端选择文本能力。
- 输入运行期间仍可编辑；普通文本 Enter 始终提交 `UserInputOp`，由 Runtime admission 决定 Started、Steered 或 typed rejection，TUI 不依据 `running bool` 自行改写为下一 Turn。
- 运行中普通文本 Tab 显式进入 attachment-scoped `NextTurnQueue`；它与 Enter steer 分开展示、分开恢复，并只在 matching terminal 后逐条提交。
- Amadeus 只维护这一套 Rich TUI 交互运行时；不提供 `--plain` 第二套输入、状态和事件路径。

运行中按 Enter 提交普通文本并获得 `Steered` 时，TUI 保持当前 Turn 的 elapsed timer、Working/activity state、details store 和 active item，不重新执行新 Turn 初始化，也不插入第二个 Worked boundary。获得 `Started` 时由后续 `TurnStartedEvent` 初始化新 Turn；获得 `ActiveTurnNotSteerable` 等 rejection 时恢复或保留 composer 内容并显示明确错误，不能把 Enter submission 偷换成 Tab queue。Tab enqueue 时不插入 UserMessageCell；只有 terminal 后真正提交 FIFO head 时才使用普通 optimistic/canonical UserMessage lifecycle。

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
- AgentMessageCell（transient stable Assistant/Plan stream run）
- StreamingAgentTailCell（transient mutable Assistant/Plan stream tail）
- AgentMarkdownCell（final source-backed Assistant）
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

### 19.3 Assistant Markdown 与 Streaming Transcript

Assistant Markdown 的外层生命周期以当前 Codex TUI 的 `MarkdownStreamCollector → StreamingRender → StreamController → AgentMarkdownCell` 为主要参考。这里的“对齐”指 raw source owner、Item identity、增量边界、completion authority、resize/cache 和 final HistoryCell 职责同构，不要求把 Rust、Ratatui、pulldown-cmark 或 Syntect 逐字翻译为 Go。

Amadeus保留成熟Go组件：Goldmark是Markdown grammar/AST的唯一parser authority，Chroma是fenced code syntax highlighting authority。现有Glamour只能在迁移期辅助对照样式；由于它向当前调用方返回opaque ANSI string，AB完成后的Assistant/Plan生产renderer必须直接消费Goldmark AST/source offsets，不能把Glamour包在target-shaped adapter中继续作为HistoryCell数据模型，也不能手写CommonMark parser或从ANSI输出反向解析结构。若迁移后Glamour无其他真实调用方则删除依赖。

目标主链固定为：

```text
AgentMessageContentDeltaEvent{ItemID, Delta, Reset}
→ TUI EventReducer validates ItemID/lifecycle
→ ChatWidget-equivalent MarkdownStreamHost owns StreamController
→ StreamCore / MarkdownStreamCollector stores raw attempt source
→ newline-gated committed source → StreamingRender
→ StreamState commit queue emits stable AgentMessageCell run
→ mutable StreamingAgentTailCell stays in active transcript slot
→ ItemCompletedEvent{AssistantMessage, authoritative Text}
→ finalize source / reconcile authoritative Text
→ TranscriptSurface replaces the exact active attachment range
→ AgentMarkdownCell{MarkdownSource, RenderCache}
→ finalized cell prints once to native scrollback
→ mutable cells remain in bounded active frame

Resume:
completed Assistant TurnItem
→ AgentMarkdownCell directly
```

#### 19.3.1 Codex Reference Chain And Amadeus Target

Codex 的完整链路有明确、不可混淆的 owner：

```text
App-server delta / completed ThreadItem
→ ChatWidget (stream lifecycle, status, interrupt ordering)
→ StreamController / PlanStreamController
→ StreamCore { MarkdownStreamCollector, StreamingRender, StreamState }
→ transient AgentMessageCell stable run + StreamingAgentTailCell
→ App TranscriptSurface consolidation / resize reflow
→ final AgentMarkdownCell source + render cache
```

- **Protocol/UI reducer**：只验证 event/thread/item identity 并把 Assistant/Plan delta 交给当前 stream host；不得解析 Markdown、决定 wrap 或写 terminal rows。
- **MarkdownStreamHost**：是 Amadeus 对应 Codex ChatWidget 的 owner，拥有当前 Assistant/Plan controller、commit tick、active tail、status restore、completion reconciliation，以及 active stream 期间会改变 transcript 顺序的 deferred projection FIFO；它不是 Markdown parser，也不保存 final transcript source 副本。
- **StreamCore**：共享 Assistant/Plan 的 source accumulation、newline commit watermark、incremental render、stable/mutable partition、commit queue、tail 和 resize rebuild。两个 controller 只添加各自 presentation/header/final cell 类型。
- **Markdown writer**：消费 Goldmark AST/event，拥有 style stack、block lifecycle、indent stack、link/table state、syntax highlighting 与 typed line/hyperlink ranges；它不拥有 delta、retry、HistoryCell 或 terminal redraw。
- **TranscriptSurface**：是单一 Bubble Tea frontend 内model-owned的canonical transcript owner，拥有全部HistoryCell、active stream attachment range、active tail、final-cell replacement，以及session-header/history native-print watermark。不可变SessionHeader/User/Tool/final Assistant cells通过`tea.Println`按顺序只提交一次到native scrollback；transient `AgentMessageCell`/tail绝不打印，只在bounded active frame显示。
- **Final cell**：`AgentMarkdownCell`/`ProposedPlanCell` 是 completed item source 的唯一 owner；cache 只保存 derived layout。Rollout/Protocol 永远不保存 AST、wrap rows、style stack、tail 或 cache。

Amadeus不复制Ratatui类型或Rust动画实现，但必须保留上述owner、状态转换和completion protocol。仅在completion渲染且没有live controller/tail的旧路径不是目标；当前适配必须同时具备source-backed live frame、authoritative consolidation和finalized native-history sink。

#### 19.3.2 数据模型与 Owner

```go
type MarkdownSource struct {
    Text string
    CWD  string
}

type MarkdownRenderKey struct {
    Width               int
    RenderMode          HistoryRenderMode
    PaletteRevision     string
    SyntaxThemeRevision string
    ColorLevel          colorLevel
}

type MarkdownStyle struct {
    Bold          bool
    Italic        bool
    Strikethrough bool
    Underline     bool
    Foreground    *ColorToken
}

type MarkdownSpan struct {
    Text        string
    Style       MarkdownStyle
    Destination *LinkDestination
    Syntax      *SyntaxToken
}

type MarkdownLine struct {
    Spans            []MarkdownSpan
    InitialIndent    []MarkdownSpan
    SubsequentIndent []MarkdownSpan
    Hyperlinks       []HyperlinkRange
    BlockKind        MarkdownBlockKind
    NoWrap           bool
}

type HyperlinkRange struct {
    Columns     Range
    Destination LinkDestination
}

type MarkdownWriter struct {
    Styles       MarkdownStyles
    InlineStack  []MarkdownStyle
    Indents      []IndentContext
    Link         *LinkState
    Table        *TableState
    NeedsNewline bool
}

type MarkdownStreamCollector struct {
    source             strings.Builder
    committedSourceLen int
}

type StreamingRender struct {
    Lines             []MarkdownLine
    StableSourceLen   int
    StableRenderedLen int
    HasReferences     bool
}

type StreamState struct {
    CommitQueue      []MarkdownLine
    EmittedStableLen int
    HasSeenDelta     bool
}

type StreamCore struct {
    Collector MarkdownStreamCollector
    Render    StreamingRender
    State     StreamState
    Width     int
    CWD       string
    Mode      HistoryRenderMode
}

type StreamController struct {
    ItemID    protocol.ItemID
    Source    MarkdownStreamCollector
    Render    StreamingRender
    CWD       string
    Mode      HistoryRenderMode
}

type StreamAttachment struct {
    ItemID   protocol.ItemID
    Kind     protocol.ItemKind
    RunStart int
}

type TranscriptViewportState struct {
    Top          int
    FollowBottom bool
    Anchor       string
}

type TranscriptSurface struct {
    SessionHeader HistoryCell
    Cells        []HistoryCell
    ActiveStream *StreamAttachment
    ActiveTail   HistoryCell
    Viewport     TranscriptViewportState
    HeaderPrinted bool
    PrintCursor   int
}

type MarkdownStreamHost struct {
    Assistant           *StreamController
    Plan                *PlanStreamController
    TranscriptProtected bool
    Deferred            []DeferredTranscriptProjection
}

type AgentMarkdownCell struct {
    Source MarkdownSource
    Cache  MarkdownRenderCache
}
```

- 每个 attachment 同时最多有一个 Assistant 和一个 Plan active controller；controller 绑定当前 ItemID，所有 Delta、Reset 和 Completed 必须匹配该绑定。不得把 ItemID 发展为无约束的 controller map，也不得继续用与 ItemID 无关的全局 `draft string` 作为 transcript truth。Plan 使用共享 `StreamCore` 和独立 `PlanStreamController`/presentation wrapper，不复制 parser、source buffer、queue 或 tail state。
- `MarkdownStreamCollector` 只保存 raw source 和 newline commit watermark，不解析 Markdown、不保存 rendered terminal rows。`Reset=true` 表示 Provider retry 的新 attempt，必须清空当前 attempt source、render、stable queue 与 tail，不能提交旧 attempt HistoryCell。
- `StreamingRender` 只拥有当前 width/render mode 下的 derived lines 及增量缓存边界。Goldmark parse result 必须提供最后一个 top-level block 的 source start 和 reference-definition presence；已经完成的 top-level blocks 可以保留，最后一个 block必须允许随着后续 list tightness、setext heading、fence 或 reference link 变化而重渲染。不得用空行扫描、正则或字符串特征代替 parser block boundary。`StreamState` 是 stable line queue 的唯一 owner；queued、emitted 与 tail range 必须分离，不能复制 source。
- Goldmark AST 只负责 grammar；生产 Markdown writer 必须以 block/inline event 流持有 `MarkdownStyle` stack、list/blockquote indent stack、link state、hard/soft break 和 table state。Strong、emphasis、strikethrough、heading 与 link 是可组合 style patch，不得压缩为互相覆盖的单个 `semanticStyle` enum。
- `AgentMessageCell` 是从 `StreamState.CommitQueue` 取出的 stable rendered line run，只属于 live `TranscriptSurface`；`StreamingAgentTailCell` 是 enqueued boundary 之后的 mutable region。`TranscriptSurface.ActiveStream` 在 start 时冻结 `ItemID/Kind/RunStart`，completion/reset 按该 attachment range 精确替换或删除，不能通过扫描“当前尾部连续 cell”猜测范围。二者绝不是 Rollout item 或 Resume source。
- `SessionHeaderCell`是`TranscriptSurface`的固定首个HistoryCell，冻结当前attachment的Version/Model/CWD并结构化渲染Logo与信息框。它不是`View()`中“仅history为空时显示”的装饰分支；初始化/attach时与replay history按顺序提交native scrollback，首条UserMessage不会删除它。
- `TranscriptSurface`保留全部canonical cells，但`View()`只投影print cursor之后的transient stream/tool cells；immutable cell一旦进入native print queue就立即从active frame排除，不能同时显示两份。active frame按可用高度有界，用户通过终端原生scrollback查看finalized history。禁止先截取最后N条raw source再解析Markdown。
- `AgentMarkdownCell` 是唯一 final Assistant Markdown cell，保存 authoritative completed source 和当时 Session CWD；它按 render key 缓存 derived lines。当前 `AgentMessageCell{Markdown string}` 直接删除，不保留 alias、wrapper 或“stream cell 与 final cell 共用一个模糊类型”的路径。
- `/copy` 从 completed `AgentMarkdownCell.Source.Text` 派生，不保留 `LastAgentMarkdown` 或其他 source 副本，也不读取 active stream、ANSI输出或trim后的文本。

#### 19.3.3 Streaming、Completion 与 Bubble Tea 适配

- 与Codex一致，未结束的source line不进入committed render。半个inline code、link、list marker、fence或table row不能先以错误形状显示再跳变；没有newline的最终尾行只在completion/finalize时提交。
- Rich streaming 解析 `stable_source_len` 之后的 pending source，并以 Goldmark 顶层节点 source offset 缓存 stable prefix 和 mutable final block。fenced code、loose list、blockquote、HTML block 内部的空行不是稳定边界。Goldmark `parser.Context.References()` 等 source-wide parse state 出现时允许 full recompute；普通 Delta 不得每个 frame 重新扫描或解析全部 source。任何会改变 parser input 字节位置的 source transform 必须提供 offset map，否则该次 render 禁止推进 stable boundary。
- 每个 block 的 writer 生命周期负责视觉呼吸和结构：paragraph、heading、blockquote、list、code 和 table 之间按 Codex `needs_newline`/block boundary 产生结构化空行；list marker、initial indent 与 subsequent indent 必须保留到 wrap 之后，不能把所有 block 紧贴成连续文本。
- stable region 进入 `StreamState.CommitQueue`，由 Bubble Tea tick 按确定顺序转为 transient `AgentMessageCell` 并写入 `TranscriptSurface`；mutable tail 只存在 active slot。table header/delimiter 出现后，`TableHoldbackState{PendingHeader|Confirmed}` 必须把候选 table 起点之后保持 mutable，避免新增 row 改写已经 committed 的列宽。queued 与 emitted boundary 必须分离，queued row 不得同时出现在 tail。
- `AgentMessageCell`和`StreamingAgentTailCell`的`IsStreamContinuation`必须返回`!First`：stream首个cell与前一User/Tool cell之间从第一帧就产生正常cell spacing，后续stable/tail run不重复插空行。completion替换成final `AgentMarkdownCell`前后spacing必须完全相同，不能在finalize时闪现空行。
- `ItemCompletedEvent` 中的 Assistant Text 是权威完成事实。即使 stream source 非空，只要与 completed Text 不同，final cell 必须使用 completed Text；stream 只负责 live preview，不能覆盖 canonical completion。无 Delta 但有 completed Text 时直接建立 final cell。`ItemPlan` Text 也采用相同覆盖规则；这是 Amadeus 的 canonical protocol 决定，不宣称为 Codex 当前 Plan 路径的既有行为。
- Active Assistant/Plan stream 期间，所有会插入、完成或删除 HistoryCell 的 Warning、Tool、Approval、UserInput、diagnostic 和 attachment-boundary projection 必须由 `MarkdownStreamHost` defer-or-apply。deferred projection 保持原 Event 顺序；先 finalize/reset 精确 stream attachment，再 FIFO flush。不能允许非-stream cell 插入 stream run 后再依赖 trailing-run scan consolidation。
- Retry reset、interrupt 和 terminal error 必须按冻结的 attachment range 释放 controller、stable run 与 tail。已完成 cell 只能由 completed item创建一次；live 与 Resume 使用同一个 `completed item → AgentMarkdownCell`（或 Plan final cell）projector。
- Bubble Tea保持单一frontend。stock standard renderer会丢弃超高`View()`顶部rows，因此完整history由`flushHistory`通过有序`tea.Println`提交native scrollback，mutable stream/tail使用bounded active viewport。Mouse capture保持关闭，终端原生滚轮/选择继续工作。
- completion先清除active tail、finalize controller并比较streamed source与authoritative Text；有stream时按`ActiveStream.RunStart`原子替换为单个final cell。transient stable/tail从未进入native history，因此无需撤回；replacement完成后final cell一次性print，不能把provisional rows和final rows重复打印。
- terminal resize 或 Rich/Raw mode change 以 complete source重建controller queue/tail和final cell derived layout，再 clamp viewport offset。Terminal resize由TUI内部的`transcriptReflowState`观察`WindowSizeMsg`并以75ms trailing debounce合并；到期后先通过`tea.ClearScreen`清理活动画面，再以标准terminal `CSI 3 J`清理scrollback，最后从immutable `HistoryCell` prefix按新宽度生成一份`tea.Println` payload并重置native print watermark。旧generation的resize消息会被丢弃。用户处于 follow-bottom 时 resize 后仍跟随底部，用户正在查看旧历史时尽量保持同一逻辑 cell/line anchor。

#### 19.3.4 Markdown Render Contract

Markdown parser和terminal projection必须保持分层：

```text
exact MarkdownSource
→ Goldmark AST + source offsets
→ block/inline semantic nodes
→ width-aware []MarkdownLine/[]MarkdownSpan
→ TerminalPalette + Chroma styles
→ Bubble Tea output
```

- Renderer输出typed lines/spans，不把整块Glamour ANSI string塞入`styleRendered`后失去结构。`MarkdownStyle` 是 composable patch，`MarkdownSpan` 保留可选 syntax token 与 typed link destination；`MarkdownLine` 同时保存 initial/subsequent indent、block kind 与 wrap policy，使 wrapping、prefix、code no-wrap 和 Raw projection不从ANSI文本反推。
- Assistant source不得`strings.TrimSpace`。`MarkdownSource.Text` 保留 authoritative completed Text 的原始字节序列；若 parser 需要末尾换行，只能为本次 render 构造临时 `parseSource`，不得把该虚拟换行写回 source、cache、`/copy` 或 Resume。leading spaces、trailing newline、indented code 和 fence 边界属于 Markdown 语义。UI 可在 derived render 阶段规范化纯空白 display line，但不能改写 source。
- Rich和Raw是两种独立projection：Rich在`NO_COLOR`下仍解析Markdown并移除语法marker，只是不发颜色/修饰ANSI；Raw保留原始Markdown。不得把“NoColor”退化为显示`**bold**`等raw marker。
- Assistant/Plan首行使用dim `• `，所有后续视觉行使用等宽`  ` gutter；soft wrap也必须保留subsequent indent，不能只给渲染字符串第一行加prefix。
- Goldmark soft break 表示 source 中明确存在的逻辑换行，在普通 paragraph/list/blockquote 中投影为新的 structured line；hard break也换行但保留其明确语义。table cell 可按 table layout policy把 soft break规范为空格。两者都必须复用当前 subsequent indent，不能统一替换成普通空格。
- wrapping 必须先基于整条 logical line 计算 display-width aware word/grapheme ranges，再把输出 range remap 回原 `MarkdownSpan`/style/link destination；不能逐 span 分词或按 rune 重拼。默认 `break_words=false`，普通单词不得被拆为两行；只有明确的 token-heavy URL/path/hash fallback 才可在可解释边界拆分，且每个 fragment 达到宽度后必须实际 flush output line。fenced/indented code block设置`NoWrap`，Bubble Tea只投影当前viewport可见列，完整code仍保留在`MarkdownSource`并可通过`/copy`取得；基础版不增加水平滚动frontend。
- Heading、emphasis、strong、strikethrough、inline code、list、blockquote和horizontal rule语义与Codex对应；具体颜色通过TerminalPalette semantic token选择，不在Markdown AST writer硬编码truecolor值。
- Fenced code继续使用Chroma，不复制Codex Syntect/Two Face实现。语言alias、unknown-language plain fallback、输入大小/行数/单行长度上限和light/dark/ANSI/no-color行为必须有明确contract；不要求与Codex支持完全相同的语法集合。
- GFM table由 Goldmark table events 驱动的 typed table group/cell rows。基础 renderer 先计算共享 intrinsic widths；需要收缩时必须真实 wrap 每个 cell 并生成等高 physical rows，不能只减小 width 数字后继续输出完整 cell。任何列低于最小可读宽度、列数/行数/总cell bytes超限或 grid仍无法放入viewport时，整个body确定性降级为key/value records。Codex 的 spillover filtering 与 Narrative/TokenHeavy/Compact 启发式不属于基础范围。完整 `md`/`markdown` fence table若做source transform，必须保守识别并遵守前述offset-map/full-recompute规则。
- 本地和Web link的typed span必须保留destination。local destination覆盖`file://`、Unix absolute/relative、`~/`、Windows drive/UNC，并规范化`:line[:column]`/`#Lline[Ccolumn]`后按cell冻结CWD缩短；web link默认显示label，并在label不等于destination时提供可读` (destination)` fallback，同时保留完整OSC-8 target。wrap/table/clone后visible range必须继续指向完整destination，不能漏拷贝table prefix或只转换table外层row。
- Terminal projection在不改写`MarkdownSource`的前提下移除span text中的CSI/OSC和非换行/Tab控制字符；只有经过scheme、host和control-byte校验的`http/https`destination可以生成OSC-8。
- Inline visualization、Codex theme picker/custom `.tmTheme`、远程图片Markdown和raw reasoning body不属于AB基础范围。Reasoning delta只用于Codex风格status header；只有未来产品明确展示reasoning summary时才复用Markdown renderer建立独立cell。

#### 19.3.5 Cache、失败与可观测性

- `MarkdownRenderCache`按width、Rich/Raw mode和当前固定 palette/color level失效；本阶段 Chroma theme 由 `terminalPalette.Dark` 静态选择，不存在独立 runtime syntax revision。CWD属于immutable source/cell identity，不从当前process cwd动态读取。cache只保存derived render，不成为第二份source。
- Markdown parse/render失败不得使Turn失败。Goldmark/Chroma的可恢复失败直接降级plain token；unexpected parser/render panic在统一recover boundary生成bounded plain-text projection，下一次live delta从exact pending source重新尝试structured render。fallback始终保留raw source供`/copy`、Resume和后续修复，且不得把正文写入普通diagnostic日志。
- Streaming render必须有CPU/内存边界：TUI在进入Goldmark前独立限制structured-render source bytes，syntax highlighting单独限制bytes/lines/line length，table layout限制rows/columns/cell width；超限只把相关presentation降级为bounded plain projection，`MarkdownSource`、Rollout、Resume和`/copy`不丢Assistant文本。
- Debug/trace可以记录source bytes、committed watermark、stable/mutable line counts、full recompute reason、render duration和cache hit；不得记录完整敏感Assistant正文到普通日志。
- Markdown source、stream controller和render cache只属于TUI。Protocol继续只发布typedDelta/completed item，Session、ModelClient、Rollout和Application不得了解Goldmark node、terminal width、Chroma theme或HistoryCell。

### 19.4 Visual Runtime

与 Codex 对齐的关键行为：

- `◦ Working (1m 32s • esc to interrupt)` 使用单调时钟。
- Working shimmer 使用终端主题感知的 foreground/dim，而不是固定彩虹色。
- Tool 工作与最终 Assistant 回复之间显示不带耗时的 dim rule；完成后在最终回复下方显示 `─ Worked for 7m 18s ─────`。
- User、Working、Assistant、Tool、Separator和Composer的空行由previous/current boundary共同决定。普通主要区域使用两条blank rows；任一侧是`FinalMessageSeparator`或Tool activity tree时使用一条；stream continuation为0。`historyBoundaryBlankRows(previous,current)`同时驱动内存layout、native print和active leading boundary，避免同一个Explored/Ran组合因是否落在同一`ToolHistoryCell`而出现1/2行随机变化。
- Composer 按终端显示宽度软换行；`› ` 只属于第一条视觉行，后续软换行与显式换行使用等宽空白 gutter。五行上限是可见 viewport 高度而不是输入长度限制；超过上限后，展示投影截取包含当前 cursor 的五条视觉行，Home/End/方向移动必须同步滚动可见窗口。输入使用 Bubble Tea textarea 的软件光标；当前 Bubble Tea renderer 不暴露 model hardware-cursor position，基础版不通过 output writer 或手写 cursor reposition 强行实现 IME 候选窗口锚定。
- Footer 左侧通常显示 Model、CurrentDir、GitBranch、ThreadTitle 与 Context 等固定会话元数据；running Turn 中存在 queueable draft 时临时替换为 queue hint。Plan collaboration indicator 使用 magenta 独立右对齐，空闲时附带 `shift+tab to cycle`，Default mode 不显示模式标签。
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

### 19.5 Statusline 与 Footer State

Amadeus 不复制 Codex 的 `/statusline` 命令、picker、持久化配置或任意 item 排序能力；基础版只展示产品指定的固定信息。但内部架构、数据模型、概念术语、命名与生命周期按 Codex 的 statusline/footer 分层对齐：

```text
Session-owned canonical state
→ typed StatusLineItem projection
→ cached FooterState
→ pure FooterProps layout/render
```

固定 item 集合及默认顺序为：

```go
type statusLineItem uint8

const (
    statusLineItemModelWithReasoning statusLineItem = iota
    statusLineItemCurrentDir
    statusLineItemGitBranch
    statusLineItemThreadTitle
    statusLineItemContextUsed
    statusLineItemContextWindowSize
)
```

`CurrentDir` 表示 Session 当前工作目录，是 Codex `CurrentDir` 的同义概念；不得再使用含义模糊的 `Project` 作为 statusline 字段名。未来若引入 `ProjectRoot`，它表示指令发现或 workspace policy 的根边界，不与当前 CWD 混为同一数据。`GitBranch` 是以 CurrentDir 为 key 的派生 workspace metadata，不进入 `SessionConfiguration` 成为第二 owner。

目标数据模型保持 typed projection 与渲染缓存分离：

```go
type statusLineSegment struct {
    Item statusLineItem
    Text string
}

type statusLineState struct {
    Segments           []statusLineSegment
    ContextUsedPercent int64
}

type footerState struct {
    StatusLine             statusLineState
    CollaborationIndicator collaborationModeIndicator
}

type footerProps struct {
    Width             int
    Running           bool
    HasQueueableDraft bool
    State             footerState
    Palette           terminalPalette
    LeftPadding       int
    RightPadding      int
}
```

- `statusLineValueForItem()` 只从 TUI 已持有的 `sessionViewState` 和派生 cache 读取值；item 当前不可用时返回 unavailable 并临时省略，不显示 `unknown`、`-` 或 Application status fallback。
- `refreshStatusLine()` 在 canonical session state、title、usage/context、CurrentDir 对应 branch cache 或 terminal size 变化时重建 `statusLineState`。Resize 先刷新语义 statusline projection，再由当前 width 构造 `footerProps` 完成布局；该刷新不执行 Git/文件系统 IO。
- `statusLineSegment` 不提前持有 Lip Gloss style。`statusLineAccentForItem()` 在 Footer render 边界集中映射 TerminalPalette accent，保证颜色策略与数据模型解耦，并在 `NO_COLOR` 下自然降级。
- Statusline 颜色解析采用 Codex 的 theme-first/fallback 分层：TrueColor 与 ANSI256 根据终端明暗背景选择 Catppuccin Mocha/Latte Chroma style，以 type、string、function、number、keyword、heading token 对应 Codex 的 Model、Path、Branch、Usage、Mode、Thread scope family，之后执行同样的 85% saturation softening；ANSI16 保留 cyan/green/magenta fallback。该基础版不引入 `/theme` 或自定义 tmTheme owner。
- `footerState` 分别缓存左侧 statusline 与右侧 collaboration mode indicator；Working/status indicator 不属于 Footer metadata，也不得作为 statusline 缺失值的替代文本。
- `HasQueueableDraft` 不写入 `footerState`，而是在每次构造 `footerProps` 时从 TUI 已持有的 `running + composer text + ParseInput` 纯派生；它不创建第二份 Composer 或 queue truth。
- `renderFooter(footerProps)` 是纯布局/渲染函数，不查询 Application、不访问文件系统、不启动 branch lookup、不修改 model state。`View()` 只组合已有 view state，不承担 SessionConfiguration 投影。
- Footer 使用 Codex 风格的 statusline 左列与 indicator 右列：Amadeus 固定六个 item 由同一 typed projection提供并按 ModelWithReasoning、CurrentDir、GitBranch、ThreadTitle、ContextUsed、ContextWindowSize 顺序组成左侧 statusline；Plan indicator与queue hint在右侧布局按可用宽度收缩。完整 statusline 先生成 styled line，再以左列可用宽度从右侧截断并追加`…`，不按 item 删除或将 Context 固定移到右列。完整 `Plan mode (shift+tab to cycle)` 无法与左侧内容共存时收缩为 `Plan mode`，并继续保证 indicator 右对齐。Default mode 不渲染模式标签。
- `HasQueueableDraft=true` 时 Footer 进入 transient queue-hint layout：左侧优先显示 dim `tab to queue message`，宽度不足时收缩为 `tab to queue`；固定 statusline 暂停渲染。Plan indicator 只有与 hint 同行可容纳时才保留，空间不足时先删除 Plan indicator，queue hint 是该状态的最后保留信息。Composer 清空或 Turn terminal 后恢复普通 statusline layout。
- Slash/File/Skill 等 Composer popup 激活时占用 Codex 的 popup/footer 区域并替换普通 Footer；不得在 popup 下方继续渲染 statusline、queue hint 或 mode indicator。Popup 关闭后 Footer 才恢复。Slash Command Popup 的 selection 只通过 command name/description style 表达，不显示 Modal picker 使用的 `›` cursor glyph。
- Selection overlay 对齐 Codex `SelectionViewParams`：footer hint 默认为空，不由公共 renderer 合成按键说明；确有必要时由调用方显式提供。非空 subtitle 与列表/搜索输入之间统一保留一行，不允许按命令增加视觉特例开关。`/skills` 顶层菜单与 `/resume` picker 不显示 footer hint。
- Collaboration indicator 的“右对齐”只表示 Footer 当前布局行内的独立右列，不要求复制 Ratatui 类型或 cursor protocol。`View()`通过同一个Bubble Tea model渲染mutable TranscriptSurface、Composer、Popup与Footer；native reflow使用Bubble Tea自身的`tea.ClearScreen`、`tea.Println`和标准terminal `CSI 3 J` scrollback erase，不引入额外output writer或手写cursor up/down reposition协议。只有immutable finalized transcript cells经Bubble Tea自身`tea.Println`提交。

Session 配置部分使用单一应用路径：

```text
Session.Configuration
├─ SessionConfiguredEvent.Configuration
├─ ThreadSettingsAppliedEvent.Configuration
└─ ThreadViewSnapshot.Configuration
          ↓
sessionViewState
          ↓
refreshStatusLine()
          ↓
statusLineState{Segments}
          ↓
renderFooter(footerProps)
```

Thread title、TokenUsageInfo/ActiveContextTokens 和 Git branch 分别通过 typed Application event、`TokenCountEvent` 与 CurrentDir-keyed derived cache 合入同一个 `sessionViewState`，不塞入 `SessionConfiguration` 扩大其职责。TokenCountEvent 和 ThreadViewSnapshot 都携带完整 snapshot，Reducer 只替换、不累加。`ThreadSettingsAppliedEvent` 携带实际生效的完整 `SessionConfiguration`，并与 `SessionConfiguredEvent`、snapshot attach 共用 `applySessionConfiguration()`。该函数原子替换 CurrentDir、Provider、Model、ReasoningEffort 和 CollaborationMode；不能只更新 Mode 后继续从 Startup 或 Application Status 读取其他字段。Resume、new thread 和 attach 必须先清理旧 Thread 的 session/footer 派生状态，再安装新 snapshot，避免旧目录、branch、title 或 context 泄漏。

`ThreadSettingsAppliedEvent` 同时驱动两条相互独立的 UI 路径。第一条通过 `sessionViewState → footerState.CollaborationIndicator → renderFooter()` 更新 Footer 右列模式标签。第二条对齐 Codex 的 settings acknowledgement/info-history 生命周期，但消息必须描述 Amadeus 实际发生的业务事实：基础版 Mode 切换不改变 Model 或 ReasoningEffort，因此插入 `• Mode changed to <Mode>.`，而不是伪造 `Model changed`。该消息不得使用普通 dim notice，也不得把 Composer、Popup 或 Footer 内容拼进 history；它与其他 HistoryCell 都由 `TranscriptSurface` 投影，随后 Bubble Tea 渲染单份活动 frame。

生命周期固定为：

| 触发 | 状态变化 | Footer 动作 |
|---|---|---|
| constructor / initial snapshot | 初始化 active `sessionViewState` | projection 一次 |
| `SessionConfiguredEvent` | 应用完整 Configuration | refresh |
| `ThreadSettingsAppliedEvent` | 应用完整已生效 Configuration，Mode 变化时插入 settings acknowledgement info row | refresh projection + right-aligned mode |
| `ThreadAttached` | 替换 Thread、Configuration、title、usage/context | 清空旧 cache 后 refresh |
| `TokenCountEvent` | 替换 TokenUsageInfo snapshot，并从 LastTokenUsage/active projection 更新 context | refresh context items |
| `ThreadNameUpdated` | 更新 active Thread title | refresh title item |
| branch lookup completion | 更新 CurrentDir 对应 branch cache | 校验 generation/CWD 后 refresh |
| Turn start/end 或 retry | 只更新 Working/status indicator 与 cycle hint | 不改变 statusline items |
| running Turn 中 queueable Composer draft 出现/变化 | 只更新派生 `HasQueueableDraft` | queue hint 替换 passive statusline；完整/短文案按宽度选择 |
| Tab enqueue 后 Composer 清空 | NextTurnQueue 增加 Pending，`HasQueueableDraft=false` | queue hint 消失，queued preview 保留，普通 statusline 恢复 |
| terminal resize | 更新 width/height、refreshStatusLine、安排source-backed native history reflow | status surface、layout与scrollback reflow |
| `View()` | 无业务状态变化 | 纯 render |

Git branch 查询必须在 CurrentDir 改变时清空旧值并异步刷新；请求携带 attachment generation 与 CWD，迟到结果只有在两者仍匹配时才能写入 cache。Statusline 不通过 `Application.Status()` 轮询补全目录、模型、标题或上下文，也不在 `View()` 中同步执行 Git/文件系统 IO。

必须严格区分四个 UI 概念：

- **Working/status indicator**：表示当前 Turn 的 Working、retry 或其他短期活动状态，生命周期来自 Event。
- **Statusline**：表示固定的 Session/Thread metadata 投影，不展示瞬时运行状态。
- **Collaboration mode indicator**：表示 Plan mode，并在 Footer 右侧独立布局；它读取 `sessionViewState.Configuration.Mode`，但不是 `StatusLineItem`。
- **Queue hint**：表示当前 Composer draft 可用 Tab 排入下一 Turn，是纯 TUI transient guidance；它临时取代 passive statusline，但不表示已经 enqueue，也不进入 footerState/canonical state。

### 19.6 Interactive Request 与 Diff

Approval Dialog 是 Rich TUI 的专用交互状态，不复用只显示文字的通用 selection overlay：

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

### 19.7 Tool Projection 与展示 Contract

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
- Session spawn 使用 InitialHistory 恢复 SessionMeta 中的 exact BaseInstructions/provenance、canonical conversation/Replacement History、WorldState full/patch baseline 和 TurnContext reference；不恢复 `update_plan` checklist 或 `PlanUpdateEvent`。
- SessionPermissionContext 在 Resume 时重置为空 Read/Edit Directories 和空 Command/External Grants；下一 Step 的 `PermissionsState` 相对恢复后的 baseline 生成必要 developer diff。CollaborationMode 从 SessionConfiguration/TurnContext 恢复并通过同一 WorldState lifecycle 生效，不建立临时 replace-key Context Update。
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

## 21. Basic Multi-Agent

### 21.1 目标与取舍

Amadeus Basic Multi-Agent以当前`../codex-main`的Multi-Agent V1为主要架构参考：`agent/control`与`thread_manager`决定root-tree ownership和spawn/close边界，`agent/status`与`TurnCompleteEvent.last_agent_message`决定status/final answer，V1 handlers决定spawn/send/wait/close Tool lifecycle，completion watcher与SubagentNotification决定parent交付。`../claude-code-main`只用于吸收background child独立取消域、Tool allowlist、权限不升级和failed/killed partial result等局部经验，不复制其Query/AppState/Task外层。这里的“对齐”要求对齐Thread/Session所有权、数据模型、概念术语、命名、状态生命周期、Prompt owner、Event/Rollout顺序和TUI projection；Codex V2与Claude Code进阶multi-agent体系是永久产品非目标，不以“以后再实现”的方式保留入口。

目标是形成最小但真实可用的Basic闭环：

```text
Root AmadeusThread
→ spawn_agent
→ Root-scoped AgentControl
→ Child AmadeusThread / Session
→ Child regular Turn
→ AgentStatus watch
→ <subagent_notification>
→ wait_agent / send_input / close_agent
```

固定取舍：

- SubAgent 是完整 `AmadeusThread`，拥有独立 Session、ContextManager、ActiveTurn、RunningTask、SessionServices、ToolExecutionService 和 canonical Rollout；不得实现为父 Session 内的嵌套 `SessionTask`、`run_turn` callback 或一次裸 Provider 请求。
- 同一 Root Thread 创建的全部 SubAgent 共享一个 root-tree scoped `AgentControl`；`AgentControl` 不是进程级 singleton，也不归 TUI、Application 或 ToolDefinition 所有。
- Basic Multi-Agent采用Codex V1风格显式控制工具和open-agent slot；只持久化depth-one root→child open/closed edge，不实现且不预留V2 `AgentPath` mailbox、`send_message`/`followup_task`、LRU residency或通用持久化agent graph。
- child角色固定为`explorer`，只承担代码搜索、文件阅读、架构分析、证据收集和只读验证；文件编辑、命令执行、MCP Call、`request_user_input`和继续创建SubAgent均不可见。
- Root与child使用同一CWD、workspace roots和宿主文件系统；其他Agent或用户产生的文件变化对child立即可见。Basic实现不创建worktree、不做文件锁和冲突合并。
- fresh spawn不继承父conversation history；父Agent必须通过明确task message传递必要背景。Full/recent history fork、model/reasoning override、自定义Agent Definition和write-capable worker均为产品非目标，不进入后续任务。
- SubAgent 的 canonical thread history 与 root→child spawn edge 可以持久化；Root Resume 只恢复仍为 open 的 child metadata 作为 unloaded AgentRecord，不自动重启旧 Turn，也不提供公开 `resume_agent`，child 仍不出现在默认 `/resume` 列表。

### 21.2 SessionSource、Agent Identity 与 Metadata

Thread 是否属于 SubAgent 必须是 Runtime 和 Persistence 中的显式事实，不通过 title、路径、Prompt 文本或 TUI 状态推断：

```go
type SessionSourceKind string

const (
    SessionSourceRoot     SessionSourceKind = "root"
    SessionSourceSubAgent SessionSourceKind = "subagent"
)

type SessionSource struct {
    Kind     SessionSourceKind
    SubAgent *SubAgentSource
}

type SubAgentSource struct {
    ParentThreadID ThreadID
    Depth          int
    AgentNickname  string
    AgentRole      string
}
```

Go 实现使用 tagged struct 表达 Codex 的 enum/sum type 语义，使同一模型可以确定性进入 JSONL 与 SQLite；`Kind=root` 时 `SubAgent=nil`，`Kind=subagent` 时 metadata 必须完整，不保留 interface 实现或旧格式 reader。

Basic实现的`AgentID`直接使用child `ThreadID`，不增加与Thread平行的第二套ID。`AgentMetadata`是`AgentControl`的read model：

```go
type AgentMetadata struct {
    ThreadID       ThreadID
    ParentThreadID ThreadID
    Depth          int
    AgentNickname  string
    AgentRole      string
}
```

约束：

- Root Session 使用 `RootSessionSource`；child 使用 `SubAgentSessionSource`。
- `ParentThreadID`、Depth、Nickname 和 Role 在 spawn 成功后不可变，并写入 child `SessionMetaItem`。
- 最大Depth固定为1；child的ToolRouter不暴露任何Multi-Agent Tool。
- Nickname 由 `AgentControl` 从内置名称池分配，必须在当前 root tree 内唯一；spawn 失败时预留 nickname 和 slot 必须一起回滚。
- Role固定为`explorer`；稳定字段用于canonical明确当前角色和Resume校验，不表示未来worker/reviewer扩展点。

### 21.3 AgentControl 所有权

`AgentControl` 是整棵 root agent tree 的控制面，对应 Codex `AgentControl` 的最小 Go 等价实现：

```go
type AgentControl struct {
    host       AgentHost
    sessionID  SessionID
    rootID     ThreadID
    agents     map[ThreadID]*AgentRecord
    maxAgents  int
    maxDepth   int
}

type AgentRecord struct {
    Metadata AgentMetadata
    Status   AgentStatus
    LastTurn *AgentTurnResult
    Runtime  AgentRuntime // persisted/unloaded child 可以为空
    Changed  <-chan struct{}
}
```

`AgentHost` 是由 ThreadManager 实现的窄 runtime port，只允许 AgentControl创建/恢复/查询/提交/停止child Thread、向parent Session durable交付notification，并请求root Session记录typed spawn-edge mutation；它不得暴露ThreadManager完整内部map、ThreadStore、ContextManager或Application callback。AgentControl不能直接写Rollout，ThreadManager也不能绕过Session append owner修改root history。

所有权固定为：

```text
ThreadManager starts Root Thread
→ creates AgentControl(root Thread)
→ passes same AgentControl through Root SessionServices
→ AgentControl asks AgentHost to spawn Child Thread
→ Child SessionServices receives the same AgentControl
→ Child ToolRouter hides collaboration tools because Depth = 1
```

`ThreadManager` 仍是 live Thread 实例与 ThreadStore writer 的唯一 owner；`AgentControl` 持有从 Root ThreadID 派生的 SessionID，并将同一个 SessionID 传给全部 child Session。它只拥有 session-scoped agent-tree metadata、状态订阅、spawn reservation 和控制操作，不成为第二个 Thread registry。Root 和 child 的运行态查找、AgentID、父子关系与消息路由始终使用各自 ThreadID，不能使用共享 SessionID 作为 map key。

Root Resume 必须恢复该 root tree 的 persisted child metadata，而不是创建一个空 AgentControl 后遗忘历史 child：

```text
Resume Root ThreadID
→ 从 Root SessionMeta 恢复并校验 SessionID
→ 创建 AgentControl(SessionID, RootThreadID)
→ 从 Root Rollout/SQLite projection选择 AgentSpawnEdgeState=open 的 descendants
→ 读取每个 child Rollout 的 canonical SessionMeta
→ 校验 child ID、ParentThreadID 与 SessionID
→ 注册 persisted/unloaded AgentRecord
→ send_input 等操作按 child ThreadID 触发 ThreadManager internal child resume
```

AgentControl 可以保存尚未加载 Runtime 的 persisted AgentRecord，但不能自己打开 Rollout 或构造 Session；实际 child resume 仍由 ThreadManager/AgentHost 完成。内部恢复必须先从 root canonical spawn-edge projection 选择仍为 open 的 child，再使用 child ThreadID 定位 StoredThread/Rollout，并要求 child SessionMeta.SessionID 与 Control.SessionID 相同。公开 `/resume`、`--resume` 和 picker 不直接选择 child Thread；不存在公开 `resume_agent` Tool。

禁止：

- 在 CLI、exec、TUI appModel 或 Application 中维护第二份 `map[AgentID]AgentStatus` 作为事实源。
- 让 ToolDefinition 直接构造 Session、ModelClient、ThreadStore writer 或 goroutine。
- 让 child Session 回调父 `regularTask` 或共享父 ActiveTurn/TurnState。
- 为 SubAgent 新建与 `run_turn` 平行的 simplified agent loop。

### 21.4 AgentStatus 与生命周期

Basic实现使用Codex术语和状态语义：

```go
type AgentStatusKind string

const (
    AgentStatusPendingInit AgentStatusKind = "pending_init"
    AgentStatusRunning     AgentStatusKind = "running"
    AgentStatusInterrupted AgentStatusKind = "interrupted"
    AgentStatusCompleted   AgentStatusKind = "completed"
    AgentStatusErrored     AgentStatusKind = "errored"
    AgentStatusShutdown    AgentStatusKind = "shutdown"
    AgentStatusNotFound    AgentStatusKind = "not_found"
)

type AgentStatus struct {
    Kind    AgentStatusKind
    Message string
}

type AgentTurnResult struct {
    TurnID           TurnID
    Outcome          TurnOutcome
    Reason           string
    LastAgentMessage *string
}
```

`Message` 只在 Completed/Errored 时承载 bounded final agent message 或错误文本；其他 Kind 必须为空。这样保留 Codex enum-with-payload 语义，同时使用稳定、易编码的 Go typed struct。`AgentTurnResult` 是同一 terminal Event 的无损派生 read model，用于保留 Amadeus 已有 `completed|blocked|failed|aborted` outcome 与 reason；它不是第二个完成协议，也不得脱离 canonical `TurnCompleteEvent`/`TurnAbortedEvent` 单独写入。

Codex 的 AgentStatus 表达 child control-plane 的当前可用状态，Amadeus 的 TurnOutcome 表达最近 Turn 是否真正完成目标，两者不得压扁成一个字符串：

- `AgentStatusCompleted` 表示 child 当前 Turn 已结束且 Thread 空闲，可以继续 `send_input`；它不自动断言 `AgentTurnResult.Outcome == completed`。
- `AgentTurnResult{Outcome: blocked}` 保留预算或外部依赖造成的未完成事实、具体 reason 和可选 partial final report；不得新增与 Codex 状态枚举平行的 `AgentStatusBlocked`。
- WorldState `<subagents>` 只展示 AgentStatus；`wait_agent`、completion notification 和 TUI collaboration detail 同时携带 optional LastTurn，供父 Agent 区分 completed 与 blocked。

状态转换固定为：

```text
PendingInit → Running
Running → Completed | Errored | Interrupted
Completed | Errored | Interrupted → Running   // send_input starts a new Turn
PendingInit | Running | Completed | Errored | Interrupted → Shutdown
unknown/closed ID → NotFound
```

`Interrupted` 不是终态 Agent；它只表示当前 child Turn 被中断。`Completed` 和 `Errored` 表示当前 Turn 已结束，但 child Thread 仍保持 open、继续占用 agent slot，并可通过 `send_input` 启动后续 Turn。只有 `close_agent` 或 root tree shutdown 才释放 slot。

AgentStatus 与 LastTurn 由 child 的 typed Session Event 通过同一个纯 reducer 派生：

- `TurnStartedEvent` → Running。
- completed `TurnCompleteEvent` → Completed；`Message` 只复制该 Event 的 optional `last_agent_message`，LastTurn 复制 outcome/reason/last_agent_message。
- failed `TurnCompleteEvent` 或 terminal `ErrorEvent` → Errored；错误文本来自 terminal Event，不从 TUI、ToolResult 或日志重建。
- `TurnAbortedEvent` → Interrupted，并将 aborted outcome/reason 保存在 LastTurn；Interrupted 不属于 completion watcher 的 final status。
- `ShutdownCompleteEvent` → Shutdown。

`TurnStartedEvent` 必须原子切换到Running并清空上一Turn的LastTurn，避免新Turn运行期间wait/WorldState继续暴露旧completion。ErrorEvent可以更新live Errored诊断，但parent completion notification必须等待该Turn的canonical terminal Event或明确的Session终止，不能在随后还会改变reason/final message的中间错误上抢先提交。

`run_turn → TaskOutput.LastAgentMessage → TurnCompleteEvent.last_agent_message` 是 final answer 的唯一链路。Tool Call 前导语、任意 completed AssistantMessage、Reasoning、stream delta 和“最近一条有文本的消息”都不能作为 fallback。AgentStatus 是 `AgentControl` 内部 watch/read model，不增加公开 `SessionIo.Status` channel，也不替代 child 自身 Event/Rollout 终态。Live event reduction 与 Root Resume rollout reconstruction必须调用同一个 reducer，禁止分别维护 `latestAssistant` 与 `persistedAgentStatus` 两套算法。

### 21.5 Spawn Reservation、并发与取消树

`spawn_agent` 必须先完成 reservation，再创建 Thread：

```text
Validate parent/depth/input
→ reserve open-agent slot + nickname
→ snapshot parent live configuration
→ ThreadManager creates child LiveThread + Session
→ register child event reducer
→ submit initial UserInputOp
→ durable append root AgentSpawnEdgeItem(open)
→ commit reservation
→ return child ThreadID + nickname
```

任何一步失败都必须释放 slot、nickname、child writer 和已创建 runtime。不得先增加计数后依赖后续定时清理。

Basic限制：

- `max_depth = 1`。
- `max_agents = 4`，只统计当前 root tree 中未 close 的 spawned child，不包含 root。
- Completed、Errored 和 Interrupted child 继续占用 slot，直到 `close_agent`。
- 单个 child 继续使用既有 TurnBudget，但额外覆盖为有界 child budget：最多 20 次 sample、100 次 Tool Call 和 15 分钟 Turn duration；这些值是包含 finalization reserve 的总预算，不允许通过额外无界 request 绕过。
- child 在任一维度进入 soft boundary 后停止新探索，冻结当前 PromptSnapshot并执行至多一次 Tools 为空的 finalization sample；成功时以权威 `last_agent_message` 交付已验证事实和限制，只有跨过 hard boundary或 finalization 失败时才返回 blocked LastTurn。
- `spawn_agent` 本身声明为 parallel-safe，使同一次模型响应中的多个独立 spawn 可以由现有 Tool batch 有界并行；AgentControl reservation 仍负责跨 batch 的树级总量限制。

取消关系：

```text
Application / ThreadWorkspace lifetime
└── Root AmadeusThread / Session
    └── AgentControl tree lifetime
        └── Child AmadeusThread / Session
            └── Child Turn Context
```

创建完成后的 child Session 绑定 root tree/ThreadWorkspace lifetime，而不是 spawn Tool、父 Model Step 或父 ActiveTurn context；父 Turn 正常结束、failed、blocked 或普通 Interrupt 都不得取消已启动 child。spawn 调用的 context 只控制“reservation → child configured → initial input admitted → open edge durable → commit”事务，在成功返回前取消必须完整回滚。Root Thread runtime shutdown、Application shutdown 或显式 `close_agent` 必须停止对应 child runtime；其中只有显式 `close_agent` 将 durable spawn edge 标记为 closed。`send_input(interrupt=true)` 只中断目标 child 当前 Turn，再提交新的 input，不销毁 child Thread。

这与 Claude Code background agent 使用独立、未链接父 query 的取消域语义一致；Amadeus 不复制其 Task/AppState 外层，只保留“background child 不随父 Turn finally 自动 abort”的生命周期事实。

### 21.6 Multi-Agent Tool Contract

Basic实现只向Root Agent暴露四个Codex V1风格Tool：

#### `spawn_agent`

```json
{
  "message": "A complete, concrete delegated task"
}
```

返回：

```json
{
  "agent_id": "thread-id",
  "nickname": "atlas"
}
```

- `message` 必须非空、边界清晰且可独立执行。
- child使用当前Root的model/provider/reasoning、CWD、environment和基础指令；Basic实现不接受model、reasoning、role、service tier或fork override。
- Tool 调用成功表示 child 已创建并接纳初始任务，不表示 child 已完成。

#### `send_input`

```json
{
  "id": "thread-id",
  "message": "follow-up input",
  "interrupt": false
}
```

- 目标 Running 且 `interrupt=false` 时，通过 child 的现有 UserInput admission/steer 主链投递，在 Model Step 边界进入同一 Turn。
- 目标 Running 且 `interrupt=true` 时，先提交 `InterruptOp`，通过内部 correlated turn-stop waiter 等待当前 Turn 离开 Running，再启动新 Turn；不得复用面向模型的 `wait_agent` final-status 语义。
- 目标 Completed、Errored 或 Interrupted 时，直接提交新的 `UserInputOp` 创建后续 Turn。
- 目标 Shutdown/NotFound 时返回稳定模型可见错误。

#### `wait_agent`

```json
{
  "ids": ["thread-id"],
  "timeout_ms": 30000
}
```

- 使用 AgentStatus watch 并发等待，不轮询 Thread map。
- Codex V1 final status 固定为 Completed、Errored、Shutdown 或 NotFound；Interrupted 表示 Thread 可继续接收 input，不结束 completion wait。
- 任一目标已处于 final status 时立即返回全部已 final 的快照；否则任一 watch 首次进入 final 后返回它以及同一时刻已经 final 的其他目标，不等待全部 Agent。
- timeout 返回 `timed_out=true` 和空 final-status 集合；timeout 不改变 AgentStatus，也不伪装成 Agent failure。TUI 可以在 completed wait item 中继续显示已知 receiver identity，但不能制造 completed status。
- Completed 快照包含来自 `TurnCompleteEvent.last_agent_message` 的 Message以及 optional LastTurn；blocked outcome/reason必须原样保留，不要求父 Agent读取 child transcript或猜测停止原因。

#### `close_agent`

```json
{
  "id": "thread-id"
}
```

- 返回 shutdown 请求前的 previous status。
- 先将目标及 open descendants 的 canonical spawn edge durable 标记为 closed，再 interrupt/shutdown Session、等待 Terminated、移除 live Thread、释放 slot 和 nickname；关闭事实不得只存在于 AgentControl map。
- Root/Application shutdown 只卸载 open child runtime，不写 closed edge；Root Resume 后这些 child 仍作为 unloaded open AgentRecord 恢复。显式 close 后的 child 不得因 Root Resume 再次占用 slot。
- Depth固定为1；close仍按“目标及open descendants”定义Contract，用统一树关闭算法保证当前父子关系和清理顺序，不表示支持更深层级。

Basic实现不注册`resume_agent`、`list_agents`、`send_message`、`followup_task`或独立`interrupt_agent`。已有child通过`send_input`继续；当前active agents通过WorldState `<subagents>`和TUI read model展示；中断语义由`send_input.interrupt`承载。

### 21.7 Child Prompt、WorldState 与 Context

Multi-Agent Prompt 继续遵循 Codex Prompt 所有权：Tool 使用说明属于 ToolSpec description，child 身份与行为约束属于 ModelMessages/developer instructions，活动 Agent 列表属于 WorldState，completion notification 属于 role-aware ContextFragment。不得把这些文本散落在 Tool handler、TUI 或 ThreadManager。

#### Parent delegation guidance

`spawn_agent` Tool description 必须包含稳定 guidance：

- 先分析整体任务和 critical path，再决定哪些独立 side task 可以委派。
- 只委派具体、边界清晰、自包含且能实质推进主任务的探索工作。
- 不委派下一步立即依赖的 blocking work；父 Agent 应继续推进本地 critical path，而不是 spawn 后立即反复 wait。
- 不重复执行已经委派的同一工作，也不对同一 unresolved task 重复 spawn。
- 多个互不依赖的信息检索任务应在同一次模型响应中并行 spawn。
- child固定为explorer；代码修改、命令执行、Approval相关工作由Root Agent自己完成。
- 调用 `wait_agent` 应当克制，只在确实需要结果才能继续时等待。
- 成功 spawn 的 child 绑定 root tree 而不是当前父 Turn；父 Agent可以结束当前 Turn并在后续 Turn接收 canonical notification，不得在没有 typed cancellation/LastTurn 证据时声称“父回合结束中断了 child”。

这些 guidance 可以按 Amadeus 能力删减 Codex 中关于 worker patch、model override 和 fork 的段落，但不得改成鼓励无条件 delegation 的简单一句话。

#### Child developer instructions

SubAgent 使用 `ModelMessages.MultiAgent.Role.Subagent` 或等价 typed model-message field 中独立、版本化的 role instructions，而不是新增与 Codex 数据模型平行的顶层 `SubagentDeveloperInstructions` 字段，也不是复用 Root Default/Plan instructions 后追加 Tool guidance。至少表达：

```text
You are a sub-agent spawned by another Amadeus agent.
Work only on the delegated task.
You do not have the parent conversation; treat the task message as the complete brief.
You share the same workspace with the parent and may observe concurrent changes.
Do not modify files, execute commands, request user input, or spawn agents.
Use the available read-only tools to gather concrete evidence.
Return a concise final answer with relevant file paths, symbols, findings, and uncertainties.
If the runtime asks you to finalize because the child budget is nearing its limit, stop exploring and return the best verified partial report immediately.
Do not fabricate progress or results that you did not verify.
```

Child 仍接收 canonical environment、AGENTS.md、Skill metadata 和当前日期/时区；但 ToolRouter 最终可见能力才是工具事实源，Prompt 不得宣称实际未注册的 Tool。

#### Fresh context

child Context固定由以下内容组成：

```text
BaseInstructions
→ MultiAgent Role Subagent developer fragment
→ Environment/Permissions/AGENTS.md/Skills WorldState
→ delegated UserInput
```

不得复制父 reasoning、AssistantMessage、ToolCall、ToolResult、Plan、pending steer 或 compaction replacement history。父 Agent必须像向刚进入项目的同事交接一样，在 `message` 中说明目标、背景、已知结论、范围和期望输出。

#### Active subagents WorldState

Root WorldState 的 `<environment_context>` 增加 Codex 风格 `<subagents>`：

```xml
<subagents>
  - thread-id: atlas [explorer] running
  - thread-id: curie [explorer] completed
</subagents>
```

该列表由 AgentControl snapshot 构造，只包含当前 root tree 中未 close 的 child；状态或成员变化后通过现有 WorldState revision 机制在下一 Model Step 刷新，不作为普通用户消息。

#### Completion notification

child 当前 Turn 进入 Completed 或 Errored final status 后，AgentControl 向直接 parent 注入一次 Codex 风格 contextual user fragment；Interrupted 不触发 completion notification，显式 close 由 `close_agent` ToolResult 表达，不在已有 Turn completion 之后追加第二条 Shutdown notification：

```xml
<subagent_notification>
{"agent_id":"thread-id","nickname":"atlas","status":{"completed":"final answer"},"turn_result":{"turn_id":"turn-id","outcome":"completed","reason":"","last_agent_message":"final answer"}}
</subagent_notification>
```

- notification 是 model-visible runtime context，不是用户意图，不得投影成普通 UserMessage HistoryCell。
- canonical SubagentNotificationEvent显式携带AgentID和被交付的child TurnID；Root Resume用该typed watermark恢复notified state，不解析XML/JSON Content判断是否已交付。
- 同一 child Turn 的 terminal result 最多注入一次；`wait_agent` 返回相同派生快照不追加第二份 notification，completed 后的显式 close 也不追加 shutdown notification。
- `turn_result.outcome=blocked` 时必须携带真实 reason 和可选 finalization report；不得把它压成 `status.completed="result: blocked"`，也不得由父模型推断取消来源。
- notification 必须经过 parent Session 的 canonical append/context owner，不能由 AgentControl 直接修改 ContextManager 内部 slice。
- child Error 文本、LastAgentMessage 和 envelope 使用统一 token-based truncation，单条 notification 默认不超过 1000 tokens并为结构化 envelope保留预算；不得使用 rune count让中文、代码或 JSON 绕过 Context budget。
- notification delivery 状态只能在 parent canonical durable append成功后推进；失败必须保留可诊断 pending/error 状态或使 owning Session 明确失败，不能先标记 notified 再静默丢弃写入错误。

### 21.8 Child Tool 与权限边界

Claude Code的可取之处是child使用明确Tool allowlist，真正权限仍由统一Tool pipeline决定；Amadeus Basic实现固定为无交互explorer，不引入跨Thread Approval UI。

child 可见 Tool allowlist：

- `read`
- `glob`
- `grep`
- `read_skill`（存在可用 Skill 时）
- `web_search`（配置启用且不需要 Approval 时）

`view_image`仅在实现能够保证工作区内读取不产生交互Approval、且模型支持image input时可见；否则隐藏。所有edit/write/command/process/input/MCP/network-fetch/multi-agent Tool均隐藏。

约束：

- child `SessionPermissionContext` 独立创建，不共享 Root 的 Session grant，也不把 Root grant 复制成 child grant。
- child 继承相同 FileSystemPolicy 与 denied roots，不能获得比 Root 更宽的 workspace/read 范围。
- Tool allowlist 必须在 StepContext 捕获 ToolRouter 时生效，使模型 ToolSpec 与执行 exact route 一致；不得只在 Prompt 中告知“只读”而仍注册写 Tool。
- child Tool 调用仍执行 Validate、Prepare、Permission、Execute 和 typed ToolResult；只读角色不是绕过 ToolExecutionService 的理由。
- 任何理论上需要 Approval 的 child 调用都必须以 denied/failed ToolResult 返回，不得等待一个没有 TUI owner 的 Approval request。

Write-capable worker、child Approval routing、child `request_user_input`和MCP-specific ToolSet不属于Amadeus Basic Multi-Agent产品范围；不得为这些能力增加interactive routing、pending-request queue、空接口或配置占位。

### 21.9 Event、Rollout 与 TurnItem

Multi-Agent 控制操作使用 Codex 风格专用 TurnItem，不复用普通 ToolCall HistoryCell 拼字符串：

```go
type CollabAgentTool string

const (
    CollabAgentSpawnAgent CollabAgentTool = "spawn_agent"
    CollabAgentSendInput  CollabAgentTool = "send_input"
    CollabAgentWait       CollabAgentTool = "wait_agent"
    CollabAgentCloseAgent CollabAgentTool = "close_agent"
)

type CollabAgentToolCallStatus string

const (
    CollabAgentToolInProgress CollabAgentToolCallStatus = "in_progress"
    CollabAgentToolCompleted  CollabAgentToolCallStatus = "completed"
    CollabAgentToolFailed     CollabAgentToolCallStatus = "failed"
)

type CollabAgentRef struct {
    ThreadID      ThreadID
    AgentNickname string
    AgentRole     string
}

type CollabAgentState struct {
    Status            AgentStatus
    LastTurn          *AgentTurnResult
    NotificationError string
}

type CollabAgentToolCallItem struct {
    ID                ItemID
    Tool              CollabAgentTool
    Status            CollabAgentToolCallStatus
    SenderThreadID    ThreadID
    ReceiverAgents    []CollabAgentRef
    Prompt            string
    AgentsStates      map[ThreadID]CollabAgentState
    CreatedAt         time.Time
    CompletedAt       *time.Time
}
```

生命周期：

- spawn/send/wait/close 开始时发布 `ItemStartedEvent{CollabAgentToolCallItem}`。
- 成功、模型可见失败或 timeout 后发布且持久化唯一 completed item。
- `CollabAgentToolCallItem` 进入 canonical EventMsgItem，使 Resume 与 live TUI 使用同一 projection。
- Tool 的模型协议仍保留标准 FunctionCall/FunctionCallOutput；专用 TurnItem 是 UI/Event projection，不取代 Provider Tool Result。
- Multi-Agent Tool 必须通过 Tool event policy 抑制重复的 generic ToolHistoryCell，不能同时显示一条普通 Tool 和一条 collaboration row。
- child 自身完整 Tool/Assistant history只属于 child rollout；Root history只保存 collaboration item 与 bounded completion notification，不复制 child 全 transcript。
- CollabAgentState、`wait_agent` ToolResult、WorldState read model 与 notification只能从同一个 AgentControl snapshot/terminal reducer投影；TUI不得从 ToolResult JSON、`result: blocked` 文本或 child transcript重新解释 outcome。

### 21.10 TUI Visual Contract

Rich TUI对齐Codex multi-agent history presentation，但Basic产品面只实现root conversation中的控制操作和状态投影，不实现完整`/agent` picker、Alt+Left/Right thread navigation或child transcript attach。

`CollabAgentHistoryCell` 使用稳定标题：

- spawn completed：`Spawned <nickname> [explorer]`
- spawn failed：`Agent spawn failed`
- send completed：`Sent input to <nickname>`
- wait started：`Waiting for <nickname>` 或 `Waiting for N agents`
- wait completed：`Finished waiting`
- close completed：`Closed <nickname>`

展示规则：

- Agent nickname 使用 cyan，主动作使用 bold；状态使用与现有 success/warning/error 一致的颜色语义。
- spawn/send 展示截断后的 delegated prompt preview，不展开完整 child transcript。
- wait completed 对本次返回的 final Agent 显示 status；Completed 显示 bounded `last_agent_message` preview，Errored 显示 bounded error，LastTurn 为 blocked 时额外显示 `blocked: <reason>`，不得把它展示为普通成功完成。
- InProgress wait 使用 ActiveHistoryCell/working projection，completed 后原位替换或追加 canonical completed cell，不留下重复 spinner。
- completion notification 不渲染成用户气泡；若 notification 在 Root 没有 active wait 时到达，可追加轻量 `AgentStatusHistoryCell`，例如 `atlas completed`，其事实仍来自 AgentStatus/Event reducer。
- live TUI 使用相同 CollabAgentToolCallItem reducer 和文本语义，不从 ToolResult 字符串重新解析 agent 状态。
- Resume 只重放 completed collaboration item，不恢复过去的 wait spinner 或 child streaming delta。

Amadeus不实现完整Codex `/agent` picker、root/child history切换、相关快捷键或非当前child Approval overlay；TUI不得为模拟这些非目标而读取AgentControl mutable map或增加占位导航状态。

### 21.11 Persistence、Resume 与 Shutdown

```go
type AgentSpawnEdgeState string

const (
    AgentSpawnEdgeOpen   AgentSpawnEdgeState = "open"
    AgentSpawnEdgeClosed AgentSpawnEdgeState = "closed"
)

type AgentSpawnEdgeItem struct {
    AgentID        ThreadID
    ParentThreadID ThreadID
    State          AgentSpawnEdgeState
    UpdatedAt      time.Time
}
```

- 每个 child 使用正常 LiveThread/ThreadStore writer 和 canonical JSONL；`SessionMetaItem` 记录 `SubAgentSessionSource`。
- Root canonical rollout持有最小 typed spawn-edge lifecycle：`AgentSpawnEdgeItem{AgentID, ParentThreadID, State: open|closed}`。spawn 只有在 child configured、初始 UserInput admitted且 open edge durable 后才成功；显式 `close_agent` 在停止 runtime前先 durable记录 closed。该 item 是 root tree恢复的权威，不从 completed CollabAgentToolCall文本、child title或 SQLite存在性猜测 open/closed。
- SQLite metadata index投影session source、parent Thread与可重建edge state，使默认顶层session list排除SubAgent；只提供语义明确的`ListOpenChildren`，不保留会无条件返回archived/closed历史child的`ListChildren`。SQLite丢失时从Root/child当前格式rollout重建相同projection。
- Root Resume恢复 open child metadata为 persisted/unloaded AgentRecord，但不自动重启旧 Turn或创建 completion watcher。对 unloaded child执行`send_input`时，由 ThreadManager按 child ThreadID恢复完整 Session；`wait_agent`可以直接返回已恢复的 final snapshot而不加载 runtime。
- Live child event与Resume child rollout必须通过同一 AgentStatus/AgentTurnResult reducer；SessionMeta-only history恢复为 PendingInit，未闭合 Turn经统一 interrupted-turn recovery后恢复为 Interrupted，completed/blocked/failed恢复 exact LastTurn和`last_agent_message`，不得默认伪造 Completed。
- 已完成的 Root collaboration item和completion notification按 canonical history恢复；closed child rollout仍可供 diagnostics读取，但不进入 AgentControl、不占 slot，也不成为普通 `/resume`入口。
- Root shutdown 固定顺序为：停止新 spawn → 对所有 open child 发 Shutdown → 等待 child Terminated → 关闭 Root SessionServices/LiveThread → 释放 AgentControl。
- Root/Application shutdown只卸载 runtime并保留 open edge；显式 `close_agent`才关闭 edge。二者不得复用一个会丢失关闭意图或错误复活 child的无标记 helper。
- child shutdown 不得关闭共享 AgentControl；只有 root tree owner 关闭 control。
- child event consumer、completion watcher 和 status waiter 都必须受 AgentControl/root lifetime 管理，不允许 fire-and-forget 泄漏。

### 21.12 配置

在现有 `agent` 配置域增加：

```yaml
agent:
  max_parallel_tools: 4
  multi_agent:
    enabled: true
    max_agents: 4
    max_depth: 1
    child_max_samples: 20
    child_max_tool_calls: 100
    child_max_duration: 15m
```

约束：

- `enabled=false` 时不注册 Multi-Agent Tool，也不注入 delegation guidance 或 `<subagents>` WorldState。
- `max_depth`只接受1；字段只使固定限制成为配置事实，不作为更深层级扩展点。
- `max_agents` 必须为正且有安全上限；不能复用 `max_parallel_tools` 表达 agent tree 容量。
- child budget 是 Root 配置冻结到 child Session 的 runtime snapshot；运行中的 child 不读取 mutable config。soft finalization boundary从该 snapshot确定并保留至多一次 no-tools sample，不增加另一套可变 budget配置。

### 21.13 明确产品非目标

Amadeus明确不实现、也不为未来预留以下Multi-Agent能力：

- Codex Multi-Agent V2、AgentPath、task name、mailbox、`send_message`、`followup_task`、residency eviction 和通用冷加载调度。
- 公开 `resume_agent`、用户直接 attach child transcript、任意 detached child恢复和Codex V2通用AgentGraphStore；基础版为Root Resume持久化flat depth-one spawn edge属于identity/lifecycle正确性，不是V2 graph功能。
- Parent history full/recent fork、Prompt cache fork 和 compaction-aware fork filtering。
- 自定义 Agent Definition、Markdown agents、Plugin agents、agent memory、model/reasoning/service-tier override。
- write-capable worker、并发代码修改、worktree、文件锁或自动 merge。
- child Approval、child `request_user_input`、background permission bubble。
- team/teammate、remote agent、daemon task、TaskOutput/TaskStop 第二套任务系统。
- 完整 `/agent` picker、child transcript切换和跨 Thread interactive overlay。

这些能力不属于后续路线图，不得以空字段、未使用接口、feature flag、generic extension point、兼容DTO或文档TODO预埋进生产主链。未来只有用户重新明确改变产品范围时，才允许重新进行独立源码审计和架构设计；当前实现与AC任务不得为其预付复杂度。

### 21.14 验收不变量

Basic Multi-Agent 必须满足：

1. SubAgent 是完整 Thread/Session；代码中不存在 Tool handler 直接调用 `run_turn` 或 Provider 的旁路。
2. 同一 root tree 只有一个 AgentControl owner；ThreadManager 仍是 live Thread 的唯一 registry。
3. child Context、ActiveTurn、SessionPermissionContext、Tool state 和 rollout 与 Root 隔离。
4. child ToolRouter 只暴露真实 read-only allowlist，模型 ToolSpec 与执行 route 完全一致。
5. 多个 spawn reservation 并发安全，失败、取消和 panic 不泄漏 slot、nickname、writer 或 goroutine。
6. `send_input` 正确区分 steer、interrupt-and-restart 与 idle new Turn。
7. `wait_agent` 事件驱动，在任一 Agent进入Codex final status后返回当时全部 final snapshot；timeout不改变AgentStatus且不返回伪造final状态。
8. Completed/Errored/Interrupted child 在 close 前继续占用 slot，并可再次 `send_input`。
9. completion notification 对每个 child Turn terminal result至多durable注入一次，保留 blocked outcome/reason且不是普通 UserMessage UI；写入失败不推进notified状态。
10. Root shutdown 必须终止全部 child；父当前 Turn interrupt 不自动关闭 child。
11. live TUI 与 Resume 对 CollabAgentToolCallItem 使用同一 typed projection。
12. 默认 session list 不把 child thread 当作顶层用户会话。
13. TUI 不从 ToolResult 文本解析状态，也不维护第二份 AgentStatus 真相。
14. `TurnCompleteEvent.last_agent_message` 是唯一final answer authority；AgentControl不存在`latestAssistant`，live/Resume使用同一terminal reducer。
15. child进入soft budget boundary后至多执行一次Tools为空的finalization sample；hard blocked仍通过LastTurn把真实reason交给parent。
16. Architecture tests 禁止 nested SessionTask、generic agent task bus、child write Tool、未绑定 owner 的 watcher、Assistant Item final-message推断和旧式字符串 completion message。
17. Root Resume 只恢复同一SessionID、parent relation合法且spawn edge为open的persisted child metadata；closed/archived child不占slot，child内部恢复、send/wait/close全部以child ThreadID路由，SessionID不进入registry key。
18. Root Turn正常完成、failed、blocked或Interrupt后，已成功spawn的child仍可完成并向同一Root追加notification；只有root-tree shutdown、Application shutdown或显式close停止child runtime。

## 22. Persistence

Amadeus 使用 JSONL canonical rollout + SQLite metadata index：

```text
$AMADEUS_HOME/
├── sessions/YYYY/MM/DD/rollout-<timestamp>-<thread-id>.jsonl
└── data/amadeus.db
```

### 22.1 JSONL Canonical Rollout

- 当前Rollout格式为v6；v5及更早格式直接拒绝，不提供AC migration/decoder。
- 每个 Thread 一个 Rollout 文件。
- 每行是独立的 RolloutLine JSON 对象。
- sequence 从 1 开始严格递增，不重复、不倒退。
- ThreadID 使用 Protocol/Identity domain 的 canonical UUID value object，新创建值为 UUIDv7；TurnID 使用其对应的 typed stable identity。SessionMetaItem 以 `session_id + id + optional parent_thread_id` 保存 SessionID/ThreadID/parent relation，TurnContextItem、ResponseItem 和 EventMsg payload 按需携带 `thread_id`；RolloutLine 不定义 ID owner。
- LiveThread/ThreadStore 是唯一文件写入边界；Session、Tool 和 TUI 不直接打开文件追加。
- 恢复时逐行解析；只有最后一条不完整记录可以被安全忽略或截断，完整但缺少末尾换行的最后一条记录会被补齐后再追加，其他解析错误必须显式报告。
- append batch 必须先完整编码再写入；部分写失败时回滚到 batch 起始 offset，flush 使用文件同步保证 durable 顺序。
- ToolCall/ToolResult 使用稳定 CallID 配对。
- CompactedItem、TurnContextItem、ResponseItem、Root-only AgentSpawnEdgeItem以及store policy选中的Plan/Token/Approval/Turn EventMsg均进入Rollout；未决ApprovalRequestEvent、未决RequestUserInputEvent、ApprovalDecisionOp与UserInputAnswerOp不作为独立canonical history。
- Runtime 临时状态、goroutine、进程句柄和 Pending Future 不持久化。
- SessionMetaItem 保存 canonical SessionID、ThreadID、可选 ParentThreadID，以及重建索引所需的 CWD、标题、模型、Git metadata、归档初态和创建时间；后续标题/归档变化使用对应 typed EventMsg。
- Rollout 文件名中的 `<thread-id>` 必须是与 SessionMetaItem.ID、SQLite `threads.id` 和 Resume target 完全相同的 canonical UUID，不增加 `thread-` 前缀，也不维护单独 display ID。

### 22.2 SQLite State DB

SQLite 位于 `$AMADEUS_HOME/data/amadeus.db`，核心表保持最小：

当前SQLite metadata schema为v5；v4及更早schema直接拒绝并由当前Rollout重建。

#### `schema_info`

- version
- created_at

#### `threads`

- id
- source_kind
- parent_thread_id
- agent_depth
- agent_nickname
- agent_role
- agent_edge_state
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

`threads.id`保存canonical ThreadID UUID并作为主键；parent/source/agent metadata只用于列表过滤、parent/child traversal和可重建索引。SubAgent行的`agent_edge_state`投影Root rollout中该edge的最新open/closed状态，Root行必须为空；它不是独立事实源。SQLite `StoredThread`不保存SessionID，也不以SessionID建索引或作为agent tree事实源；SessionID必须从目标Thread Rollout的canonical SessionMeta恢复。SQLite adapter必须先扫描ID string，再通过Protocol/Identity parser构造typed ThreadID；不能让任意数据库文本绕过Domain validation。

不建立 `projects`、`turns`、`messages`、`summaries` 或 SQLite `rollout_items` 表。Turn、消息、Tool 和 Compaction 历史只存在于 canonical Rollout。

Amadeus 只定义并校验当前 `schema_info.version`，不提供 schema migration、历史格式分类或专门的数据重置提示。SQLite 是可重建 index，可以直接删除并从当前格式 Rollout 重建；非当前 schema、表结构或 Rollout 不进入生产读取路径。

### 22.3 Metadata Sync 与 Rebuild

```text
LiveThread.AppendItems
→ LocalThreadStore durable write + flush JSONL
→ LiveThread.MetadataSync 观察本次已 durable 的 typed facts
→ 生成 MetadataPatch
→ StateDB.ApplyThreadMetadataPatch
```

- SQLite 可以暂时落后 JSONL，但不能包含尚未 durable 的 Rollout 事实。
- Recorder 必须维护 durable watermark；MetadataSync 只能消费不超过该 watermark 的已 durable typed facts。Buffered Append 不触发 SQLite upsert，显式 Flush 或 Durable Append 成功后才能同步索引。
- 正常 append 不得重新读取完整 Rollout 再重建 metadata；MetadataSync 必须从刚追加的 typed facts 产生增量 patch。完整 Rollout 扫描只属于 Resume、显式 Rebuild 或 reconciliation。
- MetadataSync 必须维护自己的 pending patch/generation，并在 patch 应用成功后推进 watermark；patch 应用失败不能丢弃已 durable 的 JSONL 事实。
- `append → SQLite upsert → flush` 在任何路径都属于非法顺序；进程在 flush 前崩溃时，恢复结果允许缺少 buffered tail，但 SQLite 不能引用该 tail。
- Metadata 更新失败必须记录警告并保留可重建状态，不能回滚已经 durable 的 canonical history。
- 当前schema的SQLite index缺失或漂移时，从当前格式`SessionMetaItem`、ResponseItem、AgentSpawnEdgeItem和EventMsg重建StoredThread；重建过程校验SessionMeta.SessionID/ID/ParentThreadID与edge parent contract，但只把Thread metadata、parent relation与edge state投影进SQLite，不复制SessionID。活动Thread即使索引被清空，下一次canonical append也能直接重新upsert。
- `/resume`、列表和搜索优先查询 SQLite；索引缺失或漂移时可扫描 Rollout 修复。
- `tokens_used` 投影最后一个 canonical TokenCountEvent.Info.TotalTokenUsage.TotalTokens；TokenCountEvent 是累计 snapshot，因此 Metadata projector 必须替换该值，不能把历次 snapshot 再次求和。
- Session Permission State（Mode、Additional Working Directories 和 Session Rules）只存在于活动 internal Session，不写入 Thread metadata，也不从历史 Approval Decision 恢复。

只有当真实产品需求证明需要分页历史或全文搜索时，才增加可重建的 SQLite History Projection。

旧 JSONL、旧 SQLite、旧 fixture 和旧本地开发数据不属于恢复输入。发生格式变更时直接删除旧 codec、旧表访问和兼容测试，并要求清理开发数据后重建；不得留下 migration decoder、legacy reader/writer 或双格式探测。

## 23. MCP 与 Skill

### 23.1 MCP

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

### 23.2 Skill

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
- `SkillCatalog` 的常驻对象是 `SkillMetadata`/`SkillResource`；完整正文和资源内容属于按需加载的 read result，不得缓存为另一套 Skill owner。bootstrap 只提供 Skill roots/settings，SessionServices 直接创建并拥有 SkillCatalog。
- `SKILL.md` 是显式工作流和说明的唯一正文入口。普通 Turn 通过 `skills_catalog` WorldState 注入 Skill Index/metadata；用户在输入中使用 `$skill-name` 后，Session 生成 `SkillInjection`，以 contextual user `<skill>` ResponseItem 追加到 canonical history；TUI 的 Skill 操作只改变 enabled policy，不伪造正文注入。
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

## 24. Web 与网络

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

## 25. 配置

### 25.1 配置位置

Amadeus 配置固定属于 `$AMADEUS_HOME`：

```text
$AMADEUS_HOME/config.yaml
```

如果未设置 `AMADEUS_HOME`，由 Bootstrap 使用二进制所在目录作为默认 Home。不得回退到任意当前工作目录寻找配置。

仓库中的完整模板固定命名为 `configs/config.yaml.example`。它只是供用户复制或通过 `--config` 显式校验的样例，不是第二个自动发现位置；生产 Loader 仍只自动读取 `$AMADEUS_HOME/config.yaml`。

### 25.2 配置优先级

```text
CLI Flags
> Environment Variables
> $AMADEUS_HOME/config.yaml
> Built-in Field Defaults
```

API Key 默认通过环境变量或配置文件提供，不要求暴露 CLI Flag，避免进入 Shell History。

Built-in Field Defaults 只表示 retry、timeout、Tool Output truncation 等字段级归一化默认值，不创建内置 Provider 或内置 Provider entry。`model_provider` 必须引用用户在 `model_providers` 中定义的条目。

### 25.3 核心配置域

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

## 26. Event Protocol

### 26.1 协议分层

Amadeus 使用 Codex 风格的 `Submission/Event/EventMsg` 作为 Session 的唯一公开协议，但不建立支持任意 topic/subscriber 的通用 Event Bus。边界固定为：

```text
Submission {id, op}  Interface/Application → internal Session
Event {id, msg}      internal Session → Interface/Application
RolloutItem          internal Session → LiveThread，canonical persistence
TUI Message          Bubble Tea 内部按键、动画、Popup 与异步结果
Trace / Telemetry    Runtime 内部诊断，不进入产品 Event Protocol
```

Approval request 是 `EventMsg` variant，回答使用带 RequestID 的 `ApprovalDecisionOp` 作为新的 Submission。`request_user_input` 使用独立的 `RequestUserInputEvent → UserInputAnswerOp` pair，两种交互共享 Session waiter 基础设施但保持 payload 与语义分离。

NextTurnQueue 是尚未提交的 TUI state，不是 Session Event Protocol。enqueue、preview、InFlight gate 和 attachment replacement 不新增 `QueuedInputOp`、`QueuedAdmissionEvent` 或 durable queue item；只有 dequeue 后正常提交的 `UserInputOp` 及其 Started Turn lifecycle 进入公共协议。

### 26.2 Submission 与 Event Envelope

Protocol/Identity domain 拥有 SessionID、ThreadID、TurnID、SubmissionID、RequestID 和 ItemID；rollout、thread、turn 或 persistence package 不得重新定义这些 ID。SessionID/ThreadID 是封装 UUID 的不同领域类型：新建值使用 UUIDv7，跨 CLI、JSON、Tool 与 Persistence boundary 时必须显式 parse/format；其他 ID 是否采用 UUID 由各自 Contract 决定，不通过一个 `NextID(kind)` 抹平语义。

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

### 26.3 最小 EventMsg

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
ApprovalRequestEvent
RequestUserInputEvent

WarningEvent
ErrorEvent
StreamErrorEvent
```

`TurnCompleteEvent` 是所有 consumer 的 terminal authority，并按 Codex Contract直接携带 `last_agent_message`：

```go
type TurnCompleteEvent struct {
    ThreadID         ThreadID
    TurnID           TurnID
    Status           TurnTerminalStatus
    Outcome          TurnOutcome
    Reason           string
    Summary          string
    Error            string
    LastAgentMessage *string
    FinishedAt       time.Time
}
```

`LastAgentMessage` 只由 `run_turn` 的最终模型完成设置；它与供TUI展示的completed Assistant TurnItem来自同一最终文本，但前者是Turn completion/AgentStatus authority，后者是History projection。Tool continuation preamble、Reasoning和stream delta不能填充该字段。Resume直接读取canonical TurnCompleteEvent，不扫描此前Assistant Item。

`SessionConfiguredEvent` 必须携带 SessionID、ThreadID、可选 ParentThreadID 和真实生效的完整 `SessionConfiguration`，不允许只发送无内容的 configured marker：

```go
type SessionConfiguredEvent struct {
    SessionID      SessionID
    ThreadID       ThreadID
    ParentThreadID *ThreadID
    Configuration  SessionConfiguration
}
```

Root event 的 SessionID 与 ThreadID 使用同一 UUID value，ParentThreadID 为空；child event 继承 Root SessionID，使用自己的 ThreadID 和直接 parent ThreadID。Event routing、`ThreadIDOf`、attachment matching 和 Rollout EventMsg scope 始终使用 ThreadID；scoping helper 不能覆盖或从 ThreadID 重新推导 SessionID。

`ThreadSettingsAppliedEvent` 表示设置已由 Session 接纳并应用，也必须携带应用后的完整 `SessionConfiguration`，而不是只返回 Mode 或 caller 请求值；设置失败使用 correlated ErrorEvent。`ThreadViewSnapshot.Configuration`、`SessionConfiguredEvent.Configuration` 和 `ThreadSettingsAppliedEvent.Configuration` 共用同一个 typed model，TUI 统一通过 `applySessionConfiguration()` 应用，避免 snapshot/live event 两套字段、两套 owner 或部分更新生命周期。

`TokenCountEvent` 携带完整 snapshot，不是 delta：

```go
type TokenCountEvent struct {
    ThreadID              ThreadID
    TurnID                TurnID
    Info                  *TokenUsageInfo
    ActiveContextTokens   int64
    ActiveContextEstimated bool
    ObservedThroughSequence uint64
}
```

每个成功 request、local suffix refresh 和 Compaction replacement recompute 都产生完整 snapshot；`ObservedThroughSequence` 是该 active checkpoint 已包含的 canonical history watermark，request 执行期间并发到达但未被模型看到的事实即使排在 TokenCountEvent 之前，也必须作为 watermark 后的 local suffix 计入。live reducer 与 Resume projector 只替换当前值。`TotalTokenUsage` 用于累计统计，`LastTokenUsage.TotalTokens` 用于 Provider-observed baseline，`ActiveContextTokens` 是 Session 已汇合 Provider baseline、local suffix 和 estimator fallback 后的唯一 UI occupancy；`ActiveContextEstimated` 只在没有可信 Provider baseline 或 replacement 后本地重算时为 true。Event consumer 不自行累加，也不使用 `InputTokens` 覆盖 `EstimatedInputTokens` 一类字段优先级猜测当前占用。

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

### 26.4 TurnItem

`TurnItem` 是 EventMsg 和 TUI History 的稳定业务项；需要恢复的完成态通常通过 `EventMsgItem(ItemCompletedEvent)` 原样持久化，拥有专用 canonical item 的类型按 store policy 只保留一个 durable owner。首批类型：

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

`TurnItem` 的 payload 必须由 `Kind` 唯一决定，并在内存领域模型中使用对应的 typed Go struct；稳定领域 Contract 不使用 `Payload any`、按 Tool 名解释的通用 map，或让 JSON decode 后的 `map[string]any` 充当事实模型。JSON `RawMessage` 只允许存在于 codec envelope 或明确的不透明外部扩展边界，进入 `TurnItem` 前必须完成 variant decode 与 validation。每个 variant 都必须有独立的 identity、状态、字段校验和 round-trip fixture；TUI、Rollout projector 与 Tool renderer 只能消费已校验的 typed item。

职责：

- `ToolCallItem`：read、glob、grep、MCP、Skill、Web、view_image 等通用 Tool。
- `CommandExecutionItem`：命令、进程、stdout/stderr、退出码和 duration。
- `FileChangeItem`：edit/write、Structured Diff、Approval 结果和最终修改状态。
- `PlanUpdateEvent` 不属于 TurnItem；它是 live transient checklist notification，不参与 canonical replay。
- `PlanItem`：显式 Plan Mode 中 `<proposed_plan>` 的完整 Markdown；它是可 Replay 的完成态业务投影。
- `ContextCompactionItem`：live 使用普通 ItemStarted/ItemCompleted；Replay 的唯一完成事实是 CompactedItem，store policy 不再额外持久化该 ItemCompletedEvent，避免同一 checkpoint 生成两个 HistoryCell。

Item 状态至少包括：

```text
in_progress
completed
failed
declined
stale
```

### 26.5 Item 生命周期与 Delta

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

### 26.6 Proposed Plan

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

### 26.7 Approval Request

`ApprovalRequestEvent` 带稳定 RequestID、ThreadID、TurnID 和完整 typed Presentation。它至少包含 Tool 名、操作类型、Title、Subtitle、Question、目标路径/命令/CWD、Details、`*FileChangePreview` 和 typed Options；Diff 不得在协议桥接时转换为 `string` 或 `any` 的扁平文本。TUI 只渲染 Presentation，不重新解释权限语义；回答通过 `ApprovalDecisionOp` 返回。

```go
type ApprovalRequestEvent struct {
    RequestID    RequestID
    Approval     ApprovalRequest
    Presentation ApprovalPresentation
}
```

未决 ApprovalRequestEvent 不作为 canonical history。最终 completed/declined/failed 结果进入对应 Tool/File/Command ItemCompletedEvent；安全审计可以单独记录，但不成为第二输出协议。

### 26.8 Request User Input

`RequestUserInputEvent` 带稳定 RequestID、ThreadID、TurnID、CallID 和 typed Questions；每个 Question 使用稳定 `snake_case` ID，并包含 Header、Question、Options、`MultiSelect` 等展示与回答约束。TUI/adapter 不改变问题语义，回答通过 correlated `UserInputAnswerOp` 返回。

未决 `RequestUserInputEvent` 与 `UserInputAnswerOp` 不作为独立 canonical history。`request_user_input` 的普通 Function Call 与最终 Function Call Output 仍按 ResponseItem 持久化，因此模型上下文和 Resume 保留最终 Tool 事实，而不恢复已关闭的 Overlay 或 pending waiter。

### 26.9 Event、Rollout 与 Delivery

持久化策略：

- 持久化 store policy 指定的 TurnStartedEvent、TurnCompleteEvent/TurnAbortedEvent、ItemCompletedEvent（包含 completed Proposed Plan）、TokenCountEvent 和恢复所需 Context facts；Compaction replay 以 CompactedItem 为唯一 checkpoint，不再持久化或发布第二个 ContextCompactedEvent 完成协议。
- Root rollout还持久化flat basic multi-agent的AgentSpawnEdgeItem；open/closed edge是Root Resume恢复AgentControl成员的权威，不把child metadata存在性等同于open。
- 不持久化高频 Agent/Reasoning/Command/Plan Delta、Working、未决 ApprovalRequestEvent、未决 `RequestUserInputEvent`、`PlanUpdateEvent`、NextTurnQueue/queued preview、Popup、动画 Tick 和 retrying StreamErrorEvent。`update_plan` 与 `request_user_input` 的 Function Call/Function Call Output 仍作为普通 ResponseItem 持久化。
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

## 27. 错误模型

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

Multi-Agent terminal/persistence error至少区分：

- `agent_budget_blocked`：child hard budget或finalization无法产生合法交付，LastTurn保留具体reason。
- `agent_notification_persistence_failed`：parent canonical notification未durable，notified状态不得推进。
- `agent_edge_persistence_failed`：spawn open或explicit close edge未durable；spawn不得对模型报告成功，close不得假装已形成可恢复关闭事实。
- `agent_resume_inconsistent`：open edge、child SessionMeta、SessionID或ParentThreadID不一致，child不得进入AgentControl。

Provider/stream error 还必须区分：

- transient retrying：Core 已决定自动重试，Turn 保持 running，只更新 live status。
- terminal provider failure：不可恢复或重试耗尽，进入 failed SessionTask result 和唯一 Turn terminal protocol。
- context window exceeded：Provider 明确拒绝当前上下文，Adapter 归一化为 typed `context_window_exceeded`，不得退化为通用 invalid_request 或依赖错误字符串触发 compact。

Compaction error 至少区分：

- `compaction_no_history`：没有可压缩的真实模型历史。
- `compaction_stale`：生成摘要后 source version/hash 已变化，replacement 未安装。
- `compaction_insufficient`：安装预览或安装后 active tokens 没有实质下降，不能继续无界重复 compact。
- `compaction_invalid_output`：摘要为空、包含 Tool Call、finish reason 不完整或 replacement 无法通过 typed validation。
- `compaction_persistence_failed`：replacement durable append/flush 失败，live ContextManager 保持旧状态。

所有错误必须：

- 带稳定错误码。
- 保留底层错误链。
- 不泄露 API Key。
- 在 TUI 显示可行动说明。
- 在 Turn 终态中可恢复或可诊断。

禁止出现“Working 动画停止但没有 Error/Completed Event”的静默失败。
禁止把 transient retrying error 插入永久 ErrorHistoryCell、提前finalize Assistant stream或停止Turn；也禁止重试耗尽后只清除`Reconnecting...`状态而没有最终错误和Turn终态。

## 28. 测试策略

### 28.1 Runtime Contract

- 一个用户输入只创建一个 Turn。
- 生产 RegularTask/CompactTask 不持有或回调 CLI/Application controller，不通过 request/result side channel 取得执行依赖。
- CLI invocation 在进入 Session 前已归一化；Session 可以在没有 Cobra/TUI 对象的测试中独立运行完整 Turn。
- SessionServices 在 Session spawn 时只构造一次；连续两个 Turn 复用 ModelClient、ToolRegistry、ToolExecutionService、ProcessManager、MCP/Skill/Web、Approval/Permission 与 UserInputRequester 交互能力，Session 关闭后统一释放。
- architecture test 验证 SessionState.Configuration 是配置唯一事实源，SessionServices 是 Session capability owner。
- RegularTask 不创建或关闭完整 Agent Runtime，只调用 Session 模块内唯一 `run_turn`。
- Turn 在模型调用前持久化。
- ItemStartedEvent/ItemCompletedEvent 使用相同 ItemID；持久化的 ItemCompletedEvent 足以独立 Replay。
- ResponseItem 与 ItemCompletedEvent 在 live ItemCompletedEvent 前进入 ordered canonical append；进程在 Turn 中途退出时不会出现“UI 已 completed、Rollout 无事实”。
- 每个成功 sampling/compaction request 在下一模型动作前只记录一次 TokenUsageInfo；Turn terminal 不再从 TaskOutput 追加第二个聚合 usage。
- Compaction install 将 CompactedItem 与 refreshed TokenCountEvent snapshot 作为同一 durable boundary；flush 失败时 ContextManager、live completed item 和后续 sampling 都不能越过该 checkpoint。
- Completed/Aborted 终态唯一，失败信息进入唯一终态。
- completed、blocked、failed 和 aborted 按 TaskOutcome/terminal contract 映射，不使用通用 Go error 表达 blocked 或 budget limit。
- `run_turn` final text经TaskOutput直接进入TurnCompleteEvent.last_agent_message；Tool continuation Assistant Item不能成为final-message fallback。
- Interface 只依赖 Event/EventMsg 识别 Turn terminal，不同时等待私有 Task completion。
- Rollout append/flush 和 ActiveTurn 清理先于终态 Event。
- Resume 后 Rollout 顺序稳定。
- crash/fault injection 验证 SQLite 永不超过 JSONL durable watermark，Buffered Append 不提前 upsert metadata。
- architecture test 验证 production SessionTask 不引用 CLI/TUI controller、TUI appModel、Cobra command 或完整 invocation，并直接通过 Session/SessionServices 完成运行。
- Root/child 创建验证共享 SessionID 与独立 ThreadID；Root Resume 验证requested ID、StoredThread、Rollout filename、SessionMeta.ID/SessionID和canonical open edge一致，只恢复parent relation合法且open的persisted child metadata。
- persisted/unloaded child通过child ThreadID触发internal resume；closed/archived child、错误SessionID、ParentThreadID或Rollout identity不进入AgentControl registry，也不留下live writer/runtime或占用slot。
- 同一pure reducer分别处理live child Event和Resume rollout；覆盖SessionMeta-only、interrupted tail、completed、blocked、failed及optional LastAgentMessage。
- Root Turn先terminal而child后terminal的E2E证明parent Turn context不拥有child lifetime；Root shutdown与显式close分别验证“保留open edge”和“durable closed edge”两种不同语义。
- child soft budget触发至多一次no-tools finalization sample；Tool preamble后final/blocked均只通过TurnCompleteEvent authority交付，hard blocked reason在notification/wait/TUI中无损可见。

### 28.2 Event Protocol

- Submission/Event 使用 correlation ID；具体 EventMsg 按需携带 ThreadID/TurnID/RequestID/ItemID。
- SessionConfiguredEvent 同时携带 SessionID、ThreadID 和可选 ParentThreadID；其他 EventMsg 不为 tree-level correlation 重复增加 SessionID，事件路由仍以具体 ThreadID 为准。
- Tool Invocation、Audit record 与 Provider request metadata 验证 Root/child 使用相同 SessionID、不同 ThreadID 和正确 TurnID；`spawn_agent` 只能从 Invocation.ThreadID 取得 parent target。
- Delta 只能更新相同 ItemID 的 Active Item；迟到 Delta 不改变 Completed Item。
- 没有 ItemStartedEvent 的 ItemCompletedEvent 仍可直接渲染。
- ApprovalRequestEvent 必须通过带 RequestID 的 ApprovalDecisionOp 完成，且不使用独立 Request channel。
- RequestUserInputEvent 必须通过带 RequestID 的 UserInputAnswerOp 完成；Default 与 Plan Mode 复用同一 Tool、Event、waiter 和回答协议，不复用 ApprovalDecision。
- Plan Mode 的 `<proposed_plan>` 必须产生独立 Plan Item；PlanDeltaEvent 只能更新相同 ItemID，completed Plan Item 必须足以独立 Replay，且不能与 PlanUpdateEvent checklist 混用。
- `/plan <task>` 的 mode override 与用户输入在同一个 UserInputOp 中应用；设置失败不得创建 Turn，ActiveTurn 期间不得改变已经冻结的 CollaborationMode。
- 慢 Renderer、已关闭 TUI 或调试 Subscriber 不导致 Agent Turn 失败。
- Live 和 Replay 对相同 ItemCompletedEvent 生成一致 HistoryCell。
- TokenCountEvent 在 live、attach snapshot 和 Resume 中都采用 replace-snapshot reducer；ContextCompaction live Item lifecycle 与 CompactedItem replay 只生成一个 ContextCompactedCell。
- Protocol package 只包含 identity/DTO/contracts；TranscriptState、TUI reducer 和 rollout replay projector 位于各自 projection/application package。

### 28.3 Context

- 超大 `docs/design.md` 不导致静默停止。
- 读取整个 `docs` 目录后仍可继续对话。
- Tool Result 被安全投影，live replay 与 Rollout/Resume projection 对 status、error、partial、metadata 保持语义等价。
- Prompt 数据模型和装配职责与 Codex 同构：Session-owned persisted `BaseInstructions`、`ModelMessages`、`ResponseItem`、`ToolSpec`、typed `WorldState`、`CollaborationModeState`、role-aware `ContextFragment`、`Prompt` 和 `PromptSnapshot` 各自只有一个 owner。
- Responses wire 使用独立 `instructions` 字段且 input 不包含 synthetic system Base；Chat Completions 只由 Dialect 执行唯一兼容映射。Base resolution 遵循 custom > resumed SessionMeta > model template，并在同一 Thread Resume 后保持 exact text/provenance。
- 普通 Turn 不存在独立 `RegularTaskPrompt`；Default/Plan 使用一个完整 Collaboration Mode source，Compact 使用独立 Codex `SUMMARIZATION_PROMPT`/`SUMMARY_PREFIX` assets，三者不通过 Go 字符串追加第二 owner。
- 首次 Turn 在真实 User input 前记录 full initial contextual messages；后续只记录 typed WorldState diff。模型可见 fragment、WorldState full/patch 和 TurnContext reference 的持久化顺序可恢复且 baseline 不越过 history。
- Prompt 资产、动态 Context Fragment、ToolSpec 和 ContextManager history 不通过旧 `Assets`、`DeveloperInstructions(mode, toolNames)`、`ContextUpdateEvent`、replace-key map 或 request-only developer string 双写。
- Claude Code guidance 只进入对应 `read`/`edit`/`write`/`glob`/`grep` ToolSpec；Codex guidance 进入 `update_plan`/`request_user_input`/`execute_command`/`write_stdin`/`view_image`、Multi-Agent 与 MCP Resource ToolSpec；`web_search`/`web_fetch`、`read_skill` 和 lazy MCP wrapper 以 Amadeus Contract 为权威。Collaboration Mode 不随 Tool 名列表变化。
- 每个 Model Step 的 WorldState、Prompt、ToolSpecs 和 Tool execution router 来自同一 StepContext；Tool/MCP/Skill/AgentsMd revision 变化后下一 Step 会重新 capture。
- 空或 stale RequestSnapshot 不会被注入 ToolExecutionService；deferred/lazy capability 使用精确 revision 校验。
- canonical Rollout payload 通过统一 typed encoder/decoder round-trip；writer 与 projector 不使用彼此独立的 ad-hoc schema，也不存在旧格式 decoder/fallback。
- ContextManager 只能由 Session 根据 canonical facts 增量更新；Resume 重建一次，普通 append 不触发全量 Rollout rebuild。
- 访问嵌套目录后应用对应 AGENTS.md；AgentsMdManager/LoadedAgentsMd 是唯一 owner，stale snapshot 触发下一 Step recapture。
- Compaction request 保留 Session BaseInstructions，把独立 `SUMMARIZATION_PROMPT` 作为最后一个 synthetic User item，Tools 为空；摘要模型看到 exact PromptSnapshot 的 WorldState/AGENTS.md/Skill 与 canonical history。
- Compaction 保留目标、修改、失败和待办；ReplacementHistory 使用 typed ResponseItem、按最近真实 User message 总预算选择并以 `User(SUMMARY_PREFIX + summary)` 结束。pre-turn/manual 清空 context baseline，mid-turn 在 summary 保持最后一项的前提下安装 full initial context 和新 baseline。
- TotalTokenUsage、LastTokenUsage、ActiveContextTokens 与 EstimatedInputTokens 在多 Model Step、Tool Result、Compaction、interrupt、live 和 Resume 中保持各自语义；TokenCountEvent/replay/SQLite 不重复累加 snapshot。
- 文本 ASCII/中文、Tool Call/Result、Reasoning、ToolSpec、OutputSchema、prepared high/original image 的结构化估算有独立 fixture；图片 Base64 不按文本 token 重复计费。
- 手动 compact 与 auto pre-turn/mid-turn 使用相同 Session.runCompaction/install contract；auto 不嵌套 CompactTask，pending steer 不进入 compact request。
- source hash/version race、compaction request context overflow oldest-group trimming、stream retry/cancel、invalid summary、durable append failure 和 compact 后无进展均保持原 ContextManager 或产生唯一成功 checkpoint，不出现无限 compact。
- ModelInfo input modalities 控制不支持内容的投影，不把能力检查推迟到 Provider Adapter 报错。
- architecture guards 禁止 `TaskOutput.Usage`、Role/Content-only ReplacementMessage、Compactor 返回 RolloutItem、`compactFunc/compactCallback`、ContextCompactedEvent 第二完成协议、字符串 compaction error 判断和 TUI/replay usage 累加重新进入生产路径。

### 28.4 Tool 与 Approval

- `read/glob/grep` 的输出上限、稳定排序、截断标记和截断原因可预测。
- 每个内置 Tool 的模型指导只存在于其 ToolSpec description/schema；Provider request 中不再出现由 `toolGuidance(toolNames)` 生成的重复 developer block。
- `read/edit/write/glob/grep` 的 ToolSpec 与 Claude Code source manifest 对应，并明确删除图片/PDF/Notebook、mtime排序、multiline/output-mode 等 Amadeus 未实现能力；`edit/write` 明确要求 complete non-truncated read。
- `update_plan/request_user_input/execute_command/write_stdin/view_image`、Multi-Agent 和 MCP Resource ToolSpec 与 Codex source manifest 对应，但 schema 使用 Amadeus 实际字段；`web_search/web_fetch/read_skill/mcp_list_tools/mcp_call` 有独立 Amadeus contract fixture。
- ToolSpec 的 property descriptions、required/optional semantics、visibility variant、可选 OutputSchema 和 Strict policy 与实际 Validate/Prepare/Execute 一致；不能只对 description 做字符串快照。
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

### 28.5 TUI

- Assistant Markdown stream按ItemID隔离；retry `Reset=true`清除旧attempt source/stable run/tail，迟到或错误ItemID delta不污染当前stream，也不伪造ItemStarted。
- `MarkdownStreamCollector`在newline前不commit；半个inline code/link/list marker、未闭合fence和table row不会提前形成永久HistoryCell，completion会提交最后一个无newline尾行。
- Goldmark顶层节点source offset只保留completed stable prefix，mutable final block随setext heading、list tightness、reference link和后续delta正确重渲染；fence/list/quote内空行不成为边界，普通append不反复解析或扫描全部stable source。
- Table、fence和reference link从完整source重投影；table holdback保护未完成table不进入stable run；收缩列宽必须真实wrap cell，否则整个table降级key/value records。
- `ItemCompletedEvent`的Assistant Text为权威：无delta、delta完整、delta缺失、delta与final不一致、retry后final和interrupt各只有一个completed `AgentMarkdownCell`，live与Resume输出一致。
- `AgentMarkdownCell`保存exact source与冻结CWD，不TrimSpace；resize、Raw/Rich切换、attachment replay和`/copy`均从source派生，不从ANSI或当前process cwd反推。
- Active stream 的 stable run 与 mutable tail 都在 `TranscriptSurface` 中显示；surface以冻结的attachment range完成completion/reset replacement，stream期间的其他transcript projection延迟到replacement之后FIFO应用。
- `TranscriptSurface`用print watermark把immutable HistoryCell只提交一次到native scrollback，并用有界viewport投影mutable cells；`View()`不超过terminal height，active frame与printed history不重复。
- Rich `NO_COLOR`仍解析Markdown并移除语法marker，Raw mode保留原source；两者都不携带非法ANSI，且wide glyph、中文、halfwidth sound mark和超窄终端wrap稳定。
- Heading、emphasis、strong、strikethrough、inline code、nested list、blockquote、horizontal rule和fenced code有Goldmark AST fixture；Chroma覆盖language alias、unknown fallback、大小边界、light/dark与ANSI family。
- Assistant每条视觉行保持`• `/`  ` gutter；soft wrap、Goldmark soft/hard break、nested list continuation与代码/表格边界不会丢indent或重复prefix，code block按独立no-wrap policy投影。
- Local file link按cell CWD显示并保留line/column suffix；web/local typed destination在wrap/resize后仍对应正确visible range，终端使用可读underlined/plain fallback。
- Markdown render cache按width、render mode、palette/syntax revision和color level失效；cache hit不改变source，parse/highlight/table降级不使Turn失败。
- Slash Popup 键盘交互。
- Approval 上下键与 Enter。
- 中文输入和 Backspace。
- Working/Worked 计时和间距。
- ActiveHistoryCell 只提交一次。
- Working 只由 TurnStartedEvent/TurnCompleteEvent/TurnAbortedEvent 控制。
- retrying StreamErrorEvent复用status indicator显示`Reconnecting... n/m`与details，不生成HistoryCell、不finalize Assistant stream；下一条非retry live Event恢复此前status header。
- Replay/Resume 忽略 transient retry status；无颜色、窄终端和隐藏 status indicator 场景仍有稳定降级。
- Bubble Tea Task 返回不作为第二套 Turn 终态。
- Enter 与 Tab 在运行期间保持不同语义：Enter 提交并 steer，Tab 只 enqueue；enqueue 不调用 Runtime、不插入 UserMessageCell、不改变当前 Working/elapsed/activity。
- 多条 queued input 按 FIFO 每个 terminal 至多提交一条；InFlight/start-pending gate 阻止 terminal、admission 和键盘竞态重复发送，正常 dequeue admission 必须为 Started。
- TurnAborted/Blocked 和 queued submission rejection 将输入恢复到 composer；ErrorEvent 不提前 drain，旧 attachment generation 的 queue 不泄漏到 Resume/Clear 后的新 Thread。
- queued preview 在宽屏、窄屏和 No Color 下保持有界，不覆盖 Composer/Footer；存在 queued Plan input 时不显示 implementation popup。
- running + ordinary draft 时 Footer 显示 `tab to queue message`，窄屏降级为 `tab to queue`；queue hint 优先于固定 statusline，Plan indicator 仅在可容纳时保留，popup/overlay 与非 queueable Slash input 不显示该提示。
- Terminal 无颜色和窄宽度降级。
- Tool 展示使用真实 `TurnItem.ToolName`，不从 `action_summary` 或自然语言标题猜测工具身份。
- `read`、`grep`、`glob` 的探索树叶节点显示对应 Tool 名和必要参数；`execute_command` 显示 Codex 风格的 `Running`/`Ran` 与命令结果。
- `update_plan` 使用 Codex 风格的 Plan 展示，仅由 live `PlanUpdateEvent` 驱动，不进入普通 ToolHistoryCell，不生成通用 Tool Started/Completed activity，Resume 不恢复旧 checklist。
- Plan Mode 的 Proposed Plan 使用 `PlanDeltaEvent → completed PlanItem → ProposedPlanCell`；Turn 完成后只对 live completed Plan Item显示基础 implementation prompt，Replay 不恢复旧 Popup。
- `write`、`edit` 分别使用 Claude Code 风格展示目标路径、操作名称、变更统计和结构化 Diff 入口，不归入通用 `Ran`。
- Tool 行覆盖 queued、running、waiting approval、completed、failed、denied 和 partial 状态。
- Rich/Raw、Live/Replay 对持久化的 Completed Tool Item 生成一致的 Tool-specific HistoryCell；`PlanUpdateEvent` 等明确标记为 transient 的领域事件只保证 live Rich/Raw 一致，不伪造 replay item。
- `apply_patch` 不出现在 Tool Catalog、Event、Rollout 或 TUI 主链；生产遗留实现、注册和兼容测试直接删除，仅在设计文档中标记为遗留能力。

### 28.6 Provider

- Responses 与 Chat Completions Tool Call。
- Responses 将 `Prompt.BaseInstructions.Text` 映射为 wire `instructions`，不在 input 前插入 synthetic system message；Chat Completions 的 Base 前缀由 Dialect 映射并保持唯一。
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
- Config show/explain、strict decode、默认值、CLI/env override 与 example validation 全部使用无版本 schema；输出不包含 `version`，`version:` 输入被拒绝，测试只读取 `configs/config.yaml.example`。

## 29. 架构验收场景

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
10. 用户运行 `/compact`，Runtime 以普通 BaseInstructions + synthetic summarization User prompt 生成 typed replacement，原始 Rollout 保留，后续对话不丢目标；live 和 Resume 各只显示一个 Context compacted checkpoint。
11. 用户中断 Turn 后输入“继续”，新 Turn 能看到中断事实并重新规划。
12. `execute_command` 完成后展示命令、输出和退出码，不伪造文件 Diff 或修改归因。
13. Provider、Context 或 Tool 失败时 TUI 明确显示错误，不静默卡死。
14. `/resume` 恢复 Session 后 Proposed Plan、Compaction 和 Tool 历史语义一致，但不恢复旧 request overlay、checklist 或 implementation popup。
15. Rich TUI 在正常终端中完成完整 Runtime 流程。
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
28. response stream在部分Assistant/Reasoning/Tool Call/Plan Delta后断开并恢复时，transcript、canonical Rollout和后续Resume均不出现重复文本、重复Tool Call、重复Proposed Plan或旧attempt source/frame。
29. Root Agent 在同一 Turn 中并行 spawn 多个 read-only explorer；child 使用独立 Thread/Session/Context 和同一 workspace，完成后只注入一次 token-bounded notification，wait/send/close 的AgentStatus+LastTurn与TUI/Resume projection一致。
30. Root Turn被中断时open child继续运行；Root shutdown或close_agent后child writer、event consumer、watcher、slot和nickname全部释放。Root shutdown保留open edge供Root Resume，explicit close durable关闭edge；两种路径都不让child出现在默认`/resume`列表。
31. Regular Turn 运行期间按 Enter 提交补充信息时仍进入当前 Turn；相同状态下按 Tab 只进入 NextTurnQueue，当前 Turn 的 Session/Context/Rollout 不出现该输入。
32. 当前 Turn 正常 terminal 后 queued input 按 FIFO 每次启动一个新 Turn；aborted/blocked、submit rejection 和 attachment replacement 不会把输入静默注入错误 Turn，queued state 不出现在 Resume replay。
33. Turn 运行期间 Composer 输入可排队普通文字时，Footer 用 `tab to queue message`/`tab to queue` 临时替换固定 statusline；Plan indicator 按宽度让位，Slash/popup/overlay、空输入和 idle draft 不显示错误 hint，Tab enqueue 后固定 statusline 恢复。
34. 多步 Tool Turn 的 TotalTokenUsage 持续累计而 LastTokenUsage 只反映最近 request；大 Tool Result 进入 active suffix，auto compact 后 active context 明显下降，Resume 与 live 显示相同 context occupancy，SQLite `tokens_used` 不重复累计 snapshot。
35. 当前 Turn 的大 Tool Result 导致 mid-turn compact 时，replacement 覆盖 exact source 并继续原 model/tool continuation；compaction 无法降低 active context、source 在等待期间变化或 Provider 返回 context length error 时产生 typed failure，不重复 compact 或覆盖新事实。
36. `amadeus "inspect this project"` 启动与空 Prompt 相同的 TUI，先建立 configured active Thread 和恢复历史，再以 pending UserMessage 显示并通过正常 admission 提交；Turn 完成后 TUI 继续运行。latest/explicit Resume 时旧历史先于 initial message，Prompt 只提交一次；非 TTY 由 TUI terminal preflight 明确拒绝。
37. `go list ./internal/...` 只暴露目标 package；不存在 `agent/engine`、`agent/turn`、`agent/protocol`、`context`/`agentcontext` 路径不一致、`state`、`interface/tui`、`sandbox` 或空 `agent/plan`/`agent/task` 目录，生产与测试代码也不通过 alias/wrapper 回引旧路径。
38. Protocol、Session、ContextManager、ThreadStore、ThreadManager 和 TUI 的依赖方向与第 7 章一致：Protocol 不嵌套在 Runtime，ThreadStore 不 import Session，ThreadManager 是唯一 Session spawn owner，TUI reducer 不位于 Application domain。
39. Tool 调用仍严格保持 Normalize→Validate→Prepare→Permission/Approval→Execute；package 收敛后相同 Tool batch 顺序、read-before-write、stale check、Diff、grant、Event/Rollout 和 Resume 行为不变，generic `internal/tool` 不包含具体 Tool 名 catalog switch。
40. 责任文件拆分后 architecture guards、focused tests、全仓 functional/race tests、Provider/Core Tool E2E 和构建均通过；测试 owner 跟随新 package，单一 guard 文件不再承担跨全部架构域的检查。
41. Responses request 的 Base 只出现在 wire `instructions`，input 第一项是真实 context/history；Chat Completions 按 Dialect 只生成一个 Base 前缀，二者的 Prompt authority snapshot 有独立 fixture。
42. Fresh Thread、第二 Turn、Mode change、AGENTS.md replacement/removal、Permission grant 和 SubAgent status change 分别生成正确 role/order 的 WorldState full/diff；unchanged state 不重复注入，Resume 后下一 request 与 live 等价。
43. Thread 创建后升级内置 Prompt 资产再 Resume，仍使用 SessionMeta 中的 exact BaseInstructions/provenance；新 Thread 使用新 revision，未知模型使用 neutral fallback且不声称错误模型身份。
44. Default、Plan、Compact、Summary Prefix 和 Multi-Agent Role assets 与 pinned Codex source/manifest hash 一致；不存在 Go hard-coded Plan suffix、全局复合 revision、`toolGuidance(toolNames)` 或 request-only budget developer injection。
45. manual/pre-turn compact 后下一普通 Turn full reinject current context；mid-turn compact 的 summary 保持最后一项，full initial context 位于最后真实 User/summary 之前，安装的 WorldState/TurnContext baseline 在 live 与 Resume 中一致。
46. `read/edit/write/glob/grep` 的最终 ToolSpec 与 Claude Code source/manifest 及 Amadeus Runtime 同时一致；不会向模型声明图片/PDF/Notebook、mtime排序、multiline/output modes 等未实现能力，partial read 后 Edit/Write 的错误与 guidance一致。
47. Codex Runtime Tool 和 Amadeus-specific Tool 分别使用自己的 source matrix/schema fixture；Provider request只包含冻结 ToolSpecs，不包含第二份 Tool Markdown developer block，ToolSpec property/output/strict/visibility 与实际 handler一致。
48. Assistant response以多个delta交付时，半个Markdown token不会污染stable run；Goldmark top-level offset而非空行扫描决定stable/mutable边界，completed item最终只产生一个source-backed AgentMarkdownCell。
49. Provider重连在partial Assistant delta后发送reset并重新输出时，surface按冻结attachment range删除旧attempt source、derived render、stable run和tail；final transcript、`/copy`、Rollout replay与Resume只包含成功attempt的authoritative text。
50. Stream期间插入Warning/Tool/Approval/UserInput/diagnostic projection时保持FIFO但不切断stream run；delta缺失、乱序或与completed Text不一致时，completed item精确替换active range且不重复Assistant cell。
51. Markdown表格逐行到达、代码fence内部含空行、reference link后置定义和终端resize时，TranscriptSurface从完整source稳定重投影；中宽表格不会因“只缩width不wrap cell”被terminal截断。
52. Final Assistant cell保存原始Markdown和当时Session CWD；切换目录、attachment、Raw/Rich/NoColor或窗口宽度后重新投影不会改变link语义、丢失initial/subsequent indent或从ANSI反解source。
53. 真实Bubble Tea renderer下主frame高度始终不超过terminal height；SessionHeader、早期User/Tool和超过一屏的final Assistant全部进入native scrollback，鼠标上滚不会直接跳回启动shell命令，active frame不重复printed rows。
54. Root Turn在child仍运行时先正常completed、failed、blocked或被Interrupt，child runtime仍继续；child完成后向同一Root追加一次canonical notification，只有Root/Application shutdown或显式close停止child。
55. child先产生Tool Call前导语再在soft budget boundary finalization时，AgentStatus/notification/wait只使用`TurnCompleteEvent.last_agent_message`；hard blocked时保留exact outcome/reason，不把前导语或`result: blocked`伪装成completed交付。
56. Root依次spawn并显式close超过`max_agents`个child后仍可Resume；closed edge不恢复、不占slot，仍open的unloaded child恢复exact live/Resume status与LastTurn，并可按child ThreadID内部继续。

## 30. 最终架构结论

1. Codex 是 Amadeus 的 Thread、Session、SessionServices、Turn、Context、SessionTask、`run_turn`、Slash Command 和 TUI 架构骨架。
2. Codex 的 StepContext/ToolRouter 与 Claude Code 的 Validate/Prepare/Permission/Approval/Execute 共同构成 Amadeus Tool 调用链；Claude Code 仍是文件修改、Diff Preview 和 Permission UX 的主要行为参考。
3. 默认 Agent 使用单一 Codex 风格 Turn continuation loop；`update_plan` 按需发布 transient checklist Event，普通 Tool Call/Result 进入模型历史，checklist 不参与 Resume 恢复。
4. `/plan` 是显式只规划不实施的 Collaboration Mode，复用同一 RegularTask、`run_turn`、Context 和 Event/Rollout 主链；`/plan <task>` 原子应用 mode override，正式方案使用 `<proposed_plan>`、PlanDeltaEvent 和 completed PlanItem。
5. `request_user_input` 是 Default/Plan 通用的独立交互 Tool；两种模式共享 Tool/Event/waiter/answer lifecycle，只由 Prompt 决定提问倾向，不复用 Approval 或 checklist lifecycle。
6. `/compact` 提交 CompactOp 并由 CompactTask 请求 Session.runCompaction；自动压缩由 `run_turn` inline 调用同一 Session lifecycle。SessionServices.Compaction 只生成 typed output，Session 独占 source validation、TokenUsageInfo、durable CompactedItem install、ContextManager replacement 和完成协议。
7. 结构化文件修改遵循 Read → Diff Preview → Approval → Revalidate → Atomic Apply → Verify。
8. `execute_command` 默认 Ask，经 Session 精确规则复用授权后直接在宿主执行；不解析任意命令的完整路径副作用。
9. Prompt 构造以 Codex 的 Session-owned persisted `BaseInstructions`、`Prompt`、`ResponseItem`、`ToolSpec`、`ModelMessages`、typed `WorldState` full/diff 和 Collaboration Mode 语义为唯一目标；Responses Base 使用 wire `instructions`，不保留旧 replace-key/context-prefix 装配层。
10. Amadeus 只有 Default/Plan 两个 Collaboration Mode；BaseInstructions 是二者共享的稳定层而不是第三种模式。Default/Plan/Compact request assets 使用 pinned Codex source + explicit patch manifest；Claude Code 提供文件/搜索 ToolSpec guidance，Codex 提供 Runtime/Image/Multi-Agent/MCP Resource ToolSpec骨架，Amadeus-specific Tool 以自身 Contract 为权威。任何 Tool guidance 都不附加到 Collaboration Mode。
11. MCP 以 Session-owned `MCPRuntime`、稳定的 `MCPBinding`、lazy `ToolCatalog`/`ResourceCatalog` 和 StepContext snapshot 为唯一生产主链；基础版不复制 Codex 的 OAuth、Elicitation、Plugin 和 Remote Connector 复杂度。
12. Skill 以 `SkillCatalog`、`SkillMetadata`、`SkillInjection` 和 Resource Boundary 为唯一生产主链；正文渐进式披露，references 按需读取，scripts 统一经 `execute_command`，不建立独立 Skill Executor。
13. JSONL typed RolloutItem 是完整 durable history 的唯一事实；SQLite StoredThread 只保存可重建 metadata/index，旧格式数据直接删除重建。
14. ThreadManager 是 Thread 创建和恢复入口；LiveThread → ThreadStore → LocalThreadStore 是唯一持久化链。
15. AmadeusThread 是 Interface 唯一 Runtime 句柄；TUI 通过它提交带 ID 的 Submission、等待 UserMessageAdmission、消费 Event/EventMsg，并分别以 ApprovalDecisionOp、UserInputAnswerOp 回答 ApprovalRequestEvent、RequestUserInputEvent。
16. TurnItem 是 EventMsg 和 HistoryCell 的稳定业务项；需要恢复的完成态以 EventMsgItem(ItemCompletedEvent) 原样持久化，Delta 只服务实时更新。
17. Slash Command 分为 TUI Local、Application Action 与 Core Op，不直接拥有 Runtime 或持久化状态。
18. internal Session 是 SessionTask、ActiveTurn、ContextManager、Event Delivery 和终态收尾的唯一所有者；ContextManager 在运行期增量记录，Resume 时仅重建一次。
19. 生产 SessionTask 由 Session 直接创建和执行，不反向调用 CLI/Application executor；SessionServices 直接拥有全部 capability。
20. canonical Rollout使用SessionMetaItem、ResponseItem、WorldStateItem、CompactedItem、TurnContextItem、AgentSpawnEdgeItem和EventMsgItem等typed contract；SessionMeta持久化exact BaseInstructions/provenance，WorldStateItem持久化full/patch baseline，Root-only AgentSpawnEdgeItem持久化basic multi-agent open/closed membership。
21. Provider request retry 与 response stream reconnect 是独立生命周期；Turn-scoped ModelClientSession/Core retry helper 拥有 retry 决策，TUI 只投影 typed transient StreamErrorEvent。
22. `Reconnecting... n/m` 复用 Codex 风格 status indicator，retrying 不进入 History/Rollout、不结束 Turn，下一条非 retry live Event 恢复先前 status，Resume 不重放瞬态状态。
23. StepContext 持有 immutable ToolRouter；同一 snapshot 同时提供模型 specs 与 exact ToolDefinition/MCP dispatch，不在执行时重新查询 mutable registry。
24. AgentsMdManager/LoadedAgentsMd、SkillCatalog 和 MCPRuntime 分别直接归 SessionServices 所有，不存在 generic Extension/Instruction assembly。
25. Amadeus 处于开发阶段，不提供旧配置、旧 Protocol、旧 Rollout、旧 SQLite schema 或旧 API 兼容；架构替换直接删除旧实现与兼容测试。
26. 普通用户消息只有一个 UserInputOp；无 ActiveTurn 时 admission 为 Started，Active Regular Turn 时为 Steered，显式 strict steer 使用 ExpectedTurnID 防止错误注入。
27. Steered input 由 ActiveTurn TurnState 中的 TurnInputQueue 按 FIFO 保存，在当前 Model Step、Tool 和必要 compaction continuation 后进入 canonical history 并触发同一 Turn follow-up；它不创建第二个 Turn lifecycle，也不复用 Approval 或 request_user_input。
28. Basic Multi-Agent使用Codex V1风格root-scoped AgentControl和完整child AmadeusThread/Session；AgentStatus保持Codex枚举，`run_turn → TurnCompleteEvent.last_agent_message`是final answer唯一权威，AgentTurnResult从同一terminal Event无损保留Amadeus blocked outcome/reason。child固定为read-only explorer，结合Claude Code风格Tool allowlist、独立background取消域与权限不升级原则，不引入nested SessionTask或第二agent loop，也不预留V2产品面。
29. Multi-Agent Prompt由ToolSpec delegation guidance、`ModelMessages.MultiAgent.Role.Subagent`、WorldState `<subagents>`和canonical `<subagent_notification>`分层拥有；notification/wait/CollabAgentState共享同一AgentStatus/LastTurn projection，CollabAgentToolCallItem是live TUI与Resume的唯一协作展示协议。
30. SessionID 是 Root/child tree-level correlation/ownership，ThreadID 是具体 Thread 的 registry、routing、Rollout 和 Resume identity；SQLite StoredThread 不复制 SessionID，canonical SessionID 只来自 Rollout SessionMeta。
31. Root Resume必须从canonical flat spawn-edge lifecycle只恢复open child，并校验其persisted metadata、SessionID和parent relation；显式close先durable关闭edge，Root shutdown只卸载runtime。Tool Invocation、Audit和Provider request metadata同时携带真实SessionID/ThreadID，而Multi-Agent target、Event scope、Application attachment和CLI/TUI resume始终使用ThreadID。
32. TUI Enter steer 与 Tab next-turn queue 是不同输入意图：前者立即进入唯一 UserInputOp/admission 主链，后者由 attachment-scoped TUI FIFO 暂存并在 terminal 后逐条重新使用该主链；Core 不拥有第二个用户输入 queue 或 `Queued` admission。
33. Queue hint 是 queueable Composer draft 的 transient Footer guidance，不是 StatusLineItem、footerState、HistoryCell 或 Runtime Event；它在 running draft 时优先于 passive statusline，并通过纯 footerProps layout 实现 Codex 风格完整/短文案降级。
34. Amadeus 用户配置使用唯一 versionless strict schema；代码和输出不包含顶层 Config version，旧 `version:` 文件直接拒绝且不迁移，仓库模板唯一命名为 `configs/config.yaml.example`。
35. 根 positional 参数是 Codex 风格可选 `PROMPT`；CLI 统一启动 TUI，TUI appModel 独占 pending initialUserMessage，并在 configured/snapshot/replay barrier 后复用普通 UserMessage submission/admission。当前基础范围只有 TUI Approval/UserInput overlay 和一个 SessionIo consumer。
36. 顶层 package 边界对齐 Codex：`protocol`、`contextmanager`、`threadstore`、`threadmanager` 和 `tui` 分别拥有公共 contract、模型历史、持久化、Thread runtime registry 和界面状态；不使用含义混杂的 `engine`、`state` 或 `interface` namespace。
37. TurnContext、StepContext、SessionState、SessionServices、ActiveTurn 和 RunningTask 都属于 `agent/session` owner；Go 通过同 package 责任文件表达 Codex private `session/state` module，不为类型名对齐建立人工小 package。
38. ModelClientSession 的 sampling/stream/reconnect 由窄 `agent/modelclient` package 拥有；Tool Runtime construction、Tool Event、model completion persistence 和 Plan stream lifecycle 回归 Session/Tool owner，不存在泛化 Agent Engine facade。
39. Tool package 同时对齐 Codex 外层 Router/Event 架构与 Claude Code 内层执行协议：generic `tool`、权限 `policy`、具体 `tool/builtin` 和 TUI projection 各有单一职责，基础版不复制 Claude Code 的一 Tool 一 package 目录结构。
40. Assistant Markdown TUI对齐Codex的source-backed collector/writer/render/stream-core/controller/transcript-surface/final-cell职责：每个active Assistant/Plan Item绑定controller和surface attachment range，StreamState管理stable queue/tail，Goldmark/Chroma拥有解析与高亮，completed item拥有最终文本权威。Bubble Tea适配以bounded mutable frame显示live content，以native print watermark提交immutable final history；不打印provisional stream rows，不复制Ratatui或手写Markdown parser。
41. `UserInputOp.ThreadSettings` 先完成 immutable validation，再根据 Started/Steered admission 在对应成功边界应用；rejected/cancelled input 不改变 SessionConfiguration，非法独立 settings update 产生 correlated ErrorEvent，当前 TurnContext 保持冻结。
42. 稳定 `TurnItem` 使用由 `Kind` 决定的 typed payload variant；`Payload any` 和 JSON decode 后的 `map[string]any` 不得成为领域事实模型，RawMessage 只存在于 codec envelope 或明确的不透明外部扩展边界。
43. Session 初始化失败使用 LiveThread/ThreadStore 的 discard 生命周期释放未提交 writer；正常 shutdown 才执行 durable flush + writer close。所有 attachment、child release、process waiter 和 shutdown goroutine 都必须由 owner 跟踪并在 bounded timeout 内等待或报告。
44. MetadataSync 从已 durable 的 typed append facts 生成增量 MetadataPatch；完整 Rollout 扫描只属于 Resume、显式 Rebuild 和 reconciliation，SQLite 永远不超过 JSONL durable watermark。
45. ThreadManager 关闭多个独立 Thread 时可并发发起 bounded shutdown，并分别保留 completed、submit-failed、timed-out 结果；ProcessManager、MCP、Audit 和 Store 的 cancel/close 不能在未观察完成的情况下伪装成 shutdown 已完成。
46. ContextManager/Prompt 热路径允许使用由 history/version、ModelInfo、Tool/WorldState revision 驱动的 derived cache，减少重复 normalization、token estimate 和 Prompt hash；任何缓存都不能成为第二份历史或配置事实源。
47. 当前 Amadeus 仍以 Workspace runtime 为 ThreadManager 生命周期；只有未来真实存在跨 Frontend/Workspace 的长生命周期服务时，才提升共享 capability manager，不为对齐 Codex 名称新增宽泛 `Core`、`Runtime` 或 Service Locator aggregate。
