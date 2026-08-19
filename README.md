# Amadeus

Amadeus 是一个使用 Go 实现的 codex-like 终端 Coding Agent。它可以在指定项目中读取和搜索代码、编辑文件、执行命令、查看图片、调用 Web、Skill 与 MCP 工具，并通过可恢复的会话持续完成软件开发任务。

Amadeus 支持交互式 TUI 和一次性任务两种使用方式。运行时使用单一 Turn continuation loop：模型可以连续调用工具、读取执行结果并继续工作，直到返回最终回答。会话历史以 canonical rollout 持久化，可用于继续或恢复此前的对话。

## 启动命令

### 交互模式

在目标项目目录中直接启动：

```bash
amadeus
```

未指定 `--project` 时，启动命令所在的当前目录就是目标项目目录。

### 执行一次性任务

通过位置参数提交一个任务：

```bash
amadeus "修复登录接口的错误处理并运行相关测试"
```

也可以从标准输入读取任务：

```bash
printf '%s\n' '总结这个项目的目录结构' | amadeus
```

一次启动最多接受一个任务参数。带任务参数或从非 TTY 标准输入读取任务时，Amadeus 使用一次性模式；没有任务且标准输入为终端时，Amadeus 进入交互模式。

### 指定项目和附加目录

```bash
amadeus --project /path/to/project "运行测试并修复失败用例"
```

通过可重复的 `--add-dir` 增加当前进程可以写入的附加目录：

```bash
amadeus \
  --project /workspace/backend \
  --add-dir /workspace/frontend \
  --add-dir ../shared \
  "同步修改后端、前端和共享模块"
```

相对路径以启动 Amadeus 时的当前工作目录为基准。`--add-dir` 不会改变主项目身份或会话归属。

### 继续或恢复会话

继续当前项目最近使用的会话：

```bash
amadeus --continue
```

在交互界面中选择一个历史会话：

```bash
amadeus --resume
```

直接恢复指定会话：

```bash
amadeus --resume <session-id>
```

恢复指定会话并立即执行一个任务：

```bash
amadeus --resume <session-id> "继续完成剩余测试"
```

也可以使用等号形式：

```bash
amadeus --resume=<session-id> "继续完成剩余测试"
```

`--continue` 和 `--resume` 不能同时使用。不带会话 ID 的 `--resume` 只适用于交互终端。

## 启动参数

命令格式：

```text
amadeus [task] [flags]
```

| 参数 | 说明 |
|---|---|
| `[task]` | 可选的一次性任务文本，最多一个 |
| `--project <path>` | 指定主项目目录；默认使用启动时的当前目录 |
| `--add-dir <path>` | 增加附加可写目录，可重复使用 |
| `--continue` | 继续当前项目最近使用的会话 |
| `--resume[=<session-id>]` | 恢复指定会话；省略 ID 时打开会话选择界面 |
| `--config <path>` | 使用显式指定的配置文件 |
| `--model-provider <name>` | 为当前进程覆盖模型 Provider |
| `--model <name>` | 为当前进程覆盖模型名称 |
| `--wire-api <api>` | 覆盖 Provider Wire API，可用值为 `responses` 或 `chat_completions` |
| `--dialect <dialect>` | 覆盖 Provider 方言，可用值为 `standard`、`openai`、`deepseek`、`qwen` 或 `glm` |
| `--base-url <url>` | 为当前进程覆盖 Provider Base URL |
| `-h`, `--help` | 显示命令帮助 |

示例：

```bash
amadeus \
  --config ./config.yaml \
  --project /workspace/project \
  --model-provider openai \
  --model gpt-5 \
  "检查当前修改并运行测试"
```

可以随时通过以下命令查看当前版本支持的参数：

```bash
amadeus --help
```

## Slash Command

Slash Command 仅在交互模式中使用。在输入框中输入 `/` 可以查看和筛选可用命令。

| 命令 | 说明 |
|---|---|
| `/resume [session-id]` | 恢复历史会话；省略 ID 时打开会话选择界面 |
| `/skills` | 浏览可用 Skill，或启用、禁用 Skill |
| `/rename [name]` | 重命名当前会话；省略名称时打开输入界面 |
| `/delete` | 确认后永久删除当前会话并退出 |
| `/compact` | 压缩当前会话上下文，降低上下文占用 |
| `/plan [task]` | 切换到 Plan Mode；提供任务时会在切换后立即提交该任务 |
| `/copy` | 将最近一次 Agent Markdown 回答复制到剪贴板 |
| `/status` | 显示当前会话、模型、权限和 Token 使用状态 |
| `/mcp` | 显示已配置的 MCP Server、Tool 和 Resource 摘要 |
| `/mcp verbose` | 显示更详细的 MCP 清单 |
| `/clear` | 清空当前界面并创建一个新会话 |
| `/exit` | 关闭当前会话并退出 Amadeus |

`/resume`、`/rename`、`/plan` 和 `/mcp` 支持行内参数，其他 Slash Command 不接受参数。

任务运行期间，仅 `/skills`、`/copy`、`/status` 和 `/mcp` 可用；此时 `/skills` 可以查看 Skill，但不能修改启用状态。Plan Mode 也可以通过 `Shift+Tab` 在 Plan 与默认执行模式之间切换。
