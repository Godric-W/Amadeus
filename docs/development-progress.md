# Amadeus 开发进度

> 创建日期：2026-07-29
> 最近重排：2026-08-10
> 唯一目标架构：`docs/design.md`
> 当前状态：M8R、M8U、M8P、M8H、M9 与 M9V 已完成；当前进入 M10 正式发布收尾，M11 Multi-Agent 不阻塞正式发布

## 1. 文档规则

### 1.1 状态

- `TODO`：尚未开始。
- `DOING`：正在实施，同一时间只允许一个主任务处于该状态。
- `BLOCKED`：存在明确外部阻塞，必须记录原因和解除条件。
- `DONE`：代码、测试、文档和验收全部完成。
- `SKIPPED`：经架构决策不再实现，必须记录替代方案。
- `SUPERSEDED`：曾经完成，但已被当前架构取代，只保留历史事实。

### 1.2 完成定义

任务只有同时满足以下条件才能标记 `DONE`：

1. 代码或文档已进入仓库，且没有夹带无关修改。
2. 受影响包可以编译，针对性测试通过。
3. 涉及并发、取消、持久化或安全边界时，对应 race/E2E/失败路径通过。
4. 新增配置、命令、事件和用户可见行为已同步示例与文档。
5. 旧主链已删除或明确隔离，不能长期保留两个生产事实源。

### 1.3 执行约束

- 每次领取当前关键路径中编号最小且依赖已完成的任务。
- 一个任务只解决一个可验证问题；过大时先拆分。
- 默认使用 mock Provider、临时目录和临时 SQLite；真实 Provider smoke 单独执行。
- `docs/design.md` 是唯一架构事实源；历史实现和 Git 历史不能覆盖当前 ADR。
- `docs/architecture-audit.md` 已删除，不再维护第二份静态架构结论；架构守卫必须落在测试、搜索规则和本进度文档中。

## 2. 当前架构基线

后续任务必须共同满足以下语义：

1. `Session` 是可恢复对话，`Run` 是一次真实用户输入，`RolloutItem` 是 append-only canonical history。
2. SQLite 最终只保留 `schema_migrations/projects/sessions/runs/rollout_items` 五类核心表，不保留 Conversation 第二事实源。
3. `SessionRuntime` 管理 SessionHistory、Rollout append/flush、活动 Run 和 ExtensionRuntime 生命周期；`RunRuntime` 管理当前 Run 的取消、状态、资源所有权和统一收尾。
4. Reactor 是唯一 Agent 执行内核；默认 execute Run 使用 Plan-guided ReAct，`update_plan` 是软计划工具，不引入 DAG/Scheduler。
5. `/plan` 不接收任务文本，只切换当前 Composer 到 Plan Mode；后续普通输入创建只规划不实施的 `plan` Run，切回 `execute` 后根据 canonical rollout 实施计划。
6. ToolOutcome 是工具执行唯一结果事实；普通失败、拒绝、超时和中断进入结构化结果，不复制通用 Evidence 树。
7. Tool 主链统一为 `ToolCall → ToolRouter → ToolRegistry → ToolHandler → ToolOutput → ToolCallOutcome/ToolExecution`；不并存 Dispatcher、Executor、ExecutionGate 或通用 Prepared Call 第二执行链。
8. `ToolRouter` 是唯一 repair/parse/schema validation、可见性检查、并发调度和结果投影入口；`ToolHandler` 是领域执行入口，简单 Handler 直接执行，资源型 Handler 按需调用统一 Policy。
9. Tool 并发只保留 Run 级读写锁语义：Handler 通过 `SupportsParallelToolCalls() bool` 声明能力，`true` 获取 shared guard，`false` 获取 exclusive guard；不暴露 Shared/Exclusive 枚举，也不建立资源级路径锁。
10. 支持并行的 Tool 受 `max_parallel_tools` 限流；非并行 Tool 等待此前并行调用完成并阻止后续调用，结果仍按模型原始调用顺序回灌。
11. `ToolSpec` 只描述名称、说明和输入 Schema；目标架构删除通用 `Prepare/PreparedToolCall`、`TargetStrategy`、`PathGuard`、全工具 Authorizer 与伪通用 Pre/Post Hook。
12. 文件 Handler 直接调用 `FileSystemPolicy`；`apply_patch` 私有使用 `PreparedPatch`，`execute_command` 私有使用 `ExecRequest + 轻量进程 Runtime`；第二类进程 Tool 出现前不抽取具名通用 ToolOrchestrator，这些领域对象也不得扩展成所有 Tool 的公共流水线。
13. `apply_patch` 保留唯一匹配、无静默覆盖、全量 preflight、staging、identity revalidation、partial metadata 与 exact delta；文件变化由 Run 级 `RunDiffProjector` 投影，不实现 Snapshot/Revert、`revert_run` 或 ThreadRollback。
14. 中断 Run 补齐协议并追加 marker；下一次输入创建新 Run，不恢复旧调用栈，也不注入 Previous Work 第二摘要。
15. AGENTS.md、Context Compaction、Skill、MCP、Web、TUI 和未来 SubAgent 都复用同一 SessionRuntime/RunRuntime/Reactor/Tool 主链。
16. `Project.RootPath` 只持久化初始 CWD 对应的项目身份；RunContext.CWD 负责相对路径，WorkspaceRoots 是 `[CWD] + --add-dir` 的派生项目集合，不再使用 PrimaryRoot 参与权限判断。
17. 基础 PermissionProfile 只表达 ReadHost、WorkspaceRoots、TemporaryRoots、ReadOnlyRoots 和 DeniedRoots；`ReadHost=true`，WorkspaceRoots 与 TemporaryRoots 默认可写，ReadOnly/Denied 限制不可被普通授权覆盖。
18. RunPermissionStore 与 SessionPermissionStore 只保存规范化、去重后的 Additional Writable Roots；EffectivePermissionProfile 始终由 Base + Run + Session 组合，不存在 one-shot 权限。
19. 所有访问文件系统资源的 Tool 都先执行 Permission Check；缺少写权限时返回 `permission_required`，由模型调用 `request_permissions` 请求 `Allow for this run / Allow for this session / Deny`，批准后模型重新调用原 Tool。
20. 权限、操作审批与隔离分层：Permission Check 作用于所有资源型 Tool；SandboxRunner 只约束 Shell/子进程；结构化 Tool 权限通过后直接执行，不再做普通 Operation Approval。
21. Shell 只使用 `sandboxed` 与 `unsandboxed` 两种 IsolationMode。Linux Bubblewrap 在 Sandboxed 模式强制 EffectivePermissionProfile；Unsandboxed 模式无法强制声明范围，因此 Permission Check 后仍做完整 Command Operation Approval。
22. `execute_command` 只规范化显式 cwd 与 `requested_permissions.writable_roots`，不从 command 字符串提取 Root。SessionApprovalStore 只缓存 Unsandboxed Command 的 `Allow for session`，Key 精确包含 Shell、Command、canonical CWD、TTY 与 IsolationMode。
23. Run/Session Permission Store 与 SessionApprovalStore 都只属于活动 Runtime 内存，不写 SQLite、不随 `--resume` 恢复；MVP 不实现 DeniedGlobs、AdditionalReadableRoots、RunApprovalStore、NetworkPermissionStore 或 Run 专属临时根，网络默认允许。
24. `--add-dir` 是附加 Workspace Root，加载目标相关 AGENTS.md，但首版不加载其中的 MCP/Skill；Additional Writable Root 只扩大写权限，不获得工作区指令或扩展语义。
25. Prompt 是内部 Runtime 资产：模板位于 `internal/prompt/builtin/templates`，Bootstrap 负责分层装配并注入 Iterator，Reactor 不直接读取或兜底内置 Prompt；Codex Prompt 只选择性改写为 Amadeus 语义。
26. Token 语义向 Codex 收敛：Provider `max_output_tokens` 只控制单次采样；Context Profile 使用 95% 有效窗口与 90% 自动压缩阈值；稳定 Agent 配置不提供累计 input/output token 终止预算，usage 只用于事件、持久化、审计和未来可选 Rollout Budget。

## 3. 里程碑总览

| 里程碑 | 目标 | 状态 | 说明 |
|---|---|---:|---|
| D0 | 初始架构与计划 | DONE | 历史起点 |
| M0 | Go 工程与配置骨架 | DONE | 可构建 CLI 与配置链 |
| M1 | Provider Adapter 与文本/流式协议 | DONE | Responses/Chat Completions 基线 |
| M2 | 首版 Agent、工具与安全链 | DONE | 历史实现基线 |
| M3 | 首个可用 Coding Agent CLI | DONE | 根命令、AGENTS.md、工具调用 |
| M4 | 长上下文、会话与 Patch 基线 | DONE | 后续将迁移到 canonical runtime |
| M5/M5R | Adaptive/DAG/Plan-and-Execute 历史路径 | SUPERSEDED | 不进入目标架构 |
| M6/M6R | Coding Workflow 与 Reactor/TUI 重构 | DONE | 已提供可用产品基线，但仍含旧语义 |
| M7/M8T | Process、Web/MCP/Skill、Rich Inline TUI | DONE | 已交付能力，需接入新主链 |
| M8R | 目标架构收敛 | DONE | canonical Runtime、Reactor、Context、Tool、Extension 与 TUI 已统一并通过发布门 |
| M8S | Prepared Tool Pipeline 历史收敛 | SUPERSEDED | 曾统一 Prepare/PreparedToolCall；目标架构由 M8H 改为 Handler 私有领域准备对象 |
| M8U | Permission/Isolation/Approval Runtime 收敛 | DONE | Codex 风格 Root/Policy 分层、Run/Session Permission Store、Sandboxed/Unsandboxed Shell、Session Command Approval 与安全回归已完成 |
| M8P | Prompt Runtime 优化 | DONE | Prompt 资产内置化、Reactor 解耦、Coding Agent 文案重写与 Contract/E2E 已完成 |
| M8H | Tool Handler 主链与 Apply Patch 收敛 | DONE | Codex 风格 Handler/Registry/Router、私有 ExecRequest/PreparedPatch、typed exact delta 与 RunDiffProjector 已完成并通过发布门禁 |
| M9 | TUI 交互与 Slash Command 重构 | DONE | Composer、Slash/Selection、语义视觉与 Codex HistoryCell/TranscriptState/History insertion 最终结构全部完成 |
| M9V | TUI Visual Runtime 重构 | DONE | TerminalPalette、Motion、HistoryCell/ActiveHistoryCell、Tool History 和 Separator 视觉状态已完成 |
| M10 | 兼容回归与首个正式发布 | TODO | M8H 与 M9-26 已完成；只负责冻结、回归、构建、文档和发布 |
| M11 | 可选只读 Multi-Agent 与高级入口 | TODO | 不阻塞 M10 |

当前关键路径：

```text
M8R-A Canonical Persistence
→ M8R-B Session/Run Runtime
→ M8R-C Reactor/Context/Plan
→ M8R-D Tool Boundary/Concurrency/RunDiff
→ M8R-E Extension/TUI Integration
→ M8R-F Legacy Removal/Release Gate
→ M8S Prepared Tool Pipeline
→ M8U Workspace/Permission/Approval Runtime
→ M8P Prompt Runtime
→ M9 TUI/Slash Command
→ M9V TUI Visual Runtime 行为基线
→ M9 HistoryCell 结构收敛
→ M8H Tool Handler/Apply Patch Convergence
→ M10 Release
```

## 4. 当前焦点

- 当前阶段：`M8H` 已完成，进入 `M10` 兼容回归与首个正式发布阶段。
- 当前任务：无；下一项可领取任务为 `M10-01`。
- 当前阻塞：无。
- 最近实施进展：M10-00A 已完成 Token/Context 收敛；稳定 Agent 配置删除累计 input/output token 双预算，Provider `max_output_tokens` 只服务单次采样，Context Profile 使用 95% effective window 与 90% auto compact，历史和当前 Run 的 Tool Result 都参与安全投影，usage 继续完整累计。
- 最近 TUI 进展：修复实机黑白回归；ANSI16 继续使用 Codex 标准 cyan，Status Bar 按 model cyan/path green/branch magenta/usage green 分类，Lip Gloss profile 与 Palette 同步，Rich TUI 不再被继承的 `NO_COLOR` 静默剥色；Composer 文案已英文化；scrollback 提交不再附加尾部空行，最终回复到输入框恢复 Codex 同款两次换行。
- 发布约束：M10 完成前不得发布首个稳定版，也不得把 M11 Multi-Agent 接入默认主链。

### 4.1 交付顺序

| 顺序 | 范围 | 目的 |
|---:|---|---|
| 1 | M8R-01～06 | 完成 canonical persistence、Replacement History 与历史投影 |
| 2 | M8R-07～11 | 收敛 SessionRuntime、RunRuntime、RequestContext 与中断语义 |
| 3 | M8R-12～16 | 收敛唯一 Reactor、ContextManager、PlanState 与 Typed Events |
| 4 | M8R-17～25 | 交付历史 ToolOutcome、Run 级并发、安全边界与 Patch Diff 基线 |
| 5 | M8R-26～29 | 让 Skill、MCP、Web 和 TUI 接入统一 Runtime |
| 6 | M8R-30～34 | 删除旧链并完成全仓验收 |
| 7 | M8S-01～08 | 历史 Prepared Tool Call 收敛；其公共流水线由 M8H 取代 |
| 8 | M8U-01～11 | 收敛 Workspace、Permission、Approval、ExecPolicy 与 Sandbox 主链 |
| 8P | M8P-01～04 | Prompt 资产、装配、文案和 Contract/E2E 收敛 |
| 9 | M9-01～26 | 重构 Composer、Slash Popup、SelectionOverlay、Codex 对齐命令、语义视觉样式与 HistoryCell 结构 |
| 9V | M9V-01～10 | 建立 TerminalPalette、Motion、旧 TranscriptCell 行为基线、Tool History 与 Separator Visual Runtime |
| 8H | M8H-01～10 | 收敛 Handler/Registry/Router、权限接入、并发声明、PreparedPatch 与 RunDiffProjector |
| 10 | M10-00A～10 | 在 M8H 后先冻结 token/context 配置，再完成发布兼容、构建、文档和版本候选 |
| 11 | M11-01～07 | 发布后验证只读 Multi-Agent 的真实收益 |

## 5. 历史交付摘要

历史里程碑的逐任务记录保留在 Git 历史中；本文档只保留仍影响当前迁移的事实。

### 5.1 已交付能力

- Go CLI、根命令、配置覆盖链、Provider Adapter、Responses/Chat Completions、文本流和 Tool Call 流解析。
- 默认交互 TUI、Plain 模式、中文输入、原生 scrollback、Approval 选择器、Session 选择器和运行中输入队列。
- 结构化读取/搜索、`apply_patch`、`execute_command/write_stdin`、图片、Skill、MCP、Web Search/Fetch 和项目验证。
- AGENTS.md 分层加载、上下文预算/压缩基础、SQLite 会话基础、Run 中断和 Resume 基础。
- 历史 Path Guard、Command Guard、Approval、Audit、Tool Schema 校验和有界输出基线；M8U 将其迁移为 PermissionProfile、ExecPolicy、Session Store 与 Sandbox 分层。

### 5.2 必须替换的历史实现

- `conversation_sessions/conversation_messages/conversation_summaries` 六表模型与 Previous Work 摘要。
- `ConversationSession`、旧 Session Coordinator、旧 Run/Message 双事实源。
- 外层 Plan Controller、Plan Task、DAG/Scheduler/Replanner 和强制 Final Synthesizer 语义。
- Reactor 通用 Evidence/Verified 树和旧 Engine/Task 命名。
- 历史 `ParallelSafe + ResourceStrategy + per-resource Mutex` 并发模型。
- 全项目 Snapshot、`revert_run`、文件恢复和相关 TUI/Approval/测试主链。
- 旧 CommandGuard 的 Shell path-token 扫描、按 Tool 名称宽泛授权、Run-local 通用 GrantCache，以及“所有写入/命令一律审批”的固定分类。
- PrimaryRoot 同时承担项目身份、CWD、Workspace 和权限边界的旧模型，以及 `ProtectedRoots` 混合只读与 deny 的模糊语义。
- 单 Root Instruction Resolver；目标改为先选择覆盖 canonical target 的 Workspace Root，再加载该 Root 的 AGENTS.md 目录链。
- 任何继续依赖 `architecture-audit.md` 的静态完成结论。

## 6. M8R：目标架构收敛

### M8R-A：Canonical Persistence

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8R-01 | DONE | M7 | 定义 canonical SQLite schema | `schema_migrations/projects/sessions/runs/rollout_items` 约束、索引、sequence 和状态枚举固定 |
| M8R-02 | DONE | M8R-01 | 实现破坏性开发迁移 | 首个稳定版前事务删除旧 Conversation/Run/Message/Summary 表并建立 canonical schema；失败原子回滚；不维护未发布数据转换层 |
| M8R-03 | DONE | M8R-02 | 实现 RolloutStore append/replay | Session 全局 sequence、Run item sequence、批量 append、flush 和悬空 Tool Call 修复可测试 |
| M8R-04 | DONE | M8R-03 | 实现 Replacement History compaction item | `context_compaction + tail` 可重建有效历史；source hash 可验证；Tool Call/Result 协议组不可被拆分；不删除 canonical rollout |
| M8R-05 | DONE | M8R-03 | 删除 Conversation 第二事实源 | 生产代码不再写旧 Message/Summary Store；兼容只存在 migration fixture |

### M8R-B：Session 与 Run Runtime

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8R-06 | DONE | M8R-03 | 实现 `SessionHistory` replay/projection | completed、failed、interrupted、compaction 和 Tool 协议组按 canonical item 正确投影 |
| M8R-07 | DONE | M8R-05,M8R-06 | 实现 `SessionRuntime` | 拥有 history、append/flush、活动 Run 和 ExtensionRuntime 生命周期；切换 Session 重建并关闭旧扩展；不承担 Reactor/Tool/Provider 业务 |
| M8R-08 | DONE | M8R-07 | 实现 `RunContext` 与 `RequestContext` | 稳定 Run 事实和单次采样动态环境分离；每次 Think 重新冻结 canonical history、指令、Workspace、Tool/Skill/MCP revision；RequestContext 不持久化 |
| M8R-09 | DONE | M8R-08 | 实现 `RunRuntime/RunState` | 取消、状态、Usage、PlanState、Process Owner、Run 级 Tool Gate、逆序 cleanup 和 Finish Once 边界清晰；进程树随 Run 收尾 |
| M8R-10 | DONE | M8R-06 | 收敛中断语义 | 补齐悬空 Tool Result、追加 marker、flush；下一输入新建 Run，不恢复旧调用栈或 Previous Work |
| M8R-11 | DONE | M8R-10 | 删除 `ChatSession` | 管理入口、Plan Mode、Compactor 和 Agent 共用无状态 LLMRuntime 与 SessionRuntime |

### M8R-C：Reactor、Context 与 Plan

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8R-12 | DONE | M8R-06 | 定义统一 Reactor Iteration | Think→Analyze→Act→Observe 只消费 RequestView、模型输出和 ToolOutcome；完整 RunRuntime 接线由 M8R-09 负责 |
| M8R-13 | DONE | M8R-12 | 实现 ContextManager 主链 | Static Context、AGENTS.md、canonical Rollout Projection、Tool Projection、token budget、Replacement History compaction 与 per-Think RequestView 职责分离 |
| M8R-14 | DONE | M8R-12 | 实现 `update_plan` 与 PlanState | 计划是软状态和 `plan_update` RolloutItem，不创建 DAG、Plan Task 或 Scheduler |
| M8R-15 | DONE | M8R-14 | 重做 `/plan` Plan Mode | 只分析和输出计划；写 Tool、修改型 Shell 和实施副作用双层禁止 |
| M8R-16 | DONE | M8R-15 | 统一 Typed EventHub | SessionID/RunID/LLMCallID/Iteration 清晰；Renderer、TUI、Audit 和 API 消费同一事件；旧 Plan Task 文本抑制与 Inline task 状态已删除 |

### M8R-D：Tool Boundary、Concurrency 与 Run Diff

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8R-17 | DONE | M8R-12 | 以 ToolOutcome 替换通用 Evidence | succeeded/failed/denied/interrupted、partial、content、metadata 和 error 统一投影到 Model/UI/Audit/Rollout |
| M8R-18 | DONE | M8R-17 | 迁移 Tool Spec | 历史阶段将 `ParallelSafe/ResourceStrategy` 收敛为 `Concurrency(shared/exclusive)` 并引入非锁定 `TargetStrategy`；后者由 M8S 删除 |
| M8R-19 | DONE | M8R-18 | 实现 Run 级 ToolExecutionGate | Shared–Shared 可重叠；其余组合经 Exclusive 保序屏障；等待可取消；结果按原 Tool Call 顺序回灌；无第二层资源锁 |
| M8R-20 | DONE | M8R-19 | 分类内置与动态 Tool | 读取/搜索/图片/Web/可信只读 MCP Shared；Patch/Shell/write_stdin/未知 Tool Exclusive |
| M8R-21 | DONE | M8R-17 | 拆分 PathResolver/FileSystemPolicy | canonical target、read/write/deny、PrimaryRoot、`--add-dir`、ProtectedRules 和 symlink 语义统一；路径策略拒绝映射为独立 `path_denied` |
| M8R-22 | DONE | M8R-21 | 接入 SandboxRunner | Linux 优先真实 workspace-write；其他平台明确 degraded；降级原因通过 Typed Diagnostic Event 可见；CommandGuard 不冒充强 Sandbox |
| M8R-23 | DONE | M8R-17,M8R-21 | 让 `apply_patch` 输出 exact delta | add/update/delete/move、partial 和取消只报告实际提交变化；Move 目标已创建但源删除失败时记录真实 Add delta |
| M8R-24 | DONE | M8R-17 | 实现 `RunDiffTracker` | 只聚合 `apply_patch` exact delta；普通 Shell/MCP 不更新也不 invalidate；Patch delta 不可靠时内部失效；不建表、不恢复文件 |
| M8R-25 | DONE | M8R-24 | 删除 Snapshot/Revert 主链 | 删除模型 Tool、Runtime、Store、Approval、TUI、Prompt 和测试中的文件恢复语义 |

### M8R-E：Extension 与 TUI 接入

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8R-26 | DONE | M8R-07,M8R-13,M8R-17 | 收敛 Skill Runtime | user/project 覆盖、metadata-only index、按需正文/References、显式 `$skill` injection、load warning Event、`read_skill` 与项目 script 复用统一 Tool 边界；用户 Skill Root 受保护 |
| M8R-27 | DONE | M8R-07,M8R-13,M8R-17,M8R-20 | 收敛 MCPRuntime | Session 级连接、Binding/Catalog revision、lazy gateway、目标审批、只读并发声明、重连重校验和错误映射一致；过期采样 Binding 拒绝执行 |
| M8R-28 | DONE | M8R-13,M8R-17,M8R-20 | 收敛 Web Search/Fetch | DuckDuckGo/Tavily/SearXNG/Brave 与 Fetch 统一 ToolOutcome、Approval、timeout/retry、SSRF/redirect 限制和 Shared Gate |
| M8R-29 | DONE | M8R-16,M8R-24 | 重接 Rich Inline TUI | 只消费 Typed Events；Plan、Tool、Approval、RunDiff、Context、Interrupted/Failed 状态无第二事实源 |

### M8R-F：旧链删除与出口

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8R-30 | DONE | M8R-15,M8R-17,M8R-25,M8R-29 | 删除旧 Engine/Task/Evidence/Previous Work | 生产代码不再引用旧 Planner、DAG、Conversation、Snapshot、ResourceExecutor 或通用 Evidence 主链 |
| M8R-31 | DONE | M8R-30 | 建立架构守卫 | 测试禁止旧 Planner/Replanner/Scheduler、Conversation、Snapshot、ResourceExecutor、Evidence 等术语和包重新进入生产代码；migration fixture 例外 |
| M8R-32 | DONE | M8R-31 | 更新配置、示例和用户文档 | README、Provider/Web 配置、MCP 示例与 Skill 示例解释 Tool Concurrency、RunDiff、Session、`/plan`、Extension 和 Sandbox 行为 |
| M8R-33 | DONE | M8R-32 | 执行目标架构 E2E | 默认 execute、Plan Mode、Patch/Command、`--add-dir`、Shared/Exclusive、取消、Resume/Replacement History、显式 Skill、MCP/Web 与 Rich TUI 全链通过 |
| M8R-34 | DONE | M8R-33 | 完成 M8R 发布门 | `go test ./...`、全仓 race、`make check`、canonical migration fixture、Responses/Chat Provider mock E2E、架构守卫和 `git diff --check` 全绿 |

### M8R 出口

- 生产只有一个 Reactor、一个 Session/Rollout 事实源和一条 ToolOutcome 执行主链。
- 默认 execute Run 可按需更新软计划；`/plan` 只规划不实施。
- Shared/Exclusive Gate 取代资源级 Mutex；同资源读取不会被无意义串行化，所有写入与未知副作用仍保守独占。
- RunDiffTracker 取代 Snapshot/Revert；用户撤销依赖 Git 或新的正常 Patch。
- 中断、Resume、Compaction、MCP、Skill、Web 和 TUI 全部从 canonical rollout 与 Typed Events 投影。

## 7. M8S：Prepared Tool Pipeline 历史收敛（已被 M8H 取代）

M8S 记录已经交付过的历史实现：它曾删除 `TargetStrategy` 和重复 target 提取，并让通用 PreparedToolCall 成为授权与执行之间的 Run-local 事实。该实现事实继续保留，但公共 Prepared Pipeline 已被当前 `ToolHandler + 私有 PreparedPatch/ExecRequest` 设计取代，不再作为后续任务的目标架构。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8S-01 | DONE | M8R-34 | 定义 PreparedToolCall 领域模型 | 私有不可变字段、clone/read-only accessor、PreparedTarget/PreparedPayload、规范化 Call 关联和验证规则完成；不进入 rollout |
| M8S-02 | DONE | M8S-01 | 重构 Tool Prepare/Execute Contract | ToolExecutor 固定执行 Schema → Prepare → Authorize → Execute；targetless Tool 有显式 passthrough Prepare；Prepare 失败映射结构化 Outcome |
| M8S-03 | DONE | M8S-02 | 删除 TargetStrategy 全链 | Spec、Registry、Context Envelope、Provider schema、MVP/MCP/Web/Skill Tool、测试和文档不再暴露或读取 TargetStrategy/ArgumentPaths |
| M8S-04 | DONE | M8S-03 | 迁移结构化读取与图片 Tool | read/list/glob/grep/view_image 在 Prepare 中一次解析 canonical target；Execute 只消费 PreparedToolCall，不再次解析 raw path |
| M8S-05 | DONE | M8S-04 | 迁移 apply_patch 单次解析 | Prepare 一次 Parse Document、解析全部 source/destination target 并缓存执行输入；Approval/Audit/Execute/RunDiff 共用；副作用前只做 staleness/identity revalidation |
| M8S-06 | DONE | M8S-05 | 迁移 Command、Process 与动态 Tool | execute_command cwd、write_stdin owner、MCP binding、Web URL 和 Skill target 使用各自 Prepared Payload；展示由 Presentation Adapter 负责，不恢复泛化 target 字段 |
| M8S-07 | DONE | M8S-06 | 收敛 ToolAuthorizer 与授权审计 | 删除按 Tool 名称硬编码 path preflight/重复 Patch Parse；Policy、Approval、Grant Key 和 Audit 只消费 PreparedToolCall；audit fail-closed 保持 |
| M8S-08 | DONE | M8S-07 | 完成安全回归与架构守卫 | symlink/TOCTOU、跨 Root、Protected、Patch move/partial、degraded Command、MCP target、并发/取消、E2E/race 全绿；守卫禁止 TargetStrategy 与授权后重解析 raw arguments |

### M8S 出口

- 每个 Tool Call 只有一份规范化 Call、一份 PreparedToolCall 和一个 ToolOutcome；不存在 Authorizer/Tool 双重 target 事实。
- PathResolver/FileSystemPolicy 和领域 Parse 只在 Prepare 产生权威结果；副作用前复检只验证 prepared target 未失效，不重新决定权限。
- TargetStrategy/ArgumentPaths 从生产代码、Context、Provider schema 和测试基线删除。
- Shared/Exclusive Gate、Approval、Sandbox、RunDiffTracker、Typed Events 和 canonical rollout 行为保持兼容。

完成证据（2026-08-06）：`go test ./... -count=1`、`go test -race ./... -count=1`、`make check` 与 `git diff --check` 全部通过；Provider mock E2E 覆盖 Responses/Chat Completions，架构守卫禁止 `TargetStrategy`、`ArgumentPaths`、`preflightPaths` 及 ToolAuthorizer raw arguments 重解析。

## 8. M8U：Permission/Approval Runtime 收敛

M8U 在 M8S 的 PreparedToolCall 单一事实之上重构 Workspace、权限、隔离与审批主链。目标是采用 Codex 风格的职责分离但保持 Amadeus 可交付：`Project.RootPath` 只用于持久化身份，RunContext.CWD 与 `--add-dir` 派生 WorkspaceRoots；所有文件系统资源型 Tool 先做 Permission Check；结构化 Tool 通过后直接执行；Shell 再按 Sandboxed/Unsandboxed 分流。Linux Bubblewrap 强制 EffectivePermissionProfile，无法提供 OS Sandbox 时明确使用 Unsandboxed 并执行完整 Command Operation Approval。首版不引入 EnvironmentID、复杂 Policy DSL、Shell path-token 推断、权限 fingerprint、DeniedGlobs、AdditionalReadableRoots、RunApprovalStore、NetworkPermissionStore、Run 专属临时根、Docker 或 macOS/Windows 原生 Sandbox。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8U-01 | DONE | M8S-08 | 固定 Root 与 Isolation 领域模型 | `Project.RootPath` 只作为 SQLite 项目身份；RunContext.CWD 是相对路径基准；WorkspaceRoots 精确等于 `[CWD] + --add-dir`；TemporaryRoots 按平台派生；IsolationMode 只允许 `sandboxed/unsandboxed` |
| M8U-02 | DONE | M8U-01 | 实现基础 PermissionProfile | 固定 ReadHost、WorkspaceRoots、TemporaryRoots、ReadOnlyRoots 与 DeniedRoots 的规范化及优先级；ReadHost=true，ReadOnly 最多 read，Denied 最终 deny，普通 Grant 均不可覆盖 |
| M8U-03 | DONE | M8U-02 | 实现 Run/Session Permission Store | 两个内存 Store 只保存规范化、去重后的 Additional Writable Roots；Run Grant 随 Run 终态销毁，Session Grant 随 SessionRuntime 关闭或切换销毁，均不持久化 |
| M8U-04 | DONE | M8U-03 | 接入 Effective Permission Check | EffectivePermissionProfile 精确组合 Base + Run + Session；所有访问文件系统资源的 Tool 都在 Prepare 后执行 Permission Check，权限命中与拒绝不可被 Approval 缓存跳过 |
| M8U-05 | DONE | M8U-04 | 实现 `request_permissions` | 缺少写权限时返回 `permission_required`；模型调用 `request_permissions`，UI 提供 Allow for this run/Allow for this session/Deny；批准后模型重新调用原 Tool，Runtime 不复用旧 PreparedToolCall |
| M8U-06 | DONE | M8U-05 | 迁移多 Workspace Root 指令解析 | canonical target 只选择一个覆盖它的 Workspace Root；加载用户级与该 Root 的 AGENTS.md 目录链；`--add-dir` 加载 AGENTS.md，但不加载附加 Root 的 MCP/Skill |
| M8U-07 | DONE | M8U-06 | 重构 `execute_command` 权限声明 | 只规范化 Shell、canonical cwd 与 `requested_permissions.writable_roots`；不解析 command 内部 Root；ExecPolicy 只保留 `skip/needs_approval/forbidden` 与极小灾难性拒绝集 |
| M8U-08 | DONE | M8U-07 | 接入 Sandboxed/Unsandboxed 分流 | Linux Sandboxed Shell 进入 Bubblewrap 并强制 EffectivePermissionProfile；无可用 Sandbox 时明确使用 Unsandboxed，Permission Check 后仍执行完整 Command Operation Approval |
| M8U-09 | DONE | M8U-08 | 实现精确 SessionApprovalStore | 只缓存 Unsandboxed Command 的 Allow for session；CommandApprovalKey 包含规范 Shell、最小规范化 Command、canonical CWD、TTY 与 IsolationMode；Store 是 Session 内存集合，Allow once 不缓存且不存在 RunApprovalStore |
| M8U-10 | DONE | M8U-09 | 删除旧链并同步入口 | 删除 PrimaryRoot 安全语义、ProtectedRoots、DeniedGlobs、复杂 CommandGuard 风险树、通用 GrantCache、one-shot permission 和旧 Degraded 模式；保留轻量参数检查/灾难性拒绝集，并同步 CLI/TUI/config/docs、ToolOutcome 与 Audit |
| M8U-11 | DONE | M8U-10 | 完成安全 E2E 与架构守卫 | 覆盖 ReadHost、ReadOnly/Denied、Run/Session Grant 生命周期、request_permissions 重调用、Sandbox、Unsandboxed Approval 精确 Key、网络默认允许、resume 不恢复 Store 及 race/E2E |

完成证据（2026-08-07）：Responses 与 Chat Completions Provider mock E2E 已覆盖 `permission_required → request_permissions → Allow for this run → 模型重新调用原 apply_patch → 成功写入`；重复调用监控不会把授权后的合法重试误判为 stalled；CLI、Inline TUI 与 Fullscreen TUI 均覆盖 Permission Run/Session Scope；Audit 写入失败时 Permission Store 保持未授权；RunDiffTracker 可归因 WorkspaceRoots、TemporaryRoots 与 Additional Writable Roots 的 absolute delta。`go test ./... -count=1`、`go test -race ./... -count=1`、`make check`、`git diff --check` 和旧术语架构扫描全部通过。

### M8U 出口

- `Project.RootPath` 只持久化项目身份；RunContext.CWD 负责相对路径，WorkspaceRoots 精确由 `[CWD] + --add-dir` 派生。首版只有本地主机执行环境，不引入 EnvironmentID。
- PermissionProfile 只决定文件系统访问范围，ExecPolicy 只决定 Shell 是否跳过操作审批、需要审批或禁止，Sandbox 只负责 Sandboxed Shell 的真实执行约束，不存在职责重叠的第二套路径判断。
- WorkspaceRoots 默认可写并承载目标相关 AGENTS.md；Additional Writable Roots 只扩大写权限，不获得 Workspace 指令或 Extension 语义。项目级 MCP/Skill 只从 CWD 对应默认 Workspace Root 加载。
- ReadOnlyRoots 与 DeniedRoots 是独立且不可扩权的语义；AMADEUS_HOME 只保护配置、SQLite、审计、用户指令与用户扩展等敏感子 Root，不封禁整个根目录。MVP 不实现 DeniedGlobs。
- 所有文件系统资源型 Tool 先做 Permission Check；缺权时通过 `permission_required → request_permissions` 申请 Run/Session Grant。批准后模型重新调用原 Tool，Runtime 不恢复旧 PreparedToolCall；结构化 Tool 权限通过后直接执行。
- `execute_command` 只规范化显式 cwd 与 requested writable roots，不解析 command 字符串内部路径。Sandboxed 由 Bubblewrap 强制权限；Unsandboxed 在 Permission Check 后仍做完整 Operation Approval。
- SessionApprovalStore 只缓存 Unsandboxed `execute_command` 的 Allow for session；精确 Key 只包含 Shell、Command、canonical CWD、TTY 与 IsolationMode，Approval 命中不得跳过 Permission Check。
- RunPermissionStore、SessionPermissionStore 与 SessionApprovalStore 都只属于活动 Runtime 内存；`--resume` 跨进程恢复 canonical history，但不恢复历史权限或命令批准。
- Unix TemporaryRoots 为规范化去重后的 `/tmp + $TMPDIR`，Windows 为 `os.TempDir()`；不建立 `/tmp/amadeus/{run-id}` 或其他 Run 专属临时根。网络默认允许，不建立 NetworkPermissionStore。
- 旧 PrimaryRoot 安全语义、ProtectedRoots、DeniedGlobs、Shell path-token 扫描、复杂 CommandGuard 风险树、通用 GrantCache、one-shot permission、RunApprovalStore 和旧 Degraded 模式从生产代码及测试基线删除；只保留轻量参数检查与极小灾难性命令拒绝集。

### M8U-11 验收清单

- `ReadHost=true` 时可以读取项目外普通文件；DeniedRoots 的 Root 自身与后代均不可读写，ReadOnlyRoots 不可被 Run/Session Grant 绕过。
- Run Grant 只在当前 Run 生效；Session Grant 可跨当前活动 Session 的多个 Run 生效；Run/Session Permission Store 与 SessionApprovalStore 均不随 `--resume` 恢复。
- `request_permissions` 只提供 `Allow for this run / Allow for this session / Deny`；批准后由模型重新调用原 Tool，Runtime 不执行旧 PreparedToolCall。
- 结构化 Tool 在 Permission Check 通过后直接执行，不进入 Command Operation Approval。
- Sandboxed Shell 的越界写入由 Bubblewrap 拒绝；Unsandboxed Shell 即使 Permission Check 通过，也必须执行 `Allow once / Allow for session / Deny` 的完整命令审批。
- SessionApprovalStore 对 Shell、Command、canonical CWD、TTY 或 IsolationMode 任一不同的调用均不复用批准；Store 命中不能跳过新的 Permission Check。
- 网络默认允许；Web 的 SSRF/Redirect Guard 和 MCP 的 Server/Binding 边界保持生效，但生产代码中不存在 NetworkPermissionStore。
- 生产代码、配置、Provider Tool Schema 与测试基线不再引用 DeniedGlobs、AdditionalReadableRoots、one-shot permission、RunApprovalStore、旧 Degraded 模式或 Shell Root 猜测。

## 9. M8P：Prompt Runtime 优化

M8P 是独立的预发布能力里程碑，先于 M9 TUI/Slash Command 重构完成。它只优化 Prompt 资产归属、装配边界、Coding Agent 指令质量和行为 Contract，不修改 Session/Run/Reactor/Permission 的既有领域语义，也不提前实现 Multi-Agent、Goal、Realtime、Memory 或 Review Runtime。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8P-01 | DONE | M8U-11 | 内置化 Prompt 资产 | 删除顶层 `prompts/` package；迁移到 `internal/prompt/builtin/templates`；保留 ID、embed、来源 SHA-256、变量校验和稳定装配顺序 |
| M8P-02 | DONE | M8P-01 | 解耦 Reactor 与 Prompt 资产 | Bootstrap 装配 Agent Prompt 并显式注入 Iterator；Reactor 删除 `AgentSystem()` fallback；Execute/Plan/Runtime/Tool/Compaction 层按 RunMode 与 Exposure 组合 |
| M8P-03 | DONE | M8P-02 | 重写 Coding Agent Prompt | 选择性吸收 Codex 的任务持续执行、进度沟通、计划边界、验证纪律、Apply Patch、动态 Permission 和最终交付规则，并全部改写为 Amadeus Tool/Runtime 语义 |
| M8P-04 | DONE | M8P-03 | 完成 Prompt Contract 与 E2E | 覆盖模板/变量/层级、Plan Mode 无写指令、Tool Guidance 按 Exposure 注入、Permission 与 Policy 一致、Responses/Chat 等价语义和 Coding Agent smoke |

## 10. M8H：Tool Handler 主链与 Apply Patch 收敛

M8H 是首个正式发布前追加的 Tool 收敛阶段。它以 `docs/design.md` 的 Codex 风格 Handler 模型为唯一目标：生产主链统一为 `ToolCall → ToolRouter → ToolRegistry → ToolHandler → ToolOutput → ToolCallOutcome/ToolExecution`；Permission 规则集中在 `FileSystemPolicy`，资源型 Handler 按需调用；并发只由 Handler 声明 `SupportsParallelToolCalls()`，Router/Runtime 使用 Run 级读写锁实现 shared/exclusive 行为。M8H 不增加新能力，而是删除通用 Prepared/Dispatcher/Executor/Hook 叠层，并把 `apply_patch` 收敛为拥有私有 `PreparedPatch` 的可靠结构化写入 Handler。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M8H-01 | DONE | M8P-04 | 冻结 Tool Handler Contract 与迁移清单 | 明确 ToolCall/Payload/Invocation/Spec/Handler/Registry/Router/Output/Outcome/Execution 的唯一职责；列出旧 Tool/PreparedCall/Dispatcher/Executor/Gate/Hook 到目标模型的逐项迁移与删除清单 |
| M8H-02 | DONE | M8H-01 | 建立 ToolRegistry 与 ToolRouter 唯一入口 | Spec 与 Handler 绑定注册；Router 完成名称/可见性查找、Invocation 构造、Handler 调用和 fatal error 边界；生产代码不再绕过 Router 直接进入第二分发链 |
| M8H-03 | DONE | M8H-02 | 集中 Tool Call Normalizer 与 Schema Validate | Provider Adapter 只保证 Tool Call fragment 身份与完整性；Router 对 Function Payload 唯一一次执行有界 JSON repair、parse 和 JSON Schema validation，参数错误稳定转换为模型可见 ToolOutput/Outcome |
| M8H-04 | DONE | M8H-02 | 对齐 Handler 并发能力与 Run 级读写锁 | Handler 只暴露 `SupportsParallelToolCalls() bool`；`true` 调用在 `max_parallel_tools` 内获取 shared guard，`false` 获取 exclusive guard；等待可取消、结果按原调用顺序回灌；删除 Shared/Exclusive 枚举和资源级锁 |
| M8H-05 | DONE | M8H-03,M8H-04 | 迁移简单与文件 Handler | 简单 Handler 直接执行；文件 Handler 在领域参数可用后调用统一 FileSystemPolicy；Handler 不复制 Root/Grant 判断，Router 不建立全工具 Authorizer；Permission Required/Deny 进入结构化 ToolOutput/Outcome |
| M8H-06 | DONE | M8H-05 | 收敛 ExecuteCommandHandler 私有执行链 | `execute_command` 私有构造 ExecRequest 并进入轻量进程 Runtime；复用 EffectivePermissionProfile、Sandboxed/Unsandboxed、SessionApprovalStore、取消和进程收尾；第二类进程 Tool 出现前不抽取通用 ToolOrchestrator，也不扩展成普通 Tool 的公共 Pipeline |
| M8H-07 | DONE | M8H-05 | 实现 ApplyPatchHandler 私有 PreparedPatch | Patch Document 只解析一次；规范化 add/update/delete/move 与 source/destination target；唯一匹配、CRLF/上下文容错有界且每级唯一；禁止模糊歧义、路径逃逸和静默覆盖 |
| M8H-08 | DONE | M8H-07 | 强化 Patch Preflight、Staging 与 Commit | 所有 target 在副作用前完成 FileSystemPolicy、存在性、类型、冲突和目标占用检查；staging 后在 commit 前执行 identity/staleness revalidation；失败/取消只报告真实 partial metadata，不把未提交变化记为成功 |
| M8H-09 | DONE | M8H-08 | 接入 exact delta 与 RunDiffProjector | ApplyPatchHandler 对实际 add/update/delete/move 输出 exact delta；RunDiffProjector 只消费可信 Patch delta，Shell/write_stdin/MCP 不伪造归因；Model/UI/Audit/Rollout 使用受控投影且无内部归因警告 |
| M8H-10 | DONE | M8H-06,M8H-09 | 删除旧 Tool 链并完成发布门禁 | 删除通用 Prepare/PreparedToolCall、ToolDispatcher、ToolExecutor、ToolExecutionGate、TargetStrategy、PathGuard、全工具 Authorizer 与 Pre/Post Hook；完成 Handler/Permission/Parallel/Patch/Command/Provider mock E2E、取消/partial/race、全仓测试、架构扫描、`make check` 与 `git diff --check` |

### M8H 出口

- 生产代码只有一条 Tool 分发主链；ToolSpec 只描述暴露 Contract，ToolHandler 承担领域执行，ToolRouter 统一输入校验、调度和结果投影。
- Permission Check 规则只存在于 FileSystemPolicy；资源型 Handler 负责在正确时机调用，非资源 Tool 不经过伪通用权限层。
- Run 级读写锁行为保留，但公共语义只暴露 `SupportsParallelToolCalls()`；不再维护 Shared/Exclusive 枚举、TargetStrategy 或资源级锁。
- `apply_patch` 是首选结构化写入能力，私有 PreparedPatch 同时支撑唯一匹配、全量 preflight、staging、revalidation、partial metadata 与 exact delta，不建立通用 Prepared Call 框架。
- `execute_command` 的 ExecRequest/轻量进程 Runtime、`apply_patch` 的 PreparedPatch 都是 Handler 私有领域对象，不被抽象成所有 Tool 必须经过的公共层。
- M8H 已解除 Tool Contract 冻结前置条件；M10 现在可以冻结 Tool 暴露、Permission、并发、Patch 与 Tool 结果 Contract。

### M8H 完成证据（2026-08-08）

- Contract/Router：`internal/tool`、`internal/agent/react`、`internal/app/bootstrap` 定向测试通过；架构守卫确认生产 Handler 只由 ToolRouter 调用，Router 独占参数 Normalize/Schema Validate。
- Permission/Command：`internal/project`、`internal/policy`、`internal/tool/builtin` 覆盖 FileSystemPolicy、Permission Required/Deny、Sandboxed/Unsandboxed、SessionApprovalStore、取消与进程收尾。
- Patch/RunDiff：`internal/tool/patch`、`internal/tool/builtin`、`internal/diff` 覆盖 add/update/delete/move、CRLF、EOF、唯一匹配、preflight、identity/staleness、partial commit、exact Old/New Content、合法 UnifiedDiff 与 typed RunDiffProjector。
- Provider E2E：`TestCodingAgentProviderMockE2E` 与 `TestCoreToolsProviderMockE2E` 在 Responses、Chat Completions 两种协议下通过；malformed Tool arguments 延后到 Router 处理，且无内部 diff attribution warning。
- 全仓门禁：`go test ./... -count=1`、`go test -race ./... -count=1`、`make check` 全部通过；架构扫描只在守卫字符串中保留被删除符号；旧 prepared/path_guard/tool_authorizer 文件名扫描为空；`git diff --check` 通过。

## 11. M9：TUI 交互与 Slash Command 重构

M9 在正式发布冻结前完成交互主链重构。它不改变 Reactor、Prepared Tool、Permission、Sandbox 或 Provider 的领域语义，而是把现有 Bubble Tea Rich Inline TUI 收敛为 Codex 风格的 Composer、Slash Popup、SelectionOverlay 和命令分发结构。现有 `Amadeus Logo + >_` Braille 品牌头部保持不变；启动信息面板、状态展示、Slash Command 文案和交互优先直接复用 Codex 用户可见设计，只做产品名、`thread → session` 与真实能力差异所需替换。

M9-01～14 已于 2026-08-08 交付交互与命令基线；M9-15～20 完成 Palette、Composer、Selection、Tool/Status 层级和 Markdown/代码渲染的视觉一致性。2026-08-10 进一步对照 Codex 源码后确认，旧 `transcriptCell.Render() string + transcriptState.Cells/Committed` 只完成行为级视觉对齐，仍混合 Active Cell、正式历史和终端提交进度；M9-21～26 现已完成 `HistoryCell/ActiveHistoryCell/HistoryRenderMode/TranscriptState/InsertHistoryCell` 结构收敛，且没有建立第二套 Visual Runtime。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M9-01 | DONE | M8P-04 | 冻结 Codex 对齐交互 Contract | 明确 Composer、SlashCommandCatalog、SlashCommandPopup、SelectionOverlay、Dispatcher、运行中可用性和十一个命令的名称/说明/参数规则；`docs/design.md` 为唯一事实源 |
| M9-02 | DONE | M9-01 | 拆分 Rich TUI 组件职责 | 将输入草稿、Popup、Selection、Transcript、Activity、Status 与异步 Command Action 从单体 `fullscreenModel/application.go` 拆到职责清晰文件；UI 不拥有 Session/Agent 事实 |
| M9-03 | DONE | M9-02 | 实现共享 SlashCommandCatalog | Rich TUI 与 Plain fallback 共用名称、Codex 英文说明、展示顺序、参数规则和可用性；删除 `/help`、`/sessions`、`/tools` 的重复注册与分散 switch |
| M9-04 | DONE | M9-03 | 实现 SlashCommandPopup 与 Composer 状态机 | 输入 `/` 自动显示无边框两列列表；完全/前缀过滤、`↑/↓` 循环选择、Enter 执行、Tab 补全、Esc 保留草稿关闭、Unicode/IME/Backspace 和 Popup/History 优先级通过 |
| M9-05 | DONE | M9-04 | 统一 SelectionOverlay | Resume、Skills、Rename、Delete 与 Approval 共用无边框选择/输入框架；方向键、Enter、Esc、搜索、空状态、disabled reason 和异步结果行为一致 |
| M9-06 | DONE | M9-05 | 收敛 `/resume` 与实现 Codex `/clear` | `/resume` 迁入统一 Overlay；`/clear` 清空终端及瞬态 UI、解除当前前台 Session、进入新 Draft，旧 Session/rollout 保留且可恢复，Draft 不写空记录 |
| M9-07 | DONE | M9-06 | 重做 `/plan` 模式切换 | `/plan` 不接收任务；切换 Composer CollaborationMode，Footer/Status 显示 Plan，后续输入创建 `plan` Run；切换 Session/重启恢复 execute，Plan Mode 写能力继续 fail closed |
| M9-08 | DONE | M9-07 | 实现 `/status`、`/copy` 与 `/mcp` | `/status` 展示 Session 配置与 token/permission/extension 摘要；`/copy` 复制最后一条原始 assistant Markdown；`/mcp` 和 `/mcp verbose` 使用现有 Manager 懒加载状态/工具详情 |
| M9-09 | DONE | M9-08 | 实现 `/skills` 浏览与管理 | 提供 `List skills`、`Enable/Disable Skills`、搜索和状态切换；启用状态写入权威用户/项目配置并使 ExtensionRuntime revision 正确失效，不只保存在 TUI |
| M9-10 | DONE | M9-09 | 增加 Session Rename/Delete Store API | Memory/SQLite Store、Coordinator 与 SessionRuntime 支持规范化重命名和事务删除；从属 Run/Rollout 一致清理，活动 Run 与跨 Project 操作拒绝，Draft Title 可延迟到首次创建 |
| M9-11 | DONE | M9-10 | 实现 `/rename` 与 `/delete` | Rename Prompt 预填当前标题并同步状态/Resume 列表；Delete 使用 Codex 确认文案，永久删除后退出，Draft 无持久记录时不给数据库制造副作用 |
| M9-12 | DONE | M9-11 | 实现手动 `/compact` | 主动生成语义化会话摘要并 append `context_compaction`，校验 covered sequence/source hash/replacement history；不 UPDATE/DELETE 原 rollout，失败保持旧历史可用 |
| M9-13 | DONE | M9-12 | 对齐启动面板、状态和用户文案 | 保留现有 Amadeus Logo 与 Braille `>_`；启动面板只显示产品/version、model、directory；命令说明、确认、空状态和结果提示优先复用 Codex 文本并集中管理 |
| M9-14 | DONE | M9-13 | 完成 TUI/Slash 发布门禁 | 覆盖 Popup/Overlay 快照、中文输入、运行中命令 gating、Plain `/` Catalog、Resume/Clear/Plan/Skill/MCP/Compact/Rename/Delete/Copy/Exit E2E、无空 Session、race、全仓测试和 `make check` |
| M9-15 | DONE | M9V-10 | 冻结 Codex 语义渲染补强 Contract | `design.md` 与 `tui-visual-contract.md` 明确 default/strong/dim/accent/selection/status/Markdown 规则；确认不直接复制 Rust 类型、不维护固定 Dashboard Palette |
| M9-16 | DONE | M9-15 | 收敛 TerminalPalette 语义层 | `palette.go` 已实现 deep/light accent、ANSI256 最近色、ANSI16 标准 cyan/green/magenta、NoColor 降级、strong/muted/selection/border/separator 和 bold success/failure；Program 启动同步 Lip Gloss profile |
| M9-17 | DONE | M9-16 | 对齐 Composer 与统一 Selection UI | Composer 与 Overlay 输入使用 `›`；Focused strong、Blurred dim；Slash/Resume/Approval/Skills 选中整行使用 accent + bold，普通名称 default、说明 dim，黄色 selected palette 已删除 |
| M9-18 | DONE | M9-17 | 对齐 Tool History 与 Status Bar 层级 | `Ran/Explored/Updated Plan` 使用 default + bold；命令 default、输出 dim、Read/List/Search accent；Status 按 Codex fallback 使用 model cyan、path/usage green、branch/mode magenta，高 context 使用 yellow/red |
| M9-19 | DONE | M9-18 | 完整实现 Codex Markdown 语义与代码高亮 | `markdown.go` 集中配置 heading/emphasis/strong/link/blockquote/list/inline code；Go/Bash/JSON fenced block 使用独立深浅 Chroma 主题与 terminal16m/256/16 formatter，无背景；未知语言与 NoColor 确定性降级 |
| M9-20 | DONE | M9-19 | 完成语义视觉矩阵与发布门禁 | `semantic_visual_test.go` 与既有视觉/交互测试覆盖深浅 accent、Composer enabled/disabled、Selection、Tool、Status、Markdown/多语言代码、ANSI16/NoColor；全仓、race、`make check`、主屏 smoke 与 diff check 通过 |
| M9-21 | DONE | M9-20 | 冻结 Codex HistoryCell Contract 与迁移清单 | `design.md`/视觉契约已明确 HistoryCell、ActiveHistoryCell、HistoryRenderMode、TranscriptState、HistoryCells、PendingHistoryCells、HasEmittedHistoryLines 和不复制的 Ratatui 专属能力；旧 transcriptCell/Kind/Render/Committed 迁移清单已落地 |
| M9-22 | DONE | M9-21 | 引入 HistoryCell 与具体 Message Cell | `history_cell.go` 定义 Rich/Raw DisplayLines Contract；UserMessageCell、AgentMessageCell、Plain/Notice/Diagnostic/Error Cell 已拆分；通用 text cell、HistoryCellKind 和 kind render switch 已删除 |
| M9-23 | DONE | M9-22 | 分离 TranscriptState 与正式 History 所有权 | TranscriptState 只保留 ActiveHistoryCell、ActiveCellRevision、LastAgentMarkdown 和 Turn flags；fullscreenModel 持有 historyCells/pendingHistoryCells/hasEmittedHistoryLines；Cells/Committed 混合状态已删除 |
| M9-24 | DONE | M9-23 | 实现统一 History insertion 主链 | application.go 已实现 insertHistoryCell、displayLinesForHistoryInsert、flushHistory；独立 Cell 插入结构化空 StyledLine，stream continuation 保持相邻；`"\n" + output` 和 flushTranscript 兼容入口已删除 |
| M9-25 | DONE | M9-24 | 迁移 Active Tool、Plan 与 Separator Cell | `history_cell_tools.go` 的 ToolHistoryCell/ExecCell/ExploreCell/WebSearchCell 与 PlanUpdateCell/FinalMessageSeparator 均使用 DisplayLines/RawLines；ActiveHistoryCell 按 Event 原位更新并以完成 Cell 提交 |
| M9-26 | DONE | M9-25 | 完成 Rich/Raw、结构测试与发布门禁 | history/application/activity/semantic/visual 测试覆盖 Rich/Raw、跨批次间距、stream continuation、revision、Agent/Tool/Separator、Working/Composer、Unicode/NoColor/Plain/main-screen；全仓、race、make check、build 与 diff check 通过，架构守卫确认无旧主链 |

M9 完成证据：M9-01～20 的 Catalog、Popup、Selection、Session、Compaction、Palette、Composer、Tool/Status 和 Markdown 测试继续通过；M9-21～26 新增 `history_cell.go`、`history_cell_tools.go` 与 HistoryCell 架构守卫，覆盖 Rich/Raw、跨批次 spacing、stream continuation、ActiveCellRevision 和正式 History ownership。2026-08-10 执行 `go test ./... -count=1`、`go test -race ./... -count=1`、`make check`、`make build` 与 `git diff --check` 均成功。

## 12. M9V：TUI Visual Runtime 重构

M9V 位于已完成的交互命令重构与正式发布冻结之间。M9 解决 Composer、Slash Popup、SelectionOverlay、Session Command 和交互状态机；M9V 专门解决用户实际看到的 Transcript、Tool History、Working、颜色、分隔线与间距。它保留 Go/Bubble Tea/Lip Gloss、Amadeus Logo、主屏 scrollback、原生鼠标滚动和现有 Slash 行为，不修改 Reactor、Session、Permission、ToolExecutor 或 Provider 的领域语义。

M9V 以当前 `../codex-main/codex-rs/tui` 源码和快照为视觉事实源，移植行为与状态模型而不是直接复制 Rust 类型。旧 `fullscreenEntry{kind, content}`、frame 阶梯颜色、固定 pastel 状态栏、按 `IterationCompleted` 批量打印 Tool 和每轮无条件 separator 已从生产代码删除。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M9V-01 | DONE | M9-14 | 冻结 Codex Visual Contract | `docs/tui-visual-contract.md` 固定 Working、Palette、Cells、Tool History、Separator、Layout 与终端矩阵边界 |
| M9V-02 | DONE | M9V-01 | 实现 TerminalPalette 与统一 Style | `palette.go` 检测 True Color/ANSI256/ANSI16/No Color 与明暗背景，集中派生语义 Style 和 Markdown accent |
| M9V-03 | DONE | M9V-02 | 实现 Motion 与 Codex Shimmer | `motion.go` 使用可注入 Clock、32ms redraw、2s/10-padding/half-width-5 余弦光带、600ms marker fallback 与 Reduced Motion |
| M9V-04 | DONE | M9V-03 | 引入 TranscriptCell 行为基线与统一 Layout | 历史 `transcript.go` 曾建立 StyledLine、旧 TranscriptCell/ActiveCell、TranscriptState 和 commit spacing 的首版行为；M9-21～26 已将其替换为 `history_cell.go`/`history_cell_tools.go` 的最终结构 |
| M9V-05 | DONE | M9V-04 | 实现 ExecCell 活动生命周期 | Tool Started/Completed 原位更新；Running/Ran/You ran、命令/output 上限、omission、success/failure、partial 与 duration 均有测试 |
| M9V-06 | DONE | M9V-05 | 实现 Explore 与 WebSearch Cell | Exploring/Explored、Read 去重、Read/List/Search accent、Searching/Searched 与 CallID/sequence 顺序已实现 |
| M9V-07 | DONE | M9V-06 | 实现 Transcript Separator 状态 | HadWorkActivity/NeedsFinalMessageSeparator 驱动内容边界；Iteration 不再排版；短 Run dim rule、长 Run 零填充 Worked for 已测试 |
| M9V-08 | DONE | M9V-07 | 接入 Bottom Pane 与既有交互 | Active Tool、流式 Assistant、Working、Approval/Selection、Composer、Status、排队输入、Ctrl+T、Logo 与 main-screen scrollback 已统一布局 |
| M9V-09 | DONE | M9V-08 | 增加视觉与终端矩阵测试 | 固定 Clock/快照覆盖颜色能力、Reduced Motion、中文/窄终端、Tool、Separator、Approval、Resume、Plain 与控制序列 |
| M9V-10 | DONE | M9V-09 | 删除旧视觉链并完成发布门禁 | 旧 frame/pastel/Iteration/kind-content 链已删除；全仓测试、race、`make check`、主屏 smoke 与 `git diff --check` 通过 |

M9V 出口要求：用户在普通问候、只读探索、命令成功/失败、长输出、并行 Tool、长 Run、中断、Approval、Session 切换和 No Color/Plain 场景下，都能得到与 Codex 同一视觉语义且符合 Amadeus 能力边界的稳定输出。不能仅以 Slash Command 可用或字符串包含 `Working/Ran` 作为视觉验收。

M9V 出口证据：`palette_test.go`、`motion_test.go`、`transcript_test.go`、`activity_test.go`、`application_test.go` 与 `visual_snapshot_test.go` 覆盖视觉状态和交互矩阵；2026-08-08 执行 `go test ./... -count=1`、`go test -race ./... -count=1`、`make check` 和 `git diff --check` 均通过。

## 13. M10：兼容回归与首个正式发布

M10 只在 M9V-10、M9-26、M10-00 与 M8H-10 全部完成后继续。M10 不再进行 Agent Engine、Prompt、Permission、Persistence、Tool、TUI 或 Slash Command 主链重构，只冻结已经完成的能力、执行发布级回归、构建各平台产物并准备正式版本。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M10-00 | DONE | M9V-10 | 对齐 Codex Diff 语义 | 历史实现已让 Patch exact delta 成为唯一归因来源；M8H-09 负责将其迁移到 RunDiffProjector 与最终 Handler 主链 |
| M10-00A | DONE | M10-00,M8H-10 | 收敛 Token 与 Context 配置 | 已删除 Agent 累计 input/output token 配置与 Reactor 终止条件；Provider `max_output_tokens` 只控制单次请求；Context Profile 改为 95% effective window、90% auto compact；历史 Tool Result 安全投影与 usage 记录通过测试 |
| M10-01 | TODO | M10-00A | 冻结 CLI、配置、TUI 和 Tool 暴露基线 | 根命令、`--plain`、Slash Catalog、Resume/Clear、Plan Mode、Provider、Exposure、Handler Tool Contract、Workspace/Permission/Approval、Visual Runtime 和错误语义固定 |
| M10-02 | TODO | M10-01 | 冻结稳定版迁移策略 | 首个稳定版 schema 成为数据兼容起点；后续目标版本有保留数据的明确迁移，不支持版本给出可恢复错误且不静默丢数据 |
| M10-03 | TODO | M10-02 | 执行安全回归 | Path、Sandbox、Command、Approval、Audit、MCP/Web、凭证脱敏和非 TTY fail-closed 通过 |
| M10-04 | TODO | M10-03 | 执行可靠性与性能基线 | 长 Session、Compaction、parallel Handler shared guard、non-parallel Handler exclusive guard、取消和内存/token 指标有记录 |
| M10-05 | TODO | M10-04 | 完成 Linux amd64/arm64 构建 | 二进制启动和 Coding Agent smoke 通过 |
| M10-06 | TODO | M10-05 | 完成 macOS amd64/arm64 构建 | 二进制启动和 Coding Agent smoke 通过 |
| M10-07 | TODO | M10-06 | 评估 Windows 支持范围 | 支持则构建；否则明确 TTY、Process、Sandbox 和安装限制 |
| M10-08 | TODO | M10-05 | 完成安装、配置和故障排查文档 | 新用户可从零完成配置、编码任务、中断、Resume/Clear、Slash Popup 和 Plan Mode |
| M10-09 | TODO | M10-08 | 生成版本说明与迁移说明 | 明确历史架构差异、数据库迁移、已知限制和 M11 状态 |
| M10-10 | TODO | M10-09 | 生成首个正式版本候选 | version、commit、build time、checksums、changelog、已知问题和 smoke 结果齐全 |

## 14. M11：可选 Multi-Agent 与高级入口

M11 不阻塞 M10。首版只验证最多两个只读 SubAgent，不建立 Team Engine、共享 DAG、可写并发 Workspace 或递归委派。

| ID | 状态 | 依赖 | 最小任务 | 验收标准 |
|---|---|---|---|---|
| M11-01 | TODO | M10-10 | 定义 DelegatedTask/Result | objective、最小 context、budget、summary、usage、stop reason 和 TaskID 可表达 |
| M11-02 | TODO | M11-01 | 实现只读 SubAgent Runtime | 独立内存 Context/Reactor；共享只读 WorkspaceRoots 与 Instructions 基线；没有写入、Shell、Approval 和继续委派能力 |
| M11-03 | TODO | M11-02 | 增加 Agent Tool API | `spawn_agent/send_input/wait_agent/close_agent` 由主 Reactor 调用，最多两个、depth=1 |
| M11-04 | TODO | M11-03 | 实现主 Agent 所有权边界 | 主 Agent 独占正式 Run、最终回答、写 Tool、Shell、RunDiffProjector 和 Approval |
| M11-05 | TODO | M11-04 | 实现取消与失败收敛 | 主 Run 取消传播；子 Agent 失败形成结构化结果，不自动递归创建替代 Agent |
| M11-06 | TODO | M11-05 | 增加 Multi-Agent E2E | 两个独立只读调查并行、主 Agent 汇总后修改/验证、单子失败和取消通过 |
| M11-07 | TODO | M11-06 | 执行收益审计 | 记录延迟、token、完成质量和复杂度；收益不足则保持可选实验能力 |

高级入口只有在 M11-07 后按真实需求单独立项：Browser、TUI 文件树/高级 Diff、Runtime API 和后台 Job。每项必须先补充独立设计与验收，不预先组成新的大里程碑。

## 15. 架构验收矩阵

| 领域 | 必须成立的事实 | 主要验收 |
|---|---|---|
| Persistence | canonical rollout 是唯一历史事实源 | migration、append/replay、crash recovery |
| Session/Run | SessionRuntime 与 RunRuntime 职责不重叠 | 生命周期、取消、Finish Once、race |
| Reactor | 只有 Think→Analyze→Act→Observe 一条循环 | 默认 execute、Plan Mode、旧 Engine guard |
| Context | RequestContext 动态构建，compaction append-only；95% effective window、90% auto compact 与 Provider 单次输出语义分离 | token、历史 Tool Result 投影、Replacement History、Resume |
| Prompt | 内置模板只由 Bootstrap 分层装配，稳定规则与动态事实分离 | asset migration、变量/层级 contract、RunMode/Exposure、Permission developer message、双 API E2E |
| Tool | ToolRouter/Registry/Handler 是唯一调用主链，ToolOutput 是内容事实，ToolCallOutcome 是生命周期事实 | normalizer/schema、handler dispatch、failure/denied/partial/interrupted E2E、旧链架构守卫 |
| Concurrency | Handler 只声明 `SupportsParallelToolCalls()`，Run 级读写锁实现 shared/exclusive 行为且无第二层资源锁 | parallel overlap、non-parallel barrier、cancel、order、race |
| Safety | PermissionProfile、Run/Session Permission Store、Isolation、Session Command Approval、ExecPolicy 与 Audit 职责不重叠且不可绕过 | ReadHost、symlink、ReadOnly/Denied、request_permissions 重调用、Run/Session 生命周期、Linux Sandbox、Unsandboxed 精确 Command Key、非 TTY、resume 不恢复 Store |
| Diff | RunDiffProjector 只投影可信 Patch exact delta；WorkspaceDiff 独立按需读取 Git 状态 | add/update/delete/move、partial、staging/revalidation、Shell/MCP ignore、无 transcript 警告 |
| Extensions | MCP/Skill/Web 复用统一 Runtime | binding、approval、timeout、event projection |
| TUI | UI 只消费事件，不拥有 Agent/Session 事实；Composer/Slash/Selection 与 Visual Runtime 分层；目标 HistoryCell/ActiveHistoryCell、TerminalPalette、Motion 和 Separator 状态不依赖 Reactor Iteration 排版 | scrollback、Unicode、popup/filter/navigation、approval、resume/clear、plan mode、copy/status/MCP/Skill、Working 固定 Clock 快照、True Color/ANSI/No Color、Rich/Raw、Exec/Explore/WebSearch、separator、interrupt、diff |

## 16. 决策日志

| 日期 | 决策 | 影响 |
|---|---|---|
| 2026-08-05 | canonical rollout 取代 Conversation 六表主链 | Session/Run/Context/中断与 Resume 全部重排 |
| 2026-08-05 | 首个稳定版前采用一次破坏性 canonical schema 迁移 | 删除未发布旧表，不维护开发期数据转换层；稳定版后恢复数据保留型前向迁移 |
| 2026-08-05 | Reactor 成为唯一 Agent 内核，计划收敛为 `update_plan` 与只规划 `/plan` | 删除 DAG、Scheduler 和外层 Plan Executor |
| 2026-08-05 | ToolOutcome 取代通用 Evidence | Tool、Model、UI、Audit 和 Rollout 使用同一结果事实 |
| 2026-08-05 | 删除 Snapshot/Revert 与 ThreadRollback 主链 | 历史阶段使用 RunDiffTracker；当前目标由 RunDiffProjector 投影 exact Patch delta，撤销仍依赖 Git 或新 Patch |
| 2026-08-06 | Tool 并发曾收敛为 Run 级 Shared/Exclusive Gate | 历史实现删除资源级锁；当前进一步由 `SupportsParallelToolCalls()` + Run 级读写锁取代公共 Shared/Exclusive 枚举 |
| 2026-08-06 | 删除 TargetStrategy 并引入 PreparedToolCall | 历史 Prepared Pipeline 已完成；当前 M8H 仅为复杂 Handler 保留私有 PreparedPatch/ExecRequest，删除通用 PreparedToolCall |
| 2026-08-06 | 删除 `architecture-audit.md` | `design.md` 为唯一架构事实源，完成度由测试和本进度文档跟踪 |
| 2026-08-06 | ExtensionRuntime 提升为 Session 生命周期 | Skill Catalog 与 MCP Manager 跨 Run 复用，切换 Session 时重建；RequestContext 冻结 Skill/MCP/Tool revision，MCP 重连后重新验证 Catalog |
| 2026-08-07 | Permission、Isolation、Operation Approval 与 ExecPolicy 分层 | 所有文件系统资源型 Tool 先做 Permission Check；结构化 Tool 通过后直接执行；Linux Shell 使用 Sandboxed，其他情况明确 Unsandboxed 并做完整 Command Approval |
| 2026-08-07 | 删除 PrimaryRoot 安全语义 | `Project.RootPath` 只持久化项目身份，RunContext.CWD 负责相对路径，WorkspaceRoots 精确由 `[CWD] + --add-dir` 派生并参与指令与权限计算 |
| 2026-08-07 | ReadOnly 与 Denied 权限语义分离 | ReadOnlyRoots 只撤销写能力，DeniedRoots 拒绝访问且普通 Grant 不可绕过；AMADEUS_HOME 只 deny 敏感子 Root，不封禁整个根；MVP 不实现 DeniedGlobs |
| 2026-08-07 | `--add-dir` 定义为附加 Workspace Root | 附加 Root 加载目标相关 AGENTS.md；首版项目 MCP/Skill 仍只从 CWD 对应默认 Workspace Root 加载，Additional Writable Root 不获得 Workspace 语义 |
| 2026-08-07 | 平台临时写路径不绑定 Run | Unix 使用 `/tmp + $TMPDIR`、Windows 使用 `os.TempDir()`；不实现 `/tmp/amadeus/{run-id}` 或其他 Run 专属临时目录 |
| 2026-08-07 | Permission Store 收敛为 Run 与 Session 两层 | 两个 Store 只保存 Additional Writable Roots；没有 one-shot permission；Grant 后模型重新调用原 Tool，所有 Store 均不写 SQLite、不随 resume 恢复 |
| 2026-08-07 | SessionApprovalStore 只服务 Unsandboxed Command | Allow once 不缓存，Allow for session 使用 Shell/Command/CWD/TTY/IsolationMode 精确 Key；Approval 命中永远不能跳过 Permission Check，不建立 RunApprovalStore |
| 2026-08-07 | MVP 网络默认允许 | 不建立 NetworkPermissionStore；Web 保留 SSRF/Redirect Guard，MCP 受已配置 Server/Binding 边界约束 |
| 2026-08-07 | M8U Permission/Approval Runtime 完成 | Permission Grant 重调用、Session Command Approval、多 Workspace AGENTS、Sandboxed/Unsandboxed 分流、Audit fail-closed 与多 Root RunDiff 全部接入统一 PreparedToolCall 主链并通过全仓/race/发布门禁 |
| 2026-08-07 | Prompt 优化拆为独立 M8P | M8P-01～04 负责目录迁移、Reactor 解耦、Codex 成熟规则选择性改写和 Prompt Contract/E2E；Prompt 完成后再进入交互与发布阶段 |
| 2026-08-07 | M9 重排为 TUI 交互与 Slash Command | 保留 Amadeus Logo；统一 Composer/Popup/Selection/Dispatcher，最终加入 `/resume /skills /rename /delete /compact /plan /copy /status /mcp /clear /exit`；原发布 M9 顺延 M10，Multi-Agent 顺延 M11 |
| 2026-08-08 | 在 M9 后新增 M9V TUI Visual Runtime | M9 只视为交互命令重构完成；M9V 建立 Codex TerminalPalette、Motion、首版 TranscriptCell/ActiveCell、Exec/Explore/WebSearch 与内容边界 Separator 行为基线；2026-08-10 的 M9-21～26 再完成 HistoryCell 最终结构 |
| 2026-08-08 | 重新打开 M9 进行 Codex 语义视觉一致性补强 | M9-15～20 收敛 terminal-aware semantic palette、`›` Composer prompt、统一 cyan selection、Tool/Status 层级与 Markdown/代码高亮；复用 M9V 行为基线，不建立第二套 Visual Runtime；该阶段门禁后来由 2026-08-10 的 M9-21～26 HistoryCell 结构收敛继续延后 |
| 2026-08-10 | 修复 Rich TUI 实机黑白回归并对齐 Codex StatusLineAccent | ANSI16 accent 保留标准 cyan；状态栏使用 cyan/green/magenta 分类；Program 同步 Lip Gloss profile；继承 `NO_COLOR` 不再关闭 Rich TUI 颜色；输入占位与运行提示统一英文 |
| 2026-08-10 | 修复最终回复到 Composer 的三行距离 | `flushTranscript` 提交精确 Cell 输出，不再使用 `tea.Println(output + "\n")` 追加空行；空闲 Bottom Pane 独占两次换行，避免 scrollback 尾行与 View 前导留白叠加 |
| 2026-08-10 | M9 新增 Codex HistoryCell 结构收敛 | M9-21～26 对齐 HistoryCell、ActiveHistoryCell、HistoryRenderMode、TranscriptState 与 InsertHistoryCell 语义；正式历史、活动状态和 history insertion 分离，删除 Kind/Render string/Committed 兼容链，M10 改为等待 M9-26 |
| 2026-08-10 | M9-21～26 HistoryCell 重构完成 | Message/Tool/Plan/Separator 全部迁入 DisplayLines/RawLines；TranscriptState 与正式 History ownership 分离；结构化 insertion、Rich/Raw、continuation、revision 和架构守卫通过全仓/race/make check/build/diff check |
| 2026-08-10 | 新增 M10-00A Token/Context 收敛 | 向 Codex 主链看齐，移除稳定 Agent 累计 input/output token 双预算；Provider 输出上限只服务单次请求；Context Profile 改为 effective window percent 与 auto compact token limit |
| 2026-08-10 | M10-00A Token/Context 收敛完成 | 配置、Reactor Budget、ContextProfile、历史 Tool Result 投影、示例/README/config explain 与兼容测试完成；旧 Agent token 字段明确拒绝，不静默忽略 |
| 2026-08-08 | RunDiff 对齐 Codex 双层语义 | RunDiffProjector 只记录可信 Patch exact delta；普通 Shell/MCP 不触发失效或 transcript 警告；真实工作区变化留给独立 WorkspaceDiff `/diff` 按需读取 Git 状态且不做 Tool 归因 |
| 2026-08-08 | 新增 M8H Tool Handler 与 Apply Patch 收敛 | ToolRouter/Registry/Handler 成为唯一主链；Permission 集中于 FileSystemPolicy；并发改为 Handler capability；ApplyPatchHandler 私有 PreparedPatch 负责 preflight、staging、revalidation、partial 与 exact delta |

## 17. 每次更新模板

```text
日期：YYYY-MM-DD
任务：M?-??
状态：TODO / DOING / BLOCKED / DONE / SKIPPED / SUPERSEDED
改动：
- ...
验证：
- ...
遗留：
- ...
下一步：
- ...
```
