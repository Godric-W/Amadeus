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
| `--model-reasoning-effort <effort>` | 覆盖推理强度：`none`、`minimal`、`low`、`medium`、`high`、`xhigh` 或 `max`；省略时使用厂商默认值 |
| `--model-input-modalities <values>` | 覆盖模型输入模态，逗号分隔；基础版支持 `text`、`image` |
| `--model-supports-original-image-detail` | 声明当前模型支持 `view_image.detail=original` |
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

配置文件可使用顶层 `model_reasoning_effort`，环境变量为 `AMADEUS_MODEL_REASONING_EFFORT`。显式配置时 Responses 发送 `reasoning.effort`，Chat Completions 发送 `reasoning_effort`；DeepSeek、Qwen、GLM 的 Chat `none` 会转换为各自的关闭 thinking 字段。`amadeus config show` 输出脱敏后的有效配置，`amadeus config explain` 同时显示各字段来源。

模型图片能力必须显式配置，不能从 Provider 或 OpenAI-compatible Dialect 推断。默认 `model_input_modalities: [text]`；只有真实支持图片的模型才应配置 `[text, image]`。`model_supports_original_image_detail` 默认 `false`，仅对明确支持 original detail 的模型开启。对应环境变量为 `AMADEUS_MODEL_INPUT_MODALITIES` 与 `AMADEUS_MODEL_SUPPORTS_ORIGINAL_IMAGE_DETAIL`。

## Web 工具

`web_search` 与 `web_fetch` 使用独立开关。仅启用搜索时，可以使用无需 API Key 的 DuckDuckGo：

```yaml
web:
  search:
    enabled: true
    provider: duckduckgo
  fetch:
    enabled: false
```

`duckduckgo` 是默认搜索 Provider，因此 `web.search.enabled: true` 且未显式设置 `provider` 时也会使用 DuckDuckGo。还可配置 `tavily`、`searxng` 或 `brave`；Tavily 和 Brave 需要各自的 API Key，SearXNG 通常需要配置实例 `base_url`。完整字段参见 `configs/amadeus.example.yaml`。

- `web_search` 返回搜索引擎整理的标题、链接和摘要，默认不请求 Approval；摘要适合发现来源，但不等同于已核验的网页全文。
- `web_fetch` 读取一个精确 HTTP(S) URL 的有界正文并转换为可读 Markdown，适用于用户直接提供 URL、搜索摘要不足或需要核对原文的场景。
- `web_fetch` 对未授权 Hostname 请求 Approval；“不再询问”只授权当前 Session 中的精确 Hostname，不会授权其他域名。
- 同 Hostname 重定向可在安全校验后按限制跟随；跨 Hostname 重定向会停止，后续 URL 必须通过新的 `web_fetch` 调用单独授权。
- `web.fetch.max_redirects: 0` 表示拒绝所有重定向；`max_bytes` 超限时返回带 `Partial` 标记的截断结果。

若只需要搜索，打开 `web.search.enabled` 即可；若还需要读取网页正文，再单独打开 `web.fetch.enabled`。两个能力关闭时不会注册对应 Tool，模型也不可见。

## 图片工具

`view_image` 只对显式声明 `image` 输入能力的模型可见。它读取本地 PNG、JPEG、WebP 或静态 GIF，经过尺寸和 patch 预算约束后再作为图片 ToolResult 发送给模型；动态 GIF 会被拒绝，静态 GIF 会规范化为 PNG。

```yaml
model_input_modalities: [text, image]
model_supports_original_image_detail: false
```

默认 detail 为 `high`。只有开启 `model_supports_original_image_detail` 时，Tool Schema 才会向模型暴露 `original`；该模式仍受 6000 单边和 10000 个 32×32 patch 的预算约束，不表示无界原始文件直传。工作目录外的图片沿用 read-directory Approval 与当前 Session Grant。

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
| `/status` | 显示当前会话、模型、已配置 reasoning effort、权限和 Token 使用状态 |
| `/mcp` | 显示已配置的 MCP Server、Tool 和 Resource 摘要 |
| `/mcp verbose` | 显示更详细的 MCP 清单 |
| `/clear` | 清空当前界面并创建一个新会话 |
| `/exit` | 关闭当前会话并退出 Amadeus |

`/resume`、`/rename`、`/plan` 和 `/mcp` 支持行内参数，其他 Slash Command 不接受参数。

任务运行期间，仅 `/skills`、`/copy`、`/status` 和 `/mcp` 可用；此时 `/skills` 可以查看 Skill，但不能修改启用状态。Plan Mode 也可以通过 `Shift+Tab` 在 Plan 与默认执行模式之间切换。
