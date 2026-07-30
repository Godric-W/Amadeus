# Amadeus 迁移进度

> 创建日期：2026-07-29  
> 对应架构：`docs/design.md`  
> 源项目：`../paicli-main`  
> 总体状态：M2 统一 Agent Engine 与核心工具完成，当前目标是交付首个真正可用的 Amadeus Coding Agent CLI

## 1. 使用规则

### 1.1 状态

- `TODO`：尚未开始。
- `DOING`：正在进行，同一时间尽量只有一个主任务处于该状态。
- `BLOCKED`：存在明确外部阻塞，备注中必须写原因。
- `DONE`：产物、测试和验收标准全部满足。
- `SKIPPED`：经决策不迁移，必须记录原因和替代方案。

### 1.2 完成定义

一个任务只有同时满足以下条件才可标记 `DONE`：

1. 代码或文档已进入仓库。
2. 对应包可编译。
3. 任务中列出的自动测试通过；无自动测试时完成手工验收记录。
4. 新增配置和行为已更新相关文档或示例。
5. 若发生优化，已更新本文档“优化记录”。

### 1.3 任务执行约束

- 每次优先领取最小编号且依赖已完成的任务。
- 一个任务只解决一个可验证问题，不夹带无关重构。
- 若任务实际过大，先拆出子任务再编码。
- Java 源文件仅作为行为基线，不直接修改。
- 所有真实网络测试默认使用 mock；手工 Provider smoke test 单独记录。

## 2. 里程碑总览

| 里程碑 | 目标 | 状态 | 完成度 |
|---|---|---:|---:|
| D0 | 架构与迁移计划 | DONE | 100% |
| M0 | Go 工程与配置骨架 | DONE | 100% |
| M1 | OpenAI SDK 与纯文本会话 | DONE | 100% |
| M2 | 统一 Agent Engine、ReAct 与核心工具 | DONE | 100% |
| M3 | 首个可用 Coding Agent CLI | TODO | 0% |
| M4 | 长上下文、会话持久化与恢复 | TODO | 0% |
| M5 | Adaptive Planning 与 Replan | TODO | 0% |
| M6 | Coding Workflow 扩展 | TODO | 0% |
| M7 | Multi-Agent 与高级入口 | TODO | 0% |
| M8 | 兼容回归与发布 | TODO | 0% |

## 3. 当前焦点

- 当前阶段：`M3`。
- 下一任务：`M3-00`，删除 `agent.mode` 配置与枚举。
- 当前阻塞：无。
- 最近完成：`M2-33` 与 M2 完成审计；DirectEngine、ReActRunner、Verifier、Reflector 和 MVP 工具已形成内部闭环。

### 3.1 交付优先级

1. **M3 可用基线**：真实 Provider + 根命令 `amadeus`/`amadeus "<task>"` + 项目工具 + 分层 `AGENTS.md` + 安全审批，可以在用户项目中完成读、改、测。
2. **M4 单 Agent 可靠性**：长上下文、Conversation、压缩、Checkpoint/Resume，不增加新的 Agent 模式。
3. **M5 智能规划增强**：Plan-on-Demand、ExecutionGraph、Scheduler、Replan 和 `/plan`，仍由同一个 ReActRunner 执行 Task。
4. **M6 Coding Workflow 扩展**：Snapshot、LSP、Skill、MCP 与 Web，不阻塞首个可用版本。
5. **M7 可选高级能力**：Multi-Agent、图片、Browser、TUI、Runtime API 和后台任务，不阻塞单 Agent 发布。
6. **M8 发布**：以稳定的单 Agent + Adaptive Planning 为发布基线；M6/M7 能力按成熟度选择性纳入。

## 4. D0：架构与计划

| ID | 状态 | 任务 | 产物/验收 |
|---|---|---|---|
| D0-01 | DONE | 阅读 `docs/thought.md` | 目标、约束和交付物已提炼 |
| D0-02 | DONE | 梳理 `paicli-main` 架构 | 已识别入口、三条 Agent 路径、主要模块和风险 |
| D0-03 | DONE | 编写架构与进度文档 | `docs/design.md` 与本文档存在且互相引用 |

## 5. M0：Go 工程与配置骨架

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M0-01 | DONE | D0-03 | 确定 module path、Go 版本、SDK/CLI/YAML 库 | `go.mod` 已初始化，选型写入 `docs/design.md` ADR-006 |
| M0-02 | DONE | M0-01 | 创建 `cmd/amadeus` 和最小 `version` 命令 | `go run ./cmd/amadeus version` 输出 `amadeus dev` |
| M0-03 | DONE | M0-02 | 增加构建信息结构 | 默认输出稳定，version、commit、build time 可通过 `-ldflags -X` 注入 |
| M0-04 | DONE | M0-02 | 建立 `internal/config` 数据结构 | Provider、Agent、Approval、Logging 配置可直接构造，默认配置不包含文件 IO |
| M0-05 | DONE | M0-04 | 加载 Amadeus 根目录 YAML | 临时 Amadeus 根目录可读取 `<amadeus-root>/config.yaml`；不存在时返回独立默认配置 |
| M0-06 | DONE | M0-05 | 支持显式配置文件路径 | 任意路径可加载并覆盖默认值；显式文件不存在时返回带路径错误 |
| M0-07 | DONE | M0-06 | 实现 `${ENV_VAR}` 展开 | 支持完整值和嵌入式引用；缺失变量错误包含字段路径与变量名 |
| M0-08 | DONE | M0-07 | 实现环境变量覆盖 | Provider、key、base URL、model、API 模式覆盖及优先级测试通过 |
| M0-09 | DONE | M0-08 | 实现 CLI flags 覆盖 | `--config/--provider/--api/--base-url/--model` 高于环境和文件；不暴露 `--api-key` |
| M0-10 | DONE | M0-09 | 增加配置校验 | Provider、API、URL、数值和枚举错误均返回稳定字段路径；API key/model 允许诊断阶段为空 |
| M0-11 | DONE | M0-10 | 增加配置脱敏 | 所有已设置 API key 固定替换为 `[REDACTED]`；原配置不变，YAML 输出不含原始密钥 |
| M0-12 | DONE | M0-11 | 实现 `config check` | 完整配置链校验成功返回 0；加载或字段校验错误返回非 0，输出不泄露 API key |
| M0-13 | DONE | M0-12 | 实现 `config explain` | 按稳定顺序输出全部有效字段及 default/file/environment/cli 来源，API key 固定脱敏 |
| M0-14 | DONE | M0-12 | 添加示例配置 | `configs/amadeus.example.yaml` 可通过 `config check`，且不包含真实密钥 |
| M0-15 | DONE | M0-14 | 建立日志初始化 | level 可配置，敏感结构化属性统一脱敏，不使用全局 Logger |
| M0-16 | DONE | M0-15 | 添加基础 CI 命令脚本或 Makefile | `make check` 统一执行 format check、vet、test、build |

### M0 出口

```bash
amadeus version
amadeus config check --config ./configs/amadeus.example.yaml
amadeus config explain
```

三条命令均可运行，且不需要真实 API key 才能完成配置结构检查。

## 6. M1：OpenAI SDK 与纯文本会话

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M1-01 | DONE | M0-16 | 定义 Domain Message/Request/Response | `internal/llm` 类型不引用 OpenAI SDK，角色、结束原因和 usage 测试通过 |
| M1-02 | DONE | M1-01 | 定义 LLM Client/Stream 接口 | fake client/stream 可独立实现，`Recv`、`io.EOF`、`Close` 契约测试通过 |
| M1-03 | DONE | M1-02 | 创建 OpenAI SDK client factory | API key/base URL/timeout/retry 均来自配置，mock transport 契约测试通过 |
| M1-04 | DONE | M1-03 | 实现 Responses 文本请求转换 | httptest 断言 model/input/temperature/max_output_tokens 和消息角色顺序 |
| M1-05 | DONE | M1-04 | 实现 Responses 文本流解析 | 文本/reasoning 增量、完成、usage、incomplete、error/failed 和损坏 SSE fixture 通过 |
| M1-06 | DONE | M1-03 | 实现 Chat Completions 文本请求转换 | compatible fixture 断言 model/messages/temperature/max_tokens 和角色顺序 |
| M1-07 | DONE | M1-06 | 实现 Chat Completions 文本流解析 | SSE 文本分片、结束原因、尾部 usage 和无 usage 回退测试通过 |
| M1-08 | DONE | M1-05,M1-07 | 归一化 Provider 错误 | 认证、限流、网络、取消及扩展分类契约测试通过 |
| M1-09 | DONE | M1-08 | 定义 Runtime event 与 Sink | Memory Sink 顺序、快照隔离、并发和拒绝行为测试通过 |
| M1-10 | DONE | M1-09 | 实现最小单轮 Session | fake LLM 单用户消息、流聚合、事件顺序和错误路径测试通过 |
| M1-11 | DONE | M1-10 | 实现 plain renderer | 文本流、完成换行、错误 stderr 和 Writer 失败测试通过 |
| M1-12 | DONE | M1-11 | 实现交互式 `chat` 循环 | 配置装配、多轮输入、流式回答、`/exit` 和 EOF 测试通过 |
| M1-13 | DONE | M1-12 | 接入 Ctrl+C/context cancel | 当前请求取消、失败历史回滚和后续输入测试通过 |
| M1-14 | DONE | M1-13 | 添加 Provider mock 集成测试 | 两种 API 模式均通过本地命令级 mock，无需公网 |
| M1-15 | DONE | M1-14 | 完成一次真实 Provider smoke test | 记录日期、配置模式、成功结果；request ID unavailable 已说明 |

### M1 出口

```bash
amadeus chat --provider openai
```

可以流式完成纯文本多轮对话，支持 Ctrl+C 取消当前响应。

### M1-15 真实 Provider Smoke Test

| 日期 | Provider | API 模式 | 模型 | 结果 | Request ID |
|---|---|---|---|---|---|
| 2026-07-29 | `compatible` | `chat_completions` | `deepseek-v4-flash_DeepSeek` | PASS：真实流式返回预期标记 `AMADEUS_SMOKE_OK` | unavailable：Provider/SDK 流未向 plain 命令暴露 HTTP request ID |

安全记录：测试使用项目根目录 `config.yaml`，`config explain` 中 API key 显示为 `[REDACTED]`；未记录 API key、base URL 或敏感请求正文。根目录 `config.yaml` 已加入 `.gitignore`。

## 7. M2：统一 Agent Engine、ReAct 与核心工具

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M2-01 | DONE | M1-15 | 定义 Tool Spec/Call/Result/Executor 接口 | call ID、side effect、ParallelSafe、resource strategy 不依赖 Provider SDK 和 UI |
| M2-02 | DONE | M2-01 | 实现并发安全 Tool Registry | 注册、重复注册、查找和快照测试通过 |
| M2-03 | DONE | M2-02 | 实现 JSON Schema 校验与受限 JSON repair | 缺失、类型、未知字段和损坏 JSON 行为明确；repair 后仍必须校验 |
| M2-04 | DONE | M2-01 | 定义 Run/ExecutionGraph/Task/Step/Evidence Domain | 单 root Task 与多 Task 图可序列化，不依赖 LLM/工具实现 |
| M2-05 | DONE | M2-04 | 定义 RunStatus/TaskStatus/StopReason 与合法状态转换 | 非法跳转被拒绝，candidate complete 不等于 completed |
| M2-06 | DONE | M2-03,M2-04 | 定义 Provider Dialect/Capabilities 与显式选择配置 | `standard/openai/deepseek/qwen/glm` 可选择；不按 URL 或模型名猜测 |
| M2-07 | DONE | M2-06 | 实现标准 Responses/Chat 工具协议转换 | tool 定义、assistant tool call、tool result 两种格式 fixture 通过 |
| M2-08 | DONE | M2-07 | 实现 DeepSeek/Qwen/GLM Chat 方言 | 仅覆盖已验证 reasoning、token、tool 字段差异 |
| M2-09 | DONE | M2-07,M2-08 | 实现流式 tool call 聚合与方言归一化 | call ID/name/参数分片正确拼接，损坏输入有稳定错误 |
| M2-10 | DONE | M2-05,M2-09 | 定义 ReActRunner/TaskOutcome 接口 | 支持 candidate_complete/needs_plan/blocked/failed/cancelled |
| M2-11 | DONE | M2-10 | 实现单次 ReAct model iteration | fake LLM 可产生文本、tool calls 或候选结果，事件顺序稳定 |
| M2-12 | DONE | M2-03,M2-11 | 实现 Tool 执行与 Observation/Evidence 转换 | 参数校验失败不执行；成功/失败均形成结构化 Observation |
| M2-13 | DONE | M2-12 | 回灌 tool result 消息 | Domain call ID 经方言映射后与两种 API fixture 顺序一致 |
| M2-14 | DONE | M2-12 | 实现确定性 Progress Monitor | 重复签名、相同错误、无 Evidence 增量和高影响动作可检测 |
| M2-15 | DONE | M2-13,M2-14 | 实现 ReActRunner 循环与 CandidateTaskResult | fake LLM 可完成“一次工具+候选结果”，无 tool call 不直接完成 Run |
| M2-16 | DONE | M2-15 | 增加步骤、token、工具次数和 wall-clock 边界 | 达上限返回结构化 stop reason，不继续调用模型 |
| M2-17 | DONE | M2-15 | 定义 Verifier Port 与确定性 Verification | fake verifier 可表达 pass/fail/evidence gaps，不依赖 LLM 自评 |
| M2-18 | DONE | M2-17 | 定义 Reflector Port 与结构化 verdict | 同一模型输出 accept/retry/replan/ask_user/abort，拒绝自由格式结果 |
| M2-19 | DONE | M2-05,M2-16..M2-18 | 实现统一 Engine 的 Direct 单 root Task 主链 | candidate → verify → reflect → accept/retry；needs_plan 可结构化暂停 |
| M2-20 | DONE | M2-19 | 增加取消、partial step 和 Engine 事件 | 取消不误报完成，已执行 Evidence 可保留，事件可供 Renderer/测试消费 |
| M2-21 | DONE | M2-02 | 实现项目根路径对象 | 相对路径稳定解析，不依赖进程后续 chdir |
| M2-22 | DONE | M2-21 | 实现 `read_file` | offset/limit/大小限制和二进制错误测试通过 |
| M2-23 | DONE | M2-21 | 实现 `write_file` | 自动建父目录、大小上限和原子写策略测试通过 |
| M2-24 | DONE | M2-21 | 实现 `list_dir` | 排序稳定，隐藏/超量结果有明确规则 |
| M2-25 | DONE | M2-21 | 实现 `glob_files` | 忽略 VCS/构建目录，结果预算可配置 |
| M2-26 | DONE | M2-21 | 实现 Go fallback `grep_code` | 行号、上下文、结果上限测试通过 |
| M2-27 | DONE | M2-26 | 增加 ripgrep fast path | 有 `rg` 使用它，无 `rg` 自动回退且结果语义一致 |
| M2-28 | DONE | M2-21 | 实现 `execute_command` 基础执行 | cwd 固定、stdout/stderr 合并规则明确 |
| M2-29 | DONE | M2-28 | 增加命令 timeout/cancel | 超时和 Ctrl+C 可杀死子进程树或明确平台边界 |
| M2-30 | DONE | M2-29 | 增加命令输出预算 | 超量输出截断并标注 partial 与原始长度 |
| M2-31 | DONE | M2-22..M2-30 | 注册 MVP 工具集 | `amadeus tools list` 显示 6 个核心工具及副作用元数据 |
| M2-32 | DONE | M2-31 | 实现资源感知的有界并发 Tool Executor | 只并行只读/ParallelSafe 且资源不冲突的调用，结果保持原顺序 |
| M2-33 | DONE | M2-20,M2-32 | 增加 Direct Engine 临时项目端到端测试 | 单 root Task 可读、改、测，经 Verifier/Reflector 接受后完成 |

### M2 出口

Amadeus 使用统一 Engine 的单 root Task 完成“读取文件 → 修改文件 → 执行验证命令 → Verification → Reflection → 总结结果”，并能把复杂度升级需求表达为 `needs_plan`，不再把无 tool call 直接视为 Run 完成。

## 8. M3：首个可用 Coding Agent CLI

M3 的唯一目标是让用户可以在真实项目中启动 Amadeus，使用真实 Provider 完成“理解任务 → 读取/搜索代码 → 修改文件 → 执行测试 → Verification/Reflection → 输出结果”。首个可用版本只使用 Direct 单 root Task，不等待 Planner、Checkpoint、MCP、LSP 或 Multi-Agent。

跨 Run 的用户偏好和项目规范由显式 `AGENTS.md` 提供：`$AMADEUS_HOME/AGENTS.md` 为用户级，项目根与子目录中的 `AGENTS.md` 按目录树形成作用域链。`AMADEUS_HOME` 与目标项目根独立解析；默认项目为启动工作目录，允许通过 `--project` 显式指定。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M3-00 | TODO | M2-19 | 删除 `agent.mode` 配置与枚举 | config struct/default/patch/validation/example/explain/tests 不再出现 react/plan/team mode，预算与并发配置保持兼容 |
| M3-01 | TODO | M2-21 | 定义目标 Project 解析与 `--project` | 默认使用启动 cwd；显式路径稳定解析；不与 `AMADEUS_HOME` 混用 |
| M3-02 | TODO | M3-01 | 建立 Agent composition root | Provider、project.Root、events、tools、runner、verifier、reflector 可通过单一 bootstrap 装配 |
| M3-03 | TODO | M2-19 | 建立首版内置 Prompt 文件结构 | DirectEngine/ReAct/Verifier/Reflector 使用稳定、可测试的内置协议 |
| M3-04 | TODO | M3-03 | 实现 Prompt Repository 与 Assembler | 缺失层诊断、变量校验、来源清单和最终 hash 测试通过 |
| M3-05 | TODO | M2-21 | 定义 InstructionDocument/Scope/Resolver Port | source/path/scope/hash/content 不依赖 UI、LLM 或 Store |
| M3-06 | TODO | M3-05 | 加载用户级 `$AMADEUS_HOME/AGENTS.md` | 缺失时正常回退；UTF-8、大小预算和来源测试通过 |
| M3-07 | TODO | M3-05 | 发现项目根与目录级 `AGENTS.md` | 从 project.Root 到目标路径逐层发现，目录作用域稳定 |
| M3-08 | TODO | M3-06,M3-07 | 实现指令优先级与目标感知解析 | deeper project > project root > user；file/dir/command cwd 使用适用指令链 |
| M3-09 | TODO | M2-21,M3-07 | 完成 PathGuard 与 symlink 防护 | 绝对路径、`..` 和文件/目录 symlink 外逃均被拒绝 |
| M3-10 | TODO | M2-28 | 实现 CommandGuard 风险分类 | 危险命令在执行和审批之前被稳定识别或快速拒绝 |
| M3-11 | TODO | M3-10 | 定义 Approval Handler 与决策模型 | allow/deny/session/always、风险、原因和规范化参数可表达 |
| M3-12 | TODO | M3-11 | 实现 terminal approval | TTY 可交互批准/拒绝；无 TTY 按显式配置处理 |
| M3-13 | TODO | M3-09,M3-12 | 接入统一工具安全流水线 | PathGuard/CommandGuard 先于 approval；拒绝工具不执行 |
| M3-14 | TODO | M3-13 | 实现 JSONL Audit Sink 与脱敏 | allow/deny/error、耗时、来源可查询；key/header/大正文不落盘 |
| M3-15 | TODO | M3-04,M3-08 | 实现首版 Agent Context Envelope | 内置 Prompt、适用 Instructions、用户任务、工具定义和来源按稳定顺序组装 |
| M3-16 | TODO | M2-20 | 实现 Agent CLI 事件渲染 | 文本、工具开始/结果、Verification、Reflection、usage、取消和错误可读 |
| M3-17 | TODO | M3-01 | 实现根命令 Coding Agent 启动语义 | `amadeus` 进入交互模式；`amadeus "<task>"`、stdin 和 `--project` 可执行；不创建 `run` 子命令 |
| M3-18 | TODO | M3-02,M3-13,M3-15..M3-17 | 根命令装配真实 DirectEngine 主链 | 真实 Provider、MVP Registry、ToolExecutor、ReActRunner、质量门和 Renderer 全部接通 |
| M3-19 | TODO | M3-18 | 接入 Agent 预算配置 | max steps、tool calls、tokens、duration、parallel tools 有默认值并可解释 |
| M3-20 | TODO | M3-18 | 实现单次 Run 退出语义 | completed/partial/failed/cancelled/needs_plan 映射稳定退出码与总结 |
| M3-21 | TODO | M3-18 | 实现交互式连续任务循环 | 无 task 时进入输入循环；每次 Run 独立状态，`/exit` 和 EOF 可退出 |
| M3-22 | TODO | M3-20,M3-21 | 接入 Ctrl+C 与工具中断 | 首次中断取消当前 Run，已产生 Evidence 保留，终端可继续或退出 |
| M3-23 | TODO | M3-18..M3-22 | 增加命令级 Coding Agent E2E | 临时项目中通过 CLI 读文件、修复代码、运行测试并完成质量门 |
| M3-24 | TODO | M3-23 | 增加 Responses/Chat Provider mock E2E | 两种 API 模式均可通过 CLI 完成至少一次工具调用和最终回答 |
| M3-25 | TODO | M3-24 | 执行真实 Provider Coding Agent smoke test | 在隔离临时项目完成安全的读/改/测任务，日志和输出不泄露凭证 |
| M3-26 | TODO | M3-25 | 编写首个可用版本使用文档 | 从配置、`AGENTS.md`、项目选择、审批到 `amadeus`/`amadeus "<task>"` 的完整示例可复现 |

### M3 出口

用户可以在任意项目目录直接运行 `amadeus` 进入交互模式，或运行 `amadeus "<task>"` 执行一次性任务。Amadeus 使用真实 API Key，在项目围栏和审批策略下读取、搜索、修改代码并执行验证命令；用户级、项目级和目录级 `AGENTS.md` 生效，CLI 能展示工具、质量门、错误和取消状态。此出口即首个真正可用的 Amadeus Coding Agent。

## 9. M4：长上下文、会话持久化与恢复

M4 只增强单 Agent 的持续工作能力，不引入 Planner 或新的 Agent 模式。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M4-01 | TODO | M3-26 | 定义 Context Budget/Estimator Port | system/instructions/history/tool/resources/output reserve 可独立预算 |
| M4-02 | TODO | M3-21 | 定义 ConversationStore 并迁移交互历史 | 原始消息按 thread 有序保存，失败 Run 不污染正式历史 |
| M4-03 | TODO | M4-01,M4-02,M3-15 | 定义 ContextView/ContextBuilder Port | Goal/Task、Conversation、Prompt、Instructions、Evidence 和来源包络可组合 |
| M4-04 | TODO | M4-03 | 实现 Context 裁剪管线 | 优先裁剪旧工具大结果并保留来源、头尾和原始长度 |
| M4-05 | TODO | M4-03,M4-04 | 实现 Conversation Compactor | 摘要记录覆盖范围、hash、模型和时间，不隐藏在 Append 中 |
| M4-06 | TODO | M2-18 | 固定 Reflection 的 Run-local 生命周期 | verdict 可恢复，不自动生成或检索跨 Run Lesson |
| M4-07 | TODO | M2-05 | 定义 Run Checkpoint/Store Port | Graph、Task、Step、Evidence、budget 和 pending action 可快照 |
| M4-08 | TODO | M4-07 | 实现本地 Checkpoint Store | 原子保存、schema 版本、列出、读取和删除测试通过 |
| M4-09 | TODO | M3-08,M4-08 | 实现指令 hash 与恢复校验 | `AGENTS.md` 变化可检测，待执行副作用重新验证 |
| M4-10 | TODO | M4-08,M4-09 | 实现根命令 `amadeus --resume` | 从合法状态恢复，不重复已完成副作用或丢失 Evidence |
| M4-11 | TODO | M4-03..M4-10 | Engine 接入 Context、Conversation 与恢复 | 长 Run、失败、取消和恢复共享清晰来源与状态边界 |
| M4-12 | TODO | M4-11 | 增加长上下文与恢复 E2E | 压缩、取消、指令变化和 resume 后仍满足 acceptance criteria |

### M4 出口

单 Agent 可以持续多轮工作，长上下文会按来源和预算裁剪/压缩；Run 可 checkpoint/resume，恢复时不会重复已完成副作用，并能检测指令变化。

## 10. M5：Adaptive Planning 与 Replan

M5 在已经可用且可恢复的单 Agent 上增加 Plan-on-Demand。Plan 不是独立 Agent 模式，所有 Task 继续复用相同 ReActRunner、Verifier、Reflector、安全和 Provider 主链。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M5-01 | TODO | M4-12 | 定义 PlanningPolicy/StrategyDecision | 支持 auto、force plan、show plan、require review，并记录选择原因 |
| M5-02 | TODO | M5-01 | 实现确定性初始 Strategy Selector | `/plan`、高风险、显式依赖和多交付物进入 Planned，其余 Direct |
| M5-03 | TODO | M2-04 | 实现 ExecutionGraph DAG 校验 | 缺失依赖、重复 ID、环和非法 completed dependency 被拒绝 |
| M5-04 | TODO | M5-03,M4-03 | 实现 Planner 结构化输出 | 同一模型生成 objective/dependencies/criteria/budget，非法输出可重试/报错 |
| M5-05 | TODO | M5-04 | 实现 CLI plan review parser | approve/edit/cancel 行为测试通过 |
| M5-06 | TODO | M5-03 | 实现串行 Scheduler | ready task 顺序、失败传播和 blocked 状态测试通过 |
| M5-07 | TODO | M5-06,M3-18 | Scheduler 接入统一 ReActRunner | 每个 Task 独立 context/budget/attempts，共享安全和 Provider 主链 |
| M5-08 | TODO | M5-07 | 增加 ready Task 有界并发 | 仅无依赖且资源/副作用不冲突的 Task 并发，结果写回稳定 |
| M5-09 | TODO | M5-04,M5-07 | 实现 Replanner | 保留 completed Task、Evidence、当前 diff、已发生副作用和剩余预算 |
| M5-10 | TODO | M5-07,M5-09,M2-18 | 实现 Task Verification/Reflection/Replan 闭环 | accept/retry/replan/ask_user/abort 有状态机测试和次数上限 |
| M5-11 | TODO | M5-02,M5-09,M5-10 | 实现 Direct → Planned 无损升级 | discovery Step/Evidence/current diff/剩余预算进入新图，不重复动作 |
| M5-12 | TODO | M5-10 | 实现 Run Final Verification 与 Synthesis | 最终回答引用关键 Evidence，并区分完成、部分完成和失败 |
| M5-13 | TODO | M5-05,M5-12 | 实现 `/plan` 强制展示审核语义 | `/plan` 只改变本次 Run 的 PlanningPolicy，不创建另一套 Agent |
| M5-14 | TODO | M5-11..M5-13 | 增加 Adaptive Planning E2E | Direct 可保持单 Task 或动态升级，多 Task 可 replan 后完成 |

### M5 出口

默认任务继续优先 Direct；复杂任务可直接 Planned，执行中的 Direct 可基于 Evidence 无损升级；`/plan` 强制展示审核，失败计划可在预算内局部 Replan。

## 11. M6：Coding Workflow 扩展

M6 增加高价值编码辅助与外部扩展能力，但不阻塞 M3 可用基线或 M5 稳定单 Agent。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M6-01 | TODO | M3-13 | 定义 Snapshot Service 接口 | fake 实现可挂到安全工具流水线 |
| M6-02 | TODO | M6-01 | 实现 turn 前后快照 | 新增、修改、删除文件可识别 |
| M6-03 | TODO | M6-02 | 实现 `revert_turn` | 只恢复目标 turn 且产生审计记录 |
| M6-04 | TODO | M2-23 | 定义 LSP Client/Diagnostic | 与具体语言服务器解耦 |
| M6-05 | TODO | M6-04 | 实现 LSP process 生命周期 | initialize/shutdown/cancel 测试通过 |
| M6-06 | TODO | M6-05 | 实现写后诊断 hook | 写成功不因诊断失败回滚，诊断形成 Evidence |
| M6-07 | TODO | M3-04 | 定义 Skill 与 frontmatter parser | 无效元数据给出文件位置错误 |
| M6-08 | TODO | M6-07 | 实现 Skill 多级加载 | 内置 < 用户 < 项目优先级测试通过 |
| M6-09 | TODO | M6-08 | 实现 Skill 启用状态 Store | 状态可持久化和重载 |
| M6-10 | TODO | M6-09,M4-03 | 实现 Skill 索引与上下文缓冲 | 只注入启用且被选择的内容，并保留来源 |
| M6-11 | TODO | M3-26 | 定义 MCP 配置结构与合并 | 用户/项目配置和变量展开测试通过 |
| M6-12 | TODO | M6-11 | 实现 JSON-RPC message/client | request ID、response、error 和通知可处理 |
| M6-13 | TODO | M6-12 | 实现 stdio transport | fixture server 可 initialize/close |
| M6-14 | TODO | M6-13 | 实现 tools/list 与 tools/call | schema 清理、文本结果回灌和协议错误分类测试通过 |
| M6-15 | TODO | M6-14 | 实现 MCP 工具原子注册 | server 重连时无半更新状态 |
| M6-16 | TODO | M6-15 | 实现 streamable HTTP transport | mock server 连接、取消和重连测试通过 |
| M6-17 | TODO | M6-16,M4-03 | 实现 resources 与 `@mention` | list/read、解析、补全、来源包络和上下文预算测试通过 |
| M6-18 | TODO | M2-01 | 定义 SearchProvider/WebFetcher | fake provider 可独立测试 Tool |
| M6-19 | TODO | M6-18 | 实现网络策略 | scheme、私网、重定向、限流和大小限制测试通过 |
| M6-20 | TODO | M6-19 | 实现 `web_fetch` 与正文提取 | HTML fixture 输出稳定正文和来源 |
| M6-21 | TODO | M6-19 | 实现至少一个 `web_search` Provider | mock 响应格式化测试通过 |
| M6-22 | TODO | M6-03,M6-06,M6-10,M6-17,M6-20,M6-21 | 增加 Coding Workflow 扩展 E2E | Snapshot、诊断和外部工具均复用统一安全、事件和 Context 主链 |

## 12. M7：Multi-Agent 与高级入口

M7 全部属于可选增强，不阻塞单 Agent 可用版本或首个发布候选。Multi-Agent 只是 ExecutionGraph 上的 Task placement，SubAgent 必须复用相同 ReActRunner、Verifier、Reflector、安全和预算协议。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M7-01 | TODO | M5-14 | 定义 Agent Role 与结构化 Handoff | 角色、目标、预算、上下文范围和 acceptance criteria 可表达 |
| M7-02 | TODO | M7-01,M3-18 | 实现单 SubAgent TaskRunner | 独立 ContextView 并复用统一 Task 执行与质量门 |
| M7-03 | TODO | M7-02,M5-07 | 实现 Orchestrator Task placement | Scheduler 可委派 ready Task 并收回结构化结果/Evidence |
| M7-04 | TODO | M7-03 | 增加 Team 总预算和委派深度限制 | 超限可预测终止，不形成递归委派循环 |
| M7-05 | TODO | M7-04 | 增加多 SubAgent 有界并发 | 并发受配置限制，无 goroutine 泄漏，结果按 Task ID 写回 |
| M7-06 | TODO | M7-05 | 实现 `/team` placement 请求 | CLI 可请求委派，但仍运行同一个 Engine 与 ExecutionGraph |
| M7-07 | TODO | M7-06 | 增加 Multi-Agent E2E | 委派、失败回收、Evidence 汇总、总预算和最终验证通过 |
| M7-08 | TODO | M1-01 | 实现 `@image:` 解析 | file URL、绝对、相对路径测试通过 |
| M7-09 | TODO | M7-08 | 实现图片校验和处理 | 类型、大小、alpha 和缩放 fixture 通过 |
| M7-10 | TODO | M7-09 | 实现 SDK 图片请求转换 | Responses/兼容格式 fixture 通过 |
| M7-11 | TODO | M7-10 | 接入剪贴板图片输入 | 支持平台有明确范围，不支持时友好降级 |
| M7-12 | TODO | M3-13 | 定义 Browser Connector/Session | fake session 可测试工具行为 |
| M7-13 | TODO | M7-12 | 实现敏感页面策略 | 配置样例和拒绝规则测试通过 |
| M7-14 | TODO | M7-13 | 实现 Browser 工具最小集 | 连接、页面信息和截图结果可回灌 |
| M7-15 | TODO | M7-14 | 实现 Browser 会话复用 | 多次调用复用连接并正确关闭 |
| M7-16 | TODO | M3-21 | 抽取 CLI Session Controller | plain/inline/TUI/API 可共享输入与 Run 生命周期 |
| M7-17 | TODO | M7-16 | 实现 inline 状态事件 | 文本、工具、usage、计划和审批状态可展示 |
| M7-18 | TODO | M7-17 | 实现 slash palette/history/completion | 非 TTY 自动降级 plain |
| M7-19 | TODO | M7-16 | 选择并初始化 Go TUI 框架 | 最小窗口可启动/退出，不接 Agent |
| M7-20 | TODO | M7-19 | TUI 订阅 Runtime 事件 | 对话、计划、工具状态和输入栏可工作 |
| M7-21 | TODO | M7-20 | 实现 TUI Approval Handler | approve/deny/cancel 测试或手工验收通过 |
| M7-22 | TODO | M4-02,M3-16 | 定义 Runtime Thread/Event Store | 内存实现通过并发测试 |
| M7-23 | TODO | M7-22 | 实现 `POST /v1/threads` | 鉴权、创建和错误响应测试通过 |
| M7-24 | TODO | M7-23 | 实现 turns endpoint | 可启动回合并绑定取消 context |
| M7-25 | TODO | M7-24 | 实现 events stream | 断线、重连和游标语义明确 |
| M7-26 | TODO | M7-25 | 强制 Runtime API key | 未配置拒绝启动，错误不泄露密钥 |
| M7-27 | TODO | M4-07 | 定义 Durable Task/Store | 与 ConversationStore/CheckpointStore 边界明确，非法状态跳转被拒绝 |
| M7-28 | TODO | M7-27 | 实现 SQLite Task Store | enqueue/claim/complete/fail/cancel 测试通过 |
| M7-29 | TODO | M7-28 | 实现 worker pool | 并发数、关闭和崩溃恢复测试通过 |
| M7-30 | TODO | M7-29 | 实现 `/task` CLI 闭环 | add/list/cancel/log 可用 |

## 13. M8：兼容回归与发布

M8 以 M5 的稳定单 Agent + Adaptive Planning 为发布基线，不要求 M6/M7 的可选增强全部完成。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8-01 | TODO | M5-14 | 建立 Java 行为基线 fixture | 覆盖配置、Prompt、工具顺序和策略 |
| M8-02 | TODO | M8-01 | 建立 Go golden compatibility tests | 稳定行为差异均被解释或修复 |
| M8-03 | TODO | M8-02 | 实现 PaiCLI 配置导入器 | dry-run、脱敏预览和备份可用 |
| M8-04 | TODO | M8-02 | 完成 Linux amd64/arm64 构建 | 二进制可启动并通过 Coding Agent smoke test |
| M8-05 | TODO | M8-04 | 完成 macOS amd64/arm64 构建 | 二进制可启动并通过 Coding Agent smoke test |
| M8-06 | TODO | M8-05 | 评估 Windows 支持范围 | 支持则构建；不支持则记录命令/TTY 边界 |
| M8-07 | TODO | M8-04 | 增加安装与升级文档 | 新用户可从零配置并完成首个编码任务 |
| M8-08 | TODO | M8-07 | 增加迁移与差异文档 | PaiCLI 用户可理解配置和行为变化 |
| M8-09 | TODO | M8-08,M3-14 | 执行安全回归 | 路径、命令、审批、审计和凭证脱敏用例全部通过 |
| M8-10 | TODO | M8-09 | 执行性能基线 | 启动、长会话、工具并发、规划和恢复有记录 |
| M8-11 | TODO | M8-10 | 生成首个版本候选 | 版本、校验和、变更日志和已知问题齐全 |

## 14. 原项目到 Go 模块映射

| Java 模块/文件 | Go 目标 | 迁移阶段 |
|---|---|---|
| `cli/Main.java` | `cmd/amadeus` + `internal/app/*` + `internal/interface/cli` | M0-M3、M7 |
| `config/PaiCliConfig.java` | `internal/config` | M0 |
| `llm/*Client.java` | `internal/llm/openai` + capability config | M1 |
| `agent/Agent.java` | 作为行为参考映射到 `internal/agent/engine` + `internal/agent/react` | M2-M3 |
| `agent/PlanExecuteAgent.java` | 只迁移规划/审核/DAG 行为到统一 Engine 的 `internal/agent/plan`，不迁移独立 task loop | M5 |
| `agent/AgentOrchestrator.java`、`SubAgent.java` | 迁移为统一 ExecutionGraph 上的 Task placement 与 `internal/agent/team` | M7 |
| `tool/ToolRegistry.java` | `internal/tool` + `internal/tool/builtin/*` | M2-M7 |
| `policy/*`、`hitl/*` | `internal/policy` | M3 |
| `prompt/*`、`resources/prompts/*` | `internal/prompt` + `prompts/*` | M3 |
| `memory/*` | 仅迁移 Conversation 压缩与恢复行为到 `internal/conversation`、`internal/context` 和 Checkpoint Store；长期偏好改由 `internal/instruction` 加载 `AGENTS.md` | M3-M4 |
| `mcp/*` | `internal/mcp` | M6 |
| `skill/*` | `internal/skill` | M6 |
| `snapshot/*` | `internal/snapshot` | M6 |
| `lsp/*` | `internal/lsp` | M6 |
| `image/*` | `internal/image` | M7 |
| `browser/*` | `internal/browser` | M7 |
| `render/*`、`tui/*` | `internal/render` + `internal/interface/tui` | M1、M3、M7 |
| `runtime/api/*`、`runtime/task/*` | `internal/runtimeapi` + `internal/app/task` | M7 |

## 15. 优化记录

说明：`PROPOSED` 仅表示在代码梳理中发现的优化机会；只有 Go 实现落地且测试通过后，才能改为 `DONE`。记录必须写明原文件、原函数/区域、Go 目标和行为影响。

| OPT-ID | 状态 | 原文件与函数/区域 | 优化方案 | 预期收益 | 行为影响 |
|---|---|---|---|---|---|
| OPT-001 | PROPOSED | `config/PaiCliConfig.java`：`getApiKey`、`getModel`、`getBaseUrl`、`readFromDotEnv` | 启动时一次性加载并分层合并配置，保存字段来源 | 减少重复文件扫描，配置优先级可解释 | 默认值和优先级将被显式化 |
| OPT-002 | PROPOSED | `llm/AbstractOpenAiCompatibleClient.java` 与多个 Provider Client | 使用一个官方 SDK Adapter + Provider capability 配置 | 去除重复 HTTP/JSON/SSE 代码，便于升级 | Provider 特例改为配置或小 hook |
| OPT-003 | PROPOSED | `cli/Main.java`：`main` 及交互循环 | 拆为 composition root、session controller、command handlers | 降低大入口耦合，可复用 Runtime API | CLI 文案可变化，命令语义保持 |
| OPT-004 | PROPOSED | `tool/ToolRegistry.java`：`register*Tools`、`doExecuteTool`、`executeTools` | Registry、Executor、Policy pipeline、builtin tool 分包 | 单工具可测试，避免巨型类继续增长 | 工具名和 schema 保持兼容 |
| OPT-005 | PROPOSED | `agent/Agent.java`：`run`；`PlanExecuteAgent.java` task loop；`SubAgent.java` run loop | 建立 Plan-on-Demand 统一 Engine：ExecutionGraph + ReActRunner + Verifier + Reflector + Replan，SubAgent 仅做 Task placement | 所有执行共享状态、Evidence、终止、取消、安全、恢复和工具协议 | 内部取消三套 Agent 模式；`/plan`、`/team` 改为 PlanningPolicy/placement 请求 |
| OPT-006 | PROPOSED | `tool/ToolRegistry.java`：`executeTools` | 固定 worker pool + 工具副作用分类 + 顺序回灌 | 避免无界并发和非确定结果 | 可并发范围更保守、更安全 |
| OPT-007 | PROPOSED | `tool/ToolRegistry.java`：`executeCommand`、`readProcessOutput` | `exec.CommandContext`、输出预算、平台化进程组取消 | 取消更可靠，避免超大输出占内存 | 超量输出会明确截断 |
| OPT-008 | PROPOSED | `tool/ToolRegistry.java`：`write_file` 实现 | 原子临时文件写入、写前后路径复核、hook 分离 | 降低部分写入和路径竞态风险 | 文件权限继承策略需明确 |
| OPT-009 | PROPOSED | `agent/Agent.java`：直接 Renderer 调用和状态更新 | Runtime 发布结构化事件，Renderer 订阅 | CLI/TUI/API 共用同一行为 | 展示顺序由事件协议固定 |
| OPT-010 | PROPOSED | `memory/ConversationHistoryCompactor.java` 与图片历史清理逻辑 | 统一 Context Budget 管线并记录摘要范围/hash | 压缩可观测、可测试、可替换 tokenizer | 摘要元数据更丰富 |
| OPT-011 | PROPOSED | `mcp/McpServerManager.java` 与 ToolRegistry 动态注册 | 使用不可变工具快照原子替换 server 工具 | 重连期间不暴露半更新注册表 | 无用户可见变化 |
| OPT-012 | PROPOSED | 多处 `System.getenv`、`System.getProperty`、`System.out/err` | 通过 Config、Clock、FS、Event Sink 注入 | 测试无需污染全局环境 | 无用户可见变化 |
| OPT-013 | PROPOSED | `policy/CommandGuard.java` 字符串规则区域 | 保守 tokenizer 与规则对象，保留 raw command 审计摘要 | 降低大小写/空白/转义绕过 | 可能新增拒绝，需兼容用例 |
| OPT-014 | PROPOSED | `prompt/PromptAssembler.java` / `PromptRepository.java` | Prompt 层 schema 校验、最终 hash 和来源清单 | Prompt 漂移可追踪 | 配置错误更早暴露 |
| OPT-015 | PROPOSED | `runtime/task/DurableTaskManager.java` | Store 与 worker 解耦，使用显式状态机事务 | 恢复和并发 claim 更可靠 | 非法状态跳转改为明确错误 |
| OPT-016 | PROPOSED | WeKnora `internal/agent/engine.go`、`act.go`、`observe.go` | 吸收显式 State/Step、think-analyze-act-observe、空响应重试、卡死检测、partial step、tool timeout 和顺序回灌；按 Amadeus Domain 重写 | ReAct 工程边界和异常路径更完整 | 不引入其知识库耦合、Chat 协议类型、粗粒度并发或未完成 Reflection |
| OPT-017 | PROPOSED | WeKnora `ToolCall.Reflection` 预留字段与 PaiCLI 缺少 Reflection 闭环 | 新增 Evidence-first Verifier + Triggered Reflector + bounded Replan | 降低模型误报完成，允许执行中纠错和升级计划 | 增加受控模型调用；只保存结构化 verdict/lesson |
| OPT-018 | PROPOSED | PaiCLI `memory/MemoryManager.java` 同时管理 Conversation、Long-term Memory、Retriever、Compressor、TokenBudget、ContextProfile 和 project scope | 只迁移 ConversationStore、ContextBuilder/Compactor 和 CheckpointStore；用户/项目长期规则改为分层 `AGENTS.md`，不实现自动 MemoryManager | 消除隐藏压缩和偏好推断，指令可编辑、可审查、可版本控制 | 不提供自动 remember/recall；未来仅在真实需求验证后另立 ADR |

## 16. 决策与阻塞日志

| 日期 | 类型 | 内容 | 结果/后续 |
|---|---|---|---|
| 2026-07-29 | 决策 | 采用行为迁移，不进行 Java 文件一一翻译 | 架构按 Runtime 和 Adapter 重组 |
| 2026-07-29 | 决策 | OpenAI 官方 Go SDK 封装在基础设施层 | M1 固定具体版本并增加契约测试 |
| 2026-07-29 | 决策 | Responses 优先，Chat Completions 作为兼容模式 | Provider 配置必须显式 `api` |
| 2026-07-29 | 决策 | ReAct MVP 先于 Plan/Team/TUI | 2026-07-30 被统一 Engine 方案取代；仍保持 M2 先形成 Direct 可执行闭环，但 Domain 从一开始支持 ExecutionGraph/Reflection |
| 2026-07-30 | 决策 | 采用 Plan-on-Demand 统一 Agent Engine | Direct 使用单 root Task；复杂任务直接 Planned；执行中可基于 Evidence 无损升级 |
| 2026-07-30 | 决策 | ReAct 是 TaskRunner，不是独立 Agent 模式 | Plan task、SubAgent 和后台任务复用同一 ReActRunner、Verifier、Reflector 和工具链 |
| 2026-07-30 | 决策 | Reflection 初期使用同一模型并按检查点触发 | 无 tool call 只产生 Candidate Result；Verifier + Reflector 接受后才能完成 |
| 2026-07-30 | 决策 | `/plan` 只改变 PlanningPolicy | 强制生成、展示和审核 ExecutionGraph，不创建或切换另一套 Agent |
| 2026-07-30 | 决策 | Memory System 不采用全能 MemoryManager | 已被同日“显式 `AGENTS.md` 取代自动长期记忆”决策进一步收敛 |
| 2026-07-30 | 决策 | 显式 `AGENTS.md` 取代自动长期记忆 | 用户级位于 `$AMADEUS_HOME/AGENTS.md`；项目根和目录级按作用域覆盖；M3 移除 Durable Memory/MemoryRetriever/Lesson 持久化 |
| 2026-07-30 | 决策 | 删除 `agent.mode` 配置 | ReAct/Plan/Team 分别属于 TaskRunner、ExecutionGraph 和 placement；`/plan` 只改变本次 Run 的 PlanningPolicy |
| 2026-07-30 | 决策 | 首要目标调整为可用 Coding Agent | M3 直接交付真实 Provider + 根命令 Coding Agent + 工具 + `AGENTS.md` + 安全审批；不等待规划、恢复或扩展能力 |
| 2026-07-30 | 决策 | 根命令直接启动 Coding Agent | `amadeus` 进入交互模式，`amadeus "<task>"` 执行一次性任务；不创建独立 `run` 子命令 |
| 2026-07-30 | 决策 | Multi-Agent 后移为可选增强 | Adaptive Planning 与 Replan 先在单 Agent 上稳定；SubAgent placement 移至 M7，不阻塞首个可用版本或发布基线 |

## 17. 每次更新模板

完成或阻塞任务时追加一行：

```text
YYYY-MM-DD | TASK-ID | DONE/BLOCKED | 变更文件 | 测试命令与结果 | 备注
```

执行记录：

| 日期 | 任务 | 结果 | 变更/验证 | 备注 |
|---|---|---|---|---|
| 2026-07-29 | D0-01..D0-03 | DONE | 新增 `docs/design.md`、`docs/migration-progress.md` | 代码迁移从 M0-01 开始 |
| 2026-07-29 | M0-01 | DONE | 初始化 `go.mod`；更新 ADR-006 和进度状态 | `go version` 为 1.26.4；module 最低版本为 1.26.0 |
| 2026-07-29 | M0-02 | DONE | 新增 `cmd/amadeus` 根命令、`version` 子命令和单元测试 | `go test ./...`、`go vet ./...`、`go run ./cmd/amadeus version` 通过 |
| 2026-07-29 | M0-03 | DONE | 新增 `internal/buildinfo` 并接入 `version` 命令 | 单测、vet、默认输出及 ldflags 注入构建均通过 |
| 2026-07-29 | M0-04 | DONE | 新增 `internal/config` 强类型配置、默认值和单元测试 | `go test ./...`、`go vet ./...`、CLI 回归验证通过 |
| 2026-07-29 | M0-05 | DONE | 新增用户 YAML Loader、严格字段解码和字段级默认值 patch | 临时 HOME、缺失文件、部分覆盖、显式 false、未知字段测试及全量回归通过 |
| 2026-07-29 | M0-05 修订 | DONE | 默认路径改为 `<amadeus-root>/config.yaml`，Loader API 统一为 Amadeus 根目录语义 | 配置专项测试、全量测试和 vet 通过 |
| 2026-07-29 | M0-06 | DONE | 新增显式配置路径 Loader，并区分默认路径与显式路径的缺失行为 | 配置专项测试、全量测试、vet 和 `go mod tidy` 通过 |
| 2026-07-29 | M0-07 | DONE | 新增 YAML 节点级 `${ENV_VAR}` 展开和可注入环境查询器 | URL 嵌入、API key、model、duration、空值、缺失字段路径测试及全量回归通过 |
| 2026-07-29 | M0-08 | DONE | 新增 `AMADEUS_*` Provider 直接覆盖并接入 Loader 主链 | 默认配置、YAML 展开、Provider 切换、空值和优先级测试及全量回归通过 |
| 2026-07-29 | M0-09 | DONE | 新增根命令持久 flags 和通用 `config.Overrides` 应用链 | CLI 高于环境、显式空值、配置路径和无 `--api-key` 测试及全量回归通过 |
| 2026-07-29 | M0-10 | DONE | 新增结构化 ValidationError、Provider/URL/数值/枚举校验及自定义 Provider 运行默认值 | URL 边界、稳定字段路径、空凭证/model 和全量回归测试通过 |
| 2026-07-29 | M0-11 | DONE | 新增不可变配置脱敏与固定 `[REDACTED]` 标记 | 多 Provider、空值、原配置隔离和 YAML 序列化泄露测试及全量回归通过 |
| 2026-07-29 | M0-12 | DONE | 新增 `config check`、Amadeus 根目录解析和完整配置链入口 | 成功命令返回 0；无效 URL 返回字段错误与状态 1；专项/全量测试和 vet 通过 |
| 2026-07-29 | M0-12 修订 | DONE | 移除当前工作目录回退，根目录改为 `AMADEUS_HOME > 可执行文件目录` | 根目录专项测试、全量测试、vet 和临时二进制路径验收通过 |
| 2026-07-29 | M0-13 | DONE | 新增配置来源模型、YAML 字段检查、环境/CLI 来源覆盖和 `config explain` | 跨层来源、稳定输出、无效配置及 API key 脱敏测试和实际命令验收通过 |
| 2026-07-29 | M0-14 | DONE | 新增 `configs/amadeus.example.yaml` 和无环境变量回归测试 | 示例覆盖 Responses/Chat Completions，可无凭证通过 `config check` |
| 2026-07-29 | M0-15 | DONE | 新增 `internal/logging` 结构化日志运行时、级别过滤和敏感属性脱敏 | 专项/全量测试、vet 和 CLI 回归通过；Logger 保持显式依赖 |
| 2026-07-29 | M0-16 | DONE | 新增根目录 `Makefile` 和 `bin/` 忽略规则 | `make check`、构建后二进制 `version` 与示例配置 `config check` 均通过；M0 完成 |
| 2026-07-29 | M1-01 | DONE | 新增 `internal/llm` Message、Request、Response、Usage 和 FinishReason | 专项测试与 `make check` 通过；领域类型不依赖 OpenAI SDK |
| 2026-07-29 | M1-02 | DONE | 新增 LLM Client、Stream、StreamChunk、ModelInfo 和 Capabilities | fake client/stream 契约测试与 `make check` 通过；无 SDK 依赖 |
| 2026-07-29 | M1-03 | DONE | 引入 `openai-go/v3 v3.47.0` 并新增 OpenAI SDK client factory | mock transport 验证认证、base URL、timeout、retry；`go mod tidy` 与 `make check` 通过 |
| 2026-07-29 | M1-04 | DONE | 新增 Responses 纯文本请求转换和 HTTP JSON 契约测试 | system/developer/user/assistant、采样参数和 reasoning 隔离测试通过；`make check` 通过 |
| 2026-07-29 | M1-05 | DONE | 新增 Responses SDK stream 包装器、事件归一化和 SSE fixture | delta/completed/incomplete/usage/error/failed/decode error 与 `make check` 全部通过 |
| 2026-07-29 | M1-06 | DONE | 新增 Chat Completions 纯文本请求转换和兼容 JSON fixture | system/developer/user/assistant、`max_tokens` 与 reasoning 隔离测试通过；`make check` 通过 |
| 2026-07-29 | M1-07 | DONE | 新增 Chat Completions SDK stream 包装器和 SSE fixture | 文本 delta、finish reason、尾部 usage、无 usage 回退、损坏 JSON 与 `make check` 全部通过 |
| 2026-07-29 | M1-08 | DONE | 新增 Domain `ProviderError` 与 OpenAI 统一归一化入口 | HTTP/code、认证、限流、网络、取消、timeout、协议错误及 `make check` 全部通过 |
| 2026-07-29 | M1-09 | DONE | 新增 `internal/agent/event` 类型化事件、Sink Port 和并发安全 Memory Sink | 顺序、快照隔离、错误元数据、并发、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-10 | DONE | 新增 `internal/agent/runtime.Session` 单轮流式执行主链 | fake LLM 请求、响应聚合、事件顺序、Provider 错误、异常 EOF 与 `make check` 全部通过 |
| 2026-07-29 | M1-11 | DONE | 新增 `internal/render.PlainRenderer` Event Sink | stdout 文本流、完成换行、stderr 错误、部分输出、Writer 失败、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-12 | DONE | 新增 OpenAI Adapter、CLI `ChatLoop`、`amadeus chat` 装配和成功轮次历史 | Responses/Chat 路由基础、多轮请求、流式回答、Provider 错误继续、`/exit`、EOF、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-13 | DONE | 新增 per-turn context factory，并在 `amadeus chat` 请求期接入 `os.Interrupt` | 单轮取消、父 context 终止、错误输出、失败历史不提交、后续输入、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-14 | DONE | 新增 `amadeus chat` 双协议 Provider mock 命令级集成测试 | 本地 server 验证路径、认证、请求字段、SSE、usage、输出脱敏、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-15 | DONE | 使用真实 compatible Chat Completions Provider 执行最小流式 smoke test；根配置加入忽略规则 | 返回 `AMADEUS_SMOKE_OK`；request ID unavailable 已记录；随后 `make check` 通过，M1 完成 |
| 2026-07-30 | M2 计划修订 | DONE | OpenAI 具体类型重命名为 `Adapter`；补充 Provider Dialect/Capabilities 与 DeepSeek/Qwen/GLM 方言任务 | Domain Port、Adapter、SDK Client 命名边界明确；方言任务已纳入后续统一 Engine 重排 |
| 2026-07-30 | Agent Engine 重设计 | DONE | `design.md` 改为 Plan-on-Demand + ReActRunner + Verifier + Triggered Reflection + Replan；重排 M2-M4 | PaiCLI 仅作行为参考；吸收 WeKnora ReAct 工程优点；下一任务仍为 M2-01 |
| 2026-07-30 | Memory System 重设计 | DONE | 初步拆分 Conversation、Context、Memory、Lesson 与 Checkpoint | 后续已被“指令与记忆设计收敛”决策取代，仅保留 Conversation、Context、Run-local Reflection 与 Checkpoint |
| 2026-07-30 | 指令与记忆设计收敛 | DONE | 删除 Agent mode 设计；以用户/项目/目录级 `AGENTS.md` 取代自动长期记忆；重排 M3 与后续依赖 | 新增 M3-00、M3-12～M3-23 指令/Context 任务；Durable Memory、MemoryRetriever、自动 Lesson 退出当前路线图 |
| 2026-07-30 | 可用 Coding Agent 路线重排 | DONE | 将 CLI Agent composition、安全、Prompt 和 `AGENTS.md` 前移到 M3；Context/恢复置于 M4；Planning/Replan 置于 M5；Multi-Agent 后移到 M7 | M3 出口改为真实项目可直接执行 `amadeus`；M6/M7 不阻塞可用基线，M8 以稳定单 Agent 为发布基线 |
| 2026-07-30 | M2-01 | DONE | 新增 Provider/UI 无关的 Tool Spec/Call/Result/Tool/Executor Domain | Domain 契约测试、race 与 `make check` 通过 |
| 2026-07-30 | M2-02 | DONE | 新增并发安全 Tool Registry、稳定快照和重复/typed-nil 防护 | 并发注册/查找、隔离、race 与 `make check` 通过 |
| 2026-07-30 | M2-03 | DONE | 引入 JSON Schema Draft 2020-12 校验与受限 JSON repair | required/type/unknown/损坏 JSON、repair 后复验、race 与 `make check` 通过 |
| 2026-07-30 | M2-04 | DONE | 新增 Run/Goal/ExecutionGraph/Task/Step/Observation/Evidence/Budget Domain | direct/planned 图与 JSON 序列化测试、race 和 `make check` 通过 |
| 2026-07-30 | M2-05 | DONE | 新增 Run/Task 状态、结构化 StopReason 与合法转换 | candidate→verify→reflect→completed、非法跳转、终态、race 与 `make check` 通过 |
| 2026-07-30 | M2-06 | DONE | 新增显式 Provider Dialect 配置、来源链、Adapter Dialect Port 与 Capabilities | 五种方言选择、无 URL/名称/模型推断、无效组合、CLI/env/YAML、race 与 `make check` 通过 |
| 2026-07-30 | M2-07 | DONE | 扩展 LLM Tool Definition/Call/Result 消息并实现 Responses/Chat 标准请求转换 | 两种协议 tool definition、assistant call、tool result HTTP fixture 与 `make check` 通过 |
| 2026-07-30 | M2-08 | DONE | 新增 Chat Dialect hook 与 DeepSeek/Qwen/GLM reasoning/token/tool 请求差异 | Qwen `enable_thinking`、GLM `thinking`/reasoning history、DeepSeek 标准形状、race 与 `make check` 通过 |
| 2026-07-30 | M2-09 | DONE | 新增 Responses/Chat ToolCall 流聚合、JSON 完成校验和方言 reasoning delta 归一化 | ID/name/arguments 分片、排序、损坏 JSON、三家 reasoning fixture、race 与 `make check` 通过 |
| 2026-07-30 | M2-10 | DONE | 新增 ReActRunner/TaskRunner Port、TaskRunInput 与五类互斥 TaskOutcome | 输入状态、outcome 必填字段、stop reason、JSON 契约、race 与 `make check` 通过 |
| 2026-07-30 | M2-11 | DONE | 新增 `internal/agent/react.Iterator` 单次流式模型调用与结果归类 | 文本/reasoning/usage 事件、ToolCalls、Candidate、空回答/异常 finish、race 与 `make check` 通过 |
| 2026-07-30 | M2-12 | DONE | 新增 Registry/Schema/repair/Tool.Execute 到 Observation/Evidence 的执行边界 | repair、参数拒绝不执行、partial failure、未知工具、隔离、race 与 `make check` 通过 |
| 2026-07-30 | M2-13 | DONE | 新增结构化 ToolResult payload 与按 assistant call 顺序回灌 | 缺失/重复/额外 call ID 拒绝、Responses/Chat 顺序 fixture、race 与 `make check` 通过 |
| 2026-07-30 | M2-14 | DONE | 新增确定性 ProgressMonitor、canonical action/error 计数、Evidence 增量与 high-impact signals | 阈值、重置、JSON 顺序归一化、并发、race 与 `make check` 通过 |
| 2026-07-30 | M2-15 | DONE | 新增组合 Iterator/ToolExecutor/Replay/Progress 的 ReActRunner 循环与 CandidateTaskResult | 一次工具后候选、失败回灌、needs-plan、累计 usage、Candidate 非完成、race 与 `make check` 通过 |
| 2026-07-30 | M2-16 | DONE | 新增步骤、token、工具次数、wall-clock 预算检查与结构化 LimitReached | 到限前检查、批量工具原子拒绝、Provider 超额阻断、deadline 分类、race 与 `make check` 通过 |
| 2026-07-30 | M2-17 | DONE | 新增 Verifier Port、Verification/Check Domain 与确定性 Evidence/criterion 校验 | pass/fail/gap、required/optional、missing/unverified evidence、取消、race 与 `make check` 通过 |
| 2026-07-30 | M2-18 | DONE | 新增结构化 Reflector Port 与共享 llm.Client 的 JSON-only 实现 | 五类 verdict、严格解码、Verification 约束、无 CoT 字段、race 与 `make check` 通过 |
| 2026-07-30 | M2-19 | DONE | 新增 DirectEngine 单 root 状态机主链与 retry/replan/ask-user/failure 分支 | Candidate 质量门、attempts、反馈回灌、预算/Evidence 保留、race 与 `make check` 通过 |
| 2026-07-30 | M2-20 | DONE | 新增 Engine 生命周期/状态/质量门事件与取消 partial 增量保留 | 预取消、工具中断、cancelled Step、partial Result、Evidence、terminal event、race 与 `make check` 通过 |
| 2026-07-30 | M2-21 | DONE | 新增不可变 project.Root、固定绝对根与相对路径解析 | chdir 独立、根 symlink 稳定、绝对/`..` 外逃拒绝、race 与 `make check` 通过 |
| 2026-07-30 | M2-22 | DONE | 新增 UTF-8 `read_file`、line offset/limit 和文件大小预算 | partial/metadata、oversize、binary、escape、race 与 `make check` 通过 |
| 2026-07-30 | M2-23 | DONE | 新增原子 `write_file`、父目录创建、大小限制与权限保留 | create/replace/temp cleanup/escape/cancel、race 与 `make check` 通过 |
| 2026-07-30 | M2-24 | DONE | 新增稳定排序 `list_dir` 与 hidden/entry budget 语义 | file/dir/symlink 格式、hidden omission、partial、race 与 `make check` 通过 |
| 2026-07-30 | M2-25 | DONE | 新增 `glob_files`、`**` 匹配和默认 VCS/build ignore | hidden、recursive、limit、escape、stable paths、race 与 `make check` 通过 |
| 2026-07-30 | M2-26 | DONE | 新增纯 Go `grep_code` fallback 与统一结果格式 | literal/regex/case、line/context、limit、binary/oversize/ignore、race 与 `make check` 通过 |
| 2026-07-30 | M2-27 | DONE | 新增 ripgrep candidate fast path 并复用 Go 最终扫描器 | fast/fallback 语义一致、失败自动回退、输出预算、race 与 `make check` 通过 |
| 2026-07-30 | M2-28 | DONE | 新增固定 Root cwd 的 `execute_command` 与 combined stdout/stderr | success/nonzero、cwd/escape、exit metadata、race 与 `make check` 通过 |
| 2026-07-30 | M2-29 | DONE | 新增 per-call timeout、parent cancel 与 Unix 进程组终止 | deadline 分类、partial output、快速回收、非 Unix 边界、race 与 `make check` 通过 |
| 2026-07-30 | M2-30 | DONE | 新增命令 byte/line 双输出预算与原始总量统计 | stable truncation、partial、bytes/lines metadata、race 与 `make check` 通过 |
| 2026-07-30 | M2-31 | DONE | 新增六工具 MVP Registry、默认限制与 `amadeus tools list` | stable specs、side-effect/resource metadata、CLI/race 与 `make check` 通过 |
| 2026-07-30 | M2-32 | DONE | ReActRunner 接入资源感知有界并发执行器 | independent read 并行、resource conflict 串行、write barrier、原序结果、race 与 `make check` 通过 |
| 2026-07-30 | M2-33 | DONE | 新增真实临时 Go 项目的 Direct Engine 读/改/测质量闭环 | MVP Registry、实际 `go test`、criterion Evidence、Verifier/Reflector accept、terminal event、全仓 race 与 `make check` 通过 |
| 2026-07-30 | M2 完成审计 | DONE | 按 M2-01～M2-33 逐项核对实现、测试、文档与出口 | `make check`、`go test -race ./... -count=1`、`git diff --check` 全部通过；指令设计收敛后当前下一任务调整为 M3-00 |
