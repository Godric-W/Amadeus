# Amadeus 架构设计

> 状态：Draft v0.4
> 创建日期：2026-07-29
> 最近修订：2026-08-03
> 输入依据：`docs/thought.md`、`../paicli-main` 当前代码、WeKnora ReAct 实现研究
> 目标语言：Go
> 产品形态：本地 Agent CLI，后续可复用同一运行时提供 Runtime API

## 1. 背景

Amadeus 的目标是实现面向真实软件工程任务的通用 Agent CLI。`../paicli-main` 提供功能面和迁移基线，但不是最终架构模板；Go 实现应在保留有价值行为的同时修正其 Agent 模式分裂、职责过度集中、Provider 重复实现和运行时耦合问题，并吸收 WeKnora 等项目中成熟的局部工程实践。

这里的“翻译”定义为：

1. 保留 PaiCLI 已有的核心能力和交互语义。
2. 不要求 Java 类与 Go 文件一一对应。
3. 优先建立可测试的稳定边界，再逐阶段迁移功能。
4. 对确认有价值的逻辑进行 Go 化重构，并在 `docs/development-progress.md` 的优化记录中追踪。

## 2. 原项目结论

### 2.1 已识别的主要能力

`paicli-main` 当前包含以下能力域：

- 三条 Agent 路径：ReAct、Plan-and-Execute、Multi-Agent。
- OpenAI-compatible LLM 调用、流式输出、工具调用和多模态消息。
- 文件、代码搜索、Shell、Web、Browser、Memory、Skill、Snapshot 等工具。
- HITL 审批、路径围栏、命令快速拒绝和审计日志。
- Prompt 分层覆盖、上下文压缩和长期记忆。
- MCP stdio/streamable HTTP、资源与 `@mention`。
- CLI、inline renderer、Lanterna TUI。
- LSP 诊断、Side-Git 快照、后台任务和 Runtime API。

### 2.2 三条核心执行路径

| 路径 | Java 入口 | 核心行为 |
|---|---|---|
| ReAct | `agent/Agent.java` | 模型响应、工具调用、工具结果回灌，循环至最终回答 |
| Plan | `agent/PlanExecuteAgent.java` | 生成计划、审阅计划、按任务依赖执行 |
| Team | `agent/AgentOrchestrator.java` | 角色分工、子 Agent 执行、结果汇总 |

三条路径共享 LLM、工具、记忆、Prompt、渲染、安全策略与取消机制。Go 版应将这些共享能力下沉到统一 Runtime，而不是在每种 Agent 中重复编排。

### 2.3 原项目的主要结构问题

1. `cli/Main.java` 同时承担依赖装配、启动、自检、交互循环、命令分发和模式切换。
2. `tool/ToolRegistry.java` 同时承担工具声明、注册、参数解析、策略检查、并发执行以及大量具体工具实现。
3. 多个 Provider Client 重复维护 OpenAI-compatible 请求逻辑。
4. ReAct、Plan task executor 与 SubAgent 存在相似的“请求模型—执行工具—回灌结果”循环。
5. 配置读取分散在 JSON、环境变量、`.env` 和系统属性中，优先级不够集中透明。
6. 一些模块直接依赖控制台输出或全局环境，增加单元测试和 Runtime API 复用难度。

## 3. 设计目标

### 3.1 功能目标

- 提供名为 `amadeus` 的单二进制 CLI。
- 使用 OpenAI 官方 Go SDK 作为主要模型访问实现。
- 通过配置文件切换 `base_url`、`api_key`、`model` 和 API 模式。
- 默认交付独立纯 ReAct 执行内核；用户显式输入 `/plan` 时，由外层 Plan-and-Execute 编排器拆分任务并复用同一个 Reactor，Multi-Agent 作为后续 placement 增强。
- 保持工具调用、流式输出、上下文取消、HITL 和审计能力。
- 核心运行时不依赖具体 UI，可被 CLI、TUI 和 HTTP API 复用。

### 3.2 工程目标

- 使用项目启动时选定并受依赖支持的 Go 版本；具体最低版本在创建 `go.mod` 时锁定。
- 核心包默认无全局可变状态。
- 所有外部调用接收 `context.Context`。
- Provider、Tool、Renderer、Store 均以小接口隔离。
- 单元测试不访问真实模型、网络或用户主目录。
- 敏感配置不出现在日志、错误和审计正文中。

### 3.3 非目标

- 第一阶段不追求与 Java TUI 像素级一致。
- 第一阶段不同时迁移所有 Provider 专属特例。
- 不把本地策略层描述为强隔离沙箱。
- 不在核心 Runtime 中硬编码某个模型名或供应商域名。
- 不为保持类结构相似而牺牲 Go 的组合、接口和错误处理习惯。

## 4. 总体原则

1. **行为迁移优先于代码直译**：以用户可观察行为和测试为兼容基准。
2. **Runtime 与入口解耦**：CLI/TUI/API 只负责输入输出和生命周期。
3. **SDK 隔离**：官方 SDK 被封装在 Adapter 中，业务层不依赖 SDK 类型。
4. **能力显式化**：模型是否支持图片、工具、推理字段等由 capability 描述。
5. **安全默认拒绝**：越界路径、明显危险命令和未审批危险工具默认不执行。
6. **配置可解释**：最终配置可查看来源，但密钥只显示掩码。
7. **小步可运行**：每个迁移任务都应产生可编译、可测试或可手工验收的增量。

## 5. 系统上下文

```text
User
  │
  ▼
CLI / TUI / Runtime API
  │  normalized input + cancellation
  ▼
Application Service
  │
  ├── Agent Engine ──────── LLM Adapter ───── OpenAI/OpenAI-compatible endpoint
  ├── Tool Executor ─────── Filesystem / Shell / Web / MCP / LSP / Browser
  ├── Context Manager ───── Prompt / Instructions / Conversation / Skills
  ├── Safety Pipeline ───── PathGuard / CommandGuard / HITL / Audit
  └── Typed EventHub ────── Renderer / API stream / Audit / Trace log
```

## 6. 分层架构

### 6.1 Interface 层

负责协议适配，不包含 Agent 决策逻辑。

- `cmd/amadeus`：命令行入口；根命令直接启动 Coding Agent，不设置独立 `run` 子命令。
- `internal/interface/cli`：交互循环、slash command、补全和 history。
- `internal/interface/tui`：默认 Rich Inline TUI 与 Plain fallback。
- `internal/interface/httpapi`：Conversation Session、Run 和事件流 API。
- `internal/render`：plain、inline、TUI/API event renderer。

根命令语义固定为：

| 调用 | 行为 |
|---|---|
| `amadeus` | stdin 为 TTY 时，以当前工作目录为目标项目并进入交互式 Coding Agent |
| `amadeus "<task>"` | 在当前工作目录执行一次性 Coding Agent 任务，完成后退出 |
| `amadeus --project <path>` | 以显式项目目录进入交互模式 |
| `amadeus --project <path> "<task>"` | 在显式项目目录执行一次性任务 |
| `amadeus --continue` | 恢复当前项目最近活跃的 Conversation Session |
| `amadeus --resume` | 打开当前项目 Session 选择器；取消选择则返回当前对话或 Draft Session |
| `amadeus --resume <session-id>` | 直接恢复当前项目内指定的 Conversation Session |
| `amadeus sessions list` | 列出当前项目的 Conversation Session；首版不提供 `--all` |
| `printf '%s\n' '<task>' \| amadeus` | 非 TTY 时从 stdin 读取一次性任务 |

`--help` 和已注册子命令继续按 CLI 语义处理；`version`、`config`、`tools`、`chat` 等管理/诊断入口不进入 Coding Agent。`amadeus chat` 保留为不装配工具和 Agent Engine 的纯聊天命令。无位置参数、stdin 非 TTY 且读取不到有效任务时返回明确错误。`AMADEUS_HOME` 只解析配置与用户级 `AGENTS.md`，目标项目默认来自启动工作目录，也可由 `--project` 覆盖。

交互模式使用 `/resume` 打开同一个当前项目 Session 选择器；用户按 `Esc` 时不切换 Session 并回到当前对话，因此首版不增加重复的 `/sessions` 命令。`--resume` 只表示恢复 Conversation Session，不接受 Run ID。`amadeus` 启动时只建立内存 Draft Session，`/help`、`/resume`、`/exit`、EOF 或未提交任何真实任务的进程不会写入空 Session；第一条真实用户消息到达时才原子创建 Project、Conversation Session、用户 Message 和 Run。

每条非空用户输入创建独立 Run，并为该 Run 创建可取消 Context。交互模式中的取消只终止当前 Run，父 Session 继续存在并重新接受输入；一次性模式中的取消映射为退出码 130。CLI/Application 只负责生命周期和结果映射，不以 `Turn`、`Task` 或 Provider Call 代称 Run。

### 6.2 Application 层

负责用例编排和生命周期。

- `internal/app/bootstrap`：加载配置并装配依赖。
- `internal/app/session`：会话状态、slash command 和取消。
- `internal/app/command`：本地命令处理。
- `internal/app/task`：后台任务用例。

### 6.3 Domain/Runtime 层

包含 Agent 的稳定业务模型。

- `internal/agent/react`：独立 Reactor；按 Think、Analyze、Act、Observe 组织模型/工具循环，不依赖 Plan、Graph 或 Task Domain。
- `internal/agent/plan`：显式 `/plan` 的外层编排；负责 Planner、宽容 Planning Protocol、ExecutionGraph、串行 ready-task 选择、Replanner 与 `ReActTaskExecutor`，不拥有第二套 Agent 循环。
- `internal/agent/runtime`：Provider Call 与流式事件基础设施；不得用 Turn 语义表示用户 Run。
- `internal/agent/event`：Runtime 强类型事件、Metadata Record、同步 Publisher、Fanout 与 UI/API channel subscription adapter。
- `internal/agent/team`：把独立只读 Task 委派给最多两个临时 SubAgent；SubAgent 仍复用同一个 Reactor。
- `internal/conversation`：消息、内容块和上下文压缩。
- `internal/context`：为单次 LLM 请求构建受预算约束的 ContextView。
- `internal/instruction`：用户级、项目级和目录级 `AGENTS.md` 的发现、作用域、优先级与来源追踪。
- `internal/prompt`：Prompt 分层装配。
- `internal/tool`：工具协议、注册表和执行器。

### 6.4 Infrastructure 层

负责外部系统实现。

- `internal/llm/openai`：官方 OpenAI Go SDK Adapter。
- `internal/config`：配置文件、环境变量和 CLI override 合并。
- `internal/logging`：结构化日志初始化、级别过滤和敏感属性脱敏。
- `internal/store`：SQLite/JSONL/文件存储。
- `internal/mcp`、`internal/web`、`internal/browser`、`internal/lsp`。
- `internal/skill`、`internal/snapshot`。
- `internal/policy`：路径、命令、审批和审计实现。

## 7. 建议目录结构

```text
amadeus/
├── cmd/amadeus/main.go
├── configs/amadeus.example.yaml
├── docs/
│   ├── thought.md
│   ├── design.md
│   └── development-progress.md
├── internal/
│   ├── app/
│   │   ├── bootstrap/
│   │   ├── command/
│   │   ├── session/
│   │   └── task/
│   ├── agent/
│   │   ├── engine/
│   │   ├── runtime/
│   │   ├── react/
│   │   ├── plan/
│   │   ├── reflect/
│   │   └── team/
│   ├── config/
│   ├── conversation/
│   ├── context/
│   ├── llm/
│   │   └── openai/
│   ├── tool/
│   │   ├── builtin/
│   │   └── executor/
│   ├── policy/
│   ├── prompt/
│   ├── instruction/
│   ├── mcp/
│   ├── skill/
│   ├── snapshot/
│   ├── runtimeapi/
│   ├── render/
│   ├── web/
│   ├── browser/
│   ├── image/
│   ├── lsp/
│   └── store/
├── prompts/
├── skills/
├── go.mod
└── go.sum
```

初期实现不应一次创建所有空包。目录随进度文档中的阶段逐步增加。

## 8. 核心领域模型

### 8.1 消息模型

业务层定义自己的消息类型，避免 SDK 类型向上泄漏：

```go
type Message struct {
    Role      Role
    Content   string
    Reasoning string
}
```

M1 先实现纯文本投影，角色集合为 `system/developer/user/assistant/tool`。`Reasoning` 仅承载 Adapter 归一化后的非答案推理内容，用于兼容特定 OpenAI-compatible Provider 的续轮协议；默认不进入用户可见答案，也不得写入普通审计正文。工具调用字段在 M2 接入 Tool Domain 时增加，多模态 `ContentPart` 在对应阶段扩展，避免 M1 提前绑定尚未实现的协议结构。

`Request` 只包含模型、消息、采样温度和最大输出 token，不包含 API key、base URL、Provider 扩展字段或 SDK 参数类型。Provider 方言由 Adapter/Capability 层负责。

`Response` 同时保留归一化 `FinishReason` 与原始 `ProviderFinishReason`；前者供 Runtime 做稳定决策，后者用于非敏感诊断。`Usage` 统一记录 input、cached input、output、reasoning 和 total token，厂商原始字段由 Adapter 转换。

Provider 调用错误统一进入 Domain `ProviderError`，分类为 `authentication/rate_limit/network/cancelled/timeout/invalid_request/unavailable/protocol/unknown`。错误同时保留 HTTP status、Provider code/param、非敏感 message、request ID 和可选 cause；Runtime 只依赖稳定分类决定提示、重试或取消语义，不依赖 SDK 类型或字符串匹配。`ProviderError.Error()` 不拼接 SDK 原始响应正文，避免调试字段或凭证意外进入日志。

### 8.2 LLM Port

```go
type Client interface {
    Complete(ctx context.Context, req Request) (Response, error)
    Stream(ctx context.Context, req Request) (Stream, error)
    Model() ModelInfo
    Capabilities() Capabilities
}

type Stream interface {
    Recv() (StreamChunk, error)
    Close() error
}
```

`Complete` 用于非流式或聚合后的完整响应，`Stream` 使用 `Recv` 顺序消费 `StreamChunk`，正常结束返回 `io.EOF`，调用方始终负责 `Close`。M1 的 `StreamChunk` 包含文本增量、推理增量、响应/request ID、结束原因和可选 usage；工具调用增量在 M2 扩展。

`ModelInfo` 暴露稳定的 Provider 与模型名称。`Capabilities` 显式描述 streaming、developer role、reasoning、JSON Schema、工具选择/并行调用、多模态、stream usage 和 prompt cache usage，Adapter 根据 Provider/API 方言填充，Runtime 不通过域名或模型名猜测能力。

#### 8.2.1 SDK、Adapter 与流式聚合边界

Amadeus 继续使用官方 `github.com/openai/openai-go/v3` 完成请求构造、HTTP 传输、SSE 解码和 SDK typed event 产出，不自行维护一套原始 HTTP/SSE parser。Responses 使用 `Client.Responses.NewStreaming`，Chat Completions 使用 `Client.Chat.Completions.NewStreaming`；SDK 类型只允许存在于 `internal/llm/openai`。

SDK 完成协议解码后，Amadeus Adapter 仍需承担 Domain 转换和增量聚合：

```text
HTTP / SSE bytes
    ↓ OpenAI Go SDK
SDK typed stream events / chunks
    ↓ openai.Adapter
text/reasoning/tool-argument fragment aggregation
    ↓
llm.StreamChunk / llm.ToolCall
    ↓ Reactor
```

这层自定义代码不是替代 SDK，而是把 Responses、Chat Completions 和兼容 Provider 的事件投影到稳定的 Amadeus `llm.Client/Stream` Domain。Adapter 负责：

- 按 SDK 事件类型聚合 text、reasoning、usage、finish reason 和 Tool Call fragments；
- 规范化 Provider error、request ID 和方言差异；
- 保持 Tool Call ID、名称、顺序和 fragment 归属稳定；
- 将 SDK 类型转换为 `llm.Message/StreamChunk/ToolCall`，不让 Runtime 依赖官方 SDK。

`toolCallAggregator` 只负责 fragment 完整性和调用身份，不负责 Tool Domain 的 JSON Schema。目标实现中它校验 call ID、tool name、顺序和 arguments 非空，但不得仅因聚合后的 arguments 暂时不是合法 JSON 就在 Adapter 层终止整个 Run；JSON 语法修复和 Schema 校验统一下沉到 Tool Call Normalizer。若 Provider 事件自身缺失、索引漂移或流在 Tool Call 完成前中断，仍属于不可修复的 Provider protocol error。

### 8.3 Tool 模型

```go
type Tool interface {
    Spec() Spec
    Execute(ctx context.Context, input json.RawMessage) (Result, error)
}

type Result struct {
    Text     string
    Parts    []ContentPart
    Metadata map[string]any
    Partial  bool
}

type Spec struct {
    Name             string
    Description      string
    InputSchema      json.RawMessage
    SideEffect       SideEffect
    ParallelSafe     bool
    Idempotent       bool
    ResourceStrategy ResourceStrategy
}
```

工具定义与工具实现分离；注册表只负责查找和快照，不实现具体业务。工具是否可以并行不能只由 Provider 的 `parallel_tool_calls` 决定，还必须考虑副作用、资源冲突和审批策略。例如多个只读搜索可以并行，写同一文件、写后执行命令或 Git 操作必须保持顺序。

M2-01～M2-03 已在 `internal/tool` 落地 Provider/UI 无关的 `Spec/Call/Result/Tool/Executor`，并实现并发安全 Registry。工具输入使用 JSON Schema Draft 2020-12 校验，禁止外部 `$ref`；当前受限 repair 已处理尾随逗号和字符串完整时缺失的 `}`/`]`，repair 后仍必须重新通过严格 JSON 解析和 Schema 校验。

目标 Tool Call 参数主链为：

```text
aggregated raw arguments
    ↓ strict JSON parse
valid ───────────────────────────────┐
invalid                              │
    ↓ bounded conservative repair    │
    ↓ strict JSON parse again        │
    ↓ JSON Schema validation         │
    └────────────────────────────────┘
                    ↓
normalized Tool Call
    ↓
PathGuard / CommandGuard / Approval / Audit / Execute / Replay
```

保守 repair 只允许可证明不改变业务结构的语法恢复：

- 删除 `,}` / `,]` 前的尾随逗号；
- 在所有字符串已经闭合时补齐缺失的 `}` / `]`；
- 将 JSON 字符串中的非法反斜杠 escape 转成字面量反斜杠，主要覆盖 regex/search 参数；
- 设置明确输入大小上限，repair 必须幂等，并记录非敏感 `RepairKind` 诊断。

明确禁止：

- 自动闭合被截断的字符串；
- 将空参数替换成 `{}`；
- 猜测修复单引号、未引用 key 或 `key=value`；
- string/number/array/object 之间的类型强制转换；
- 补造必填字段、Tool 名称、Task 依赖或任何业务语义；
- repair 后跳过 JSON Schema、PathGuard、CommandGuard 或 Approval。

规范化后的 arguments 必须统一用于资源计算、审批、安全检查、审计、工具执行和 assistant Tool Call replay，不能执行修复后的参数却把原始非法 JSON 回灌给 Provider。严格解析、repair 或 Schema 校验失败时不得调用工具；Act/Observe 将结构化参数错误作为 Tool Error Observation 回灌模型，允许下一轮有限纠正，并由 ProgressMonitor 阻止相同错误无限重复。

该 repair 仅属于 Tool/MCP 参数协议容错，不适用于 Planner 的业务语义。Plan-and-Execute 继续使用 `PLAN/COMPLETE` 宽容行协议和程序生成 DAG；JSON repair 不能修复错误依赖、循环图、错误 side effect 或缺失任务，因此不得作为恢复复杂 Planner JSON 的理由。

### 8.4 Session、Message 与 Run

产品和持久化语义只使用 `Project → ConversationSession → Message/Run`：

```go
type ConversationSession struct {
    ID        SessionID
    ProjectID ProjectID
    Title     string
    Status    SessionStatus
}

type Message struct {
    ID        MessageID
    SessionID SessionID
    RunID     RunID
    Sequence  int64
    Role      MessageRole
    Content   string
}

type Run struct {
    ID               RunID
    SessionID        SessionID
    Sequence         int64
    ContextFromRunID *RunID
    Objective        string
    ExecutionMode    ExecutionMode
    Status           RunStatus
    StopReason       string
    Usage            Usage
}
```

- `ConversationSession` 是可被 `/resume` 恢复和切换的长期对话；取消当前 Agent 不会结束 Session。
- `Message` 是正式的用户可见对话记录。真实用户输入在执行前持久化；只有 Run 成功完成才持久化正式 assistant Message。
- `Run` 是一条真实用户输入触发的一次完整 Agent 执行，从接收目标开始，到 `completed/interrupted/failed` 结束。
- `Run` 不是一次 Provider 请求、工具调用、ReAct 循环或 DAG Task；一次 Session 可以包含多个 Run。
- `Turn` 不再作为核心 Domain、数据库实体或生命周期使用。UI 可以说“本轮对话”，但代码和事件必须使用更具体的 `Run`、`LLMCall` 或 `Message`。

持久化 `Run` 与 Reactor 的内存执行状态属于不同层。前者记录可恢复、可审计的生命周期，后者记录当前执行的运行时进度；代码中应使用 `session.Run` 与 `react.RunState` 或更明确的名称区分，不能让两者共享含糊的 `RunState` 语义。

### 8.5 Reactor Iteration、Observation 与 Evidence

一次 `Think → Analyze → Act → Observe` 是 Reactor 主循环的一次迭代。它只属于 `internal/agent/react` 的内部运行时，不提升为持久化或跨模块核心实体：

```go
type Iteration struct {
    Index        int
    ModelCall    ModelCallSummary
    ToolCalls    []ToolCall
    Observations []Observation
    Evidence     []Evidence
    Status       IterationStatus
}

type Observation struct {
    CallID   string
    ToolName string
    Result   ToolResult
    Error    string
    Blocking bool
}

type Evidence struct {
    Kind     EvidenceKind
    Source   string
    Summary  string
    Artifact *ArtifactRef
    Verified bool
}
```

`Iteration` 取代旧的 `Step` 术语，避免与 Plan 步骤、DAG Task、工具步骤和开发进度混淆。包内可以使用 `react.Iteration/IterationResult`；Session、SQLite、ContextBuilder 和用户界面不依赖 Iteration ID，也不精确恢复某次 Iteration。

`ToolCall` 是模型在某次 Iteration 中请求的一次工具调用；`Observation` 是工具执行后的标准化结果；`Evidence` 是从 Observation、文件状态、命令结果、测试、Diff 或诊断中提炼出的可用于判断任务状态的事实。中断恢复关心的是 `CompletedWork/Evidence/PendingWork`，不是完成了多少 Iteration。

### 8.6 ExecutionGraph、Task 与 Plan Ports

`ExecutionGraph` 与 `Task` 只属于显式 `/plan` 路径：

```go
type ExecutionGraph struct {
    Version int
    Tasks   []Task
}

type Task struct {
    ID           TaskID
    Objective    string
    Dependencies []TaskID
    Status       TaskStatus
    Result       *TaskResult
}
```

Planner 不输出完整 Domain JSON，只输出最小自然语言任务列表；GraphBuilder 为任务生成稳定 ID、顺序依赖和初始状态。普通 ReAct Run 不创建 Graph、Task 或 synthetic root Task。`Task` 表示 Plan 中可独立调度的目标单元，不表示用户输入、ReAct Iteration 或工具调用。

```go
type Planner interface {
    Decide(ctx context.Context, input PlanningRequest) (PlanningDecision, error)
}

type ReActTaskExecutor interface {
    Execute(ctx context.Context, task Task, base react.Request) (TaskResult, error)
}
```

Planner 和 Reactor 初期使用同一个 `llm.Client` 与模型，但保持独立边界。Planner 在 initial 阶段产生任务列表，在 review 阶段根据 ExecutionReport 判断完成或产生下一张图；`ReActTaskExecutor` 把 Planned Task 映射为 `react.Request`。额外质量模型、自动模式选择和独立答案合成不属于 MVP 必经主链。

Reactor 只报告 `completed/stalled/blocked/failed/interrupted` 等自身终止原因，Replan 决策只存在于 Plan Controller。Provider 请求使用独立 `LLMCallID` 与 `llm_call.started/completed/failed` 事件；不得继续使用 `TurnID` 将一次模型调用伪装成用户对话轮次。

## 9. 独立 ReAct 与外层 Plan-and-Execute

本节是当前 Agent Engine 的权威设计。默认路径是独立、纯粹的 ReAct：普通输入经过 ContextBuilder 后直接进入 Reactor，不构造 ExecutionGraph，也不创建 synthetic root Task。只有用户输入 `/plan <task>` 时，Plan-and-Execute 才作为外层编排器出现，由 Planner 生成任务、Scheduler 选择任务，并通过 `ReActTaskExecutor` 调用同一个 Reactor。模式选择完全由用户显式决定，不调用额外 Router，也不进行 ReAct→Planned 自动升级。

架构原则固定为：**采用 WeKnora 的 Think→Analyze→Act→Observe 语义分层，但保留 Amadeus 面向 Coding Agent 的参数校验、Project Root、PathGuard、CommandGuard、Approval、Audit、Snapshot、Evidence、Budget 和资源感知并行。** 不复制 WeKnora 的知识库耦合、`Data interface{}` Event payload、乐观 Final Answer 回撤或无资源约束的工具并行。

### 9.1 两条显式主流程

普通输入使用默认 ReAct：

```text
User Input
    ↓
BaseContextBuilder
    ↓
Reactor
    ├── ContextWindowManager → Think
    ├── Analyze
    ├── Act
    └── Observe
    ↓
final answer
```

显式 `/plan <task>` 使用规划路径：

```text
User /plan Input
    ↓
BaseContextBuilder
    ↓
Plan Controller → Planner → PlanParser / GraphBuilder
    ↓
Scheduler → ReActTaskExecutor → Reactor
    ↓
Planner.Decide(review)
    ├── COMPLETE → final answer
    └── PLAN     → next graph
```

默认 ReAct 不调用 Planner 或 Scheduler，也不知道 `/plan` 命令的存在。ProgressMonitor 只报告 repeated action、repeated error、no progress 或 stalled；Reactor 可以注入一次恢复提示、执行一次禁用工具的总结，或以 `stalled` 结束。若当前运行本来就是 Plan 模式，则由外层 Plan Controller 将 `stalled/blocked/failed` Task Result 交给同一个 Planner 的 review 阶段；普通 ReAct 只向用户解释当前停止原因。

### 9.2 组件边界

MVP 保留以下核心组件：

1. `BaseContextBuilder` / `ContextWindowManager`：前者组装 Run 级 Prompt、`AGENTS.md`、Conversation、当前 Goal、工具定义和最近中断上下文；后者在每次 Think 前结合 Runtime Messages、ContextProfile 和 Provider Usage 生成唯一 RequestView。
2. `Reactor`：独立执行一个 Goal 的模型/工具循环；输入只包含 Goal、Messages、Tools、Budget 和可选 Execution Metadata，不依赖 Plan、Graph、TaskStatus 或 Scheduler。
3. `Thinker` / `Analyzer` / `Actor` / `Observer`：分别负责一次模型交互、纯响应分类、工具安全执行和 Observation/消息回灌；`ReactLoop` 只负责编排四阶段与终止保护。
4. `PlanController`：仅由 `/plan` 选择，持有统一 Planner、GraphBuilder、串行 Scheduler 和 Plan cycle 状态。
5. `Planner`：通过 `Decide(PlanningRequest)` 同时承担初始 Plan 与执行后 Review；二者是同一能力在不同上下文阶段的语义调用，不维护两套 LLM 规划器。
6. `PlanParser` / `GraphBuilder`：解析统一的 `PLAN/COMPLETE` 行协议，将任务目标转换为程序拥有的串行 DAG。
7. `ReActTaskExecutor`：将 Planned Task 转换为 `react.Request`，并将 `react.Result` 映射回 Task Result；这是 Plan Domain 依赖 ReAct 的唯一边界。
8. `Session Coordinator`：在第一条真实任务时创建 Session/Run，并记录 `react` 或 `planned` execution mode；Run 是持久化/取消边界，不要求 Reactor 内部存在 root Task。

bootstrap 只创建一个共享 ToolExecutor、Policy、Context、Provider、Reactor 和 Planner。默认路径直接把 Reactor 的最终正文交给 UI；Plan 路径通过带 Execution Metadata 的事件 Filter 抑制子 Task 候选正文，只由 Planner 的 `COMPLETE` 决策发布最终答案。不能通过复制两套 Runner 或两个规划模型表达阶段差异，模式差异必须停留在外层编排、Planning Phase 和事件投影。

统一规划接口为：

```go
type PlanningPhase string

const (
    PlanningInitial PlanningPhase = "initial"
    PlanningReview  PlanningPhase = "review"
)

type PlanningRequest struct {
    Phase         PlanningPhase
    Goal          string
    Messages      []llm.Message
    Workspace     WorkspaceState
    PreviousGraph *ExecutionGraph
    TaskReports   []TaskReport
    Evidence      []Evidence
    LastError     string
    Cycle         int
    Budget        BudgetState
}

type PlanningDecision struct {
    Action      PlanningAction // plan / complete
    Tasks       []TaskDraft
    FinalAnswer string
    Usage       llm.Usage
}

type Planner interface {
    Decide(context.Context, PlanningRequest) (PlanningDecision, error)
}
```

第一次调用使用 `PlanningInitial`，只允许产生 `PLAN`；一张 DAG 执行完毕或提前停止后使用 `PlanningReview`，允许产生 `COMPLETE` 或下一轮 `PLAN`。代码可以保留 `plan()` / `replan()` 私有方法增强阅读语义，但二者必须委托同一个 `Planner.Decide`。

### 9.3 统一 Planning 协议

Planner 的 canonical 输出使用自然、稳定的行协议，而不是完整 `ExecutionGraph` JSON。初始 Plan 和后续 Replan 共用相同协议：

```text
PLAN
- Inspect the target directory and relevant files
- Summarize the discovered contents for the user
```

Review 阶段确认目标已经完成时返回：

```text
COMPLETE
<直接展示给用户的最终回答>
```

解析规则保持简单：

- 第一个非空行只允许 `PLAN` 或 `COMPLETE`；`PlanningInitial` 阶段不接受没有执行事实支撑的 `COMPLETE`。
- 接受 `-`、`*` 或数字编号列表。
- 每个非空列表项只表示一个 Task Objective。
- 程序生成 `task-1`、`task-2` 等稳定 ID。
- 第一版默认 `task-N` 依赖 `task-(N-1)`，因此天然构成合法串行 DAG。
- `PLAN` 后如果只有一段非空文本而没有列表，整段文本降级为一个 Task，而不是报复杂结构化解析错误。
- `COMPLETE` 后的非空正文直接作为最终 assistant answer，不调用独立 Final Synthesizer。
- 空响应或无法识别的首行只允许一次明确格式纠正请求；第二次仍失败则返回清晰协议错误。

Planner 不再负责输出以下字段：

- `acceptance_criteria`
- `budget`
- `side_effect`
- `resources`
- `status`
- `attempts`
- `result`

这些字段要么由程序管理，要么在 MVP 中完全删除。模型输出的不确定性被限制在“任务目标文本”这一层，不再直接反序列化为复杂 Domain Struct。

### 9.4 GraphBuilder 与 Scheduler

`GraphBuilder` 将 PlanDraft 转成内部图：

```go
type PlanDraft struct {
    Tasks []string
}

type ExecutionGraph struct {
    Version int
    Tasks   []Task
}

type Task struct {
    ID           TaskID
    Objective    string
    Dependencies []TaskID
    Status       TaskStatus
    Result       *TaskResult
}

type TaskResult struct {
    Status     TaskStatus
    Summary    string
    Evidence   []Evidence
    Usage      llm.Usage
    StopReason react.StopReason
}
```

第一版 Scheduler 只做三件事：

1. 按依赖找到下一个 ready Task。
2. 串行调用 ReAct TaskExecutor。
3. 将 completed/failed/blocked/interrupted 和结果写回图。

不做 Task 级资源推断、`side_effect` 声明或并行调度。工具层已有副作用分类、审批与资源安全机制，Agent Engine 不重复建模。只有串行版本在真实任务中稳定后，才重新评估 DAG 并行。

这里的“串行 Scheduler”只表示 **Task 之间串行**，不表示所有工具调用都串行。一个 ReAct 回合中，如果模型一次返回多个 Tool Call，现有 `internal/agent/react/resource_executor.go` 仍允许安全的 Tool 级并行：只有 `ParallelSafe=true` 且副作用为 `none/read`、资源策略不是 exclusive、参数资源不冲突的调用才进入并发批次；写文件、Patch、命令执行、网络调用和 exclusive 资源调用保持串行。并发上限由 `agent.max_parallel_tools` 控制，结果按原始 Tool Call 顺序回灌。

### 9.5 独立 Reactor 与语义阶段

Reactor 自身只理解一个 Goal，不理解默认模式、`/plan`、ExecutionGraph 或 Planned Task：

```text
Goal + Messages + Tools + Budget
    ↓
Think：调用 LLM 并聚合流式响应
    ↓
Analyze：纯函数判断 final / act / retry / fail
    ↓
Act：通过工具安全流水线执行 Tool Calls
    ↓
Observe：生成 Observation/Evidence 并回灌消息
    ↓
continue until completed / stalled / blocked / failed / interrupted
```

目标接口为：

```go
type Request struct {
    Goal      string
    Messages  []llm.Message
    Tools     []tool.Spec
    Budget    Budget
    Execution ExecutionContext
}

type ExecutionContext struct {
    SessionID string
    RunID     string
    TaskID    string // 默认 ReAct 为空；Plan Task 执行时设置
}

type Result struct {
    FinalMessage llm.Message
    Iterations   []Iteration
    Evidence     []Evidence
    Usage        llm.Usage
    StopReason   StopReason
}

type Reactor interface {
    Run(context.Context, Request) (Result, error)
}
```

每轮进入 `Think` 前，ContextWindowManager 先基于 BaseEnvelope、当前 Runtime Messages、ContextProfile 和上一轮 Usage 生成 RequestView。`Think` 只消费该 RequestView，通过官方 SDK Adapter 聚合 text/reasoning/usage/tool-call fragments，并处理空响应/瞬时错误的有限重试；`Analyze` 不执行副作用，先把完整模型响应分类为 `final/act/retry/fail`，并将 Tool Calls 交给统一参数 Normalizer 执行 strict parse → conservative repair → strict parse → Schema validation；`Act` 只接收 normalized Tool Calls，并保留 Project Root、PathGuard、CommandGuard、Approval、Audit、Snapshot Hook、timeout 和资源感知并行；`Observe` 将结果或参数错误转换为结构化 Observation/Evidence、标准 assistant/tool replay 消息和 Progress Sample，供下一轮 RequestView 投影。

Reactor 可以读取、搜索、修改文件和执行命令，但所有副作用仍必须经过工具安全流水线。语义分层只重组控制流，不放松任何 Coding Agent 能力。

默认 ReAct 不再使用 `TaskOutcomeCandidateComplete` 或 `TaskOutcomeNeedsPlan`。终止结果直接表达为 `completed/stalled/blocked/failed/interrupted/budget_exhausted`；“是否 Replan”只由 Plan Controller 根据 Task Result 决定。

默认路径的 final response 直接成为本次 Run 的最终 assistant message。Plan 路径通过 `ReActTaskExecutor` 将相同 Result 映射为 `TaskResult`；测试命令、文件 diff 和工具结果作为 Evidence 交给 Planner review 阶段，由 Planner 从整个用户目标角度决定是否完成。

### 9.6 ExecutionReport 与 Replan 语义

一张 DAG 执行完毕，或某个 Task `failed/stalled/blocked` 后，Plan Controller 停止当前图并再次调用同一个 `Planner.Decide`，此时 `Phase=PlanningReview`。用户取消不触发 Replan，直接以 `interrupted` 结束当前 Run。

Planner 不接收无限增长的原始 Tool Result 和完整消息重放，而是接收有界 `ExecutionReport`：

```go
type ExecutionReport struct {
    Cycle       int
    GraphStatus GraphStatus
    Tasks       []TaskReport
    Evidence    []EvidenceSummary
    LastError   string
    Workspace   WorkspaceState
    Usage       llm.Usage
}

type TaskReport struct {
    ID         TaskID
    Objective  string
    Status     TaskStatus
    Summary    string
    StopReason string
}
```

报告至少包含：

- 原始用户目标；
- 本轮 DAG 和每个 Task Result；
- 已执行 Task 的有界摘要与关键 Evidence；
- 当前 Git status/diff 摘要；
- 最近失败、卡死、阻塞信息；
- 当前累计使用量和剩余 Run Budget。

Review 阶段返回 `COMPLETE` 时正文直接成为最终回答；返回 `PLAN` 时按同一解析规则生成下一张 DAG。Replan 不在原 DAG 上做复杂局部合并。已执行历史、Evidence 和副作用保留在本次 Run 的内存状态及有界中断摘要中；下一张 DAG 只描述“从当前工作区状态开始还要做什么”。这样既不会重放旧工具调用，也不需要让随机模型精确复刻 completed Task ID。

### 9.7 循环与终止

Plan Controller 主循环可以直接表达为：

```go
decision := planner.Decide(initialRequest)
for cycle := 1; cycle <= maxCycles; cycle++ {
    graph := graphBuilder.Build(decision.Tasks, cycle)
    execution := scheduler.Execute(graph, reactTaskExecutor)
    decision = planner.Decide(reviewRequest(goal, execution, workspace))
    if decision.Action == PlanningComplete {
        return decision.FinalAnswer
    }
}
return ErrMaxPlanCycles
```

Scheduler 正常执行完整张 DAG 后进入 review；任意 Task `failed/stalled/blocked` 时提前结束当前图并进入 review。Task `interrupted`、父 Context 取消或不可恢复的基础设施错误不继续调用 Planner。

只保留必要的终止保护：

- Context cancellation；
- Run 总 iterations/tool calls/token/duration 预算；
- 固定 `max_plan_cycles`，首版默认 8；
- 空或非法 Planner 响应的一次格式纠正重试。

这些保护用于防止无限循环，不用于追求最少模型调用。实现优先级是“任务能继续、错误可理解、流程可复现”。

### 9.8 计划交互语义

MVP 提供 `/plan <task>`，它只负责为本次 Run 选择 Plan-and-Execute，不表示计划审核，也不写入全局配置。Planner 生成任务列表后立即执行；Replan 生成下一张任务列表后也立即继续。普通输入始终使用默认 ReAct。

`/plan` 不接受空目标。一次性命令可使用 `amadeus "/plan <task>"`；交互 TUI 和 Plain 模式均识别同一语义。计划审核如果未来需要，应设计为独立交互能力，不改变本次模式选择协议。

### 9.9 明确非目标与延后能力

当前主链明确不包含自动模式 Router、默认 ReAct 到 Planned 的动态升级、默认 ReAct synthetic root Task、复杂 Planner JSON、Task 级资源声明、Task 并行 Scheduler、强制 Verifier/Reflection 双质量门或独立 Final Synthesizer。它们不得以兼容代码为理由重新进入 bootstrap。

保留的最小能力是：Plan 专属 ExecutionGraph、作为上下文事实的 Evidence、Reactor 与 Plan Controller 分层预算、显式 `/plan <task>`、以及中断时的有界 Previous Work。Plan、Task、Iteration 和 Evidence 只存在于当前 Run 内存，不建立 Checkpoint 或独立业务表。

### 9.10 实现边界

- `internal/agent/react` 独立拥有 Reactor、Iteration、Think/Analyze/Act/Observe、ProgressMonitor 和运行时消息；
- `internal/agent/plan` 独立拥有 Planner、GraphBuilder、Scheduler、Task、ExecutionReport 和 Plan Controller；
- `ReActTaskExecutor` 是 Plan 到 Reactor 的唯一适配边界；
- `internal/context` 提供 BaseContextBuilder 与 Per-Think ContextWindowManager，不依赖 Plan/Task；
- `internal/session` 只管理 Session、Message、Run、Summary 和 Previous Work，不依赖 Reactor Iteration；
- `internal/agent/event` 使用 SessionID/RunID/TaskID/Iteration/LLMCallID 关联事件；
- `internal/tool`、Policy、Approval、Audit、Snapshot 与 Provider Adapter 被两条执行路径共享。

bootstrap 只装配一个 Reactor 和一个 Planner。默认输入直接调用 Reactor；`/plan` 调用 Plan Controller。兼容代码必须逐步迁移到这些边界，不能形成第二套生产执行循环。

## 10. 后续调度扩展与 Multi-Agent

### 10.1 Multi-Agent

Multi-Agent 第一版只解决一个明确问题：当 ExecutionGraph 同时存在多个互不依赖的只读调查 Task 时，主 Agent 最多并行派出两个临时 SubAgent 收集代码库信息；SubAgent 返回 Summary/Evidence 后，由主 Agent继续修改、测试和生成最终回复。

```text
Main Agent / Supervisor
├── SubAgent 1: read-only investigation
└── SubAgent 2: read-only investigation
        ↓
structured Summary + Evidence
        ↓
Main Agent continues normal Engine execution
```

Multi-Agent 不是新的 Agent Engine，也不创建 PaiCLI 式固定 Planner/Worker/Reviewer 主链。主 Agent 是唯一 Run/ExecutionGraph 所有者；SubAgent 只是 Scheduler 可选择的 Task placement，复用同一个 Reactor、`llm.Client`、Budget、Event 和只读 Tool Pipeline。第一版不创建独立 Reviewer Agent。

#### 10.1.1 MVP 边界

- 同一 Run 最多两个 SubAgent，固定 `max_delegation_depth = 1`；SubAgent 不能继续创建 SubAgent。
- SubAgent 初期使用与主 Agent 相同的 Provider/model，不实现角色级模型选择。
- SubAgent 只注册 `read_file`、`list_dir`、`glob_files`、`grep_code`，不注册 `apply_patch`、`write_file` 或 `execute_command`。
- 所有文件修改、命令执行、审批和用户最终回答仍由主 Agent 完成。
- SubAgent 彼此不能直接发消息，不存在 Agent Team 群聊、共享 scratchpad 或长期子会话。
- SubAgent 内部消息不写正式 Conversation，只把 Task 状态、Summary、Evidence、Usage 和 stop reason 写回当前 Run 内存状态。
- 第一版不实现 Git Worktree、并行写文件、Patch 合并、跨进程 Worker、后台 Agent 或精确恢复被中断的 SubAgent。

#### 10.1.2 Task 与结果

Planner/Scheduler 只把明确标记为 `read_only` 的 ready Task 委派给 SubAgent；`mutable` 或无法确定副作用的 Task 始终由主 Agent 执行：

```go
type ExecutionKind string

const (
    ExecutionReadOnly ExecutionKind = "read_only"
    ExecutionMutable  ExecutionKind = "mutable"
)

type SubAgentTask struct {
    ID        TaskID
    Objective string
    Context   string
    Budget    Budget
}

type SubAgentResult struct {
    TaskID   TaskID
    Status   TaskStatus
    Summary  string
    Evidence []Evidence
    Usage    Usage
}
```

ContextBuilder 为 SubAgent 构造独立、最小必要 ContextView：内置 Prompt、当前 Task、适用 `AGENTS.md`、必要依赖 Evidence、Project Root 和只读工具定义。不得复制主 Agent 的完整临时 ReAct 消息链，也不得把其他无关 Task 或完整 Conversation 无预算注入。

#### 10.1.3 Placement 规则

第一版不增加 Router LLM，只使用确定性条件：

```text
至少两个 ready Task
AND Task 之间没有依赖
AND Task.execution == read_only
AND 当前 SubAgent 数量 < 2
AND 剩余 Run/Task Budget 足够
```

满足条件时使用固定大小为 2 的 bounded executor 并行执行；否则由主 Agent 串行处理。初期只通过 `/team` 为本次 Run 设置 `prefer_subagents` PlacementPolicy，普通 Run 不自动创建 SubAgent；即使用户使用 `/team`，若没有值得并行的只读 Task，也安全退化为主 Agent 单独执行，而不是为了展示 Team 强行拆分。

#### 10.1.4 返回、失败与取消

SubAgent Result 以 Task ID 稳定写回 ExecutionGraph，并将 Evidence 提供给依赖 Task 和主 Agent。SubAgent 不决定整个 Run 完成，也不直接生成正式 assistant message。一个 SubAgent 失败不会自动创建替代 Agent；主 Agent 可以自行完成该 Task、触发 Replan、忽略非关键结果或请求用户输入。

主 Run 取消时取消所有子 Context，等待有界退出并保留已经完成的 Summary/Evidence；旧 SubAgent 不恢复。用户之后输入“请继续”时仍创建新 Run，根据上一 Run 的中断摘要重新规划是否需要再次委派。

#### 10.1.5 延后能力

只有只读 SubAgent 的效果、成本和调度稳定后，才重新评估自动 placement、资源声明、隔离 Worktree、并行写任务、Reviewer SubAgent、不同模型和 Agent 间消息。这些都不属于第一版 Multi-Agent 的验收范围。

## 11. OpenAI SDK 适配设计

截至 2026-07-29，设计采用 OpenAI 官方 Go SDK，并优先面向 Responses API；对于只实现 Chat Completions 的 OpenAI-compatible 服务，通过配置选择兼容路径。最终依赖版本在实现阶段通过 `go.mod` 固定，不在架构文档中绑定易过时的版本号。

M1-03 首次实现锁定 `github.com/openai/openai-go/v3 v3.47.0`。`internal/llm/openai.NewClient` 从最终 Provider 配置显式注入 API key、base URL、每次请求尝试的 timeout 和最大 retry 次数；测试通过自定义 `http.Client` 验证实际请求 URL、Authorization header、context deadline 和请求次数，不访问公网。

配置结构检查允许 API key 暂时为空，但 SDK Client factory 属于开始模型调用前的运行期边界，因此拒绝空 API key、空或非法 base URL、非正 timeout 和负 retry。错误不得包含 API key。

Responses 纯文本请求转换将 Domain `system/developer/user/assistant` 消息按原顺序映射为 `input` message item，并映射 model、temperature 和 max output tokens。Domain `Reasoning` 不进入 Responses message content；它仅供需要 reasoning history replay 的兼容 Provider 方言使用。M1 不接受 `tool` role，避免在 Tool Domain 和 function-call output 协议完成前生成不完整请求。

转换层对空 model、空 messages、超出 `[0,2]` 的 temperature、非正 max output tokens 和未知 role 返回带字段位置的错误。HTTP 契约测试通过 `httptest.Server` 断言 `/v1/responses` 的实际 JSON 字段，不依赖公网或真实 API key。

Chat Completions 纯文本请求转换将 Domain `system/developer/user/assistant` 消息按原顺序映射为兼容模式的 `messages`，并映射 model、temperature 和最大输出 token。最大输出 token 使用兼容面更广的 `max_tokens`，暂不切换为部分新模型使用的 `max_completion_tokens`；后续由 Provider capability/dialect 层按服务能力选择扩展字段，而不是在共享转换器中根据域名或模型名猜测。

标准 Chat 请求不发送 Domain `Reasoning`，避免把 DeepSeek 等 Provider 的 `reasoning_content` 扩展泄漏到不支持该字段的服务；reasoning history replay 留给后续方言层处理。`tool` role 同样推迟到 M2 的 Tool Domain 和 tool call 配对语义完成后接入。HTTP transport fixture 断言 `/chat/completions`、消息角色与顺序、`temperature`、`max_tokens`，并确保不出现 `max_completion_tokens` 或 `reasoning_content`。

Responses 流解析使用官方 SDK `NewStreaming`，并包装为 Domain `llm.Stream`：`response.output_text.delta` 映射为 `ContentDelta`，`response.reasoning_summary_text.delta` 与 `response.reasoning_text.delta` 映射为 `ReasoningDelta`。`response.created/in_progress` 只更新内部 response ID，不向 Runtime 发布空事件。

`response.completed` 输出 `FinishReasonStop` 和完整 usage；`response.incomplete` 根据 `max_output_tokens/content_filter` 归一化为 `length/content_filter`，同时保留 Provider 原始原因。usage 映射 input、cached input、output、reasoning 和 total tokens。

`error` 与 `response.failed` 转为保留 code/param/message 的 Adapter 错误，SSE 解码或 transport 错误保留错误链；流正常耗尽返回 `io.EOF`。M1 fixture 覆盖 reasoning/text 增量、完成、截断、usage、显式错误、失败响应、损坏 JSON 和 `stream:true` 请求字段。

Chat Completions 流解析使用官方 SDK `NewStreaming`，请求显式设置 `stream_options.include_usage=true`。首个 choice 的 `delta.content` 映射为 `ContentDelta`；角色声明等无文本 chunk 被跳过。`stop/length/tool_calls/function_call/content_filter` 分别归一化为 Domain 结束原因，同时保留 Provider 原始 `finish_reason`，未知值映射为 `unknown`。

Chat 协议通常先发送带 `finish_reason` 的 choice chunk，再发送独立的空 choices usage chunk。因此 Adapter 暂存结束事件，收到 usage 后合并为一个完成 chunk，避免 Runtime 在看到结束后提前停止而丢失 token 统计。若兼容 Provider 忽略 `include_usage`，流到达 `[DONE]` 时仍返回无 usage 的完成 chunk。usage 映射 prompt、cached prompt、completion、reasoning 和 total tokens；SSE 解码或 transport 错误保留错误链，正常耗尽返回 `io.EOF`。

两种 API 模式共用 Provider 错误归一化入口。`401/403` 和认证类 code 映射为 `authentication`，`429`、quota/rate-limit code 映射为 `rate_limit`，context cancel/deadline 分别映射为 `cancelled/timeout`，`net.Error`、URL transport error 和意外 EOF 映射为 `network`，损坏 JSON/SSE 映射为 `protocol`，其他 4xx/5xx 分别映射为 `invalid_request/unavailable`。Responses 流内 `error`、`response.failed` 事件也转换为同一 Domain 类型，不再向上暴露 Adapter 私有错误。

M1-12 新增实现 Domain `llm.Client` 的 `openai.Adapter`，内部持有官方 `openaisdk.Client`，并根据 Provider `api` 配置选择 Responses 或 Chat Completions 流。`Complete` 通过同一流协议聚合 Domain `Response`，`Stream` 不向上暴露 SDK 类型。Responses 模式声明当前已实现的 developer/reasoning/stream usage/cache usage 能力；通用 Chat compatible 模式只保守声明 streaming，等待后续 capability/dialect 配置，避免根据 API 名称推断厂商扩展能力。

### 11.1 Adapter 责任

- 根据配置创建 SDK Client，注入 API key、base URL、超时和重试策略。
- 在 Domain Message 与 SDK 请求/响应之间转换。
- 把 SDK 流事件归一化为 Amadeus event。
- 处理 tool call 参数增量拼接。
- 保留 usage、request ID 和 Provider 错误分类。
- 根据 capability 决定图片、工具、并行调用和 reasoning 字段处理方式。

### 11.2 不应由 Adapter 承担

- Agent 最大步数和终止策略。
- HITL、路径保护或命令审批。
- 对话压缩、记忆检索和 Prompt 拼装。
- Renderer 输出格式。

### 11.3 API 模式

| 模式 | 用途 | 说明 |
|---|---|---|
| `responses` | OpenAI 及支持该接口的兼容服务 | 默认选择，统一文本、图片和工具事件 |
| `chat_completions` | 传统 OpenAI-compatible Provider | 兼容原 PaiCLI Provider 行为 |

不根据域名猜测 API 模式，必须由 Provider 配置或明确默认值决定。

### 11.4 Provider Dialect

API 模式解决“使用 Responses 还是 Chat Completions”，Dialect 解决“同一 API 模式下某个 Provider 与标准协议的字段差异”。OpenAI Adapter 继续复用 SDK、HTTP、SSE、错误归一化和 Domain 映射，只把确有差异的请求字段、消息回灌、流事件和 capability 判断交给 Dialect hook，不为每个厂商复制完整 Client。

M2 显式支持 `standard/openai/deepseek/qwen/glm` 方言选择。配置未指定时使用与 API 模式匹配的标准方言；不得根据 base URL、Provider 名称或模型名隐式猜测。厂商方言只实现 fixture 或官方协议能够证明的差异，例如 token 上限字段、reasoning history、tool choice/parallel tools、assistant tool call、tool result 关联字段和流式参数分片；未知扩展保持标准行为或返回明确 capability 错误。

M2-06 已实现显式 `ProviderConfig.dialect`、`AMADEUS_DIALECT`、`--dialect`、来源追踪和结构校验。内置 OpenAI 默认配置显式选择 `openai`，新建 compatible Provider 默认选择 `standard`；配置可选值固定为 `standard/openai/deepseek/qwen/glm`。`openai.Adapter` 通过 Dialect Port 提供 capability，不读取 Provider 名称、base URL 或模型名进行推断；DeepSeek/Qwen/GLM 当前只接受 `chat_completions`，具体字段差异留在 M2-08。

Domain Tool Call/Result 保留稳定的 call ID、tool name 和 JSON arguments，不包含 SDK union 类型。Responses 与 Chat Completions 的 `function_call_output`、`tool_calls`、`role=tool`、`call_id/tool_call_id` 等线协议格式差异由协议转换器与 Dialect 共同负责，Runtime 只处理归一化后的调用和结果。

M2-07 已扩展 `internal/llm` 的 `ToolDefinition/ToolCall` 与 Message/Request，并实现标准协议转换。Responses 将 assistant ToolCall 转为 `function_call` input item、Tool Result 转为 `function_call_output`；Chat Completions 将其分别转为 assistant `tool_calls` 与 `role=tool` 消息。两种协议共用工具名称、重复定义、JSON Schema object 和 call ID/arguments 校验，Domain 不暴露 SDK union 类型；流式 ToolCall 的接收与聚合留在 M2-09。

M2-08 已将 Chat 请求转换拆为标准转换与显式 Dialect hook。DeepSeek 保持标准 `max_tokens/tool_calls/role=tool` 请求形状，并拒绝回灌历史 reasoning；Qwen 将显式 reasoning 开关映射为顶层 `enable_thinking`；GLM 将其映射为 `thinking.type/clear_thinking`，仅在调用方明确保留 reasoning 时回灌 assistant `reasoning_content`。三家兼容方言不发送未由其协议确认的 OpenAI `strict` tool 字段，未知 reasoning option 返回明确错误而不静默降级。

M2-09 已在 Adapter 流边界实现 ToolCall 聚合。Responses 按 output item ID/index 聚合 `function_call_arguments`，Chat 按 `tool_calls[].index` 聚合可能分片的 call ID、name 和 arguments；完成前统一校验 call ID/name 与 JSON arguments，损坏分片返回 Domain `protocol` 错误。完成 chunk 一次性交付排序稳定的 Domain ToolCalls，DeepSeek/Qwen/GLM 的流式 `reasoning_content` 同时归一化为 `ReasoningDelta`。

## 12. 配置设计

### 12.1 配置位置

- 默认配置：`$AMADEUS_HOME/config.yaml`。
- 显式配置路径：Loader 已支持任意文件路径，后续由 CLI `--config` 接入；显式文件不存在时返回错误。
- 环境变量：适合 CI 和密钥注入。
- CLI flags：仅覆盖本次进程。

### 12.2 优先级

从高到低：

1. CLI flags。
2. 环境变量。
3. `$AMADEUS_HOME/config.yaml`。
4. 程序默认值。

每个最终字段应保留来源信息，以支持 `amadeus config explain`。

### 12.3 示例

```yaml
version: 1
default_provider: openai

providers:
  openai:
    api: responses
    dialect: openai
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    model: your-model-id
    timeout: 120s
    max_retries: 2
    temperature: 0.2
    max_output_tokens: 8192

  compatible:
    api: chat_completions
    dialect: standard
    api_key: ${COMPATIBLE_API_KEY}
    base_url: https://example.invalid/v1
    model: compatible-model

agent:
  max_iterations: 30
  max_tool_calls: 120
  max_input_tokens: 1000000
  max_output_tokens: 245760
  max_duration: 30m
  max_parallel_tools: 4

logging:
  level: info
  trace_llm: false
```

`api_key` 等 YAML 字符串值支持 `${ENV_VAR}` 引用和字面值。变量在字段级 YAML 解码前展开；变量未设置时，错误包含字段路径与变量名。若使用字面 API key，配置加载器应检查文件权限并给出安全警告；打印有效配置时统一掩码。

`agent` 不提供持久 `mode` 配置。普通输入默认使用 ReAct；`/plan <task>` 只为当前 Run 显式选择 Plan→Execute→Replan。该 slash command 是一次性执行策略选择，不是计划审核，也不会改变后续 Run 的默认行为。

配置文件不提供 `approval.enabled`、`approval.default` 或工具级 allow/deny 规则。审批属于内置安全机制，不能通过 YAML 关闭；TTY 中按固定规则询问，非 TTY 对需要审批的调用 fail closed。

`providers.<name>.max_output_tokens` 限制单次模型请求可生成的 token；`agent.max_input_tokens` 与 `agent.max_output_tokens` 分别限制整次 Run 的累计输入和输出 token。`agent.max_iterations`、`agent.max_tool_calls`、`agent.max_duration` 控制总执行预算，`agent.max_parallel_tools` 只控制同一批可并行工具的并发度，不会扩大工具调用总额度。首版默认值保持保守且显式：30 iterations、120 tool calls、1,000,000 input tokens、245,760 output tokens、30 分钟和 4 个并行工具。

仓库提供可直接通过结构校验的 `configs/amadeus.example.yaml`。该文件可复制为 `$AMADEUS_HOME/config.yaml`，默认不包含凭证或固定模型；实际运行时优先通过 `AMADEUS_API_KEY` 和 `AMADEUS_MODEL` 注入，避免把密钥提交到版本控制。

文件加载完成后，以下直接环境变量覆盖当前配置：

| 环境变量 | 覆盖字段 |
|---|---|
| `AMADEUS_PROVIDER` | `default_provider`，并决定其余变量作用的 Provider |
| `AMADEUS_API` | 当前 Provider 的 `api` |
| `AMADEUS_DIALECT` | 当前 Provider 的 `dialect` |
| `AMADEUS_API_KEY` | 当前 Provider 的 `api_key` |
| `AMADEUS_BASE_URL` | 当前 Provider 的 `base_url` |
| `AMADEUS_MODEL` | 当前 Provider 的 `model` |

直接环境变量在 `${ENV_VAR}` 文件内容展开之后应用，因此优先级高于 YAML。变量已设置为空字符串时视为显式覆盖；合法性由配置校验阶段处理。

CLI 提供以下仅对当前进程生效的持久 flags：

| Flag | 作用 |
|---|---|
| `--config` | 选择显式配置文件 |
| `--provider` | 覆盖默认 Provider |
| `--api` | 覆盖当前 Provider API 模式 |
| `--dialect` | 覆盖当前 Provider 方言 |
| `--base-url` | 覆盖当前 Provider base URL |
| `--model` | 覆盖当前 Provider model |

CLI 不提供 `--api-key`，避免密钥进入 shell history、进程列表和 CI 命令日志。API key 应通过 `${ENV_VAR}` 或 `AMADEUS_API_KEY` 提供。显式传入空 flag 值视为覆盖，最终合法性由配置校验阶段处理。

任何面向用户、日志或诊断输出的有效配置都必须先生成不可变脱敏副本。已设置的 Provider API key 统一替换为 `[REDACTED]`，不保留前缀、后缀或长度信息；未设置的 API key 保持为空。脱敏不得修改运行时持有的原始配置。

`amadeus config check` 执行完整配置主链并进行结构校验。用户级目录优先读取 `AMADEUS_HOME`，未设置时使用解析符号链接后的可执行文件所在目录；不使用当前工作目录。显式 `--config` 不依赖 `AMADEUS_HOME`。校验成功返回 0，并只输出配置路径和默认 Provider；加载或校验失败返回非零状态，错误包含字段路径但不输出 API key。使用 `go run` 开发时，可执行文件位于临时目录，如需读取仓库配置应显式设置 `AMADEUS_HOME`。

`amadeus config explain` 在同一完整配置链和结构校验之后，按稳定字段顺序输出最终有效值及来源。来源类型包括 `default`、`file`、`environment` 和 `cli`；`${ENV_VAR}` 会显示变量名和引用文件，直接环境变量显示 `AMADEUS_*` 名称，CLI 显示具体 flag。API key 在输出前统一脱敏为 `[REDACTED]`。

### 12.4 日志初始化

日志运行时由 `internal/logging` 根据最终 `logging` 配置显式创建，并作为依赖传递，不调用 `slog.SetDefault` 写入全局状态。默认使用标准库 `log/slog` 的 JSON Handler 输出结构化日志，支持 `debug`、`info`、`warn` 和 `error` 四级过滤。

日志 Handler 对 API key、Authorization、access/refresh token、client secret、password、credential 和 cookie 等敏感属性名统一输出 `[REDACTED]`，包括嵌套 group 属性。调用方仍不得把原始请求正文、响应正文或完整配置拼入日志消息字符串；结构化字段脱敏是最后一道保护，不替代边界处的数据最小化。

`trace_llm` 只表示是否允许记录 LLM 调用的非敏感诊断信息，默认关闭。即使开启，也只能记录 request ID、模型、耗时、usage、事件类型和脱敏后的错误元数据，不得记录 API key 或未经处理的 Prompt/Response 正文。

### 12.5 校验规则

- `version` 必须等于当前支持的配置版本。
- `default_provider` 不能为空，并且必须存在于 `providers`。
- Provider 名称不能为空；`api` 只接受 `responses` 或 `chat_completions`；`dialect` 只接受 `standard/openai/deepseek/qwen/glm`。
- `base_url` 必须是无 userinfo、无 fragment 的绝对 `http` 或 `https` URL。
- Provider timeout 范围为 `(0, 30m]`，重试次数为 `[0, 10]`，temperature 为 `[0, 2]`，max output tokens 为 `[1, 1_000_000]`。
- approval default 和 log level 必须属于已定义枚举；最大步骤为 `[1, 1000]`，并行工具数为 `[1, 64]`。
- 未提供 API key 时允许启动配置诊断命令，但不允许开始模型回合。
- 未配置 model 时返回明确错误，不静默选择可能变化的远端默认模型。

结构校验允许 API key 和 model 暂时为空；需要模型调用的用例应在启动回合前执行能力校验。所有配置层应用完成后再执行结构校验，确保 CLI flags 可以修正文件或环境中的值。

## 13. Prompt 架构

内置 Prompt 只定义 Amadeus 的稳定执行协议，不承载用户偏好或项目规范：

```text
base → engine_protocol → approval → runtime_context
     → instructions → skills → context_management → handoff
```

首版内置资源固定在 `prompts/`，并由同目录的 Go catalog 嵌入二进制：

```text
prompts/
├── builtin.go
├── base.md
├── reactor_protocol.md
├── planning_protocol.md
├── approval.md
├── runtime_context.md
├── instructions.md
├── skills.md
├── context_management.md
└── handoff.md
```

默认 ReAct 使用 `reactor_protocol.md` 定义工具循环、停止条件和结果表达；显式 `/plan` 额外使用 `planning_protocol.md` 定义 `PLAN/COMPLETE` 文本协议。Prompt 不要求模型输出完整 Task JSON、Verification verdict、Reflection verdict 或隐藏 reasoning。若上层已经提供组装完成的 system message，Reactor 不重复注入默认协议。

M3-03 只提供不可变资产、稳定 ID、固定层顺序和嵌入完整性；M3-04 再由 `internal/prompt` 实现 Repository、Assembler、变量校验、来源清单和最终 hash，避免把资源 catalog 与运行期组装职责混在一起。

`internal/prompt.Repository` 从只读 `fs.FS` 加载单个 Prompt 文档，统一执行路径合法性、空内容和受限变量语法检查。变量只接受 `{{variable_name}}`，名称必须为小写 snake case；不支持条件、循环、函数、文件包含或任意 Go template 执行，避免 Prompt 资产演变为隐式脚本系统。

`internal/prompt.Assembler` 接收显式有序层和变量 map，并遵循以下确定性契约：

1. 保持调用方给出的层顺序，拒绝空层和重复层。
2. 一次性报告全部缺失层，而不是只暴露第一个文件错误。
3. 汇总所有必需变量，同时拒绝缺失变量和未被任何层声明的未知变量。
4. 使用两个换行连接渲染后的层，不递归解释变量值中的 `{{...}}`。
5. 为每个来源记录 `kind/path/raw SHA-256/variables`，并对最终渲染内容计算独立 SHA-256。

最终 hash 会随层顺序或渲染值变化；来源 hash 只描述规范化后的原始 Prompt 文档。bootstrap 使用同一个 built-in Repository/Assembler 生成 Reactor 与 Planner 所需 Bundle。Repository/Assembler 不负责发现 `AGENTS.md`、拼接用户目标、保存 Conversation 或裁剪 Context，这些职责属于 Instruction Resolver、BaseContextBuilder 与 ContextWindowManager。

用户和项目通过 `AGENTS.md` 提供明确、可编辑、可审查的持久指令，不开放任意内置 Prompt 覆盖。Instruction Resolver 负责指令发现、来源清单和作用域优先级；Context Envelope 再按稳定顺序组合 Prompt Bundle、适用指令和运行期内容。具体发现与优先级规则见 16.3、16.4。

## 14. 工具体系

### 14.1 设计原则

Amadeus 采用“结构化高频工具 + 通用 Shell fallback”，不因为 Shell 可以运行 `cat`、`find`、`grep` 或重定向写文件，就删除专用文件工具：

- 结构化工具是模型读取、搜索和修改项目的主路径，提供严格 schema、Project Root/PathGuard、稳定输出、预算元数据、Evidence 和跨平台语义。
- `execute_command` 是构建、测试、Git、格式化、代码生成、项目脚本和未被专用工具覆盖操作的通用逃生舱，不作为绕过文件工具、安全策略或审批的捷径。
- 工具数量保持克制；只有高频操作确实需要更稳定输出、更细权限或更强领域语义时，才从 Shell 提升为专用工具。
- Tool 名称表达能力而非具体命令行程序；内部可以使用 ripgrep 或平台能力加速，但 fallback 必须保持同一领域结果。
- 结构化 Tool Result 必须显式报告来源、截断、partial、资源使用和副作用，不能把普通 stdout 当作完整事实。

### 14.2 内置工具分层

首个稳定工具面分为三组：

```text
Exploration
├── read_file
├── list_dir
├── glob_files
└── grep_code

Mutation
├── apply_patch
└── write_file

Execution
└── execute_command
```

交互和策略能力不强制伪装成 Provider Tool：需要用户补充信息时，Reactor 以 `blocked` 终止并给出明确问题；`request_approval` 由 Tool Pipeline 在副作用前调用 Approval Port。未来的 LSP、Web、MCP、Snapshot/Revert、Browser 和 SubAgent 作为扩展能力加入；不恢复自动长期 Memory/remember/recall 工具。

### 14.3 探索工具

- `read_file`：读取 Project Root 内 UTF-8 regular file，支持 offset/limit、总行数、字节数、partial 和稳定路径元数据；读取正文优先于 Shell `cat/head/sed`。
- `list_dir`：稳定列出目录项，提供类型、隐藏项和 entry budget 语义；优先于仅为查看目录而调用 `ls`。
- `glob_files`：按稳定 project-relative 路径发现文件，统一 ignore、symlink 和结果预算；优先于 `find` 或 Shell glob。
- `grep_code`：按文件/行号返回有界文本匹配，统一 literal/regex/case/context 和 engine 元数据；优先于直接运行 `grep/rg`。

这些工具与 Shell 有意重叠。区别不在“是否能完成”，而在专用工具可以严格限制读取范围、避免启动子进程、提供可验证 metadata，并直接进入 Context Budget、Evidence 和 Previous Work 摘要。

### 14.4 修改工具

`apply_patch` 是修改已有文件的默认工具，并支持受控的 create/update/delete operation。输入采用可版本化、确定性解析的 Patch Document；每个 update hunk 必须携带足够上下文并在当前文件唯一匹配，旧内容不匹配时返回冲突，不进行猜测式替换。执行前解析并预检整个 Patch，所有路径都必须通过 PathGuard；每个 create/update 使用同目录临时文件、同步必要内容并原子 rename，delete 只允许 regular file。跨文件中途失败必须返回明确 partial/已应用 operation，不得伪装成原子成功，Snapshot 能力按 Run/Snapshot ID 提供恢复。

M4-01 将 Patch Document v1 固定为 UTF-8 行协议。规范头为 `*** Begin Patch v1`，同时接受 `*** Begin Patch` 作为 v1 兼容入口；未知版本明确拒绝。Add 正文行使用 `+`，Delete 不允许正文，Update 至少包含一个以 `@@` 开始的 hunk，hunk 行分别以空格、`+`、`-` 表示 context/add/delete，并且必须同时包含旧内容和真实变更。单文档禁止对同一路径声明多个 operation，解析错误稳定包含 line/column；默认限制 1 MiB、128 operations、1024 hunks 和 20000 行。

```text
*** Begin Patch v1
*** Add File: docs/new.md
+new file content
*** Update File: internal/example.go
@@ target function
 old line
-old value
+new value
*** Delete File: obsolete.txt
*** End Patch
```

M4-02 的文件执行器采用“全 Patch 预检、逐 operation 提交”的边界：先验证 Document、解析并守卫全部路径、检查目标类型与大小、在内存中完成所有 hunk 的唯一匹配和新内容计算，再为 Add/Update 在目标同目录创建临时文件。首次修改前会重新校验全部目标，随后按文档顺序提交；Add/Update 通过临时文件 `Sync` 后原子 rename，Update 保留原权限和既有 CRLF/末尾换行风格，Delete 仅删除 regular file。预检冲突不会产生任何文件变化；若跨文件提交中途失败，则结果明确携带已应用 operation 和 `partial=true`，供 Tool Result、Evidence 与后续 Replan 使用。

M4-03 将 `apply_patch` 作为第七个核心工具接入 Registry。Tool Spec 使用单一必填 `patch` 字符串、`SideEffectWrite`、`ParallelSafe=false`、`Idempotent=false` 和 `ResourceModeExclusive`：Patch 内嵌多条路径，在引入可靠的 operation-level resource extraction 前必须作为全局串行屏障。Policy 在请求高风险审批前再次解析 Patch 并对全部 operation 执行 PathGuard 预检；Approval 和 Audit 沿用规范化完整参数的 SHA-256，只展示/持久化哈希而不记录 Patch 正文。成功或失败结果都返回 operation metadata；中途失败保留 `partial=true` 和已应用 operation，Observation 标记失败，Evidence 明确为未验证且指出部分副作用，取消沿 ToolExecutor 传播且提交前取消不产生写入。

`write_file` 保留，但职责收窄为创建新文件或用户/模型明确要求的整文件替换，不再作为修改已有文件的首选。参数必须显式区分 `create` 与 `replace`，默认拒绝隐式覆盖；replace 继续使用原子临时文件写入并保留权限。Prompt 和 Tool description 必须引导已有文件优先使用 `apply_patch`。

M4-04 将 `write_file` 的 `mode` 固定为必填枚举 `create|replace`。`create` 只允许目标不存在，必要时创建父目录，并通过同目录 staged file + hard link 以 no-replace 语义原子发布；并发出现同名目标时确定性失败，不覆盖任何内容。`replace` 只允许目标已经是 regular file，不创建缺失目标或父目录，使用同目录 staged file + atomic rename，并继承原文件权限。旧的无 `mode` 参数在 Tool Schema 和直接执行层都明确失败；非法 mode、create-existing、replace-missing、路径逃逸、超限和提交前取消均为零内容副作用。

首版不单独增加 `edit_file`、`create_project`、`git_status`、`git_diff`、`run_tests` 或 `format_code`：Patch 已覆盖结构化编辑，Git/测试/格式化和项目脚本继续由 `execute_command` 处理。只有后续实际使用证明需要独立权限、结构化结果或可移植行为时再拆分。

### 14.5 Shell 使用策略

模型选择顺序固定为：

1. 读取、目录浏览、文件发现和代码搜索优先使用 Exploration Tool。
2. 修改已有文件优先使用 `apply_patch`；创建或明确整文件替换才使用 `write_file`。
3. 构建、测试、Git、格式化、生成器和项目自定义 CLI 使用 `execute_command`。
4. 专用工具无法表达需求时允许 Shell fallback，但仍经过 CommandGuard、Approval、Audit、timeout、进程组取消和输出预算。

Shell 中的 `cat`、`sed`、`grep`、Python/Node 文件访问不会绕过 Project Root 和安全模型。CommandGuard 能确定性识别的只读命令可以使用只读策略；无法可靠判定的动态命令按更高风险处理，而不是假设无副作用。专用工具失败时，模型可以根据错误选择修正参数或使用 Shell，但不得为了绕过策略拒绝而改写成等价 Shell 命令。

M4-05 新增独立 `Tool Selection` Agent Prompt 层，并将相同边界写入七个 Tool Spec description：常规读取、目录、发现和搜索优先结构化工具；已有文件普通编辑使用 `apply_patch`，冲突后重新读取并构造新 Patch；`write_file` 只接受显式 `mode=create|replace` 的整文件操作；构建、测试、Git、格式化、生成器和项目脚本使用 `execute_command`。Shell 只在专用工具无法表达时 fallback，且不得通过重定向、脚本或等价命令绕过 Tool contract、PathGuard、策略拒绝或审批。失败工具调用本身是未完成 Evidence，模型必须修正或选择合法替代，不能静默宣称成功。

### 14.6 执行流水线

```text
lookup → schema validation → policy precheck → approval
       → snapshot hook → execute → post-edit hook → audit → result normalization
```

所有内置和动态工具共享该流水线。文件读取/搜索通常标记为只读且 `ParallelSafe`；Patch、整文件写入和 Shell 根据资源与副作用分类进入串行屏障。Audit 记录工具名、参数摘要/hash、目标资源、策略结论、审批结果、耗时、partial 和 outcome，不记录凭证或无限正文。

### 14.7 并发规则

- 默认只并行执行模型在同一响应中发起、且工具声明为 `ParallelSafe` 的调用。
- `apply_patch`、`write_file`、`execute_command` 和 Snapshot 恢复默认不与其他有副作用工具并行。
- 使用固定大小 worker pool，不为每次调用创建无界 goroutine。
- 一个调用失败不自动取消独立调用；上下文取消或策略拒绝除外。
- 结果按原始 tool call 顺序回灌，保证行为可复现。

## 15. 安全模型

Amadeus 的本地安全模型是策略与人工审批，不宣称进程隔离。

### 15.1 路径围栏

- 所有文件工具先把路径解析为绝对、清理后的真实路径。
- 拒绝 `..`、绝对路径外逃和符号链接逃逸。
- 项目根目录在 session 创建时固定，不随工具参数变化。
- 写入前后都验证目标，降低竞态窗口。

M2-21 已新增不可变 `project.Root`：构造时把传入目录转为绝对路径、解析根目录本身的符号链接并确认其为目录；之后所有相对路径均基于该固定根解析，不受进程后续 `chdir` 影响。Root 现阶段拒绝绝对路径和词法 `..` 外逃，并提供稳定的 project-relative 表示；针对路径内部 symlink 的真实路径围栏仍由 M3 PathGuard 完成。

M3-09 已在 `internal/project` 增加统一 `PathGuard`，补足 Root 的真实路径边界。`ResolveExisting` 解析完整软链接链、验证真实路径仍在项目根，并可要求 regular file、directory 或任意现有对象；`ResolveForWrite` 逐段检查所有已存在祖先，拒绝外逃软链接、非目录祖先和最终 symlink 写目标，新路径只允许从最后一个已验证的项目内祖先继续创建。

M2 已实现的六个 MVP 工具共享该 Guard：read/list/execute cwd 使用现有路径解析，write 使用 write-target 解析，grep 校验入口、ripgrep 候选和 Go walk 软链接，glob 在返回软链接条目前验证真实目标。M4 新增的 `apply_patch` 必须复用同一 PathGuard，并在解析完整 Patch 后、任何写入前完成所有目标路径预检。项目内部目录软链接仍可正常读写；绝对路径、`..` 以及文件或目录软链接逃逸会在读取正文、启动命令或写入副作用之前失败。该实现降低但不宣称完全消除检查与系统调用之间的 TOCTOU 竞态，未来可按平台增加 descriptor-relative/openat 强化。

M2-22～M2-25 已在 `internal/tool/builtin` 实现首批固定 Root 文件工具。`read_file` 只读取大小预算内的 UTF-8 regular file，支持零基 line offset/limit，并明确报告 partial/总行数；`write_file` 自动创建父目录，在目标同目录写临时文件并原子 rename，限制输入大小且保留已有权限；`list_dir` 按名称稳定排序，默认隐藏 dot entry，并受 entry budget 限制；`glob_files` 支持 slash glob 和 `**`，默认忽略 VCS、vendor、node_modules 和常见 build tree，输出稳定 project-relative 路径并标注截断。所有工具均使用严格 JSON 参数且不读取当前工作目录。

M2-26～M2-27 已实现 `grep_code` 的统一语义层。纯 Go fallback 稳定遍历项目文本文件，跳过 hidden/VCS/build tree、binary 和超大文件，支持 literal/regex、case sensitivity、行号、上下文和结果上限。若 `rg` 可用，则只用 `--files-with-matches` 快速筛候选文件，再由同一 Go scanner 生成最终结果，因此 fast path 与 fallback 的输出、行号、context 和 partial 语义一致；`rg` 不存在、失败或候选输出超限时自动回退，context 取消不会被回退掩盖。

M2-28～M2-30 已实现 `execute_command`。命令通过固定 `project.Root` 下的 project-relative cwd 启动，stdout/stderr 写入同一个并发安全 writer，以实际到达顺序生成 combined output；非零退出同时返回结构化 exit code 和已产生输出。每次调用使用受全局上限约束的 timeout；Unix 平台为 shell 创建独立进程组，取消或超时时终止整组，其他平台明确退化为 `exec.CommandContext` 能力。输出同时受 byte/line budget 限制，仍统计原始总字节/行数，截断、timeout 和 cancel 均返回 partial Result，供 Observation/Evidence 保留。

M2-31 已提供 `builtin.DefaultMVPOptions`、`RegisterMVP/NewMVPRegistry` 和稳定 `MVPSpecs`，集中装配 `read_file/write_file/list_dir/glob_files/grep_code/execute_command`。`amadeus tools list` 不需要读取 Provider 配置即可按稳定顺序展示六个工具的 side effect、ParallelSafe、Idempotent 和 resource mode，供用户与后续 Bootstrap 检查实际工具面。

M2-32 已把资源感知的有界并发执行器接入 ReActRunner。只有 SideEffect 为 none/read、声明 ParallelSafe 且非 exclusive 的连续调用组可以并行；argument resource strategy 从规范化 JSON pointer 提取资源键，同键调用串行，write/execute/network/unknown/exclusive 调用作为前后屏障。Worker 数受 MaxParallelTools 限制，取消后不启动剩余调用，Observation/Evidence 和 ToolResult 始终恢复为原始 model call 顺序。

Project Root 越界属于不可恢复的当前执行边界错误。`project.Root` 使用 `ErrPathOutsideRoot` 标识绝对路径、父目录逃逸和项目外路径；ToolExecutor 将其转换为 Blocking Observation，Reactor 立即停止重复调用并以 `blocked` 返回。Plan 模式由外层 review 决定是否生成新 Task，默认 ReAct 直接向用户解释边界。

### 15.2 命令策略

- 命令经 tokenizer/保守规则检查，不只依赖字符串包含判断。
- 明显破坏性命令在 HITL 前快速拒绝。
- 所有 `execute_command` 调用都进入审批，不根据“看起来只读”自动放行。
- `exec.CommandContext` 负责取消，输出按字节和行数限制。

M3-10 在 `internal/policy` 增加 CommandGuard。Guard 使用受限 shell tokenizer 识别引号、转义、环境变量前缀、管道、条件连接、重定向和多命令边界，再按每个实际 program 聚合风险，而不是对原始字符串做单一关键词包含判断。风险分为 low、moderate、high、blocked；风险级别用于审批说明和拒绝判断，但 `execute_command` 最终均固定请求审批。

只读检查命令可归为 low；构建和测试因可执行项目代码归为 moderate；文件修改、依赖安装、网络和嵌套 shell 归为 high。`sudo`/关机/磁盘工具、广泛 `rm -rf`、丢弃工作区的 git reset/clean/checkout/restore，以及 curl/wget 管道到 shell 会直接 blocked。命令替换、畸形引号、空 segment 和 NUL 均被显式识别。明显的 `../`、参数绝对路径、`~/`、`$HOME`/`${HOME}` 和项目外重定向也会在审批前拒绝；绝对程序路径及 `/tmp` 中的显式构建缓存变量仍可使用。该规则是保守的命令预检，不是 OS 级 Sandbox，脚本内部行为仍依赖用户审批与现有进程权限边界。

### 15.3 审批与审计

- Approval Request 包含工具、规范化参数 hash、风险级别和原因。
- CLI/TUI/API 提供不同 Handler，Runtime 只依赖 `ApprovalHandler`，不直接读取终端输入。
- 审计为 JSONL，记录时间、会话、工具、策略/审批结果、审批范围和耗时。
- API key、Authorization header、图片二进制和完整敏感正文必须脱敏或省略。

MVP 直接采用 PaiCLI 风格的固定分类，不新增 Sandbox、ExecutionBoundary、Policy DSL 或可配置决策矩阵：

```text
只读工具             → 直接执行
写入/命令/网络/MCP   → 请求审批
路径越界/明确危险命令 → 直接拒绝
```

固定工具分类如下：

| 工具/行为 | MVP 行为 |
|---|---|
| `read_file`、`list_dir`、`glob_files`、`grep_code` | PathGuard 通过后直接执行 |
| `write_file`、`apply_patch` | PathGuard 预检通过后请求审批 |
| `execute_command` | CommandGuard 未 blocked 时仍请求审批 |
| network side effect、`mcp_list_tools`、`mcp_call` | 默认请求审批 |
| Skill 读取说明文件 | 按只读工具执行，不额外审批 |
| Skill 引发写入、命令、网络或 MCP 调用 | 复用对应工具审批，不建立 Skill 专属审批系统 |
| Project Root 外路径、软链接逃逸 | 直接拒绝，不提供扩大路径权限的审批 |
| `sudo`、广泛 `rm -rf`、磁盘工具、远端脚本管道执行等 blocked 命令 | 直接拒绝，任何 grant 都不能绕过 |

`ToolAuthorizer` 顺序固定为：参数 schema 校验 → PathGuard preflight → CommandGuard（命令工具）→ 固定工具分类 → ApprovalHandler（如需要）→ 工具内部副作用前复检 → Tool Execute → Audit。只读工具跳过 ApprovalHandler，但不跳过参数、路径、预算、取消和审计。

TTY 审批只提供 `allow once`、`allow this tool for current session` 和 `deny`。默认 Rich Inline TUI 使用 Codex 风格三项选择器：第一项默认选中，用户通过 `↑/↓`（兼容 `j/k`）移动，按 Enter 确认，Esc 直接拒绝；`y/s/n` 继续作为无提示兼容快捷键，但不再作为面板主交互说明。删除 `always`：当前实现没有持久化的永久 Grant Store，继续展示该选项会造成错误预期。Session Grant 按工具名缓存，而不是按完整参数 hash 缓存；例如批准本 Session 的 `apply_patch` 后，后续 `apply_patch` 不再询问，但 `write_file` 和 `execute_command` 仍分别询问。每次调用仍先经过 PathGuard/CommandGuard，因此 Session Grant 不能绕过路径越界或 blocked 命令。

非 TTY 不读取 stdin，也不使用配置自动允许：所有需要审批的调用直接拒绝。配置文件删除 `approval.enabled` 与 `approval.default`，Approval 不能由用户关闭；未来若出现明确的自动化场景，再单独设计受限的非交互授权入口。

M3 已有的 canonical ApprovalRequest、参数 hash、Terminal Handler、GrantCache、ToolAuthorizer、Audit 和事件主链继续复用。当前重构只删除不必要的配置和 `always` 语义，调整 Session Grant 粒度，并把 `execute_command` 收敛为“非 blocked 也一律审批”。不实现 Docker、容器沙箱、项目外路径授权或复杂 Policy DSL。

`OpenJSONLFile` 自动建立 0700 父目录、以 append 模式打开 0600 regular file，并拒绝最终 symlink；每条记录先独立编码，再在锁内单次写入，保证并发调用仍是一行一个合法 JSON object。ToolAuthorizer 对 policy allow、policy deny、用户/default/grant 决策和 preflight/handler error 统一记录耗时；配置了 Audit Sink 时，写入失败会在 `Tool.Execute` 前 fail closed，并与原始拒绝或策略错误保留完整 error chain。Agent bootstrap 同时强制注入 ApprovalHandler 与 Audit Sink，避免真实 MVP 链静默绕过审批或审计。

## 16. 上下文与显式指令

### 16.1 上下文预算

Amadeus 必须严格区分两类 token 预算：

1. **Run Budget**：`agent.max_input_tokens/max_output_tokens` 表示整个 Run 所有 LLM 调用的累计消耗，与 steps、tool calls 和 duration 一起控制 Run 终止。
2. **Request Context Profile**：表示单次模型调用的 Context Window、输出预留、安全余量和压缩阈值，决定本轮 Request 实际可以携带多少输入。

不得再使用 Run 的累计 token 上限推导单次 Context Window。目标配置或 Provider capability 必须提供明确的 `context_window`；OpenAI-compatible Provider 不根据 URL 或模型名猜测窗口。单次有效输入上限为：

```text
effective_input_limit
    = context_window
    - output_reserve
    - safety_margin
```

```go
type ContextProfile struct {
    ContextWindow  int64
    OutputReserve  int64
    SafetyMargin   int64
    CompressAt     float64
}
```

`CompressAt` 首版使用约 80%～85% 的保守阈值，为 Provider 差异、Tool Schema 编码和估算误差留出空间。Context Window 未知时不得退回 `agent.max_input_tokens`；显式配置缺失可以使用保守 Provider 默认值或拒绝启动，但必须在 `config explain` 中显示来源。

### 16.1.1 BaseContextBuilder

当前 `ContextBuilder` 收敛为 Run 级 `BaseContextBuilder`，在用户提交真实任务后执行一次，负责组装稳定基础资产：

- 内置 System Prompt；
- 当前生效的用户级、项目级和目录级 `AGENTS.md`；
- 最新有界 Conversation Rollup Summary 与最近完整 user/assistant Message Pairs；
- 最近中断 Run 的有界摘要和重新验证结果；
- 当前用户 Goal、显式 Plan Task metadata；
- 核心 Tool Specs、Skill Index 和带来源的外部资源索引；
- Source category、path/scope、SHA-256、顺序和最终 Envelope hash。

```go
type BaseContextBuilder interface {
    Build(context.Context, BaseBuildRequest) (BaseEnvelope, error)
}
```

BaseContextBuilder 不负责 Provider Usage 反馈、ReAct 每轮 Tool Result 压缩、Session/SQLite 写入、Summary 持久化或 Run Budget 累计。它只消费调用方已经解析的 Prompt、Instructions、Conversation、Summary、Interrupted Work、Tools 和 Skills，不直接扫描数据库、项目或 `AGENTS.md`。

### 16.1.2 Per-Think ContextWindowManager

真正发送给 LLM 的上下文必须在每次 `Think` 前重新投影，而不是只在 Run 开始时检查一次：

```text
BaseEnvelope
  + Reactor Runtime Messages
  + Current Task / Plan Cycle
  + Previous Provider Usage
        ↓
ContextWindowManager.Prepare
        ↓
RequestView
        ↓
Provider
```

```go
type WindowRequest struct {
    Base          BaseEnvelope
    Runtime       []llm.Message
    Profile       ContextProfile
    PreviousUsage *llm.Usage
    LastSentCount int
}

type RequestView struct {
    Messages   []llm.Message
    Tools      []tool.Spec
    Usage      ContextUsage
    Compaction *CompactionReport
    SHA256     string
}

type ContextWindowManager interface {
    Prepare(context.Context, WindowRequest) (RequestView, error)
}
```

`RequestView` 是本轮 LLM 请求的唯一真相。禁止像 PaiCLI 历史实现一样同时维护 ShortTermMemory 和另一套真正发送给模型的 conversation history，造成“内存已经压缩、请求却没有变”的双状态源。

### 16.1.3 Token 估算与 Usage Feedback

Token 计数采用“Provider Usage 优先、Estimator 补充”的策略：

- 第一轮没有 Usage 时，对完整 Messages、Tool Specs 和协议开销做保守估算；
- 上一轮 Provider 返回 usage 后，以其 input/total token 为基线，只估算新增 assistant/tool messages 的 delta；
- Context 发生裁剪、摘要替换或 Tool Result 投影后，重新完整估算；
- Provider/Dialect 可以注入更准确的 Estimator，但非 OpenAI 模型不得把 `cl100k_base` 当成精确结果；
- Prompt cache/cached input 只影响成本和诊断，不改变 Context Window 安全判断。

```go
type TokenEstimator interface {
    EstimateText(string) int64
    EstimateMessage(llm.Message) int64
    EstimateTools([]tool.Spec) int64
}
```

当前按 UTF-8 字节比例计算的 `ConservativeEstimator` 保留为 fallback，但不能继续作为所有 Provider 的唯一计数来源。

### 16.1.4 动态优先级与原子分组

上下文不再使用不可借用的固定百分比硬分区。System、Instructions、History、Tools、Interrupted、Resources 等分类继续用于统计和诊断，但实际装配使用优先级：

| 优先级 | 内容 | 行为 |
|---|---|---|
| Pinned | System Prompt、有效 `AGENTS.md`、当前 Goal/Task、当前未完成 Tool Call/Result 协议组 | 不静默裁剪；自身超限时明确报错并指出来源 |
| High | 最近完整 Conversation Message Pairs、当前 Run 最近 Iterations/Evidence、Previous Work、当前 Plan Cycle | 优先保留 |
| Medium | 较旧 Conversation、已完成 Task 明细、较旧 Tool Results | 超限时结构化摘要或投影 |
| Low | 重复 Tool 输出、已由 Evidence 表达的日志、过时 workspace snapshot、非必要资源索引 | 首先删除或按需加载 |

未使用的分类预算可以借给其他分类；`BudgetUsage` 必须覆盖 current task、skill index、runtime replay、tool schemas、tool results 和 protocol overhead，而不只是记录 system/instructions/history/tools。

压缩单元不是单条 Message，而是原子 `MessageGroup`：

- 一个完整 user/assistant Conversation Message Pair；
- assistant Tool Calls 与所有对应 Tool Results；
- current user goal；
- system/developer pinned source；
- conversation summary 或 previous-work envelope。

不得保留孤立 Tool Result，也不得删除 Tool Result 后留下待完成的 Tool Call。当前用户消息、当前 Tool 协议组和最近完整 Message Pairs 必须优先保留。

### 16.1.5 Tool Result Context Projection

Tool 自身的分页、行数和字节上限是第一层保护；进入 LLM Context 前还需要统一的 `ToolResultContextProjector`。完整 Tool Result/Evidence 可以留在 Run 内存、Artifact 或审计摘要中，但 RequestView 只注入有界投影：

```text
tool result
    ↓
structured metadata + summary
    ↓
token-aware head/tail projection
    ↓
LLM tool-result message
```

命令输出、日志和大文件片段优先保留开头的环境/标题与结尾的错误、结论、exit code，中间使用明确 omission marker。投影按 token 预算计算，不只按字符数；投影不得修改原始 Evidence，也不得隐藏 `partial/truncated/exit_code/error` metadata。

### 16.1.6 Conversation Rollup Summary

Conversation Compaction 只改变 ContextView，不删除 SQLite 中的正式 user/assistant Messages。压缩必须按完整 user/assistant Message Pair 进行，不能留下孤立 user 或 assistant 历史。

持久化 Summary 使用有界 Rollup，而不是把 `existing summary + new summary` 无限追加：

```text
previous bounded summary
  + newly covered complete message pairs
        ↓
new bounded rollup summary
```

目标 `ConversationSummaryV2` 至少保留：

- 用户目标和明确约束；
- 已确认决策；
- 已修改/关注的路径；
- 执行过的关键命令、测试和结果；
- 已验证 Evidence；
- 未解决问题和 Pending Work。

摘要继续记录覆盖消息范围、source hash、summary hash、Provider/model/time，并明确标记为 derived data 而不是 instruction。首版可使用确定性结构化摘要；后续可增加可选 LLM summarizer，失败时回退确定性摘要或保留更多最近 Message Pairs，不能无语义保护地直接删除关键历史。

### 16.2 数据生命周期边界

Amadeus 不采用 PaiCLI 式全能 `MemoryManager`，也不在近期版本实现由模型自动推断用户偏好、项目事实或可复用 Lesson 的 Durable Memory。自动长期记忆需要额外解决误判、冲突、过期、敏感信息、检索排序和可解释删除等问题，工程成本高且对编码 Agent 的实际收益不稳定。用户希望长期生效的偏好和项目规范改由显式 `AGENTS.md` 表达。

系统必须区分以下数据：

| 数据 | 语义 | 所有者 | 持久化方式 |
|---|---|---|---|
| Conversation | 用户与 Assistant 的正式可见消息历史 | `internal/session` Conversation Store | SQLite `conversation_messages` |
| Run Working State | Reactor Iterations、Observations、Evidence、Plan Graph/Task、Usage 和 Budget | `internal/agent/react` / `internal/agent/plan` | 当前 Run 内存；终态只保存 Run 结果或有界中断摘要 |
| BaseEnvelope | 当前 Run 稳定的 Prompt、Instructions、Conversation、Summary、Interrupted Work、Tools 和来源清单 | `internal/context` BaseContextBuilder | 不单独持久化 |
| RequestView | 当前一次 Think 实际发送给 LLM 的受预算动态投影 | `internal/context` ContextWindowManager | 不单独持久化；可记录 hash/compaction diagnostics |
| ConversationSummary | 对已覆盖正式消息的有界派生 Rollup | `internal/session` Summary Store | SQLite `conversation_summaries` |
| InstructionDocument | 用户和项目主动维护的长期指令 | `internal/instruction` | `AGENTS.md` 文件 |

必须保持以下边界：

- ToolResult 首先转为 Observation/Evidence，不自动生成长期偏好或项目规则。
- Run Budget 与 Request Context Profile 属于不同 Policy；前者累计整个 Run，后者限制单次 LLM 请求，均不属于 Conversation 或 Instruction。
- Conversation Compaction 只生成 Context Summary，不删除正式 Conversation 记录。
- Summary、Interrupted Work 和 Tool Result Projection 都是 derived data，不得获得 `AGENTS.md` 或当前用户消息的指令优先级。
- ContextWindowManager 只投影 RequestView，不修改 BaseEnvelope、正式 Conversation、Run Evidence 或指令文件。
- MCP resources、Skill 和 Web 内容属于带来源的外部上下文，不能获得 `AGENTS.md` 的指令优先级。

### 16.3 `AGENTS.md` 指令层级

Amadeus 使用明确、可编辑、可版本控制的 `AGENTS.md` 代替模型推断式长期记忆：

| 层级 | 位置 | 作用域 |
|---|---|---|
| 用户级 | `$AMADEUS_HOME/AGENTS.md` | 所有 Amadeus 项目 |
| 项目根级 | `<project-root>/AGENTS.md` | 整个目标项目 |
| 目录级 | `<project-root>/<path>/AGENTS.md` | 该文件所在目录及其后代 |

`AMADEUS_HOME` 是 Amadeus 配置根，目标项目根是工具被允许操作的 `project.Root`，二者必须独立解析。默认配置和用户级指令来自前者；项目指令、代码搜索、文件修改和命令 cwd 来自后者。CLI 后续通过显式 `--project` 或启动工作目录确定目标项目，不得把 `AMADEUS_HOME` 隐式当作项目根。

每个 `AGENTS.md` 作为完整 InstructionDocument 读取，至少记录 `source/path/scope/hash/content`。文件必须是受大小预算约束的 UTF-8 文本；项目指令路径必须位于 `project.Root` 内，符号链接不得绕过路径围栏。指令中的命令示例只是上下文，不会自动执行，也不能绕过 Tool Policy、Approval 或审计。

M3-05 在 `internal/instruction` 固定首版 Domain 契约：

```go
type InstructionDocument struct {
    Source  Source
    Path    string
    Scope   Scope
    SHA256  string
    Content string
}

type Scope struct {
    Kind ScopeKind
    Path string
}
```

`Source` 只表达文档来自用户级还是项目级指令链，不表示 UI、Store 或 LLM message role。文档 `Path` 是规范化绝对来源路径；`Scope.Path` 使用可移植的项目相对正斜杠路径：用户级为空、项目根级为 `.`、目录级为 `pkg/subdir`。文档构造时验证 UTF-8、非空正文、source/scope 一致性，并以正文原始字节生成 SHA-256；后续反序列化或恢复时再次校验 hash，避免内容和 provenance 静默分离。

M3-06 的 `UserLoader` 在构造时解析并规范化 Amadeus home，固定读取 `<amadeus-home>/AGENTS.md`。默认文件预算为 64 KiB，可由显式 options 收紧；读取使用 `limit + 1` 的有界 Reader，精确区分边界内文件和超预算文件。文件不存在返回 `nil` 且不报错，表示没有用户级指令；文件一旦存在，则目录、空正文、非法 UTF-8、读取失败和超预算都属于配置错误，不允许静默降级。用户可以显式使用 `AGENTS.md` 软链接，Document provenance 记录解析后的真实来源路径；用户级文件不套用项目围栏，项目 symlink 防护由 M3-07/M3-09 负责。

M3-07 的 `ProjectLoader` 绑定单个 `project.Root`，接收规范化前的“目标目录”并生成 `.`、`pkg`、`pkg/service` 这类从根到目标的稳定目录链。每一级只查找该目录直属的 `AGENTS.md`，结果始终按项目根到最深目录排列；兄弟目录规则不会进入当前链。中间目录尚不存在时停止向下发现，但保留此前已经加载的上层指令，支持新文件所在父目录尚未完全创建的场景。

目标目录语义保持显式：M3-08 通过 `TargetKind=file|directory|command_cwd` 区分目标，文件操作使用父目录，目录操作和命令使用目标目录或 cwd，避免发现器根据文件是否存在猜测 file/dir 类型。项目指令默认同样使用每文件 64 KiB 上限；缺失文件正常跳过，已存在的空文件、非法 UTF-8、非普通文件和超预算文件明确失败。

发现器在读取前解析每一级目录和 `AGENTS.md` 的真实路径并验证仍位于 `project.Root`。指向项目外部的目录或文件软链接立即拒绝；指向项目内部的软链接允许读取，但 Document 保留项目内看到的逻辑路径和对应逻辑 Scope，使目录优先级、Resolution 校验和用户诊断保持稳定。M3-09 会把同类围栏规则扩展到所有项目文件和工具操作。

### 16.4 发现、作用域与优先级

Instruction Resolver 先读取可选的 `$AMADEUS_HOME/AGENTS.md`，再从 `project.Root` 沿目标路径逐层发现 `AGENTS.md`。目录级文件的作用域是其所在目录树；操作某个文件或目录前，必须使用覆盖该目标的完整指令链，不能只加载启动目录后忽略更深层规则。对项目根执行的命令至少应用用户级与项目根级指令；若命令 cwd 位于子目录，则继续应用从项目根到该 cwd 的目录级指令。

冲突时从高到低为：

1. Amadeus 内置安全边界、Tool Policy 和 Approval Policy。
2. 用户当前明确提出的任务要求。
3. 距离目标路径最近的目录级 `AGENTS.md`。
4. 更上层目录和项目根 `AGENTS.md`。
5. `$AMADEUS_HOME/AGENTS.md`。
6. Amadeus 默认行为。

Resolver Port 使用 `ResolveRequest{Project, TargetPath, TargetKind}`，其中目标路径必须是规范化、不可逃逸的项目相对路径，`.` 表示项目根。`TargetKind` 只接受 `file`、`directory` 和 `command_cwd`；Resolution 必须回显相同 target path/kind。`Resolution.Documents` 按“用户级 → 项目根 → 逐层目录级”从宽到窄排列；空列表是合法结果，表示当前目标没有持久指令。Resolution 校验每个 Scope 确实覆盖目标对应的有效目录、来源路径不重复、同一作用域不重复、顺序不倒置，并要求项目来源文档位于 `project.Root` 内且文件所在目录与 Scope 匹配。

M3-08 的 `LayeredResolver` 组合一个 UserLoader 和与请求同根的 ProjectLoader：先读取用户级文档，再发现项目链，最终保持 user → project root → deeper directory 的稳定顺序，因此越靠后的项目文档具有越高普通指令优先级。若 `AMADEUS_HOME` 与目标项目根恰好相同，同一路径不会作为 user/project 两份文档重复注入，而只保留更具体的项目来源。任一加载、围栏或 Domain 校验错误都会终止解析，不返回部分 Resolution。

M3-05 只定义 `Resolver` 接口和领域不变量；M3-06 已实现用户级加载，M3-07 已实现项目根与目录发现，M3-08 已完成优先级与目标感知组合。整个 `internal/instruction` 仍不依赖 CLI/TUI、Provider/LLM、Conversation 或 Store。

项目级指令因此高于用户级指令，更深目录高于更浅目录；当前用户请求可以覆盖普通工程约定，但不能绕过安全和审批。Assembler 不把多层文件静默拼成无来源文本，而是使用独立结构化包络注入，并保留稳定顺序、路径、scope 和 hash，便于诊断、缓存和审计。无法确定冲突含义时应向用户提问，而不是让模型猜测。

### 16.5 Session、Message 与 Run

持久化会话只区分以下概念：

| 概念 | 语义 | 生命周期 |
|---|---|---|
| Conversation Session | 用户可查看、切换和跨进程恢复的项目对话 | SQLite 持久化 |
| Terminal Session | 一次 `amadeus` 进程从启动到退出的交互生命周期 | 仅内存 |
| Message | 正式、用户可见的 user/assistant 对话消息 | SQLite 持久化 |
| Run | 一条真实用户输入触发的一次完整 Agent 执行 | SQLite 持久化 |
| Iteration | Reactor 内部一次 Think→Analyze→Act→Observe 循环 | 当前 Run 内存 |
| Task | `/plan` DAG 中可独立调度的目标单元 | 当前 Run 内存 |
| LLM Call | 一次 Provider 请求 | 事件/审计关联，不作为会话实体 |

`Turn` 不再作为核心 Domain 或数据库实体。产品文案可以使用“本轮对话”，但代码不能用 `TurnID` 同时表示用户输入、Run 或 LLM Call。一个真实用户 Message 对应一个新 Run；Run 完成后追加 assistant Message，中断或失败时不追加不完整 assistant Message。

Session 的恢复与 Run 的恢复是不同语义：`/resume` 和 `--resume <session-id>` 只切换 Conversation Session，不接受 Run ID。已终止 Run 永远不会重新变为 `running`；用户之后输入任何内容都会创建新 Run。

### 16.6 Conversation 与 ContextView

```go
type ConversationStore interface {
    BeginRun(ctx context.Context, input BeginRunInput) (StartedRun, error)
    FinishRun(ctx context.Context, input FinishRunInput) error
    ListCompletedMessages(ctx context.Context, sessionID ConversationSessionID) ([]MessageRecord, error)
}

type BaseContextBuilder interface {
    Build(ctx context.Context, input BaseBuildRequest) (BaseEnvelope, error)
}

type ContextWindowManager interface {
    Prepare(ctx context.Context, input WindowRequest) (RequestView, error)
}
```

Conversation History 只重建完整的成功消息对：completed Run 对应的 user Message 与 assistant Message 进入历史；interrupted/failed Run 的孤立 user Message 不作为普通历史注入，而由 Previous Work Envelope 单独表达。这样避免同一个中断目标同时以孤立 user Message 和中断摘要重复出现。

BaseContextBuilder 根据当前 Goal、完整 Conversation History、Conversation Summary、适用 `AGENTS.md`、最近 Previous Work、内置 Prompt、Tool Specs 和 Skill Index 构造 Run 级 BaseEnvelope。Reactor 每次 Think 前把 Runtime Messages 与 BaseEnvelope 交给 ContextWindowManager，得到唯一 RequestView。裁剪和压缩只改变 RequestView，不修改 SQLite 消息、Run Evidence 或指令文件。

BaseEnvelope 与 RequestView 必须保留类别和来源，使模型能区分当前用户目标、有效指令、已完成历史、派生 Summary、Previous Work 和工具结果。Conversation Summary、Previous Work、MCP Resource、Skill 与 Web 内容不得伪装为 system、`AGENTS.md` 或当前用户请求。

### 16.7 中断与 Previous Work

MVP 不恢复旧调用栈，而采用“保存有界工作摘要、重新读取现场、创建新 Run、自然重新规划”的模型：

```text
old Run interrupted/failed
  -> persist bounded previous-work summary
  -> keep Conversation Session alive
  -> receive next real user message
  -> create new Run
  -> load completed conversation pairs only
  -> revalidate workspace and AGENTS.md
  -> inject Previous Work envelope
  -> append current user message last
  -> run normal Reactor or explicit /plan path
```

Previous Work 至少包含：

```go
type PreviousWork struct {
    SourceRunID  RunID
    Objective    string
    Status       RunStatus
    StopReason   string
    CompletedWork []string
    Evidence       []string
    RelevantPaths  []string
    PendingWork    []string
    LastError      string
    Usage          UsageSummary
    Plan            *InterruptedPlanSummary
}
```

`Plan` 只在 `/plan` Run 中存在，可摘要当前 cycle、completed Tasks、pending Tasks 和 active Task。默认 ReAct 不创建 Task，也不以完成 Iteration 数量代替工作进度。旧字段 `CompletedSteps` 应迁移或映射为 `CompletedWork`。

新 Run 不恢复旧 Go 调用栈、流式响应位置、未完成 Tool Call、Approval、Plan 指针或 Reactor Iteration，也不自动重放任何副作用。构建 Previous Work 时重新检查相关文件、Git status/diff、测试是否需要重跑以及当前 `AGENTS.md`；所有写入、命令和网络行为重新经过当前 Run 的 PathGuard、CommandGuard、Approval 与 Audit。

`runs.context_from_run_id` 只表示新 Run 构建上下文时参考了哪个未完成 Run，不表示精确 continuation。Pending 生命周期保持简单：最近一个尚未被成功 Run 覆盖、且带有 Previous Work 摘要的 interrupted/failed Run 自动提供给下一 Run；下一 Run 成功后不再自动注入更早的 Pending Work。

`继续`、`请继续`、`请你继续` 等文本不经过额外 Router、正则分类或第二次模型调用。Previous Work Envelope 明确说明它是历史状态而不是当前指令：当前用户要求继续时从现状重新规划；当前用户提出新任务时优先执行最新请求。

### 16.8 不持久化的运行时状态

Plan、Task、Iteration、Tool Call、Observation、Evidence、Provider Call 和 Approval Grant 不建立独立业务表。完整 Tool Result 可以存在于当前 Run 内存、Artifact 或审计摘要中，但中断时只提炼继续工作需要的有界事实。

不保存 API key、Authorization header、隐藏 reasoning、可绕过重新审批的授权状态，也不保存可自动 Replay 的调用栈。Conversation Summary 与 Previous Work 是派生历史数据，不能覆盖当前用户输入或当前有效 `AGENTS.md`。

### 16.9 实施边界

Session Coordinator 负责 BeginRun/FinishRun、正式消息事务、Pending Previous Work 查询和 Summary 持久化；Context 组件不直接写 SQLite；Reactor 不扫描 Session Store、数据库或 `AGENTS.md`。代码迁移时应将 `StartedTurn/BeginTask/FinishTask` 收敛为 `StartedRun/BeginRun/FinishRun`，将 LLM 层 `TurnStarted/TurnCompleted/TurnID` 收敛为 `LLMCallStarted/LLMCallCompleted/LLMCallID`。

Durable Memory、自动偏好提取、MemoryRetriever、跨 Run Reflexion Lesson、Checkpoint 序列和精确 Run 恢复不进入当前路线图。只有真实使用证明 Conversation、`AGENTS.md`、Summary 与 Previous Work 无法满足明确需求时，才通过独立 ADR 重新评估。

## 17. MCP、Skill 与扩展

### 17.1 MCP

- 用户级配置固定为 `$AMADEUS_HOME/mcp.yaml`，项目级配置固定为 `<project>/.amadeus/mcp.yaml`；项目同名 server 整体覆盖用户 server，不做 command/args/env/headers 字段级混合。
- 配置只定义 server name、transport、command/args/env 或 url/headers、timeout 和 enabled；字符串支持环境变量展开，凭证不进入日志、TUI、Audit 或 `config explain`。
- 使用成熟 Go MCP Client Library 承担协议细节，借鉴 WeKnora 的 Client/Manager 边界，但不迁移其 GORM、Tenant、HTTP Handler、Redis、跨实例 Approval 和数据库服务层。
- MVP 首批实现 stdio、streamable HTTP、initialize、tools/list 和 tools/call；resources、prompts、sampling、notifications、mentions 和图片后置。
- server 默认懒启动：启动 Amadeus 时只加载和校验配置，第一次需要该 server 时连接并 initialize；单 server 失败不阻止 Agent 或其他 server 工作。
- 当前生产 CLI 为每个 Run 创建 MCP Manager，并在 Run 内复用已建立连接；Run 结束时关闭进程/连接。一次调用失败允许一次有界重连，不做无限后台重试。跨交互 Session/Run 复用连接属于后续优化。
- 生产工具面固定注册 `mcp_list_tools(server)` 与 `mcp_call(server, tool, arguments)` 两个 lazy gateway。启动 Amadeus 和开始 Run 时不连接 server，也不执行 `tools/list`；第一次调用 gateway 时才启动目标 server、initialize 并按需查询工具。
- `mcp_list_tools` 返回清理后的有界工具元数据；`mcp_call` 在调用前验证目标工具已由该 server 的 `tools/list` 声明，再执行远端调用。两者均为 `ParallelSafe=false`、network side effect，并进入现有 Validation → Approval → Audit → Execute 主链。
- 旧的 `mcp__{server}__{tool}` Tool Adapter 与原子 Registry 替换能力保留为基础设施，但不是当前生产 CLI 入口。MCP 描述和 server 返回值不能声明自己无需审批。
- MVP 只回灌有界文本结果；`isError` 转为失败 Tool Result，超长结果标记 partial。正文增加“外部不可信数据”来源包络，不能伪装成 system、`AGENTS.md`、Skill 或当前用户指令。

### 17.2 Skill

- 用户级 Skill 固定放在 `$AMADEUS_HOME/skills/<name>/SKILL.md`，项目级 Skill 固定放在 `<project>/.amadeus/skills/<name>/SKILL.md`；项目同名 Skill 整体覆盖用户 Skill。MVP 不额外设计用户根目录，也不要求内置 Skill 层。
- Skill MVP 只支持 `SKILL.md` 与可选 `references/`，不正式支持 `scripts/`、`execute_skill_script`、Docker 或 Skill Sandbox。
- `SKILL.md` 使用 YAML frontmatter，MVP 只接受必填 `name` 和 `description`；name 为小写字母、数字和连字符，正文、描述、Skill 数量和索引总大小均受预算限制。
- 启动时只扫描并注入 `name + description + source` 索引，不把所有正文塞入 Prompt。模型匹配任务后调用只读内置工具 `load_skill(name)`，将正文写入一次性 SkillContextBuffer，下一次模型请求消费后清空。
- SkillContextBuffer 按 Skill name 去重、数量和总字节有界；注入内容带 `$AMADEUS_HOME` 或 project source，但优先级低于内置安全、当前用户任务和适用 `AGENTS.md`。
- `references/` 只能通过受 Skill Root 围栏保护的只读接口按需读取，拒绝绝对路径、`..`、软链接逃逸、binary 和超限文件；不扫描或自动注入整个资料目录。
- MVP 不实现 enable/disable Store：发现且校验成功的 Skill 默认可用，无效 Skill 记录 warning 并跳过，不阻塞 Agent 启动；真实需要出现后再增加 `/skill on/off` 和持久化 disabled 列表。
- PaiCLI 的 `scripts/` 只是由 Skill 指导模型调用普通 `execute_command`，没有独立 Script Executor 或 Sandbox。Amadeus MVP 不复制该能力；项目 Skill 未来可复用普通命令审批，用户 Skill 脚本因位于 Project Root 外必须另行设计专用边界后才能支持。
- Skill 引发的写入、命令、网络或 MCP 调用复用对应工具 Approval；Skill 文本不能关闭审批、绕过 PathGuard/CommandGuard、扩大 Project Root 或自动获得 Session Grant。

## 18. Snapshot、LSP、Browser 与图片

- Snapshot：每个 Run 使用 lazy tracker；只有首个已获授权的 write-side-effect 工具真正执行前才创建 before snapshot，一个 Run 最多创建一次。纯读取 Run 不扫描项目、不创建 snapshot；Run 完成时仅在已 Begin 的情况下记录 after manifest。当前首次写入仍使用有界的全项目 FileService snapshot，恢复操作按 Run/Snapshot ID 表达，增量快照后置。
- LSP：配置通过 `lsp.enabled/command/args/extensions/timeout` 显式启用。Process Client 在首个匹配扩展的成功写入后由 PostWrite Hook 懒启动，发布诊断或失败 Evidence；诊断失败不回滚已经成功的文件写入，Run 结束时关闭 Client。
- Browser：连接、会话、敏感页面策略和审计分离。
- 图片：本地图片先校验类型、尺寸与上限，再压缩/缩放；历史轮次只保留文本元信息。

这些能力均通过 Tool 或 Hook 接入 Runtime，不能反向依赖 CLI。

## 19. 事件与渲染

### 19.0 Typed EventHub

Amadeus 使用强类型事件，不采用 `Event{Type, Data interface{}}`。事件 Record 统一携带明确关联元数据：

```go
type Metadata struct {
    SessionID string
    RunID     string
    TaskID    string // 仅 /plan 或 SubAgent Task
    Iteration int    // 仅 Reactor 内部事件
    LLMCallID string // 仅 Provider Call 事件
    Sequence  int64
    Timestamp time.Time
}
```

`TurnID` 不进入目标事件协议。一次用户执行使用 RunID，一次 Provider 请求使用 LLMCallID，Plan 子任务使用可选 TaskID，Reactor 内部循环使用可选 Iteration 序号。任何事件不得复用同一字段表达多个生命周期。

核心事件分为：

- Run：`RunStarted`、`RunStatusChanged`、`RunCompleted`、`RunInterrupted`、`RunFailed`；
- Reactor：`IterationStarted`、`IterationCompleted`、`TextDelta`、`ReasoningDelta`；
- Provider：`LLMCallStarted`、`UsageUpdated`、`LLMCallCompleted`、`LLMCallFailed`；
- Tool：`ToolCallStarted`、`ToolCallCompleted`；
- Plan：`PlanUpdated`、`TaskStatusChanged`；
- Safety：`ApprovalRequested`、`ApprovalResolved`、`DiagnosticPublished`、`ErrorOccurred`。

Renderer、TUI、HTTP/SSE、Audit 和 Trace 订阅同一事件流。Channel 只作为 Subscriber Adapter；核心 Reactor、ToolExecutor 和 Approval 不直接以 channel 互相调用。Approval、Tool start/completed 和 Run terminal 等关键事件同步发布并传播错误，不能 fire-and-forget；TextDelta/ReasoningDelta 可以批量刷新，但不得改变最终顺序。

第一版不实现全局单例 Bus、跨进程 broker、事件重放或持久化 Event Store。迁移期间旧 `TurnStarted/TurnCompleted` 仅视为待删除的 LLM Call 兼容事件，不能继续扩散到新 API、Store 或 UI Domain。

### 19.1 默认 Bubble Tea Rich Inline TUI

当前交互模式复用 Bubble Tea、Bubbles textarea、Lip Gloss 和 Glamour，但不再使用 alternate screen 或应用内 transcript viewport。默认运行在终端主屏幕：已完成的用户消息、计划、工具结果、assistant 回答和错误通过 Bubble Tea 持久提交到终端 scrollback；底部活动区只重绘当前流式草稿、Approval/Session selector、输入框和状态栏。旧 `TerminalInteractionController`、InlineRenderer 和逐行 Approval 只保留给 `--plain`、非 TTY、`TERM=dumb` 以及一次性命令，不承担默认交互输入。

```text
终端历史：启动 Banner / 当前 project、model、session
        ↓
终端历史：用户输入、计划、assistant 完整回答、工具块、错误
        ↓
活动区：当前 assistant 流式草稿或 Approval/Session selector
        ↓
输入框与 Status Bar：历史、slash palette、phase、usage、cwd
```

当前组件边界：

- `FullscreenApplication`：名称为早期全屏实现遗留，实际承载 Rich Inline Bubble Tea Program，同时实现 `event.Sink` 和 `policy.ApprovalHandler`；通过线程安全 message bridge 接收 Agent 事件，并让工具审批阻塞在 response channel 上，不允许后台 Tool goroutine 直接读取终端。
- `fullscreenModel`：维护 Bubbles textarea、待提交 transcript entry、提交游标、当前 assistant draft、usage、phase、输入历史、Approval modal 和 Session selector；它只保存 UI 投影，不成为 Agent 或 Conversation 的事实源。完成 entry 使用 `tea.Println` 写入终端历史，活动区不重复渲染已经提交的 entry。
- `TerminalCapabilities`：检测 TTY、颜色、终端宽度和 dumb terminal；`--plain`、非 TTY 或 `TERM=dumb` 使用逐行模式。`AMADEUS_PLAIN` 已删除；`TERM` 为空只关闭颜色，不把真实 TTY 降级为 Plain。
- `SlashPalette`：提供 `/help`、`/exit`、`/clear`、`/resume`、`/status`、`/tools` 和 `/plan`；Tab 对唯一前缀补全，不注册已删除的 `/sessions`。
- `FullscreenApproval`：在全屏模型内显示 tool/risk/reason，使用 `y=once`、`s=session`、`n=deny`；Ctrl+C 取消当前 Run，Esc 拒绝本次调用。

第一版交互语义：

| 操作 | 行为 |
|---|---|
| Enter | 空闲时提交当前输入；Run 执行中将下一条普通任务或 `/plan <task>` 加入串行队列；空行不创建 Run |
| Up/Down | 浏览当前 Terminal Session 的输入历史 |
| Tab | 补全 slash command；路径补全后置 |
| Ctrl+C（Run 执行中） | 取消当前 Run，保留已产生 Evidence，回到输入状态 |
| Ctrl+C（空闲） | 清空当前输入，不退出进程 |
| Ctrl+D（空闲） | 退出交互循环 |
| Esc | 关闭 slash palette 或当前选择器；不撤销已执行副作用 |
| `/resume` | 打开当前 Project 的 Session 选择器；Esc 返回原会话 |
| 鼠标滚轮 | 由终端滚动原生 scrollback，不向 Bubble Tea 注册 mouse tracking |
| 鼠标拖拽 | 使用终端原生文本选择，不要求 Shift 修饰键 |

`/resume` 选择器使用 main-screen 内联无边框列表，不使用 RoundedBorder 或 modal 方框。标题、当前选中项和 `›` 指示符使用与状态栏 amber 一致的自适应黄色（light `#A16207` / dark `#FDE68A`），未选中项保持普通前景色，辅助按键提示使用 muted gray。用户通过 `↑/↓` 或 `j/k` 移动，Enter 恢复，Esc 取消并返回当前对话。

Bubble Tea 负责跨平台 raw mode、UTF-8 rune 输入、paste、resize 和退出恢复；Bubbles textarea 直接维护 Unicode 文本，因此中文输入不再被旧的单字节 reader 拆坏。默认不启用 Bubble Tea mouse tracking，也不启用 alternate screen，滚轮和拖拽选择均由终端原生处理。流式 assistant 草稿只显示适合当前终端高度的尾部，完成后再以完整 Markdown 写入 scrollback，避免长回答挤掉输入框。输入边界仍过滤残留的 terminal control response，但必须按 Unicode 控制码点识别 C1 响应，不能按原始 `0x9d/0x9b` 字节匹配，否则会误吞 UTF-8 汉字。真实程序级回归覆盖中文 rune 退格、运行中输入排队、控制片段过滤、Ctrl+C 清空、`/exit`、主屏幕模式和启动历史提交。逐行 Plain fallback 仍共享同一个 buffered reader，避免工具审批读取丢失。

交互 TUI 必须展示但不泄露内部 reasoning：

- 计划块显示当前 Plan 轮次、Task ID、目标和 `pending/running/completed/failed/blocked` 状态；Replan 追加新轮次，不覆盖历史计划。
- 工具块显示工具名、开始/完成状态、耗时、截断标记和安全摘要，不展示完整密钥、Authorization、超长参数或隐藏 reasoning。
- assistant 正文继续流式输出；状态事件到达时先结束当前文本行，避免正文与状态行交错。
- 底部状态栏显示 `idle/planning/executing/replanning/awaiting_approval/cancelling/error`、可选当前 Task、Run iteration/tool 计数、最近 usage 和 project-relative cwd。
- Approval、取消、错误和 Run 终态都使用事件驱动，不允许工具或 Engine 直接写 stdout/stderr。

当前版本已经实现多行像素 `A` 品牌头部、version/provider/model/session 信息、主屏幕 Rich Inline 渲染、终端原生 scrollback 与文本选择、Markdown assistant 文本、多行 Unicode textarea 和运行中任务排队，但仍不实现文件树 Pane、可拖拽布局、Diff 折叠器或持久化输入历史。默认 `amadeus` 启动时显示 `draft session`，第一条任务创建正式 Session 后状态同步真实 ID；只有 `--continue`、`--resume` 或交互 `/resume` 才恢复旧 Session。输入历史只保留进程内；Session 对话历史仍由 Conversation Store 管理。FullscreenApplication、InlineRenderer 与 PlainRenderer 消费同一个 Typed EventHub，终端 UI 只能改变展示，不改变 Reactor、Plan Controller、Approval 或 Session 语义。

### 19.2 目标 Rich Inline 产品形态

首个发布前将默认 TUI 收敛为 `docs/tui.md` 所示的 Codex 风格主屏交互，但继续遵守 19.1 的终端原生 scrollback 约束，不退回 alternate screen、全屏 transcript viewport 或 mouse tracking。目标布局为：

```text
大型 Amadeus 终端 Logo

╭───────────────────────────────────────────────────╮
│ Amadeus (dev)                                     │
│ model:     GPT-TOP                                │
│ provider:  openai                                 │
│ directory: /project/root                          │
│ session:   draft                                  │
╰───────────────────────────────────────────────────╯

› 用户输入

• assistant 过程说明或最终回答

• Explored
  └ Read planner.go
    Search createPlan|replan in plan_execute.go

• Ran command
  │ go test ./internal/agent/plan
  └ ok github.com/.../internal/agent/plan

• Working (40s • esc to interrupt)

─Worked for 40s────────────────────────────────────

> 用户输入框
GPT-TOP · /project/root · main · Context 47% used · 128K window
```

这一形态只改变事件到终端的投影，不改变 Reactor、Plan、ToolExecutor、Session 或 Approval 的业务语义。

#### 19.2.1 Logo 与启动面板

`docs/Amadeus_logo.webp` 是品牌源文件，但首版不在运行时直接使用 Kitty Graphics、iTerm Inline Image 或 Sixel 协议。终端图片协议兼容性、tmux/SSH 透传和 scrollback 行为差异过大，不能成为默认 UI 前置条件。

构建期使用一次性生成器对源图裁边、阈值化并转换为 Unicode Braille 黑白点阵，把生成结果作为静态 Go 常量提交；Logo 内的 `>_` 同样使用 2×4 Braille 光栅生成，不能直接打印 `>_` 两个字符，也不能混用 `█` 块字符形成另一套笔触。Amadeus 主体与 `>_` 必须保持相同的点阵密度、边缘风格和视觉重量，并保存为两个独立矩形画布：先使用终端显示宽度计算主体最大列，再把提示符统一放到 `brandWidth + gap` 的固定起始列；宽版 gap 为 8 列，紧凑版 gap 为 4 列，禁止把提示符逐行追加到不同长度的主体行尾。`>_` 必须位于 Amadeus 图形右侧并保留明显净间距，不能与主体轮廓重叠；宽版断点由合成后 Logo 的真实最大显示宽度自动计算，紧凑版继续满足 40 列降级。至少提供：

- `wideLogo`：终端宽度能够容纳完整合成画布时显示完整品牌字符画；
- `compactLogo`：终端无法容纳宽版时使用源图生成的紧凑点阵与紧凑 Braille `>_`，不允许文字占位；
- 品牌区域使用终端默认前景色，不设置彩色 foreground 或 background；无颜色模式保持同一轮廓。

启动面板显示 version、provider、model、project root 和 Session；Git branch 作为可选工作区信息，不在 `View()` 中执行 Git 命令。Branch 在 TUI 启动时通过有界 workspace metadata resolver 读取，不是 Agent Tool Call，也不进入 Conversation。

启动信息栏在宽终端下必须保留 4 列右侧 margin，Lip Gloss 的内容宽度需要扣除 border 盒模型，不能让右边界贴住终端最右列。窄于 60 列时继续使用无边框降级布局。

#### 19.2.2 Transcript Entry 与 Activity Presenter

当前 `fullscreenEntry{kind, content}` 可以继续作为最终提交单元，但工具事件不能再按 started/completed 各提交一条原始记录。新增 TUI Activity Presenter，把运行时事实投影为稳定、可测试的人类描述：

```go
type ActivityKind string

const (
    ActivityExplore ActivityKind = "explore"
    ActivityRun     ActivityKind = "run"
    ActivityPlan    ActivityKind = "plan"
    ActivityNetwork ActivityKind = "network"
)

type ToolActivity struct {
    CallID     string
    Iteration  int
    Kind       ActivityKind
    Title      string
    Detail     string
    Result     string
    Duration   time.Duration
    Success    bool
    Partial    bool
}
```

建议实现位置为 `internal/interface/tui/activity.go`。Presenter 只消费结构化事件，不查 Tool Registry、不读取文件、不执行命令，也不解析 Provider reasoning。

工具展示采用以下稳定语义：

| Tool/Side Effect | 默认展示 |
|---|---|
| `read_file` | `Read <path>` |
| `list_dir` | `List <path>` |
| `glob_files` | `Find <pattern>` |
| `grep_code` | `Search <pattern>` |
| `load_skill` | `Load skill <name>` |
| `read_skill_reference` | `Read skill reference <path>` |
| `apply_patch` | `Applied patch` |
| `write_file` | `Wrote <path>` |
| `revert_turn` | `Reverted changes` |
| `execute_command` | `Ran command` |
| `web_search` | `Searched web` |
| `web_fetch` | `Fetched <host>` |
| MCP | `Called MCP tool` |

未知 Tool 按 `SideEffect` 安全降级为 `Explored`、`Ran tool` 或 `Called network tool`，不直接打印任意参数。

Activity 标题和树形详情使用统一的低饱和文本色，其中动作词 `Read`、`Search` 使用与状态栏协调的 cyan 高亮并加粗，路径、pattern 和结果继续保持普通前景色。高亮只作用于展示文本，不改变 ActionSummary、Transcript Detail 或持久化数据。`• Explored` 或 `• Ran/Called` 对应的 Tool 全部 completed、success 且非 partial 时，标题前的 `•` 使用与 Context healthy 状态一致的 green；失败、未完成或 partial 保持普通前景色。每个 completed Reactor Iteration 继续在全部 Activity 之后提交整行 `────` separator，并在 separator 后追加一个普通换行，使下一段 assistant/Activity 与上一轮执行边界清晰分离。

#### 19.2.3 安全 Tool Action Summary

为了展示路径、搜索式和命令，`ToolCallStarted` 增加安全展示字段，而不是把完整 raw arguments 交给 TUI：

```go
type ToolCallStarted struct {
    // 现有关联字段
    CallID        string
    ToolName      string
    SideEffect    string
    ActionSummary string
    Detail        string
}
```

摘要在 ToolExecutor 完成 strict parse、保守 repair、Schema validation 并得到 normalized call 后生成；生成器只读取白名单字段，例如 `path/pattern/query/command/name`，执行统一长度上限、换行规范化和凭证脱敏。MCP/网络参数默认不展开，未知字段不进入事件。TUI 不解析 raw JSON，也不负责安全判断。

`ToolCallCompleted` 继续承载 success、partial、duration 和有界结果摘要。完整 Tool Result 仍只存在于 Reactor Observation/Evidence 和 Provider replay；TUI 摘要不是新的事实源。

#### 19.2.4 Iteration 聚合与提交时机

`fullscreenModel` 增加纯 UI 投影状态：

```go
activeIterations map[int]*IterationActivity
activeTools      map[string]*ToolActivity
```

事件处理顺序为：

```text
IterationStarted
  → 创建本轮 Activity Buffer

ToolCallStarted
  → 创建 active ToolActivity，不立即提交 scrollback

ToolCallCompleted
  → 按 CallID 更新结果，并放入当前 Iteration

IterationCompleted
  → 把只读活动合并为一个 “• Explored” 块
  → 写入/命令/网络活动按稳定顺序生成 “• Ran/Called” 块
  → 必要时提交分隔线
  → 清理本轮临时状态
```

延迟到 completed/iteration terminal 再调用 `tea.Println`，避免 started entry 已进入原生 scrollback 后无法原位更新。并行 Tool Calls 使用 CallID 关联，最终按模型原始调用顺序提交；完成顺序只影响动态活动区，不改变最终 transcript 顺序。

Iteration 分隔线只用于两个可见循环之间：没有工具、错误、计划变化或可见活动的简单回答不打印空分隔线；最终回答后不额外追加无意义横线。

#### 19.2.5 Working 动画与运行中输入

底部活动区增加 `tea.Tick` 驱动的 Working 行，显示 Run elapsed time 和取消提示：

```text
• Working (40s • esc to interrupt)
```

状态点使用终端兼容性较好的实心 `•` 与空心 `◦` 每 8 帧循环交替，避免 `●/◉/○` 在部分字体中回退成方框。`Working` 保持黑白语义，通过自适应灰阶亮度构造带柔和肩部的高光束，并以 32ms Tick、每字符三个子帧从左向右连续扫过；禁止使用青绿等品牌色逐字符跳变。elapsed 与 `esc to interrupt` 提示必须始终显示并使用 muted 灰色。动画只能更新底部活动区，不能不断向 scrollback 追加行。

Run 期间 textarea 继续可编辑；Enter 把下一任务加入现有串行队列。Esc 在输入为空且 Run 正在执行时取消当前 Run；输入非空、Approval 或 Session selector 状态下沿用各自局部 Esc 语义。Ctrl+C 继续作为明确取消键，不能因增加 Esc 而删除。

Working 行与上方的 assistant 草稿或最近提交内容之间固定保留一行空白；输入框与 Working/内容区域之间固定保留两行空白，状态栏仍紧贴输入框下一行。该留白属于默认 Rich Inline 视觉节奏，不能通过在 transcript 中提交空 entry 实现。

Run 成功完成后，临时 Working 行消失，并在最终 assistant 输出之后提交一条持久完成分隔线：`─Worked for 3h 4m 5s────`。耗时按秒四舍五入，小时和分钟按需出现，并在各单位之间保留一个空格；分隔线使用终端显示宽度补齐 `─`，进入原生 scrollback。取消或失败 Run 不显示 `Worked for`，继续使用各自的取消或错误条目。

Reactor 在 Tool Call 前提交的 assistant think content 必须在批次末尾补一个换行，与后续 Tool Activity 保持一行空白。`Worked for` 不自行添加前置或尾部换行：当最终 assistant 与完成行同批提交时使用正常 entry 间距；当最终内容此前已单独提交时，直接复用该内容批次留下的尾部空行，避免形成两行空白。完成行提交后，底部活动区自身固定的两行留白直接决定它与输入框的距离。用户提交下一条指令时，如果前一个 committed entry 是 `worked`，则只在新 user entry 批次前补一个换行，使用户消息与完成分隔线之间保持一行空白。所有留白由 Transcript batch 边界控制，不能在正文内容中写入伪造空行。

#### 19.2.6 Context Status 与 Workspace Metadata

状态栏使用 Provider `context_window`，不能再把 `agent.max_input_tokens` 当作模型窗口。两者语义保持：

- Provider `context_window`：单次 LLM Request 的模型上下文窗口；
- Agent `max_input_tokens/max_output_tokens`：整个 Run 的累计预算。

为了准确显示 `Context 47% used`，在 ContextWindowManager 生成 RequestView 后发布 `ContextWindowUpdated`：

```go
type ContextWindowUpdated struct {
    EstimatedInputTokens int64
    ContextWindow        int64
    EffectiveInputLimit  int64
    ProjectedToolResults int
    DroppedMessagePairs  int
}
```

TUI 使用 `EstimatedInputTokens / ContextWindow` 展示最近一次 RequestView 百分比；Provider `UsageUpdated` 继续显示真实 token usage，但不替代 ContextView 估算。窗口使用 `128K/258K/1M` 等稳定格式。状态栏按宽度依次保留 model、project、branch、context percent 和 window，窄终端从低优先级字段开始隐藏。状态栏使用适配明暗终端的柔和 pastel 前景强调：model 为 sky、project 为 soft blue、branch 为 lavender，Context 按 `<70%` mint、`70～89%` soft amber、`>=90%` soft red 分级，window 与分隔符使用 zinc gray；暗色终端使用较高亮度，亮色终端使用对应深色保证对比度。不使用背景色、低亮度 ANSI 色或彩虹式动画。

#### 19.2.7 Bounded Transcript Detail Viewer

原生 scrollback 中已经提交的行不可原位展开，因此 `Ctrl+T` 不修改旧输出，而是打开临时详情查看器：

```text
Rich Inline
  → Ctrl+T
临时 Bubbles viewport：查看完整 Tool/Command detail
  → Esc
返回 Rich Inline 输入状态
```

默认 transcript 只打印首尾摘要和 `… +N lines (ctrl+t to view transcript)`。完整详情保存在 bounded in-memory store：单项和单 Run 都有字节上限，超限时保留 head/tail 并标记 truncated；不得把完整详情写入 Conversation Message 或 SQLite。Viewer 可以暂时使用 viewport，但默认交互仍在 terminal main screen，退出 Viewer 后恢复原生 scrollback 和文本选择。

首版 Viewer 只支持查看、上下滚动、PgUp/PgDn 和 Esc 返回；搜索、复制模式、文件树、Diff Pane 与持久化 transcript 后置。

#### 19.2.8 响应式与降级边界

- `>= 100` 列：wide Logo、完整启动面板与状态栏；
- `64～99` 列：wide Logo、压缩面板、按宽度隐藏次要状态；
- `60～63` 列：compact Logo、压缩面板、隐藏 provider/session 等次要状态；
- `< 60` 列：单列标题、无边框信息、最小状态栏；
- `--plain`、非 TTY、`TERM=dumb`：不启用 Logo 动画、Working 动画、Viewer 或 Bubble Tea selector，继续使用 Plain Renderer；
- 不启用 mouse tracking 或 alternate screen；Rich Inline 默认滚轮和文本选择继续由终端处理；
- 中文、宽字符、emoji 和 ANSI 截断统一使用 Lip Gloss/xansi 宽度计算，不能按 byte 或 rune 数量直接截断显示宽度。

TUI 产品化验收必须覆盖：宽度档位、中文输入/退格、运行中排队、Esc/Ctrl+C 取消、并行 Tool 顺序、敏感参数脱敏、超大详情截断、Context 百分比、Git 非仓库降级、Approval、`/resume`、`--plain`、main-screen 控制序列守卫和全仓 race。

#### 19.2.9 实现切片与代码落点

M8T 按“先稳定外壳，再建立安全活动语义，最后增加详情能力”的顺序落地，避免同时改动输入、事件和 Context 三条主链：

| 切片 | 主要代码落点 | 实现边界 |
|---|---|---|
| 视觉外壳 | `internal/interface/tui/logo_generated.go`、`workspace.go`、`inline.go`、`application.go` | 只负责 Logo、启动面板、Transcript 前缀、Working 和状态栏；不得读取 Agent 内部状态或执行 Tool |
| Action Summary | `internal/tool/presentation.go`、`internal/agent/event/*`、`internal/agent/react/tool_execution.go` | ToolExecutor 在参数校验与规范化后生成安全摘要；事件只传展示白名单，不把 raw arguments 下放给 UI |
| Activity Presenter | `internal/interface/tui/activity.go`、`inline.go` | 按 CallID/Iteration 构建 UI 投影，读操作聚合、其他副作用逐项展示；不得成为 Tool Result 或 Conversation 的事实源 |
| Context 状态 | `internal/context/window.go`、`internal/agent/event/context.go`、TUI 状态投影 | 每次 RequestView 构建后发布估算；Provider Usage 与 Context 估算并存但语义分离 |
| Detail Viewer | `internal/interface/tui` 内 bounded detail store 与临时 viewport | 详情仅驻留有界内存；Viewer 关闭后回到 main-screen Rich Inline，不持久化到 SQLite |

实现和测试遵循以下顺序：

1. 先完成静态 Logo、workspace metadata、响应式 Banner 和 Working，锁定 main-screen 与输入行为；
2. 再完成安全 Action Summary、Typed Event 字段和 Activity 聚合，锁定脱敏与并行稳定顺序；
3. 然后发布 `ContextWindowUpdated`，接入状态栏和 bounded detail store；
4. 最后实现 `Ctrl+T` Viewer，并执行宽度、Unicode、TTY/Plain、Approval、Session、取消和 race 矩阵。

每个切片优先使用纯函数或纯 Model 更新测试；终端级测试只验证 Bubble Tea 命令、控制序列和主屏行为。任何为了通过视觉验收而引入 alternate screen、mouse tracking、无界 transcript 缓存或 raw Tool arguments 的实现都视为架构回归。

## 20. 持久化

| 数据 | 默认实现 | 位置 |
|---|---|---|
| Amadeus 主配置 | YAML | `$AMADEUS_HOME/config.yaml` |
| 用户级指令 | Markdown | `$AMADEUS_HOME/AGENTS.md` |
| 项目/目录级指令 | Markdown | `<project-root>/**/AGENTS.md` |
| 审计 | JSONL | `~/.local/state/amadeus/audit/` |
| Conversation Session/Run/Message/Summary | SQLite | `$AMADEUS_HOME/data/amadeus.db` |
| 用户 Skill | 文件 | `$AMADEUS_HOME/skills/<name>/` |
| 用户 MCP | YAML | `$AMADEUS_HOME/mcp.yaml` |
| 项目 Skill/MCP | 文件 | `<project>/.amadeus/` |

`AMADEUS_HOME` 是唯一的用户级配置、指令、Skill、MCP 和运行数据根，不是目标项目根；文档和代码不再为同一目录引入第二个根目录术语。SQLite 固定放在 `$AMADEUS_HOME/data/amadeus.db`，由 `projects.canonical_path` 区分不同目标项目；不得在目标项目内生成数据库，也不得回退当前工作目录。`data` 目录权限为 `0700`，数据库文件权限为 `0600`。审计继续使用独立 JSONL，避免把 append-only 安全记录和可迁移的业务状态耦合。

SQLite 初始化使用 `foreign_keys=ON`、WAL、`busy_timeout=5000` 和 `synchronous=NORMAL`。同一数据库允许在单个事务内原子完成 Session/Run 状态与正式消息写入。调用方必须提供 clean absolute `AMADEUS_HOME`；数据库或 data 目录为 symlink/非预期类型时拒绝打开，并使用受限目录与文件权限。

### 20.1 SQLite 表

M5R 收敛后固定六张表：

```text
schema_migrations
projects
conversation_sessions
runs
conversation_messages
conversation_summaries
```

明确不创建 `session_turns`、`run_checkpoints`、`checkpoint_instructions`、`plans`、`tasks`、`execution_graphs`、`tool_calls`、`observations`、`evidence`、`reflections`、`memories`、`user_preferences`、`embeddings`、`reflection_lessons`、`context_views`、`prompt_documents`、`persistent_grants` 或 `audit_records`。`Turn` 不属于目标持久化 Domain；Plan/Task/Iteration/Tool/Evidence 是单次 Run 的内存状态；ContextView/Prompt 动态重建，审批重新验证，审计写独立 JSONL。

### 20.1.1 收敛原则与数据边界

这次收敛不是为了减少表的数量本身，而是为了让持久化模型与新的固定 Agent 主链保持一致：

1. **只持久化用户需要恢复和查看的事实**：项目、会话、Run、正式消息和历史摘要。
2. **不把执行中间态当成对话事实**：Plan、Task、DAG、Tool Call、Observation、Evidence、Reflection 和预算只存在于当前 Run 的内存状态中；Run 结束时只提炼出最终消息或有界中断摘要。
3. **不恢复旧调用栈**：中断后不保存可重放的 checkpoint 序列。下一次用户输入创建新的 Run，读取旧 Run 的 `interrupted_context_json`，重新检查工作区和 `AGENTS.md`，再由普通 Reactor 或显式 `/plan` 路径重新规划。
4. **不把安全审计混入业务库**：Approval、授权结果和工具审计写独立 JSONL；Session Grant 只保存在当前进程内存中，进程退出后重新审批。
5. **不提前实现长期记忆**：不创建 memory、preference、embedding 或 reflection lesson 表；用户级和项目级 `AGENTS.md` 是可编辑、可解释的持久指令来源。

因此，六表是最终 MVP 业务模型，而不是把所有运行时对象都映射成表。后续若确有持久化后台任务、Multi-Agent 或可恢复工作流需求，应先新增独立 ADR 和迁移方案，不能直接向核心会话表追加临时字段。

### 20.2 `schema_migrations`

| 字段 | 约束 | 语义 |
|---|---|---|
| `version` | PK integer | 单调迁移版本 |
| `name` | not null | 迁移名称 |
| `applied_at` | not null | UTC 应用时间 |

迁移必须事务化、可重复检测且禁止跳过未知版本。

迁移必须保留可映射的 Project/Session/Message/Run/Summary 数据，并移除旧的 Turn/Checkpoint/Instruction Snapshot 结构。无法可靠映射的旧 checkpoint 只能转换为有界 `interrupted_context_json`，不能继续作为可恢复调用栈；正式 user/assistant 消息不得静默丢失。

### 20.3 `projects`

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Project ID |
| `canonical_path` | unique, not null | 规范化目标项目根路径 |
| `display_name` | not null | 展示名称 |
| `created_at` | not null | 创建时间 |
| `updated_at` | not null | 元数据更新时间 |
| `last_opened_at` | not null | 最近打开时间 |

首版以规范化真实路径作为项目身份。`--resume`、`--continue` 和 `sessions list` 默认只查询当前 Project，不能静默跨项目恢复。

### 20.4 `conversation_sessions`

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Conversation Session ID |
| `project_id` | FK, not null | 所属 Project |
| `title` | not null | 首条任务截断生成的标题 |
| `status` | not null | `active` 或 `archived` |
| `next_run_sequence` | not null | 下一个 Run 序号 |
| `created_at` | not null | 创建时间 |
| `updated_at` | not null | 更新时间 |
| `last_active_at` | indexed, not null | 当前项目最近 Session 查询依据 |

`amadeus` 启动得到 Draft Session，但数据库不保存 draft 状态；第一次真实任务才创建本表记录。Session 没有 `completed`，因为历史对话可以再次恢复。

### 20.5 `runs`

一个真实用户输入对应一个 Run。Run 是一次默认 ReAct 或显式 Plan→Execute→Replan 生命周期；下一次用户输入创建新的 sequence/Run，不精确恢复旧 Run。

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Run ID |
| `session_id` | FK, not null | 所属 Session |
| `sequence` | unique per session | 用户输入/Run 顺序 |
| `context_from_run_id` | self FK, nullable | 本 Run 使用的最近中断 Run |
| `objective` | not null | 本轮目标/用户任务 |
| `status` | not null | `running/completed/interrupted/failed` |
| `stop_reason` | nullable | 用户取消、进程退出、工具失败等原因 |
| `provider`/`model`/`api_mode`/`dialect` | nullable | 实际模型主链 |
| `execution_mode` | not null | `react` 或 `planned`；旧数据库迁移默认 `react` |
| `usage_json` | nullable | Run 实际使用量 |
| `interrupted_context_json` | nullable | 中断/失败时写入的有界重新规划摘要 |
| `started_at` | not null | 开始时间 |
| `finished_at` | nullable | 终态时间 |

`interrupted_context_json` 只在 Run 中断或失败时保存一次，至少包含 objective、已完成任务摘要、Evidence、相关路径、pending work 和 last error；它不是可重放的调用栈。发现遗留 `running` Run 时，启动或下一次真实任务可将其视为 `interrupted/process_terminated`，随后重新检查工作区并规划，不恢复旧工具调用。

MVP 不保存 `budget_json`、`latest_checkpoint_seq` 或独立 `continuation_of_run_id`。预算由当前配置和 RunState 动态构建；`context_from_run_id` 只表示新 Run 使用过哪个中断上下文。

### 20.6 `conversation_messages`

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Message ID |
| `session_id` | FK, not null | 所属 Session |
| `run_id` | FK, not null | 所属 Run |
| `sequence` | unique per session | 正式消息顺序 |
| `role` | not null | 首版仅 `user`/`assistant` |
| `content` | not null | 用户可见正文 |
| `created_at` | not null | 创建时间 |

每个 Run 必须有一条 user、最多一条 assistant 正式消息。用户消息在真实任务开始时持久化；Run 成功完成后才写 assistant。中断或失败只保留 user，不写未完成 assistant。system/developer/tool、流式增量和 reasoning 不进入本表。

### 20.7 `conversation_summaries`

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Summary ID |
| `session_id` | FK, not null | 所属 Session |
| `from_message_sequence` | not null | 覆盖起始序号 |
| `to_message_sequence` | not null | 覆盖结束序号 |
| `content` | not null | 摘要正文 |
| `source_hash` | not null | 原消息范围哈希 |
| `summary_hash` | not null | 摘要哈希 |
| `provider`/`model` | nullable | 生成来源 |
| `created_at` | not null | 创建时间 |

摘要是可重建派生数据，不删除或替换原始 Conversation Message，也不承担长期记忆职责。

### 20.8 事务边界

- 首个真实任务：原子创建 Project（若不存在）、Conversation Session、Run sequence、user message 和 Run。
- 后续任务：原子分配 Run sequence、创建 user message/Run，并更新 Session 活跃时间。
- Run 成功：原子写 assistant message、完成 Run，并更新 Session。
- Run 中断或失败：原子写 `interrupted_context_json`、完成 Run；保留 user message，不写未完成 assistant message。
- Context Summary 作为独立派生数据事务写入；当前 AGENTS.md 每次新 Run 重新发现，不依赖旧指令快照。
- 任何旧副作用在新 Run 中仍需重新发现、验证和审批；没有 Checkpoint 追加或工具重放事务。

Store 必须以事务保证 user Message 在执行前持久化、completed Run 与 assistant Message 原子完成、interrupted/failed Run 只写 Previous Work 摘要。Conversation 查询默认只返回 completed Run 的完整 Message Pairs；Pending 查询只返回最近尚未被成功 Run 覆盖的 interrupted/failed Run。Session Coordinator 对外使用 BeginRun/FinishRun 语义，并负责把 `context_from_run_id` 与有界 Previous Work 交给 ContextBuilder。

## 21. 错误处理与可观测性

- 使用 `fmt.Errorf("...: %w", err)` 保留错误链。
- 定义少量稳定错误类别：配置、认证、限流、网络、取消、策略、工具和协议。
- 用户错误与内部错误分开展示；debug 模式才输出堆栈或详细 payload。
- LLM trace 默认关闭；开启后仍必须脱敏。
- 每个 Run、LLM Call 和 Tool Call 生成独立关联 ID。
- 记录耗时、usage、工具成功率和终止原因，不记录 API key。

## 22. 测试策略

### 22.1 单元测试

- 配置优先级、变量展开、校验和脱敏。
- 消息与 SDK 请求转换。
- 流式 tool call 参数拼接。
- Runtime 终止条件、取消、重复调用和预算。
- PathGuard、CommandGuard、Approval 和 Audit。
- Tool schema、输出截断和并发顺序。
- Prompt 覆盖和 hash。

### 22.2 集成测试

- 使用 `httptest.Server` 模拟 Responses/Chat Completions 流。
- 使用临时目录验证文件工具和符号链接逃逸。
- 使用假命令验证超时、取消和输出限制。
- 使用进程 fixture 验证 MCP stdio JSON-RPC。
- 使用 SQLite 临时库验证 Session 恢复、完整消息对加载和 Previous Work 生命周期。

### 22.3 兼容性测试

为 Java 项目的关键行为建立 golden cases：同一输入、同一模型 fixture 和同一工具结果下，比较：

- 工具声明和参数 schema。
- tool call/result 消息顺序。
- Prompt 层次和动态上下文。
- 配置选择结果。
- 策略允许/拒绝结果。
- 终止原因和用户可见输出。

不比较内部类结构、日志文案或不可稳定的模型自然语言。

### 22.4 统一工程命令

仓库根目录 `Makefile` 提供稳定的本地与 CI 入口：

| 命令 | 作用 |
|---|---|
| `make fmt` | 使用 `gofmt` 格式化 `cmd` 和 `internal` 下的 Go 文件 |
| `make fmt-check` | 检查格式但不修改文件 |
| `make vet` | 执行 `go vet ./...` |
| `make test` | 执行 `go test ./...` |
| `make build` | 构建 `bin/amadeus` |
| `make check` / `make ci` | 顺序执行格式检查、vet、测试和构建 |

构建目标默认使用 `-buildvcs=false`，发布版本信息仍由 ADR-006 规定的 `-ldflags -X` 显式注入，避免构建结果依赖本地 VCS 元数据是否完整。

## 23. 开发阶段

| 阶段 | 交付目标 | 可执行出口 |
|---|---|---|
| M0 | 工程骨架与配置 | `amadeus version/config check` 可运行 |
| M1 | OpenAI SDK + 基础会话 | 可流式完成纯文本问答 |
| M2 | Provider、工具与首版 ReAct 基础能力 | 可在临时项目中完成模型—工具—观察闭环 |
| M3 | 首个可用 Coding Agent CLI | 根命令可在真实项目中安全读、改、测，分层 `AGENTS.md` 生效 |
| M4 | 核心工具增强、长上下文与 Session 持久化 | `apply_patch` 成为默认编辑路径；可跨进程恢复会话，中断后新 Run 重新规划 |
| M5R | Agent 主链收敛 | 默认 ReAct 与显式 `/plan` 的产品语义、Session 持久化和 TUI 主链完成收敛 |
| M6 | Coding Workflow 扩展 | Snapshot、LSP、Skill、MCP 与 Web 可选接入 |
| M6R | 独立 Reactor 与 Typed EventHub 重构 | 默认路径直接执行 Think→Analyze→Act→Observe Iterations；`/plan` 通过适配器复用同一个 Reactor，事件统一使用 Run/Task/LLMCall 关联语义 |
| M8T | Rich Inline TUI 产品化 | 完成品牌 Logo、Codex 风格 Activity、Working 动画、Context 状态和 bounded Transcript Viewer，同时保留终端原生 scrollback |
| M8 | 兼容回归与发布 | 形成可发布二进制和迁移说明 |
| M7 | Multi-Agent 与高级入口 | placement、TUI 多 Pane/高级 Diff、Runtime API 和后台任务复用统一 Runtime；默认 Rich Inline TUI 产品化不依赖 M7 |

每个阶段的最小任务、依赖与验收见 `docs/development-progress.md`。

## 24. 兼容性策略

### 24.1 必须兼容

- ReAct、显式 `/plan`、任务依赖和 SubAgent 协作的用户可见核心语义；内部不保留多套执行循环。
- OpenAI-compatible base URL/model/key 切换。
- 工具调用和图片工具结果回灌。
- Prompt 用户级/项目级覆盖。
- 路径围栏、HITL、审计与取消。
- MCP 动态工具命名和生命周期。

### 24.2 可有意识变化

- 配置文件从 `~/.paicli/config.json` 迁移为 Amadeus YAML；提供一次性导入命令。
- CLI 文案和 TUI 布局不要求完全一致。
- Provider 专属类改为配置驱动 capability。
- Java 中隐式或分散的默认值改为显式、可解释配置。
- 错误改为结构化类别，用户文案可变化。

## 25. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| OpenAI-compatible 服务协议差异 | 工具或流解析失败 | API 模式显式配置，fixture 覆盖差异 |
| 功能面过大 | 长期不可运行 | 严格按 M0-M8 保持每阶段可执行 |
| Go SDK 版本变化 | Adapter 编译失败 | SDK 限于单包，锁定版本并做契约测试 |
| Prompt 行为漂移 | Agent 能力退化 | Prompt golden test 与 Java 基线对比 |
| 并发工具引入竞态 | 文件损坏、结果乱序 | 副作用分类、有界并发、顺序回灌 |
| Plan/Replan 自循环 | 任务无法终止 | Context cancellation、Run 总预算和默认 8 次 plan cycle 上限 |
| 用户误用 `/plan` 处理简单任务 | 增加 Planner/Replanner 延迟 | 默认普通输入走 ReAct；仅在确实需要任务拆分时使用 `/plan` |
| 模型计划格式漂移 | 复杂 JSON 无法解析 | 使用宽容行协议；程序生成 ID、依赖、状态和 DAG，非列表文本降级为单 Task |
| 模型过早声称完成 | 工作未完成 | Replanner 同时接收原目标、Task 结果、Evidence 与当前 workspace，再决定 COMPLETE/REPLAN |
| 本地安全边界被误解 | 用户风险 | 明确 Amadeus 只有 PathGuard、CommandGuard、Approval 与审计，不宣称进程隔离沙箱 |
| TUI 产品化范围膨胀 | 首个发布继续延迟 | M8T 只实现 Logo、Activity、Working、Context 与 bounded Viewer；文件树、多 Pane、Diff 交互和持久化 transcript 继续后置 |

## 26. 架构决策记录

### ADR-001：官方 SDK 隔离在 Adapter

- 决策：Domain 不引用 OpenAI SDK 类型。
- 原因：支持 base URL、兼容 API、测试替身和未来 SDK 升级。

### ADR-002：Responses 优先、Chat Completions 兼容

- 决策：Provider 配置显式指定 API 模式。
- 原因：兼顾 OpenAI 当前主接口和原项目多 Provider 兼容需求。

### ADR-004：结构化事件驱动 UI

- 决策：Runtime 不直接打印终端。
- 原因：同一运行时支持 CLI、TUI 和 HTTP event stream。

### ADR-005：`AMADEUS_HOME` YAML 配置与明确优先级

- 决策：默认读取 `$AMADEUS_HOME/config.yaml`，环境变量和 flags 覆盖文件配置；目标工作项目不承载 Provider 主配置。
- 原因：满足自主切换 endpoint/key/model，并使最终结果可解释。

### ADR-006：Go 工程与基础依赖基线

- 决策：module path 使用 `github.com/Godric-W/Amadeus`，最低 Go 版本使用 `1.26.0`。
- 决策：CLI 使用 `github.com/spf13/cobra`，首个锁定版本为 `v1.10.2`。
- 决策：YAML 使用 `go.yaml.in/yaml/v3`，首个锁定版本为 `v3.0.5`。
- 决策：OpenAI Adapter 使用官方 `github.com/openai/openai-go/v3`，M1 首次实现锁定 `v3.47.0`。
- 决策：构建信息集中在 `internal/buildinfo`，开发构建默认使用 `dev / unknown / unknown`，发布构建通过 Go `-ldflags -X` 注入 version、commit 和 build time。
- 决策：根目录 `Makefile` 作为本地开发和 CI 的统一质量入口，`make check` 覆盖格式检查、vet、测试和二进制构建。
- 原因：Cobra 适合多级命令与后续补全；YAML v3 支持严格解码所需节点信息；OpenAI SDK 仅封装在 Adapter 中，后续升级不会污染 Domain。
- 约束：依赖只在首次被代码引用时写入 `go.mod`，避免提前加入未使用依赖。

### ADR-007：共享协议 Adapter 与显式 Provider Dialect

- 决策：`openai.Adapter` 实现 Domain `llm.Client`；官方 OpenAI Go SDK 负责请求、HTTP 传输、SSE 解码和 typed events，Adapter 只在 SDK 之上聚合 text/reasoning/usage/tool fragments、规范化错误并转换为 Amadeus Domain，不自行维护原始 HTTP/SSE parser。
- 决策：Responses、Chat Completions 共享 Tool Call fragment aggregator，但 aggregator 只验证调用身份、顺序和参数非空；Tool arguments 的严格 JSON 解析、保守 repair 和 JSON Schema 校验属于 Tool Call Normalizer，不在 Adapter 层重复实现。
- 决策：Dialect 由配置显式选择，标准方言作为默认回退，不根据 URL、Provider 名称或模型名猜测。
- 原因：避免复制完整 Provider Client，同时让工具调用、reasoning 和扩展字段差异可测试、可解释、可逐步增加。

### ADR-009：显式指令取代自动长期记忆

- 决策：不在当前路线图实现模型推断式 Durable Memory、MemoryStore/Retriever、自动偏好提取或跨 Run Reflexion Lesson；用户长期偏好写入 `$AMADEUS_HOME/AGENTS.md`，项目规范写入项目根和目录级 `AGENTS.md`。
- 决策：项目指令高于用户指令，更深目录高于更浅目录；当前用户请求高于普通工程约定，内置安全与 Approval 始终最高。每次注入保留 source/path/scope/hash。
- 决策：Conversation、ContextView 与 Previous Work 保持独立生命周期；每个新 Run 都重新解析当前 `AGENTS.md`，历史派生数据不能覆盖当前指令。
- 原因：显式文件可编辑、可审查、可版本控制且行为可预测，避免为收益不稳定的自动记忆承担误判、冲突、过期、隐私和检索复杂度。

### ADR-011：Session 恢复与中断后重新规划

- 决策：用户级 `resume` 只恢复当前项目的 Conversation Session；`amadeus --continue` 恢复最近 Session，`amadeus --resume` 打开选择器，`amadeus --resume <session-id>` 直接恢复，交互 `/resume` 可切换并允许 `Esc` 取消。首版只实现 `amadeus sessions list`，不提供 `--session`、`/sessions` 或 `sessions list --all`。
- 决策：`amadeus` 只创建内存 Draft Session，第一条真实用户消息才在 `$AMADEUS_HOME/data/amadeus.db` 原子创建 Session、user Message 和 Run。
- 决策：被中断或失败的 Run 永久结束；下一次真实输入创建新 Run，只加载完整 completed Message Pairs，并注入最近 Previous Work 摘要。MVP 不恢复调用栈、不重放工具、不使用额外 Router，也不实现 `continuation_of_run_id`。
- 原因：Session 恢复符合日常 CLI 习惯；重新规划比精确恢复未完成副作用更安全、更容易验证，并避免复杂自然语言继续意图识别。

### ADR-012：结构化读搜、Patch 修改与 Shell 执行

- 决策：保留 `read_file`、`list_dir`、`glob_files` 和 `grep_code` 作为高频结构化探索工具，即使 Shell 可以执行等价的 `cat/ls/find/grep`；专用工具负责 Project Root、稳定 schema、输出预算、Evidence 和跨平台语义。
- 决策：新增 `apply_patch` 作为已有文件修改主路径，支持版本化 create/update/delete Patch Document、上下文冲突检测、全 Patch 预检和逐文件原子写；`write_file` 收窄为显式 create/replace，默认拒绝隐式覆盖。
- 决策：`execute_command` 继续负责构建、测试、Git、格式化、生成器、项目脚本和专用工具无法表达的 fallback；Shell 不得绕过 PathGuard、CommandGuard、Approval、Audit 或输出预算。
- 原因：完全 Shell 化会降低权限判定、结构化结果、可移植性和上下文预算质量；为每个命令建立专用工具又会扩大模型选择面和维护成本，混合方案在 Coding Agent 能力、安全和复杂度之间更平衡。

### ADR-013：只读 SubAgent MVP

- 决策：第一版 Multi-Agent 不实现独立 Team Engine，只在 `/plan` 的 ExecutionGraph 上增加 SubAgent placement；主 Agent 是唯一 Run、Workspace 副作用和最终回答所有者。
- 决策：同一 Run 最多两个 SubAgent、委派深度固定为 1，并使用相同 Provider/model；只有互不依赖且显式 `read_only` 的 ready Task 可委派，SubAgent 只获得 read/list/glob/grep 工具。
- 决策：SubAgent 通过结构化 `SubAgentTask/Result` 接收最小 ContextView 并返回 Summary/Evidence/Usage；不写正式 Conversation，不写文件、不执行命令、不互相通信、不创建子 Agent。初期仅 `/team` 为本次 Run 设置 `prefer_subagents`，不自动 Router。
- 原因：只读并行已经能覆盖大型代码库调查的主要收益，同时规避共享工作区写冲突、Worktree、Patch 合并、Reviewer Agent、递归委派和多模型路由的实现风险；后续增强必须以真实收益为依据。

### ADR-014：默认 ReAct 与显式 Plan-and-Execute

- 决策：普通任务执行 `ContextBuilder → Reactor(Think→Analyze→Act→Observe)`，不创建 ExecutionGraph 或 synthetic root Task；只有 `/plan <task>` 执行 `ContextBuilder → PlanController → Planner.Decide(initial) → GraphBuilder → Scheduler → ReActTaskExecutor → Reactor → Planner.Decide(review)`。
- 决策：模式由用户显式选择；删除 StrategySelector、LLM Router、Direct→Planned 动态升级、Task 级强制 Verifier/Reflection、独立 Final Synthesizer 和复杂 completed-task graph merge。
- 决策：Reactor 不知道 `/plan`，也不返回 `needs_plan`；卡住只表达 `stalled/blocked/failed`，是否 Replan 由外层 Plan Controller 决定。
- 决策：初始 Plan 与执行后 Replan 由同一个 `Planner.Decide` 实现，通过 `PlanningInitial/PlanningReview` 区分语义阶段；代码可保留 `plan()`/`replan()` 包装方法，但不维护两个 Planner、两套模型或两套解析器。
- 决策：统一 Planning 协议只使用 `PLAN + task list` 或 `COMPLETE + final answer`；后续 review 返回新的 `PLAN` 即表示 Replan，不再增加单独的 `REPLAN` 输出关键字。
- 决策：Planner 不输出完整 Domain JSON，只输出自然语言任务列表；程序生成 Task ID、顺序依赖、状态和串行 DAG。非列表非空 Plan 正文降级为单 Task，空或非法响应只允许一次格式纠正。
- 决策：纯问候和简单查询默认不经过 Planner；显式 `/plan` 内仍要求 Planner 生成最小任务列表。
- 决策：一张 DAG 正常执行完毕，或某个 Task `failed/stalled/blocked` 时生成有界 ExecutionReport 并进入 review；用户取消、父 Context 取消和不可恢复基础设施错误直接结束，不额外调用 Planner。
- 决策：第一版 Scheduler 严格串行；资源推断、Task 并行和 Multi-Agent 均延后。Reactor 内资源安全的 Tool 级并行以及 PathGuard、CommandGuard、Approval、Audit、Evidence、取消和总 Run Budget 保持不变。
- 决策：MVP 提供 `/plan <task>` 选择规划路径但不提供 PlanReviewer；进入规划路径后的 Plan 和 Replan 自动继续执行。
- 原因：LLM 输出具有随机性，让所有任务强制规划会增加延迟和故障面；独立 Reactor 保证简单任务可用和代码边界清晰，显式规划作为外层编排器继续使用宽容文本协议处理复杂任务。

### ADR-015：采用 PaiCLI 风格的最小 Approval 机制

- 决策：MVP 不实现 Sandbox/ExecutionBoundary 抽象、Policy DSL、Docker/microVM 或项目外路径授权，只复用现有 PathGuard、CommandGuard、ApprovalHandler 和 Audit。
- 决策：只读工具在路径预检通过后直接执行；`write_file`、`apply_patch`、所有 `execute_command`、network side effect、`mcp_list_tools` 和 `mcp_call` 固定请求审批。
- 决策：Project Root 外路径、软链接逃逸和 CommandGuard blocked 操作直接拒绝，不允许通过 Approval Grant 绕过。
- 决策：配置文件删除 `approval.enabled/default`，TTY 固定询问，非 TTY 对需要审批的调用固定拒绝。
- 决策：MVP 只提供 `once/session/deny`；删除未持久化的 `always`。Session Grant 按工具名缓存，但每次调用仍先经过 PathGuard/CommandGuard。
- 决策：Skill 不建立独立审批系统；读取 Skill 文档属于只读，Skill 引发的副作用复用对应工具审批。
- 原因：现有 M3 已完成审批主链，固定分类最容易交付和测试；先接受写入与命令审批带来的交互频率，真实使用后再决定是否增加安全命令白名单或更细策略。

### ADR-016：文本 Skill 优先与最小 MCP Client

- 决策：用户级 Skill/MCP 统一位于 `$AMADEUS_HOME/skills` 和 `$AMADEUS_HOME/mcp.yaml`；项目级位于 `<project>/.amadeus/skills` 和 `<project>/.amadeus/mcp.yaml`，项目同名定义整体覆盖用户定义。
- 决策：Skill MVP 采用 Progressive Disclosure，只实现 `SKILL.md`、`references/`、metadata index、`load_skill` 和一次性 SkillContextBuffer；不实现 scripts、专用脚本执行器、Docker 或 Sandbox。
- 决策：MVP 不实现 Skill enable/disable Store，发现且校验成功的 Skill 默认可用；真实需求出现后再增加状态持久化。
- 决策：MCP 优先封装 `github.com/mark3labs/mcp-go`，第三方类型限制在基础设施 Adapter；不复制 WeKnora 的 Tenant/GORM/Handler/Redis/跨实例 Approval 层，也不从零维护完整 JSON-RPC 协议栈。
- 决策：MCP MVP 只交付 stdio、streamable HTTP、initialize、tools/list、tools/call、lazy lifecycle、一次有界重连、`mcp_list_tools`/`mcp_call` gateway、Approval、Audit 和有界文本结果；动态 Tool Adapter 保留为非生产基础设施。
- 决策：resources、prompts、sampling、notifications、mentions、图片和 Skill scripts 延后，不阻塞首个 MCP/Skill 可用版本。
- 原因：WeKnora 的 Go Client/Manager 和 Progressive Disclosure 思路值得复用，但其 Web、多租户和 Sandbox 复杂度不适合本地 CLI；PaiCLI 的配置覆盖、工具命名和按需注入范围更利于先交付。

### ADR-017：持久化收敛为六表与 Run 摘要

- 决策：业务持久化只保留 `projects`、`conversation_sessions`、`runs`、`conversation_messages` 和 `conversation_summaries`，另加 `schema_migrations`；`Turn` 不作为核心 Domain 或独立表。
- 决策：一个真实用户输入对应一个 Run；completed Run 形成完整 user/assistant Message Pair，interrupted/failed Run 的孤立 user Message 只通过 Previous Work 表达未完成状态。
- 决策：删除 `run_checkpoints` 与 `checkpoint_instructions`。中断或失败时在 `runs.interrupted_context_json` 一次保存有界摘要，下一 Run 重新检查工作区、当前 `AGENTS.md` 和工具状态后规划。
- 决策：Run 状态只保留 `running/completed/interrupted/failed`；`needs_plan`、`partial`、`awaiting_user` 和精确恢复状态不落库。
- 决策：MCP、Skill、TUI、Approval Grant、Plan、Task、Iteration、Tool Call、Evidence 和 Audit 不创建 SQLite 表，分别使用文件、内存运行态或独立 JSONL。
- 原因：新 Engine 已放弃精确 Run 恢复和复杂图持久化；保留消息、会话、Run 结果和摘要足以支持 `resume`、中断后 Replan、审计和长上下文，同时降低 Store、迁移和测试复杂度。

### ADR-018：语义化 Reactor 与 Typed EventHub

- 决策：`internal/agent/react` 按 `Think/Analyze/Act/Observe` 组织，`ReactLoop` 只负责编排、预算和终止；保留现有 ToolExecutor、Approval、Snapshot、Evidence、Replay 和资源感知并行能力。
- 决策：Reactor 输入输出改为独立 `react.Request/Result`，不依赖 `engine.TaskRunInput/TaskOutcome`、ExecutionGraph、TaskStatus 或 Scheduler；默认路径直接调用 Reactor，Plan 仅通过 `ReActTaskExecutor` 适配。
- 决策：一次 Think→Analyze→Act→Observe 使用包内 `react.Iteration` 表达，不再使用跨层 `Step` Domain；Iteration 不持久化，也不精确恢复。
- 决策：Think 消费 SDK Adapter 聚合后的 Domain stream；Analyze/Act 边界统一规范化 Tool Arguments。只允许尾随逗号、缺失容器闭合符和非法 escape 等有界语法 repair，禁止闭合截断字符串、空参数转对象、类型强转或字段猜测；修复后仍必须通过 Schema 和完整工具安全链。
- 决策：参数规范化失败不执行工具，而是生成结构化 Tool Error Observation 供下一轮有限纠正；normalized arguments 必须同时用于资源判断、Approval、Audit、Execute 和 Replay，避免执行参数与回灌参数不一致。
- 决策：现有强类型 `Event` 保留，在其上增加 metadata record、Fanout 和订阅能力；关联字段统一为 SessionID、RunID、可选 TaskID/Iteration 和 LLMCallID，目标协议不使用 TurnID。
- 决策：不复制 WeKnora 的知识库 Engine、`Data interface{}` payload、全异步 EventBus、乐观 Final Answer 回撤和无资源冲突的工具并行；只吸收语义分层、空响应重试、有限 Provider 降级、长 Run context 管理和多消费者事件思想。
- 原因：目录与控制流应能一目了然；ReAct 是可独立复用的执行内核，Plan-and-Execute 是外层编排能力，事件扩展不能牺牲类型安全、顺序和工具副作用前的失败边界。

### ADR-019：BaseContextBuilder 与 Per-Think ContextWindowManager

- 决策：现有一次性 ContextBuilder 收敛为 Run 级 BaseContextBuilder；它只组装稳定 Prompt、Instructions、completed Conversation Message Pairs/Summary、Previous Work、Goal、Tools、Skills 和 Source provenance，不管理 Reactor 每轮消息或写入 SQLite。
- 决策：Reactor 每次 Think 前必须调用 ContextWindowManager，以当前 BaseEnvelope、Runtime Messages、Provider Usage 和 ContextProfile 生成本轮唯一 RequestView；禁止维护一套“Memory 已压缩”但实际 LLM Request 未同步的第二上下文状态源。
- 决策：`agent.max_input_tokens/max_output_tokens` 继续表示整个 Run 的累计预算；单次请求必须使用独立 ContextProfile，明确区分 context window、output reserve、safety margin 和 compression threshold。未知模型窗口不得回退为 Run Budget。
- 决策：Token 计算使用 Provider Usage 作为上一轮基线，Estimator 只负责首轮和新增消息 delta；压缩或替换后重新完整估算。Provider-specific tokenizer 可插拔，但任何估算都必须保留安全余量。
- 决策：Context 装配采用 Pinned/High/Medium/Low 动态优先级和可借用预算；完整 user/assistant Message Pair 与 assistant Tool Calls/Tool Results 是不可拆分的 MessageGroup，Current Goal、有效 Instructions 和当前未完成工具协议不得静默裁剪。
- 决策：Tool Result 在进入 RequestView 前经过 token-aware Context Projection，保留结构化 metadata、错误、exit code、partial/truncated 状态和 head/tail；投影不修改原始 Evidence 或审计事实。
- 决策：Conversation Summary 使用覆盖范围和 hash 可验证的有界 Rollup，不无限追加旧摘要；首版使用确定性结构化摘要，可选 LLM summarizer 失败时必须安全回退，不启用 LongTermMemory、Retriever、RAG 或用户偏好推断。
- 原因：Coding Agent 的上下文会在一个 Run 内因 Tool Calls/Results 快速增长，只在 Run 开始时压缩历史无法保证后续请求不溢出；分离基础资产和每轮 Request 投影既保留可追踪性，又避免 PaiCLI 双状态源和 WeKnora 简单滑动删除造成的事实丢失。

### ADR-020：Codex 风格 Rich Inline 与 Tool Activity Presenter

- 决策：默认 TUI 继续使用 Bubble Tea main-screen Rich Inline、`tea.Println` 和终端原生 scrollback，不恢复 alternate screen、mouse tracking 或永久 transcript viewport。
- 决策：`docs/Amadeus_logo.webp` 只作为品牌源文件；首版使用构建期生成并静态嵌入的宽版/紧凑终端字符 Logo，不把 Kitty/iTerm/Sixel 图片协议设为默认依赖。
- 决策：ToolExecutor 在 normalized arguments 之后生成有界脱敏的 Action Summary；Typed Event 传递 SideEffect/ActionSummary，TUI Activity Presenter 不接收或解析任意 raw Tool arguments。
- 决策：读工具按 Reactor Iteration 聚合为 `Explored`，写入、命令、网络和 MCP 使用 `Ran/Called` 语义；started 状态只在底部活动区动态展示，completed/iteration terminal 后才持久提交 scrollback。
- 决策：ContextWindowManager 发布 RequestView 使用量，状态栏显示 Provider context window 与最近 ContextView 百分比；Run token budget 与模型 context window 不混用。
- 决策：`Ctrl+T` 使用 bounded in-memory Transcript Detail Viewer 临时查看完整输出，不尝试原位修改已提交的终端历史，也不把详情写入 Conversation SQLite。
- 原因：目标交互需要接近 Codex 的可读运行轨迹，同时保留当前中文输入、运行中排队、原生滚轮和文本选择优势；把活动语义放在 Presenter、把安全摘要放在 ToolExecutor，可以避免 UI 解析业务参数或形成第二事实源。

## 27. 已确认与待确认的实现决策

已确认：

1. 普通任务默认直接调用独立 Reactor；只有 `/plan <task>` 调用 Plan Controller、统一 Planner、GraphBuilder、Scheduler 和 review 循环，不进行自动模式选择。
2. 默认 ReAct 与 Plan Task 共用一个 Reactor、模型和 ToolExecutor；Plan 子 Task 通过 Execution Metadata/Event Filter 抑制正文，不复制第二套 Runner。
3. 初始 Plan 与执行后 Replan 是同一个 `Planner.Decide` 在 `initial/review` 阶段的语义调用；两阶段使用同一模型、实现和解析器。
4. Planning 协议只保留 `PLAN/COMPLETE`；程序从自然语言任务列表生成串行 DAG，review 返回新的 `PLAN` 即生成下一张图继续执行，模型不输出复杂 Task Domain JSON。
5. MVP 提供 `/plan <task>` 选择规划路径；Plan 和 Replan 仍直接执行，不引入人工审核分支。
6. MVP 不增加强制的 Task 级额外质量模型或独立 Final Synthesizer；Evidence、测试结果、Diff 和诊断作为 Reactor 最终回答或 Planner review 的上下文。
7. 不实现自动长期记忆；用户级、项目级和目录级 `AGENTS.md` 是跨 Run 指令的唯一基线来源。
8. `agent.mode` 不属于持久配置；ReAct 是默认执行内核，Plan-and-Execute 是用户显式选择的外层编排，Team 是未来 placement 能力。
9. Conversation Session、Terminal Session、Message 与 Run 使用独立语义；`Turn` 不作为核心实体，用户级 resume 不接受 Run ID，Task/Iteration 不持久化。
10. SQLite 固定在 `$AMADEUS_HOME/data/amadeus.db`；目标项目只提供 Project 身份与工作区，不承载运行数据库。
11. 中断后始终创建新 Run 并重新规划；只记录 `context_from_run_id`，不做精确 Run 恢复或继续意图分类。
12. 内置工具采用结构化探索、`apply_patch` 修改和受约束 Shell 执行；`write_file` 只承担新建或显式整文件替换。
13. Multi-Agent 第一版只有主 Agent 与最多两个只读 SubAgent；只通过 `/team` 请求并行调查，所有修改、命令、验证和最终回答仍由主 Agent 完成。
14. Approval 采用固定分类：只读直接执行，写入/命令/网络/MCP 请求审批，路径越界和 blocked 命令直接拒绝。
15. 配置文件不提供 Approval 开关或默认决策；MVP 只支持 once/session/deny，非 TTY 对需要审批的调用固定拒绝。
16. 默认交互 TUI 采用 Bubble Tea Rich Inline 主屏模式；Bubbles textarea 负责 Unicode 输入和 resize，终端负责原生 scrollback 与文本选择，Amadeus event/approval bridge 负责 Agent 集成。
17. 默认 TUI 使用静态终端 Logo、安全 Tool Activity Presenter、Iteration 聚合、Working 动画与 ContextWindowUpdated；`Ctrl+T` 只打开 bounded 临时 Viewer，不原位修改 scrollback。
18. `amadeus --plain`、非 TTY 和 `TERM=dumb` 使用逐行模式；不再支持 `AMADEUS_PLAIN`。文件树 Pane、多 Pane Diff 和持久化输入历史仍后置。
19. 用户级 Skill/MCP 只使用 `$AMADEUS_HOME/skills` 与 `$AMADEUS_HOME/mcp.yaml`；项目级 `.amadeus` 同名定义整体覆盖用户定义。
20. Skill MVP 只实现 SKILL.md、references、load_skill 和一次性上下文注入，不实现 scripts 或 Sandbox；MCP MVP 封装 mcp-go，只实现最小 tool client 闭环。
21. SQLite 最终收敛为 `schema_migrations`、`projects`、`conversation_sessions`、`runs`、`conversation_messages`、`conversation_summaries` 六表；不单独持久化 Turn、Checkpoint、Plan、Task、Iteration、Tool Call 或 Approval Grant。
22. 中断 Run 只保存有界 `interrupted_context_json`；下一 Run 只加载完整 completed Message Pairs，重新检查现场并注入 Previous Work，不恢复旧调用栈、工具位置或旧指令快照。
23. Context 分为 Run 级 BaseEnvelope 与每次 Think 的 RequestView；BaseContextBuilder 只组装稳定来源，ContextWindowManager 负责每轮 token 计算、动态投影和压缩。
24. Run 累计 token budget 与单次 Provider context window 使用不同类型和配置来源；不得用 `agent.max_input_tokens` 推导模型窗口。
25. Conversation Summary 使用有界 Rollup；Tool Call/Result 与完整 user/assistant Message Pair 按原子组压缩，Tool Result 在进入 LLM 前使用 token-aware projection。
26. Reactor 内部一次 Think→Analyze→Act→Observe 统一称为 Iteration；`Step` 不再作为跨层 Domain，Run Budget 使用 `max_iterations/iterations_used` 表达循环限制。
27. Provider 请求使用 LLMCallID；事件协议不再使用 TurnID 混合表达用户对话和模型请求。

后续增强开始前仍需固定：

1. 不同 Provider/model 的 ContextProfile 与 context window 来源。
2. 当前全项目 FileService snapshot 何时升级为增量或 Side-Git 实现。
3. Web 工具是否继续全部审批，还是为显式只读搜索增加受限白名单。
4. Multi-Agent 只读 placement 的真实收益达到什么阈值后再进入生产主链。
