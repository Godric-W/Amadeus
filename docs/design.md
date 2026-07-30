# Amadeus 架构设计

> 状态：Draft v0.3
> 创建日期：2026-07-29
> 最近修订：2026-07-30
> 输入依据：`docs/thought.md`、`../paicli-main` 当前代码、WeKnora ReAct 实现研究
> 目标语言：Go
> 产品形态：本地 Agent CLI，后续可复用同一运行时提供 Runtime API

## 1. 背景

Amadeus 的目标是实现面向真实软件工程任务的通用 Agent CLI。`../paicli-main` 提供功能面和迁移基线，但不是最终架构模板；Go 实现应在保留有价值行为的同时修正其 Agent 模式分裂、职责过度集中、Provider 重复实现和运行时耦合问题，并吸收 WeKnora 等项目中成熟的局部工程实践。

这里的“翻译”定义为：

1. 保留 PaiCLI 已有的核心能力和交互语义。
2. 不要求 Java 类与 Go 文件一一对应。
3. 优先建立可测试的稳定边界，再逐阶段迁移功能。
4. 对确认有价值的逻辑进行 Go 化重构，并在 `docs/migration-progress.md` 的优化记录中追踪。

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
- 以统一 Agent Engine 为目标分阶段交付：先完成单 root Task 的 ReAct/Verification/Reflection 闭环，再补 Adaptive Planning、Replan 和 Multi-Agent placement。
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
  └── Event Bus ─────────── Renderer / API stream / Trace log
```

## 6. 分层架构

### 6.1 Interface 层

负责协议适配，不包含 Agent 决策逻辑。

- `cmd/amadeus`：命令行入口；根命令直接启动 Coding Agent，不设置独立 `run` 子命令。
- `internal/interface/cli`：交互循环、slash command、补全和 history。
- `internal/interface/tui`：可选全屏 TUI。
- `internal/interface/httpapi`：线程、回合和事件流 API。
- `internal/render`：plain、inline、TUI/API event renderer。

根命令语义固定为：

| 调用 | 行为 |
|---|---|
| `amadeus` | stdin 为 TTY 时，以当前工作目录为目标项目并进入交互式 Coding Agent |
| `amadeus "<task>"` | 在当前工作目录执行一次性 Coding Agent 任务，完成后退出 |
| `amadeus --project <path>` | 以显式项目目录进入交互模式 |
| `amadeus --project <path> "<task>"` | 在显式项目目录执行一次性任务 |
| `printf '%s\n' '<task>' \| amadeus` | 非 TTY 时从 stdin 读取一次性任务 |

`--help` 和已注册子命令继续按 CLI 语义处理；`version`、`config`、`tools`、`chat` 等管理/诊断入口不进入 Coding Agent。`amadeus chat` 保留为不装配工具和 Agent Engine 的纯聊天命令。无位置参数、stdin 非 TTY 且读取不到有效任务时返回明确错误。`AMADEUS_HOME` 只解析配置与用户级 `AGENTS.md`，目标项目默认来自启动工作目录，也可由 `--project` 覆盖。

### 6.2 Application 层

负责用例编排和生命周期。

- `internal/app/bootstrap`：加载配置并装配依赖。
- `internal/app/session`：会话状态、PlanningPolicy、slash command 和取消。
- `internal/app/command`：本地命令处理。
- `internal/app/task`：后台任务用例。

### 6.3 Domain/Runtime 层

包含 Agent 的稳定业务模型。

- `internal/agent/engine`：统一 Run 状态机、Plan-on-Demand、调度、验证、Reflection 和 Replan。
- `internal/agent/runtime`：M1 已有的单轮流式 Session；M2 起逐步演进为 Engine 可复用的模型调用与事件基础设施。
- `internal/agent/event`：Runtime 结构化事件、Sink Port 与测试用 Memory Sink。
- `internal/agent/react`：执行单个 Task 的通用 ReActRunner，不拥有独立 Agent 模式。
- `internal/agent/plan`：ExecutionGraph、Planner、Scheduler、计划审阅和 Replan。
- `internal/agent/reflect`：Verifier 结果上的结构化 Reflection 决策。
- `internal/agent/team`：把 Task 委派给 SubAgent；SubAgent 仍复用同一个 ReActRunner。
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
│   └── migration-progress.md
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

M2-01～M2-03 已在 `internal/tool` 落地 Provider/UI 无关的 `Spec/Call/Result/Tool/Executor`，并实现并发安全 Registry。工具输入使用 JSON Schema Draft 2020-12 校验，禁止外部 `$ref`；受限 repair 只处理尾随逗号和缺失的闭合括号，repair 后仍必须重新通过 schema 校验，不能把广义“猜测修复”带入执行边界。

### 8.4 Run、ExecutionGraph 与 Task

```go
type RunState struct {
    ID           RunID
    Goal         Goal
    Graph        ExecutionGraph
    ActiveTaskID TaskID
    Status       RunStatus
    Budget       BudgetState
    Evidence     []Evidence
    Reflections  []Reflection
    StopReason   StopReason
}

type ExecutionGraph struct {
    Kind    ExecutionKind
    Version int
    Tasks   []Task
}

type Task struct {
    ID                 TaskID
    Objective          string
    Dependencies       []TaskID
    AcceptanceCriteria []Criterion
    Status             TaskStatus
    Attempts           int
    Budget             Budget
    Result             *TaskResult
}
```

每个 Run 都至少包含一个根 Task，但并非每次都调用 Planner。简单请求使用程序创建的单节点 `direct` ExecutionGraph；复杂请求或执行中升级后的请求使用 `planned` DAG；未来委派给 SubAgent 时使用同一图，只改变 Task 的执行归属。

```go
const (
    ExecutionDirect    ExecutionKind = "direct"
    ExecutionPlanned   ExecutionKind = "planned"
    ExecutionDelegated ExecutionKind = "delegated"
)
```

`direct` 不是另一套 Agent，而是只有一个 root Task 的执行图。这样简单任务、Plan task、SubAgent task 和后台任务可以共享状态、预算、取消、事件、工具执行、验证、Reflection 和 checkpoint。

M2-04 已在 `internal/agent/engine` 落地上述 Domain，并补充 `TaskResult/Step/Observation/Evidence/ArtifactRef/Budget/ReflectionRecord`。Domain 可 JSON 序列化，不依赖 LLM、Provider SDK、具体 Tool 或 CLI；`NewDirectExecutionGraph` 负责创建程序生成的单 root Task 图。

### 8.5 Step、Observation 与 Evidence

```go
type Step struct {
    Index        int
    Decision     DecisionSummary
    ToolCalls    []ToolCall
    Observations []Observation
    Evidence     []Evidence
    Status       StepStatus
}

type Evidence struct {
    Kind     EvidenceKind
    Source   string
    Summary  string
    Artifact *ArtifactRef
    Verified bool
}
```

Evidence 是一等 Domain，而不是藏在工具输出字符串中。命令退出码、测试结果、文件 diff、诊断、生成物和结构化校验都应转为 Evidence，供 Verifier、Reflector、Replan 和最终回答引用。Agent 不应只因为模型声称“已经完成”就把 Run 标记为成功。

### 8.6 Engine Ports

```go
type Planner interface {
    Plan(ctx context.Context, input PlanInput) (ExecutionGraph, error)
    Replan(ctx context.Context, input ReplanInput) (ExecutionGraph, error)
}

type Scheduler interface {
    Ready(graph ExecutionGraph) ([]TaskID, error)
}

type TaskRunner interface {
    Run(ctx context.Context, input TaskRunInput) (TaskOutcome, error)
}

type Verifier interface {
    Verify(ctx context.Context, input VerificationInput) (Verification, error)
}

type Reflector interface {
    Reflect(ctx context.Context, input ReflectionInput) (Reflection, error)
}
```

初期 Planner、ReActRunner 和 Reflector 统一使用同一个 `llm.Client` 与模型，但保持独立 Port，未来可在不修改 Engine 的情况下按角色选择不同模型。

M2-10 已在 `internal/agent/engine` 定义 `ReActRunner`/`TaskRunner` Port、`TaskRunInput` 和互斥的 `TaskOutcome`。Outcome 只允许 `candidate_complete/needs_plan/blocked/failed/cancelled`：candidate 必须携带 TaskResult；needs-plan 必须给出升级原因；blocked 固定使用 `user_input_required`；failed 必须使用失败类 StopReason；cancelled 固定使用 `cancelled`。Runner 只接受处于 running 状态的单个 Task，不承担全局 Plan 或 Run 完成判断。

M1-10 在 `internal/agent/runtime` 实现最小单轮 `Session`。构造时显式注入 `llm.Client`、`event.Sink`、temperature 和 max output tokens；模型名与 Provider 信息来自 Client 的稳定 `ModelInfo`。`RunTurn` 接收显式 turn ID 和一条用户文本，不读取配置、不生成 ID，也不直接输出到 stdout。M1-12 在此基础上增加最小对话历史：只有成功完成的 user/assistant 消息对会提交到 Session，失败轮次不会污染下一次请求；上下文裁剪、持久化和记忆仍留在 M3。

单轮事件顺序固定为 `TurnStarted`，随后按 SDK 流顺序发布 `ReasoningDelta/TextDelta`，收到 usage 时发布 `UsageUpdated`，流关闭成功后发布 `TurnCompleted`。Session 同时聚合 assistant content/reasoning、response/request ID、finish reason 和 usage，返回完整 Domain `Response`。若打开或读取流失败，发布 `ErrorOccurred` 并返回原错误；若在完成事件前收到 `io.EOF`，归类为 `protocol` 错误。无论成功失败都关闭流，Event Sink 发布失败不会被静默忽略。

## 9. 统一 Agent Engine

Amadeus 的目标是面向真实软件工程任务的统一 Agent，而不是复刻 PaiCLI 的 `ReactAgent/PlanExecuteAgent` 分裂。Plan-and-Execute、ReAct 和 Reflection 是同一个 Engine 的三个层次：Plan 提供任务结构，ReAct 执行单个 Task，Verification + Reflection 决定接受、重试或 Replan。

```text
User Request
    │
    ▼
Strategy Selector / Planning Policy
    │
    ├── Direct：程序创建单 root Task
    └── Planned：Planner 创建 ExecutionGraph
                    │
                    ▼
                 Scheduler
                    │
                    ▼
                ReActRunner
                    │
                    ▼
                  Verifier
                    │
                    ▼
             Triggered Reflector
                    │
      ┌─────────────┼──────────────┐
      ▼             ▼              ▼
    Accept       Retry Task       Replan
      │                            │
      └──────────────┬─────────────┘
                     ▼
              Final Verification
                     │
                     ▼
                 Synthesis
```

### 9.1 Plan-on-Demand

默认不为每个请求额外调用一次 LLM Router。Engine 先使用本地确定性信号决定初始策略：`/plan`、用户明确要求先规划/等待确认、高风险操作、明确步骤依赖、多个独立交付物、跨项目/服务和大规模迁移直接进入 Planned；其余请求先进入 Direct discovery。

Direct 时 Engine 不调用 Planner，只创建不可见的 root Task：

```text
ExecutionGraph(kind=direct)
└── root：用户目标
```

这不是用户可见的形式主义计划，而是统一状态、预算、事件和恢复语义的执行容器。简单任务不会增加模型调用或计划审核延迟。

### 9.2 动态升级为 Planned

Direct 允许先执行读文件、搜索代码、查看 diff 和定向测试等低风险 discovery。真正复杂度往往只有在观察仓库后才能确定，因此以下信号可以把当前 Run 无损升级为 Planned：

- 发现多个相对独立的工作单元或明确依赖。
- 修改范围跨越多个模块、多个产物或多个验证阶段。
- 连续无进展、重复工具调用或相同错误达到阈值。
- 工具失败揭示接口、schema、fixture 或调用方的连锁影响。
- Candidate Result 无法通过确定性验证，且简单 retry 不足以修复。
- 即将执行高风险、大范围或资源冲突明显的副作用。
- Reflector 返回 `replan`，或 ReActRunner 返回 `needs_plan`。

升级时保留已完成 discovery 的 Step、Observation、Evidence、当前 workspace 状态和剩余预算；Planner 基于真实当前状态创建后续 DAG，不重新假设任务从零开始。已完成调查可以作为 completed discovery Task 写入新图。

### 9.3 统一状态机

```text
INITIALIZING
    ↓
STRATEGY_SELECTING
    ├── direct  → TASK_RUNNING
    └── planned → PLANNING → PLAN_READY → SCHEDULING
                                      ↓
                                  TASK_RUNNING
                                      ↓
                           TASK_CANDIDATE_COMPLETE
                                      ↓
                              VERIFYING_TASK
                                      ↓
                              REFLECTING_TASK
        ┌──────────────┬──────────────┼──────────────┬──────────┐
        ▼              ▼              ▼              ▼          ▼
      ACCEPT         RETRY          REPLAN        ASK_USER     ABORT
        │                              │              │          │
        ▼                              ▼              ▼          ▼
 TASK_COMPLETED                    PLANNING       SUSPENDED    FAILED
        │
        └── 所有 Task 完成 → VERIFYING_RUN → REFLECTING_FINAL
                                      ├── accept → SYNTHESIZING → COMPLETED
                                      ├── retry  → TASK_RUNNING
                                      └── replan → PLANNING
```

终止原因必须结构化，包括 `completed/cancelled/max_steps/budget_exceeded/provider_error/tool_error/verification_failed/replan_exhausted/user_input_required`。

M2-05 已实现 Run/Task 显式状态与合法转换。Candidate Complete 不允许直接进入 Completed，必须依次经过 Verifying 和 Reflecting；终态拒绝后续转换，非法跳转返回携带 from/to 的稳定 `TransitionError`。

### 9.4 ReActRunner

ReActRunner 只负责执行一个 Task，不负责整个产品模式，也不直接创建全局 Plan：

```text
1. 构建 Task objective、acceptance criteria、上下文切片和预算
2. 调用 LLM 并流式发布 reasoning/text/tool-call 事件
3. 若有 tool call：
   3.1 归一化 call ID 并聚合参数分片
   3.2 解析 JSON，必要时做受限 repair，再执行 JSON Schema 校验
   3.3 经过 Policy → HITL → Audit
   3.4 按副作用、资源键和 ParallelSafe 做有界并发
   3.5 把结果转换为 Observation/Evidence，并按原 call ID 回灌
4. 检查取消、预算、最大步骤、重复动作和无进展状态
5. 若模型不再调用工具，只返回 CandidateTaskResult，不直接宣布 Run 完成
6. 必要时返回 needs_plan/blocked/failed/cancelled
```

应吸收 WeKnora ReAct 的成熟工程处理：显式 Agent state/step、Engine 单轮尽量无状态、空回答 retry、重复内容与卡死检测、取消时保存 partial step、tool call ID normalize、每工具 timeout、并行结果保持原顺序、max steps 后可选 synthesis 和完整事件追踪。实现时按 Amadeus Domain 重写，不把 WeKnora 的 Chat 协议、知识库依赖或 EventBus 类型复制进 Engine。

M2-11 已在 `internal/agent/react` 实现无状态单次 `Iterator`。它把 Task context 与 Tool Specs 转为 Domain LLM Request，流式聚合 reasoning/text/usage/tool calls，并复用统一 Event Sink 发布稳定顺序；无 ToolCall 且正常 stop 时只生成 Candidate TaskResult，有 ToolCall 时只返回规范化 `tool.Call`，不执行工具、不循环，也不将候选结果标记为 Task/Run completed。空回答或 finish reason/tool call 不一致返回 protocol 错误。

M2-12 已实现单 ToolCall 执行边界：Registry lookup → 受限 repair/JSON Schema 校验 → Tool.Execute → Observation/Evidence。参数失败不会调用 Tool；查找、校验和执行失败仍保留结构化 Observation、partial Result 和未验证 Evidence，同时返回原 error 供循环策略判断。M2-13 在此基础上把 assistant ToolCalls 与执行结果按原 call 顺序构造成 JSON ToolResult payload，拒绝缺失、重复或额外 call ID；Responses/Chat Adapter 分别映射为 `function_call_output` 与 `role=tool`，顺序由双协议 fixture 固定。

M2-15 已实现组合上述组件的 `react.Runner`。每轮调用 Iterator；出现 ToolCalls 时顺序执行、记录 Step/Observation/Evidence、调用 ProgressMonitor 并按原 call ID 回灌，再进入下一轮；出现正常文本时只返回 `CandidateTaskResult`，其中包含候选 TaskResult、最终 Message、累计 Usage、Steps 和 Evidence。工具失败会作为可见结果回灌；Progress signal 可以返回 `needs_plan`；Runner 不修改调用方 Task 状态，也不把 Candidate 当作 completed。

M2-16 已把步骤、输入/输出 token、工具调用次数和 wall-clock 纳入 `BudgetState`。Runner 在每次模型调用前检查已耗尽预算，并按剩余输出 token 收紧单次请求上限；模型返回后立即累计 step/usage，越界时不执行工具，工具批次超过剩余额度时整批拒绝。TaskOutcome 携带最终 BudgetState 和结构化 `LimitReached`，区分 steps/tool_calls/input_tokens/output_tokens/wall_clock；父 context 取消仍归类为 cancelled，Runner 自己的 wall-clock deadline 归类为 budget_exceeded。达到上限后不会继续调用模型。

### 9.5 Progress Monitor

Progress Monitor 优先使用确定性状态，不额外调用模型：统计工具签名、资源键、错误、修改范围、验证结果、Evidence 增量和剩余预算。它可以触发 `no_progress/repeated_action/high_impact/needs_plan`，但不负责语义评价。

M2-14 已实现并发安全的确定性 `ProgressMonitor`。Tool arguments 先 canonicalize，再与 tool name 组成稳定 action signature；错误按 tool name 与规范化消息计数；连续无 Evidence 增量单独计数并在出现新 Evidence 后清零；write/execute/network 或 exclusive-resource ToolCall 产生 high-impact signal。所有阈值显式配置，signal 只提供 reason/count/recommend-plan，不直接修改 Task 状态或调用 LLM。

第一版应保持保守：明显复杂任务直接 Planned，其余 Direct；初始选择允许不完美，因为执行中可以升级。不能只靠关键词或模型名猜测任务复杂度。

### 9.6 Verifier

Verifier 负责确定性检查，不负责自由文本自评：

- 命令退出码、测试、构建、格式和静态检查。
- 文件是否存在、是否真的修改、diff 是否包含目标范围。
- JSON/schema、生成物、诊断和 Task acceptance criteria 的机器可验证部分。
- Evidence 是否足以支撑“已完成”的声明。

LLM 无 tool call 只代表 Candidate Complete；只有 Verifier 和后续 Reflection 接受后，Task 或 Run 才能进入 completed。

M2-17 已在 `internal/agent/engine` 定义 `Verifier`、`VerificationInput`、`Verification` 和逐项 `VerificationCheck`。确定性实现只消费 Task acceptance criteria、Candidate 引用的 Evidence 以及 Evidence 的 verified/criterion attribution：缺失或未验证的 Candidate Evidence、Required criterion 缺少匹配证据都会形成稳定 evidence gap；Optional criterion 无证据时标记 skipped，不调用 LLM。Evidence 可用 `criterion_ids` 显式声明支持哪些验收条件，Verification 结果只能是 passed/failed，并通过契约校验防止“passed 但仍有失败检查或证据缺口”。

### 9.7 Triggered Reflection

Reflection 初期与 Planner、ReActRunner 使用同一个模型，但通过独立 `Reflector` Port 调用。Reflection 不在每个工具后无条件执行，以免延迟和成本近似翻倍；以下检查点触发：

- Task 产生 Candidate Result。
- 整个 Run 即将完成。
- 验证失败、工具连续失败、重复动作或无进展。
- 计划关键假设被 Observation 否定。
- 达到步骤/重试边界，或需要决定 retry/replan/ask user。

Reflection 输出必须结构化，且不得持久化完整 chain-of-thought：

```go
type Reflection struct {
    Scope          ReflectionScope
    Verdict        ReflectionVerdict
    Issues         []Issue
    EvidenceGaps   []string
    NextActionHint string
    PlanChanges    []PlanChange
    Lesson         string
}

const (
    ReflectionAccept  ReflectionVerdict = "accept"
    ReflectionRetry   ReflectionVerdict = "retry"
    ReflectionReplan  ReflectionVerdict = "replan"
    ReflectionAskUser ReflectionVerdict = "ask_user"
    ReflectionAbort   ReflectionVerdict = "abort"
)
```

`Reflection` 是当前 Run 内的质量控制；可跨 Run 持久化的 `Reflexion Memory` 留到 M3，只保存简短失败原因、有效策略和证据要求，不保存隐藏思维链或敏感正文。

M2-18 已在 `internal/agent/engine` 定义严格的 `Reflector` Port、ReflectionInput、五种 verdict、Issue 和 PlanChange；`internal/agent/reflect` 使用注入的同一 Domain `llm.Client` 执行独立非流式检查点调用。模型必须只返回单个 JSON object，未知字段、Markdown code fence、尾随文本、非法 verdict 或不符合 Verification 的 accept 都被拒绝；非 accept verdict 必须包含 issue、evidence gap 或 next action，完整 chain-of-thought 不在协议中。

M2-19 已实现 `DirectEngine` 单 root Task 主链。Engine 从 initialized direct graph 进入 strategy_selecting/task_running，调用同一个 TaskRunner；Candidate 必须依次经过 task/run 状态转换、Verifier 和 Reflector。accept 才写入 Task.Result 并完成 Run；retry 保留 Budget/Evidence/Steps、增加 attempts，并把结构化验证反馈加入下一次执行上下文；runner needs_plan 或 reflector replan 进入 planning，ask_user 进入 suspended，abort/预算/Provider 等失败保留结构化 StopReason。Direct 单 root 的 task-level Verification/Reflection 同时作为最终 Run 质量门，完成时仍经过 verifying_run/reflecting_final/synthesizing 状态，不增加重复模型检查。

M2-20 已为统一 Engine 增加 run started、run/task status changed、verification completed、reflection completed 和 terminal run completed 事件。DirectEngine 显式注入 Event Sink，Renderer 和测试不依赖 Engine 内部实现即可观察状态机；取消使用不受原 context 取消影响的终态发布 context，确保 cancelled 仍可被记录。ReActRunner 在工具执行期间取消时保留已尝试工具次数、cancelled Step、partial Tool Result、Observation 和 Evidence；DirectEngine 继续保留这些增量且不误报 completed。

### 9.8 Replan 与重试边界

Retry 处理同一 Task 内可局部修复的问题；Replan 处理任务拆分、依赖、验收条件或关键假设已经变化的问题。Engine 必须限制每 Task attempts、全局 replan 次数、总步骤、总 token、工具次数和 wall-clock 时间，避免 Reflection/Replan 自循环。

Replan 输入至少包含用户目标、旧图、已完成 Task、失败记录、Evidence、当前 workspace/diff 摘要和剩余预算。新图不得把已完成副作用当作未发生，也不得悄悄丢弃用户验收条件。

### 9.9 `/plan` 语义

`/plan` 不是切换到另一套 Plan Agent，而是设置本次 Run 的 Planning Policy：

```go
PlanningPolicy{
    ForcePlanning: true,
    ShowPlan:      true,
    RequireReview: true,
}
```

默认输入由 Engine 自动选择 Direct/Planned，并允许动态升级；`/plan <任务>` 强制生成完整 ExecutionGraph、展示并等待 `approve/edit/cancel`，随后仍由同一个 Scheduler、ReActRunner、Verifier 和 Reflector 执行。执行完成后不存在“切回 ReAct”，因为全程只有一个 Engine。

### 9.10 参考实现取舍

PaiCLI 只作为能力面、行为基线和迁移参考。其 `Agent.java` 与 `PlanExecuteAgent.java` 分别维护工具循环，Amadeus 不继承这种顶层分裂。

WeKnora 的 ReAct 工程化优于 PaiCLI，可吸收显式 Engine/State/Step、think-analyze-act-observe 分解、流式边界处理、空响应重试、卡死检测、取消、tool timeout、并行顺序和事件设计；但不能整体照搬：其 Engine 依赖知识库产品上下文、消息回灌偏 Chat Completions、并行策略缺少 coding tool 的资源冲突语义，而且 `Reflection` 只有预留字段而没有完整闭环。

WeKnora 使用 MIT License。若未来复制实质性代码片段必须保留相应版权和许可证；默认策略仍是借鉴算法、边界用例和测试思想，按 Amadeus Domain 独立重写。

## 10. Adaptive Planning 与 Multi-Agent

### 10.1 ExecutionGraph 与 Scheduler

- Planner 只生成结构化 ExecutionGraph，不执行工具。
- Reviewer 负责用户确认、修改或取消。
- Scheduler 根据依赖关系选择 ready tasks，并限制并发度。
- 每个 Task 复用同一个 ReActRunner，但使用独立 context view、预算、attempts 和 acceptance criteria。
- Task 输出、Evidence 和 Reflection 写回 RunState，供后续 Task、Replan 和最终 synthesis 使用。
- 默认自动规划可以不要求人工确认；`/plan` 强制展示和审核，高风险计划仍受 Policy/HITL 控制。

### 10.2 Multi-Agent

- Multi-Agent 不是新的执行循环，而是 Scheduler 的 Task placement 能力。
- Orchestrator 负责角色选择、任务拆分、上下文裁剪和预算分配。
- SubAgent 共享工具定义，但拥有独立会话视图和最小必要上下文。
- SubAgent 接收结构化 Task/Handoff，仍调用相同的 ReActRunner、Verifier 和 Reflector。
- Handoff 使用结构化消息，不依赖拼接自然语言约定。
- 全局限制总并发、总 token、总工具次数和递归委派深度。

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

- 默认配置：`<amadeus-root>/config.yaml`。
- 显式配置路径：Loader 已支持任意文件路径，后续由 CLI `--config` 接入；显式文件不存在时返回错误。
- 环境变量：适合 CI 和密钥注入。
- CLI flags：仅覆盖本次进程。

### 12.2 优先级

从高到低：

1. CLI flags。
2. 环境变量。
3. Amadeus 根目录 `config.yaml`。
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
  max_steps: 30
  max_parallel_tools: 4

approval:
  enabled: true
  default: ask

logging:
  level: info
  trace_llm: false
```

`api_key` 等 YAML 字符串值支持 `${ENV_VAR}` 引用和字面值。变量在字段级 YAML 解码前展开；变量未设置时，错误包含字段路径与变量名。若使用字面 API key，配置加载器应检查文件权限并给出安全警告；打印有效配置时统一掩码。

`agent` 不提供 `mode`。ReAct 是所有 Task 共用的执行机制，Plan 是统一 Engine 按需生成的 ExecutionGraph，Team 是 Task placement；三者都不是可切换的 Agent 实现。默认由 Plan-on-Demand 规则选择 Direct/Planned，并允许执行中动态升级；`/plan` 只覆盖本次 Run 的 PlanningPolicy，强制展示和审核完整计划。

仓库提供可直接通过结构校验的 `configs/amadeus.example.yaml`。该文件可复制为 `<amadeus-root>/config.yaml`，默认不包含凭证或固定模型；实际运行时优先通过 `AMADEUS_API_KEY` 和 `AMADEUS_MODEL` 注入，避免把密钥提交到版本控制。

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

`amadeus config check` 执行完整配置主链并进行结构校验。默认 Amadeus 根目录优先读取 `AMADEUS_HOME`，未设置时使用解析符号链接后的可执行文件所在目录；不使用当前工作目录。显式 `--config` 不依赖根目录。校验成功返回 0，并只输出配置路径和默认 Provider；加载或校验失败返回非零状态，错误包含字段路径但不输出 API key。使用 `go run` 开发时，可执行文件位于临时目录，如需读取仓库配置应显式设置 `AMADEUS_HOME`。

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

用户和项目通过 `AGENTS.md` 提供明确、可编辑、可审查的持久指令，不开放任意内置 Prompt 覆盖。Assembler 负责变量校验、指令来源清单、作用域解析和最终 hash，便于 trace 与测试。具体发现与优先级规则见 16.3、16.4。

## 14. 工具体系

### 14.1 MVP 内置工具

1. `read_file`
2. `write_file`
3. `list_dir`
4. `glob_files`
5. `grep_code`
6. `execute_command`

随后迁移：`create_project`、`search_code`、`web_search`、`web_fetch`、`revert_turn`、Browser、Memory、Skill 和 MCP 动态工具。

### 14.2 执行流水线

```text
lookup → schema validation → policy precheck → approval
       → snapshot hook → execute → post-edit hook → audit → result normalization
```

### 14.3 并发规则

- 默认只并行执行模型在同一响应中发起、且工具声明为 `ParallelSafe` 的调用。
- `write_file`、`execute_command`、`revert_turn` 默认不与其他有副作用工具并行。
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

M2-22～M2-25 已在 `internal/tool/builtin` 实现首批固定 Root 文件工具。`read_file` 只读取大小预算内的 UTF-8 regular file，支持零基 line offset/limit，并明确报告 partial/总行数；`write_file` 自动创建父目录，在目标同目录写临时文件并原子 rename，限制输入大小且保留已有权限；`list_dir` 按名称稳定排序，默认隐藏 dot entry，并受 entry budget 限制；`glob_files` 支持 slash glob 和 `**`，默认忽略 VCS、vendor、node_modules 和常见 build tree，输出稳定 project-relative 路径并标注截断。所有工具均使用严格 JSON 参数且不读取当前工作目录。

M2-26～M2-27 已实现 `grep_code` 的统一语义层。纯 Go fallback 稳定遍历项目文本文件，跳过 hidden/VCS/build tree、binary 和超大文件，支持 literal/regex、case sensitivity、行号、上下文和结果上限。若 `rg` 可用，则只用 `--files-with-matches` 快速筛候选文件，再由同一 Go scanner 生成最终结果，因此 fast path 与 fallback 的输出、行号、context 和 partial 语义一致；`rg` 不存在、失败或候选输出超限时自动回退，context 取消不会被回退掩盖。

M2-28～M2-30 已实现 `execute_command`。命令通过固定 `project.Root` 下的 project-relative cwd 启动，stdout/stderr 写入同一个并发安全 writer，以实际到达顺序生成 combined output；非零退出同时返回结构化 exit code 和已产生输出。每次调用使用受全局上限约束的 timeout；Unix 平台为 shell 创建独立进程组，取消或超时时终止整组，其他平台明确退化为 `exec.CommandContext` 能力。输出同时受 byte/line budget 限制，仍统计原始总字节/行数，截断、timeout 和 cancel 均返回 partial Result，供 Observation/Evidence 保留。

M2-31 已提供 `builtin.DefaultMVPOptions`、`RegisterMVP/NewMVPRegistry` 和稳定 `MVPSpecs`，集中装配 `read_file/write_file/list_dir/glob_files/grep_code/execute_command`。`amadeus tools list` 不需要读取 Provider 配置即可按稳定顺序展示六个工具的 side effect、ParallelSafe、Idempotent 和 resource mode，供用户与后续 Bootstrap 检查实际工具面。

M2-32 已把资源感知的有界并发执行器接入 ReActRunner。只有 SideEffect 为 none/read、声明 ParallelSafe 且非 exclusive 的连续调用组可以并行；argument resource strategy 从规范化 JSON pointer 提取资源键，同键调用串行，write/execute/network/unknown/exclusive 调用作为前后屏障。Worker 数受 MaxParallelTools 限制，取消后不启动剩余调用，Observation/Evidence 和 ToolResult 始终恢复为原始 model call 顺序。

M2-33 已增加临时 Go 项目的 Direct Engine 端到端测试。测试使用真实 `project.Root`、MVP Registry、ArgumentValidator、ToolExecutor、ReActRunner、DeterministicVerifier、Reflector 和 Engine Event Sink，按模型脚本实际执行 `read_file → write_file → execute_command(go test ./...) → Candidate`。测试命令成功 Evidence 显式关联 required criterion，Verifier passed、Reflector accept 后 Task/Run 才 completed，并断言文件真实修改、Steps/Evidence/iterations 完整及 terminal event 存在。至此 M2 的单 root Agent Engine 出口已实现。

### 15.2 命令策略

- 命令经 tokenizer/保守规则检查，不只依赖字符串包含判断。
- 明显破坏性命令在 HITL 前快速拒绝。
- 其他有副作用命令进入审批。
- `exec.CommandContext` 负责取消，输出按字节和行数限制。

### 15.3 审批与审计

- Approval Request 包含工具、规范化参数、风险级别和原因。
- CLI/TUI/API 提供不同 Handler，Runtime 只依赖接口。
- 审计为 JSONL，记录时间、会话、工具、结果、审批来源和耗时。
- API key、Authorization header、图片二进制和完整敏感正文必须脱敏或省略。

## 16. 上下文与显式指令

### 16.1 上下文预算

- 预算分为 system、instructions、history、tool results、resources 和 output reserve。
- 优先裁剪历史大工具结果与旧图片 payload。
- 压缩产生摘要消息，并保留摘要覆盖的消息范围和 hash。
- 模型 tokenizer 不可用时使用可替换的保守估算器。
- ContextView 是单次 LLM 请求的动态投影，不是持久化记录；原始 Conversation、RunState 和指令文件不因裁剪或压缩而删除。

### 16.2 数据生命周期边界

Amadeus 不采用 PaiCLI 式全能 `MemoryManager`，也不在近期版本实现由模型自动推断用户偏好、项目事实或可复用 Lesson 的 Durable Memory。自动长期记忆需要额外解决误判、冲突、过期、敏感信息、检索排序和可解释删除等问题，工程成本高且对编码 Agent 的实际收益不稳定。用户希望长期生效的偏好和项目规范改由显式 `AGENTS.md` 表达。

系统必须区分以下数据：

| 数据 | 语义 | 所有者 | 持久化方式 |
|---|---|---|---|
| Conversation | 用户与 Assistant 的正式可见消息历史 | `internal/conversation` | ConversationStore |
| Run Working State | Graph、Task、Step、Observation、Evidence、Reflection、Budget | `internal/agent/engine` | Checkpoint Store |
| ContextView | 当前一次 LLM 调用实际看到的受预算投影 | `internal/context` | 不单独持久化 |
| InstructionDocument | 用户和项目主动维护的长期指令 | `internal/instruction` | `AGENTS.md` 文件 |
| Reflection | 当前 Run 的 accept/retry/replan/ask_user/abort 判断 | `internal/agent/reflect` | 默认只进入 RunState/Checkpoint |

必须保持以下边界：

- ToolResult 首先转为 Observation/Evidence，不自动生成长期偏好或项目规则。
- Token Budget 属于 Context/Run Policy，不属于 Conversation 或 Instruction。
- Conversation Compaction 只生成 Context Summary，不删除正式 Conversation 记录。
- Checkpoint 只保存可恢复的 RunState，不替代 `AGENTS.md`。
- Reflection 默认只影响当前 Run，不自动沉淀为跨 Run Lesson。
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

### 16.4 发现、作用域与优先级

Instruction Resolver 先读取可选的 `$AMADEUS_HOME/AGENTS.md`，再从 `project.Root` 沿目标路径逐层发现 `AGENTS.md`。目录级文件的作用域是其所在目录树；操作某个文件或目录前，必须使用覆盖该目标的完整指令链，不能只加载启动目录后忽略更深层规则。对项目根执行的命令至少应用用户级与项目根级指令；若命令 cwd 位于子目录，则继续应用从项目根到该 cwd 的目录级指令。

冲突时从高到低为：

1. Amadeus 内置安全边界、Tool Policy 和 Approval Policy。
2. 用户当前明确提出的任务要求。
3. 距离目标路径最近的目录级 `AGENTS.md`。
4. 更上层目录和项目根 `AGENTS.md`。
5. `$AMADEUS_HOME/AGENTS.md`。
6. Amadeus 默认行为。

项目级指令因此高于用户级指令，更深目录高于更浅目录；当前用户请求可以覆盖普通工程约定，但不能绕过安全和审批。Assembler 不把多层文件静默拼成无来源文本，而是使用独立结构化包络注入，并保留稳定顺序、路径、scope 和 hash，便于诊断、缓存和审计。无法确定冲突含义时应向用户提问，而不是让模型猜测。

### 16.5 Conversation 与 ContextView

```go
type ConversationStore interface {
    Append(ctx context.Context, message MessageRecord) error
    List(ctx context.Context, threadID ThreadID) ([]MessageRecord, error)
}

type ContextBuilder interface {
    Build(ctx context.Context, input ContextInput) (ContextView, error)
}
```

ConversationStore 保存完整、按序、用户可见的消息记录。ContextBuilder 根据当前 Goal/Task、最近 Conversation、Conversation Summary、适用的 InstructionDocument、Run Evidence、内置 Prompt 和工具定义构造 ContextView；裁剪、压缩和检索只改变 View，不修改原始消息或指令文件。

ContextBuilder 注入的数据必须保留类别和来源，使模型能区分当前用户任务、用户级指令、不同目录作用域的项目指令、历史对话、Run Evidence 和外部资源内容。Conversation Summary、MCP resources、Skill 与 Web 内容不得伪装为 system、`AGENTS.md` 或本轮用户指令。

### 16.6 Reflection 与 Checkpoint

Reflection 只产生当前 Run 所需的结构化 verdict、issues、evidence gaps、next action 和简短说明，用于 accept/retry/replan/ask_user/abort。它可以随 RunState 写入 Checkpoint 以支持恢复，但第一版不自动提炼、检索或跨 Run 注入 Lesson，也不保存 chain-of-thought。

Checkpoint Store 保存 Graph、Task、Step、Evidence、Budget、pending action 和恢复所需的 Conversation 引用。恢复时重新解析当前有效 `AGENTS.md`，并比较保存的 instruction hash；规则发生变化时应记录差异并重新验证待执行动作，不能把旧指令快照静默当作当前规则。

### 16.7 实施时机

M3 按顺序实现内置 Prompt、InstructionDocument/Resolver、用户级与目录级 `AGENTS.md`、ConversationStore、ContextBuilder/Compactor 和 Checkpoint。ReActRunner 只消费已经构造好的 ContextView，不直接扫描指令文件、保存 Conversation 或执行压缩，确保执行循环不依赖文件发现和持久化细节。

Durable Memory、自动偏好提取、Memory SQLite、MemoryRetriever、remember/recall 工具和跨 Run Reflexion Lesson 不进入当前路线图。只有真实使用证明 `AGENTS.md`、Conversation 和 Checkpoint 无法满足明确需求时，才以可选 ADR 重新评估；任何未来方案都必须默认显式写入、可查看、可删除、可追踪来源，并且不能自动把模型推测提升为用户指令。

## 17. MCP、Skill 与扩展

### 17.1 MCP

- 支持用户级和项目级配置合并。
- 首批迁移 stdio transport、initialize、tools/list、tools/call。
- 第二批迁移 streamable HTTP、resources、notifications 和 mentions。
- 动态工具命名为 `mcp__{server}__{tool}`，注册和卸载使用原子快照。
- MCP 进程和连接必须绑定 session 生命周期并可取消。

### 17.2 Skill

- Skill 元数据、内容、启用状态和上下文缓冲相互分离。
- 内置、用户级、项目级 Skill 按固定优先级加载。
- 只把索引和本轮选中的必要内容注入 Prompt。

## 18. Snapshot、LSP、Browser 与图片

- Snapshot：工具写操作前后创建 turn snapshot，支持 `revert_turn`。
- LSP：写文件后的诊断通过异步 hook 发布，不阻塞文件已成功写入的事实。
- Browser：连接、会话、敏感页面策略和审计分离。
- 图片：本地图片先校验类型、尺寸与上限，再压缩/缩放；历史轮次只保留文本元信息。

这些能力均通过 Tool 或 Hook 接入 Runtime，不能反向依赖 CLI。

## 19. 事件与渲染

Runtime 发布结构化事件：

- `TurnStarted` / `TurnCompleted`
- `TextDelta` / `ReasoningDelta`
- `ToolCallStarted` / `ToolCallCompleted`
- `ApprovalRequested` / `ApprovalResolved`
- `UsageUpdated`
- `StatusChanged`
- `DiagnosticPublished`
- `ErrorOccurred`

Renderer 订阅事件。这样 plain CLI、inline CLI、TUI 和 Runtime API 可以共享完全相同的 Agent 行为。

M1-09 将事件协议落在 `internal/agent/event`。`Event` 是带稳定 `Type()` 的类型化 value interface，`Sink` 仅暴露 `Publish(context.Context, Event) error`；Runtime 不直接依赖 stdout、Renderer 或持久化实现。M1 首批事件为 `TurnStarted`、`TextDelta`、`ReasoningDelta`、`UsageUpdated`、`TurnCompleted` 和 `ErrorOccurred`，并预留工具、审批、状态和诊断事件类型，具体 payload 随对应里程碑增加。

`ErrorOccurred` 使用值类型 `ErrorInfo`，只复制 Domain `ProviderError` 的分类、status、code、param、request ID 和安全 message，不把 SDK error/cause 传给 Renderer。`MemorySink` 使用互斥锁按成功 `Publish` 的先后顺序保存事件，`Snapshot` 返回独立切片，供 Runtime/session 测试验证事件顺序；取消的 context、nil event 和未知 event type 会被拒绝。

M1-11 在 `internal/render` 实现 `PlainRenderer`，它直接实现 `event.Sink`，只把 `TextDelta` 原样写入 stdout；`ReasoningDelta`、usage 和状态事件默认不展示。一个 turn 输出过文本后，`TurnCompleted` 补一个换行；空回答不会产生多余空行。

`ErrorOccurred` 以单行 `error: <message>` 写入 stderr，并折叠 Provider message 中的换行等空白，避免破坏终端输出结构。若错误前 stdout 已有部分文本，Renderer 先结束当前 stdout 行，再写 stderr。stdout/stderr Writer 错误和取消 context 都向调用方返回；Renderer 使用互斥锁避免并发事件写入时交错。

M1-12 在 `internal/interface/cli` 实现基础 `ChatLoop`，使用逐行 Reader 接收输入，忽略空行，以 `/exit` 正常退出，并在 Ctrl+D/管道 EOF 时返回成功；EOF 前没有换行的最后一条输入仍会执行。Loop 生成进程内递增 turn ID，并调用同一个 Runtime Session，因此成功轮次会形成最小多轮历史。Provider 错误已由事件/Renderer 输出，Loop 保持可继续读取下一条输入；Reader、Renderer 或其他基础设施错误则终止命令。

`amadeus chat` 执行完整配置主链和校验，选择 default Provider，创建 OpenAI Adapter、Plain Renderer、Runtime Session 与 ChatLoop。基础 plain 模式不打印 banner 或输入提示符，保持终端和管道输出一致；更丰富的交互提示、history 和补全留在 M7。

M1-13 为每个 turn 创建独立子 context。生产命令通过 `signal.NotifyContext(parent, os.Interrupt)` 只在模型请求执行期间监听 Ctrl+C，并在该轮返回后立即停止监听；因此 Ctrl+C 会取消当前 SDK HTTP/SSE 调用，但不会取消 ChatLoop 的父 context。父 context 被外部取消时仍会终止整个循环。

取消沿 `ChatLoop → Session → llm.Client → SDK transport` 传播。Session 关闭流、发布安全的 `ErrorOccurred(request cancelled)`，不提交本轮 user/assistant 历史；Plain Renderer 结束可能存在的部分 stdout 行并把取消信息写入 stderr；ChatLoop 识别单轮 `context.Canceled` 或 `ProviderError(cancelled)` 后继续读取下一条输入。测试通过可注入的 turn context factory 模拟 Ctrl+C，不向测试进程发送真实信号。

M1-14 使用 `httptest.Server` 建立命令级 Provider mock 集成测试，不替换 OpenAI Adapter 或 Runtime。测试从临时 Amadeus `config.yaml` 启动真实 `amadeus chat` 命令链，覆盖配置加载、default Provider 选择、Bearer 认证、SDK 请求、SSE 解码、Session 聚合、事件发布和 Plain Renderer 输出。

Responses fixture 断言 `/v1/responses`、`model/input/temperature/max_output_tokens/stream`；Chat Completions fixture 断言 `/v1/chat/completions`、`model/messages/temperature/max_tokens/stream` 和 `stream_options.include_usage`。两种模式均返回流式 `hello` 和 usage，命令输出一致为 `hello\n`，且 stdout/stderr 不包含 API key。所有测试只访问进程内本地 server，不依赖公网或真实 Provider 凭证。

M1-15 于 2026-07-29 使用项目根目录真实配置完成 Chat Completions compatible Provider smoke test。测试通过 `AMADEUS_HOME=<project-root>` 启动构建产物，发送只要求返回固定标记的最小输入，真实流式输出为 `AMADEUS_SMOKE_OK`，命令状态为成功。Provider 名称、API 模式和模型可以记录，但不记录 base URL、API key 或请求/响应正文；本次 Provider/SDK 流未向 plain 命令暴露 HTTP request ID，因此 smoke 记录明确标记为 unavailable，而不使用响应正文或内部地址代替。

根目录运行配置 `config.yaml` 可能包含真实凭证，必须由 `.gitignore` 排除；仓库只提交 `configs/amadeus.example.yaml`。真实 smoke test 前后都使用 `config explain` 确认 API key 输出为 `[REDACTED]`。

## 20. 持久化

| 数据 | 默认实现 | 位置 |
|---|---|---|
| Amadeus 主配置 | YAML | `<amadeus-root>/config.yaml` |
| 用户级指令 | Markdown | `<amadeus-root>/AGENTS.md` |
| 项目/目录级指令 | Markdown | `<project-root>/**/AGENTS.md` |
| CLI history | 文本或 SQLite | `~/.local/share/amadeus/history` |
| 审计 | JSONL | `~/.local/state/amadeus/audit/` |
| Conversation | SQLite 或可替换 Store | OS-aware data directory |
| Run Checkpoint | SQLite 或可替换 Store | OS-aware state directory |
| 后台任务 | SQLite | `~/.local/share/amadeus/tasks.db` |
| 用户 Skill/MCP | 文件 | `<amadeus-root>/` 下的对应目录 |
| 项目 Skill/MCP | 文件 | `<project>/.amadeus/` |

路径通过 OS-aware directory resolver 生成；测试中注入临时目录。

## 21. 错误处理与可观测性

- 使用 `fmt.Errorf("...: %w", err)` 保留错误链。
- 定义少量稳定错误类别：配置、认证、限流、网络、取消、策略、工具和协议。
- 用户错误与内部错误分开展示；debug 模式才输出堆栈或详细 payload。
- LLM trace 默认关闭；开启后仍必须脱敏。
- 每个 turn、LLM request 和 tool call 生成关联 ID。
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
- 使用 SQLite 临时库验证任务恢复。

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

## 23. 迁移阶段

| 阶段 | 交付目标 | 可执行出口 |
|---|---|---|
| M0 | 工程骨架与配置 | `amadeus version/config check` 可运行 |
| M1 | OpenAI SDK + 基础会话 | 可流式完成纯文本问答 |
| M2 | 统一 Engine + ReAct + Verification/Reflection + 核心工具 | 单 root Task 可在临时项目中读、改、测并经质量闭环完成 |
| M3 | 安全、Prompt、分层指令、上下文与恢复 | `AGENTS.md` 作用域生效，长运行受策略保护并可 checkpoint/resume |
| M4 | Adaptive Planning + Replan + Multi-Agent placement | Direct 可动态升级，`/plan`、`/team` 共用同一 Engine |
| M5 | MCP + Skill + Web | 外部工具和知识扩展可用 |
| M6 | Snapshot/LSP/Image/Browser | 高级编码工作流可用 |
| M7 | TUI + Runtime API + 后台任务 | 多入口共享 Runtime |
| M8 | 兼容回归与发布 | 形成可发布二进制和迁移说明 |

每个阶段的最小任务、依赖与验收见 `docs/migration-progress.md`。

## 24. 兼容性策略

### 24.1 必须兼容

- ReAct、计划审核、任务依赖和 SubAgent 协作的用户可见核心语义；内部不保留三套 Agent Engine。
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
| Reflection/Replan 自循环 | 成本失控、任务无法终止 | attempts、replan、步骤、token、工具次数和 wall-clock 全局上限 |
| 过度规划简单任务 | 延迟和 token 成本增加 | Direct 单 root Task 不调用 Planner，复杂度只在明确规则或运行证据下升级 |
| 模型自称完成但缺乏证据 | 错误修改被误报成功 | Candidate Result 必须经过确定性 Verifier 和结构化 Reflection |
| 本地安全边界被误解 | 用户风险 | 明确不是沙箱，默认 HITL 和审计 |
| TUI 过早消耗开发量 | 核心 Runtime 延迟 | Plain/inline CLI 先行，TUI 后置 |

## 26. 架构决策记录

### ADR-001：官方 SDK 隔离在 Adapter

- 决策：Domain 不引用 OpenAI SDK 类型。
- 原因：支持 base URL、兼容 API、测试替身和未来 SDK 升级。

### ADR-002：Responses 优先、Chat Completions 兼容

- 决策：Provider 配置显式指定 API 模式。
- 原因：兼顾 OpenAI 当前主接口和原项目多 Provider 兼容需求。

### ADR-003：统一 Agent Engine

- 决策：Plan-and-Execute、ReAct、Reflection 和 Multi-Agent 不是独立 Agent 模式；所有 Run 使用同一个状态机，Plan 提供任务结构，ReActRunner 执行单 Task，Verifier/Reflector 决定 accept/retry/replan，SubAgent 只是 Task placement。
- 决策：每个 Run 至少有一个 root Task；简单任务使用不调用 Planner 的 Direct 单节点图，复杂任务使用 Planned DAG，并允许 Direct 基于运行 Evidence 无损升级。
- 原因：避免 PaiCLI 中多套工具循环的行为漂移，同时让预算、取消、安全、恢复、事件和 Provider 协议只实现一次。

### ADR-004：结构化事件驱动 UI

- 决策：Runtime 不直接打印终端。
- 原因：同一运行时支持 CLI、TUI 和 HTTP event stream。

### ADR-005：Amadeus 根目录 YAML 配置与明确优先级

- 决策：默认读取 `<amadeus-root>/config.yaml`，环境变量和 flags 覆盖文件配置；目标工作项目不承载 Provider 主配置。
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

- 决策：`openai.Adapter` 实现 Domain `llm.Client` 并共享 SDK/HTTP/SSE 主链；Responses、Chat Completions 负责协议级转换，DeepSeek/Qwen/GLM 等只通过 Dialect hook 表达已验证差异。
- 决策：Dialect 由配置显式选择，标准方言作为默认回退，不根据 URL、Provider 名称或模型名猜测。
- 原因：避免复制完整 Provider Client，同时让工具调用、reasoning 和扩展字段差异可测试、可解释、可逐步增加。

### ADR-008：Evidence 驱动完成判断与触发式 Reflection

- 决策：模型无 tool call 只产生 Candidate Result；Task/Run 必须经过确定性 Verification 和结构化 Reflection 才能标记完成。
- 决策：Reflection 初期与 Planner/ReActRunner 使用同一模型，但仅在候选完成、验证失败、卡死、关键假设失效和计划结束等检查点触发，不在每个工具后无条件调用。
- 决策：Reflection 只保存 verdict、issues、evidence gaps、next action 和简短说明，不保存完整 chain-of-thought，也不在 M3 自动沉淀跨 Run Lesson。
- 原因：以测试、diff、诊断和结构化 Evidence 约束模型自评，同时控制额外调用成本。

### ADR-009：显式指令取代自动长期记忆

- 决策：不在当前路线图实现模型推断式 Durable Memory、MemoryStore/Retriever、自动偏好提取或跨 Run Reflexion Lesson；用户长期偏好写入 `$AMADEUS_HOME/AGENTS.md`，项目规范写入项目根和目录级 `AGENTS.md`。
- 决策：项目指令高于用户指令，更深目录高于更浅目录；当前用户请求高于普通工程约定，内置安全与 Approval 始终最高。每次注入保留 source/path/scope/hash。
- 决策：Conversation、ContextView、Reflection 和 Checkpoint 继续保持独立生命周期；Reflection 默认只在当前 Run 内使用，Checkpoint 恢复时重新解析并比对当前指令。
- 原因：显式文件可编辑、可审查、可版本控制且行为可预测，避免为收益不稳定的自动记忆承担误判、冲突、过期、隐私和检索复杂度。

### ADR-010：统一 Engine 不暴露 Agent Mode

- 决策：删除 `agent.mode` 及 `react/plan/team` 配置枚举；配置只保留预算、并发和其他真正影响 Policy 的参数。
- 决策：ReAct 是 TaskRunner，Plan 是 ExecutionGraph，Team 是 Task placement；`/plan` 仅为本次 Run 设置强制展示和审核策略。
- 原因：避免配置重新引入多套 Agent 的错误心智模型，并与 Plan-on-Demand、动态升级和统一状态机保持一致。

## 27. 已确认与待确认的实现决策

已确认：

1. 简单任务不调用 Planner，但每个 Run 内部都有一个不可见的 root Task。
2. Planner、ReActRunner 和 Reflector 初期使用同一个模型，并保持独立 Port。
3. `/plan` 只强制生成、展示和审核完整计划，不切换到另一套 Agent。
4. 默认采用 Plan-on-Demand：确定性规则先选 Direct/Planned，Direct 可基于运行 Evidence 动态升级。
5. Reflection 采用触发式检查，完成判断由 Verifier + Reflector 共同决定。
6. 不实现自动长期记忆；用户级、项目级和目录级 `AGENTS.md` 是跨 Run 指令的唯一基线来源。
7. `agent.mode` 不属于统一 Engine 配置，ReAct/Plan/Team 分别是 Task 执行、任务图和 placement 概念。

仍需在对应任务开始时固定：

1. 首个验收 Provider/API 模式。
2. 是否在 M2 就加入 inline renderer，还是先使用 plain renderer。
3. Side-Git 继续采用独立快照仓库，还是先实现轻量文件备份 MVP。
