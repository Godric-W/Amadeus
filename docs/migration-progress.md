# Amadeus 迁移进度

> 创建日期：2026-07-29  
> 对应架构：`docs/design.md`  
> 源项目：`../paicli-main`  
> 总体状态：M1 OpenAI SDK 与纯文本会话完成，准备进入 M2

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
| M2 | ReAct 与核心工具 | TODO | 0% |
| M3 | 安全、Prompt、上下文、记忆 | TODO | 0% |
| M4 | Plan 与 Multi-Agent | TODO | 0% |
| M5 | MCP、Skill、Web 与 RAG | TODO | 0% |
| M6 | Snapshot、LSP、图片与 Browser | TODO | 0% |
| M7 | TUI、Runtime API 与后台任务 | TODO | 0% |
| M8 | 兼容回归与发布 | TODO | 0% |

## 3. 当前焦点

- 当前阶段：`M2`。
- 下一任务：`M2-01`，定义 Tool Spec/Result/Executor 接口。
- 当前阻塞：无。
- 最近完成：`M1-15`，完成真实 Chat Completions compatible Provider smoke test，M1 结束。

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

## 7. M2：ReAct 与核心工具

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M2-01 | TODO | M1-14 | 定义 Tool Spec/Result/Executor 接口 | 不依赖具体工具和 UI |
| M2-02 | TODO | M2-01 | 实现并发安全 Tool Registry | 注册、重复注册、查找和快照测试通过 |
| M2-03 | TODO | M2-02 | 实现 JSON Schema 参数校验 | 缺失、类型错误和未知字段行为明确 |
| M2-04 | TODO | M2-03 | 实现 SDK tool 定义转换 | Responses/Chat 两种格式 fixture 通过 |
| M2-05 | TODO | M2-04 | 实现流式 tool call 参数聚合 | 分片 JSON 可正确拼接，损坏输入有错误 |
| M2-06 | TODO | M2-05 | 实现统一 ReAct turn loop | fake LLM 可完成“一次工具+最终答案” |
| M2-07 | TODO | M2-06 | 增加最大步数终止 | 达上限返回 `max_steps`，不继续请求模型 |
| M2-08 | TODO | M2-07 | 增加重复工具调用保护 | 相同签名连续重复达到阈值后终止或提示模型 |
| M2-09 | TODO | M2-02 | 实现项目根路径对象 | 相对路径稳定解析，不依赖进程后续 chdir |
| M2-10 | TODO | M2-09 | 实现 `read_file` | offset/limit/大小限制和二进制错误测试通过 |
| M2-11 | TODO | M2-09 | 实现 `write_file` | 自动建父目录、大小上限和原子写策略测试通过 |
| M2-12 | TODO | M2-09 | 实现 `list_dir` | 排序稳定，隐藏/超量结果有明确规则 |
| M2-13 | TODO | M2-09 | 实现 `glob_files` | 忽略 VCS/构建目录，结果预算可配置 |
| M2-14 | TODO | M2-09 | 实现 Go fallback `grep_code` | 行号、上下文、结果上限测试通过 |
| M2-15 | TODO | M2-14 | 增加 ripgrep fast path | 有 `rg` 使用它，无 `rg` 自动回退且结果语义一致 |
| M2-16 | TODO | M2-09 | 实现 `execute_command` 基础执行 | cwd 固定、stdout/stderr 合并规则明确 |
| M2-17 | TODO | M2-16 | 增加命令 timeout/cancel | 超时和 Ctrl+C 可杀死子进程树或明确平台边界 |
| M2-18 | TODO | M2-17 | 增加命令输出预算 | 超量输出截断并标注 partial |
| M2-19 | TODO | M2-10..M2-18 | 注册 MVP 工具集 | `amadeus tools list` 显示 6 个核心工具 |
| M2-20 | TODO | M2-19 | 实现有界并发 Tool Executor | 只并行 ParallelSafe，结果按调用顺序返回 |
| M2-21 | TODO | M2-20 | 回灌 tool result 消息 | tool_call_id 和角色顺序与 fixture 一致 |
| M2-22 | TODO | M2-21 | 增加临时项目端到端测试 | fake 模型驱动读文件、写文件、跑命令后回答 |

### M2 出口

Amadeus 能在临时项目中完成“读取文件 → 修改文件 → 执行验证命令 → 总结结果”的完整 ReAct 回合。

## 8. M3：安全、Prompt、上下文与记忆

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M3-01 | TODO | M2-09 | 实现 PathGuard 清理与根目录检查 | 绝对外逃和 `..` 被拒绝 |
| M3-02 | TODO | M3-01 | 增加符号链接逃逸测试与防护 | 文件和目录 symlink 外逃均被拒绝 |
| M3-03 | TODO | M2-16 | 实现 CommandGuard 风险分类 | Java 基线危险样例均命中 |
| M3-04 | TODO | M3-03 | 定义 Approval Handler 接口 | allow/deny/always/session 结果可表达 |
| M3-05 | TODO | M3-04 | 实现 terminal approval | 无 TTY 时按配置拒绝或使用显式策略 |
| M3-06 | TODO | M3-05 | 接入工具安全流水线 | policy 先于 approval，拒绝工具不执行 |
| M3-07 | TODO | M3-06 | 实现 JSONL Audit Sink | allow/deny/error、耗时和 approver 可查询 |
| M3-08 | TODO | M3-07 | 实现审计脱敏 | key/header/大正文不进入日志 |
| M3-09 | TODO | M2-06 | 建立内置 Prompt 文件结构 | ReAct base/mode/approval 层可加载 |
| M3-10 | TODO | M3-09 | 实现用户/项目 Prompt 覆盖 | 项目覆盖优先且缺失文件回退内置 |
| M3-11 | TODO | M3-10 | 实现 Prompt Assembler | 层顺序、变量校验和 hash 测试通过 |
| M3-12 | TODO | M2-21 | 实现 token 预算接口 | 可注入精确 tokenizer 或估算器 |
| M3-13 | TODO | M3-12 | 实现历史图片 payload 清理 | 旧图片仅保留来源和尺寸文本 |
| M3-14 | TODO | M3-12 | 实现大工具结果裁剪 | 保留头尾、标记 partial 和原始长度 |
| M3-15 | TODO | M3-14 | 实现 Conversation Compactor | fake LLM 摘要后消息范围与 hash 可追踪 |
| M3-16 | TODO | M3-15 | 实现 session 内记忆 | 多轮消息保存、清空和查询测试通过 |
| M3-17 | TODO | M3-16 | 定义长期记忆 Store | 内存实现通过契约测试 |
| M3-18 | TODO | M3-17 | 实现 SQLite 长期记忆 | 增删查和并发访问测试通过 |
| M3-19 | TODO | M3-18 | 实现显式记忆工具 | 未经明确触发不自动保存敏感内容 |
| M3-20 | TODO | M3-19 | ReAct 接入 Prompt/预算/记忆 | 长会话达到预算后仍可继续完成回合 |

## 9. M4：Plan 与 Multi-Agent

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M4-01 | TODO | M3-20 | 定义 Task 与 ExecutionPlan | ID、依赖、状态、输入输出可序列化 |
| M4-02 | TODO | M4-01 | 校验计划 DAG | 缺失依赖、重复 ID 和环被拒绝 |
| M4-03 | TODO | M4-02 | 实现 Planner 结构化输出 | 非法模型输出可重试/报错 |
| M4-04 | TODO | M4-03 | 实现 CLI plan review parser | approve/edit/cancel 行为测试通过 |
| M4-05 | TODO | M4-04 | 实现串行 Plan Scheduler | 依赖顺序和失败传播测试通过 |
| M4-06 | TODO | M4-05 | 接入 task Runtime | 每个 task 有独立预算并共享安全策略 |
| M4-07 | TODO | M4-06 | 增加 ready task 有界并发 | 无依赖任务可并发，结果写回稳定 |
| M4-08 | TODO | M4-07 | 实现最终结果 synthesis | 汇总包含任务状态和关键产物 |
| M4-09 | TODO | M4-08 | 实现 `/plan` 模式切换 | 下一任务或持久模式语义明确 |
| M4-10 | TODO | M3-20 | 定义 Agent Role 与 Handoff | 角色、目标、预算和上下文范围结构化 |
| M4-11 | TODO | M4-10 | 实现单 SubAgent | 独立会话视图并复用 Runtime |
| M4-12 | TODO | M4-11 | 实现 Orchestrator 委派 | fake 模型可委派一个任务并收回结果 |
| M4-13 | TODO | M4-12 | 增加 Team 总预算和深度限制 | 超限可预测终止 |
| M4-14 | TODO | M4-13 | 增加多 SubAgent 有界并发 | 并发数受配置限制，无 goroutine 泄漏 |
| M4-15 | TODO | M4-14 | 实现 `/team` 模式切换 | CLI 可启动并展示角色执行事件 |

## 10. M5：MCP、Skill、Web 与 RAG

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M5-01 | TODO | M3-20 | 定义 MCP 配置结构与合并 | 用户/项目配置和变量展开测试通过 |
| M5-02 | TODO | M5-01 | 实现 JSON-RPC message/client | request ID、response、error 和通知可处理 |
| M5-03 | TODO | M5-02 | 实现 stdio transport | fixture server 可 initialize/close |
| M5-04 | TODO | M5-03 | 实现 tools/list | schema 经过清理后可注册 |
| M5-05 | TODO | M5-04 | 实现 tools/call | 文本结果可回灌，协议错误可分类 |
| M5-06 | TODO | M5-05 | 实现 MCP 动态工具原子替换 | server 重连时无半更新状态 |
| M5-07 | TODO | M5-06 | 实现 MCP 图片 content | base64、MIME 和来源元信息保留 |
| M5-08 | TODO | M5-06 | 实现 streamable HTTP transport | mock server 连接、取消和重连测试通过 |
| M5-09 | TODO | M5-08 | 实现 resources/list/read | 资源可作为虚拟工具和 Prompt 索引 |
| M5-10 | TODO | M5-09 | 实现 MCP `@mention` | 解析、补全和展开测试通过 |
| M5-11 | TODO | M3-11 | 定义 Skill 与 frontmatter parser | 无效元数据给出文件位置错误 |
| M5-12 | TODO | M5-11 | 实现 Skill 多级加载 | 内置<用户<项目优先级测试通过 |
| M5-13 | TODO | M5-12 | 实现 Skill 启用状态 Store | 状态可持久化和重载 |
| M5-14 | TODO | M5-13 | 实现 Skill 索引与上下文缓冲 | 只注入启用且被选择的内容 |
| M5-15 | TODO | M2-01 | 定义 SearchProvider/WebFetcher | fake provider 可独立测试 Tool |
| M5-16 | TODO | M5-15 | 实现网络策略 | scheme、私网、重定向和大小限制测试通过 |
| M5-17 | TODO | M5-16 | 实现 `web_fetch` 与正文提取 | HTML fixture 输出稳定正文 |
| M5-18 | TODO | M5-16 | 实现至少一个 `web_search` Provider | mock 响应格式化测试通过 |
| M5-19 | TODO | M3-12 | 定义 RAG chunk/embed/vector 接口 | 内存实现通过契约测试 |
| M5-20 | TODO | M5-19 | 实现代码分块与索引 | 增量更新、删除和忽略规则测试通过 |
| M5-21 | TODO | M5-20 | 实现 `search_code` | 返回来源、分数和建议读取范围 |

## 11. M6：Snapshot、LSP、图片与 Browser

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M6-01 | TODO | M3-06 | 定义 Snapshot Service 接口 | fake 实现可挂到工具流水线 |
| M6-02 | TODO | M6-01 | 实现 turn 前后快照 | 新增、修改、删除文件可识别 |
| M6-03 | TODO | M6-02 | 实现 `revert_turn` | 只恢复目标 turn 且产生审计记录 |
| M6-04 | TODO | M2-11 | 定义 LSP Client/Diagnostic | 与具体语言服务器解耦 |
| M6-05 | TODO | M6-04 | 实现 LSP process 生命周期 | initialize/shutdown/cancel 测试通过 |
| M6-06 | TODO | M6-05 | 实现写后诊断 hook | 写成功不因诊断失败回滚 |
| M6-07 | TODO | M1-01 | 实现 `@image:` 解析 | file URL、绝对、相对路径测试通过 |
| M6-08 | TODO | M6-07 | 实现图片校验和处理 | 类型、大小、alpha 和缩放 fixture 通过 |
| M6-09 | TODO | M6-08 | 实现 SDK 图片请求转换 | Responses/兼容格式 fixture 通过 |
| M6-10 | TODO | M6-09 | 接入剪贴板图片输入 | 支持平台有明确范围，不支持时友好降级 |
| M6-11 | TODO | M3-06 | 定义 Browser Connector/Session | fake session 可测试工具行为 |
| M6-12 | TODO | M6-11 | 实现敏感页面策略 | 配置样例和拒绝规则测试通过 |
| M6-13 | TODO | M6-12 | 实现 Browser 工具最小集 | 连接、页面信息和截图结果可回灌 |
| M6-14 | TODO | M6-13 | 实现 Browser 会话复用 | 多次调用复用连接并正确关闭 |

## 12. M7：TUI、Runtime API 与后台任务

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M7-01 | TODO | M3-20 | 抽取 CLI Session Controller | plain/inline renderer 可共享输入循环 |
| M7-02 | TODO | M7-01 | 实现 inline 状态事件 | 文本、工具、usage 和审批状态可展示 |
| M7-03 | TODO | M7-02 | 实现 slash palette/history/completion | 非 TTY 自动降级 plain |
| M7-04 | TODO | M7-01 | 选择并初始化 Go TUI 框架 | 最小窗口可启动/退出，不接 Agent |
| M7-05 | TODO | M7-04 | TUI 订阅 Runtime 事件 | 对话、状态和输入栏可工作 |
| M7-06 | TODO | M7-05 | 实现 TUI Approval Handler | approve/deny/cancel 测试或手工验收通过 |
| M7-07 | TODO | M3-20 | 定义 Runtime Thread/Event Store | 内存实现通过并发测试 |
| M7-08 | TODO | M7-07 | 实现 `POST /v1/threads` | 鉴权、创建和错误响应测试通过 |
| M7-09 | TODO | M7-08 | 实现 turns endpoint | 可启动回合并绑定取消 context |
| M7-10 | TODO | M7-09 | 实现 events stream | 断线、重连或游标语义明确 |
| M7-11 | TODO | M7-10 | 强制 Runtime API key | 未配置拒绝启动，错误不泄露密钥 |
| M7-12 | TODO | M3-18 | 定义 Durable Task/Store | 状态机非法跳转被拒绝 |
| M7-13 | TODO | M7-12 | 实现 SQLite Task Store | enqueue/claim/complete/fail/cancel 测试通过 |
| M7-14 | TODO | M7-13 | 实现 worker pool | 并发数、关闭和崩溃恢复测试通过 |
| M7-15 | TODO | M7-14 | 实现 `/task` CLI 闭环 | add/list/cancel/log 可用 |

## 13. M8：兼容回归与发布

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8-01 | TODO | M7-15 | 建立 Java 行为基线 fixture | 覆盖配置、Prompt、工具顺序和策略 |
| M8-02 | TODO | M8-01 | 建立 Go golden compatibility tests | 稳定行为差异均被解释或修复 |
| M8-03 | TODO | M8-02 | 实现 PaiCLI 配置导入器 | dry-run、脱敏预览和备份可用 |
| M8-04 | TODO | M8-02 | 完成 Linux amd64/arm64 构建 | 二进制可启动并通过 smoke test |
| M8-05 | TODO | M8-04 | 完成 macOS amd64/arm64 构建 | 二进制可启动并通过 smoke test |
| M8-06 | TODO | M8-05 | 评估 Windows 支持范围 | 支持则构建；不支持则记录命令/TTY 边界 |
| M8-07 | TODO | M8-04 | 增加安装与升级文档 | 新用户可从零配置并完成首轮对话 |
| M8-08 | TODO | M8-07 | 增加迁移与差异文档 | PaiCLI 用户可理解配置和行为变化 |
| M8-09 | TODO | M8-08 | 执行安全回归 | 路径、命令、审批、审计用例全部通过 |
| M8-10 | TODO | M8-09 | 执行性能基线 | 启动、空闲内存、长会话和并发工具有记录 |
| M8-11 | TODO | M8-10 | 生成首个版本候选 | 版本、校验和、变更日志和已知问题齐全 |

## 14. 原项目到 Go 模块映射

| Java 模块/文件 | Go 目标 | 迁移阶段 |
|---|---|---|
| `cli/Main.java` | `cmd/amadeus` + `internal/app/*` + `internal/interface/cli` | M0-M2、M7 |
| `config/PaiCliConfig.java` | `internal/config` | M0 |
| `llm/*Client.java` | `internal/llm/openai` + capability config | M1 |
| `agent/Agent.java` | `internal/agent/runtime` + `internal/agent/react` | M2-M3 |
| `agent/PlanExecuteAgent.java` | `internal/agent/plan` | M4 |
| `agent/AgentOrchestrator.java`、`SubAgent.java` | `internal/agent/team` | M4 |
| `tool/ToolRegistry.java` | `internal/tool` + `internal/tool/builtin/*` | M2-M6 |
| `policy/*`、`hitl/*` | `internal/policy` | M3 |
| `prompt/*`、`resources/prompts/*` | `internal/prompt` + `prompts/*` | M3 |
| `memory/*` | `internal/conversation` + `internal/memory` | M3 |
| `mcp/*` | `internal/mcp` | M5 |
| `skill/*` | `internal/skill` | M5 |
| `rag/*` | `internal/rag` | M5 |
| `snapshot/*` | `internal/snapshot` | M6 |
| `lsp/*` | `internal/lsp` | M6 |
| `image/*` | `internal/image` | M6 |
| `browser/*` | `internal/browser` | M6 |
| `render/*`、`tui/*` | `internal/render` + `internal/interface/tui` | M1、M7 |
| `runtime/api/*`、`runtime/task/*` | `internal/runtimeapi` + `internal/app/task` | M7 |

## 15. 优化记录

说明：`PROPOSED` 仅表示在代码梳理中发现的优化机会；只有 Go 实现落地且测试通过后，才能改为 `DONE`。记录必须写明原文件、原函数/区域、Go 目标和行为影响。

| OPT-ID | 状态 | 原文件与函数/区域 | 优化方案 | 预期收益 | 行为影响 |
|---|---|---|---|---|---|
| OPT-001 | PROPOSED | `config/PaiCliConfig.java`：`getApiKey`、`getModel`、`getBaseUrl`、`readFromDotEnv` | 启动时一次性加载并分层合并配置，保存字段来源 | 减少重复文件扫描，配置优先级可解释 | 默认值和优先级将被显式化 |
| OPT-002 | PROPOSED | `llm/AbstractOpenAiCompatibleClient.java` 与多个 Provider Client | 使用一个官方 SDK Adapter + Provider capability 配置 | 去除重复 HTTP/JSON/SSE 代码，便于升级 | Provider 特例改为配置或小 hook |
| OPT-003 | PROPOSED | `cli/Main.java`：`main` 及交互循环 | 拆为 composition root、session controller、command handlers | 降低大入口耦合，可复用 Runtime API | CLI 文案可变化，命令语义保持 |
| OPT-004 | PROPOSED | `tool/ToolRegistry.java`：`register*Tools`、`doExecuteTool`、`executeTools` | Registry、Executor、Policy pipeline、builtin tool 分包 | 单工具可测试，避免巨型类继续增长 | 工具名和 schema 保持兼容 |
| OPT-005 | PROPOSED | `agent/Agent.java`：`run`；`PlanExecuteAgent.java` task loop；`SubAgent.java` run loop | 抽取统一 turn/tool Runtime | 三种模式共享终止、取消、流和工具协议 | 减少模式间细微行为差异 |
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

## 16. 决策与阻塞日志

| 日期 | 类型 | 内容 | 结果/后续 |
|---|---|---|---|
| 2026-07-29 | 决策 | 采用行为迁移，不进行 Java 文件一一翻译 | 架构按 Runtime 和 Adapter 重组 |
| 2026-07-29 | 决策 | OpenAI 官方 Go SDK 封装在基础设施层 | M1 固定具体版本并增加契约测试 |
| 2026-07-29 | 决策 | Responses 优先，Chat Completions 作为兼容模式 | Provider 配置必须显式 `api` |
| 2026-07-29 | 决策 | ReAct MVP 先于 Plan/Team/TUI | 以 M0-M2 尽早形成可执行闭环 |

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
| 2026-07-29 | M1-12 | DONE | 新增 OpenAI Domain Client、CLI `ChatLoop`、`amadeus chat` 装配和成功轮次历史 | Responses/Chat 路由基础、多轮请求、流式回答、Provider 错误继续、`/exit`、EOF、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-13 | DONE | 新增 per-turn context factory，并在 `amadeus chat` 请求期接入 `os.Interrupt` | 单轮取消、父 context 终止、错误输出、失败历史不提交、后续输入、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-14 | DONE | 新增 `amadeus chat` 双协议 Provider mock 命令级集成测试 | 本地 server 验证路径、认证、请求字段、SSE、usage、输出脱敏、race 与 `make check` 全部通过 |
| 2026-07-29 | M1-15 | DONE | 使用真实 compatible Chat Completions Provider 执行最小流式 smoke test；根配置加入忽略规则 | 返回 `AMADEUS_SMOKE_OK`；request ID unavailable 已记录；随后 `make check` 通过，M1 完成 |
