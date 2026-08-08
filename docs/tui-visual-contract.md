# Amadeus TUI Visual Contract

本文档冻结 M9V 的视觉验收契约。参考事实源为 `../codex-main/codex-rs/tui`，实现保留 Amadeus 的 Go、Bubble Tea、Lip Gloss、Braille Logo、主屏 scrollback 和原生鼠标选择，不复制 Rust 类型。

## Working

- 动画刷新周期约为 32ms，完整柔光扫过周期为 2s。
- True Color 使用终端前景色与背景色混合的余弦光带，左右 padding 为 10 个字符，光带半宽为 5。
- ANSI256/ANSI16 使用稳定的明暗降级；活动点每 600ms 在 `•` 与 `◦` 之间切换。
- Reduced Motion 使用静态活动点和普通 `Working`；No Color 不输出 ANSI 控制序列。
- 文案为 `• Working (40s • esc to interrupt)`，时间超过一分钟后使用零填充格式，例如 `2m 05s`、`1h 03m 09s`。

## Palette

- 颜色能力分为 True Color、ANSI256、ANSI16、No Color，并区分明暗背景。
- Style 只通过 default、dim、accent、success、failure、warning、separator 和 user background 语义访问。
- Read/List/Search 使用 accent；成功点使用 success；失败点和错误使用 failure；提示与分隔线使用 dim/separator。
- No Color 保留文字、符号、缩进和边界，不以颜色作为唯一信息。

## Transcript Cells

- User、Assistant、Plan、Notice、Error、Exec、Explore、WebSearch 和 Final Separator 都是独立 Cell。
- Plan Update 使用 Codex 风格的 `• Updated Plan` 标题；说明与第一项共用唯一 `└` 入口，进行中/待处理项使用 `□`，完成项使用 `✔`，主界面不展示内部 revision 或 `plan-N` 标识。
- Cell 内容不携带为了排版伪造的首尾换行；Transcript Layout 在 Cell 之间统一插入一行空白。
- 流式 Assistant 与活动 Tool Cell 留在 Bottom Pane，完成后一次提交到主屏 scrollback。
- Reactor 的 `IterationStarted/IterationCompleted` 不是视觉边界，不触发横线或批量 Tool 输出。

## Tool History

- Exec 主界面将 Running/Ran/You ran 与首行命令放在同一行，命令续行使用 `│`；输出仅首行使用唯一的 `└`，后续行对齐缩进。主界面不显示详情 transcript 专用的 `✓/✗ + duration` 尾行，成功或失败由标题点颜色与输出内容表达。
- Explore 聚合连续只读行为，以 Exploring/Explored 展示；连续相同 Read 去重；第一项使用唯一的 `└` 树形入口，后续同级项只做对齐缩进；Read/List/Search 使用 accent。
- WebSearch 单独显示 Searching/Searched the web。
- Tool Started 立即出现活动 Cell；Tool Completed 按 CallID 原位更新；并行调用按首次出现 sequence 保持稳定顺序。
- 成功、失败和活动状态不能仅依赖颜色：分别保留 `•`、失败说明和动态/静态活动点。

## Separator

- Tool 工作结束后，在最终 Assistant 消息前按内容边界插入 dim rule。
- Run terminal 时如仍有未封口的工作活动，追加 Final Message Separator。
- 不超过 60s 的 Run 只显示 dim rule；超过 60s 显示 `─ Worked for 2m 05s ───`。
- 不因 Reactor iteration 数量增加 separator。

## Layout And Terminal Matrix

- Assistant、Tool、Separator 与下一条 User Message 之间保持统一的一行空白。
- Composer 与活动内容保持两行视觉距离；运行期间 Composer 仍可输入并排队。
- 40～59 列使用紧凑 Logo/面板；60 列以上使用宽布局并保留右侧 margin。
- 中文、emoji 和宽字符不破坏截断、换行、选择器或输入光标。
- Plain fallback、No Color、ANSI16、ANSI256、True Color、Reduced Motion 都必须有确定性测试。
