# AGENTS.md — 用 MyGo + MujicaUI 编码的注意事项

本仓库（`crux-agent`，即 **Crux**，一个 Codex 风格编码 Agent；前身
MujicaUI-agent-demo / Atlas）
建立在两个库之上：

- **MyGo** `github.com/egoist/mygo`（**v0.3.4**）—— 原生 GPU 自绘 UI 与窗口运行时（无 WebView / HTML / JS）。
- **MujicaUI** `github.com/HycJack/MujicaUI` —— 组件库，一个组件类别一个包。
  这是上游 `github.com/ZacharyZhang-NY/MujicaUI` 的 fork（分支 `mygo-v0.3`）：
  上游还停在 mygo v0.2.x，fork 完成了 v0.3 值类型 `Element` API 的全库迁移；
  本仓库直接 import fork 路径。

这份文件是给改动这些代码的人 / Agent 的。核心一句：**先查文档再写，别猜；改完必须用渲染测试兜底。**

---

## 1. 文档从哪来（权威来源）

**签名永远以 `go doc` 为准**（两参数形式，别用空格拼包路径）：

```sh
go doc github.com/HycJack/MujicaUI/chat PromptComposer     # 单个符号
go doc github.com/HycJack/MujicaUI/chat PromptComposerOptions  # Options 字段
go doc -all github.com/HycJack/MujicaUI/git               # 整个包
go doc github.com/egoist/mygo/ui                                  # MyGo 的 ui 包
```

**MujicaUI 模块内**（`go list -m -f '{{.Dir}}' github.com/HycJack/MujicaUI` 拿路径）：

- `docs/components/NNN-name.md` —— **每个组件一页**：用途、最小示例、参数表（字段/类型/默认值/必填）、
  状态、事件、键盘操作、**限制**。共 500 个，按类别编号（`201-chat-container.md`、`068-command-palette.md`…）。
  这是最完整的一手资料。
- `docs/upstream.md` —— 上游 MyGo 公开接口的**缺口**清单；受影响组件在各自文档的"限制"节写明。
- `docs/compat.md`、`README.md`、`AGENTS.md`（库自身的贡献者规则，含组件签名/颜色/间距/动效约定）。
- `examples/gallery/`、`examples/minimal/` —— 可运行示例；`gallery` 能起 web 镜像。
- 每个包的 `doc.go`，以及 `*_test.go` / `example_<group>_test.go`（可编译的文档示例）。

**MyGo 模块内**：`go doc github.com/egoist/mygo/ui`；`ui/*.go` 源码；`docs/ui/*.md`。

**模块缓存在哪 / 路径坑**：`go env GOMODCACHE`。缓存目录里**大写字母被 `!` 转义**
（如 `github.com/!zachary!zhang-!n!y/!mujica!u!i@...`），所以：

- 别硬编码模块缓存路径；用 `go list -m -f '{{.Dir}}' <mod>` 拿真实路径。
- 带 `!` 的路径会干扰某些检索工具（本机的 grep 工具在 `!` 路径下会报错）；
  改用 `go doc` / `findstr` / `dir /b`。

> MujicaUI 自己的 AGENTS.md 明确要求：**Read MyGo source before using an API. Never guess.**
> 请照做。

---

## 2. MyGo 的注意事项

0. **v0.3 的两个语义变化（从 v0.2 升级时最易踩）：**
   - `ui.Element` 是 **16 字节值句柄**（`frame.go`），不再是 `*ui.Element`；零值即"缺席"，
     判空用 `e.Valid()`（属于当前构建帧），构建器方法全部值接收、返回值。
   - **toggle 类 Base 的输入是延迟应用的**（`CheckboxBase`/`SwitchBase`/`ToggleBase`/
     `CollapsibleBase`/`RadioBase`/滑条键盘）：v0.2 在构建期当场翻转 `*on`，
     v0.3 挂 valueInput 回调，等**响应查询**才应用。所以
     `was := *x; Base(c, x); if *x != was` 这种括号式检测**永远看不到变化**——
     改为配置完控件后直接问 `el.Changed()`（文档原文："applies pending input …
     Configure controls before querying their response"）。
     Popover/菜单的开关（anchor `Clicked()`、backdrop 按压、Escape）仍在构建期，不受影响。
1. **它是原生窗口，不是网页。** `go run .` 需要图形环境；在 CI / 无显示环境里
   它是"跑不起来"或直接超时的，这**不代表代码有问题**。验证视图用无窗口方式：
   ```sh
   go test ./...                      # 内部用 ui.Render / ui.NewTester
   MYGO_UI_SHOTS=/tmp/shots go test ./internal/app -run TestScreenshots   # 输出每张界面 PNG
   ```
2. **视图是状态的纯函数，每帧调用一次**：事件处理器里改状态，下一帧呈现结果。
   **不要跨帧缓存 `*ui.Element`**；不要让 goroutine 直接碰元素（只能在 `Window.Update(func(){...})` 里改状态）。
3. **布局陷阱（本仓库踩过，务必避开）：**
   - `Grow(1)` **直接**放进 `ui.Scroll` 的 content 里 → 整个布局塌掉。
   - grow 元素放进"高度由内容撑开"的容器里 → 同样塌掉。
   - **`ui.Row` 的默认交叉轴对齐是 `Center`，不是 `Stretch`**（`ui.Column` 才是
     auto→Stretch）。Row 里要撑满高度的子元素（如 `Grow(1)` 的对话/列表容器），
     必须显式 `.AlignItems(ui.Stretch)`，否则子元素只按内容高度居中，里面的
     `Grow(1)` 塌成 0 → 整个区域不可见（Crux 的 chat 区就栽在这，见
     `shell.go` 的 `content()` 与 `TestChatPaneLayout`）。
   - `ui.List` 虚拟列表只构建视口内的行；`FollowEnd` 时首帧看到的是**末尾**行，
     顶部行滚上来才构建（`Find` 到 0 尺寸 / 找不到 ≠ 渲染 bug，先确认视口）。
   - 卡片 / 图表要从**有确定尺寸的 row** 里取 grow，**绝不从 scroll 里取**。
   - 需要占满高度的滚动区：外层给固定高度（`.Height(n)`）或用有确定尺寸的父 Box，再 `Grow(1).MinHeight(0)`。
4. **快捷键**：`if c.Shortcut(ui.Cmd, ui.KeyB) { ... }`。`ui.Cmd` 是平台主修饰键
   （macOS ⌘ / 其它 Ctrl），`ui.KeyA..KeyZ`、`ui.Key1` 等是键常量。
5. **局部状态**用 `ui.Local(c.Root(), "key", init)`；元素自身状态（焦点、滚动、编辑中的文本、动画）
   跟随其**兄弟位置**或 `Key`，换位置会丢状态。
6. **主题**：`c.Theme()` / `c.SetTheme(...)`；`ui.Render`、`ui.NewTester` 都能在测试里驱动。
7. **Tester API**：`ui.NewTester(view, w, h)` 有 `Click/Type/Key/HasText/Texts/Find/Frame/Image/RightClick`
   等；**没有** `Context()` / `Settle()`（要重绘用 `Frame()`）。

---

## 3. MujicaUI 的注意事项

1. **每个视图开头先装主题，再取令牌：**
   ```go
   core.Use(c, core.Settings{})   // 每帧一次，装主题（零值 = 库默认亮/暗）
   k := core.Tokens(c)            // 取色：Background/Surface/SurfaceHover/Text/TextMuted/
                                  // Border/Accent/OnAccent/Success/Warning/Danger/Info...
   ```
   颜色、字号、间距、动效时长一律走令牌与 `theme.*` / `core.Motion(c, ...)`；
   **不要自造收敛色板**（本模块的 `tokens.go` 就只做转发）。
2. **"契约式 panic" 是设计，不是 bug。** 组件对非法输入**直接 panic（fail fast）**。
   喂进去的数据必须**已经满足契约**：
   - `navigation.Command` 的 `ID`/`Label` 非空，`ID` 唯一（重复/为空会 panic）。
   - 列表项的 ID 必填（如 `ConversationList` 的会话 ID）。
   - 指针参数不能是 nil；枚举值要在允许集合内。
   渲染测试的目的之一就是把这些契约违规在 CI 里炸出来。
3. **调用前 `go doc` 拿准返回值类型**，它们形态不同，别猜事件方法：
   - `*ui.Element` → `Clicked()` / `Hovered()`（MyGo 风格）。
   - `core.ChoiceResult` → `Changed()`。
   - 命名的 `XxxResult` → 各自的事件方法，例如 `SendResult.Sent()`、
     `CommandPaletteResult.Chosen()`、`BranchSelectorResult.Changed()/Created()`、
     `MessageActionsResult.Action()`、`SuggestionChipsView.Chosen/Sent`。
4. **图标**：`icons.Must("name")` 对**未知名字 panic**。可用名字 = 内嵌的 Lucide 集合，
   列在模块的 `icons/svg/*.svg`。核实方式：
   ```sh
   go doc github.com/ZacharyZhang-NY/MujicaUI/icons   # Get / Must / Names
   # 或直接看 icons/svg 目录名（去 .svg）
   ```
   已知**不存在**的常见误写：`diff`、`sparkles`、`gauge`、`radio`、`file-diff`、`folder-git-2`；
   用 `git-pull-request`、`brain`、`sliders-horizontal`、`circle`、`file-text` 等替代。
5. **滚动 / 尺寸**：很多组件文档写明 "give it a height"（`git.DiffViewer`、`ChatContainer`、
   `code.CodeViewer`、`data.List`、`git.ChangesList`、`git.CommitList`…）。
   放进**固定尺寸的 Box** 或给 `.Height(n)`；**不要**丢进 scroll 里让它 grow（见 §2.3）。
6. **指针状态**：组件普遍用指针接收可变状态 —— `*bool`（open/on）、`*string`（value/query）、
   `*int`（索引/选中）、`*[]T`（多选/决策集）。零值即初始状态，通常直接取地址传入。
7. **列表类组件**需要 `data.ListState[K]`（`ChangeList` 用 `ChangesListState`，
   `BranchList`/`CommitList` 用 `data.ListState[string]`）。
8. **遇到"做不到"先查 `docs/upstream.md`** —— 那是上游 MyGo 的公开接口缺口
   （无障碍角色缺失、浮层焦点/快捷键不公开、Popover 只有上下两个位置、`activeDescendant`
   不公开……）。受影响组件的"限制"节会写明，别硬绕。

---

## 4. 工作流（怎么把坑降到最低）

1. **查文档**：`go doc <pkg> <sym>` 拿签名与 Options；需要行为/契约/限制时读
   `docs/components/NNN-*.md`。
2. **写代码**：状态放 `app`（或其等价物），事件改状态，视图读状态。
3. **编译**：`go build ./...`。
4. **加渲染测试兜底**——覆盖每种面板组合，捕捉布局 panic 与契约违规：
   ```go
   if ui.Render(a.view, 1280, 820, 1) == nil { t.Fatal("render nil") }
   ```
   交互测试用 `ui.NewTester`，断言**值**而非"元素存在"。
5. **校验**：`gofmt -l .`（应无输出）、`go vet ./...`、`go test ./...`。
6. **要真实运行**：`go run .`（需图形环境）。

---

## 5. 编码风格（对齐 MujicaUI 自身规则）

- 组件签名：`func Name(c *ui.Context, value *T, opts NameOptions) *ui.Element`；
  复合组件可返回命名 struct，且元素字段命名为 `Element *ui.Element`（不要内嵌）。
- 颜色：状态标记用 `OnAccent` on `Accent`，或 `AccentText`；**深色底上别单用 `Accent`**。
- 动效时长：`core.Motion(c, theme.StateDuration)`；弹层用 `theme.PopupDuration`。
- `Disabled` 在组件内先于输入处理生效；**不要**再叠 opacity。
- 不跨帧缓存 `*ui.Element`，不让 goroutine 碰元素。
- **fail fast**：程序错误就 panic，别吞。文件保持 **< 500 行**。注释一行、只在必要时写。

---

## 6. 本仓库内的参考与产物

| 路径 | 内容 |
| --- | --- |
| `README.md` | 项目说明与截图；`MYGO_UI_SHOTS` 截图用法 |
| `SPEC.md` | Crux 的完整规格文档 |
| `*.go` | UI 层在 `internal/app/`（数据/逻辑层在 `internal/store`、`internal/engine`），各文件职责见 SPEC §4；`tokens.go` 演示如何只做主题转发 |
| `internal/sandbox/` | bash 工具的执行边界（Seatbelt / bubblewrap）：workdir 读写 + scratch HOME + 断网 + 凭据目录遮蔽；无后端平台报错不降级。开关在设置 Agent 面板（`LLMConfig.Sandbox`，nil=默认开） |
| `screenshots/` | 渲染测试产出的界面截图 |

组件级的参考实现（agent / chat / code / git 各组件的直接用法）见
[mygo-dashboard](https://github.com/HycJack/mygo-dashboard) 仓库的 `pages_mujica_*.go`。

**改 Crux（本仓库）**：先读 `SPEC.md`，再动手。

---

## 7. 一句话速查

- 签名 → `go doc`；行为/契约/限制 → `docs/components/NNN-*.md`；上游缺口 → `docs/upstream.md`。
- 视图开头 `core.Use(c, core.Settings{})` + `k := core.Tokens(c)`，颜色走令牌。
- 图标先核对 `icons/svg`，`Must` 会 panic。
- 滚动里**不要**放 grow 元素；需要高度的组件给它固定尺寸。
- 数据必须满足组件契约（否则 panic）——**用渲染测试兜底**。
- 无窗口验证用 `ui.Render` / `ui.NewTester`；`go run .` 需要图形环境。
