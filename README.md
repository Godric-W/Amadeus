# Amadeus

Amadeus 是一个使用 Go 实现的终端 Coding Agent。当前版本使用单一 Reactor 完成代码探索、修改、命令执行与验证，并通过可恢复 Session、canonical rollout 和 Typed Events 统一运行时状态。

## 当前能力

- 根命令直接启动 Coding Agent：`amadeus` 或 `amadeus "<task>"`。
- 支持 OpenAI Responses 与 OpenAI-compatible Chat Completions。
- 支持 OpenAI、standard、DeepSeek、Qwen 和 GLM Provider 方言。
- 内置结构化探索、`apply_patch`、`execute_command/write_stdin`、图片、Web、Skill 和 MCP gateway 工具。
- 加载用户级、项目级和目录级 `AGENTS.md`。
- 写文件和执行命令经过 FileSystemPolicy、PathResolver、CommandGuard、Sandbox、审批和 JSONL 审计。
- 支持 append-only Session history、`--continue`、`--resume`、`sessions list`、流式输出和 Ctrl+C 中断当前 Run。
- Tool 并发只使用 Run 级 Shared/Exclusive Gate：只读调用可有界并行，写入、Shell 和未知副作用调用独占执行。
- 文件变化由 RunDiffTracker 投影；不提供 Snapshot/Revert 或 `revert_run`。

默认 execute Run 使用 Plan-guided ReAct：模型可按任务复杂度调用 `update_plan` 维护软计划，但始终由同一个 Think → Analyze → Act → Observe Reactor 执行。`/plan <task>` 是只规划不实施的 Plan Mode，会使用同一 Reactor 和只读工具集。当前不内置 RAG、不自动推断长期用户记忆，也不启用 Multi-Agent 主链。

工具选择遵循固定边界：常规读取、目录浏览、文件发现和文本搜索优先使用结构化工具；创建、修改、移动和删除项目文件统一使用 `apply_patch`；构建、测试、Git、格式化和项目脚本使用 `execute_command`，持续进程通过 `write_stdin` 轮询或输入。Shell fallback 仍受项目围栏、策略、审批和审计约束，不能用于绕过被拒绝的专用工具操作。

## 构建

要求 Go 1.26 或兼容版本。

```bash
go build -buildvcs=false -o bin/amadeus ./cmd/amadeus
```

也可以执行完整检查：

```bash
make check
```

## Amadeus Home

`AMADEUS_HOME` 是 Amadeus 自身配置和用户级指令目录，不是目标代码项目目录：

```bash
export AMADEUS_HOME="$HOME/.config/amadeus"
mkdir -p "$AMADEUS_HOME"
cp configs/amadeus.example.yaml "$AMADEUS_HOME/config.yaml"
```

未设置 `AMADEUS_HOME` 时，程序使用已解析的 `amadeus` 可执行文件所在目录。推荐显式设置该变量，避免安装位置变化影响配置发现。

目标项目默认是启动命令时的当前工作目录，也可以通过 `--project <path>` 指定。`AMADEUS_HOME` 与项目目录始终独立。

## Provider 配置

配置优先级从高到低为：

1. CLI flags。
2. `AMADEUS_*` 环境变量。
3. `$AMADEUS_HOME/config.yaml`。
4. 程序默认值。

推荐从示例开始，并通过环境变量注入凭证：

```bash
export AMADEUS_API_KEY="your-api-key"
export AMADEUS_MODEL="your-model"
amadeus config check
amadeus config explain
```

OpenAI Responses 示例：

```yaml
version: 1
default_provider: openai

providers:
  openai:
    api: responses
    dialect: openai
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    model: your-model
    timeout: 120s
    max_retries: 2
    temperature: 0.2
    max_output_tokens: 8192

    context_window: 128000
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

OpenAI-compatible Chat Completions 通常配置为：

```yaml
providers:
  compatible:
    api: chat_completions
    dialect: standard
    api_key: ${COMPATIBLE_API_KEY}
    base_url: https://provider.example/v1
    model: provider-model
```

如果厂商需要特定扩展，可将 `dialect` 设置为 `deepseek`、`qwen` 或 `glm`。不支持 `developer` role 的 Chat Provider 会由 Adapter 自动降级为 `system` role。

`providers.<name>.max_output_tokens` 是单次模型调用限制；`agent.max_input_tokens` 和 `agent.max_output_tokens` 是整次 Run 的累计限制。

旧配置中的 `agent.mode` 已删除。普通任务默认执行；`/plan <task>` 只分析并输出计划，不修改文件或执行命令。该选择只影响当前 Run，不写入配置；如果旧配置仍有 `agent.mode`，运行 `config check` 会报告未知字段，请将其删除。

## AGENTS.md

Amadeus 使用显式指令文件代替自动长期记忆：

- `$AMADEUS_HOME/AGENTS.md`：用户级默认规则。
- `<project>/AGENTS.md`：项目根规则。
- `<project>/<directory>/AGENTS.md`：目录级规则，仅作用于该目录及其后代。

优先级为更深目录规则高于项目根规则，项目规则高于用户规则。示例：

```markdown
# Project instructions

- Keep changes minimal and focused.
- Run `go test ./...` after changing Go code.
- Do not modify generated files.
```

## 运行

在当前项目执行一次任务：

```bash
amadeus "Inspect the failing tests, fix the root cause, and run the relevant tests."
```

普通任务默认进入 Plan-guided ReAct；复杂任务由模型按需维护软计划，不创建 DAG、Scheduler 或另一套 Planner。只需要先分析和审核计划时使用：

```bash
amadeus "/plan Inspect the architecture and propose an implementation and verification plan."
```

直接执行 `amadeus` 且终端支持 TTY 时，默认启动基于 Bubble Tea 的 main-screen Rich Inline TUI，不切换 alternate screen，也不捕获终端鼠标。终端原生滚动与文字选择保持可用；界面提供像素品牌头部、多行 Unicode 输入、计划/工具/审批块、运行状态、输入历史、Slash 命令和 Session 选择器。Agent 执行期间输入框仍可编辑，按 Enter 会把下一条普通任务或 `/plan <task>` 加入串行队列。普通 `amadeus` 启动为 Draft Session，不会在没有对话时创建 Session；只有 `--continue`、`--resume <id>` 或交互 `/resume` 才恢复历史 Session。

需要简单逐行输出时显式使用：

```bash
amadeus --plain
```

非 TTY 管道和 `TERM=dumb` 也会自动降级为 Plain；Amadeus 不再读取 `AMADEUS_PLAIN` 环境变量。带任务参数的一次性命令继续使用可重定向的 transcript/Plain 输出，不进入全屏界面。

指定目标项目：

```bash
amadeus --project /path/to/project "Fix the Add implementation and run go test ./..."
```

如果从本仓库的 `bin/` 目录启动，默认 PrimaryRoot 会是 `bin/`。读取是否允许由 FileSystemPolicy 决定；要把仓库根作为项目身份、默认 cwd 和可写根，请显式指定：

```bash
cd /path/to/amadeus/bin
./amadeus --project .. --config ../config.yaml "查看 docs 目录下的文件内容"
```

从管道读取单次任务：

```bash
printf '%s\n' 'Summarize this project structure' | amadeus --project /path/to/project
```

进入交互模式：

```bash
cd /path/to/project
amadeus
```

交互模式每次提交创建一个独立 Run。输入 `/exit` 或发送 EOF 退出；按 Ctrl+C 会将当前 Run 标记为 interrupted、补齐悬空 Tool 协议并停止运行。后续输入创建新 Run，模型从 canonical rollout 看到中断事实并重新规划，不恢复旧 Go 调用栈。

`version`、`config`、`tools`、`sessions` 和 `web` 等管理子命令不会启动 Coding Agent。

## 审批与安全

当工具需要写文件、执行命令、访问网络或调用有副作用的 MCP 目标时，TTY 会显示工具名、风险、原因和规范化参数哈希，不显示完整敏感参数。可使用方向键选择并按 Enter 确认：

- `y`：只允许本次调用。
- `s`：允许当前 session 中同名工具的后续调用。
- `n`：拒绝本次调用。

非 TTY 环境固定拒绝需要审批的工具调用。结构化只读工具无需审批；写入、命令、网络和 MCP 工具需要 TTY 用户确认。宿主文件系统默认可读，PrimaryRoot、重复 `--add-dir` 指定目录和临时目录可写，ProtectedRules 优先拒绝敏感路径。Linux 优先使用 Bubblewrap workspace-write Sandbox；不可用时发布 `sandbox_degraded` 诊断，不能把逻辑 Path Guard 宣称为完整 OS 隔离。

```bash
amadeus --add-dir ../shared --add-dir /workspace/frontend "Update both workspaces"
```

`--add-dir` 只增加当前进程的 Writable Root，不改变项目身份、Session 归属或配置来源。

## Skill、MCP 与 Web

- 用户 Skill：`$AMADEUS_HOME/skills/<name>/SKILL.md`；项目 Skill：`<project>/.amadeus/skills/<name>/SKILL.md`，项目同名项覆盖用户项。
- Skill Catalog 常驻 metadata，不常驻全部正文。用户可在任务中写 `$skill-name` 显式注入；模型也可通过 `read_skill` 按需读取正文和 `references/`。
- 用户 MCP：`$AMADEUS_HOME/mcp.yaml`；项目 MCP：`<project>/.amadeus/mcp.yaml`。连接和 Catalog 在 Session 内复用，默认通过 `mcp_list_tools/mcp_call` 与 Resource gateway 渐进披露。
- Web Fetch 带重定向限制、SSRF 防护、超时和正文上限；Web Search 支持 DuckDuckGo、Tavily、SearXNG 和 Brave。未启用的 Web 能力不会注册给模型。

仓库提供 `configs/mcp.example.yaml` 和 `configs/skills/review/SKILL.md` 作为最小示例。MCP 环境变量必须在加载时存在；示例 server 默认 `enabled: false`，启用前请替换命令、URL 和凭证。

审计日志默认写入：

- `$XDG_STATE_HOME/amadeus/audit/audit.jsonl`；或
- `$HOME/.local/state/amadeus/audit/audit.jsonl`。

日志只记录参数哈希、风险、决定、来源、结果和耗时，不记录原始参数、API key、Authorization header 或工具输出正文。

## 输出与退出码

模型最终文本写入 stdout；工具、审批、状态、usage、诊断和结果总结写入 stderr。

| 退出码 | Outcome | 含义 |
|---:|---|---|
| 0 | `completed` | Reactor 返回最终回答并完成 Run |
| 1 | `failed` | Provider、协议、工具或基础设施失败 |
| 2 | `partial` | Run blocked 或 stalled，未完整完成 |
| 130 | `cancelled` | 当前 Run 被 Ctrl+C 或 context 取消 |

每次 Engine 正常返回都会输出一行 `result: <outcome>` 总结。

## 常用诊断

```bash
amadeus version
amadeus config check
amadeus config explain
amadeus tools list
```

`config explain` 会显示每个最终字段来自 default、file、environment 或 CLI；API key 始终显示为 `[REDACTED]`。
