# Amadeus

Amadeus 是一个使用 Go 实现的终端 Coding Agent。当前首个可用版本能够在指定项目中读取和搜索代码、修改文件、执行命令与测试，并在完成前执行 Verification 和 Reflection。

## 当前能力

- 根命令直接启动 Coding Agent：`amadeus` 或 `amadeus "<task>"`。
- 支持 OpenAI Responses 与 OpenAI-compatible Chat Completions。
- 支持 OpenAI、standard、DeepSeek、Qwen 和 GLM Provider 方言。
- 内置 `read_file`、`list_dir`、`glob_files`、`grep_code`、`apply_patch`、`execute_command` 和 `write_stdin`。
- 加载用户级、项目级和目录级 `AGENTS.md`。
- 写文件和执行命令经过 PathGuard、CommandGuard、审批和 JSONL 审计。
- 支持流式输出、交互式连续任务和 Ctrl+C 取消当前 Run。

当前版本使用 Direct 单 root Task 和统一 ReActRunner。Plan-on-Demand、会话恢复和 Multi-Agent 属于后续里程碑；当前不内置 RAG，也不自动推断长期用户记忆。

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

旧配置中的 `agent.mode` 已删除。普通任务默认使用 ReAct；需要显式拆分和循环 Replan 时使用 `/plan <task>`。该选择只影响当前 Run，不写入配置；如果旧配置仍有 `agent.mode`，运行 `config check` 会报告未知字段，请将其删除。

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

普通任务默认直接进入 ReAct，不调用 Planner。复杂任务可显式使用：

```bash
amadeus "/plan Inspect the architecture, implement the required changes, and verify all affected components."
```

直接执行 `amadeus` 且终端支持 TTY 时，默认启动基于 Bubble Tea 的 alternate-screen 全屏 TUI，提供像素品牌头部、多行 Unicode 输入、滚动 transcript、计划/工具/审批块、状态栏、输入历史、Slash 命令和 Session 选择器。Agent 执行期间输入框仍可编辑，按 Enter 会把下一条普通任务或 `/plan <task>` 加入串行队列。TUI 默认不启用终端鼠标捕获，因此可以直接拖拽选择文字；viewport 使用 PageUp/PageDown 滚动。中文输入与 Backspace 由 Bubbles textarea 按 rune 处理。普通 `amadeus` 启动为 Draft Session，不会自动读取旧会话；只有 `--continue`、`--resume` 或交互 `/resume` 才恢复历史 Session。

需要简单逐行输出时显式使用：

```bash
amadeus --plain
```

非 TTY 管道和 `TERM=dumb` 也会自动降级为 Plain；Amadeus 不再读取 `AMADEUS_PLAIN` 环境变量。带任务参数的一次性命令继续使用可重定向的 transcript/Plain 输出，不进入全屏界面。

指定目标项目：

```bash
amadeus --project /path/to/project "Fix the Add implementation and run go test ./..."
```

如果从本仓库的 `bin/` 目录启动，默认项目根会是 `bin/`，因此无法访问其父目录的 `docs/`。请显式把仓库根设为项目根，并在任务中使用项目内相对路径：

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

交互模式每行启动一个独立 Run。输入 `/exit` 或发送 EOF 退出；按 Ctrl+C 只取消当前 Run，随后可以继续输入任务。

`amadeus chat` 是不装配工具和 Agent Engine 的纯文本聊天入口。`version`、`config` 和 `tools` 等管理子命令不会启动 Coding Agent。

## 审批与安全

当工具需要写文件或执行命令时，TTY 会显示工具名、风险、原因和规范化参数哈希，不显示完整敏感参数。可选择：

- `y`：只允许本次调用。
- `s`：允许当前 session 中同名工具的后续调用。
- `n`：拒绝本次调用。

非 TTY 环境固定拒绝需要审批的工具调用。只读工具无需审批；写入、命令、网络和 MCP 工具需要 TTY 用户确认。PathGuard 和 CommandGuard 的阻断决定始终生效，路径穿越、symlink 外逃和明确危险命令会在询问用户前直接拒绝。

审计日志默认写入：

- `$XDG_STATE_HOME/amadeus/audit/audit.jsonl`；或
- `$HOME/.local/state/amadeus/audit/audit.jsonl`。

日志只记录参数哈希、风险、决定、来源、结果和耗时，不记录原始参数、API key、Authorization header 或工具输出正文。

## 输出与退出码

模型最终文本写入 stdout；工具、审批、状态、Verification、Reflection、usage 和结果总结写入 stderr。

| 退出码 | Outcome | 含义 |
|---:|---|---|
| 0 | `completed` | Run 完成并通过质量门 |
| 1 | `failed` | Provider、工具、验证或基础设施失败 |
| 2 | `partial` | Run 暂停并需要用户输入 |
| 3 | `needs_plan` | Direct Run 判断需要 Planner；Planner 将在后续里程碑接入 |
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
