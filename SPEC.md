# MujicaUI-agent-demo（Atlas）— 规格文档（Spec）

> 一个用 MyGo 原生 UI 工具包 + MujicaUI 组件库实现的 **Codex 风格编码 Agent 桌面应用**。
> 全窗口 GPU 自绘：无 WebView、无 HTML、无 JavaScript。

本文档描述 `MujicaUI-agent-demo` 模块的完整规格：目标、架构、状态模型、界面、交互、主题、
构建与测试。它既是实现说明，也是后续迭代的契约。

---

## 1. 概述

Atlas 把一次编码会话摊开成三块：

- **会话栏（左）** —— 历史会话索引，可折叠。
- **工作区（中）** —— 与 Agent 的对话线程：用户输入、Agent 的思考 / 正文 / 工具调用 / 终端运行，
  下方是输入框（composer）。
- **仓库检视器（右）** —— 分支切换、工作区变更、提交历史，以及所选文件的 Diff / 源码。

它复用 MujicaUI 的 `chat`、`agent`、`git`、`code`、`layout`、`navigation`、`icons`、
`core`、`input`、`account` 包，外壳由 MyGo 的 `ui` 弹性布局原语拼装。对话回复
由 **pi-ai-go**（统一多模型 LLM SDK）真实驱动，支持流式文本、思维链与工具调用；
后端与 Agent 行为在一个 **Settings 模态框**里配置：左侧源列表（Providers / Agent），
右侧当前分区。

### 1.1 目标

- 展示一个 **真实可交互** 的 Agent 控制台，而非静态组件画廊。
- 每一个可见控件都绑定行为（发送、切换、复制、切分支、切面板、切模型……）。
- 全程使用 MujicaUI 的语义组件与主题令牌，不自造调色板。
- 单一窗口，`go run .` 即起；对话走 pi-ai-go 的真实 LLM 流式回复。
- 提供 **Settings 模态框**（`overlay.Dialog`）：左侧源列表切换 Providers（后端/模型/Key/URL，
  支持 OpenAI 兼容端点与拉取模型列表）与 Agent（系统提示/推理/采样/限额）两个分区。
  配置保存在 app 状态、每次调用时传入，不写磁盘。

### 1.2 非目标

- 不做真实 git 操作（提交、暂存、拉取）。仓库数据是 mock 的。
- 不做持久化（除窗口尺寸等由 MyGo `StateKey` 处理的偏好）。配置保存在
  app 状态里，运行期内存持有；"Export / Import" 等为演示性提示。
- 不做文件系统读写；Agent 工具能力沿用 pi-ai-go 的基础形态（对话 + 思维链），
  未接入文件系统/命令行沙箱工具。

---

## 2. 运行与构建

模块路径：仓库根目录（`MujicaUI-agent-demo/`），包名 `main`。

```sh
go run .            # 运行（打开窗口）
go build -o atlas.exe .   # 构建可执行文件
go test ./...       # 渲染 + 状态逻辑测试
go vet ./...        # 静态检查
gofmt -l .          # 期望无输出
```

发布打包（可选，见 `mygo.json`）：

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

### 2.1 窗口参数

| 项 | 值 |
| --- | --- |
| 标题 | `Atlas — coding agent` |
| 初始尺寸 | 1280 × 820 |
| 最小尺寸 | 1024 × 640 |
| `StateKey` | `atlas-app`（记忆窗口位置/状态） |
| 内容 | `ui.View(a.view)` |

启动时另起一个 goroutine，每秒调用 `win.Update(func(){})`，让状态栏的 “live” 指示与
时间相关渲染保持心跳。

### 2.2 `mygo.json`

```json
{
  "name": "mujicaui-agent-demo",
  "identifier": "com.example.mujicauiagentdemo",
  "version": "0.1.0",
  "out": "build"
}
```

---

## 3. 依赖

`go.mod`：

```
module mujicaui-agent-demo

go 1.27.1

tool github.com/egoist/mygo/cmd/mygo

require (
    github.com/HycJack/pi-ai-go v0.3.0
    github.com/ZacharyZhang-NY/MujicaUI v0.0.0-20261005202320-8c561ba15ede
    github.com/egoist/mygo v0.2.18
)
```

间接依赖：`ebitengine/purego`、`go-text/typesetting`、`go-text/typesetting-utils`、
`golang.org/x/image`、`golang.org/x/text`。

- **MujicaUI** —— 组件库（chat / agent / git / code / layout / navigation / icons / core / theme / data / input / account）。
- **MyGo (`github.com/egoist/mygo`)** —— 原生 UI 与窗口运行时；提供 `ui.Context`、`ui.Element`、
  弹性布局、`ui.View`、`ui.Tester`、`ui.Render`。
- **pi-ai-go** —— 统一多模型 LLM SDK：`piai.StreamSimpleWithContext` / `Complete` 做流式补全与
  一次性补全；`piai.GetProviders` / `GetModels` / `GetModel` 读内置模型注册表；
  `_ "pi-ai-go/providers"` 的 `init()` 注册全部内置 Provider（OpenAI / Anthropic / Google /
  Bedrock / DeepSeek / GLM / Kimi 等）。以独立模块 `github.com/HycJack/pi-ai-go` 引入。

---

## 4. 模块结构

| 文件 | 职责 |
| --- | --- |
| `main.go` | 入口：`newApp()`、窗口创建、`a.redraw = win.Update` 接线、心跳 goroutine、`App.Run()`。 |
| `state.go` | 数据模型：`app` / `thread` / `row` / `repo` / `file` / `session` / `LLMSettings`，种子数据；`settingsOpen`/`settingsTab` 与 `closeModals`/`openProviders`/`openAgent`。 |
| `shell.go` | 外壳布局：标题栏（含 Provider/Agent 设置入口）、会话栏、工作区、状态栏；快捷键；会话切换。 |
| `thread.go` | 对话线程：`threadView`、`renderRow`、`actions`、`composer`（展示 `backendLabel`）。 |
| `llm.go` | **pi-ai-go 集成层**：`resolveModel`（含 OpenAI 兼容端点直建模型）、`historyMessages`、`send`、`startStream`（goroutine 流式）、`streamOptions`、错误/中止处理。 |
| `settings.go` | **Settings 模态框**：`settingsDialogs` + `settingsBody`（左侧源列表 `settingsNavItem` 切换分区）、`providersPane`（provider/model/Key/BaseURL，OpenAI 兼容端点、`fetchModels` 拉取模型、TreeSelect 选模型、Test connection）、`agentPane`（系统提示/推理/温度/限额）。 |
| `welcome.go` | 新会话欢迎页：能力卡 + starter chips + composer。 |
| `repo.go` | 仓库检视器：分支选择、变更列表、提交历史、Diff / 源码切换。 |
| `tokens.go` | 主题接入：`tokens(c)`、`useTheme(c)`、`tokensT` 别名。 |
| `commands.go` | ⌘K 命令面板与 `runCommand`（含 providers / agent 命令）。 |
| `main_test.go` | 测试：各视图渲染、发送、会话切换、命令面板、Provider 重绑、Agent 配置、Settings 模态框渲染。 |
| `settings_nav_test.go` | 测试：Settings 左侧源列表切换分区（Providers→Agent）。 |
| `openaicomp_test.go` | 测试：OpenAI 兼容端点 `resolveModel`、模型树节点、headless 拉取。 |
| `mygo.json` | 打包元数据。 |
| `resources/icon.png` | 应用图标。 |

渲染约定：`app.view` 是 MyGo 的视图函数——**一帧一次**，由 MyGo 在输入后、
`Window.Update` 后、动画中重复调用。视图是状态的纯函数；事件处理器改写状态，下一帧呈现结果。

---

## 5. 数据模型（`state.go`）

### 5.1 `app` —— 顶层状态

```go
type app struct {
    thread    thread                    // 当前会话的对话线程
    repo      repo                      // 仓库检视器状态
    conv      chat.ChatConversation     // 当前会话（供 ConversationItem 等使用）
    convList  chat.ConversationListState// 会话列表的选择/滚动
    sessionID string                    // 当前会话 id
    sessions  []session                 // 会话索引
    threads   map[string]thread         // 按会话缓存的工作线程（切走再切回保留草稿）

    // shell
    navOpen     bool                    // 左栏是否展开
    paletteOpen bool                    // ⌘K 命令面板是否打开
    nextID      int                     // 新建会话的自增序号
}
```

`newApp()` 构造初始状态：默认会话 `c9`，`navOpen=true`，`repo.branch="main"`，
`thread=seededFor("c9")`。

### 5.2 `row` / `kind` —— 对话行

```go
type kind int
const (
    rowPlain     kind = iota // 纯文本（MarkdownView）
    rowReasoned              // 思考块 + StreamingText
    rowTools                 // 工具调用 + 文件变更卡
    rowTyping                // "thinking…" 指示
    rowCommand               // 终端运行卡
    rowDiff                  // 多文件 Diff 评审
)

type row struct {
    id   string
    role chat.MessageRole
    kind kind
    text string
    at   time.Time
}
```

> `rowTyping` / `rowCommand` / `rowDiff` 为可扩展形态：渲染分支已就绪，当前默认流程
> 主要产出 `rowPlain` / `rowReasoned` / `rowTools`。

### 5.3 `thread` —— 对话线程状态

```go
type thread struct {
    rows   []row
    list   chat.MessageListState
    draft  string
    mode   chat.ChatMode
    model  string
    rating chat.MessageFeedback
    think  bool
    ctx    []chat.ContextItem
    plan   agent.FileDecision   // FileChangeCard 的抉择
    do     agent.FileDecision
    cmd    agent.CommandRun      // 终端运行数据
    diff   []agent.FileDecision  // MultiFileDiffReview 的抉择
    run    []agent.CommandRun
}
```

### 5.4 `seededFor(id)` —— 每会话的种子线程

- `c9` → 完整的“夜间导出复盘”线索（`seeded()`）：两天历史、思考块、工具卡、多轮对话。
- `c4` → “归档保留策略”问答（2 行）。
- `c1` → “座位宏”线索，模式为 `ModeChat`、模型 `swift`（2 行）。
- 其它 id（新建会话）→ 空线程 = 欢迎页。

### 5.5 `repo` —— 仓库检视器状态

```go
type repo struct {
    changes  git.ChangesListState
    branches data.ListState[string]
    commits  data.ListState[string]
    diff     git.DiffViewerState
    dst      code.CodeViewerState
    branch   string
    showRepo bool
    codeTab  int // 0 = 工作区 Diff，1 = 文件源码
}
```

### 5.6 `file` —— 变更文件模型

```go
type file struct {
    path   string
    status git.GitStatus
    from   string // 旧文本（Diff 的 from）
    to     string // 新文本（Diff 的 to / 源码视图）
    lang   string // 语法高亮语言：bash / go / markdown
}
```

`demoFiles()` 返回 3 个变更文件；`changedFiles()` 投影为 `[]git.ChangedFile`
（第 0 个置为 staged），索引与 `demoFiles()` 对齐，因此变更列表的选中项能直接定位 Diff/源码。

### 5.7 `session` 与种子

`session{id,title,updated,pinned}`；`seedSessions()` 返回稳定可变的会话索引（最新在前）。

---

## 6. 界面布局（`shell.go`）

`app.view(c)` 的层级：

```
Column (Fill, Background)
├── titlebar(c,k)                    layout.TitleBar
├── Row (Grow 1, Stretch)
│   ├── [navOpen] sidebar(c,k)       会话栏，宽 260
│   ├── [navOpen] Box 宽 1           分隔线
│   └── Column (Grow 1, MinWidth 0)
│       └── content(c,k)
│           └── Row (Grow 1)
│               ├── Column (Grow 1, MinWidth 0, Padding 12) → threadView
│               ├── [showRepo] Box 宽 1 分隔线
│               └── [showRepo] repoPane(c,k)  宽 380
└── statusbar(c,k)                   layout.StatusBar
+ shortcuts(c) + palette(c)          覆盖层/快捷键（每帧处理）
```

### 6.1 标题栏 `titlebar`

- `layout.TitleBar(c, "Atlas", {Leading, Trailing})`。
- Leading：`bot` 图标 + "Atlas" 字标 + 会话栏折叠按钮（`menu`）。
- Trailing：当前模型名 + 仓库检视器折叠按钮（`git-pull-request`）+ 头像。

### 6.2 会话栏 `sidebar`

- 宽 260、`Surface` 底色、内边距 10。
- 顶部 `ui.PrimaryButton("New chat")` → `newThread()`。
- `chat.ConversationList(&a.convList, items, {Label:"Sessions"}, nil)`：
  - `.Changed()` → `openSession(a.convList.Selected)`。
  - `.Submitted()` → toast。

### 6.3 状态栏 `statusbar`

`layout.StatusBar`：

- 左：分支（`git-branch`）、模式（`brain`）、上下文用量（`sliders-horizontal`，文本 `6.4k / 8k tokens`）。
- 右：`live`（`circle`，`StatusItemSuccess`）、`3 changed`（`file-text`，`StatusItemWarning`）。

### 6.4 图标按钮 `iconToggle`

`ui.ButtonBase` + `Label`/`Tooltip`/`Size(28,28)`/`Radius(7)`，悬停着色，点击执行回调，
内部画一个 `ui.Icon`。

### 6.5 会话生命周期

- **`newThread()`** —— `saveSession()` → 生成 `new-N` id → 选中它 → 置空线程 →
  在索引头部插入 `New chat N` 会话。
- **`openSession(id)`** —— 同 id 直接返回；否则 `saveSession()` 后切换 id，命中
  `threads[id]` 则恢复，否则 `seededFor(id)`。
- **`saveSession()`** —— 把当前 `thread` 存回 `threads[sessionID]`。

---

## 7. 对话线程（`thread.go`）

### 7.1 `send()`（pi-ai-go 流式）

`send()` 现由 `llm.go` 提供，走真实 LLM：

1. 取 `strings.TrimSpace(draft)`，空或 `a.llm.Busy` 则返回。
2. 追加用户行（`rowPlain`）。
3. 追加一条空的 `rowReasoned` 助理行，`list.ScrollToEnd()`。
4. `startStream(userIdx)`：置 `a.llm.Busy=true`，建 `context.WithCancel` 存到
   `a.cancelFn`，起一个 goroutine：
   - `resolveModel()` → `piai.GetModel`（含 BaseURL 覆盖）。
   - `piai.StreamSimpleWithContext(ctx, model, {SystemPrompt, Messages}, opts)`，
     `opts` 由 `streamOptions()` 组装（`APIKey`、`MaxTokens`、`Reasoning`、`Temperature`）。
   - `stream.ForEach`：`EventTextDelta` 累加并**按值**回传 UI 线程（`a.redraw`）增量刷新该行；
     `EventThinkingDelta` 累加思维链；`EventDone` 落定正文与 `thinkText`；`EventError` 报错中止。
   - 完成后 `a.llm.Busy=false`、清 `cancelFn`。

> 线程约定：UI 线程持有全部状态。goroutine 只缓冲局部字符串，经
> `a.redraw(fn)`（即 `win.Update`）把 `fn` 调度回 UI 线程再改状态；闭包**不能**捕获
> `strings.Builder`（`win.Update` 不等待 `fn`，闭包可能在 goroutine 退出后执行）。
> `a.redraw==nil`（无窗口 / 测试）时 `send()` 同步落定占位文本，测试保持确定性。

### 7.1.1 Settings 模态框（Providers / Agent）

配置是一个**模态对话框**（`overlay.Dialog`，宽 780），由 `settingsDialogs(c)` 在
`view()` 末尾构建。`a.settingsOpen` 控制开合，`a.settingsTab`（`"providers"` /
`"agent"`）决定右侧分区；`openProviders()` / `openAgent()` 打开并定位到对应分区。
标题栏两个图标按钮（`Provider settings` / `Agent settings`）与 ⌘K 的
`providers` / `agent` 命令打开对话框；关闭走组件自带机制——右上角 ✕、点遮罩、Escape。

**正文 `settingsBody(c)`**：`ui.Row(AlignItems Start)` → 左侧源列表（`ui.Column` 宽 180，
两条 `settingsNavItem` 可点行，选中项用 `Selection` 底 + `AccentText`）+ 1px 分隔条 +
右侧 `ui.Column(Grow 1, MinWidth 0)` 按 `a.settingsTab` 渲染 `providersPane` / `agentPane`。
选左侧行只切分区、不关对话框。

**Providers 分区 `providersPane(c)`**（编辑 `a.llm`，值即时生效）：
- **Provider**：`input.Select`，选项 = `openai-compatible`（自定义端点）+ pi-ai 内置注册表。
- **Base URL**：`input.InputGroup`。已知 provider 可留空走默认；OpenAI 兼容必填。
- **API key**：`input.InputGroup`。
- **Model**：`input.TreeSelect`（单选 `input.TreeSelect`，节点来自注册表模型；切 provider 后
  `rebindModel()` 重挂当前模型）。
- **Fetch models**（自定义端点或填了 Base URL 时出现）：`fetchModels()` 在 goroutine 里
  `GET {base}/models`（`Authorization: Bearer`），解析 `data[].id` 排序后写回
  `provView.fetched`，经 `a.redraw` 回到 UI 线程；TreeSelect 节点即该列表。
- **Test connection**：`testConnection()` 走 `piai.Complete` 单轮连通性检查。

**OpenAI 兼容端点**：`resolveModel()` 对 `openai-compatible` 直接构建
`piai.Model{ID, Name, Provider: piai.ProviderOpenAI, API: piai.APIOpenAICompletions,
BaseURL: 去尾斜杠的用户 URL, ContextWindow: 8192}`（不走注册表）；Base URL 或模型为空报错。
推理层级对自定义端点默认 `none`（注册表查不到推理能力）。

**Agent 分区 `agentPane(c)`**：系统提示、推理层级、温度、最大 token、流式输出，
同前（编辑 `a.llm`）。

**选 session 永远回到对话**：`openSession` 先 `closeModals()` 再切转录，
所以开着设置框点左侧 session 也能正常切换并关掉对话框；`newThread` 同样 `closeModals()`。

### 7.2 `threadView(c)`

- 外层 `ui.Box(Grow 1, MinHeight 0, Border, Clip)`。
- `len(rows)==0` → `welcome(c,k)`。
- 否则 `chat.ChatContainer(&a.thread.list, n, opts, rowFn)`：
  - `List{ID, Date, Label}` —— 稳定 id、时间（驱动日期分隔）、无障碍标签。
  - `Input` → `composer(c)`。
  - 每行 `renderRow`。

### 7.3 `renderRow(c, r)`

按 `kind` 渲染：

| kind | 组件 |
| --- | --- |
| `rowTyping` | `chat.TypingIndicator(c, "Atlas")` |
| `rowTools` | `MessageBubble` → `agent.ToolCallCard` + `agent.FileChangeCard`（`Preview:4`） |
| `rowCommand` | `MessageBubble` → `agent.CommandExecutionCard`（可中止：改 `ExitCode=130` 并追加中止输出） |
| `rowDiff` | `MessageBubble` → `agent.MultiFileDiffReview(&diff, filesChanged, …)` |
| `rowReasoned` | `MessageBubble` → `chat.ThinkingBlock(&think,…)` + `chat.StreamingText(Streaming:true)` + `actions` |
| 默认 | `MessageBubble` → `chat.MarkdownView(text)` + `actions` |

助理消息的 `MessageBubbleOptions{Name:"Atlas"}`。

### 7.4 `actions(c, r)`

仅助理行显示 `chat.MessageActions(&rating)`：
- `ActionCopy` → `c.WriteClipboard(r.text)` + toast。
- `ActionRegenerate` → toast。

### 7.5 `composer(c)`

```
Column(Gap 8)
├── chat.ContextChips(&ctx, {Max:2})        可移除上下文芯片
├── chat.PromptComposer(&draft, {
│       Placeholder,
│       Tools:  () -> chat.ModeSelector(&mode),        模式选择（chat/agent）
│       Actions:() -> chat.SendButton({Shortcut:"Enter", Disabled: draft 空})
│   })  → Submitted() 也触发 send
└── Row(Wrap)
    ├── chat.ModelSelector(&model, [Atlas 4, Swift Mini])
    └── chat.TokenCounter(6400, 8000)
```

---

## 8. 欢迎页（`welcome.go`）

`len(rows)==0` 时显示：

```
Column(Fill)
├── Scroll(Grow 1, Padding 28)
│   └── chat.WelcomeScreen({
│         Greeting:"Good evening",
│         Subtitle:…,
│         Cards:[
│           Investigate（search 图标）：日志/追踪/指标示例
│           Change code （pencil 图标）：补丁/Diff/测试示例
│         ],
│         Prompts: () -> chat.SuggestionChips(&draft, […], {Label:"Starters", Send:true})
│       })
│       └── v.Example 非空 → 填入 draft
└── Column(Padding 12) → composer(c)
```

- 能力卡示例点击后写入 `draft`。
- starter chips 设为点击即发送（`Send:true`）：`chips.Sent && Chosen!=""` → 设 draft 并 `send()`。

---

## 9. 仓库检视器（`repo.go`）

`repoPane(c,k)`，宽 380、`Surface` 底色，自顶向下：

1. **头部**：`git-branch` 图标 + "Repository" 标题。
2. **分支选择**：`git.BranchSelector(&branch, branchList(), {AllowCreate:true})`。
   - `.Changed()` → toast “Switched to …”。
   - `.Created()` → toast “Created branch …”。
3. **Changes**：`git.ChangesList(&changes, changedFiles(), {})`，高 96。
4. **History**：`git.CommitList(&commits, commitList(), {})`，高 96。
5. **底部面板**：
   - 选中文件标题行：`git.GitStatusBadge(status)` + 路径。
   - `ui.Segmented(&codeTab, "Diff", "Source")`（宽 160）。
   - `ui.Box(Grow 1, MinHeight 140, Clip)` 内：
     - `codeTab==1` → `code.CodeViewer(splitLines(f.to), &dst, {Language:f.lang, Label:f.path})`。
     - 否则 → `git.DiffViewer(&diff, f.from, f.to, {Language:f.lang})`。

选中项取 `a.repo.changes.Selected()`，越界回退到 0，保证面板不空。

`splitLines` 去掉末尾换行后按 `\n` 切分，供源码视图使用。

### 9.1 mock 数据

- `branchList()`：`main`(current, 上游 origin/main, Ahead 1)、`fix/export-quota`、
  `origin/main`、`origin/release`。
- `commitList()`：3 条提交，含 Refs/作者/时间。
- `demoFiles()`：`jobs/export-nightly.sh`、`jobs/quota.go`、`docs/runbook.md`。
- `filesChanged`（`thread.go`）：供线程内 `MultiFileDiffReview` 使用的变更集。

---

## 10. 主题（`tokens.go`）

- `tokensT = theme.Tokens`：主题令牌别名，签名从此取色。
- `tokens(c)` → `core.Tokens(c)`：当前主题令牌。
- `useTheme(c)` → `core.Use(c, core.Settings{})`：每帧安装 MujicaUI 主题（沿用库默认亮/暗）。

配色全部走令牌（`Background/Surface/SurfaceHover/Text/TextMuted/Border/Accent/Success/Warning/...`），
不自造 `ui.Hex` 调色板。

---

## 11. 命令与快捷键（`commands.go` + `shell.go`）

### 11.1 快捷键（`shortcuts(c)`，每帧绑定）

| 组合 | 动作 |
| --- | --- |
| `⌘/Ctrl + B` | 折叠/展开会话栏 |
| `⌘/Ctrl + J` | 折叠/展开仓库检视器 |
| `⌘/Ctrl + N` | 新建会话 |
| `⌘/Ctrl + K` | 打开命令面板 |

> MyGo 用 `ui.Cmd` 表示平台主修饰键（macOS 为 ⌘，其它为 Ctrl）。

### 11.2 命令面板（`palette(c)`）

`navigation.CommandPalette(&paletteOpen, cmds, {})`，`.Chosen()` 触发 `runCommand`。

命令清单：

| id | 标签 | 分组 | 行为 |
| --- | --- | --- | --- |
| `new` | New chat | Session | `newThread()` |
| `export` | Export conversation | Session | 返回 toast “Conversation exported (demo)” |
| `toggle-nav` | Toggle sessions | View | 翻转 `navOpen` |
| `toggle-repo` | Toggle repo inspector | View | 翻转 `showRepo` |
| `diff` | Show working diff | Repo | `showRepo=true, codeTab=0` |
| `source` | Show file source | Repo | `showRepo=true, codeTab=1` |
| `branch` | Switch branch… | Repo | `showRepo=true` + toast |

`runCommand(id) string` **不接收 context**（便于单测）：执行状态变更并返回需弹出的 toast 文案（无则返回 `""`）。调用方 `palette` 负责 `c.Toast(msg)`。

---

## 12. 交互清单（验收点）

- [ ] 输入一句话回车 → 追加用户行 + 思考/正文 + 工具行，列表滚到底。
- [ ] `New chat` / ⌘N → 清空到欢迎页；能力卡示例可填入草稿；starter chip 可即发。
- [ ] 会话列表切到 `c4` / `c1` → 内容随之变化；切走再切回保留草稿。
- [ ] ⌘B / ⌘J 折叠左栏 / 右栏；标题栏两个按钮同效。
- [ ] ⌘K 打开面板；7 条命令各自生效。
- [ ] 仓库面板：切分支有 toast；Changes 选中项驱动底部面板；Diff/Source 分段切换渲染。
- [ ] 助理消息：复制写入剪贴板；重新生成有反馈。
- [ ] 终端运行卡在 `rowCommand` 形态下可中止。

---

## 13. 上游组件映射

| 区域 | MujicaUI 组件 |
| --- | --- |
| 外壳/标题/状态 | `layout.TitleBar`、`layout.StatusBar` |
| 会话列表 | `chat.ConversationList` |
| 对话容器/列表 | `chat.ChatContainer`、`chat.MessageList`(+Options) |
| 消息体 | `chat.MessageBubble`、`chat.MarkdownView`、`chat.StreamingText`、`chat.ThinkingBlock`、`chat.TypingIndicator` |
| 消息操作 | `chat.MessageActions` |
| 输入区 | `chat.PromptComposer`、`chat.SendButton`、`chat.ModeSelector`、`chat.ModelSelector`、`chat.ContextChips`、`chat.TokenCounter` |
| 欢迎 | `chat.WelcomeScreen`、`chat.SuggestionChips`、`chat.CapabilityCard` |
| Agent 卡 | `agent.ToolCallCard`、`agent.FileChangeCard`、`agent.CommandExecutionCard`、`agent.MultiFileDiffReview` |
| 仓库 | `git.BranchSelector`、`git.ChangesList`、`git.CommitList`、`git.DiffViewer`、`git.GitStatusBadge` |
| 代码 | `code.CodeViewer` |
| 命令面板 | `navigation.CommandPalette` |
| 图标/主题 | `icons.Must`、`core.Use`、`core.Tokens` |
| 布局原语 | MyGo `ui.Row/Column/Box/Scroll/Segmented/Button/ButtonBase/PrimaryButton/Icon/Text/Avatar` |

> 约束：所有固定数据一旦传入组件就必须满足其契约（否则 MujicaUI 会 panic）。
> 例如 `ConversationList` 的 ID、`ChangesList` 的文件、`CommandPalette` 的 `Command.ID`。

---

## 14. 测试（`main_test.go`）

在无窗口环境下用 `ui.Render(view, w, h, scale)` / `ui.NewTester(view, w, h)` 驱动。

1. **`TestViewRenders`** —— 对 `showRepo ∈ {false,true}` × `codeTab ∈ {0,1}` 共 4 种组合渲染，
   断言非 nil（捕捉布局 panic / 组件契约缺失）。
2. **`TestSendAndNewChat`** —— `send()` 追加恰 3 行、清空草稿、末行是 `rowTools`；
   `newThread()` 后行数为 0 且欢迎页可渲染。
3. **`TestSessionSwitch`** —— `openSession("c4")` 载入 2 行；改动 `c4` 草稿、`saveSession`、
   切回 `c9`、再切回 `c4` → 草稿保留。
4. **`TestPaletteCommands`** —— 逐个执行 7 条命令且每步可渲染；`diff`/`source` 分别落到
   正确的 `showRepo`/`codeTab`。

---

## 15. 已知限制与后续

**限制**

- Agent 回复走 pi-ai-go 真实 LLM，但为**单轮补全**：历史由 `historyMessages()` 从
  对话行重建（仅 user/assistant 文本行），工具调用结果暂不回灌到 LLM 历史。
- 配置（Provider/Model/Key/BaseURL/系统提示等）保存在 app 状态，运行期内存持有，
  不写磁盘；重启即回退默认。
- 无窗口（测试）环境 `send()` 落定占位文本，不做真实网络调用。
- git 操作只读且为 mock；分支/提交数据不随选择改变源。
- `StatusBar` 的上下文用量、变更数为固定文案。
- 「Export / Import」为演示提示，无实际 IO。

**可扩展方向**

- 变更列表接入 stage/unstage 动作（`git.ChangesList` + `git.GitListResult.Action`）。
- 在仓库面板加入 `code.ProblemsPanel` / `code.OutputPanel` / `code.Terminal`。
- 借助 `core.Settings{Light/Dark}` 做运行时亮暗切换按钮。
- 用 `chat.ConversationSearch` 给会话栏加搜索，`chat.ConversationItem` 支持重命名/置顶/删除。
- 接入 pi-ai-go 的 `agent` 层完整工具循环（read/write/bash 等沙箱工具），把
  `send()` 的单轮补全升级为多轮 Agent 循环，并在 `rowTools`/`rowCommand` 里呈现工具事件。
- 将 API Key 以系统 Keychain / 加密形式持久化，而非明文存 app 状态。