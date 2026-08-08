# Amadeus 架构设计

> 状态：Draft v0.5
> 创建日期：2026-07-29
> 最近修订：2026-08-07
> 输入依据：`docs/thought.md`、PaiCLI、WeKnora 与 `../codex-main` Agent Runtime 源码研究
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

- 历史能力包含 ReAct、Plan-and-Execute 与 Multi-Agent；目标架构只保留统一 Reactor，并把计划和委派收敛为 Tool/Runtime 能力。
- OpenAI-compatible LLM 调用、流式输出、工具调用和多模态消息。
- 文件、代码搜索、Shell、Web、Browser、Memory、Skill 与 Turn/Run Diff 等能力。
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

这些历史路径共享 LLM、工具、记忆、Prompt、渲染、安全策略与取消机制。Go 版应将共享能力下沉到统一 Reactor、RunRuntime 与 Tool Runtime；原项目的 Plan-and-Execute 只作为历史分析，不进入目标主链。

### 2.3 原项目的主要结构问题

1. `cli/Main.java` 同时承担依赖装配、启动、自检、交互循环、命令分发和模式切换。
2. `tool/ToolRegistry.java` 同时承担工具声明、注册、参数解析、策略检查、并发执行以及大量具体工具实现。
3. 多个 Provider Client 重复维护 OpenAI-compatible 请求逻辑。
4. ReAct、历史 Plan task executor 与 SubAgent 存在重复的“请求模型—执行工具—回灌结果”循环。
5. 配置读取分散在 JSON、环境变量、`.env` 和系统属性中，优先级不够集中透明。
6. 一些模块直接依赖控制台输出或全局环境，增加单元测试和 Runtime API 复用难度。

## 3. 设计目标

### 3.1 功能目标

- 提供名为 `amadeus` 的单二进制 CLI。
- 使用 OpenAI 官方 Go SDK 作为主要模型访问实现。
- 通过配置文件切换 `base_url`、`api_key`、`model` 和 API 模式。
- 默认交付统一 Plan-guided ReAct 执行内核；复杂任务由模型按需使用 `update_plan` 维护可见清单，`/plan` 进入只分析不实施的 Plan Mode，Multi-Agent 作为后续工具化委派增强。
- 保持工具调用、流式输出、上下文取消、HITL 和审计能力。
- 面向多语言、多构建系统的软件项目，不把 Go、`gopls` 或任何单一语言工具链设为核心前提。
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
- 不把未实现 OS 级强制的平台描述为强隔离 Sandbox；Linux Bubblewrap 可提供 Sandboxed 执行，其他平台只能明确标记为 Unsandboxed 执行。
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
  ├── Tool Router/Handlers  Filesystem / Shell / Web / MCP / Browser
  ├── Context Manager ───── Prompt / Instructions / Canonical Rollout / Skills
  ├── Safety Pipeline ───── PathResolver / FileSystemPolicy / Sandbox / HITL / Audit
  └── Typed EventHub ────── Renderer / API stream / Audit / Trace log
```

## 6. 分层架构

### 6.1 Interface 层

负责协议适配，不包含 Agent 决策逻辑。

- `cmd/amadeus`：命令行入口；根命令直接启动 Coding Agent，不设置独立 `run` 子命令。
- `internal/interface/cli`：交互循环、slash command、补全和 history。
- `internal/interface/tui`：默认 Rich Inline TUI 与 Plain fallback。
- `internal/interface/httpapi`：Session、Run 和事件流 API。
- `internal/render`：plain、inline、TUI/API event renderer。

根命令语义固定为：

| 调用 | 行为 |
|---|---|
| `amadeus` | stdin 为 TTY 时，以当前工作目录为目标项目并进入交互式 Coding Agent |
| `amadeus "<task>"` | 在当前工作目录执行一次性 Coding Agent 任务，完成后退出 |
| `amadeus --project <path>` | 以显式项目目录进入交互模式 |
| `amadeus --project <path> "<task>"` | 在显式项目目录执行一次性任务 |
| `amadeus --add-dir <path>` | 为当前进程增加一个附加 Workspace Root；可重复使用，不改变 Project.RootPath 或默认 CWD |
| `amadeus --continue` | 恢复当前项目最近活跃的 Session |
| `amadeus --resume` | 打开当前项目 Session 选择器；取消选择则返回当前对话或 Draft Session |
| `amadeus --resume <session-id>` | 直接恢复当前项目内指定的 Session |
| `amadeus sessions list` | 列出当前项目的 Session；首版不提供 `--all` |
| `printf '%s\n' '<task>' \| amadeus` | 非 TTY 时从 stdin 读取一次性任务 |

`--help` 和已注册子命令继续按 CLI 语义处理；`version`、`config`、`tools` 等管理/诊断入口不进入 Coding Agent。目标架构删除独立 `amadeus chat` 与 `ChatSession`；若未来需要纯聊天入口，也必须复用 SessionRuntime 与无状态 LLMRuntime，而不是恢复第二套历史。无位置参数、stdin 非 TTY 且读取不到有效任务时返回明确错误。`AMADEUS_HOME` 只解析配置与用户级 `AGENTS.md`，目标项目默认来自启动工作目录，也可由 `--project` 覆盖。

交互模式使用 `/resume` 打开同一个当前项目 Session 选择器；用户按 `Esc` 时不切换 Session 并回到当前对话，因此首版不增加重复的 `/sessions` 命令。`--resume` 只表示恢复 Session，不接受 Run ID。`amadeus` 启动时只建立内存 Draft Session，打开 slash command palette、执行 `/resume`、`/status`、`/skills`、`/mcp`、`/copy`、`/clear`、EOF 或未提交任何真实任务的进程不会写入空 Session；第一条真实用户消息到达时才原子创建 Project、Session、Run 与第一条 `user_message` RolloutItem。`/clear` 对已有 Session 执行 Codex 语义：清空终端和瞬态 UI 后切换到新的 Draft Session，旧 Session 不删除且仍可通过 `/resume` 恢复。

每条非空用户输入先交给 SessionRuntime；它通过 SessionCoordinator 原子创建 Run 与首条 user RolloutItem，再构造 RunContext 和可取消 RunRuntime。交互模式中的取消只终止当前 Run，父 SessionRuntime 继续存在并重新接受输入；一次性模式中的取消映射为退出码 130。CLI/Application 只负责生命周期和结果映射，不以 `Turn`、Plan Task 或 Provider Call 代称 Run。

### 6.2 Application 层

负责用例编排和生命周期。

- `internal/app/bootstrap`：加载配置并装配依赖。
- `internal/app/session`：SessionRuntime 生命周期、slash command、resume 和取消。
- `internal/app/command`：本地命令处理。
- `internal/app/runtime`：RunRuntime 启动与结果映射；后台任务后置。

### 6.3 Domain/Runtime 层

包含 Agent 的稳定业务模型。

- `internal/agent/react`：独立 Reactor；按 Think、Analyze、Act、Observe 组织模型/工具循环，不依赖 Plan、Graph 或 Task Domain。
- `internal/agent/plan`：轻量 `PlanState/PlanItem` 与 `update_plan` Tool；只维护模型可见清单和事件投影，不调度 Reactor。
- `internal/agent/runtime`：`RunContext/RunRuntime/RunState/RequestContext`、取消/收尾与 `LLMRuntime`；不得用 Turn 或 Plan Task 代称用户 Run。
- `internal/agent/event`：Runtime 强类型事件、Metadata Record、同步 Publisher、Fanout 与 UI/API channel subscription adapter。
- `internal/agent/team`：把独立只读 Task 委派给最多两个临时 SubAgent；SubAgent 仍复用同一个 Reactor。
- `internal/session`：持久化 Domain、SessionRuntime、SessionHistory、RolloutAppender 与 SQLite Store。
- `internal/context`：为每次模型采样构建 RequestContext 和受预算约束的 RequestView。
- `internal/instruction`：用户级、项目级和目录级 `AGENTS.md` 的发现、作用域、优先级与来源追踪。
- `internal/prompt`：Prompt 分层装配。
- `internal/tool`：工具协议、注册表和执行器。

### 6.4 Infrastructure 层

负责外部系统实现。

- `internal/llm/openai`：官方 OpenAI Go SDK Adapter。
- `internal/config`：配置文件、环境变量和 CLI override 合并。
- `internal/logging`：结构化日志初始化、级别过滤和敏感属性脱敏。
- `internal/store`：SQLite canonical rollout、文件存储与独立 Audit JSONL Sink。
- `internal/mcp`、`internal/websearch`、`internal/webfetch`、`internal/browser`。
- `internal/skill`、`internal/diff`。
- `internal/policy`：路径、命令、审批和审计实现。

## 7. 建议目录结构

```text
amadeus/
├── cmd/amadeus/
├── configs/amadeus.example.yaml
├── docs/
│   ├── thought.md
│   ├── design.md
│   └── development-progress.md
├── internal/
│   ├── app/
│   │   ├── bootstrap/
│   │   ├── command/
│   │   └── runtime/
│   ├── session/
│   │   └── sqlite/
│   ├── agent/
│   │   ├── runtime/
│   │   ├── react/
│   │   ├── plan/
│   │   ├── event/
│   │   └── team/
│   ├── context/
│   ├── instruction/
│   ├── prompt/
│   ├── llm/
│   │   └── openai/
│   ├── tool/
│   │   ├── builtin/
│   │   └── patch/
│   ├── process/
│   ├── policy/
│   ├── mcp/
│   ├── skill/
│   ├── diff/
│   ├── workspace/
│   ├── websearch/
│   ├── webfetch/
│   ├── interface/
│   │   ├── cli/
│   │   └── tui/
│   └── render/
├── prompts/
├── go.mod
└── go.sum
```

初期实现不创建空包。`internal/session` 拥有持久化 Domain、SessionRuntime、SessionHistory 与 RolloutAppender，`internal/agent/runtime` 拥有 RunContext/RunRuntime/RunState/RequestContext 与 LLMRuntime，`internal/agent/react` 拥有唯一执行循环，`internal/agent/plan` 只保存 PlanState 与 `update_plan`。旧 `engine/reflect`、DAG Planner 和第二套执行器不进入目标目录。

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
type ToolHandler interface {
    Spec() ToolSpec
    SupportsParallelToolCalls() bool
    Handle(context.Context, ToolInvocation) (ToolOutput, error)
}

type ToolCall struct {
    ID      string
    Name    ToolName
    Payload ToolPayload
}

type ToolInvocation struct {
    SessionID SessionID
    RunID     RunID
    Call      ToolCall
    Source    ToolCallSource
    Request   *RequestContext
}

type ToolOutput struct {
    Content   []ContentPart
    Metadata  map[string]any
    Partial   bool
    Artifacts []ArtifactRef
}

type ToolCallOutcome struct {
    Status   ToolCallStatus
    Duration time.Duration
    Error    *ToolError
}

type ToolExecution struct {
    Call    ToolCall
    Output  ToolOutput
    Outcome ToolCallOutcome
}

type ToolSpec struct {
    Name        ToolName
    Description string
    InputSchema json.RawMessage
}
```

`ToolHandler` 对齐 Codex Handler/CoreToolRuntime 语义：Spec 与可执行实现绑定注册，`Handle` 是一个 Tool 的完整入口，复杂度留在具体 Handler 内部，而不是强迫所有 Tool 经过通用 `Prepare → PreparedToolCall → Authorizer → Execute`。简单 Tool 直接处理 Invocation；文件 Tool 调用 FileSystemPolicy；`apply_patch` 使用私有 PreparedPatch；`execute_command` 使用私有 ExecRequest 与 ToolOrchestrator。

`ToolRouter` 是唯一分发入口，负责把 Provider Tool Call 转为 ToolInvocation、查询 ToolRegistry、检查可见性、对 Function Payload 执行唯一一次 repair/parse/Schema validation、根据 Handler 的 `SupportsParallelToolCalls` 做有界调度、调用 Handler，并将 ToolOutput 投影给模型、UI、Audit 与 Rollout。目标架构不再同时维护 ToolDispatcher、ToolExecutor 和 ToolExecutionGate 三组相近语义。

`SupportsParallelToolCalls()` 与 Codex 对齐，默认返回 false。返回 true 的连续调用可在 `max_parallel_tools` 范围内并行；返回 false 的调用等待此前并行调用完成，并阻止后续调用进入，直到自身结束。首版读取、搜索、图片、Web 和明确只读的 MCP Handler 可返回 true；`apply_patch`、`execute_command`、`write_stdin`、未知 MCP Handler 和所有无法证明安全并行的调用返回 false。不建立资源级文件锁、TargetStrategy、Shared/Exclusive 枚举或第二调度器。

`TargetStrategy` 与 `PathGuard` 从目标架构删除。文件 Handler 直接依赖统一 FileSystemPolicy；FileSystemPolicy 内部使用 PathResolver 完成相对路径锚定、lexical normalize、canonicalize、符号链接/文件类型验证，再依据 Effective Permission Profile 作唯一权限决策。删除这些包装不删除任何路径安全能力。

`ToolOutput` 是 Handler 返回并进入 canonical rollout 的唯一结果内容事实，投影为四个受控视图：

1. Model Projection：转换为 `tool_result`，回灌模型并进入 canonical rollout；
2. UI Projection：生成安全、有限的 Tool Action Summary 与专用事件；
3. Audit Projection：记录参数摘要、审批、状态、时长和副作用；
4. Domain Projection：`apply_patch` 的 exact delta 进入 RunDiffProjector，其他 Handler 使用各自明确投影。

`ToolCallOutcome` 只表达调用生命周期的 `completed/failed/denied/interrupted`、Duration 与 Error，用于事件、UI 和 Audit；它不是第二份工具内容。普通参数错误、Permission Required、审批拒绝、命令非零退出、超时和用户中断都必须转换为模型可见 ToolOutput 与对应 ToolCallOutcome，只有无法安全构造协议结果的内部故障才返回 fatal Go error。`Partial` 属于 ToolOutput，表达失败或中断前可能已经发生部分副作用。

M2-01～M2-03 已有的 Tool/PreparedCall/Result/Executor 是历史实现基础；目标重构收敛为 ToolCall、ToolPayload、ToolInvocation、ToolSpec、ToolHandler、ToolRegistry、ToolRouter、ToolOutput 与 ToolCallOutcome。工具输入使用 JSON Schema Draft 2020-12 校验，禁止外部 `$ref`；受限 repair 处理尾随逗号和字符串完整时缺失的 `}`/`]`，repair 后仍必须重新通过严格 JSON 解析和 Schema 校验。参数 repair、严格解析和 Schema 校验只能在 ToolRouter 中执行一次；Reactor Analyze 只区分 final/tool-calls/provider-argument-error，不得再次校验同一参数，也不得因为同批一个调用非法而取消其他合法调用。

目标 Tool 调用主链为：

```text
LLM aggregated Tool Call
        ↓
ToolRouter
├── Registry lookup + visibility check
├── strict JSON parse
├── invalid → bounded conservative repair → strict parse again
└── exactly-once JSON Schema validation
        ↓
normalized ToolInvocation
        ↓
SupportsParallelToolCalls scheduling
        ↓
ToolHandler.Handle
├── simple/internal → execute directly
├── filesystem → FileSystemPolicy → execute
├── apply_patch → private PreparedPatch → preflight/stage/revalidate/commit
├── execute_command → private ExecRequest → ToolOrchestrator/Approval/Sandbox
└── MCP/Web → corresponding Manager/Provider boundary
        ↓
ToolOutput + ToolCallOutcome
        ↓
Events / Audit / RunDiffProjector / canonical rollout
        ↓
Provider tool_result → next Reactor Iteration
```

这条主链仍保留三个安全概念：

1. **Permission**：决定规范资源是否允许读写，由 `FileSystemPolicy` 在 Prepare 中完成；
2. **Operation Approval**：决定是否允许本次无法被 Sandbox 充分约束的高风险操作，主要服务 Unsandboxed `execute_command`；
3. **Sandbox**：在 OS 层强制约束子进程实际可访问的文件系统与进程能力。

Permission 通过不代表 Operation Approval 通过；Operation Approval 命中缓存也不能跳过 Permission；Sandbox 是执行强制层，不负责解释 Tool JSON 或替代 FileSystemPolicy。区别只在于这些能力由真正需要它们的 Handler 显式调用，不再经过一个所有 Tool 共享的 Authorizer/PreparedCall 管道。

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
- repair 后跳过 JSON Schema、FileSystemPolicy、ExecPolicy、Sandbox、Permission 或 Operation Approval。

规范化后的 arguments 必须统一用于 ToolInvocation、参数 hash、审计和持久化 Tool Call，不能执行修复后的参数却把原始非法 JSON 回灌给 Provider。严格解析、repair 或 Schema 校验失败时不得调用 Handler；ToolRouter 将每个调用的错误独立转换为模型可见 ToolOutput 与失败 ToolCallOutcome，允许下一轮有限纠正，并由 ProgressMonitor 阻止相同调用和相同结果无限重复。

MVP 删除核心主链中的 PreExecutionHook、PostExecutionHook 与 PostWriteHooks。AuditRecorder、ToolEventPublisher 和 RunDiffProjector 使用显式接口，不伪装成通用插件 Hook；RunDiffProjector 只消费 ApplyPatchOutput 中的 exact delta。未来真正需要用户/项目 Hook 时，再建立独立 Hook Runtime 并对齐 Codex：PreToolUse 可阻止或重写输入，但重写后必须重新走 ToolRouter 校验；PostToolUse 只能阻止/替换模型可见输出或添加上下文，不能撤销已经发生的副作用。

该 repair 仅属于 Tool/MCP 参数协议容错，不适用于 PlanState、Plan Mode 文本或任何业务语义。目标架构不解析 Planner JSON，也不根据模型输出构建 DAG。

#### 8.3.1 `apply_patch` 目标设计

`apply_patch` 是 Amadeus 的首选结构化文件修改能力；`execute_command` 可以运行项目脚本和工具，但不应成为普通文本修改的默认路径。目标实现保留 JSON Tool 外壳中的 `patch` 文本参数，不为追求形式一致强制迁移成 Codex Freeform Grammar，也不允许 Shell 中出现的同名命令绕过结构化 Tool Runtime。Prompt、Parser、Executor、Permission、Diff 和 UI 必须共用同一 Patch 语义。

目标执行链为：

```text
normalized apply_patch ToolInvocation
        ↓
parse complete Patch Document
├── Begin/End Patch framing
├── Add / Update / Delete / Move operations
├── hunks and optional End of File marker
└── size / operation / hunk / line budgets
        ↓
ApplyPatch.Prepare
├── resolve every source and destination through FileSystemPolicy
├── reject Denied/ReadOnly/ungranted targets before reading payload
├── read original bytes and bind file identity
├── locate every hunk with bounded unique-match ladder
├── calculate complete new contents and exact deltas
└── full-document preflight → private immutable PreparedPatch
        ↓
stage all writable outputs
        ↓
before first commit: bytes/identity revalidation
        ↓
ordered commit
├── success → ApplyPatchOutput with exact deltas
└── failure after side effect → Partial=true + committed operation metadata
        ↓
RunDiffProjector / UI patch summary / model replay
```

以下确定性保护必须保留：

- 一个 hunk 匹配零处时失败，匹配多处时同样失败，绝不由 Runtime 猜测“最可能的位置”；
- `Add File` 目标已存在时拒绝，`Move` 目标已存在时拒绝，首版不提供静默覆盖语义；
- 整个 Patch Document 在首次副作用前完成路径授权、原文件读取、hunk 定位和结果计算，不能边解析边修改；
- Prepare 与 Commit 之间重新读取或校验文件 identity/bytes，发现外部修改时返回 `target_stale`，不能在旧授权结果上静默重解析；
- 输出先写入同文件系统 staging，再通过受控 rename/replace 提交；发生部分提交时必须准确设置 `Partial` 并列出已提交 operation；
- Parser 和 Executor 继续设置明确的 Patch 大小、operation、hunk、line 与目标文件大小预算，超限返回普通 ToolOutput，而不是耗尽 Run 内存。

匹配容错采用“逐级放宽但每级都必须唯一”的有界策略：

```text
exact line sequence
    ↓ no match
line-ending / trailing-whitespace normalized sequence
    ↓ no match
optional conservative Unicode punctuation normalization
    ↓
unique match → accept
zero or multiple matches → reject
```

Unicode 标点归一化只覆盖可枚举的等价标点，不做自然语言改写、模糊相似度搜索或跨块重排；任一级出现多个候选都立即拒绝。`*** End of File` 只表达 hunk 必须锚定文件尾，不得被当成缺失上下文的通配符。该容错用于降低模型因行尾空格或常见标点差异造成的无意义失败，但不能降低修改位置的确定性。

ApplyPatchOutput 应提供内容级 exact delta，而不只提供“可能修改了哪些路径”：

```go
type AppliedPatchDelta struct {
    Path        string
    Operation   PatchOperation
    OldContent  []byte
    NewContent  []byte
    UnifiedDiff string
    Exact       bool
}
```

`OldContent/NewContent` 可在内存中用于 RunDiffProjector 和测试，但持久化与 UI 必须遵守大小预算和敏感信息规则；超大内容只持久化受控 unified diff、摘要和 hash。只有 `Exact=true` 且对应 operation 已提交的 delta 可以进入 RunDiffProjector。普通 Shell、MCP 或 Skill Script 的工作区变化不伪装成 Patch delta，也不触发归因警告。

目标优化包括：支持 `*** End of File`、有限唯一匹配梯度、内容级 `AppliedPatchDelta`、流式参数阶段的 Patch Preview，以及更明确的 parse/permission/stale/partial 错误。暂不照搬 Codex 的 Add 覆盖、Move 覆盖和任意宽松 trim 匹配；这些行为只有在真实使用数据证明拒绝率明显影响可用性，并且能保持唯一目标与可审计性时再单独评估。

#### 8.3.2 与 Codex Tool Runtime 的取舍

Codex 的通用主链是 `ResponseItem → ToolRouter → ToolRegistry → ToolHandler`；每个 Handler 自己解析 payload 并构造领域 Request。Shell、`apply_patch` 等受保护操作再进入统一 ToolOrchestrator，由它集中处理 Approval Requirement、Sandbox 选择、首次执行、Sandbox Denied 后的审批与升级重试；普通 Tool 不强制经过同一个 Sandbox Orchestrator。Codex 不使用所有 Tool 共用的 PreparedToolCall，而是使用 `ExecCommandRequest`、`ApplyPatchRequest` 等专用请求。

Amadeus 对齐这一语义：ToolRouter 负责路由、唯一参数校验、并行调度和结果投影；ToolHandler 是业务入口；复杂 Handler 使用私有领域 Request/Prepared 类型。目标架构删除通用 `Tool.Prepare/Execute`、PreparedToolCall、PreparedTarget、ToolDispatcher、ToolExecutionGate 与全工具 Authorizer，不构建比 Codex 更庞大的通用管道。

首版只为进程执行建立轻量 Operation Runtime：

```text
ExecRequest
        ↓
ExecPolicy（skip / needs_approval / forbidden）
        ↓
ApprovalReviewer / SessionApprovalStore
        ↓
Sandbox selection
        ↓
execute once
        ↓ sandbox denied and policy permits escalation
explicit approval / unsandboxed retry
```

结构化文件 Tool、Plan、读取、Web 和普通内部状态 Tool 不进入这套进程 Orchestrator；它们分别依赖 FileSystemPolicy、网络 Guard 或自身领域边界。`apply_patch` 是进程内结构化修改，通过 Permission、私有 PreparedPatch、staging 和 revalidation 获得安全性，不为了形式统一放入 OS Sandbox。未来只有出现第二类真实进程 Tool 时，才把现有 Shell 分支抽成独立 ToolOrchestrator，避免提前复制 Codex 的完整复杂度。

### 8.4 Session、SessionRuntime、RunContext 与 RunRuntime

Amadeus 不使用 `Turn` 作为核心业务语义。Codex 的一次用户 Turn 对应 Amadeus 的一个 Run；持久化层以 Project、Session、Run 与 append-only RolloutItem 为核心，活动执行层增加 SessionRuntime、RunContext、RunRuntime、RunState 与 RequestContext。`ChatSession` 直接删除；若未来保留纯聊天入口，也必须复用统一 `SessionRuntime + LLMRuntime`，不得再次维护独立内存历史。

```go
type SessionID string
type RunID string

type Session struct {
    ID              SessionID
    ProjectID       ProjectID
    Title           string
    Status          SessionStatus
    NextRunSequence int64
    NextItemSequence int64
    CreatedAt       time.Time
    UpdatedAt       time.Time
    LastActiveAt    time.Time
}

type Run struct {
    ID         RunID
    SessionID  SessionID
    Sequence   int64
    Mode       RunMode
    Status     RunStatus
    Provider   string
    Model      string
    StartedAt  time.Time
    FinishedAt *time.Time
}

type RolloutItem struct {
    ID         RolloutItemID
    SessionID  SessionID
    RunID      *RunID
    Sequence   int64
    Kind       RolloutItemKind
    Payload    json.RawMessage
    CreatedAt  time.Time
}
```

`Session` 表示当前项目下可恢复的长期对话；`Run` 表示一次真实用户输入触发的完整工作；`RolloutItem` 表示 Session 中真实发生并按全局序号排列的一项历史事实。普通交互中，一条非空用户输入创建新 Run，并将该用户输入作为本 Run 的第一条 RolloutItem；用户中断后再次输入会创建新的 Run，不精确恢复旧调用栈。

RolloutItem 首版支持：

- `user_message`、`assistant_message`；
- `tool_call`、`tool_result`；
- `plan_update`；
- `context_snapshot`；
- `run_interrupted`、`run_failed`；
- `context_compaction`。

RolloutItem 使用 Amadeus Provider-neutral payload，不直接持久化 OpenAI Responses 或 Chat Completions SDK 对象。Provider Adapter 只在请求边界将有效历史转换为具体 API 方言。

SessionRuntime 对应 Codex 的长生命周期 Session，拥有当前进程内的唯一 SessionHistory 镜像和 Rollout 追加入口：

```go
type SessionRuntime struct {
    session Session
    history *SessionHistory
    rollout RolloutAppender
    events  event.Sink
    extensions *ExtensionRuntime
    permissions SessionPermissionStore
    approvals   SessionApprovalStore

    activeRun *RunRuntime
}
```

SessionRuntime 负责加载/replay Session、创建和取消当前 Run、分配 item sequence、追加并 flush Rollout，以及为 ContextManager 提供一致的 History View。建议唯一追加顺序为“分配 sequence → SQLite append → 更新内存 History → 发布事件”；任何失败都必须保持内存与持久层可判定一致。SessionRuntime 同时拥有进程内 SessionPermissionStore、SessionApprovalStore 与 ExtensionRuntime 的生命周期，但不实现 Permission/Approval 策略、MCP、Skill、Reactor、工具业务或 Provider 方言；三个 Session 级组件随活动 Session 创建并跨多个 Run 复用，在 SessionRuntime 关闭或切换 Session 时统一释放，权限与批准不写入 SQLite。SessionPermissionStore 保存当前活动 Session 已批准的 Additional Writable Roots；SessionApprovalStore 只缓存 Unsandboxed `execute_command` 的 `ApprovedForSession` 精确命令键。

`RunMode` 只表达协作模式：

```go
type RunMode string

const (
    RunModeExecute RunMode = "execute"
    RunModePlan    RunMode = "plan"
)
```

`execute` 使用统一 Reactor，允许模型按需调用 `update_plan`，并按工具策略执行读写操作；`plan` 使用同一个 Reactor，但只暴露非修改型能力，最终输出可审核的自然语言计划。旧 `react/planned` 是算法模式混合语义：迁移时 `react` 与历史 `planned` 都映射为 `execute`，新的 `plan` 只表示不实施的 Plan Mode。

SessionRuntime 在 Run 接纳完成后创建只读 RunContext：

```go
type RunContext struct {
    Project Project
    Session Session
    Run     Run

    Provider string
    Model    string
    Mode     RunMode
    CWD      string
    Permissions PermissionProfile

    ContextProfile ContextProfile
    Budget         react.Budget
}
```

`RunContext` 对应 Codex `TurnContext`，只保存本 Run 开始时稳定的 Project、Session、Run、Provider、模型、模式、cwd、基础 PermissionProfile、ContextProfile 和 Budget。PermissionProfile 冻结 ReadHost、WorkspaceRoots、TemporaryRoots、ReadOnlyRoots 和 DeniedRoots；Run/Session Permission Grant 在每次 Tool Prepare 前派生 EffectivePermissionProfile，不回写 RunContext。RunContext 不持有 SessionHistory、取消函数、活动进程、Reactor 状态、PlanState 或可变 Usage；现有 `StartedRun` 应收敛并重命名为 `RunContext`。

RunRuntime 接管 RunContext 的活动生命周期：

```go
type RunRuntime struct {
    context *RunContext
    state   *RunState
    cancel  context.CancelCauseFunc
    done    chan struct{}
}

type RunState struct {
    Plan      PlanState
    Usage     llm.Usage
    ToolCalls int

    Permissions     RunPermissionStore
    PendingApprovals map[string]ApprovalWaiter
    ActiveProcesses  map[string]ProcessHandle
    Terminal         bool
}
```

`RunRuntime` 对应 Codex `RunningTask`，只负责活动 Run 的取消、Done、资源清理、Reactor 驱动和 Finish Once；RunState 对应 Codex `TurnState`，持有 PlanState、Usage、RunPermissionStore、审批等待、活动进程和计数。RunPermissionStore 保存当前 Run 已批准的 Additional Writable Roots，Run 进入任意终态时销毁。模型完成的 assistant item、Tool Call/ToolOutput、Plan Update 和中断 marker 都通过 SessionRuntime.Append 进入 canonical rollout，RunRuntime 不直接拥有 SessionHistory 或 SQLite Store。两者只在进程内存在，不建表、不精确恢复。

每次模型调用前创建 RequestContext，对应 Codex `StepContext`，但使用更符合 Amadeus 语义的名称：

```go
type RequestContext struct {
    Run          *RunContext
    History      SessionHistoryView
    Tools        *ToolRouter
    Instructions instruction.Resolution
    Workspace    WorkspaceSnapshot
    Profile      ContextProfile
    MCP          MCPBinding
    Skills       SkillCatalogSnapshot
    SkillInjections []SkillInjection
}
```

RequestContext 是一次模型采样的动态快照，可随 Rollout、MCP/Skill Catalog、目录级 `AGENTS.md`、工作区和 Context Compaction 改变；同一次采样看到的 MCP Catalog、Skill Index 与显式 Skill 正文必须保持不可变，并由随后产生的 ToolInvocation 继续引用同一 Binding/Snapshot。RequestContext 不持久化，也不能反向成为新的历史事实源。

### 8.5 Reactor Iteration、ToolExecution 与 Progress

独立 Reactor 的最小运行结构如下：

```go
type Iteration struct {
    Index      int
    Think      ThinkResult
    Analysis   Analysis
    Executions []ToolExecution
}
```

`Iteration` 取代旧 ReAct `Step` 术语，表示一次 Think→Analyze→Act→Observe。Observe 仍作为清晰的语义阶段存在，但不再创建重复的 Observation/Evidence 数据树；它接收 ToolExecution，将 ToolOutput 追加为 `tool_result` RolloutItem，将 ToolCallOutcome 投影给 UI/Audit，更新 ProgressMonitor，并触发下一次 RequestContext/RequestView。

通用 Evidence 从 Reactor 和 Tool 主链删除。工具成功只表示调用完成，不表示用户任务、代码正确性或测试已经验证；旧 `Verified`、`CriterionIDs`、`EvidenceBefore/EvidenceAfter` 与 `NoEvidenceThreshold` 都属于含糊或 DAG 遗留语义。当前不新增独立 Verification Domain 或数据库表，测试、构建、Patch 与命令事实由 ToolOutput 内容/metadata 与 ToolCallOutcome 状态共同表达，模型根据 exit code、changes、output 和 status 判断任务状态。

ProgressMonitor 首版只检测重复 Tool Name+Arguments、重复 ToolExecution fingerprint、重复错误和预算耗尽，不再通过“已验证 Evidence 数量”判断进展。未来只有在 CI 自动验收、强制质量门或机器可判定 Workflow 成为真实需求后，才允许从 ToolExecution 派生非持久化 `VerificationView`；它不能成为第二事实源。

### 8.6 PlanState、PlanItem 与 update_plan

Plan-guided ReAct 使用轻量软状态，不使用 ExecutionGraph、TaskStatus、Scheduler 或 Replanner：

```go
type PlanState struct {
    Explanation string
    Items       []PlanItem
    UpdatedAt   time.Time
}

type PlanItem struct {
    Text   string
    Status PlanItemStatus
}
```

`PlanItemStatus` 只有 `pending/in_progress/completed`。PlanItem 不包含 ID、依赖、资源、SideEffect、预算、尝试次数、验收标准或 TaskResult。`update_plan` Handler 只校验清单、更新当前 RunState 的 PlanState、通过 SessionRuntime 追加 `plan_update`、发布 `PlanUpdated` Event，并向模型返回固定成功 ToolOutput；它不选择下一项、不自动调用 Reactor、不检查模型是否真实完成某项，也不切换 Plan Mode。

PlanState 不建立独立表；每次用户可见 `update_plan` 作为 `plan_update` RolloutItem 追加到 canonical rollout。恢复时从有效历史中投影最近 PlanState，不依赖中断摘要或第二份 Plan Store。

## 9. Plan-guided ReAct 与 Plan Mode

本节是当前 Agent Engine 的权威设计。Amadeus 只有一个真正执行模型/工具循环的 Reactor；执行中的计划是模型维护的可见清单，`/plan` 则是只规划不实施的协作模式。两者是正交能力，不再存在 `Planner → DAG → Scheduler → ReActTaskExecutor → Replanner` 主链。

### 9.1 两条产品流程

默认执行：

```text
User Input
    ↓
SessionRuntime.BeginRun(mode=execute)
    ↓
RunContext
    ↓
RunRuntime
    ↓
ContextManager
    ↓
Reactor
    ├── Think
    ├── 可选 update_plan
    ├── Analyze
    ├── Act
    └── Observe
    ↓
Final Answer
    ↓
RunRuntime.finish
```

Plan Mode：

```text
User selects /plan
    ↓
Composer CollaborationMode = plan
    ↓
User Input
    ↓
SessionRuntime.BeginRun(mode=plan)
    ↓
RunContext → RunRuntime
    ↓
ContextManager
    ↓
Reactor（只读工具集）
    ├── 探索代码与配置
    ├── 运行非修改型检查
    ├── 必要时请求用户输入
    └── 输出 Proposed Plan
    ↓
RunRuntime.finish
```

用户随后输入“按这个计划实现”时创建新的 `execute` Run；先前计划已经作为 Session Rollout 的 assistant/plan items 进入有效历史，执行阶段仍可使用 `update_plan` 跟踪或修订实际进度。

### 9.2 Run 接纳与执行顺序

参考 Codex 的 `Session → TurnContext → RunningTask → Agent Loop`，Amadeus 固定为：

```text
SessionRuntime
    → SessionCoordinator.BeginRun
    → RunContext
    → RunRuntime
    → Reactor
    → RunRuntime.finish
    → SessionRuntime.finish
```

SessionRuntime 在恢复或创建 Session 时先通过最新 `context_compaction + tail` replay SessionHistory；`BeginRun` 随后必须在任何模型调用和工具副作用前原子创建 Run、分配 Run/item sequence，并将当前 `user_message` 作为第一条 RolloutItem 持久化。这样 RunDiffProjector、Audit、Process、Approval 与 Tool Event 从第一刻起都有稳定 SessionID/RunID；不再关联 `context_from_run_id`，也不加载 Previous Work 摘要。

### 9.3 RunContext 边界

RunContext 只携带本 Run 开始时的稳定事实：Project、Session、Run、Provider、Model、RunMode、cwd、ContextProfile 与 Budget。SessionHistory 属于 SessionRuntime；Instructions、Skills、MCP Catalog 与工作区状态由 ContextManager 在每次模型采样前解析到 RequestContext。

RunContext 不等同于 `context.Context`，也不包含每次 Think 的 RequestView；代码中建议使用 `runContext` 或 `runCtx` 变量避免混淆。

### 9.4 RunRuntime 边界

RunRuntime 对应 Codex `RunningTask`，RunState 对应 Codex `TurnState`；Amadeus 当前只有一个 Reactor 执行主链，因此暂不复制 `RegularTask/ReviewTask/CompactTask` 等多 TaskKind 抽象：

1. 持有 RunContext 与取消树；
2. 绑定 RunState、Event Metadata、RunDiffProjector 和 Process Owner；
3. 根据 RunMode 配置同一个 Reactor 的工具能力与最终输出约束；
4. 由 RunState 持有 PlanState、累计 Usage、审批等待和活动进程；
5. 保证 Finish Once，统一映射 completed/interrupted/failed；
6. 清理活动进程、审批等待和运行时资源；
7. 通过 SessionRuntime.Append 追加 assistant、tool、plan 或 interruption item，并由 SessionRuntime/SessionCoordinator 更新 Run 终态。

CLI/TUI 只负责接收输入、请求创建 RunContext、启动 RunRuntime、发送取消和渲染事件，不再直接编排 Reactor、ContextManager、RunDiffProjector、Usage、Rollout 持久化与 FinishRun 分支。

### 9.5 独立 Reactor

Reactor 输入只包含 RunRuntime 提供的 RequestView、AvailableTools、Budget 与可选 Execution Metadata，不直接依赖 RunStatus、SessionRuntime 或 Store。每次 Think 前由 ContextManager 基于 RequestContext 生成唯一 RequestView。

一次循环固定为：

1. `Think`：调用 LLMRuntime，获得文本、Tool Calls 和 Usage；
2. `Analyze`：规范化响应和工具参数，判断 Final 或 Act；
3. `Act`：由 ToolRouter 完成 Registry、唯一 Schema 校验和并行调度，再调用 ToolHandler；具体 Handler 按需使用 FileSystemPolicy、ExecPolicy、Permission、Approval、Sandbox 和 Audit，并把精确 Patch 变化投影给 RunDiffProjector；
4. `Observe`：将 ToolExecution 中的 ToolOutput 追加为 `tool_result`、将 ToolCallOutcome 投影给 UI/Audit、更新 ProgressMonitor，并进入下一轮 RequestContext/RequestView。

模型输出 Final Assistant Message 且没有必须跟进的 Tool Call 时 Run 完成；工具调用、可纠正 Tool Error、用户 steer 或 Stop Hook 要求继续时进入下一 Iteration。

### 9.6 Plan-guided 执行

普通 `execute` Run 不强制创建计划。提示词根据任务规模引导模型：简单问答、单文件读取和明确小修改直接执行；长任务、跨文件修改、多阶段验证或存在明显依赖时调用 `update_plan`。

`update_plan` 是非副作用 Runtime Tool：

```text
模型调用 update_plan
    ↓
PlanState 校验
    ↓
RunRuntime 更新内存状态
    ↓
发布 PlanUpdated Event
    ↓
返回 "Plan updated"
    ↓
同一 Reactor 继续执行
```

模型可根据 ToolExecution 重写、合并或调整 PlanItem。Runtime 不把 Plan 编译为 DAG，也不因某个 Item 状态自动触发工具；真实执行事实仍以 canonical Tool Call/ToolOutput、Workspace 状态与最终回答为准。

### 9.7 Plan Mode

`/plan` 表示进入 Plan Mode，而不是强制 Plan-and-Execute。Plan Mode 可以读取文件、搜索代码、检查配置、运行不会修改项目受跟踪文件的测试/构建和静态分析；禁止 `apply_patch`、写文件、修改型 Shell 及其他写工具。

Plan Mode 最终输出结构清晰的自然语言 Proposed Plan，包含目标摘要、关键改动、测试方案和必要假设。对齐 Codex，`/plan` 本身不接收任务，而是把当前 Composer 的临时 CollaborationMode 切换为 `plan`；用户随后提交的普通输入创建 `mode=plan` Run。当前模式必须在输入框 Footer/Status 中明确展示，并通过统一模式切换快捷键返回默认 `execute`。该状态只属于当前交互 Runtime，不写入 YAML 或 SQLite；重新启动或切换 Session 时默认回到 `execute`，不得复活 DAG 执行器。

Plan Mode 中不暴露 `update_plan`：前者产出供用户审核的实施方案，后者是执行阶段的 TODO/进度工具，语义不可混用。

### 9.8 完成、中断与停止

Reactor 可返回 `completed/stalled/blocked/failed/interrupted/budget_exhausted`。RunRuntime 负责将结果映射为持久化 RunStatus，并在所有路径执行资源清理和 FinishRun。

用户中断时不恢复旧 Reactor 调用栈。RunRuntime 补齐悬空 Tool Call、追加 `run_interrupted` marker 并 flush；下一条用户输入创建新 Run，模型根据真实 rollout、当前工作区和新输入重新判断和执行。

### 9.9 明确非目标

当前主链不包含：

- 独立 Planner/Replanner Model Call；
- ExecutionGraph、DAG、TaskStatus 或 Scheduler；
- 自动 ReAct→Planned 模式升级；
- synthetic root Task；
- 强制 Verifier/Reflection 双质量门；
- 独立 Final Synthesizer；
- Plan/Iteration/Tool Call 独立业务表；
- 精确恢复旧 Run 的工具位置或调用栈。

### 9.10 实现边界

- `internal/session`：Session、Run、RolloutItem、SessionHistory、RunContext、Coordinator 与 Store；
- `internal/agent/runtime`：RunRuntime、Executor Port、LLMRuntime、取消和统一收尾；
- `internal/agent/react`：Reactor、Iteration、Think/Analyze/Act/Observe 与 ProgressMonitor；
- `internal/agent/plan`：PlanState、PlanItem、`update_plan` Tool 与事件投影；
- `internal/context`：StaticContextBuilder、HistoryReplayer、PromptProjector、ContextCompactor、ContextManager 与 RequestView；
- `cmd/amadeus`：输入接纳、slash command、RunRuntime 启动和结果映射，不拥有 Agent 控制流。

## 10. Multi-Agent 后续扩展

Multi-Agent 不进入首个可用版本主链。第一版验证时采用与 Codex 相近的 Tool-based Delegation：主 Agent 的 Reactor 通过 `spawn_agent/send_input/wait_agent/close_agent` 一类工具管理最多两个只读 SubAgent，而不是引入 Team Engine、共享 DAG 或中央 Scheduler。

### 10.1 MVP 边界

- 仅主 Agent 拥有 Session Run、最终回答、写工具、Shell、RunDiffProjector 与 Approval；
- SubAgent 拥有独立内存上下文和 Reactor，但共享只读 WorkspaceRoots 与基础 Instructions；
- SubAgent 只做代码探索、定位、比较、审查和方案研究；
- 主 Agent 明确分配边界清晰、可并行、不会阻塞当前关键路径的工作；
- SubAgent 返回结构化摘要、证据和文件引用，不直接合并修改；
- 主 Agent 不等待时继续处理非重叠工作，避免把 Multi-Agent 变成串行远程调用。

### 10.2 DelegatedTask 语义

Multi-Agent 的 `DelegatedTask` 是主 Agent 发给 SubAgent 的临时工作单元，不等同于 Session Run、PlanItem 或 Reactor Iteration：

```go
type DelegatedTask struct {
    ID          string
    ParentRunID RunID
    Objective   string
    Scope       []string
    ReadOnly    bool
}
```

它只在 Multi-Agent Runtime 内存在；首版不建表，不参与 Session resume，也不作为 `update_plan` 的执行依据。主 Agent 将 SubAgent 结果规范化为 ToolOutput，并通过 SessionRuntime 追加到自己的 canonical rollout。

### 10.3 失败与取消

SubAgent 超时、失败或返回低质量结果时，主 Agent 可以重试一次、改写目标或回退为本地探索。父 Run 取消时所有 SubAgent 必须级联取消；主 Agent 结束前必须关闭活动 SubAgent。任何 SubAgent 失败都不得绕过主 Agent 的最终验证和责任边界。

### 10.4 延后能力

以下能力继续后置：可写 SubAgent、共享 Workspace 修改、自动任务拆分、依赖图调度、远程 Worker、动态角色市场、长期驻留 Agent 与跨 Session Agent Memory。

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

web_search:
  enabled: false
  provider: duckduckgo # duckduckgo | tavily | searxng | brave
  timeout: 15s
  max_results: 5
  providers:
    duckduckgo:
      proxy_url: ""
    tavily:
      api_key: ${TAVILY_API_KEY}
      base_url: https://api.tavily.com
    searxng:
      base_url: https://search.example.com
    brave:
      api_key: ${BRAVE_SEARCH_API_KEY}
      base_url: https://api.search.brave.com

logging:
  level: info
  trace_llm: false
```

`api_key` 等 YAML 字符串值支持 `${ENV_VAR}` 引用和字面值。变量在字段级 YAML 解码前展开；变量未设置时，错误包含字段路径与变量名。若使用字面 API key，配置加载器应检查文件权限并给出安全警告；打印有效配置时统一掩码。

`agent` 不提供持久 `mode` 配置。默认 Composer CollaborationMode 为 `execute`，普通输入创建 `execute` Run，并由统一 Reactor 按需使用 `update_plan`；`/plan` 只切换当前交互 Runtime 的 Composer 到 Plan Mode，随后普通输入创建只分析不实施的 `plan` Run。模式不写入配置或 SQLite，重新启动或切换 Session 时回到 `execute`。

配置文件不提供 `approval.enabled`、`approval.default` 或工具级 allow/deny 规则。审批属于内置安全机制，不能通过 YAML 关闭；TTY 中按固定规则询问，非 TTY 对需要审批的调用 fail closed。

`web_search` 与 `providers` 中的 LLM 配置是两个独立能力域：用户可以用 DeepSeek/Qwen/GLM 生成推理，同时使用 Tavily、SearXNG、Brave 或 DuckDuckGo 搜索。`web_search.enabled=false`、Provider 不存在或必要凭证缺失时，不向模型注册 `web_search` Tool，避免模型反复调用必然失败的能力。搜索凭证同样参与环境变量展开、来源追踪、权限警告和统一脱敏，但不复用 `AMADEUS_API_KEY`。

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
- log level 必须属于已定义枚举；最大 Iteration 为 `[1, 1000]`，并行工具数为 `[1, 64]`。配置 schema 不包含 approval default/enabled 字段。
- 未提供 API key 时允许启动配置诊断命令，但不允许开始模型回合。
- 未配置 model 时返回明确错误，不静默选择可能变化的远端默认模型。

结构校验允许 API key 和 model 暂时为空；需要模型调用的用例应在启动回合前执行能力校验。所有配置层应用完成后再执行结构校验，确保 CLI flags 可以修正文件或环境中的值。

## 13. Prompt 架构

Prompt 是 Agent Runtime 的内部实现资产，不是公共 Go API。顶层 `prompts/` package 迁移至 `internal/prompt/builtin`，Markdown 模板统一放在 `internal/prompt/builtin/templates`；`internal/prompt` 继续拥有 Repository、Assembler、变量渲染、来源 Hash、Bundle 与 Contract Validation。目标目录为：

```text
internal/prompt/
├── assembler.go
├── repository.go
├── types.go
└── builtin/
    ├── builtin.go
    └── templates/
        ├── agent/
        │   ├── base.md
        │   ├── execution.md
        │   └── handoff.md
        ├── modes/
        │   ├── execute.md
        │   └── plan.md
        ├── runtime/
        │   ├── permissions.md
        │   ├── workspace.md
        │   ├── instructions.md
        │   └── skills.md
        ├── tools/
        │   ├── general.md
        │   ├── apply_patch.md
        │   └── execute_command.md
        └── context/
            └── compaction.md
```

`internal/agent/react` 不得导入内置 Prompt package，也不得在缺少 Prompt 时自行调用 `AgentSystem()` 兜底。唯一装配顺序是 Bootstrap 创建 Builtin Repository/Assembler，按当前配置、RunMode、Tool Exposure 和 RequestContext 生成 Prompt Bundle，再把稳定的 Agent Prompt 注入 Iterator。Reactor 只消费已经装配好的 Prompt 和 RequestView，不拥有文件路径、模板选择或 Prompt fallback。

Prompt 使用稳定职责分层，而不是为不同 Agent 模式复制整套模板：

1. **Agent Base/System**：Amadeus 身份、Coding Agent 基本行为、证据优先、保护用户改动和不可覆盖的安全规则；
2. **Mode/Developer**：当前 RunMode。Execute Mode 负责完成任务并可按需使用 `update_plan`；Plan Mode 只探索和规划，不修改、不执行命令；
3. **Runtime/Developer**：当前 Workspace、Permission/Isolation、Session/Run、interruption marker、环境能力和预算等动态事实；
4. **Instructions/Developer**：用户级、项目级和目录级 `AGENTS.md`，保持已有作用域与覆盖顺序；
5. **Extensions/Developer**：当前 Skill Catalog 摘要、显式 SkillInjection 和 MCP Binding/Catalog Snapshot；
6. **Tool Definitions**：当前 RunMode 实际暴露的 Tool Schema；只有复杂工具在暴露时追加 Tool-specific Guidance；
7. **Conversation/User**：Canonical Rollout 经 ContextManager 投影出的 RequestView。

基础 Prompt 只描述长期稳定的工作方式，不写死当前 cwd、Root、Sandbox 状态、可用 Tool、Skill/MCP 列表或权限授权结果。动态 Permission Instructions 必须从 EffectivePermissionProfile、IsolationMode 和 Session Approval Facts 投影，明确 `permission_required → request_permissions → 模型重新调用原 Tool`，但不能向模型承诺 Runtime 未实现的能力。Provider 不支持 developer role 时，由 Adapter 按 Dialect 降级到 system；Domain 仍保留 system/developer/user/assistant/tool 的清晰语义。

默认 Execute Mode 描述统一 Think→Analyze→Act→Observe/Tool-Use 循环，并建议复杂任务按需使用 `update_plan`；不要求简单任务先生成计划，不要求模型输出 DAG、Task JSON、Verification verdict、Reflection verdict 或隐藏 reasoning。Plan Mode 明确只进行可验证的探索和规划，最终输出自然语言 Proposed Plan；不加载 `update_plan`、`request_permissions` 或任何写入/执行 Tool，也不复用旧 Planner/Replanner Prompt。`update_plan` Tool Schema 只包含 `explanation` 和 `plan[{step,status}]`。

Tool Guidance 必须与真实 Tool Contract 一致，并避免在 Base、Runtime 和 Tool Description 中重复同一规则：

- `apply_patch.md` 说明 Patch Grammar、上下文匹配、冲突后重新读取和 partial/failure 语义；
- `execute_command.md` 说明 cwd、`requested_permissions.writable_roots`、Sandboxed/Unsandboxed、持续 Process 与 `write_stdin`；
- `general.md` 说明结构化探索、失败 ToolExecution、验证和不得绕过 Permission/Approval；
- Tool 不可见时不注入其专属 Guidance，防止模型尝试调用未暴露能力。

Amadeus 选择性吸收 Codex 开源 Prompt 的成熟做法，包括任务持续执行、先探索再修改、进度沟通、计划使用边界、验证纪律、Apply Patch 指导、Permission 动态注入和最终交付格式；不整套复制 Codex Prompt。所有内容必须改写为 Amadeus 的 Reactor、Tool Schema、PermissionProfile、`request_permissions`、Provider Adapter、Plan Mode 和 TUI 语义。GPT/Codex 模型专属指令、Goal Runtime、Realtime、Memories、Review 和 Multi-Agent Orchestrator Prompt 不进入当前主链；未来只有对应能力真实实现并通过 E2E 后才能增加独立模板。

Context Compaction 使用独立 `context/compaction.md`，不与普通 Agent Base 混合。它只生成 Replacement History 所需的事实摘要，必须保留用户目标、重要决策、修改文件、ToolExecution、失败、未完成项和验证结果，不生成新的任务决定或虚构状态。可选 Multi-Agent Delegation Prompt 留到 M11，不在当前 Prompt 优化中预埋。

Prompt Repository 保留 ID、来源、SHA-256 和 Required Variables；新增 Contract Test 固定以下不变量：所有模板可读取且非空、变量完整无未知项、装配顺序稳定、Plan Mode 不暴露写/执行指令、动态 Permission 与实际 Policy 一致、不存在旧 Planner/Replanner/Verifier/Reflector 术语、Responses 与 Chat Completions 得到等价 Domain Message 语义。Prompt 文案变化不依赖逐字 Snapshot，而以关键行为 Contract、Provider mock E2E 和真实 Coding Agent smoke 验收。

## 14. 工具体系

### 14.1 设计原则

Amadeus 采用“结构化高频工具 + 通用 Shell fallback”，不因为 Shell 可以运行 `cat`、`find`、`grep` 或重定向写文件，就删除专用文件工具。Codex 的 Shell-first 依赖成熟的 Sandbox、Permission Profile、PTY、持续 Process、权限升级和跨平台隔离；Amadeus 向其学习“权限决定可访问范围、ExecPolicy 决定是否询问、Sandbox 负责真实强制”的分层，但不复制完整跨平台实现。在没有同等级 Sandbox 前只删除模型可见的读工具，会把几个简单 Tool 的维护成本转化为 Shell 副作用识别、频繁审批和宿主机安全问题：

- 结构化 Handler 是模型读取、搜索和修改工作区的主路径，提供严格 schema、PathResolver/FileSystemPolicy、稳定 ToolOutput/ToolCallOutcome、预算元数据和跨平台语义；`Project.RootPath` 只表达持久化项目身份，CWD 与 WorkspaceRoots 表达运行上下文，均不兼任唯一文件系统权限边界。
- `execute_command` 是构建、测试、Git、格式化、代码生成、项目脚本和未被专用工具覆盖操作的通用逃生舱，不作为绕过文件工具、安全策略或审批的捷径。
- 工具数量保持克制；只有高频操作确实需要更稳定输出、更细权限或更强领域语义时，才从 Shell 提升为专用工具。
- Tool 名称表达能力而非具体命令行程序；内部可以使用 ripgrep 或平台能力加速，但 fallback 必须保持同一领域结果。
- 结构化 Tool Result 必须显式报告来源、截断、partial、资源使用和副作用，不能把普通 stdout 当作完整事实。
- 维护成本通过共享 `WorkspaceReader/IgnoreMatcher/FileEnumerator/TextScanner/OutputLimiter` 降低，而不是把所有读取退化为 Shell；四个模型可见 Exploration Tool 应保持薄 Adapter。
- Shell-first 只作为未来 Sandbox 成熟后的整体架构候选，不进行“删掉读工具但继续直接在宿主机执行 Shell”的半迁移。

模型选择工具时遵循以下稳定分工，而不是因为能力重叠就随机二选一：

| 需求 | 首选能力 | Shell 的位置 | 原因 |
|---|---|---|---|
| 读取已知文件片段 | `read_file` | 仅处理专用工具不支持的格式或组合流水线 | 一基行号、截断和 Context 预算稳定，常规读取无需命令审批 |
| 浏览目录与发现文件 | `list_dir` / `glob_files` | 复杂 `find`、项目专用脚本作为 fallback | FileSystemPolicy、ignore、排序和 partial 语义跨平台一致 |
| 搜索代码或文本 | `grep_code` | `rg` 高级表达式、管道组合或一次性诊断作为 fallback | 返回稳定 file/line/column，而不是让模型解析任意 stdout |
| 修改项目文件 | `apply_patch` | 不使用 `sed -i`、重定向或脚本绕过 Patch/Approval | 变更可预检、可审计、可生成结构化 changes，并支持冲突诊断 |
| 构建、测试、Git、格式化、项目脚本 | `execute_command` + `write_stdin` | 主能力 | 这些操作本身属于 Process，而不是文件读取协议 |

未来只有同时满足以下条件，才重新评估是否收缩 `list_dir/glob_files/grep_code` 并转向 Shell-first：

1. Shell 已运行在真实、可验证且跨平台的 Sandbox 中，而不是直接继承 Amadeus 进程的宿主机权限；
2. Permission Profile 能区分只读探索、workspace write、越界 write、网络和进程控制，并只在权限升级时询问用户；
3. PTY、后台 Process、取消、超时、孤儿清理和输出预算已经稳定；
4. Shell 输出必须生成与结构化 Handler 等价的 ToolOutput、ToolCallOutcome、来源、截断和审计信息；
5. 真实基准证明缩小 Tool Set 能提高模型成功率，而不是只减少少量 Go Adapter 代码。

即使未来采用 Shell-first，`apply_patch`、`view_image` 和必要的交互/扩展 gateway 仍保留专用语义；是否删除某个探索工具必须逐项用成功率、安全性和跨平台结果验证，不能一次性清空。

### 14.2 Project、文件系统权限与 Sandbox

Amadeus 对齐 Codex，将路径概念拆成“持久化项目身份”“执行上下文”和“文件系统权限”三层，不再使用 `PrimaryRoot` 同时表达所有含义：

```text
Project（持久化）
└── RootPath              # 初始 CWD 的规范绝对路径，只用于 Project/Session 归属

Execution Context
├── CWD                   # 相对路径与默认命令工作目录锚点
└── WorkspaceRoots        # [CWD] + --add-dir，用户主动交给 Agent 的项目目录

FileSystemPolicy
├── Host filesystem       # 默认 read
├── WorkspaceRoots        # write
├── TemporaryRoots        # /tmp + $TMPDIR 或 os.TempDir()，write
├── Run writable roots    # 当前 Run Permission Grant，write
├── Session writable roots# 当前 Session Permission Grant，write
├── ReadOnlyRoots         # 最多 read
└── DeniedRoots           # deny：根自身及其后代

PathResolver
└── 绝对化、规范化、软链接/父目录检查和访问决策

SandboxRunner
└── 仅对 Sandboxed Shell/子进程真实强制文件系统权限
```

`Project.RootPath` 由显式 `--project` 或 Amadeus 启动工作目录确定并持久化，只负责 Project ID、Session 列表过滤和 `--continue` 匹配，不决定配置来源，也不进入 Tool 权限模型。首版不实现 `/cd` 或动态 Session CWD，因此 RunContext.CWD 初始化为 Project.RootPath 并在当前 Session 中保持稳定；单次 `execute_command.cwd` 可以相对该 CWD 指向其他可读目录，但不会改变后续 Tool 的默认 CWD。

`WorkspaceRoots` 是 `[CWD] + --add-dir roots` 的规范化去重派生视图，不单独建表。它表达用户主动交给 Agent 的项目集合，用于项目级/目录级 AGENTS.md 作用域、Sandbox symbolic project roots、路径展示与工作区元数据。首版项目级 MCP/Skill 仍只从 CWD 对应的默认 Workspace Root 加载，避免多个附加根之间出现隐式配置合并和同名扩展优先级；`--add-dir` 不自动加载其中的 `.amadeus/mcp.yaml` 或 `.amadeus/skills`。TemporaryRoots 与 Run/Session 动态授权目录都不是 Workspace Root。Amadeus 首版只有本地主机执行环境，不引入 Codex `environment_id`；未来只有支持多个本机/容器/远端 Environment 时才增加 EnvironmentID。

`--add-dir` 因此不是泛化的“额外可写目录”，而是附加 Workspace Root：

```text
amadeus --project /workspace/backend --add-dir /workspace/frontend

CWD = /workspace/backend
WorkspaceRoots = [/workspace/backend, /workspace/frontend]
```

`--add-dir` 不改变 Project.RootPath、Session 项目 ID、配置来源或默认 CWD；它不写入 config.yaml 或 SQLite，恢复 Session 时需要重新传入。它作为 Workspace Root 加载目标相关的 AGENTS.md，但首版不加载其中的 MCP/Skill。`request_permissions` 产生的 Run/Session Additional Writable Root 只扩大写权限，不成为 Workspace Root，也不加载该目录的 AGENTS.md、Skill 或 MCP。

默认文件系统策略直接对齐 Codex workspace-write：

```text
宿主文件系统                         read
WorkspaceRoots                      write
Unix /tmp 与 $TMPDIR                write
Windows os.TempDir()                write
Run/Session Additional Roots        write
Workspace .git/.amadeus             read-only
敏感凭证、系统敏感接口、AMADEUS_HOME 敏感子路径 deny
```

Unix 平台临时写路径为存在、可规范化并去重后的 `/tmp + $TMPDIR`，Windows 使用 `os.TempDir()`；它们是平台默认权限规则，不按 Run 创建、不随 Run 删除，也不作为独立项目概念暴露。Amadeus 不设计 Run 专属临时根。

ReadOnlyRoots 与 DeniedRoots 必须分开。ReadOnlyRoots 用于父目录可写但特定子 Root 只能读取的场景，例如 Workspace 下的 `.git` 与 `.amadeus`；DeniedRoots 用于 SSH/GPG、常见云凭证、设备/内核接口和 `$AMADEUS_HOME` 中的配置、数据库、审计、用户指令与用户 Skill/MCP 等敏感 Root。不能把整个 AMADEUS_HOME 根目录一刀切 deny，否则当它与 Project.RootPath/Workspace Root 重合时会封死整个项目；规则必须落到具体文件或目录 Root。通用 Tool 与 Sandbox Command 不得读取 Denied Root，只有 Config、Instruction、Skill、MCP 和 Session 等专用内部 Loader 可以通过受控接口读取自身所需文件。DeniedRoots 同时比较规范路径和可得的 canonical target，覆盖规则 Root 自身及全部后代，防止软链接别名绕过。Codex 的 glob policy 不进入 Amadeus MVP。

权限决策先应用不可扩权的限制，再判断可写范围：命中 DeniedRoots 时最终为 deny；未 deny 但命中 ReadOnlyRoots 时最多为 read；只有不受两类限制时，WorkspaceRoots、TemporaryRoots 或 Run/Session Additional Writable Roots 才能赋予 write，否则按 ReadHost 决定 read/deny。Run/Session Grant 不能覆盖 ReadOnlyRoots 或 DeniedRoots。`ReadHost=true` 表示宿主根文件系统默认可读，因此 Amadeus 不设计 AdditionalReadableRoots；普通项目外读取直接执行，DeniedRoots 仍最终拒绝。网络在 MVP 中默认允许，不建立 NetworkPermissionStore；Web 继续执行 SSRF/Redirect Guard，MCP 继续使用已配置 Server 边界，Unsandboxed Command 仍需要操作审批。

建议领域模型：

```go
type FileSystemAccess string

const (
    FileSystemDeny  FileSystemAccess = "deny"
    FileSystemRead  FileSystemAccess = "read"
    FileSystemWrite FileSystemAccess = "write"
)

type WorkspaceContext struct {
    CWD   string
    Roots []string
}

type PermissionProfile struct {
    ReadHost       bool
    WorkspaceRoots []string
    TemporaryRoots []string
    ReadOnlyRoots  []string
    DeniedRoots    []string
}

type AdditionalPermissions struct {
    WritableRoots []string
}

type EffectivePermissionProfile struct {
    Base    PermissionProfile
    Run     AdditionalPermissions
    Session AdditionalPermissions
}

type PathDecision struct {
    RequestedPath string
    ResolvedPath  string
    Access        FileSystemAccess
    MatchedPath   string
    Source        PermissionSource
    Disposition   PathDisposition
}

type RunPermissionStore interface {
    Snapshot() AdditionalPermissions
    GrantWritableRoots(context.Context, []string) error
}

type SessionPermissionStore interface {
    Snapshot() AdditionalPermissions
    GrantWritableRoots(context.Context, []string) error
}
```

`PermissionProfile` 表示 Run 开始时冻结的基础权限；`EffectivePermissionProfile` 显式组合 Base、当前 Run Grant 与当前 Session Grant 快照，Effective Writable Roots 由 `WorkspaceRoots + TemporaryRoots + run grants + session grants` 派生。两个 Permission Store 都只保存规范化、去重后的 Additional Writable Roots：`Allow for this run` 写入 RunPermissionStore，`Allow for this session` 写入 SessionPermissionStore。任何 Grant 都不直接修改 config.yaml、Project.RootPath、RunContext 或 WorkspaceRoots；模型必须在 `request_permissions` 返回后重新发起原 Tool Call，由对应 Handler 使用新的 EffectivePermissionProfile 重新解析目标。

文件 Handler 使用确定性访问流程：

```text
normalized ToolInvocation
         → ToolHandler.Handle
         → resolve path against RunContext.CWD
         → lexical normalize + existing parent/symlink validation
         → Effective FileSystemPolicy decision
         → permission_required / permission_denied / allowed
         → execute domain operation → ToolOutput
```

显式 Root 参数只在对应 Handler 中以 RunContext.CWD 为锚转为规范绝对 Root；没有文件系统目标的 Handler 不做无意义 Root 解析。`execute_command.cwd` 会规范化为存在、可读且未命中 DeniedRoots 的绝对目录，但不要求它位于 Effective Writable Roots：cwd 只决定进程从哪里启动，Writable Roots 决定 Sandboxed 进程可以修改哪里。command 字符串内部的 `../`、绝对路径、变量、脚本或子进程 Root 不由 Amadeus 预先猜测；模型必须通过 `requested_permissions.writable_roots` 明确声明已知的额外写 Root。运行时相对路径由 Shell 根据 canonical cwd 解释，Sandboxed 执行由 Bubblewrap 强制 EffectivePermissionProfile；Unsandboxed 执行无法强制声明范围，必须再经过完整命令 Operation Approval。

读取在 `ReadHost=true` 且未命中 DeniedRoots 时直接执行；结构化写入位于 Effective Writable Roots 内时同样直接执行，不再因为“写 Tool”固定询问。普通写 Root 未获授权时返回带 `permission_required` 错误的 ToolOutput，明确携带缺失的 Writable Roots 并提示模型调用 `request_permissions`；`request_permissions` Handler 提供 `Allow for this run / Allow for this session / Deny`，分别写入 RunPermissionStore、SessionPermissionStore 或不写入。授权后模型重新调用原 Tool。ReadOnlyRoots、DeniedRoots、软链接逃逸和不可安全规范化 Root 直接返回 `permission_denied`，不能调用 `request_permissions` 绕过；用户拒绝和 Sandbox 拒绝分别形成 `approval_denied`、`sandbox_denied` ToolCallOutcome，不作为内部 Go error。

普通文件 Handler 只解析一次用户请求路径并立即执行。只有真正需要多阶段事务的 `ApplyPatchHandler` 保存私有 PreparedPatch：Patch Parser、FileSystemPolicy、staging、commit 与 RunDiffProjector 共用其中的 canonical target 和 exact delta；提交前执行轻量 staleness/identity revalidation，失败返回 `target_stale/path_denied` 并要求模型重新发起调用。`ExecuteCommandHandler` 使用私有 ExecRequest 保存 canonical cwd、Shell、Command、TTY 与 IsolationMode，ApprovalReviewer、SandboxRunner 和执行器共用该 Request，不重新扫描 raw arguments。

旧 `CommandGuard` 的复杂路径推断与风险树不再作为目标组件；实现可保留同名轻量前置检查，但其职责必须收敛为参数健全性检查与极小灾难性命令拒绝集。是否跳过审批、需要审批或禁止执行统一由 `ExecPolicy` 输出 `skip / needs_approval / forbidden`，并结合当前 IsolationMode 与精确 Session Approval Key 决策。两者都不得扫描每个 Shell token 中的 `../`、`$HOME`、绝对 Root 或重定向来模拟文件系统 Sandbox。Shell、脚本、编译器和子进程只有在 `sandboxed` 模式下由 SandboxRunner 强制访问范围；`unsandboxed` 明确承认无法强制宿主 Root 边界。目标接口为：

```go
type SandboxProfile struct {
    CWD           string
    ReadHost      bool
    WritableRoots []string
    ReadOnlyRoots []string
    DeniedRoots   []string
}

type IsolationMode string

const (
    IsolationSandboxed   IsolationMode = "sandboxed"
    IsolationUnsandboxed IsolationMode = "unsandboxed"
)

type SandboxRunner interface {
    Start(context.Context, process.Command, SandboxProfile) (process.Handle, error)
}

type ExecApprovalRequirement string

const (
    ExecApprovalSkip      ExecApprovalRequirement = "skip"
    ExecApprovalRequired  ExecApprovalRequirement = "required"
    ExecApprovalForbidden ExecApprovalRequirement = "forbidden"
)
```

`workspace-write` 是 PermissionProfile/Sandbox Policy，表示宿主可读、WorkspaceRoots/TemporaryRoots/Run 与 Session 授权 Root 可写；Bubblewrap 是 Linux 上实施该 Policy 的 Sandbox 机制。二者组合形成 `sandboxed` 执行。没有可用 OS Sandbox 的平台使用 `unsandboxed` 执行，不再把 `degraded` 作为权限或执行模式；诊断信息可以说明 Sandbox unavailable。MVP 网络默认允许，不建立 NetworkPermissionStore，也不实现 Docker 或 macOS/Windows 原生 Sandbox。Sandboxed 普通命令在 Permission Check 通过且未命中极小 forbidden 集后直接执行；Unsandboxed 命令即使 Permission Check 已通过，仍必须经过 Operation Approval，并明确提示它可能访问声明范围之外的宿主资源。

各领域 Handler、Approval、Audit、ToolOutput 和 RunDiffProjector 一律使用 PathResolver 生成的规范绝对路径作为事实键；UI 可以同时显示相对 CWD 或最近 Workspace Root 的友好路径。普通文件 Handler 在一次 Handle 内解析并使用路径；ApplyPatchHandler 的私有 PreparedPatch 额外保留 requested path、canonical path、access、matched root 和 permission source，避免多个 Workspace Root 下同名相对路径发生权限、Diff 或审计歧义。规范路径不用于建立资源锁；不支持并行调用的 Handler 已形成全局有序屏障。

### 14.3 内置工具分层

目标稳定核心工具面分为三组：

```text
Exploration
├── read_file
├── list_dir
├── glob_files
├── grep_code
└── view_image          # 仅模型支持图片时注册

Mutation
└── apply_patch

Execution
├── execute_command
└── write_stdin
```

`write_file` 的迁移已经完成：`apply_patch` 覆盖 create/update/delete/move、整文件替换和稳定冲突诊断，Provider Prompt、E2E、Registry 与生产实现均不再包含 `write_file`。目标架构同时删除模型可见 `revert_run` 和全项目 Snapshot 主链；条件工具只保留 `read_skill`、`web_search/web_fetch`、MCP gateway 和未来 SubAgent。交互和策略能力不强制伪装成 Provider Tool：Command Approval 由 ExecuteCommandHandler 的 ToolOrchestrator 调用 Approval Port；`request_user_input` 只有在后续证明“Run 内等待用户”明显优于 blocked 后新 Run 时再立项。LSP 不进入核心 Tool/Hook 基线。

### 14.4 探索工具

- `read_file`：按一基行号局部读取 FileSystemPolicy 可读范围内的 UTF-8 regular file；支持绝对路径和相对 CWD 的 `../`，大文件不能因为总大小超限而阻止小范围读取。输出带稳定 `L<line>:` 前缀、选区/总行数、下一起点、超长行截断和 byte/token budget。
- `list_dir`：稳定列出一层目录，提供 file/dir/symlink、大小、hidden、entry budget 和截断语义；深层发现继续交给 `glob_files`，不新增重叠的 `tree` Tool。
- `glob_files`：支持 `path + pattern`，从指定可读目录开始，优先通过 `rg --files` 获得 `.gitignore/.ignore` 语义，无 `rg` 时使用共享 IgnoreMatcher 的纯 Go fallback；两条 Backend 返回相同的规范路径、友好展示路径、排序和 partial metadata。
- `grep_code`：优先直接消费 `rg` 的结构化或稳定输出获得文件、行、列、上下文和匹配文本，不再先 `--files-with-matches` 后重新读取全部候选文件；支持 path/glob/type 过滤并保留纯 Go fallback。
- `view_image`：只读取 FileSystemPolicy 可读范围内受支持的 PNG/JPEG/WebP/静态 GIF，校验格式、大小和尺寸后返回真正的多模态 Content Part；必须先扩展 `llm.Message` 与 Responses/Chat Adapter，不能把 base64 图片包装成普通文本 Tool Result。

这些工具与 Shell 有意重叠。区别不在“是否能完成”，而在专用 Handler 可以严格限制读取范围、免去常规探索审批、提供稳定 metadata，并直接形成结构化 ToolOutput 和 canonical rollout。底层是否调用 ripgrep 是实现细节，模型不需要知道或拼接平台相关命令。

### 14.5 修改工具

`apply_patch` 是唯一目标文件修改工具，并支持受控的 create/update/delete/move operation。JSON Function Tool 外壳继续只接收 `patch` 字符串，以兼容 Responses、Chat Completions、DeepSeek、Qwen 和 GLM；内部行协议对齐模型熟悉的 Codex Patch 语义，不依赖 OpenAI-only Freeform Grammar Tool。每个 update hunk 必须携带足够上下文并在当前文件唯一匹配；匹配只允许 exact → CRLF/LF 归一化 → 行尾空白归一化三层保守降级，仍不唯一时明确失败，不进行宽松猜测式替换。ApplyPatchHandler 只解析一次完整 Patch，将 Parsed Document、全部 operation 的 canonical source/destination target 和预计算执行输入放入私有 PreparedPatch；源/目标路径可为绝对路径或相对 CWD 的路径，但都必须解析到某个已声明 Writable Root，并由同一 PreparedPatch 进入提交、Audit 和 RunDiffProjector。跨文件中途失败必须返回明确 partial/已应用 operation，不得伪装成原子成功。

M4-01 将 Patch Document v1 固定为 UTF-8 行协议。历史规范头为 `*** Begin Patch v1`；目标 Prompt 和 Tool description 统一生成模型更熟悉的 `*** Begin Patch`，解析器继续接受 v1 兼容入口并拒绝未知版本。Add 正文行使用 `+`，Delete 不允许正文，Update 至少包含一个以 `@@` 开始的 hunk，hunk 行分别以空格、`+`、`-` 表示 context/add/delete，并且必须同时包含旧内容和真实变更。目标协议增加 `*** Move to:`，源路径与目标路径都进入预检、审批摘要和原子提交。解析错误除 line/column 外还应提供 error kind、path、hunk、期望上下文和有界 candidate lines，便于 Reactor 修正而不是盲目重试。

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

M4-02 的文件执行器采用“全 Patch 预检、逐 operation 提交”的边界：先验证 Document、解析并守卫全部路径、检查目标类型与大小、在内存中完成所有 hunk 的唯一匹配和新内容计算，再为 Add/Update 在目标同目录创建临时文件。首次修改前会重新校验全部目标，随后按文档顺序提交；Add/Update 通过临时文件 `Sync` 后原子 rename，Update 保留原权限和既有 CRLF/末尾换行风格，Delete 仅删除 regular file。预检冲突不会产生任何文件变化；若跨文件提交中途失败，则 ApplyPatchOutput 明确携带已应用 operation、changes 和 `partial=true`，供模型重新检查现场。

M4-03 将 `apply_patch` 作为核心 ToolHandler 接入 Registry。ToolSpec 使用单一必填 `patch` 字符串，Handler 的 `SupportsParallelToolCalls()` 返回 false：任何 Patch 都作为 Run 级有序屏障，不尝试根据目标文件拆分并行写入。当前 Authorizer 再次 Parse Patch 的历史实现必须迁移到 ApplyPatchHandler：Handler 一次完成 Document 解析、全部 operation 的 PathResolver/FileSystemPolicy 判定并构造私有 PreparedPatch；Audit 和 Commit 共用该领域对象。Audit 沿用规范化完整参数的 SHA-256，只展示/持久化哈希而不记录 Patch 正文。成功或失败 ApplyPatchOutput 都返回精确 operation delta；RunDiffProjector 只消费已经实际提交的 delta，中途失败保留 `partial=true`、已应用 operation 和明确错误状态，取消沿 Handler Runtime 传播且提交前取消不产生写入。

`write_file` 的 M4 实现只作为历史兼容记录。M7 已按“`apply_patch` 补齐 Add/Move/整文件替换与失败诊断 → Provider/E2E 迁移 → 删除生产注册、实现和 Prompt”的顺序完成退场；Patch 冲突只能通过重新读取现场并构造新的唯一匹配 Patch 修正，不能回退到整文件写入逃生路径。

M4-04 将 `write_file` 的 `mode` 固定为必填枚举 `create|replace`。`create` 只允许目标不存在，必要时创建父目录，并通过同目录 staged file + hard link 以 no-replace 语义原子发布；并发出现同名目标时确定性失败，不覆盖任何内容。`replace` 只允许目标已经是 regular file，不创建缺失目标或父目录，使用同目录 staged file + atomic rename，并继承原文件权限。旧的无 `mode` 参数在 Tool Schema 和直接执行层都明确失败；非法 mode、create-existing、replace-missing、路径逃逸、超限和提交前取消均为零内容副作用。

不单独增加 `edit_file`、`delete_file`、`move_file`、`create_directory`、`create_project`、`git_status`、`git_diff`、`run_tests` 或 `format_code`：Patch 覆盖文件变更，Git/测试/格式化和项目脚本继续由命令工具处理。只有后续实际使用证明需要独立权限、结构化结果或可移植行为时再拆分。

### 14.6 Shell 使用策略

模型选择顺序固定为：

1. 读取、目录浏览、文件发现和代码搜索优先使用 Exploration Tool。
2. 所有文件创建、更新、删除和移动优先使用 `apply_patch`；迁移期 `write_file` 不作为模型默认选择。
3. 构建、测试、Git、格式化、生成器和项目自定义 CLI 使用 `execute_command`。
4. 命令在 yield 时间内未结束时返回 `process_id`；后续通过 `write_stdin` 输入或空输入轮询，不因前台等待超时直接杀死正常长任务。
5. 专用工具无法表达需求时允许 Shell fallback，但仍经过 ExecPolicy、Permission/Approval、Sandbox、Audit、timeout、进程组取消和输出预算。

Shell 中的 `cat`、`sed`、`grep`、Python/Node 文件访问只有在 Sandboxed 执行时才能由 SandboxRunner 根据 EffectivePermissionProfile 真实约束，不能依赖 ExecPolicy 猜测 command 字符串中的所有 Root。Linux workspace-write 下，普通命令直接进入 Bubblewrap，越界访问形成 `sandbox_denied` Outcome；Unsandboxed 执行不扫描 `../`、绝对 Root、变量或重定向，而是在 Permission Check 之后对完整命令做 Operation Approval，并明确提示这是 unsandboxed host execution。专用工具失败时，模型可以根据错误选择修正参数或使用合法 Shell fallback，但不得为了绕过策略拒绝而改写成等价命令。

M4-05 的 Tool Selection Prompt 是历史七工具基线；目标 Prompt 更新为 Exploration → Patch → Command/Process 的三层选择，不再引导 `write_file`。Shell 只在专用 Handler 无法表达时 fallback，且不得通过重定向、脚本或等价命令绕过 Tool contract、FileSystemPolicy、Sandbox、策略拒绝或审批。失败 ToolExecution 是 canonical 事实，模型必须修正或选择合法替代，不能静默宣称成功。

`execute_command` 目标实现由 `ProcessManager` 支撑，参数增加 `tty`、`yield_time_ms` 和 `max_output_tokens`。命令在 yield 时间内结束则直接返回 completed；仍运行则返回 `process_id/status=running` 和当前增量输出。`write_stdin(process_id, chars, yield_time_ms)` 既可写入 stdin，也可用空 `chars` 轮询。Amadeus 使用 `process_id` 而不是 `session_id`，避免与 Session 混淆；原始命令审批覆盖该 Process 的后续输入/轮询，但每次调用仍审计，且不能操作其他 Run 创建的 Process。

命令输出采用有界 head+tail，而不是达到上限后只保留开头；结果同时报告 retained/total bytes、lines、truncated、exit code、duration 和 running/completed/cancelled/timed_out。Run 取消、Session 关闭和进程退出必须清理 Process/PTY，按 process ID 串行化 stdin 与 poll，避免并发读取破坏输出顺序。

### 14.7 执行流水线

```text
ToolRouter: lookup/visibility → exactly-once schema validation
          → SupportsParallelToolCalls scheduling
          → ToolHandler.Handle
          → ToolOutput/ToolCallOutcome normalization
          → Event/Audit/RunDiff projection → replay
```

所有内置和动态工具共享 Router/Handler 主链，但只有对应 Handler 才调用领域安全组件：文件 Handler 使用 FileSystemPolicy，ApplyPatchHandler 使用 PreparedPatch/RunDiffProjector，ExecuteCommandHandler 使用 ExecPolicy/ApprovalReviewer/Sandbox，MCP/Web 使用各自 Manager/Provider。Audit 记录工具名、参数摘要/hash、目标资源、并行能力、策略结论、审批结果、耗时、partial 和 outcome，不记录凭证或无限正文。

Registry 与模型可见 Tool Set 分离。Tool Exposure 至少支持 `Direct/Conditional/Deferred/Hidden`：核心探索、Patch 和命令工具 Direct；`view_image` 按模型图片 capability Conditional；Web/MCP/Skill 按配置与发现结果 Conditional；动态 MCP/SubAgent 可 Deferred；迁移期 `write_file` Hidden。ContextManager 只计算当前 RequestView 实际可见 Tool Schema，不再默认把 Registry 全量快照发送给每次模型请求。`tool_search` 只有动态工具数量真实造成上下文或选择问题后再立项。

### 14.8 并发规则

- Tool 并发对齐 Codex，由 Handler 的 `SupportsParallelToolCalls() bool` 声明，不建立 Shared/Exclusive 枚举，也不建立文件、目录、Process 或参数级读写锁。
- 默认值为 false；只有内置只读 Handler 或具备可信只读元数据的动态 Handler 可以返回 true。

| 前序 Handler | 后续 Handler | 执行关系 |
|---|---|---|
| supports parallel | supports parallel | 可在 `max_parallel_tools` 范围内重叠 |
| supports parallel | non-parallel | 后者等待此前并行批次完成 |
| non-parallel | supports parallel | 后者等待前者完成 |
| non-parallel | non-parallel | 严格按模型调用顺序执行 |

这张表已经完整覆盖首版需要裁决的并发关系。路径、目录、`process_id`、MCP target 等 canonical target 仍然需要被解析和记录，但它们属于策略、审批、授权、审计、Diff 与 UI 数据，不生成第二层 Mutex，也不参与 Router admission。该取舍直接对齐 Codex；如果未来需要不同文件并行写，必须用真实性能数据和新的安全设计单独立项。

- 首版 `apply_patch`、`execute_command`、`write_stdin` 和未知动态 Handler 一律返回 false；不会因为目标路径或 `process_id` 不同而尝试并行。
- 使用固定大小 worker pool 或等价有界 admission，不为每次调用创建无界 goroutine；实现采用“连续 parallel 批次 → non-parallel 屏障 → 下一 parallel 批次”，等待必须响应 `context.Context` 取消。
- 一个调用失败不自动取消独立调用；上下文取消或策略拒绝除外。
- 结果按原始 tool call 顺序回灌，保证行为可复现。
- canonical target 只服务于 Path Policy、Approval、Grant、Audit、RunDiff 与 UI，不参与并发锁计算；ToolSpec 不携带泛化 TargetStrategy。
- 只有真实性能数据证明“不同文件的并行写入”具有显著收益后，才单独设计资源级锁；它不属于首个可用版本或当前目标架构。

## 15. 安全模型

Amadeus 的本地安全模型由 PermissionProfile、Run/Session Permission Store、PathResolver/FileSystemPolicy、轻量 ExecPolicy、Linux Sandbox、SessionApprovalStore、人工审批和 Audit 共同组成。Permission Check 由所有访问文件系统资源的 Handler 显式调用；Linux Bubblewrap 只强制 Sandboxed Shell/子进程，其他平台使用 Unsandboxed 执行并明确提示宿主 Shell 未被限制。

### 15.1 路径围栏

- 所有文件工具先以 RunContext.CWD 为锚解析相对路径，接受规范绝对路径和 `../`，再生成 canonical absolute path。
- PathResolver 负责词法规范化、现有祖先、软链接、对象类型和写目标检查；FileSystemPolicy 负责 read/write/deny 决策，二者不能继续由单 Project Root Guard 混合承担。
- WorkspaceRoots 与 TemporaryRoots 形成基础 Writable Roots，不随模型参数或进程 `chdir` 隐式变化；`Allow for this run` 与 `Allow for this session` 分别将 Additional Writable Roots 写入 RunPermissionStore 与 SessionPermissionStore，并生成可审计的权限状态变化。
- ReadOnlyRoots 禁止写入，DeniedRoots、软链接逃逸和非目录父 Root 直接拒绝；普通 WorkspaceRoots 外路径不再天然等于越界错误，因为 `ReadHost=true` 允许宿主文件系统读取。
- Prepare 阶段完成 policy decision 并冻结 canonical target；实际系统调用前只做 prepared target 的 staleness/identity revalidation，不能重新解析成不同目标。SandboxRunner 只对 Sandboxed Shell 和子进程执行 OS 级强制，降低 TOCTOU 和字符串分析绕过风险。

当前 `project.Root + PathGuard` 是单根历史实现基础，迁移时只保留真实路径解析、文件类型和软链接防逃逸代码，并拆分为 `PathResolver + FileSystemPolicy`；`project.Root` 的持久化身份职责迁移为 `Project.RootPath`，不再参与权限判断。`ErrPathOutsideRoot` 不再作为所有绝对路径、父目录或项目外读取的统一错误；目标 ToolOutput/ToolCallOutcome 使用 `path_denied`、`path_not_found`、`path_type_mismatch`、`symlink_escape` 和 `sandbox_denied` 等明确类型。

探索 Handler、Patch、cwd 校验、ripgrep/Go fallback、Approval、Audit 和 RunDiffProjector 必须使用同一解析结果。目标键从 project-relative path 改为 canonical absolute path；ToolOutput/Audit 同时记录 requested path、resolved path、access、matched path 与 permission source。用户界面可以优先显示相对 CWD 或最近 Workspace Root 的友好路径，但不能丢弃绝对事实键。ToolRouter 的并行调度不读取这些路径，也不建立资源锁。

Plan Mode 可读取 FileSystemPolicy 允许的跨目录内容，但不获得任何 Writable Root 的修改能力。execute Mode 的结构化写入允许基础 Writable Roots、Run Root Grants 和 Session Root Grants；ReadOnlyRoots 或 DeniedRoots 不能通过等价 Shell 绕过。

### 15.2 ExecPolicy 与命令执行

- ExecPolicy 只决定 `skip / needs_approval / forbidden`，不承担文件系统权限 Enforcement。
- 命令仍进行空值、NUL、明显畸形输入和极小灾难性模式检查；不构建完整 Shell AST，不枚举命令内部路径。
- Sandboxed 下，Permission Check 通过且未命中极小 forbidden 集的普通命令直接在 Bubblewrap 中执行。
- Unsandboxed 下，Permission Check 通过后仍对完整命令执行 Operation Approval；`Allow once` 只执行当前调用，`Allow for session` 才写入 SessionApprovalStore。
- `exec.CommandContext` 负责取消，输出按字节、行数和 token 限制。

目标 ExecPolicy 不再维护 low/moderate/high 到 allow/deny 的复杂命令分类树。Risk 只作为 UI/Audit 展示；策略结论只有三态。`rm -rf /`、磁盘擦除、关机或其他能够广泛破坏宿主机且无法由当前 Sandbox/Profile 安全表达的极小集合可以 `forbidden`；普通构建、测试、Git、格式化、依赖安装、网络命令和复杂 Shell 在 Sandboxed 模式默认执行，在 Unsandboxed 模式统一 `needs_approval`。未来如有真实需求，可增加 Codex 风格显式 prefix rules，但当前不实现复杂 Policy DSL。

ExecPolicy 不识别 command 字符串中的相对或绝对文件 Root。`execute_command.cwd` 在 Prepare 阶段相对 RunContext.CWD 解析并冻结为存在、可读且未命中 DeniedRoots 的 canonical absolute directory，不要求命中 Writable Roots；command 内部相对 Root 由 Shell 基于该 cwd 解释，变量、脚本、解释器和子进程访问只有在 Sandboxed 模式下由 Bubblewrap 决定。Unsandboxed 无法可靠限制这些访问，因此审批面板必须明确显示“unsandboxed host execution”，而不是用字符串扫描制造虚假的 Root 安全保证。

### 15.3 审批与审计

- Approval Request 包含工具、规范化参数 hash、风险级别和原因。
- CLI/TUI/API 提供不同 Handler，Runtime 只依赖 `ApprovalHandler`，不直接读取终端输入。
- 审计为 JSONL，记录时间、会话、工具、策略/审批结果、审批范围和耗时。
- API key、Authorization header、图片二进制和完整敏感正文必须脱敏或省略。

Approval 向 Codex 的 Permission/Approval 分层靠拢，不再采用“读不询问、写/命令一律询问”的固定副作用分类，也不要求用户配置复杂 Policy DSL：

```text
权限范围内的确定性结构化读写 → 直接执行
缺少 Writable Root             → permission_required → request_permissions
Sandboxed 普通命令             → Bubblewrap 直接执行
Unsandboxed 新命令             → Operation Approval
Web 网络                       → 默认允许并执行 SSRF/Redirect Guard
Denied/ReadOnly 写入与极小灾难性命令 → 直接拒绝
```

固定工具分类如下：

| 工具/行为 | MVP 行为 |
|---|---|
| `read_file`、`list_dir`、`glob_files`、`grep_code`、`view_image` | FileSystemPolicy 判定可读后直接执行；`view_image` 还需模型 capability 与媒体预算通过 |
| `apply_patch` | 所有目标位于 Effective Writable Roots 时直接执行；缺少 Root 时返回 `permission_required`，由模型调用 `request_permissions` 后重新发起 Patch；成功变化投影给 RunDiffProjector |
| `request_permissions` | 只接受规范化 Writable Roots；提供 `Allow for this run / Allow for this session / Deny`，分别写入 RunPermissionStore、SessionPermissionStore 或不写入 |
| `execute_command`（Sandboxed） | 读取显式 `requested_permissions.writable_roots` 并执行统一 Permission Check；通过后普通命令直接进入 Bubblewrap，命令字符串内部 Root 不解析 |
| `execute_command`（Unsandboxed） | 先执行相同 Permission Check，再对完整命令执行 Operation Approval；`Allow once` 不缓存，`Allow for session` 写入 SessionApprovalStore |
| `write_stdin` | 只允许访问当前 Run 已审批 Process；空输入轮询或写 stdin 均审计，不重复扩大命令权限 |
| Web Search/Fetch | MVP 默认允许网络；继续执行 Provider 配置、SSRF、Redirect、timeout 与输出预算检查，不建立 NetworkPermissionStore |
| MCP Tool/Resource | 使用已配置 Server、Binding 与既有 MCP 目标审批，不复用文件 Root Grant 代替远端能力判断 |
| Skill 读取说明文件 | 按只读工具执行，不额外审批 |
| Skill 引发写入、命令、网络或 MCP 调用 | 复用对应工具审批，不建立 Skill 专属审批系统 |
| 普通 WorkspaceRoots 外可读路径 | 直接读取，不因跨目录单独审批 |
| 基础 Writable Roots 外的结构化写 Root | 返回 `permission_required`；模型调用 `request_permissions`，批准后重新发起原 Tool |
| ReadOnly Root 写入、Denied Root、软链接逃逸 | 直接拒绝，任何 Permission Grant 都不能绕过 |
| `rm -rf /`、磁盘擦除、关机等极小灾难性命令 | 直接拒绝，任何普通 grant 都不能绕过 |

Tool 执行顺序固定为：ToolRouter 唯一一次参数 repair/parse/schema 校验 → Handler 并行调度 → `ToolHandler.Handle` → 领域内 Permission/策略检查 → 按需 Operation Approval/Sandbox → ToolOutput/ToolCallOutcome → RunDiff/Audit/Event 投影。Permission 不足时当前调用终止；`request_permissions` 返回后由模型重新调用原 Tool，不暗中恢复旧 Handler。ApplyPatchHandler 的 PreparedPatch 与 ExecuteCommandHandler 的 ExecRequest 只在各自领域内复用，通用 Router 不提取 Root、Parse Patch 或扫描 Shell Root。

TTY 使用两套语义明确的三项选择器。`request_permissions` 显示 `Allow for this run / Allow for this session / Deny`；前两项分别写入 RunPermissionStore 与 SessionPermissionStore。Unsandboxed Command Operation Approval 显示 `Allow once / Allow for session / Deny`；只有第二项写入 SessionApprovalStore。默认 Rich Inline TUI 第一项默认选中，用户通过 `↑/↓`（兼容 `j/k`）移动，按 Enter 确认，Esc 直接拒绝；`y/s/n` 继续作为无提示兼容快捷键。删除 `always` 和永久自动授权。

文件 Permission 与命令 Operation Approval 使用三份互不替代的运行时状态：RunPermissionStore 保存当前 Run Additional Writable Roots，SessionPermissionStore 保存当前活动 Session Additional Writable Roots，SessionApprovalStore 只保存 Unsandboxed Command 的 `ApprovedForSession` 键。SessionApprovalStore 不生成、保存或扩大 Root Permission；任何缓存命中前都必须重新构造 EffectivePermissionProfile 并完成 Permission Check。

```go
type CommandApprovalKey struct {
    Shell         string
    Command       string
    CWD           string
    TTY           bool
    IsolationMode IsolationMode
}

type SessionApprovalStore interface {
    IsApproved(CommandApprovalKey) bool
    Approve(CommandApprovalKey)
}
```

Key 表达用户实际批准的完整执行语义：Shell 使用规范绝对可执行路径；Command 只拒绝 NUL 并统一 CRLF 为 LF，不做 `strings.Fields`、Shell AST、空格合并或 Root 提取；CWD 使用 ExecRequest 中的 canonical absolute cwd；TTY 与 `sandboxed/unsandboxed` 隔离模式必须参与身份。这里的 CWD 不保证恒等于 RunContext.CWD，因为 `execute_command` 可以显式传入另一个可读工作目录。timeout、yield、输出预算、requested permissions、EffectivePermissionProfile 和 Permission Store 内容不进入 Key，因为它们分别属于等待/展示或独立 Permission Check。

SessionApprovalStore 是当前活动 Session 内存中的集合，而不是 `map[key]permission`：Key 本身就是完整批准事实，Value 只需要空结构体或布尔真值。推荐实现为 `map[CommandApprovalKey]struct{}`，并在并发访问时使用互斥锁保护。只有 Unsandboxed `execute_command` 的 `Allow for session` 写入集合；`Allow once` 表示本次调用继续执行但不写 Store，`Deny` 同样不写。命中 Store 只跳过本次 Command Operation Approval，不能跳过 Permission Check、ExecPolicy、Audit 或 ExecRequest 的一致性检查。

例如，用户在当前 Session 批准：

```text
Shell         = /bin/bash
Command       = go test ./...
CWD           = /workspace/amadeus
TTY           = false
IsolationMode = unsandboxed
```

Store 中保存的就是这一个精确 Key。后续只有五个字段全部一致的命令调用可以复用批准；改成 `/bin/sh`、修改命令文本、切换到 `/workspace/other`、启用 TTY 或进入 `sandboxed` 模式都会形成新 Key。即使命中旧 Key，新的 `requested_permissions.writable_roots` 仍要重新参与 EffectivePermissionProfile 检查。Audit 单独记录时间、理由和用户决定。`write_stdin` 继承原 Process 的 Permission、IsolationMode 与 Operation Approval，不重新授权。任何 Store 都不能绕过 ReadOnlyRoots、DeniedRoots、forbidden ExecPolicy、未配置 MCP Server 或未声明远端能力。

ApprovalRequest 除 canonical arguments hash 外，还必须携带或可派生脱敏后的 Action Summary。MCP 审批面板至少展示 server、远端 tool/resource、transport、脱敏 target 和配置来源；stdio server 需要展示将启动的 command/args 摘要，HTTP server 展示 host，不显示 env、headers 或凭证。项目 `.amadeus/mcp.yaml` 定义的 stdio server 不能只以泛化的“tool accesses external systems”提示用户。

非 TTY 不读取 stdin，也不使用配置自动允许：所有需要审批的调用直接拒绝。配置文件删除 `approval.enabled` 与 `approval.default`，Approval 不能由用户关闭；未来若出现明确的自动化场景，再单独设计受限的非交互授权入口。

M3 已有的 canonical ApprovalRequest、参数 hash、Terminal Handler、Audit 和事件主链继续复用；删除全工具 ToolAuthorizer，审批逻辑进入 `ExecuteCommandHandler → ToolOrchestrator → ApprovalReviewer`，MCP stdio 启动使用同一 ApprovalReviewer 能力。旧 Run-local 通用 `GrantCache` 拆分并提升为 SessionRuntime 所有的 SessionPermissionStore 与 SessionApprovalStore。当前收敛删除旧 CommandGuard 的路径 token 拒绝、按工具名宽泛授权和“所有写入/命令一律审批”，但不实现 Docker、macOS/Windows 原生 Sandbox 或复杂 Policy DSL。

`OpenJSONLFile` 自动建立 0700 父目录、以 append 模式打开 0600 regular file，并拒绝最终 symlink；每条记录先独立编码，再在锁内单次写入，保证并发调用仍是一行一个合法 JSON object。ToolRouter 与各领域 Handler 对 route/validation、policy allow/deny、用户/default/grant 决策和 handler error 统一记录耗时；配置了 Audit Sink 时，必须在副作用前 fail closed，并与原始拒绝或策略错误保留完整 error chain。Agent bootstrap 同时强制注入 ApprovalReviewer 与 Audit Sink，避免真实链路静默绕过审批或审计。

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

### 16.1.1 ContextManager 与唯一历史源

目标架构不再由 `BaseContextBuilder`、持久化 Conversation Summary、Previous Work 和 Reactor Runtime Messages 分别维护上下文。统一使用：

```text
SQLite append-only Rollout
        ↓ replay
SessionHistory
        + Static Context
        + Tool Specs
        + Current Environment
        ↓
ContextManager.Prepare
        ↓
RequestView
        ↓
Provider Adapter
```

`SessionHistory` 是 canonical rollout 的内存镜像并由 SessionRuntime 独占；SQLite `rollout_items` 是持久化唯一事实源。模型完成一个 assistant item、Tool Call/ToolOutput、Plan Update 或 interruption marker 后，由 RunRuntime/Reactor 请求 `SessionRuntime.Append`，统一更新持久层与内存历史。Reactor 不再维护独立完整 `RuntimeMessages`。

```go
type ContextRequest struct {
    Request       RequestContext
    PreviousUsage *llm.Usage
}

type RequestView struct {
    Messages []llm.Message
    Tools    []tool.Spec
    Usage    ContextUsage
    SHA256   string
}

type ContextManager interface {
    Prepare(context.Context, ContextRequest) (RequestView, error)
}
```

ContextManager 内部分为四个职责：

1. `RequestContextFactory`：从 RunContext、SessionHistory View、当前环境、ToolRouter 和指令解析结果生成一次采样快照；
2. `StaticContextBuilder`：组装 System Prompt、有效 `AGENTS.md`、cwd、日期时区、RunMode、Skill/MCP 索引与安全上下文；
3. `HistoryReplayer`：SessionRuntime 初始化时从最近有效 `context_compaction` 加后续 tail items 重建有效历史；
4. `PromptProjector`：将 Provider-neutral RolloutItem 规范化为 `[]llm.Message`，并保证 Tool Call/Result 配对；
5. `ContextCompactor`：超过阈值时生成 Replacement History，通过 SessionRuntime.Append 持久化，然后重新 Prepare。

`RequestView` 是本次模型请求的唯一视图，但不是持久化事实源。任何影响后续恢复的压缩都必须追加 `context_compaction` RolloutItem，禁止只在内存中静默丢弃历史。

### 16.1.2 Per-Think RequestView

每次 `Think` 前都基于 SessionRuntime 当前 History View 生成 RequestContext，再投影 RequestView：

```text
RunContext
  + SessionHistory View
  + Current Environment / Instructions
  + ToolRouter
        ↓
RequestContext
        ↓
Static Context + Effective History + Plan Projection
        ↓
ContextManager.Prepare
        ↓
RequestView
```

当前用户消息已经是当前 Run 的第一条 RolloutItem，并始终保持为最近的真实用户指令；中断标记只是此前历史事实，不能覆盖当前用户意图。

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

上下文不再使用不可借用的固定百分比硬分区。System、Instructions、History、Tools、Resources 等分类继续用于统计和诊断，但实际装配使用优先级：

| 优先级 | 内容 | 行为 |
|---|---|---|
| Pinned | System Prompt、有效 `AGENTS.md`、当前用户消息、RunMode、当前未完成 Tool Call/Result 协议组 | 不静默裁剪；自身超限时明确报错并指出来源 |
| High | 最近完整 Rollout Items、当前 Run 最近 Tool Results、interruption marker、当前 PlanState | 优先保留 |
| Medium | 较旧有效历史、已完成 PlanItem 摘要、较旧 Tool Results | 超限时进入 Replacement History |
| Low | 重复 Tool 输出、已由结构化 ToolOutput metadata 表达的日志、过时 workspace snapshot、非必要资源索引 | 首先删除或按需加载 |

未使用的分类预算可以借给其他分类；`BudgetUsage` 必须覆盖 current goal、plan state、skill index、runtime replay、tool schemas、tool results 和 protocol overhead，而不只是记录 system/instructions/history/tools。

压缩单元不是单条 Message，而是原子 `MessageGroup`：

- 一个完整 user/assistant 历史组；
- assistant Tool Calls 与所有对应 Tool Results；
- current user goal；
- system/developer pinned source；
- context compaction replacement baseline。

不得保留孤立 Tool Result，也不得删除 Tool Result 后留下待完成的 Tool Call。当前用户消息、当前 Tool 协议组和最近完整 rollout groups 必须优先保留。

### 16.1.5 Tool Result Context Projection

Tool 自身的分页、行数和字节上限是第一层保护；进入 LLM Context 前还需要统一的 `ToolResultContextProjector`。完整 ToolOutput 已进入 canonical rollout，并可按策略关联 Artifact 或 Audit；RequestView 只注入有界模型投影：

```text
tool result
    ↓
structured metadata + summary
    ↓
token-aware head/tail projection
    ↓
LLM tool-result message
```

命令输出、日志和大文件片段优先保留开头的环境/标题与结尾的错误、结论、exit code，中间使用明确 omission marker。投影按 token 预算计算，不只按字符数；投影不得修改 canonical ToolOutput，也不得隐藏 `partial/truncated/exit_code/error` metadata。

### 16.1.6 Replacement History Compaction

Conversation 压缩采用 Codex 风格 Replacement History，而不是独立 `session_summaries` 表或内存字符串拼接：

- 原始 RolloutItem 永不因压缩被删除；
- ContextCompactor 对当前有效历史生成一组新的 Provider-neutral history items；
- 压缩结果作为 `context_compaction` RolloutItem 追加到同一个 `rollout_items` 表；
- `context_compaction.payload_json` 保存摘要文本、完整 `replacement_history`、窗口序号、覆盖到的 item sequence、source hash 和创建模型；
- 恢复时从后向前找到最新有效 compaction，以其中的 Replacement History 为基线，再顺序追加其后的 tail items；
- Replacement History 必须保留当前目标、关键决策、已修改文件、验证结果、未解决问题、有效 PlanState 与必要 Tool 协议事实。

```json
{
  "summary": "此前会话正在重构 Session Runtime……",
  "replacement_history": [
    {"role": "user", "content": "此前目标、约束与关键事实的结构化摘要"},
    {"role": "assistant", "content": "已完成工作、当前状态与待处理事项"}
  ],
  "covered_through_sequence": 128,
  "source_hash": "...",
  "provider": "openai",
  "model": "..."
}
```

Replacement History 是“下一次模型请求从哪里继续”的持久化替代基线，不是对原始历史的覆盖更新，也不是新的业务事实表。

### 16.2 数据生命周期边界

Amadeus 不采用 PaiCLI 式全能 `MemoryManager`，也不在近期版本实现由模型自动推断用户偏好、项目事实或可复用 Lesson 的 Durable Memory。自动长期记忆需要额外解决误判、冲突、过期、敏感信息、检索排序和可解释删除等问题，工程成本高且对编码 Agent 的实际收益不稳定。用户希望长期生效的偏好和项目规范改由显式 `AGENTS.md` 表达。

系统必须区分以下数据：

| 数据 | 语义 | 所有者 | 持久化方式 |
|---|---|---|---|
| Canonical Rollout | 用户、Assistant、Tool、Plan、Context 与中断的有序事实 | `internal/session` RolloutStore | SQLite `rollout_items` append-only |
| SessionHistory | Canonical Rollout 的当前进程内镜像 | `internal/session` / `internal/agent/runtime` | 从 Rollout replay；不作为第二持久化源 |
| Run Working State | RunRuntime、RunState、Reactor Iterations、PlanState、Usage 和 Budget | `internal/agent/runtime` / `internal/agent/react` | 当前 Run 内存；需要恢复的事实及时追加为 RolloutItem |
| Static Context | System Prompt、Instructions、Tools、Skills 与环境来源清单 | `internal/context` StaticContextBuilder | 不单独持久化；必要时只以 metadata/hash 形式写入 `context_snapshot` item |
| RequestView | 当前一次 Think 实际发送给 LLM 的受预算动态投影 | `internal/context` ContextManager | 不单独持久化；可记录 hash/diagnostics |
| Replacement History | 对旧有效历史的持久化替代基线 | `internal/context` ContextCompactor | `rollout_items.kind=context_compaction` 的 payload |
| InstructionDocument | 用户和项目主动维护的长期指令 | `internal/instruction` | `AGENTS.md` 文件 |

必须保持以下边界：

- Tool Call 与 ToolOutput 投影出的 Tool Result 是正式 RolloutItem；Reactor 不再维护重复的 Observation/Evidence 事实树，也不自动生成长期偏好或项目规则。
- Run Budget 与 Request Context Profile 属于不同 Policy；前者累计整个 Run，后者限制单次 LLM 请求，均不属于 Conversation 或 Instruction。
- Compaction 只追加 Replacement History，不删除或改写原始 RolloutItem。
- Replacement History、interruption marker 和 Tool Result Projection 都不能获得 `AGENTS.md` 或当前用户消息的指令优先级。
- ContextManager 只投影 RequestView；需要影响恢复语义的变化必须通过 RolloutStore append，不能只修改内存历史。
- MCP resources、Skill 和 Web 内容属于带来源的外部上下文，不能获得 `AGENTS.md` 的指令优先级。

### 16.3 `AGENTS.md` 指令层级

Amadeus 使用明确、可编辑、可版本控制的 `AGENTS.md` 代替模型推断式长期记忆：

| 层级 | 位置 | 作用域 |
|---|---|---|
| 用户级 | `$AMADEUS_HOME/AGENTS.md` | 所有 Amadeus 项目 |
| 默认工作区根级 | `<cwd>/AGENTS.md` | CWD 对应 Workspace Root |
| 附加工作区根级 | `<add-dir>/AGENTS.md` | 对应附加 Workspace Root |
| 目录级 | `<workspace-root>/<path>/AGENTS.md` | 该文件所在目录及其后代 |

`AMADEUS_HOME` 是 Amadeus 配置根，`Project.RootPath` 是持久化项目身份，CWD 与 `--add-dir` 共同形成 WorkspaceRoots，三者必须独立解析。默认配置和用户级指令来自 AMADEUS_HOME；每个 Workspace Root 分别提供自己的根级/目录级指令。CLI 通过显式 `--project` 或启动工作目录确定初始 CWD 与 Project.RootPath，不得把 `AMADEUS_HOME` 或第一个 `--add-dir` 隐式当作默认工作区。

每个 `AGENTS.md` 作为完整 InstructionDocument 读取，至少记录 `source/path/workspace_root/scope/hash/content`。文件必须是受大小预算约束的 UTF-8 文本；项目指令路径必须位于声明的 WorkspaceRoots 内，符号链接不得绕过 PathResolver/FileSystemPolicy。修改某个 Workspace Root 下文件时，只应用用户级指令、该 Root 的根级指令和目标目录链指令，不能让 CWD 所在 Root 的目录级规则错误覆盖其他 Root。指令中的命令示例只是上下文，不会自动执行，也不能绕过 Tool Policy、Approval 或审计。

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

M3-07 的历史 `ProjectLoader` 绑定单个 `project.Root`；目标架构将其收敛为按请求选择的 `WorkspaceLoader`。Resolver 先根据 canonical target 选择唯一覆盖它的 Workspace Root，再从该 Root 生成 `.`、`pkg`、`pkg/service` 这类稳定目录链。每一级只查找该目录直属的 `AGENTS.md`，结果始终按 Workspace Root 到最深目录排列；兄弟目录和其他 Workspace Root 的规则不会进入当前链。中间目录尚不存在时停止向下发现，但保留此前已经加载的上层指令，支持新文件所在父目录尚未完全创建的场景。

目标目录语义保持显式：M3-08 通过 `TargetKind=file|directory|command_cwd` 区分目标，文件操作使用父目录，目录操作和命令使用目标目录或 cwd，避免发现器根据文件是否存在猜测 file/dir 类型。项目指令默认同样使用每文件 64 KiB 上限；缺失文件正常跳过，已存在的空文件、非法 UTF-8、非普通文件和超预算文件明确失败。

发现器在读取前解析每一级目录和 `AGENTS.md` 的真实路径并验证仍位于本次选定的 Workspace Root。指向该 Root 外部的目录或文件软链接立即拒绝；指向 Root 内部的软链接允许读取，但 Document 保留工作区内看到的逻辑路径和对应逻辑 Scope，使目录优先级、Resolution 校验和用户诊断保持稳定。M3-09 的历史围栏代码可复用，但不能恢复单 Project Root 权限边界。

### 16.4 发现、作用域与优先级

Instruction Resolver 先读取可选的 `$AMADEUS_HOME/AGENTS.md`，再从覆盖目标的 Workspace Root 沿目标路径逐层发现 `AGENTS.md`。目录级文件的作用域是其所在目录树；操作某个文件或目录前，必须使用覆盖该目标的完整指令链，不能只加载 CWD Root 后忽略更深层规则。对某个 Workspace Root 执行的命令至少应用用户级与该 Root 根级指令；若命令 cwd 位于子目录，则继续应用从该 Root 到 cwd 的目录级指令。

冲突时从高到低为：

1. Amadeus 内置安全边界、Tool Policy 和 Approval Policy。
2. 用户当前明确提出的任务要求。
3. 距离目标路径最近的目录级 `AGENTS.md`。
4. 更上层目录和项目根 `AGENTS.md`。
5. `$AMADEUS_HOME/AGENTS.md`。
6. Amadeus 默认行为。

Resolver Port 目标使用 `ResolveRequest{WorkspaceRoots, TargetPath, TargetKind}`。TargetPath 先规范化为绝对路径并确定唯一 Workspace Root，再转换为该 Root 内不可逃逸的相对路径，`.` 表示工作区根。`TargetKind` 只接受 `file`、`directory` 和 `command_cwd`；Resolution 必须回显选中的 workspace root、target path 和 kind。`Resolution.Documents` 按“用户级 → Workspace Root → 逐层目录级”从宽到窄排列；空列表是合法结果，表示当前目标没有持久指令。Resolution 校验每个 Scope 确实覆盖目标对应的有效目录、来源路径不重复、同一作用域不重复、顺序不倒置，并要求工作区来源文档位于选中的 Workspace Root 内且文件所在目录与 Scope 匹配。

目标 `LayeredResolver` 组合一个 UserLoader 和本次选中 Workspace Root 的 WorkspaceLoader：先读取用户级文档，再发现工作区链，最终保持 user → workspace root → deeper directory 的稳定顺序，因此越靠后的工作区文档具有越高普通指令优先级。若 `AMADEUS_HOME` 与目标 Workspace Root 恰好相同，同一路径不会作为 user/workspace 两份文档重复注入，而只保留更具体的工作区来源。任一加载、围栏或 Domain 校验错误都会终止解析，不返回部分 Resolution。

M3-05～M3-08 已交付单 Root Resolver 历史基础；目标架构需要把请求、Loader 与校验迁移到 WorkspaceRoots，但继续保持 `internal/instruction` 不依赖 CLI/TUI、Provider/LLM、Conversation 或 Store。

项目级指令因此高于用户级指令，更深目录高于更浅目录；当前用户请求可以覆盖普通工程约定，但不能绕过安全和审批。Assembler 不把多层文件静默拼成无来源文本，而是使用独立结构化包络注入，并保留稳定顺序、路径、scope 和 hash，便于诊断、缓存和审计。无法确定冲突含义时应向用户提问，而不是让模型猜测。

### 16.5 Session、Run 与 Canonical Rollout

Session 是项目内可恢复对话，Run 是一次真实用户输入的执行记录，RolloutItem 是模型与工具执行过程中真实发生的持久化事实。SessionRuntime 在加载 Session 时 replay 并持有 SessionHistory；SessionCoordinator 在任何模型或工具执行前原子创建 Run，并将用户输入作为第一条 RolloutItem，随后返回不含 History 的 RunContext。

completed、interrupted 与 failed Run 的真实 assistant/tool/plan 历史均已在执行过程中逐项持久化，不再等到 FinishRun 时只写最终 Message 或 Previous Work。`Turn` 不作为 Domain、表名、事件关联字段或恢复参数。

### 16.6 SessionHistory 与 RequestView

SessionHistory 从最新有效 Replacement History 加其后 Rollout tail 重建，是 SessionRuntime 独占的 canonical history 镜像；RequestContext 是一次模型采样的动态环境快照，RequestView 是 ContextManager 基于它生成的临时模型输入。RequestView 可以做 Provider 方言映射、ToolOutput 有界投影和静态上下文注入，但不能静默改变后续 resume 会看到的历史。

assistant Tool Calls 与对应 Tool Results 必须作为原子协议组；当前用户消息、有效 Instructions、未完成工具协议和最近错误不得静默裁剪。需要丢弃旧上下文时必须先追加 `context_compaction`，再基于 Replacement History 重建。

### 16.7 中断 Marker 与下一 Run

用户按 `Esc`、父 Context 取消或进程异常退出时，RunRuntime：

1. 取消 Reactor 和活动工具，并给予短暂 graceful shutdown 窗口；
2. 为已经持久化但尚无结果的 Tool Call 追加 `status=interrupted` 的合成 Tool Result，提示命令可能已部分执行；
3. 追加 `run_interrupted` marker，说明中断事实和重新检查工作区的必要性；
4. flush Rollout，再将 Run 更新为 `interrupted`。

下一条用户输入始终创建新 Run，并作为新的 `user_message` 追加在 marker 之后。ContextManager 直接向模型提供真实历史、中断标记和当前用户输入，不做“继续/新任务”关键词分类，也不强制继续或 re-plan 旧目标，由模型结合当前请求判断。

进程启动时发现遗留 `running` Run，则将其恢复为 `interrupted`，为悬空 Tool Call 补齐合成结果，并追加 `process_terminated` 原因的 interruption marker。系统恢复历史事实，不恢复旧 Iteration、工具游标、Go 调用栈或活动 Future。

### 16.8 不持久化的运行时状态

SessionRuntime、RunRuntime、RunState、RequestContext、Iteration、RequestView、活动 Process、审批等待、Tool Call Future 和流式聚合器都不建立独立业务表。PlanState 本身不建表，但每次用户可见 `update_plan` 作为 `plan_update` RolloutItem 持久化；模型流式 delta 只用于 UI，完成后的 assistant item 才进入 canonical rollout。

### 16.9 实施边界

- SessionCoordinator 负责 Session/Run 数据事务与只读 RunContext；
- SessionRuntime 负责 SessionHistory、活动 Run、Session 全局 item sequence、Rollout append/flush、SessionPermissionStore/SessionApprovalStore 生命周期与事件衔接；
- RunRuntime/RunState 负责取消、Reactor 驱动、PlanState、Usage、审批/进程资源和 Finish Once；
- ContextManager 每次 Think 先生成 RequestContext，再生成 RequestView；
- Reactor 只消费 RequestView、Tools 与 Budget，不直接读写 SQLite。

## 17. MCP、Skill 与扩展

MCP 与 Skill 都属于 Session 级扩展能力，但不能直接塞入 SessionRuntime 形成新的万能对象。目标结构固定为：

```text
SessionRuntime
└── ExtensionRuntime
    ├── MCPRuntime
    │   ├── ConnectionManager
    │   ├── ServerState
    │   ├── CatalogCache
    │   └── CatalogRevision
    └── SkillService
        ├── MetadataCatalog
        ├── CatalogRevision
        ├── ContentLoader
        └── LoadWarnings

每次模型采样：
RequestContext
├── MCPBinding
├── SkillCatalogSnapshot
└── SkillInjections
```

ExtensionRuntime 随活动 Session 创建，在同一交互 Session 的多个 Run 之间复用；恢复历史 Session 时从配置和文件系统重新构造，不将连接、Catalog 或 Skill 正文持久化到 SQLite。MCP/Skill 产生的用户可见结果仍统一进入 ToolInvocation → ToolHandler → ToolOutput → `tool_result` RolloutItem 主链，扩展层不能建立第二套执行器、历史或审批系统。

### 17.1 MCP

- 用户级配置固定为 `$AMADEUS_HOME/mcp.yaml`，项目级配置固定为 `<cwd>/.amadeus/mcp.yaml`；首版只读取 CWD 对应默认 Workspace Root 的项目配置，不扫描 `--add-dir`。项目同名 server 整体覆盖用户 server，不做 command/args/env/headers 字段级混合。
- 配置只定义 server name、transport、command/args/env 或 url/headers、timeout 和 enabled；字符串支持环境变量展开，凭证不进入日志、TUI、Audit、Trace 或 `config explain`。解析后的 server 必须保留 `user/project + config path` 来源元数据，供审批和诊断使用。
- 协议层继续使用成熟 Go MCP Client Library 承担 transport、JSON-RPC 和协议兼容，借鉴 WeKnora 的 Client/Manager/Result Normalizer 边界，但不迁移其 GORM、Tenant、HTTP Handler、Redis、跨实例 Approval 和数据库服务层。PaiCLI Go 的自研 stdio/HTTP JSON-RPC 只作为行为参考，不复制为 Amadeus 协议核心。
- CLI 产品必须保留 stdio 与 streamable HTTP：stdio 是本地 Coding Agent 生态的重要入口，不能照搬 WeKnora 服务端产品“禁用 stdio”的策略；但 stdio 启动属于本地进程执行风险，HTTP 属于外部网络风险，MCP Manager 必须通过 ApprovalReviewer 根据目标 server 配置生成不同的审批摘要。
- MCPRuntime 是 Session 级长生命周期能力：创建 SessionRuntime 时只加载、合并和校验配置，第一次需要某个 server 时才连接并 initialize；同一 Session 的多个 Run 复用连接，SessionRuntime 关闭或切换 Session 时统一关闭。单 server 失败不阻止 Session、Agent 或其他 server 工作；一次 transport/protocol 调用失败只允许一次有界重连，不做无限后台重试。
- 生产工具面继续采用稳定的 lazy gateway，而不是默认把所有远端工具动态展开进 Provider Tool Schema：首批固定 `mcp_list_tools(server)` 与 `mcp_call(server, name, arguments)`，后续增加 `mcp_list_resources(server)` 与 `mcp_read_resource(server, uri)`。这样避免启动延迟、远端 Tool 数量导致的 Schema 膨胀和不同 Provider 的 Tool 上限差异。
- 每个已连接 server 维护有界 `ServerState`：Client、InitializeResult/Capabilities、Tool Catalog、Resource Catalog metadata、ConnectionGeneration、CatalogRevision、状态和最近错误。第一次 list/call 时加载 Tool Catalog；后续 `mcp_call` 从缓存验证远端 Tool，不得每次调用都重复执行 `tools/list`。重连时增加 ConnectionGeneration 并清空旧缓存；显式 refresh 或未来 `notifications/tools/list_changed` 使 CatalogRevision 失效，不实现 TTL 轮询或无限后台刷新。
- ContextManager 每次采样从 MCPRuntime 创建不可变 `MCPBinding`，冻结当前 server metadata、ConnectionGeneration、CatalogRevision 和已加载 Catalog。RequestView 广告的能力与随后 ToolInvocation 执行时使用的 Binding 必须一致，禁止模型看到 Catalog A、执行时静默切换到 Catalog B。由于默认生产面使用 lazy gateway，首版不复制 Codex 的 Prepared MCP Calls 和全量动态 Tool Schema，只实现 generation/revision/snapshot 的最小一致性边界。
- `mcp_list_tools` 返回清理、排序和有界的 name/description/input schema；`mcp_call` 只允许调用当前 Binding/连接代次 Catalog 中已声明的 Tool。gateway 默认 Exclusive；只有当前 Binding 中的远端 Tool 明确声明只读且 server 支持并行时，具体调用才可投影为 Shared。所有调用进入 Validation → MCP target preflight → Approval → Audit → Execute 主链。审批目标和 Session Grant Key 必须使用 `mcp:<server>:tool:<name>` 或 `mcp:<server>:resource:<uri>`，不能因为本地 gateway 名同为 `mcp_call` 而共享全部授权；UI 同时展示脱敏目标和 user/project 配置来源。
- MCP Tool Result 统一转换为普通 ToolOutput，并产生对应 ToolCallOutcome，不进入 system/developer/`AGENTS.md`。所有正文增加“外部不可信数据”来源包络；远端 `isError=true` 表示调用已送达但业务工具失败，转换为 failed ToolCallOutcome 与带 `remote_tool_error/external_context=true` metadata 的 ToolOutput，不作为终止 Run 的 Go error。只有连接初始化失败、重连失败、协议损坏或内部状态不一致等基础设施致命故障才返回 Go error。超长结果标记 partial；第一阶段保留 bounded text 与 structured JSON，Resources 只自动回灌受限 UTF-8 文本，二进制返回 URI/MIME/size 元信息。
- MCP Prompts、Sampling、Mentions 和完整 Notification 订阅不进入首个可用版本；Resources 是 Tool 稳定后的下一项能力。旧 `mcp__{server}__{tool}` Adapter 与原子 Registry 替换只保留为测试和未来小 Catalog 优化基础设施，不作为默认生产入口。
- 增加用户可观察命令：`amadeus mcp list` 只展示脱敏后的合并配置与来源，不连接 server；`amadeus mcp check` 逐个执行连接/initialize/能力检查后关闭；`amadeus mcp tools <server>` 显式查询 Tool Catalog。TUI `/status` 只展示 configured/connected/error 摘要，不泄露凭证。

### 17.2 Skill

- 用户级 Skill 固定放在 `$AMADEUS_HOME/skills/<name>/SKILL.md`，项目级 Skill 固定放在 `<cwd>/.amadeus/skills/<name>/SKILL.md`；首版只读取 CWD 对应默认 Workspace Root 的项目 Skill，不扫描 `--add-dir`。项目同名 Skill 整体覆盖用户 Skill。MVP 不额外设计用户根目录，也不要求内置 Skill 层或模型推断式自动安装。
- SkillService 采用渐进披露，但启动时只缓存 metadata，不长期保存全部正文：Level 1 为 `name + description + source + path + size/revision` 的 Metadata Catalog；Level 2 为显式选择或模型调用时按需读取的 `SKILL.md` 正文；Level 3 为按需读取的 `references/`。完整正文加载时计算 ContentHash，用于本次 Request 固定内容和诊断文件变化。
- `SKILL.md` 使用 YAML frontmatter，MVP 只接受必填 `name` 和 `description`；name 为小写字母、数字和连字符，正文、描述、Skill 数量和索引总大小均受预算限制。用户/项目扫描拒绝软链接目录逃逸、非 UTF-8、超限文件、重复名称和不完整 frontmatter；无效 Skill 产生带来源 warning 并跳过，不阻塞其他 Skill 或 Agent 启动。
- Skill 有两条明确调用路径。第一条是用户显式选择：文本中的 `$skill-name` 或未来 TUI 结构化选择由入口解析，SkillService 解析唯一 Skill、加载正文并生成 `SkillInjection`，直接进入本次 RequestContext；第二条是模型自主发现：模型只看到 Metadata Index，认为相关时调用 `read_skill`，结果作为普通只读 ToolOutput 回灌。首版不复制 Codex 的隐式命令路径检测，不根据模型或 Shell 命令猜测用户是否想调用 Skill。
- 显式 SkillInjection 表示用户主动选择的本地扩展指令，使用 Provider-neutral 的 contextual user fragment 投影，不伪装成 system、developer 或 `AGENTS.md`。其优先级低于系统安全策略和适用的 `AGENTS.md`，与当前用户任务共同表达授权意图；Skill 文本不能扩大 WorkspaceRoots、关闭 Approval 或覆盖安全策略。模型自主调用 `read_skill` 得到的正文继续作为 Tool Observation，不能提升为指令层。
- `SkillCatalogSnapshot` 在每次采样时冻结 Metadata Index、CatalogRevision 和本次显式 SkillInjection；同一次采样看到的 Index、路径和显式正文必须一致。Catalog、正文和 Injection 不写入独立数据库表；显式 Skill 名称与 ContentHash 可进入 context snapshot/trace，真正影响模型的投影由 RequestContext 生成。
- 生产工具面保留 `read_skill(name, path?, line?, limit?)`：省略 path 时返回有界 Skill 正文、description/source 和可用 Reference 文件列表；提供 path 时只读取该 Skill `references/` 下的相对路径。返回值是当前 Tool Call 的 ToolOutput，不使用 `load_skill → SkillContextBuffer → 下一次 developer message` 隐式注入链，也不允许 Reactor AdditionalMessages 建立 Skill 特例。
- `references/` 继续使用 Skill Root 围栏与专用只读接口，拒绝绝对路径、`..`、软链接逃逸、binary、非 UTF-8 和超限文件；目录列表和正文均有数量/字节预算，不扫描或自动注入整个资料目录。
- Skill 读取本身为 read side effect，不额外审批；Skill 指导模型调用写入、命令、网络或 MCP 时，复用目标工具现有 PermissionProfile、ExecPolicy、Sandbox、Approval 和 Audit，不建立 Skill 专属授权系统。Skill 文本不能关闭审批、绕过 ReadOnlyRoots/DeniedRoots/ExecPolicy/MCP target preflight、扩大 Writable Roots 或自动获得 Session Grant。
- 项目级 Skill 的 `scripts/` 在首版不注册 `execute_skill_script`：因为文件位于 CWD 对应默认 Workspace Root 内，Skill 可以指导模型通过普通 `execute_command` 显式运行 `.amadeus/skills/<name>/scripts/...`，自然复用 PermissionProfile、ExecPolicy、Sandbox、Approval、Audit、timeout 和输出预算。用户级 Skill 位于 `$AMADEUS_HOME/skills`，该目录与 `config.yaml`、`mcp.yaml`、`data/` 一起进入 Amadeus DeniedRoots；首版只允许 ExtensionRuntime/`read_skill` 受控读取说明和 References，通用文件工具与 Sandbox Command 不得读取或执行其中脚本。
- 只有独立 Sandbox 基础设施完成后，才重新评估用户级 `execute_skill_script(skill, path, args, stdin)`。该能力必须使用只读 Skill 挂载、独立临时可写目录、默认无网络、解释器与环境变量白名单、超时/输出限制、软链接防逃逸和每次显式审批；可借鉴 WeKnora 的 Tool/Manager/Sandbox 边界，但不复制其完整服务层。
- Skill frontmatter 可在基础稳定后增加声明式 MCP 依赖，例如 server 与所需 tool 列表。首版只做缺失依赖诊断：说明 server 未配置、未启用或 Catalog 中缺少 Tool；不得自动修改 `mcp.yaml`、下载安装程序、发起 OAuth、连接网络或自动批准。依赖信息属于 Skill metadata，不建立 Skill 专属执行链。
- MVP 不实现 enable/disable Store。增加用户可观察命令：`amadeus skills list` 展示 name/description/source，`amadeus skills check` 展示解析 warning，`amadeus skills show <name>` 展示正文和有界 Reference 列表。只有真实 Skill 数量和禁用需求出现后，再设计 `$AMADEUS_HOME` 下的 disabled 状态文件或交互开关。

### 17.3 实施顺序

MCP 与 Skill 不再按“协议是否存在”判断完成，而按运行时一致性、安全语义和用户可观察性三阶段落地：

1. **P0 运行时与正确性收敛**：MCP Manager 提升为 Session 级 MCPRuntime；增加最小 ConnectionGeneration/CatalogRevision/MCPBinding；Grant Key 改为 server/tool/resource 感知；远端 `isError` 转为 failed ToolExecution；Skill Catalog 改为 metadata/正文分离；增加显式 `$skill-name` → SkillInjection 与模型自主 `read_skill` 双路径；Skill/MCP warning 可被 CLI/TUI 查看。
2. **P1 可使用产品面**：增加 `amadeus mcp list/check/tools` 与 `amadeus skills list/check/show`；完善 Catalog refresh/失效、Resources gateway、SkillCatalogSnapshot 和声明式 MCP 依赖诊断；补充示例配置、Skill 目录示例、真实 stdio/HTTP fixture 与 Approval E2E。
3. **P2 稳定后增强**：支持 structured content、Tool/Resource change notifications、多模态结果和 TUI Skill 自动补全；独立 Sandbox 成熟后再评估用户级 Skill scripts。MCP Prompts、Sampling、全量动态 Tool 展开、自动 MCP 安装/OAuth 和隐式 Skill 调用必须有真实使用场景后再立项。

P0 完成前，不应把当前“代码中已有 MCP/Skill 包”视为最终产品完成：现有配置覆盖、懒连接、lazy gateway、PathResolver 基础和文本结果继续复用，但 Run 级生命周期、无 Binding 的可变 Catalog、远端错误映射、授权粒度和 Skill 调用语义都必须接入新的 SessionRuntime/RequestContext/ToolRouter/ToolHandler 主链。

## 18. Run Diff、项目验证、Web、Browser 与图片

### 18.1 RunDiffProjector

`RunDiffProjector` 是 Core/Run Runtime 内的 Run 级精确 Patch Diff 投影，用于回答“当前 Run 通过 `apply_patch` 精确提交了哪些变化”，不是所有文件系统副作用的审计器、文件备份、恢复机制或新的持久化事实源。目标架构删除模型可见 `revert_run`、全项目 before/after Snapshot、`internal/snapshot` 和自动文件恢复主链，避免在多 Writable Root、Shell 副作用和用户后续修改下作出无法可靠兑现的完整归因或撤销承诺。

```go
type RunDiffProjector struct {
    Valid   bool
    Changes map[string]FileDelta
}

type FileDelta struct {
    Path        string
    Kind        FileChangeKind
    BeforeHash  string
    AfterHash   string
    UnifiedDiff string
}
```

`RunRuntime` 持有或组合 `RunDiffProjector`。Projector 以 canonical absolute path 为键，可覆盖 WorkspaceRoots、平台临时写路径和 Additional Writable Roots；UI 再按 CWD 或最近 Workspace Root 投影友好相对路径。ApplyPatchHandler 在执行后返回实际提交的 exact operation delta，Projector 只消费这些 delta，不根据模型意图或工具参数猜测变化。

聚合语义如下：

- add/update/delete/move 从 Run 初始状态折叠到当前状态，连续修改同一文件只展示最终统一 Diff；
- add 后 delete 可以抵消，move 后 update 保留正确来源和最终目标；
- 失败或 partial Patch 只记录已经提交的 operation，提交前失败或取消不产生变化；
- 同一路径在多个 Root 中不会冲突，因为事实键始终是规范绝对路径；
- Projector 发布 `RunDiffUpdated`，TUI、App Server/API、Audit 和 Run 最终摘要可以消费同一内存投影；事件只更新状态或 Diff Surface，不在 transcript 中重复打印 `apply_patch` 已经表达的文件变化。

普通 `execute_command`、`write_stdin`、MCP、Skill script 或外部进程不接入 `RunDiffProjector`，也不会仅因“可能修改文件”而使 Projector 失效。Amadeus 不解析 Shell 字符串、不猜测修改目标，也不把未观测到的副作用伪装成精确 Tool 归因。只有 `apply_patch` 已经提交变化但返回的 delta 缺失、损坏或明确不精确时，Projector 才在内部失效并清空精确 Patch Diff；TUI 不把该内部状态作为 transcript 警告展示。

实际工作区状态属于独立 `WorkspaceDiff` 能力。后续 `/diff` 按需参考 Codex，通过受控 Git 命令读取 tracked diff 与 untracked files，展示 Patch、Shell、MCP、Skill、用户手动编辑等所有来源造成的当前工作区变化；该结果只描述 Workspace 当前状态，不反向归因到某个 Tool Call。非 Git 项目明确提示不可用，不回退到全目录 before/after Snapshot。若未来 Sandbox 或文件系统监控能提供可靠 operation delta，只能作为新的精确生产者接入，不改变 Patch Diff 与 Workspace Diff 的语义边界。

原始 `tool_call`/`tool_result` RolloutItem 与其中的 ToolOutput 是 canonical rollout 事实；`RunDiffProjector` 不建立独立 SQLite 表。活动 Run 中它是可丢弃内存状态；Resume 或历史展示需要 Diff 时，可以从持久化 ApplyPatchOutput 重放 exact delta，无法重放时只展示已有工具变化事实，不伪造完整状态。

目标架构不生成反向 Patch，也不自动恢复磁盘。用户需要撤销时优先使用 Git；也可以要求 Agent 根据当前工作区和已有 Diff 生成新的 `apply_patch`，该 Patch 仍走正常 PathResolver、Approval、执行和 Audit 主链。

Codex 的 `ThreadRollback` 只回退对话上下文，不恢复本地文件；其 `TurnDiffTracker` 位于 Core，并把 Diff 事件投影给 App Server 与 TUI。Amadeus 学习这一边界：Run Diff 是可观察状态，不是恢复能力。Amadeus 当前不实现 ThreadRollback；未来如有明确需求，也只能通过 append-only rollback marker 改变 Context Projection，不删除 canonical rollout、不修改磁盘。

### 18.2 项目验证与 LSP 边界

Amadeus 不内置或要求用户配置 Language Server。Reactor 在修改后通过项目原生 formatter、build、lint、typecheck 和 test 命令形成验证闭环，并将退出码、诊断文本和测试结果保存在结构化 ToolOutput/ToolCallOutcome 中回灌模型。验证入口的发现优先级为有效 `AGENTS.md` → 项目脚本/Makefile/CI 配置 → 语言与构建系统常见约定；不得假定项目使用 Go，也不得在核心 Runtime 中硬编码单一语言命令。

核心配置、Runtime 和写后 Hook 不承载 LSP Client。M7 已删除 `internal/lsp`、`lsp.*` 配置、CLI explain/validation 与生产接线；原写后单文件诊断只保留在历史记录中。未来只有在真实场景证明编译、测试和类型检查无法提供足够及时的局部反馈时，才以 MCP、插件或 Extension Tool 形式重新立项。

### 18.3 Web Search 与 Web Fetch

`web_search` 与 `web_fetch` 是两个独立能力。Search 面向结构化搜索 Provider API，负责查询、结果归一化、超时、错误分类和来源元数据；Fetch 面向任意外部 URL，负责 SSRF、DNS/Redirect 安全、正文提取和内容上限。二者可以共享底层 HTTP、安全和可观测性组件，但不能复用同一个业务 Fetcher 或混用配置。

目标目录收敛为：

```text
internal/websearch/
├── request.go
├── result.go
├── provider.go
├── registry.go
├── service.go
├── errors.go
└── providers/
    ├── duckduckgo/
    ├── tavily/
    ├── searxng/
    └── brave/

internal/webfetch/
├── fetcher.go
├── policy.go
├── transport.go
└── extractor.go
```

Search Provider Domain 保持最小且与厂商无关：

```go
type SearchRequest struct {
    Query          string
    Limit          int
    AllowedDomains []string
    RecencyDays    int
}

type SearchResult struct {
    Title       string
    URL         string
    Snippet     string
    Content     string
    Source      string
    PublishedAt *time.Time
}

type SearchResponse struct {
    Provider string
    Results  []SearchResult
    Partial  bool
    Duration time.Duration
}

type SearchProvider interface {
    Name() string
    Search(context.Context, SearchRequest) (SearchResponse, error)
}
```

`SearchService` 负责选择已配置 Provider、创建总超时、执行一次有界重试、归一化与去重 URL、过滤无效结果、限制 snippet/content、记录耗时并返回统一错误；Provider Adapter 只处理各厂商的认证、请求/响应格式和状态码。错误至少区分 `configuration`、`authentication`、`rate_limited`、`timeout`、`network_unreachable`、`provider_unavailable` 与 `invalid_response`，不得只向 Reactor 返回无上下文的 `context deadline exceeded`。

首版 Provider 集合固定为：

| Provider | 定位 | 首版约束 |
|---|---|---|
| DuckDuckGo | 无 API key 的 best-effort 搜索 | HTML 搜索优先，Instant Answer API 只作 fallback；不承诺网络稳定性，不再作为静默唯一实现 |
| Tavily | 面向 Agent 的托管搜索主选项 | 需要独立 API key；返回结构化 snippet/content，明确处理认证、限流与服务错误 |
| SearXNG | 用户自建或指定的元搜索后端 | 必须显式配置 Base URL；要求实例启用 JSON 格式，不假定公网实例可靠 |
| Brave | 独立商业搜索 API | 需要独立 API key；支持结构化网页结果，Provider Adapter 隔离其 header、分页和限流语义 |

当前 `api.duckduckgo.com` Instant Answer 实现属于待替换历史代码：它不是通用 SERP，常规技术、新闻和长尾查询可能返回空结果；固定 30 秒等待也会把网络不可达放大为长时间卡顿。DuckDuckGo 新实现必须参考 WeKnora 的 HTML-first/API-fallback 思路，但不复制其 Tenant、数据库、Redis、临时知识库或 RAG 压缩。

网络超时采用分层预算而不是单一无限等待：连接/DNS、TLS、Response Header 和 Search Overall 分别受限，首版 Search Overall 默认 15 秒；仅对瞬时网络错误和 HTTP 5xx 最多重试一次，认证、参数错误和限流遵守 Provider 提示而不盲目重试。显式 Proxy 与目标 URL 必须分开建模：SSRF Guard 验证用户目标和 Redirect，用户明确配置的代理连接不能被误判为目标私网访问。

Tool 输出向模型提供有界、易读的编号结果，并在 Metadata 保留结构化值：

```text
Web search results for: <query>
Provider: <provider>

[1] <title>
URL: <url>
Published: <optional date>
Snippet: <bounded snippet>
```

首版工作流保持 `web_search → 选择 URL → web_fetch`，不立即复制 Codex 的完整 `open/click/find/ref_id` 浏览协议；后续可增加一次多 query、domain/recency filter 和 Run 内稳定引用。OpenAI Responses Hosted Web Search 只能作为 capability 驱动的未来 Adapter：必须由 Provider 明确声明支持，不能根据 `dialect=openai` 猜测，更不能让 DeepSeek、Qwen、GLM 等模型依赖 OpenAI 私有的 `alpha/search` 端点。

未启用、配置不完整或没有可用 Provider 时不注册 `web_search` Tool。后续提供 `amadeus web check`，在不启动 Agent Run 的情况下验证配置、认证、连接、响应格式、耗时和脱敏错误。搜索请求彼此只读，可声明 Shared；是否继续要求网络审批仍由统一 Approval 策略决定，不在 Provider 内绕过。

### 18.4 Browser 与图片

- Browser：连接、会话、敏感页面策略和审计分离。
- 图片：本地图片先校验类型、尺寸与上限，再压缩/缩放；历史轮次只保留文本元信息。

这些能力通过 Tool、项目命令或有明确边界的扩展接口接入 Runtime，不能反向依赖 CLI。验证失败只表示需要继续观察和修复，不自动回滚已经成功的文件写入；若需要纠正，Agent 必须根据当前工作区、Diff、测试结果和用户意图生成新的正常 Patch。

## 19. 事件与渲染

### 19.0 Typed EventHub

Amadeus 使用强类型事件，不采用 `Event{Type, Data interface{}}`。事件 Record 统一携带明确关联元数据：

```go
type Metadata struct {
    SessionID string
    RunID     string
    TaskID    string // 仅未来 DelegatedTask/SubAgent
    Iteration int    // 仅 Reactor 内部事件
    LLMCallID string // 仅 Provider Call 事件
    Sequence  int64
    Timestamp time.Time
}
```

`TurnID` 不进入目标事件协议。一次用户执行使用 RunID，一次 Provider 请求使用 LLMCallID，未来 SubAgent DelegatedTask 使用可选 TaskID，Reactor 内部循环使用可选 Iteration 序号。任何事件不得复用同一字段表达多个生命周期。

核心事件分为：

- Run：`RunStarted`、`RunStatusChanged`、`RunCompleted`、`RunInterrupted`、`RunFailed`；
- Reactor：`IterationStarted`、`IterationCompleted`、`TextDelta`、`ReasoningDelta`；
- Provider：`LLMCallStarted`、`UsageUpdated`、`LLMCallCompleted`、`LLMCallFailed`；
- Tool：`ToolCallStarted`、`ToolCallCompleted`；
- Workspace：`RunDiffUpdated`、`RunDiffInvalidated`；
- Plan：`PlanUpdated`；
- Safety：`ApprovalRequested`、`ApprovalResolved`、`DiagnosticPublished`、`ErrorOccurred`。

Renderer、TUI、HTTP/SSE、Audit 和 Trace 订阅同一事件流。Channel 只作为 Subscriber Adapter；核心 Reactor、ToolRouter/Handler 和 Approval 不直接以 channel 互相调用。Approval、Tool start/completed 和 Run terminal 等关键事件同步发布并传播错误，不能 fire-and-forget；TextDelta/ReasoningDelta 可以批量刷新，但不得改变最终顺序。

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
- `fullscreenModel`：维护 Bubbles textarea、待提交 transcript entry、提交游标、当前 assistant draft、usage、phase、输入历史、Slash Popup 与 SelectionOverlay 投影；它只保存 UI 投影，不成为 Agent 或 Session 的事实源。完成 entry 使用 `tea.Println` 写入终端历史，活动区不重复渲染已经提交的 entry。命令动作位于 `application_commands.go`，统一选择交互位于 `application_selection.go`，Catalog、Popup 和 Overlay 状态各自独立文件。
- `TerminalCapabilities`：检测 TTY、颜色、终端宽度和 dumb terminal；`--plain`、非 TTY 或 `TERM=dumb` 使用逐行模式。`AMADEUS_PLAIN` 已删除；`TERM` 为空只关闭颜色，不把真实 TTY 降级为 Plain。
- `SlashCommandCatalog`：集中保存命令名称、Codex 对齐的英文说明、展示顺序、参数规则、运行中可用性和破坏性标记；Rich TUI 与 Plain fallback 共用同一目录，不再分别维护字符串和 switch，也不注册 `/help`、`/sessions`、`/tools`。
- `SlashCommandPopup`：输入第一行以 `/` 开头且光标仍位于命令 token 时自动出现；显示命令与说明，完全匹配优先、前缀匹配其次，并保持 Catalog 展示顺序。`↑/↓` 循环选择、Enter 执行、Tab 补全、Esc 关闭但保留草稿；Popup 激活时方向键不得触发输入历史。
- `SelectionOverlay`：为 `/resume`、`/skills`、`/rename`、`/delete` 和 Approval 提供统一的无边框选择/输入交互，避免每个命令维护独立键盘状态机。
- `ListVisual`：Slash Popup 与所有 SelectionOverlay 共用同一个无边框列表 Renderer，统一标题、hint、名称列宽、muted description、黄色 selected cursor/name、disabled reason、Search/Input 行和最多八行可见窗口；Read/List/Search 等 Transcript 内容语义继续使用青色，不与交互选择色混用。
- `FullscreenApproval`：复用 `SelectionOverlay` 显示 tool/risk/reason 与 Codex 风格选项，使用 `↑/↓` 选择、Enter 确认、Esc 拒绝本次调用；Ctrl+C 取消当前 Run。

第一版交互语义：

| 操作 | 行为 |
|---|---|
| Enter | Slash Popup 激活时执行当前选中命令；否则空闲时提交当前输入，Run 执行中将下一条普通输入加入串行队列；空行不创建 Run |
| Up/Down | Slash Popup 或 SelectionOverlay 激活时循环选择；否则浏览当前 Terminal Session 的输入历史 |
| Tab | 补全 Slash Popup 当前选中命令；路径补全后置 |
| Shift+Tab | 空闲时在 execute 与 Plan CollaborationMode 间切换；Run 执行中拒绝改变当前能力边界 |
| Ctrl+C（Run 执行中） | 取消当前 Run，保留已追加的 canonical Tool Call/Result 与中断 marker，回到输入状态 |
| Ctrl+C（空闲） | 清空当前输入，不退出进程 |
| Ctrl+D（空闲） | 退出交互循环 |
| Esc | 关闭 Slash Popup 或当前选择器并保留必要草稿；不撤销已执行副作用 |
| `/exit` | 在空闲状态退出交互循环；不删除当前 Session 或 canonical rollout |
| `/resume` | 打开当前 Project 的 Session 选择器；Esc 返回原会话 |
| `/clear` | 清空终端与瞬态 UI，结束当前前台 Session 绑定并进入新的 Draft Session；旧 Session 保持可恢复，不删除 canonical rollout |
| 鼠标滚轮 | 由终端滚动原生 scrollback，不向 Bubble Tea 注册 mouse tracking |
| 鼠标拖拽 | 使用终端原生文本选择，不要求 Shift 修饰键 |

`/resume` 选择器使用 main-screen 内联无边框列表，不使用 RoundedBorder 或 modal 方框。标题、当前选中项和 `›` 指示符使用与状态栏 amber 一致的自适应黄色（light `#A16207` / dark `#FDE68A`），未选中项保持普通前景色，辅助按键提示使用 muted gray。用户通过 `↑/↓` 或 `j/k` 移动，Enter 恢复，Esc 取消并返回当前对话。

#### 19.1.1 Codex 对齐的 Slash Command

Amadeus 首版只实现 Codex 中与当前能力相符的命令子集。命令名称、默认英文说明、列表布局、键盘提示、确认文案、空状态和成功/失败反馈优先直接复用 Codex TUI 的用户可见文本，只做 `Codex → Amadeus`、`thread → session` 以及实际能力差异所必需的替换，避免另行设计一套近义文案。未来同步 Codex 文本时必须通过集中 Catalog/文案常量更新，不允许散落在 TUI handler 中。

| Command | Codex-aligned description | Amadeus 语义 |
|---|---|---|
| `/resume` | `resume a saved chat` | 打开当前 Project 的 Session 选择器；支持 Esc 取消并返回原 Session/Draft |
| `/skills` | `use skills to improve how Amadeus performs specific tasks` | 打开 `List skills` 与 `Enable/Disable Skills` 二级菜单；状态写入权威配置而非只存在于 TUI |
| `/rename` | `rename the current session` | 输入并持久化当前 Session Title；Draft Session 尚未落库时保存待创建标题 |
| `/delete` | `permanently delete this session and exit` | 二次确认后事务删除当前 Session 及从属数据并退出；Draft Session 不访问数据库 |
| `/compact` | `summarize conversation to prevent hitting the context limit` | 主动执行语义化上下文压缩并追加 `context_compaction`；不得覆盖或删除原始 rollout |
| `/plan` | `switch to Plan mode` | 不接收任务文本；把 Composer CollaborationMode 切换为只规划不实施的 `plan` |
| `/copy` | `copy last response as markdown` | 复制最后一条完整 assistant 原始 Markdown，不复制 ANSI/Glamour 渲染结果 |
| `/status` | `show current session configuration and token usage` | 输出当前 Session、model、mode、usage、cwd、permission、Skill/MCP 摘要 |
| `/mcp` | `list configured MCP tools; use /mcp verbose for details` | 默认显示配置、连接和工具摘要；`/mcp verbose` 懒加载完整详情 |
| `/clear` | `clear the terminal and start a new chat` | 先清空终端和 transcript/overlay/draft 等瞬态 UI，再进入新的 Draft Session；旧 Session 仍可恢复 |

Slash Popup 使用 Codex 的无边框两列形式：命令名为普通前景/统一 accent，匹配字符加粗或 accent，说明使用 muted gray，选中行使用共享自适应 accent，不使用荧光绿或每个命令独立配色。列表最多显示固定数量的可见行并维护滚动位置；输入 `/` 展示全部命令，输入 `/re` 等 token 后实时过滤。因为 Popup 已经承担可发现性和帮助功能，Rich TUI 删除 `/help`；Plain fallback 输入单独的 `/` 时打印同一 Catalog，未知命令也基于 Catalog 返回提示。

命令分发必须产生明确的 UI/Application Action，而不是让 Popup 直接操作 Store：本地动作如 `/copy`、状态投影如 `/status`、异步查询如 `/mcp`、Session 动作如 `/resume`/`/rename`/`/delete`/`/clear` 和模式动作如 `/plan` 分别交给对应 Controller/Runtime。Run 执行期间只允许不会切换 Session、删除历史或改变当前 Run 能力的命令；不可用项从 Popup 隐藏或显示 disabled reason，并由 Dispatcher 再次校验，不能只依赖 UI 过滤。

`/clear` 与 Codex 保持同一产品语义，但遵守 Amadeus 的惰性 Session 创建：已有持久 Session 时，先解除当前前台 SessionRuntime 绑定、清空终端及 UI 投影，再建立未落库的新 Draft Session；原 Session、Run 和 RolloutItem 完整保留，可从 `/resume` 恢复。当前本来就是未提交任务的 Draft 时，只重置终端/UI 并生成新的内存 Draft，不创建空 Session。`/clear` 不是简单 ANSI 清屏，也不等于 `/delete`。

Bubble Tea 负责跨平台 raw mode、UTF-8 rune 输入、paste、resize 和退出恢复；Bubbles textarea 直接维护 Unicode 文本，因此中文输入不再被旧的单字节 reader 拆坏。默认不启用 Bubble Tea mouse tracking，也不启用 alternate screen，滚轮和拖拽选择均由终端原生处理。流式 assistant 草稿只显示适合当前终端高度的尾部，完成后再以完整 Markdown 写入 scrollback，避免长回答挤掉输入框。输入边界仍过滤残留的 terminal control response，但必须按 Unicode 控制码点识别 C1 响应，不能按原始 `0x9d/0x9b` 字节匹配，否则会误吞 UTF-8 汉字。真实程序级回归覆盖中文 rune 退格、运行中输入排队、控制片段过滤、Ctrl+C 清空、Ctrl+D/EOF 退出、主屏幕模式和启动历史提交。逐行 Plain fallback 仍共享同一个 buffered reader，避免工具审批读取丢失。

交互 TUI 必须展示但不泄露内部 reasoning：

- 计划块显示 `update_plan` 的 Explanation 与 PlanItem `pending/in_progress/completed` 状态；后续更新替换当前清单并保留必要历史 transcript。
- 工具块显示工具名、开始/完成状态、耗时、截断标记和安全摘要，不展示完整密钥、Authorization、超长参数或隐藏 reasoning。
- assistant 正文继续流式输出；状态事件到达时先结束当前文本行，避免正文与状态行交错。
- 底部状态栏显示 `idle/planning/executing/awaiting_approval/cancelling/error`、当前 RunMode、iteration/tool 计数、最近 usage、当前 CWD、WorkspaceRoots 和 Additional Writable Roots 摘要。
- Approval、取消、错误和 Run 终态都使用事件驱动，不允许工具或 Engine 直接写 stdout/stderr。

当前版本已经实现 Amadeus Logo 与 Braille `>_` 品牌头部、只含 product/version、model、directory 的启动面板、主屏幕 Rich Inline 渲染、终端原生 scrollback 与文本选择、Markdown assistant 文本、多行 Unicode textarea、运行中任务排队、Slash Popup 和统一 SelectionOverlay，但仍不实现文件树 Pane、可拖拽布局、Diff 折叠器或持久化输入历史。默认 `amadeus` 只建立内存 Draft，不在启动面板展示伪 Session；第一条任务才创建正式 Session。只有 `--continue`、`--resume` 或交互 `/resume` 恢复旧 Session。输入历史只保留进程内；Session canonical history 由 SessionRuntime/Session Store 管理。FullscreenApplication、InlineRenderer 与 PlainRenderer 消费同一个 Typed EventHub，终端 UI 只能改变展示，不改变 Reactor、RunRuntime、Approval 或 Session 语义。

### 19.2 目标 Rich Inline 产品形态

首个发布前将默认 TUI 收敛为 `docs/tui.md` 所示的 Codex 风格主屏交互，但继续遵守 19.1 的终端原生 scrollback 约束，不退回 alternate screen、全屏 transcript viewport 或 mouse tracking。目标布局为：

```text
大型 Amadeus 终端 Logo + Braille `>_`

╭───────────────────────────────────────────────────╮
│ Amadeus (dev)                                     │
│ model:     GPT-TOP                                │
│ directory: /project/root                          │
╰───────────────────────────────────────────────────╯

› 用户输入

• assistant 过程说明或最终回答

• Explored
  └ Read runtime.go
    Search RunRuntime|update_plan in internal/agent

• Ran command
  │ go test ./internal/agent/runtime ./internal/agent/react
  └ ok github.com/.../internal/agent/runtime

• Working (40s • esc to interrupt)

─ Worked for 2m 05s ───────────────────────────────

> 用户输入框
GPT-TOP · /project/root · main · Context 47% used · 128K window
```

启动品牌头部继续使用现有 `Amadeus Logo + >_` Braille 点阵设计，不改为 Codex 字标，也不删除 `>_`。Logo 下方启动信息面板则对齐 Codex 的克制展示，默认只显示产品/version、`model` 与 `directory`；Provider、Session ID、Branch、CollaborationMode、Context Usage 和 Permission 摘要进入底部状态栏或 `/status`，不在启动面板重复堆叠。面板边距、右边界和响应式断点继续使用 Amadeus 既有设计。

这一形态只改变事件到终端的投影，不改变 Reactor、Plan、ToolRouter/Handler、Session 或 Approval 的业务语义。

#### 19.2.1 Logo 与启动面板

`docs/Amadeus_logo.webp` 是品牌源文件，但首版不在运行时直接使用 Kitty Graphics、iTerm Inline Image 或 Sixel 协议。终端图片协议兼容性、tmux/SSH 透传和 scrollback 行为差异过大，不能成为默认 UI 前置条件。

构建期使用一次性生成器对源图裁边、阈值化并转换为 Unicode Braille 黑白点阵，把生成结果作为静态 Go 常量提交；Logo 内的 `>_` 同样使用 2×4 Braille 光栅生成，不能直接打印 `>_` 两个字符，也不能混用 `█` 块字符形成另一套笔触。Amadeus 主体与 `>_` 必须保持相同的点阵密度、边缘风格和视觉重量，并保存为两个独立矩形画布：先使用终端显示宽度计算主体最大列，再把提示符统一放到 `brandWidth + gap` 的固定起始列；宽版 gap 为 8 列，紧凑版 gap 为 4 列，禁止把提示符逐行追加到不同长度的主体行尾。`>_` 必须位于 Amadeus 图形右侧并保留明显净间距，不能与主体轮廓重叠；宽版断点由合成后 Logo 的真实最大显示宽度自动计算，紧凑版继续满足 40 列降级。至少提供：

- `wideLogo`：终端宽度能够容纳完整合成画布时显示完整品牌字符画；
- `compactLogo`：终端无法容纳宽版时使用源图生成的紧凑点阵与紧凑 Braille `>_`，不允许文字占位；
- 品牌区域使用终端默认前景色，不设置彩色 foreground 或 background；无颜色模式保持同一轮廓。

启动面板对齐 Codex，只显示产品/version、model 和 directory；现有 Amadeus Logo 与 Braille `>_` 保持不变。Provider、Session、Git branch、WorkspaceRoots 与 Context/Permission 摘要不在启动面板重复展示，而进入底部状态栏或 `/status`。Git branch 仍由有界 workspace metadata resolver 读取，不在 `View()` 中执行 Git 命令，不是 Agent Tool Call，也不进入 canonical rollout。

启动信息栏在宽终端下必须保留 4 列右侧 margin，Lip Gloss 的内容宽度需要扣除 border 盒模型，不能让右边界贴住终端最右列。窄于 60 列时继续使用无边框降级布局。

#### 19.2.2 TUI Visual Runtime 与 Transcript Cell

M9V 已完成 Codex 视觉运行时向 Amadeus 的行为级移植。旧 `fullscreenEntry{kind, content}`、统一 `strings.Join(..., "\n\n")`、按 kind 手工补换行、frame 阶梯颜色和 `IterationCompleted` 后批量提交 Activity 的兼容链已经从生产代码删除；M9 的 SlashCommandCatalog、Popup、SelectionOverlay 和 Session Command 继续作为独立交互层保留。

当前 Visual Runtime 保留 Go、Bubble Tea、Lip Gloss 和终端主屏 scrollback，不复制 Rust/Ratatui 类型本身；它移植 Codex 的数据模型、状态转换、终端感知颜色、动画算法和快照 Contract。核心抽象为只读 UI Projection：

```go
type TranscriptCell interface {
    Render(TranscriptRenderContext) string
    RawLines() []string
}

type ActiveCell interface {
    TranscriptCell
    Apply(event.Event) bool
    Complete() TranscriptCell
    IsComplete() bool
}

type TranscriptState struct {
    ActiveCell                  ActiveCell
    NeedsFinalMessageSeparator bool
    HadWorkActivity            bool
    LastAssistantMarkdown      string
}
```

当前实现包含 User、Assistant、Exec、Explore、WebSearch、Plan、Notice、Error、Diagnostic 与 Final Message Separator Cell；`StyledLine/StyledSpan` 在 Tool Cell 内表达语义化样式，统一 Renderer 才把它投影为终端字符串。Cell 只拥有展示投影，不写 SQLite、不替代 RolloutItem，也不成为 ToolOutput/ToolCallOutcome 或 Run 状态的第二事实源。`fullscreenModel` 只负责 Composer、Bottom Pane、活动 Cell、待提交 Cell 和 Bubble Tea 消息路由，不再承担每种内容的字符串拼装规则。

正文不携带为了排版伪造的前置或尾部 `\n`。每个 Cell 返回精确的逻辑行；Transcript Layout 统一决定 Cell 之间的一行空白、活动区与 Composer 的间距以及提交到 `tea.Println` 的批次边界。Assistant、Tool、Separator 和下一条 User Message 不再依赖 `previousKind/lastKind` 特判。

#### 19.2.3 Tool History Cell 与安全 Action Summary

Tool History Cell 只消费 ToolRouter/Handler 已验证并脱敏的结构化事件，不查 Tool Registry、不读取文件、不执行命令，也不解析 Provider reasoning。为了展示路径、搜索式和命令，`ToolCallStarted` 使用安全展示字段，而不是把完整 raw arguments 交给 TUI：

```go
type ToolCallStarted struct {
    CallID        string
    ToolName      string
    Presentation  ToolPresentationKind
    ActionSummary string
    Detail        string
}
```

摘要在 ToolRouter 完成 strict parse、保守 repair、Schema validation 并得到 normalized invocation 后生成；生成器只读取白名单字段，例如 `path/pattern/query/command/name`，执行统一长度上限、换行规范化和凭证脱敏。MCP/网络参数默认不展开，未知字段不进入事件。TUI 不解析 raw JSON，也不负责安全判断。`ToolCallCompleted` 承载 ToolCallOutcome 的 status/duration、ToolOutput 的 partial 和有界结果摘要；完整 ToolOutput 进入 `tool_result` RolloutItem，TUI Cell 只是 UI Projection。

工具展示采用以下稳定语义：

| Tool/Side Effect | 默认展示 |
|---|---|
| `read_file` | `Read <path>` |
| `list_dir` | `List <path>` |
| `glob_files` | `Find <pattern>` |
| `grep_code` | `Search <pattern>` |
| `read_skill`（正文） | `Read skill <name>` |
| `read_skill`（Reference） | `Read skill reference <path>` |
| `apply_patch` | `Applied patch` |
| `execute_command` | `Ran command` |
| `write_stdin` | `Continued process` |
| `view_image` | `Viewed <path>` |
| `web_search` | `Searched web` |
| `web_fetch` | `Fetched <host>` |
| MCP | `Called MCP tool` |

未知 Tool 按 Handler 提供的 `ToolPresentationKind` 安全降级为 `Explored`、`Ran tool` 或 `Called network tool`，不直接打印任意参数。Presentation 属于 UI 元数据，不进入 Provider ToolSpec，也不参与 Permission、Approval 或并行判断。

颜色遵循 Codex 的克制语义而不是固定 Dashboard Palette：普通正文使用终端默认前景色，辅助信息和树形前缀使用 dim，标题使用 bold，`Read/List/Search` 使用 terminal-aware cyan，成功与失败命令的 `•` 分别使用 green/red，活动中的 Tool 使用统一 Motion indicator。命令正文允许 Bash 语法高亮；路径、pattern、结果正文保持默认前景色。不得给 Assistant 正文、Tool Result 或普通 Notice 添加固定灰色背景、砖红色强调或大面积彩色边框。

`ExecCell` 表示单个命令或进程交互，显示 `Running/Ran/You ran`、命令、最多两行命令续行、最多五行输出、dim omission marker、exit status 和 duration。`ExploreCell` 合并连续只读浏览行为，去重连续 Read，并以 `Exploring/Explored` 加 `Read/List/Search` 子项展示。`WebSearchCell` 单独表达 `Searching/Searched the web`。Tool Started 时创建或更新 `ActiveCell`，Tool Delta/Completed 原位更新活动区；开始 Assistant Stream、切换为不兼容 Cell 或 Run terminal 时才把完成 Cell 提交到 scrollback。并行 Tool Call 继续按 CallID 关联，稳定展示顺序来自 Tool Call 原始 sequence，而不是完成先后。

#### 19.2.4 Transcript 边界与 Separator 状态

TUI 不再把 Reactor Iteration 当作可见排版边界。`IterationStarted/IterationCompleted` 可以继续作为诊断事件存在，但 Visual Runtime 不能在每次 `IterationCompleted` 后无条件追加横线，也不能等待整个 Iteration 完成才首次显示 Tool。

Separator 使用 Transcript 状态驱动：

```text
Tool/Exec/MCP/Patch 产生可见工作
  → HadWorkActivity = true

ActiveCell 提交或其他非流式 History Cell 插入
  → NeedsFinalMessageSeparator = true

下一段 Assistant Stream 或 Run terminal
  → 根据 HadWorkActivity/NeedsFinalMessageSeparator 插入 FinalMessageSeparatorCell
  → 清理本 Turn 的 separator 状态
```

该横线表达 Assistant、Tool History 与 Turn terminal 的内容边界，而不是“第 N 次 ReAct 循环”。简单问候或没有实际工作活动的回答不打印空横线；取消和失败保留明确状态 Cell，不伪装为成功完成分隔线。

当前 Codex 只在 elapsed 大于 60 秒时把 `Worked for <duration>` 写入完成分隔线；短 Run 显示低对比纯横线，存在 Runtime Metrics 时附加 metrics。Amadeus 首版对齐这一当前行为：duration 使用 `1m 01s`、`1h 01m 01s` 的零填充格式，分隔线使用终端显示宽度补齐，并整体采用 dim/terminal-aware separator color。不得继续固定输出 `─Worked for 1s─`。

#### 19.2.5 Terminal Palette、Motion 与运行中输入

Visual Runtime 新增集中式 `TerminalPalette` 和 `Motion` 模块。所有动画调用方只能使用 Motion API，不能在各 Widget 内自行累加 frame 或直接选择 shimmer 色阶：

```go
type MotionMode string

const (
    MotionAnimated MotionMode = "animated"
    MotionReduced  MotionMode = "reduced"
)

type TerminalPalette struct {
    ColorLevel ColorLevel
    Foreground RGB
    Background RGB
    IsLight    bool
}
```

Palette 检测 True Color、ANSI256、ANSI16/Unknown 与 No Color，并在可用时读取或推导终端默认前景/背景。用户消息低对比背景、separator、accent 和 shimmer 都从 Palette 派生；不可检测时退化为默认前景、dim、bold、cyan、green、red，不维护一组与终端主题无关的固定 pastel RGB。

底部活动区增加 `tea.Tick` 驱动的 Working 行，显示 Run elapsed time 和取消提示：

```text
• Working (40s • esc to interrupt)
```

动画按真实单调时钟计算位置并以约 32ms 请求重绘，不能使用 `frame++` 决定绝对位置。`Working` 的一个完整 sweep 为 2 秒，文本左右各保留 10 字符隐藏 padding，光带半宽为 5；每个字符使用余弦曲线计算强度，再在终端默认 foreground/background 之间连续混色。True Color 下 `•` 复用同一 shimmer；非 True Color 下 Working 退化为 dim/normal/bold，活动点每 600ms 在普通 `•` 与 dim `◦` 之间切换；Reduced Motion 使用静态普通文本和可选 dim `•`。elapsed 与 `esc to interrupt` 使用 dim，动画只能更新 Bottom Pane，不能向 scrollback 不断追加行。

Run 期间 textarea 继续可编辑；Enter 把下一任务加入现有串行队列。Esc 在输入为空且 Run 正在执行时取消当前 Run；输入非空、Approval 或 Session selector 状态下沿用各自局部 Esc 语义。Ctrl+C 继续作为明确取消键，不能因增加 Esc 而删除。

Working 行与上方的 assistant 草稿或最近提交内容之间固定保留一行空白；输入框与 Working/内容区域之间固定保留两行空白，状态栏仍紧贴输入框下一行。该留白属于默认 Rich Inline 视觉节奏，不能通过在 transcript 中提交空 entry 实现。

Run terminal 时 Working 行消失，是否提交完成分隔线由 19.2.4 的 Transcript 状态决定。Assistant think、Tool History、Final Separator 与下一条 User Message 的空白全部由 Cell Layout 管理；任何 Cell 内容都不得通过补 `\n`、检查 `previousKind` 或提交空 entry 调整视觉距离。

#### 19.2.6 Context Status 与 Workspace Metadata

状态栏使用 Provider `context_window`，不能再把 `agent.max_input_tokens` 当作模型窗口。两者语义保持：

- Provider `context_window`：单次 LLM Request 的模型上下文窗口；
- Agent `max_input_tokens/max_output_tokens`：整个 Run 的累计预算。

为了准确显示 `Context 47% used`，在 ContextManager 生成 RequestView 后发布 `ContextWindowUpdated`：

```go
type ContextWindowUpdated struct {
    EstimatedInputTokens int64
    ContextWindow        int64
    EffectiveInputLimit  int64
    ProjectedToolResults int
    DroppedMessagePairs  int
}
```

TUI 使用 `EstimatedInputTokens / ContextWindow` 展示最近一次 RequestView 百分比；Provider `UsageUpdated` 继续显示真实 token usage，但不替代 ContextView 估算。窗口使用 `128K/258K/1M` 等稳定格式。状态栏按宽度依次保留 model、project、branch、context percent 和 window，窄终端从低优先级字段开始隐藏。状态栏同样使用 TerminalPalette：主信息保持默认 foreground，低优先级字段和分隔符使用 dim，Plan/Warning 使用 yellow，健康/失败只在需要表达状态时使用 green/red，当前可交互 accent 使用 terminal-aware cyan。不得为 model、project、branch 分别维护 sky/blue/lavender 固定配色，也不使用彩虹式动画。

#### 19.2.7 Bounded Transcript Detail Viewer

原生 scrollback 中已经提交的行不可原位展开，因此 `Ctrl+T` 不修改旧输出，而是打开临时详情查看器：

```text
Rich Inline
  → Ctrl+T
临时 Bubbles viewport：查看完整 Tool/Command detail
  → Esc
返回 Rich Inline 输入状态
```

默认 transcript 只打印首尾摘要和 `… +N lines (ctrl+t to view transcript)`。UI 详情保存在 bounded in-memory store：单项和单 Run 都有字节上限，超限时保留 head/tail 并标记 truncated；canonical ToolOutput 按 Context/Audit 策略持久化，UI 专用展开状态不写入 SQLite。Viewer 可以暂时使用 viewport，但默认交互仍在 terminal main screen，退出 Viewer 后恢复原生 scrollback 和文本选择。

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

M9V 按“先建立 Palette/Motion，再替换 Transcript 数据模型，最后接入 Tool/Separator 与发布门禁”的顺序落地。它不重写 Reactor、Session、ToolRouter/Handler 或 Slash Command，只替换 Typed Event 到终端的视觉投影：

| 切片 | 主要代码落点 | 实现边界 |
|---|---|---|
| Terminal Palette | `internal/interface/tui/palette.go` | 检测 color level 与明暗背景，集中派生 default/dim/accent/success/error/separator/user background；不得散落固定 RGB |
| Motion | `internal/interface/tui/motion.go` | 使用可注入单调时钟实现 32ms、2s sweep、余弦柔光和 reduced-motion fallback；Widget 不直接维护 frame 色阶 |
| Transcript Cell | `internal/interface/tui/transcript.go` | 定义 Cell、ActiveCell、StyledLine、统一间距和 scrollback commit；删除 `kind/content` 与尾部换行特判 |
| Tool History | `internal/interface/tui/tool_cells.go`、`activity.go` | Exec/Explore/WebSearch Cell 按 CallID/sequence 原位更新 ActiveCell，完成后稳定提交；Read/List/Search 合并、命令/output 截断和状态颜色对齐 Codex |
| Action Summary | `internal/tool/presentation.go`、`internal/agent/event/*`、目标 `internal/tool/router` | 由 ToolRouter 在唯一参数校验后生成安全摘要；事件只传展示白名单，不把 raw arguments 下放给 UI |
| Separator Runtime | `internal/interface/tui/transcript.go`、`application.go` | 使用 HadWorkActivity/NeedsFinalMessageSeparator，不再把 Iteration 当作排版边界；短 Run 只显示 dim rule |
| Context 状态 | `internal/context/window.go`、`internal/agent/event/context.go`、TUI 状态投影 | 每次 RequestView 构建后发布估算；Provider Usage 与 Context 估算并存但语义分离 |
| Detail Viewer | `internal/interface/tui` 内 bounded detail store 与临时 viewport | 详情仅驻留有界内存；Viewer 关闭后回到 main-screen Rich Inline，不持久化到 SQLite |

实现和测试遵循以下顺序：

1. 先冻结 Codex 当前源码与快照所表达的 Visual Contract，并实现 TerminalPalette、Motion 和可注入 Clock；
2. 再引入 TranscriptCell/ActiveCell 与统一 Layout，迁移 User、Assistant、Notice、Error 和 Final Separator；
3. 然后迁移 Exec/Explore/WebSearch/Plan Tool History，移除按 Iteration 批量提交和固定 separator；
4. 最后接回 Slash Popup、SelectionOverlay、Approval、Detail Viewer、Status Bar 和现有 Amadeus Logo，执行宽度、Unicode、TTY/Plain、No Color、动画降级、Session、取消和 race 矩阵。

每个 Cell、Palette 和 Motion 逻辑优先使用纯函数、固定 Clock 和快照测试；终端级测试只验证 Bubble Tea 命令、控制序列、主屏 commit 和输入行为。Rust/Ratatui 代码不直接复制进 Go，但用户可见文本、颜色语义、动画参数、Cell 状态转换和快照布局应一一核对。任何为了视觉对齐而引入 alternate screen、mouse tracking、无界 transcript 缓存、raw Tool arguments 或新的 Agent 事实源都视为架构回归。

M9V 的冻结契约记录在 `docs/tui-visual-contract.md`。实现测试覆盖 True Color、ANSI256、ANSI16、No Color、Reduced Motion、固定 Clock shimmer、中文与窄终端、Exec 成败与截断、Explore 去重、WebSearch、Separator、Approval、Resume、Plain fallback 和 main-screen 控制序列；发布门禁以全仓测试、race、`make check` 与 `git diff --check` 为准。

## 20. 持久化

SQLite 固定放在：

```text
$AMADEUS_HOME/data/amadeus.db
```

数据库不放进目标项目，避免污染仓库和在多项目之间拆散用户历史。

### 20.1 SQLite 表

首个稳定版本收敛为四张核心业务表，加一张 schema 管理表：

1. `schema_migrations`
2. `projects`
3. `sessions`
4. `runs`
5. `rollout_items`

不建立 Turn、RunRuntime、Plan、PlanItem、Iteration、ApprovalGrant、LongTermMemory、独立 Message、独立 Summary 或独立 Previous Work 表。Tool Call 与 Tool Result 作为 RolloutItem 持久化，而不是拆成工具专用业务表。

### 20.2 `schema_migrations`

记录数据库 schema 版本与应用时间。迁移必须前向、事务化和可重复检测。Amadeus 尚处于首个稳定版本之前，当前 canonical rollout 收敛允许使用一次明确的破坏性开发迁移：删除旧 `conversation_sessions/conversation_messages/conversation_summaries` 与旧 `runs`，再创建 `sessions/runs/rollout_items`，不为未发布的历史开发数据长期维护转换兼容层。迁移必须通过 schema version 明确执行，失败时原子回滚；用户若需要保留开发期会话，应在升级前备份 `$AMADEUS_HOME/data/amadeus.db`。首个稳定版本发布后，新增 schema 变更必须重新采用保留已发布用户数据的前向迁移策略。

### 20.3 `projects`

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Project ID |
| `canonical_path` | unique, not null | 规范化绝对路径 |
| `display_name` | not null | 展示名称 |
| `created_at`/`updated_at` | not null | 生命周期时间 |
| `last_opened_at` | indexed, not null | 最近打开时间 |

### 20.4 `sessions`

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Session ID |
| `project_id` | FK, not null | 所属 Project |
| `title` | not null | 首条任务截断生成的标题 |
| `status` | not null | `active` 或 `archived` |
| `next_run_sequence` | not null | 下一个 Run 序号 |
| `next_item_sequence` | not null | 下一个 Session 全局 Rollout 序号 |
| `created_at`/`updated_at` | not null | 生命周期时间 |
| `last_active_at` | indexed, not null | 当前项目最近 Session 查询依据 |

`amadeus` 启动只得到内存 Draft Session；第一条真实任务到达时才创建 Session。Session 没有 completed 状态，因为历史对话可以再次恢复。

### 20.5 `runs`

一个真实用户输入对应一个 Run。RunMode 为 `execute` 或 `plan`；历史 `react/planned` 都迁移为 `execute`，因为旧 planned Run 已经实施过工作。

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Run ID |
| `session_id` | FK, not null | 所属 Session |
| `sequence` | unique per session | 用户输入/Run 顺序 |
| `status` | not null | `running/completed/interrupted/failed` |
| `stop_reason` | nullable | 用户取消、Provider、工具或资源错误 |
| `provider`/`model`/`api_mode`/`dialect` | nullable | 实际模型主链 |
| `run_mode` | not null | `execute` 或 `plan` |
| `usage_json` | nullable | Run 累计实际使用量 |
| `started_at` | not null | 开始时间 |
| `finished_at` | nullable | 终态时间 |

用户目标不在 `runs.objective` 重复保存，而是本 Run 的第一条 `user_message` RolloutItem。中断关联不使用 `context_from_run_id`；Session 全局序列已经完整表达历史先后关系。

### 20.6 `rollout_items`

`rollout_items` 是 Session 历史、恢复和 Context 构建的唯一持久化事实源：

| 字段 | 约束 | 语义 |
|---|---|---|
| `id` | PK text | Rollout Item ID |
| `session_id` | FK, not null | 所属 Session |
| `run_id` | FK, nullable | 产生该 Item 的 Run；Session 级迁移/系统 Item 可为空 |
| `sequence` | unique per session | Session 全局严格递增顺序 |
| `kind` | not null | `user_message/assistant_message/tool_call/tool_result/plan_update/context_snapshot/run_interrupted/run_failed/context_compaction` |
| `payload_json` | valid JSON, not null | Provider-neutral 结构化载荷 |
| `created_at` | not null | 创建时间 |

隐藏 Chain-of-Thought、模型流式 delta、TUI 动画与普通诊断 Event 不进入 canonical rollout。需要进入模型历史的 reasoning summary 可以作为 assistant item 的显式字段保存，但不得持久化 Provider 私有隐藏推理。

首版关键 payload：

```text
user_message       {content}
assistant_message  {response_id?, content}
tool_call          {response_id?, call_id, name, arguments}
tool_result        {call_id, name, status, output, metadata?}
plan_update        {explanation, items}
context_snapshot   {cwd, instruction_sources, instruction_hashes, model, capability_revision, workspace_metadata?}
run_interrupted    {reason, guidance, active_calls?}
run_failed         {reason, error_kind}
context_compaction {summary, replacement_history, covered_through_sequence, source_hash, provider, model}
```

### 20.7 Replacement History 的存储与恢复

Replacement History 不建立独立表，直接存放在 `rollout_items` 中：

```text
rollout_items.kind = 'context_compaction'
rollout_items.payload_json.replacement_history = [...]
```

原因是 compaction 本身也是 Session 时间线中真实发生的一项历史事件。原始 items 保持 append-only，不做 UPDATE 或 DELETE；新的 compaction 只声明“从本窗口开始，模型有效历史以这组 replacement items 为基线”。

恢复算法：

1. 按 Session sequence 从后向前找到最新有效 `context_compaction`；
2. 校验其 `covered_through_sequence`、`source_hash` 与 payload schema；
3. 以 `replacement_history` 初始化 SessionHistory；
4. 顺序追加该 compaction 之后的 model-visible tail items；
5. 规范化 Tool Call/Result 配对并生成 RequestView；
6. 没有 compaction 时从首条 RolloutItem 完整 replay。

因此同一个 Session 可以拥有多条历史 compaction，但恢复只需要最新有效基线和其后的 tail；较早 compaction 与原始历史仍保留用于审计、调试和未来重新压缩。

### 20.8 事务边界

- 首次 `BeginRun`：原子创建 Project、Session、Run，并追加第一条 `user_message`；
- 后续 `BeginRun`：原子分配 Run sequence 与 item sequence，创建 Run 并追加当前 `user_message`；
- 正常执行：assistant、tool、plan 与 context items 按完成顺序逐项追加；Tool Call 必须在产生副作用前持久化，ToolOutput 必须在 Handler 完成后立即投影为 `tool_result` 并追加；
- `FinishRun(completed)`：确认最终 assistant item 已持久化后更新 Run、Usage 与时间；
- `FinishRun(interrupted/failed)`：先补齐悬空 Tool Result、追加 marker 并 flush，再更新 Run 终态；
- Context Compaction：验证 source hash 后原子追加 `context_compaction`，不修改原始 RolloutItem；
- SessionRuntime 必须在任何模型调用或工具副作用前获得已持久化 RunContext，并已将当前 user item 纳入 SessionHistory。

SQLite 使用 WAL 模式和短事务。Amadeus 首版不复制 Codex 的 JSONL + SQLite 双存储；学习的是 append-only rollout、replacement history 和 replay 语义，避免创建第二持久化事实源。未来如需诊断，可提供只读 JSONL export。

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
- PathResolver、FileSystemPolicy、rule precedence、WorkspaceRoots、TemporaryRoots、ReadOnlyRoots、DeniedRoots、Run/Session Permission Store、ExecPolicy、IsolationMode、SessionApprovalStore、ToolRouter 并行调度、RunDiffProjector 和 Audit。
- parallel 批次、non-parallel 屏障、`max_parallel_tools` 限流、取消等待和原始 Tool Call 顺序回灌。
- RunDiffProjector 的 add/update/delete/move 聚合、同文件连续 Patch 折叠、partial Patch 和 invalidation。
- Tool schema、输出截断和并发顺序。
- Prompt 覆盖和 hash。
- Web Search Provider 请求转换、结果归一化、URL 去重、错误分类、超时和重试边界。
- Web Fetch SSRF、Proxy/目标地址分离、Redirect、正文提取和内容上限。

### 22.2 集成测试

- 使用 `httptest.Server` 模拟 Responses/Chat Completions 流。
- 使用多个临时目录验证 CWD、WorkspaceRoots、重复 `--add-dir`、绝对/`../` 读取、跨 Root Patch、规范目标键和符号链接逃逸。
- 使用平台 fixture 验证 Sandbox 内宿主可读、Workspace/Temporary/Run/Session Writable Roots 写入、ReadOnlyRoots、DeniedRoots、子进程继承和越界写 `sandbox_denied`；无 Sandbox 平台验证 Unsandboxed 提示、Permission Check 与 Command Approval 不可互相绕过。
- 验证多 Root canonical path、exact Patch Delta 重放、普通 Shell/MCP 不改变 Patch Diff、损坏或不精确 Patch Delta 触发内部 invalidation，以及 TUI/API 不把 Diff 状态重复写入 transcript。
- 使用假命令验证超时、取消和输出限制。
- 使用进程 fixture 验证 MCP stdio JSON-RPC。
- 使用 `httptest.Server` 分别模拟 DuckDuckGo HTML/API fallback、Tavily、SearXNG 与 Brave 的成功、空结果、认证、限流、5xx 和超时响应。
- 使用 SQLite 临时库验证 Session 恢复、Rollout replay、Replacement History 和中断 marker 生命周期。

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

1. **Foundation**：CLI、配置、日志、Provider Adapter 与流式协议；
2. **Canonical Persistence**：Session、Run、RolloutItem、Migration、Replay 与 Replacement History；
3. **Session/Run Runtime**：SessionRuntime、SessionHistory、RunContext、RunRuntime/RunState、RequestContext 与中断；
4. **Reactor 与 Context**：Think→Analyze→Act→Observe、ToolExecution、ContextManager 与 Typed EventHub；
5. **Plan-guided Execution**：`update_plan`、PlanState、TUI 进度投影与 Plan Mode；
6. **Coding Workflow**：ToolRouter/ToolHandler、多 Root FileSystemPolicy、Patch、Process/Sandbox、RunDiffProjector、Skill、MCP、Web 与项目验证；
7. **Productization**：Rich Inline Composer/Slash、TUI Visual Runtime、Resume、发布前兼容与迁移；
8. **Optional Enhancements**：只读 Multi-Agent、Browser、Runtime API 与后台任务。

历史 M0～M6 里程碑可能使用 Plan-and-Execute、ConversationSession、Task 或旧 Turn 术语；它们只记录当时实现，不构成当前目标架构。后续开发以本设计和 `docs/development-progress.md` 最新重构队列为准。

## 24. 兼容性策略

### 24.1 必须兼容

- 默认 Coding Agent、Plan Mode、Session resume、工具调用和后续 SubAgent 协作的用户可见核心语义；内部只保留一个 Reactor 执行循环。
- OpenAI-compatible base URL/model/key 切换。
- 工具调用和图片工具结果回灌。
- Prompt 用户级/项目级覆盖。
- Project.RootPath/CWD/WorkspaceRoots/`--add-dir`、FileSystemPolicy、Sandbox、HITL、审计与取消。
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
| 模型不调用或错误维护 `update_plan` | 进度展示不完整 | PlanState 只作为软状态，不驱动执行；最终事实以 canonical ToolExecution、Workspace 和最终回答为准 |
| 简单任务被过度规划 | 延迟和噪声增加 | 默认不强制计划，提示词只建议复杂任务使用 `update_plan` |
| Plan Mode 意外修改项目 | 破坏用户审核预期 | 使用 Tool Exposure 与 ToolRouter 可见性双层限制，禁止写 Handler 与修改型 Shell |
| SessionRuntime 变成新的万能 Engine | 职责再次集中 | 只拥有 SessionHistory、Rollout append/flush、活动 Run及 SessionPermissionStore、SessionApprovalStore、ExtensionRuntime 生命周期，不实现策略判断、Reactor、MCP/Skill 业务、Tool 或 Provider |
| RunRuntime 变成新的万能 Engine | 职责再次集中 | 限定为取消、RunState、资源所有权、Reactor 驱动和统一收尾；历史与持久化由 SessionRuntime 负责 |
| RunContext、RequestContext 与 RequestView 重复 | 上下文多状态源 | RunContext 只保存 Run 稳定事实，RequestContext 是一次采样环境快照，RequestView 是 Provider-neutral 请求投影 |
| 破坏性开发迁移导致旧会话丢失 | 首个稳定版前的开发期 Session 不可恢复 | 仅限当前未发布阶段；迁移事务化、升级前明确备份数据库、测试删除旧表并建立 canonical schema；稳定版发布后恢复数据保留型前向迁移 |
| Rollout 追加不完整 | 恢复后 Tool 协议或中断事实缺失 | Tool Call 前置持久化、Result 后置持久化、Finish 前 flush，并为悬空调用生成合成结果 |
| Tool 并发声明错误 | 有副作用调用重叠，造成文件、Process 或外部状态不一致 | 默认 Exclusive；只有内置只读 Tool 或具备可信只读元数据的动态 Tool 才能 Shared，并以并发/取消/E2E 守卫覆盖 |
| Provider 方言差异 | 请求被拒或工具调用解析失败 | Adapter 能力矩阵、有限 repair、Schema 校验与 Provider 集成测试 |
| Sandbox 尚未跨平台完成 | Unsandboxed Shell 可直接访问宿主机权限范围 | Linux 优先 SandboxRunner；其他平台明确 Unsandboxed，所有命令先做 Permission Check，再对未获 Session 精确批准的完整命令询问并明示 unsandboxed host execution，不把 requested permissions 或字符串扫描宣传为强隔离 |
| Shell 修改无法精确归因 | 把 Patch 投影误称为完整工作区状态 | RunDiffProjector 只聚合 exact Patch Delta 并忽略普通 Shell/MCP；独立 WorkspaceDiff 按需读取 Git tracked/untracked 状态但不伪造 Tool 归因 |
| Multi-Agent 过早复杂化 | 主链不稳定 | 首发不启用；后续只验证最多两个只读 SubAgent 的真实收益 |

## 26. 架构决策记录

### ADR-001：官方 SDK 隔离在 Adapter

- 决策：业务层只依赖 Amadeus LLM Domain；OpenAI SDK 与 Provider 方言封装在 Adapter。
- 原因：避免 SDK 类型污染 Agent、Context、Tool 和 Session 层。

### ADR-002：Responses 优先、Chat Completions 兼容

- 决策：默认 OpenAI Responses；兼容 Chat Completions，并通过 Dialect 处理 DeepSeek、GLM、Qwen 等差异。

### ADR-004：结构化事件驱动 UI

- 决策：Agent Runtime 发布强类型 Event，CLI/TUI/API 只消费事件；禁止核心逻辑直接打印终端。

### ADR-005：`AMADEUS_HOME` 配置与数据根

- 决策：`AMADEUS_HOME` 是配置、用户 AGENTS.md、Skill、MCP 和 SQLite 的唯一用户级根；不再引入独立 amadeus_root。

### ADR-009：显式指令取代自动长期记忆

- 决策：不实现用户偏好推断、RAG 或 LongTermMemory；使用用户级、项目级和目录级 `AGENTS.md` 与 Session canonical rollout。

### ADR-011：Session 恢复与中断后新 Run

- 决策：`amadeus --continue` 恢复当前项目最近 Session，`--resume` 打开选择器，`--resume <session-id>` 直接恢复；不接受 Run ID。
- 决策：中断后不恢复旧调用栈；RunRuntime 补齐悬空 Tool Result、追加 interruption marker 并 flush，下一输入创建新 Run，由模型根据真实历史和当前请求判断是否继续。

### ADR-012：结构化探索、Patch 与持续 Process

- 决策：保留结构化读搜工具作为稳定能力，同时支持 `execute_command/write_stdin`、`apply_patch` 和 `view_image`；不恢复 `write_file` 或 `revert_run` 主链。

### ADR-013：Tool-based 只读 Multi-Agent

- 决策：后续最多两个只读 SubAgent，由主 Reactor 通过工具创建、通信、等待和关闭；不建立 Team Engine、共享 DAG 或可写并发 Workspace。

### ADR-014：Plan-guided ReAct 与 Plan Mode

- 决策：Amadeus 只有一个 Reactor 执行内核；复杂 execute Run 可按需调用 `update_plan` 维护 PlanState，Runtime 不把计划编译为 DAG 或调度 Task。
- 决策：对齐 Codex，`/plan` 不接收任务文本，而是切换当前 Composer CollaborationMode；之后的普通输入创建只规划不实施的 `plan` Run，只暴露非修改型能力并输出 Proposed Plan。模式只存在于当前交互 Runtime，切换 Session 或重启后恢复 `execute`；`update_plan` 不在 Plan Mode 暴露。
- 决策：删除 Planner、Replanner、ExecutionGraph、Scheduler、ReActTaskExecutor 与旧 `planned` 主链；历史 `react/planned` RunMode 迁移为 `execute`。
- 原因：模型驱动 Tool-Use 循环比强制结构化 DAG 更能容忍随机输出，计划作为软状态即可提供透明度和长任务方向感。

### ADR-015：最小 Approval 机制

- 决策：权限与审批分离。所有文件系统 Tool 先使用 EffectivePermissionProfile；结构化 Tool 在权限范围内直接执行，Unsandboxed `execute_command` 在 Permission Check 通过后仍做 Operation Approval。ReadOnlyRoots 写入、DeniedRoots 与 ExecPolicy `forbidden` 直接拒绝；MVP 网络默认允许，不建立 NetworkPermissionStore，也不在配置文件暴露复杂 Approval Policy DSL。
- 决策：`request_permissions` 使用 `Allow for this run / Allow for this session / Deny`，分别写入 RunPermissionStore、SessionPermissionStore 或不写入；Unsandboxed Command 使用 `Allow once / Allow for session / Deny`，只有 Session 选项写入 SessionApprovalStore。三类 Store 都只存在于活动 Runtime 内存，不写入 SQLite 或配置文件。

### ADR-016：Schema 前的有限 Tool Argument Repair

- 决策：仅修复尾随逗号、缺失容器闭合符和非法 escape 等有界语法问题；repair 后仍必须通过 Schema 和完整安全链。

### ADR-017：Skill 与 MCP 渐进加载

- 决策：MCP 与 Skill 由 Session 级 ExtensionRuntime 组合，跨同一 Session 的多个 Run 复用，但不持久化连接、Catalog 或正文；SessionRuntime 只管理其生命周期引用。
- 决策：每次模型采样冻结 MCPBinding、SkillCatalogSnapshot 和显式 SkillInjection；MCP 继续使用 lazy gateway、generation/revision Catalog Cache 与目标感知 Approval，不默认动态展开全部远端 Tool。
- 决策：Skill 使用 metadata/正文分离和双路径调用：用户显式 `$skill-name` 形成 contextual user SkillInjection，模型自主发现继续通过 `read_skill` 返回 ToolOutput；不实现隐式命令检测、自动安装或专用脚本执行器。
- 决策：用户级 MCP 与 Skill 资源统一位于 `AMADEUS_HOME`；MCP 业务错误进入 failed ToolExecution，只有不可恢复的 transport/protocol/runtime 故障终止 Run。

### ADR-018：SessionRuntime、RunContext 与 RunRuntime

- 决策：旧 `ConversationSession/ConversationSessionID` 统一为 `Session/SessionID`；`StartedRun` 收敛为只读 RunContext。
- 决策：SessionRuntime 对应 Codex 长生命周期 Session，独占 SessionHistory、Rollout append/flush、当前 Active Run 及 SessionPermissionStore/SessionApprovalStore/ExtensionRuntime 生命周期；SessionCoordinator 只负责数据事务，Runtime 不实现这些组件的领域策略。
- 决策：RunContext 对应 Codex `TurnContext`，只保存稳定 Run 事实；RunRuntime 对应 `RunningTask`，RunState 对应 `TurnState`；RequestContext 对应每次采样的 `StepContext`。
- 决策：当前只有一个 Reactor 主链，不提前复制 `RegularTask/ReviewTask/CompactTask` 多 TaskKind 抽象，也不把 PlanItem 称为 Task。

### ADR-019：Canonical Rollout 与 ContextManager

- 决策：SQLite Rollout 是持久化唯一事实源，SessionHistory 是 SessionRuntime 独占的进程内镜像；ContextManager 每次 Think 先生成 RequestContext 再生成唯一 RequestView，持久压缩通过 `context_compaction` Replacement History 表达。

### ADR-020：Codex 风格 Rich Inline

- 决策：默认 TUI 使用 Bubble Tea 主屏 Rich Inline、终端原生 scrollback 与文本选择；`--plain` 作为非 TTY 和调试降级，不支持 `AMADEUS_PLAIN`。

### ADR-021：无内置 LSP 核心依赖

- 决策：通过项目原生命令验证多语言项目；LSP 只允许未来以 MCP、插件或 Extension Tool 回归。

### ADR-022：Web Search/Fetch Provider 分层

- 决策：Web Search 与 Fetch 独立；Search Provider 首版支持 DuckDuckGo、Tavily、SearXNG 和 Brave，未配置时不暴露工具。

### ADR-023：SQLite Canonical Rollout

- 决策：最终核心表为 `schema_migrations/projects/sessions/runs/rollout_items`；Tool Call、Tool Result、Plan Update、中断 Marker 和 Replacement History 统一作为 RolloutItem。
- 决策：首个稳定版本前允许一次事务化的破坏性开发迁移，直接删除旧 `conversation_*` 与旧 `runs` 后建立 canonical schema，不维护未发布历史数据的转换兼容层；稳定版发布后的 schema 变更必须保留已发布用户数据。

### ADR-024：ToolExecution 取代通用 Evidence

- 决策：ToolExecution 由 canonical ToolCall、ToolOutput 和生命周期 ToolCallOutcome 组成。ToolOutput 进入 Model 与 `tool_result` RolloutItem；ToolCallOutcome 投影给 UI/Audit；两者不形成相互竞争的结果事实，普通失败、拒绝、超时和中断必须产生可回灌结果，只有内部不可恢复故障终止 Run。
- 决策：删除 Reactor 通用 Evidence、`Verified`、`CriterionIDs`、`EvidenceBefore/EvidenceAfter` 和 `NoEvidenceThreshold`；工具成功不等于用户任务已验证。
- 决策：当前不引入独立 Verification Domain、数据库表或强制 Verifier；测试、构建、Patch 和命令事实由 ToolOutput 的 exit code/changes/output/metadata 与 ToolCallOutcome 的 status/error 共同表达。

### ADR-025：多 Root FileSystemPolicy、Sandbox 与 RunDiffProjector

- 决策：删除目标安全模型中的 PrimaryRoot。`Project.RootPath` 只持久化初始 CWD 对应的项目身份；RunContext.CWD 负责相对路径，WorkspaceRoots 是 `[CWD] + --add-dir` 的派生项目集合。结构化工具接受绝对路径及相对 CWD 的 `../`，由 PathResolver 与 FileSystemPolicy 判定 read/write/deny。
- 决策：WorkspaceRoots 不单独建表；首版 CWD 固定等于 Project.RootPath，单次 Command cwd 可变化。`--add-dir` 是附加 Workspace Root，不是泛化 Permission Grant；Run/Session 授权只形成 Additional Writable Roots。Amadeus 只有本地主机执行环境，不引入 Codex EnvironmentID。
- 决策：默认参考 Codex workspace-write，`ReadHost=true`，WorkspaceRoots、Unix `/tmp + $TMPDIR` 或 Windows `os.TempDir()` 形成的 TemporaryRoots、Run/Session Additional Writable Roots 可写；Workspace `.git/.amadeus` 由 ReadOnlyRoots 限制，敏感 Root 由 DeniedRoots 拒绝。MVP 不实现 AdditionalReadableRoots、DeniedGlobs、NetworkPermissionStore 或 Run 专属临时根；`--add-dir` 首版不持久化、不改变 Session 项目身份。
- 决策：Shell 文件权限只有在 Sandboxed 模式下由 SandboxRunner 在 OS 层强制，ExecPolicy 只输出 `skip / needs_approval / forbidden`；Linux 使用 Bubblewrap，其他平台明确 Unsandboxed，不把 requested permissions 或 token 扫描视为强 Sandbox。
- 决策：RunDiffProjector 是 Run 级内存 Patch Diff 投影，以 canonical absolute path 聚合 ApplyPatchOutput 提供的 exact delta，并发布 `RunDiffUpdated`；原始 ToolOutput 仍是 canonical rollout 事实，不增加独立事实表。
- 决策：普通 Shell、Process input、MCP、Skill script 和外部进程不更新也不 invalidate RunDiffProjector；只有已提交 Patch 的 delta 缺失、损坏或不精确时才内部失效。`RunDiffUpdated/Invalidated` 更新状态面，不在 transcript 中生成重复 Diff 或归因警告。
- 决策：实际工作区变化由独立 WorkspaceDiff 按需读取 Git tracked/untracked 状态；它覆盖任意修改来源但不提供 Tool 归因，不与 RunDiffProjector 合并，也不恢复全项目 Snapshot。
- 决策：目标架构不实现文件 Snapshot/Revert；撤销依赖 Git 或新的正常 `apply_patch`。Codex ThreadRollback 只回退上下文，Amadeus 当前也不实现；未来只能通过 append-only marker 改变 Context Projection，不修改磁盘或删除 canonical rollout。

### ADR-026：Handler 并行能力

- 决策：并行语义对齐 Codex，由 ToolHandler 暴露 `SupportsParallelToolCalls() bool`，默认 false；ToolRouter 使用 `max_parallel_tools` 提供有界并发，不保留 Shared/Exclusive 枚举或独立 ToolExecutionGate 类型。
- 决策：返回 true 的连续调用可并行；返回 false 的调用等待此前并行调用结束并阻止后续调用进入。实现使用可响应 context 取消的有序批次/屏障，不建立文件、目录、参数或 Process 级读写锁。
- 决策：首版文件读取/搜索、图片、Web 和明确只读的 MCP Handler 可返回 true；ApplyPatch、Shell、Process input、未知动态 Handler 和无法证明安全的调用返回 false。
- 决策：canonical path 只服务于 Permission、Approval、Audit、RunDiff 与 UI，不参与锁计算；资源级并行写只有在真实测量证明收益后才重新立项。

### ADR-027：Codex 风格 ToolRouter 与 ToolHandler

- 决策：目标核心模型为 ToolCall、ToolPayload、ToolInvocation、ToolSpec、ToolHandler、ToolRegistry、ToolRouter、ToolOutput、ToolCallOutcome 与 ToolExecution；删除通用 Tool.Prepare/Execute、PreparedToolCall、PreparedTarget、ToolDispatcher、ToolExecutor、ToolExecutionGate 和全工具 Authorizer。
- 决策：ToolRouter 对每个调用独立完成且只完成一次 JSON repair/parse/Schema 校验；Reactor Analyze 不重复校验，不因同批一个非法调用取消其他合法调用。校验后 Router 调用对应 Handler.Handle。
- 决策：简单 Handler 直接执行；文件 Handler 调用 FileSystemPolicy；ApplyPatchHandler 使用私有 PreparedPatch；ExecuteCommandHandler 使用私有 ExecRequest 与 ToolOrchestrator；MCP/Web 使用各自 Manager/Provider，不通过泛化 target/payload 模型。
- 决策：MVP 删除 PreExecutionHook、PostExecutionHook 与 PostWriteHooks。AuditRecorder、ToolEventPublisher 和 RunDiffProjector 是显式投影组件；未来真正实现 Hook 时再对齐 Codex PreToolUse/PostToolUse 语义。
- 决策：TargetStrategy 与 PathGuard 从目标架构删除；PathResolver、FileSystemPolicy、PreparedPatch/ExecRequest 的一致性检查与写入前 TOCTOU revalidation 继续保留。

### ADR-028：Permission、Approval 与 ExecPolicy 分层

- 决策：PermissionProfile 表达 ReadHost、WorkspaceRoots、TemporaryRoots、ReadOnlyRoots 和 DeniedRoots；RunPermissionStore 与 SessionPermissionStore 分别保存当前 Run、当前活动 Session 的 Additional Writable Roots。EffectivePermissionProfile 只由 Base + Run + Session 三层构成，不存在 one-shot、AdditionalReadableRoots、DeniedGlobs 或 NetworkPermissionStore。
- 决策：所有文件系统 Handler 都先做 Permission Check。普通写 Root 未授权时返回 `permission_required` 并提示模型调用 `request_permissions`；Permission UI 使用 `Allow for this run / Allow for this session / Deny`，授权后模型重新发起原 Tool，Runtime 不暗中恢复旧 Handler 执行。
- 决策：SessionApprovalStore 只缓存 Unsandboxed `execute_command` 的 `ApprovedForSession`。CommandApprovalKey 精确包含 Shell、最小规范化 Command、canonical CWD、TTY 与 IsolationMode；requested permissions、EffectivePermissionProfile、timeout、yield 和输出预算不进入 Key，因为 Permission Check 每次独立重算且不能被 Approval 缓存跳过。
- 决策：ExecPolicy 只输出 `skip / needs_approval / forbidden`。它维护极小的灾难性命令拒绝集并读取当前 Isolation/Approval Facts，不解析 command 内部 Root、不模拟文件系统 Sandbox，也不维护复杂风险等级树。
- 决策：带显式 Root 参数的结构化 Handler 在 Handle 中相对 RunContext.CWD 解析为 canonical absolute targets；`execute_command` 只规范化显式 cwd 和模型声明的 `requested_permissions.writable_roots`，不解析 command 字符串。Sandboxed 由 Bubblewrap 强制 EffectivePermissionProfile；Unsandboxed 在 Permission Check 后对完整命令询问 `Allow once / Allow for session / Deny`。
- 决策：RunPermissionStore 随 Run 终态销毁；SessionPermissionStore 与 SessionApprovalStore 随 SessionRuntime 关闭销毁。三者均不写入 SQLite、不随 `--resume` 跨进程恢复；ReadOnlyRoots、DeniedRoots、ExecPolicy forbidden 和未声明远端能力不能被普通授权绕过。
- 决策：删除 Shell path-token 扫描、复杂 CommandGuard 风险树、`AllowCommandFilesystemPaths`、按 Tool 名称宽泛授权和 Run-local 通用 GrantCache；只保留轻量参数检查与极小灾难性命令拒绝集，并保留可审计的 Permission Grant 与 Approval Decision 事件。

### ADR-029：Prompt 资产内置化与 Codex 选择性借鉴

- 决策：删除顶层 `prompts/` 公共 package，内置模板迁移到 `internal/prompt/builtin/templates`；`internal/prompt` 统一负责 Repository、Assembler、来源 Hash、变量和 Contract，Reactor 不直接依赖模板资产。
- 决策：Prompt 按 Agent Base、Execute/Plan Mode、动态 Runtime/Permission、Instructions、Extensions、Tool Guidance、Compaction 分层装配；稳定规则和动态事实不得混写，同一规则必须有唯一所有者。
- 决策：选择性借鉴 Codex 的任务执行、进度沟通、计划边界、工具纪律、权限上下文和交付格式，并全部改写为 Amadeus Runtime 语义；不整套复制模型专属、Goal、Realtime、Memories、Review 或 Multi-Agent Prompt。
- 决策：Prompt 正确性由变量/层级 Contract、Tool Exposure、Permission 一致性、Responses/Chat Provider mock E2E 和 Coding Agent smoke 验收，不以长文本逐字 Snapshot 作为主要完成证据。

### ADR-030：TUI Visual Runtime 对齐 Codex 状态模型

- 决策：M9 只视为 Composer、Slash Popup、SelectionOverlay 与 Session Command 交互重构完成；视觉运行时单独由 M9V 完成，M9V 结束前不得进入 M10 发布冻结。
- 决策：保留 Go、Bubble Tea、Lip Gloss、Amadeus Logo、主屏 scrollback 和原生鼠标行为；移植 Codex 的 TerminalPalette、Motion、TranscriptCell/ActiveCell、Exec/Explore/WebSearch Cell 与 FinalMessageSeparator 状态模型，不直接复制 Rust/Ratatui 类型。
- 决策：TUI 不把 Reactor Iteration 当作排版边界。Tool Started/Delta/Completed 原位更新 ActiveCell，提交与 separator 由 `HadWorkActivity/NeedsFinalMessageSeparator` 和内容边界驱动；Cell 内容不通过伪造 `\n` 或 previous-kind 特判控制间距。
- 决策：颜色以终端默认 foreground/background、dim/bold、terminal-aware cyan 和 success/error green/red 为主；Working 使用可注入单调时钟、约 32ms redraw、2s 余弦 shimmer 与 Reduced Motion fallback，不再维护固定 pastel Dashboard Palette 或 frame 阶梯色。

### ADR-031：Handler 主链与确定性 Apply Patch

- 决策：Tool 调用统一经过 ToolRouter 的 Registry/visibility、唯一一次 repair/parse/Schema 校验和 Handler 并行调度，再进入 ToolHandler.Handle；Reactor Analyze 不重复校验参数，同批调用独立成功或失败。
- 决策：Permission、Operation Approval 与 Sandbox 是三层不同边界。文件 Permission 由对应 Handler 通过 FileSystemPolicy 完成；ExecuteCommandHandler 的 ToolOrchestrator 处理 Approval/Sandbox；Sandbox 只在 OS 层约束进程，不能替代 Tool Schema 或 Permission。
- 决策：删除目标架构中的 PathGuard、通用 PreparedToolCall 与全工具 Authorizer；保留 PathResolver、FileSystemPolicy、ApplyPatchHandler 私有 PreparedPatch、ExecuteCommandHandler 私有 ExecRequest 和执行前 staleness/identity revalidation。
- 决策：`apply_patch` 保留完整 preflight、零/多匹配拒绝、Add/Move 不覆盖、staging、提交前 bytes/identity revalidation、partial commit metadata 与 exact delta；新增 End of File marker、每级唯一的有限匹配梯度、内容级 AppliedPatchDelta 和流式 Patch Preview。
- 决策：选择性借鉴 Codex ToolOrchestrator，只为 `execute_command` 等真实进程操作建立专用进程 Runtime；结构化文件 Handler 不进入通用 OS Sandbox Orchestrator，不复制全工具审批/重试框架。

## 27. 当前统一决策

1. `Session` 是可恢复对话；`SessionRuntime` 拥有活动会话历史、当前 Run 和 Session 级 Permission/Approval/Extension 生命周期；`Run` 是一次真实用户输入；`RolloutItem` 是有序持久化事实。
2. SessionRuntime 通过 SessionCoordinator 创建 Run、首条 user item 和只读 RunContext，再启动 RunRuntime/RunState；任何模型调用或工具副作用前必须已有持久化 RunID。
3. RunContext 只保存稳定事实，RequestContext 表示一次采样的动态环境，RequestView 是模型请求投影；SessionHistory 不进入 RunContext 或 RunRuntime 所有权。
4. Reactor 是唯一 Agent 执行内核；一次 Think→Analyze→Act→Observe 称为 Iteration，Observe 处理 ToolExecution 而不复制 Evidence 数据树。
5. 默认 `execute` Run 可按需调用 `update_plan`；PlanState 是软状态，不驱动 Scheduler。
6. `/plan` 切换当前 Composer 到只规划不实施的 Plan Mode；后续普通输入创建 `plan` Run，最终计划进入 Session Rollout，再切回 `execute` 后由新的 Run 负责实施。
7. 不保留 Planner、Replanner、DAG、Plan Task、TaskStatus、Scheduler、Verifier/Reflector 强制门或 Final Synthesizer 主链。
8. 删除 `ChatSession`；Reactor、Plan Mode、Compactor 和可选纯聊天入口统一复用无状态 `LLMRuntime`，Provider 请求使用 LLMCallID。
9. ToolExecution 取代通用 Evidence；ToolOutput 是 canonical 结果内容，ToolCallOutcome 只表达生命周期状态；当前不单独建立 Verification，模型依据结构化 Tool Call/Output 与 Workspace 判断完成状态。
10. Session、Run 与 canonical RolloutItem 持久化；SessionRuntime、RunRuntime、RunState、RequestContext、Iteration、RequestView 和 Tool Future 不建表，PlanState 通过 `plan_update` item 投影恢复。
11. 中断时补齐工具协议并追加 marker；下一输入创建新 Run，不精确恢复旧执行位置，也不使用 Previous Work 摘要强制继续。
12. Multi-Agent 后续采用最多两个只读 SubAgent 的 Tool-based Delegation，不依赖 Plan DAG。
13. Project.RootPath 只负责持久化项目身份；CWD、WorkspaceRoots、TemporaryRoots、PathResolver、FileSystemPolicy、ReadOnlyRoots、DeniedRoots、Run/Session Permission Store、ExecPolicy、SandboxRunner、SessionApprovalStore、Audit、RunDiffProjector 与 ToolRouter 共同形成 Tool 执行硬边界。ToolRouter 只依据 Handler.SupportsParallelToolCalls 做有界调度，不建立资源级锁。
14. 默认 Rich Inline TUI、ContextManager、AGENTS.md、Skill、MCP、Web 和收敛工具链继续复用同一 SessionRuntime/RunRuntime/Reactor 主链。
15. SessionRuntime 持有 Session 级 ExtensionRuntime 生命周期；每次采样通过 RequestContext 冻结 MCPBinding、SkillCatalogSnapshot 和显式 SkillInjection，MCP/Skill 不建立第二套历史、审批或执行器。
16. `ReadHost=true` 使结构化读取在未命中 DeniedRoots 时直接执行；写入 Effective Writable Roots 内直接执行，范围外返回 `permission_required` 并由 `request_permissions` 写入 Run/Session Permission Store。Sandboxed Shell 由 Bubblewrap 强制范围；Unsandboxed Shell 在相同 Permission Check 后使用 SessionApprovalStore 对完整命令做操作审批。精确 Patch 变化由 RunDiffProjector 投影，目标架构不实现 Run 文件 Snapshot/Revert。
17. Amadeus 当前不实现 ThreadRollback；未来若增加，只能追加 rollback marker 并改变 Context Projection，不修改磁盘、不删除 canonical rollout。
18. Prompt 模板属于 `internal/prompt/builtin`，由 Bootstrap 按 Agent Base、RunMode、动态 Runtime/Permission、Instructions、Extensions、Tool Exposure 和 Compaction 职责分层装配；Reactor 不读取模板资产。Codex Prompt 只选择性改写为已实现的 Amadeus Runtime 语义，不预埋模型专属或尚未实现的能力。
19. 默认 Rich Inline 的交互层与视觉层分离：Composer/Slash/Selection 已由 M9 收敛，M9V 使用 TerminalPalette、Motion、TranscriptCell/ActiveCell 和内容边界 Separator 替换旧 `fullscreenEntry`/Iteration 排版链；UI 仍只消费 Typed Events，不拥有 Agent、Session 或 Tool 事实。
20. ToolRouter 是唯一参数校验和分发入口；ToolHandler 是业务执行入口。普通 Handler 直接执行，文件 Handler 调用 FileSystemPolicy，ApplyPatchHandler 使用私有 PreparedPatch，ExecuteCommandHandler 使用私有 ExecRequest/ToolOrchestrator；目标架构不保留通用 PreparedCall、全工具 Authorizer 或伪通用 Hook。
21. `apply_patch` 是首选结构化写入能力，保留唯一匹配、无静默覆盖、全量 preflight、staging、identity revalidation、partial metadata 与 exact delta；匹配容错必须有界且每级唯一，普通 Shell 修改不伪装为 Patch 归因。
