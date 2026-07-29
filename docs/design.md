# Amadeus 架构设计

> 状态：Draft v0.1  
> 创建日期：2026-07-29  
> 输入依据：`docs/thought.md`、`../paicli-main` 当前代码  
> 目标语言：Go  
> 产品形态：本地 Agent CLI，后续可复用同一运行时提供 Runtime API

## 1. 背景

Amadeus 的目标是将 `../paicli-main` 的能力迁移到 Go，并在保持核心用户行为的前提下修正原项目中职责过度集中、Provider 重复实现和运行时耦合较重的问题。

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
- Prompt 分层覆盖、上下文压缩、长期记忆和 RAG。
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
- 首先交付可工作的 ReAct Agent，再迁移 Plan、Team 和扩展能力。
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
  ├── Agent Runtime ─────── LLM Adapter ───── OpenAI/OpenAI-compatible endpoint
  ├── Tool Executor ─────── Filesystem / Shell / Web / MCP / LSP / Browser
  ├── Context Manager ───── Prompt / Memory / Skills / RAG
  ├── Safety Pipeline ───── PathGuard / CommandGuard / HITL / Audit
  └── Event Bus ─────────── Renderer / API stream / Trace log
```

## 6. 分层架构

### 6.1 Interface 层

负责协议适配，不包含 Agent 决策逻辑。

- `cmd/amadeus`：命令行入口。
- `internal/interface/cli`：交互循环、slash command、补全和 history。
- `internal/interface/tui`：可选全屏 TUI。
- `internal/interface/httpapi`：线程、回合和事件流 API。
- `internal/render`：plain、inline、TUI/API event renderer。

### 6.2 Application 层

负责用例编排和生命周期。

- `internal/app/bootstrap`：加载配置并装配依赖。
- `internal/app/session`：会话状态、模式切换和取消。
- `internal/app/command`：本地命令处理。
- `internal/app/task`：后台任务用例。

### 6.3 Domain/Runtime 层

包含 Agent 的稳定业务模型。

- `internal/agent/runtime`：统一 turn loop、预算、终止条件和事件。
- `internal/agent/event`：Runtime 结构化事件、Sink Port 与测试用 Memory Sink。
- `internal/agent/react`：ReAct 策略。
- `internal/agent/plan`：计划生成、审阅和 DAG 执行。
- `internal/agent/team`：角色、委派和汇总。
- `internal/conversation`：消息、内容块和上下文压缩。
- `internal/prompt`：Prompt 分层装配。
- `internal/tool`：工具协议、注册表和执行器。

### 6.4 Infrastructure 层

负责外部系统实现。

- `internal/llm/openai`：官方 OpenAI Go SDK Adapter。
- `internal/config`：配置文件、环境变量和 CLI override 合并。
- `internal/logging`：结构化日志初始化、级别过滤和敏感属性脱敏。
- `internal/store`：SQLite/JSONL/文件存储。
- `internal/mcp`、`internal/web`、`internal/browser`、`internal/lsp`。
- `internal/memory`、`internal/rag`、`internal/skill`、`internal/snapshot`。
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
│   │   ├── runtime/
│   │   ├── react/
│   │   ├── plan/
│   │   └── team/
│   ├── config/
│   ├── conversation/
│   ├── llm/
│   │   └── openai/
│   ├── tool/
│   │   ├── builtin/
│   │   └── executor/
│   ├── policy/
│   ├── prompt/
│   ├── memory/
│   ├── mcp/
│   ├── rag/
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
```

工具定义与工具实现分离；注册表只负责查找和快照，不实现具体业务。

### 8.4 Agent Runtime

```go
type Runtime struct {
    LLM       llm.Client
    Tools     tool.Executor
    Context   conversation.Manager
    Prompts   prompt.Assembler
    Events    event.Sink
    Limits    Limits
}
```

策略层决定“下一步请求什么”，Runtime 负责通用循环、预算、并发工具执行、错误归一化和事件发布。

M1-10 在 `internal/agent/runtime` 实现最小单轮 `Session`。构造时显式注入 `llm.Client`、`event.Sink`、temperature 和 max output tokens；模型名与 Provider 信息来自 Client 的稳定 `ModelInfo`。`RunTurn` 接收显式 turn ID 和一条用户文本，不读取配置、不生成 ID，也不直接输出到 stdout。M1-12 在此基础上增加最小对话历史：只有成功完成的 user/assistant 消息对会提交到 Session，失败轮次不会污染下一次请求；上下文裁剪、持久化和记忆仍留在 M3。

单轮事件顺序固定为 `TurnStarted`，随后按 SDK 流顺序发布 `ReasoningDelta/TextDelta`，收到 usage 时发布 `UsageUpdated`，流关闭成功后发布 `TurnCompleted`。Session 同时聚合 assistant content/reasoning、response/request ID、finish reason 和 usage，返回完整 Domain `Response`。若打开或读取流失败，发布 `ErrorOccurred` 并返回原错误；若在完成事件前收到 `io.EOF`，归类为 `protocol` 错误。无论成功失败都关闭流，Event Sink 发布失败不会被静默忽略。

## 9. ReAct 执行流程

```text
1. 接收用户输入并清理历史图片 payload
2. 解析 @path / @image / MCP mention
3. 写入会话并构建分层 system prompt
4. 按 token budget 压缩历史、检索记忆和技能上下文
5. 调用 LLM 并流式发布事件
6. 若无 tool call：保存最终回答并结束
7. 若有 tool call：
   7.1 校验格式和本轮预算
   7.2 经过 Policy → HITL → Audit
   7.3 对允许并行的调用进行有界并发执行
   7.4 将文本/图片结果按原 tool_call_id 回灌
8. 检查取消、最大轮数、重复调用和 token budget
9. 回到步骤 5
```

终止原因必须结构化：`completed`、`cancelled`、`max_steps`、`budget_exceeded`、`provider_error`、`tool_error`。

## 10. Plan 与 Team

### 10.1 Plan-and-Execute

- Planner 只生成结构化 `ExecutionPlan`。
- Reviewer 负责用户确认、修改或取消。
- Scheduler 根据依赖关系选择 ready tasks，并限制并发度。
- 每个 task 复用同一个 Runtime，但使用独立的 task context 和预算。
- Task 输出写入 plan state，供后续任务和最终 synthesis 使用。

### 10.2 Multi-Agent

- Orchestrator 负责角色选择、任务拆分和预算分配。
- SubAgent 共享工具定义，但拥有独立会话视图和最小必要上下文。
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

M1-12 新增实现 Domain `llm.Client` 的 OpenAI Client，内部持有官方 SDK Client，并根据 Provider `api` 配置选择 Responses 或 Chat Completions 流。`Complete` 通过同一流协议聚合 Domain `Response`，`Stream` 不向上暴露 SDK 类型。Responses 模式声明当前已实现的 developer/reasoning/stream usage/cache usage 能力；通用 Chat compatible 模式只保守声明 streaming，等待后续 capability/dialect 配置，避免根据 API 名称推断厂商扩展能力。

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
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    model: your-model-id
    timeout: 120s
    max_retries: 2
    temperature: 0.2
    max_output_tokens: 8192

  compatible:
    api: chat_completions
    api_key: ${COMPATIBLE_API_KEY}
    base_url: https://example.invalid/v1
    model: compatible-model

agent:
  mode: react
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

仓库提供可直接通过结构校验的 `configs/amadeus.example.yaml`。该文件可复制为 `<amadeus-root>/config.yaml`，默认不包含凭证或固定模型；实际运行时优先通过 `AMADEUS_API_KEY` 和 `AMADEUS_MODEL` 注入，避免把密钥提交到版本控制。

文件加载完成后，以下直接环境变量覆盖当前配置：

| 环境变量 | 覆盖字段 |
|---|---|
| `AMADEUS_PROVIDER` | `default_provider`，并决定其余变量作用的 Provider |
| `AMADEUS_API` | 当前 Provider 的 `api` |
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
- Provider 名称不能为空；`api` 只接受 `responses` 或 `chat_completions`。
- `base_url` 必须是无 userinfo、无 fragment 的绝对 `http` 或 `https` URL。
- Provider timeout 范围为 `(0, 30m]`，重试次数为 `[0, 10]`，temperature 为 `[0, 2]`，max output tokens 为 `[1, 1_000_000]`。
- Agent mode、approval default 和 log level 必须属于已定义枚举；最大步骤为 `[1, 1000]`，并行工具数为 `[1, 64]`。
- 未提供 API key 时允许启动配置诊断命令，但不允许开始模型回合。
- 未配置 model 时返回明确错误，不静默选择可能变化的远端默认模型。

结构校验允许 API key 和 model 暂时为空；需要模型调用的用例应在启动回合前执行能力校验。所有配置层应用完成后再执行结构校验，确保 CLI flags 可以修正文件或环境中的值。

## 13. Prompt 架构

保留 PaiCLI 的分层思想：

```text
base → personality → mode → approval → runtime_context
     → project_context → skills → context_management → handoff
```

覆盖顺序：内置 Prompt < 用户级 Prompt < 项目级 Prompt。覆盖采用整文件替换，避免半结构合并产生不可预测结果。Assembler 负责变量校验、缺失层诊断和最终 hash，便于 trace 与测试。

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

## 16. 上下文、记忆与 RAG

### 16.1 上下文预算

- 预算分为 system、history、tool results、retrieval 和 output reserve。
- 优先裁剪历史大工具结果与旧图片 payload。
- 压缩产生摘要消息，并保留摘要覆盖的消息范围和 hash。
- 模型 tokenizer 不可用时使用可替换的保守估算器。

### 16.2 记忆

- Conversation Memory：当前 session 内消息。
- Long-term Memory：用户显式或 Agent 通过工具确认保存的事实。
- Memory 检索结果必须标注来源，避免与用户本轮指令混淆。

### 16.3 RAG

精确代码定位默认使用 `glob_files`、`grep_code`、`read_file`。RAG 只作为模糊检索辅助，不替代精确搜索。索引、分块、Embedding 和 VectorStore 均定义接口，允许后续替换实现。

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

`amadeus chat` 执行完整配置主链和校验，选择 default Provider，创建 OpenAI Domain Client、Plain Renderer、Runtime Session 与 ChatLoop。基础 plain 模式不打印 banner 或输入提示符，保持终端和管道输出一致；更丰富的交互提示、history 和补全留在 M7。

M1-13 为每个 turn 创建独立子 context。生产命令通过 `signal.NotifyContext(parent, os.Interrupt)` 只在模型请求执行期间监听 Ctrl+C，并在该轮返回后立即停止监听；因此 Ctrl+C 会取消当前 SDK HTTP/SSE 调用，但不会取消 ChatLoop 的父 context。父 context 被外部取消时仍会终止整个循环。

取消沿 `ChatLoop → Session → llm.Client → SDK transport` 传播。Session 关闭流、发布安全的 `ErrorOccurred(request cancelled)`，不提交本轮 user/assistant 历史；Plain Renderer 结束可能存在的部分 stdout 行并把取消信息写入 stderr；ChatLoop 识别单轮 `context.Canceled` 或 `ProviderError(cancelled)` 后继续读取下一条输入。测试通过可注入的 turn context factory 模拟 Ctrl+C，不向测试进程发送真实信号。

M1-14 使用 `httptest.Server` 建立命令级 Provider mock 集成测试，不替换 OpenAI Domain Client 或 Runtime。测试从临时 Amadeus `config.yaml` 启动真实 `amadeus chat` 命令链，覆盖配置加载、default Provider 选择、Bearer 认证、SDK 请求、SSE 解码、Session 聚合、事件发布和 Plain Renderer 输出。

Responses fixture 断言 `/v1/responses`、`model/input/temperature/max_output_tokens/stream`；Chat Completions fixture 断言 `/v1/chat/completions`、`model/messages/temperature/max_tokens/stream` 和 `stream_options.include_usage`。两种模式均返回流式 `hello` 和 usage，命令输出一致为 `hello\n`，且 stdout/stderr 不包含 API key。所有测试只访问进程内本地 server，不依赖公网或真实 Provider 凭证。

M1-15 于 2026-07-29 使用项目根目录真实配置完成 Chat Completions compatible Provider smoke test。测试通过 `AMADEUS_HOME=<project-root>` 启动构建产物，发送只要求返回固定标记的最小输入，真实流式输出为 `AMADEUS_SMOKE_OK`，命令状态为成功。Provider 名称、API 模式和模型可以记录，但不记录 base URL、API key 或请求/响应正文；本次 Provider/SDK 流未向 plain 命令暴露 HTTP request ID，因此 smoke 记录明确标记为 unavailable，而不使用响应正文或内部地址代替。

根目录运行配置 `config.yaml` 可能包含真实凭证，必须由 `.gitignore` 排除；仓库只提交 `configs/amadeus.example.yaml`。真实 smoke test 前后都使用 `config explain` 确认 API key 输出为 `[REDACTED]`。

## 20. 持久化

| 数据 | 默认实现 | 位置 |
|---|---|---|
| Amadeus 主配置 | YAML | `<amadeus-root>/config.yaml` |
| CLI history | 文本或 SQLite | `~/.local/share/amadeus/history` |
| 审计 | JSONL | `~/.local/state/amadeus/audit/` |
| 长期记忆 | SQLite | `~/.local/share/amadeus/memory.db` |
| 后台任务 | SQLite | `~/.local/share/amadeus/tasks.db` |
| 用户 Prompt/Skill | 文件 | `~/.config/amadeus/` |
| 项目 Prompt/Skill/MCP | 文件 | `<project>/.amadeus/` |

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
| M2 | ReAct + 核心文件/Shell 工具 | Agent 可在临时项目中读、改、测 |
| M3 | 安全、Prompt、上下文、记忆 | 可持续多轮并受策略保护 |
| M4 | Plan + Multi-Agent | `/plan`、`/team` 可验收 |
| M5 | MCP + Skill + Web/RAG | 外部工具和知识扩展可用 |
| M6 | Snapshot/LSP/Image/Browser | 高级编码工作流可用 |
| M7 | TUI + Runtime API + 后台任务 | 多入口共享 Runtime |
| M8 | 兼容回归与发布 | 形成可发布二进制和迁移说明 |

每个阶段的最小任务、依赖与验收见 `docs/migration-progress.md`。

## 24. 兼容性策略

### 24.1 必须兼容

- 三种 Agent 模式的核心语义。
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
| 本地安全边界被误解 | 用户风险 | 明确不是沙箱，默认 HITL 和审计 |
| TUI 过早消耗开发量 | 核心 Runtime 延迟 | Plain/inline CLI 先行，TUI 后置 |

## 26. 架构决策记录

### ADR-001：官方 SDK 隔离在 Adapter

- 决策：Domain 不引用 OpenAI SDK 类型。
- 原因：支持 base URL、兼容 API、测试替身和未来 SDK 升级。

### ADR-002：Responses 优先、Chat Completions 兼容

- 决策：Provider 配置显式指定 API 模式。
- 原因：兼顾 OpenAI 当前主接口和原项目多 Provider 兼容需求。

### ADR-003：统一 Agent Runtime

- 决策：ReAct、Plan task 和 SubAgent 复用 turn/tool loop。
- 原因：减少重复状态机和协议差异。

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

## 27. 开工前必须确认的实现决策

这些决策不阻塞当前架构，但应在对应任务开始时固定：

1. 首个验收 Provider/API 模式。
2. 是否在 M2 就加入 inline renderer，还是先使用 plain renderer。
3. Side-Git 继续采用独立快照仓库，还是先实现轻量文件备份 MVP。
