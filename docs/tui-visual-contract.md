# Amadeus TUI Visual Contract

本文档冻结 M9V 的视觉验收契约。参考事实源为 `../codex-main/codex-rs/tui`，实现保留 Amadeus 的 Go、Bubble Tea、Lip Gloss、Braille Logo、主屏 scrollback 和原生鼠标选择，不复制 Rust 类型。

## Working

- 动画刷新周期约为 32ms，完整柔光扫过周期为 2s。
- True Color 使用终端前景色与背景色混合的余弦光带，左右 padding 为 10 个字符，光带半宽为 5。
- ANSI256/ANSI16 使用稳定的明暗降级；活动点每 600ms 在 `•` 与 `◦` 之间切换。
- Reduced Motion 使用静态活动点和普通 `Working`；No Color 不输出 ANSI 控制序列。
- 文案为 `• Working (40s • esc to interrupt)`，时间超过一分钟后使用零填充格式，例如 `2m 05s`、`1h 03m 09s`。
- 活动区使用 UI Clock 连续刷新耗时；Task 完成消息携带实际执行耗时，Final Separator 优先使用该事实值，只有旧消息或测试夹具缺失耗时时才回退 UI Clock。
- 与 Codex 一致，只有发生具体 Tool/MCP 工作的 Run 才提交 Final Separator；纯对话不制造空分隔线。耗时超过 60 秒时显示 `─ Worked for 7m 18s ─────`，短 Run 只显示 dim 横线。

## Palette

- 颜色能力分为 True Color、ANSI256、ANSI16、No Color，并区分明暗背景。
- Style 只通过 default、strong、dim、accent、selection、success、failure、warning、separator 和 user background 语义访问，不在 Widget/Cell 中直接写 RGB 或 ANSI 编号。
- 深色或未知背景 accent 使用终端 ANSI cyan + bold；浅色背景在 TrueColor/ANSI256 下使用接近 `RGB(0,95,135)` 的深青蓝；ANSI16 仍使用标准 cyan，不退化成只有粗体的黑白文本。
- `Ran`、`Explored`、`Updated Plan` 使用默认前景 + bold，不使用固定亮白色；Read/List/Search 使用 accent；成功点使用 success + bold；失败点和错误使用 failure + bold；提示、树形线、Tool 输出与分隔线使用 dim/separator。
- success/failure 只表达状态，不把整条 Tool History 染色；warning 只用于真实警告，不作为普通选择色或健康状态色。
- No Color 保留文字、符号、缩进和边界，不以颜色作为唯一信息。

## Composer And Selection

- Composer prompt 使用 `›`，输入可用时为默认前景 + bold，真正禁用输入时为 dim；Placeholder 使用 dim，输入正文使用默认前景。Run 执行期间仍允许编辑与排队，因此 prompt 保持可用态。
- Slash Popup、Resume、Approval、Skills 等列表共用同一 Selection Renderer。Modal picker 的选中行保留 cursor，Slash Command Popup 对齐 Codex CommandPopup，不显示 `›` cursor glyph；两者的选中名称与说明仍统一使用 selection，未选中名称使用默认前景，说明使用 dim，禁用项整体 dim。
- Slash Command 列表不整体染成青色，也不使用黄色 selected row；搜索命中字符可额外 bold，但不能破坏 selected row 的统一样式。
- Footer/Statusline 对齐 Codex StatusLineAccent 的 theme-first 规则：TrueColor/ANSI256 根据终端明暗背景选择 Catppuccin Mocha/Latte，并从对应语义 token 解析 model/type、path/string、branch/function、usage/number、mode/keyword 与 thread/heading 色，再应用 Codex 85% saturation softening；ANSI16 使用 Codex cyan/green/magenta fallback，No Color 整行 dim。分隔符使用 dim；context 达到警告/失败阈值后覆盖为 yellow/red。Plan mode 不混入左侧状态段，而是在预留的独立右列使用 mode accent 右对齐；宽度允许时显示 `Plan mode (shift+tab to cycle)`，与左侧 statusline 不能共存时收缩为 `Plan mode`，Default mode 不显示模式标签。左侧内容必须在右列起点前完成裁剪，不得与 context 重叠。
- Fullscreen inline View 使用真实内容高度，不通过顶部补空行模拟 Codex/Ratatui 的全屏 surface。Footer 是活动 frame 的最后一行，Plan mode 在该行独立右对齐；Bubble Tea renderer 独占永久输出和 frame redraw，Amadeus 不在 output writer 外层追加 cursor up/down 定位协议。
- Shift+Tab 的 settings acknowledgement 刷新右侧 collaboration mode indicator，并插入 `• Mode changed to <Mode>.` Info HistoryCell。History row 只包含该消息，重复切换不得把 Composer、placeholder、Slash Popup 或 Footer 固化到 terminal scrollback。
- Slash command popup 使用 command name 的 exact/prefix 匹配；`/e` 只显示 `/exit`。Popup 激活时替换普通 Footer 区域，不与 statusline 或 mode indicator 同屏，filter 变化时 selection 重置到首个候选。
- `/exit` 与空 Composer 的退出快捷键进入唯一 shutdown-first lifecycle。等待期间活动 frame 只显示一份 `Shutting down…`，不写入 History；shutdown 完成或 bounded timeout 后先渲染空 active frame，再退出 Bubble Tea。终端 scrollback 不得残留 Composer placeholder、Popup 或 Footer；token usage 与 resume hint 只在终端恢复后由 CLI 输出。Color terminal 只将 resume command 染为 ANSI cyan，周围说明文字保持默认前景；No Color 不输出 ANSI。
- Rich TUI 不因父进程为 shell Tool 注入 `NO_COLOR` 而静默退化成黑白；No Color 由终端能力或显式 Renderer 选项控制，非 TTY 交互直接拒绝，不再切换 Plain 交互主链。
- 当前 Statusline 与 Markdown 共用明暗自适应 Chroma theme source；尚未复制 Codex 的 `/theme`、自定义 tmTheme 加载与全局 syntax scope resolver，因此对齐默认 Catppuccin Mocha/Latte 和 ANSI fallback，不宣称与用户自定义 Codex 主题逐色一致。

## Markdown And Code

- H1 使用 bold + underline，H2 使用 bold，H3 使用 bold + italic，H4～H6 使用 italic。
- 行内代码使用 accent/cyan 且不强制 bold；链接使用 accent/cyan + underline；引用使用 green；有序列表标记使用 light blue；普通正文和无序列表标记使用默认前景。
- Fenced code block 根据语言进行语法高亮；未知语言回退默认前景。代码块背景透明，不使用统一青色或固定灰色背景。
- Markdown 结构渲染与代码高亮只各执行一次；ANSI16、No Color 与未知语言都必须有确定性降级测试。

## HistoryCell Architecture

- `HistoryCell` 是正式历史与活动展示的统一结构化单位；核心方法为 `DisplayLines()`、`RawLines()` 与 `IsStreamContinuation()`，不得以 `Render() string` 作为最终 Cell Contract。
- `ActiveHistoryCell` 扩展 `HistoryCell`，只增加 Event 原位更新、完成判断和封口能力；完成后仍作为普通 `HistoryCell` 插入正式历史。
- `TranscriptState` 只保存 Active Cell、活动修订号、最近 Agent Markdown 和 Turn 级 work/separator 标记；正式 `HistoryCells`、待提交 `PendingHistoryCells` 与 `HasEmittedHistoryLines` 由 Rich Inline Application 持有。
- User、Agent、Plan、Notice、Error、Exec、Explore、WebSearch 和 Final Separator 都是独立具体 Cell；不保留 `HistoryCellKind` 或通用 kind switch。
- `HistoryRenderRich` 输出语义样式与 Markdown；`HistoryRenderRaw` 输出适合 Plain、复制和无样式 Transcript 的内容，Raw 不能从 ANSI 字符串反向剥离得到。
- Plan Update 使用 Codex 风格的 `• Updated Plan` 标题；说明与第一项共用唯一 `└` 入口，进行中/待处理项使用 `□`，完成项使用 `✔`，主界面不展示内部 revision 或 `plan-N` 标识。
- Cell 内容不携带为了排版伪造的首尾换行；`displayLinesForHistoryInsert()` 根据 `HasEmittedHistoryLines` 和 `IsStreamContinuation()` 在独立 Cell 之间插入一个空 `StyledLine`。
- 流式 Agent draft 与活动 Tool Cell 留在 Bottom Pane，完成后一次形成 `AgentMessageCell` 或完成 Tool Cell 并提交到主屏 scrollback；当前不引入 Codex 的 provisional stream consolidation。
- Model Step 不是视觉边界，不触发横线或批量 Tool 输出；视觉只跟随 Assistant/Tool Item 生命周期。

## Tool History

- Exec 主界面将 Running/Ran/You ran 与首行命令放在同一行，命令续行使用 `│`；标题使用 strong，命令使用默认前景或 Shell 语法高亮，输出使用 dim。输出仅首行使用唯一的 `└`，后续行对齐缩进。主界面不显示详情 transcript 专用的 `✓/✗ + duration` 尾行，成功或失败由标题点颜色与输出内容表达。
- Explore 聚合连续只读行为，以 Exploring/Explored 展示；连续相同 Read 去重；第一项使用唯一的 `└` 树形入口，后续同级项只做对齐缩进；Read/List/Search 使用 accent。
- WebSearch 单独显示 Searching/Searched the web。
- Tool Started 立即出现活动 Cell；Tool Completed 按 CallID 原位更新；并行调用按首次出现 sequence 保持稳定顺序。
- 成功、失败和活动状态不能仅依赖颜色：分别保留 `•`、失败说明和动态/静态活动点。

## Separator

- Tool 工作结束后，在最终 Assistant 消息前按内容边界插入不带耗时的 dim rule。
- Run terminal 时如本 Turn 发生过具体工作，在最终 Assistant 消息后追加 Final Message Separator；耗时标签不得提前出现在最终回复上方。
- 不超过 60s 的 Run 只显示 dim rule；超过 60s 显示 `─ Worked for 2m 05s ───`。
- 不因 Model Step 数量增加 separator。

## Layout And Terminal Matrix

- Agent、Tool、Separator 与下一条 User Message 之间保持统一的一行空白。
- Composer 与活动内容保持两行视觉距离；运行期间 Composer 仍可输入并排队。输入超过可用宽度时按终端显示宽度软换行并扩展到最多五行。
- Rich TUI 必须把真实终端光标锚定到 Composer 的显示光标；即使使用绘制型块光标，也不能让 macOS 等平台的输入法候选窗口停留在 Statusline。
- 已完成 Cell 提交到 main-screen scrollback 时不得附加尾部空行；空闲 Bottom Pane 独占最终回复到 Composer 的两次换行，避免提交空行与 View 前导空行叠加成三行距离。
- 40～59 列使用紧凑 Logo/面板；60 列以上使用宽布局并保留右侧 margin。
- 中文、emoji 和宽字符不破坏截断、换行、选择器或输入光标。
- No Color、ANSI16、ANSI256、True Color、Reduced Motion 和非 TTY 拒绝行为都必须有确定性测试。
