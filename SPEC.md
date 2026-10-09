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
- **检视器（右）** —— 两个标签：**Workspace**（真实目录树，懒加载，点选文件在下方预览源码）
  与 **Repository**（分支切换、工作区变更、提交历史，以及所选文件的 Diff / 源码）。

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
  配置保存在 app 状态、每次调用时传入，并在对话框关闭时持久化到用户配置目录
  （`settings.json`，见 `config.go`）。

### 1.2 非目标

- git 走**真实命令**（status / diff / log / stage / unstage / commit / checkout），
  但不做 push / pull / fetch 等远端操作，也不处理合并冲突；线程内的多文件
  Diff 评审卡仍是演示数据。
- LLM 配置持久化到用户配置目录（`settings.json`）；工作区选择与全部会话/对话记录
  持久化到同目录（`workspace.json` / `sessions.json`）；"Export / Import" 等仍为
  演示性提示。
- 工作区树对真实文件系统**只读**（列目录 + 读文件预览 + 附加到对话）；Agent 工具
  能力沿用 pi-ai-go 的基础形态（对话 + 思维链），未接入文件系统/命令行沙箱工具。

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
| `main.go` | 入口：`newApp()`、`loadSettings()`、窗口创建、`a.redraw = win.Update` 接线、心跳 goroutine、`App.Run()`。 |
| `state.go` | 数据模型：`app` / `thread` / `row`（含 `llmText`）/ `repo`（含 `vcs`）/ `session` / `LLMSettings`，种子数据；`settingsOpen`/`settingsTab` 与 `closeModals`/`openProviders`/`openAgent`。 |
| `shell.go` | 外壳布局：标题栏（含 Provider/Agent 设置入口）、会话栏、工作区、状态栏；快捷键；会话切换。 |
| `thread.go` | 对话线程：`threadView`、`renderRow`、`actions`、`composer`（展示 `backendLabel`）。 |
| `llm.go` | **pi-ai-go 集成层**：`resolveModel`（含 OpenAI 兼容端点直建模型）、`historyMessages`（用户行优先取 `llmText`）、`send`（附件文件内容折叠进消息）、`attachedFilesBlock`、`startStream`（goroutine 流式）、`streamOptions`、错误/中止处理。 |
| `settings.go` | **Settings 模态框**：`settingsDialogs` + `settingsBody`（左侧源列表 `settingsNavItem` 切换分区、正文 `PaddingX(22)` 留白）、`providersPane`（provider/model/Key/BaseURL + **Reasoning 四档** `reasoningTiers`，OpenAI 兼容端点、`fetchModels` 拉取模型、TreeSelect 选模型、Test connection）、`agentPane`（系统提示/温度/限额）。 |
| `config.go` | **配置持久化**：`settingsFile`/`loadSettings`/`saveSettings`，`settings.json` 读写与回退。 |
| `welcome.go` | 新会话欢迎页：能力卡 + starter chips + composer。 |
| `workspace.go` | **工作区**：真实目录树的状态与 IO——`listDir`（目录在前、忽略噪音）、`loadWsDir` 懒加载（goroutine + `a.redraw`）、`readCapped` 文件预览（256 KiB 上限）、`previewLang` 高亮映射、`attachFile` 附加到对话、`reloadWorkspace`。 |
| `wsstore.go` | **工作区存储**：`homeDir` 默认根、`workspace.json`（当前工作区 + recents）、`sessions.json`（按工作区分组的全部会话与对话记录）、`openWorkspace` 切换、`restoreSession`、Open-workspace 对话框。 |
| `vcs.go` | **真实 git 后端**：`runGit`（15s 超时）、`parseStatus`/`parseBranches`/`parseLog`（porcelain 解析）、`collectVCS` 快照、`fileVersions`（HEAD / index / worktree 三方取版本，二进制探测）、`vcsAction`（stage/unstage）、`commitStaged`（含 amend）、`checkoutBranch`/`createBranch`、`loadSelectedDiff`。 |
| `repo.go` | 右栏检视器：Workspace 标签（目录树 + 文件预览 + 附加按钮）与 Repository 标签（真实分支切换、暂存/未暂存变更、提交输入、历史、Diff / 源码）。 |
| `tokens.go` | 主题接入：`tokens(c)`、`useTheme(c)`、`tokensT` 别名。 |
| `commands.go` | ⌘K 命令面板与 `runCommand`（含 providers / agent / workspace / reload-workspace 命令）。 |
| `main_test.go` | 测试：各视图渲染、发送、会话切换、命令面板、Provider 重绑、Agent 配置、Settings 模态框渲染。 |
| `workspace_test.go` | 测试：目录列举排序/忽略、懒加载与失败态、预览读取/截断/错误、树渲染与点击联动、workspace 命令。 |
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
    sessions  []session                 // 会话索引（跨工作区全量，按工作区过滤展示）
    threads   map[string]thread         // 按会话缓存的工作线程（切走再切回保留草稿）

    // workspace store（wsstore.go）
    recents      []string // 最近打开的工作区，最新在前（上限 6）
    wsDialogOpen bool     // Open-workspace 对话框
    wsPathField  string   // 对话框里的目录输入框

    // shell
    navOpen     bool                    // 左栏是否展开
    paletteOpen bool                    // ⌘K 命令面板是否打开
    nextID      int                     // 新建会话的自增序号
}
```

持久化路径字段（`configPath` / `sessionsPath` / `wsPrefsPath`）为空时禁用对应
存储 —— `newApp()` 只设 `configPath`，`sessionsPath`/`wsPrefsPath` 由 `main.go`
接线，因此测试里的 `newApp()` 从不碰盘。

`newApp()` 构造初始状态：默认会话 `c9`，`navOpen=true`，`repo.branch="main"`，
`thread=seededFor("c9")`；工作区默认 `homeDir()`（用户主目录，**不是**可执行文件
所在目录），并 `seedSessions(a.ws.root)` 生成首跑演示会话。`main.go` 随后
`loadWsPrefs()` → `loadSessions()`（无存储时落种子并立即持久化）→
`restoreSession()`。

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

### 5.5 `repo` —— 检视器状态

```go
type repo struct {
    changes  git.ChangesListState
    branches data.ListState[string]
    commits  data.ListState[string]
    diff     git.DiffViewerState
    dst      code.CodeViewerState
    msg      git.CommitMessage // 提交输入的标题/正文
    vcs      vcsState          // 真实 git 状态（vcs.go，异步收集）
    branch   string
    showRepo bool
    codeTab  int // Repository 底部：0 = 工作区 Diff，1 = 文件源码
    paneTab  int // 右栏标签：0 = Workspace，1 = Repository

    // 工作区文件预览（目录树选中的文件）
    psrc             code.CodeViewerState
    previewPath      string
    previewLang      string
    previewText      string
    previewErr       string
    previewLoading   bool
    previewTruncated bool
}
```

`vcsState`（`vcs.go`）持有真实 git 的收集结果：`loaded/loading/err`、`flash`
（一次性 toast）、`committing`（提交 Busy 锁）、`branch`/`branches`/`files`/`entries`
（`entries` 与 `files` 平行，携带重命名旧路径等原始信息）/`commits`，以及选中变更的
`diffKey`/`diffFrom`/`diffTo`/`diffWt`/`diffErr`/`diffLoading`。

工作区树本身在 `workspace`（`workspace.go`）：`root`（`newApp()` 时取 `os.Getwd()`）、
`nodes map[string]wsNode`（已列目录：子项/状态/错误；文件不注册）、
`tree data.TreeState[string]`（展开/选中状态）。

### 5.6 `vcsFile` —— 变更条目模型

```go
type vcsFile struct {
    path    string
    oldPath string // 重命名时的旧路径（diff 取 HEAD:oldPath）
    status  git.GitStatus
    staged  bool
}
```

`parseStatus` 把 `git status --porcelain=v1 -b` 解析成 `[]git.ChangedFile` +
平行的 `[]vcsFile`：一个文件可同时出现暂存与未暂存两条（`MM`）；`R`/`C` 取
`old -> new` 的新路径展示。`vcsFile.changed()` 投影回组件模型。

### 5.7 `session` 与种子

`session{id,title,updated,pinned,ws}` —— `ws` 是会话所属工作区（绝对目录），
会话栏只展示当前工作区的会话。`seedSessions(ws)` 返回绑定到该工作区的首跑
演示索引（最新在前）。

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
- **工作区切换行**：`folder` 图标 + 当前工作区名（`workspaceName(root)`，Tooltip
  显示完整路径）+ `chevron-down` 图标按钮 → `openWsDialog()`。
- `ui.PrimaryButton("New chat")` → `newThread()`。
- `chat.ConversationList(&a.convList, items, {Label:"Sessions"}, nil)`，`items`
  只含 `s.ws == a.ws.root` 的会话（**会话按工作区组织**）：
  - `.Changed()` → `openSession(a.convList.Selected)`。
  - `.Submitted()` → toast。

### 6.3 状态栏 `statusbar`

`layout.StatusBar`：

- 左：工作区（`folder`，`workspaceName(root)`）、分支（`git-branch`）、模式（`brain`）、
  上下文用量（`sliders-horizontal`，文本 `6.4k / 8k tokens`）。
- 右：`live`（`circle`，`StatusItemSuccess`）、`3 changed`（`file-text`，`StatusItemWarning`）。

### 6.4 图标按钮 `iconToggle`

`ui.ButtonBase` + `Label`/`Tooltip`/`Size(28,28)`/`Radius(7)`，悬停着色，点击执行回调，
内部画一个 `ui.Icon`。

### 6.5 会话生命周期

- **`newThread()`** —— `saveSession()` → 生成 `new-N` id → 选中它 → 置空线程 →
  在索引头部插入 `New chat N` 会话（`ws` = 当前工作区）→ `persistSessions()`。
- **`openSession(id)`** —— 同 id 直接返回；否则 `saveSession()` 后切换 id，命中
  `threads[id]` 则恢复，否则 `seededFor(id)`，随后 `persistSessions()`。
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

**正文 `settingsBody(c)`**：`ui.Row(AlignItems Stretch)`，**高度按窗口高度固定**
（`settingsBodyHeight`：约窗高 62%，钳制在 `[420, 窗高-220]`，保证对话框连同
标题/按钮始终放得下）——切换 Providers / Agent 分区**不改变对话框高度**；
左栏（`ui.Column` 宽 180，两条 `settingsNavItem` 可点行，选中项用 `Selection` 底 +
`AccentText`）+ 1px 分隔条 + 右侧 `ui.Scroll(Grow 1, MinWidth 0)`（分区内容超出
时在分区内滚动，内部不放 grow 元素）按 `a.settingsTab` 渲染
`providersPane` / `agentPane`。滚动内容整体 `PaddingX(22)`：与分隔线、与滚动条
都留出呼吸空间（滚动条贴滚动区右缘，样式由 ui.Scroll 内部决定，应用层不可调）。
选左侧行只切分区、不关对话框。

**Providers 分区 `providersPane(c)`**（编辑 `a.llm`，值即时生效）：
- **Provider**：`input.Select`，选项 = `openai-compatible`（自定义端点）+ pi-ai 内置注册表。
- **Base URL**：`input.InputGroup`。已知 provider 可留空走默认；OpenAI 兼容必填。
- **API key**：`input.InputGroup`。
- **Model**：`input.TreeSelect`（单选 `input.TreeSelect`，节点来自注册表模型；切 provider 后
  `rebindModel()` 重挂当前模型）。
- **Reasoning**：`input.Select`，**固定四档** `reasoningTiers`——Off / Low / Medium /
  High（值 `none/low/medium/high` 对应 pi-ai 的 ThinkingLevel）。它跟后端走，
  所以放在 Providers 分区；不支持推理的模型忽略该值。
- **Fetch models**（自定义端点或填了 Base URL 时出现）：`fetchModels()` 在 goroutine 里
  `GET {base}/models`（`Authorization: Bearer`），解析 `data[].id` 排序后写回
  `provView.fetched`，经 `a.redraw` 回到 UI 线程；TreeSelect 节点即该列表。
- **Test connection**：`testConnection()` 走 `piai.Complete` 单轮连通性检查。

**OpenAI 兼容端点**：`resolveModel()` 对 `openai-compatible` 直接构建
`piai.Model{ID, Name, Provider: piai.ProviderOpenAI, API: piai.APIOpenAICompletions,
BaseURL: 去尾斜杠的用户 URL, ContextWindow: 8192}`（不走注册表）；Base URL 或模型为空报错。
推理层级对自定义端点默认 `none`（注册表查不到推理能力）。

**Agent 分区 `agentPane(c)`**：系统提示、温度、最大 token、流式输出
（编辑 `a.llm`；推理档位已移至 Providers）。

**选 session 永远回到对话**：`openSession` 先 `closeModals()` 再切转录，
所以开着设置框点左侧 session 也能正常切换并关掉对话框；`newThread` 同样 `closeModals()`。

**持久化（`config.go`）**：`settingsDialogs` 每帧运行并观察 `settingsOpen` 的
open→closed 转变——`Done` / ✕ / 遮罩 / Escape 任一关闭路径都落在这一处，触发
`saveSettings()` 把 `LLMSettings` 写入用户配置目录 `MujicaUI-agent-demo/settings.json`
（`Busy`/`LastError`/`ProviderOK`/`ConnectionErr` 标 `json:"-"` 不落盘；文件 0600、
目录 0700）。启动时 `main.go` 调 `loadSettings()` 合并加载：文件缺失/损坏回退默认，
并预置 `maxTokField` 与 `agentView.seededModel`，使已加载的 MaxTokens 不被
Agent 分区重播种。`configPath` 为空（拿不到用户配置目录）时持久化整体禁用。

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

## 9. 工作区与仓库检视器（`repo.go` + `workspace.go`）

`repoPane(c,k)`，宽 380、`Surface` 底色，顶部一个 `ui.Segmented(&paneTab,
"Workspace", "Repository")`（`FillWidth`）切换两个标签。

### 9.1 Workspace 标签（真实目录树）

`workspacePane(c,k)`，自顶向下：

1. **根路径行**：`folder` 图标 + 工作区根路径（`SingleLine` + `Tooltip`）+
   `refresh-cw` 图标按钮（`reloadWorkspace()`：清空 `nodes` 与树状态、清预览）。
   根默认是**用户主目录**（`homeDir()`），可从侧栏的切换行或 ⌘K "Open workspace…"
   改为任意目录（见 §9.4）。
2. **目录树**：`data.Tree[string]`（虚拟化、只构建可见行），`Element.Grow(1).MinHeight(0)`：
   - `Roots` = `[root]`（root 为空显示 `Empty` 文案）；
   - `Children`：文件 → `nil`（叶子）；目录 → 已列子路径，未列出返回**空非 nil 切片**；
   - `ItemStatus`：目录返回其加载状态（`DataUnloaded`/`DataLoading`/`DataReady`/`DataFailed`），文件返回 `DataReady`；
   - `Load`：`loadWsDir(path)` —— 标记 `DataLoading` 后列目录，有窗口时在 goroutine 里
     执行并经 `a.redraw` 回 UI 线程（`applyWsDir`），无窗口（测试）同步执行；
   - `Label`：`filepath.Base(path)`。
   - `.Changed()`（单击选中）→ `selectWsNode(path, false)`；`.Submitted()`（Enter/双击）→
     `selectWsNode(path, true)`。文件 → `openWsPreview`；目录仅在 submitted 时翻转展开。
3. **预览区**（`wsPreview`）：未选中时显示提示文案；选中后为标题行（`file-code` 图标 +
   路径 + 超过 256 KiB 显示 `truncated` + 可格式化语言显示 `Raw/Fmt` 分段 +
   `plus` 附加按钮）+ `ui.Box(Height 340, Clip)` 内的
   `code.CodeViewer`（加载中/错误分别显示占位与 `⚠` 错误行）。

**格式化视图（`fmtView` + `formatSource`）**：`Raw/Fmt` 分段切换只影响显示、
**不写回磁盘**（工作区树保持只读）。`formatSource` 进程内完成——`.go` 走
`go/format`（gofmt），`.json` 走 `json.Indent` 两空格美化；其它语言无格式化
（分段不出现），源码解析失败时显示原文。`fmtView` 缓存格式化结果（键 =
语言+NUL+内容），只在内容变化时重算，不逐帧跑 gofmt。

**附加到对话（`attachFile`）**：把文件加入 composer 的 `chat.ContextChips`
（`thread.ctx`，`ContextItem{ID:"file:<相对路径>", Kind:ContextFile}`）。入口有三处：
预览区标题行的 `plus` 按钮、Repository 底部面板的 `plus` 按钮、ChangesList 上的
双击/Enter（`Submitted()`）。重复附加去重（toast "Already attached"），上限 8 个。
`send()` 时把附件内容折叠进消息：可见行只追加 `Attached: \`a\`, \`b\``，LLM 消息
（`row.llmText`，`historyMessages` 优先取用）携带 `--- 路径 ---` + 内容
（单文件 64 KiB 上限，读取失败写明原因），发送后清空 chips。

**目录列举规则（`listDir`）**：跳过 `.git`、`.gocache`、`.gopath`、`.mygo`、`node_modules`、
`__pycache__`、`.venv`、`venv`、`dist`、`build`、`target`、`.DS_Store`、`desktop.ini`；
目录排前、文件排后，各按大小写不敏感名称排序。子目录在父目录列出时注册为
`DataUnloaded`，展开时才真正读盘（懒加载）。

**文件读取（`readCapped`）**：最多读 256 KiB，超出置 `previewTruncated`；读失败置
`previewErr`。`previewLang` 按扩展名映射高亮语言（`.go`→go、`.sh/.bash`→shell、
`.js/.ts`→javascript/typescript、`.py`、`.json`、`.sql`），其余纯文本（高亮器对未知语言安全回退）。

### 9.2 Repository 标签（真实 git）

数据来自 `vcs.go`：`collectVCS(root)` 用三条 git 命令收集快照——
`git status --porcelain=v1 -b`（当前分支 + ahead/behind + 变更）、
`git branch --all --format=…`（本地/远端分支、上游、ahead/behind）、
`git log --max-count=30 --pretty=format:…\x1f…\x1e`（哈希/父/作者/时间/主题/Refs）。
首次显示该标签时自动收集，此后由刷新按钮或任何写操作后重新收集；
非 git 目录显示 `⚠` 错误与提示，面板仍可渲染。

1. **头部**：`git-branch` 图标 + "Repository" 标题 + `refresh-cw` 刷新按钮（`refreshVCS`）。
2. **分支选择**：`git.BranchSelector(&branch, vcs.branches, {AllowCreate:true})`。
   - `.Changed()` → `checkoutBranch`（`git checkout <name>`）。
   - `.Created()` → `createBranch`（`git checkout -b <name>`）。
3. **Changes**：`git.ChangesList(&changes, vcs.files, {})`，高 96，暂存/未暂存分组：
   - `.Action()` → `vcsAction`：`stage`→`git add -- <path>`、`unstage`→
     `git restore --staged -- <path>`、`stage_all`→`git add -A`、
     `unstage_all`→`git restore --staged .`；
   - `.Submitted()`（双击/Enter）→ 附加该文件到对话。
4. **History**：`git.CommitList(&commits, vcs.commits, {})`，高 72。
5. **提交输入**：`git.CommitInput(&msg, {Busy: vcs.committing})` —— `.Committed()` →
   `commitStaged(false)`（`git commit -m 标题 -m 正文`，无暂存时 toast 提示）；
   `.Amended()` → `commitStaged(true)`（有标题带 `-m` 重写，无标题 `--amend --no-edit`）。
   提交期间 Busy 锁输入，成功后清空消息并刷新。
6. **底部面板**（选中变更的 Diff / 源码）：
   - 标题行：`git.GitStatusBadge(status)` + 路径 + `plus` 附加按钮。
   - `ui.Segmented(&codeTab, "Diff", "Source")`（宽 160）；Source 且可格式化时
     追加 `Raw/Fmt` 分段（宽 108，复用 §9.1 的 `fmtView`，实例独立）。
   - `ui.Box(Grow 1, MinHeight 220, Clip)` 内，加载中/错误有占位：
     - `codeTab==1` → `code.CodeViewer(srcFmt.render(lang, diffWt), &dst, …)`（工作区内容）。
     - 否则 → `git.DiffViewer(&diff, diffFrom, diffTo, …)`。

**版本读取（`fileVersions`）**：Diff 需要新旧两个版本，按条目状态取——
未跟踪：空 vs 工作区；暂存新增：空 vs `git show :path`（index）；暂存修改：
`HEAD:path` vs `:path`；暂存重命名：`HEAD:oldPath` vs `:path`；暂存删除：
`HEAD:path` vs 空；未暂存删除：`:path` vs 空；未暂存修改：`:path` vs 工作区。
内容含 NUL 字节时显示 "(binary file, not shown)"，超长截断。
选中项变化由 `diffKey`（staged 标志 + 路径）识别，异步加载期间选择再变会触发重载。

所有 git 写操作经 `gitThen`：有窗口时 goroutine 执行、`a.redraw` 回 UI 线程后
flash toast（成功文案或错误首行）并重新收集；无窗口（测试）同步执行。

`splitLines` 去掉末尾换行后按 `\n` 切分，供源码视图使用。

### 9.3 演示数据边界

`thread.go` 的 `filesChanged`（线程内 `MultiFileDiffReview` 卡）仍是演示数据；
右栏 Repository 标签已全部接真实 git。

### 9.4 工作区存储与切换（`wsstore.go`）

**默认根**：`homeDir()`（`os.UserHomeDir()`，取不到时回退进程目录）——工作区
**不再**跟随可执行文件的启动目录。

**`openWorkspace(path) string`**（返回 toast 文案）：TrimSpace → `filepath.Abs` →
`os.Stat` 校验是目录（否则 toast "Not a directory: …"）→ 与当前相同则仅关对话框 →
否则：`saveSession` + `persistSessions` → 旧根进 recents → `a.ws = newWorkspace(abs)`
+ `reloadWorkspace()`（树与预览重置）→ `a.repo.vcs = vcsState{}`（git 面板对新根
重新收集）→ `restoreSession()`（恢复该工作区最新会话，无会话则空线程到欢迎页）→
`saveWsPrefs` + `persistSessions` → toast "Workspace: <目录名>"。

**`restoreSession()`**：按存储顺序找第一个 `ws == 当前根` 的会话，选中并恢复其
线程（`threads` 命中用缓存，否则 `seededFor`）；找不到则 `sessionID=""` +
空线程（欢迎页）。

**持久化文件**（用户配置目录 `MujicaUI-agent-demo/` 下，0600/0700，路径字段为空
即禁用——测试不碰盘）：

| 文件 | 内容 |
| --- | --- |
| `settings.json` | LLM 配置（原有，不变） |
| `workspace.json` | `{current, recents[]}` —— 当前工作区 + 最近列表（去重、上限 6，载入时当前根置顶） |
| `sessions.json` | `{sessions:[{id,title,updated,pinned,workspace,mode,threaded,rows[]}]}` —— 全部工作区的会话与对话记录；`threaded` 标记是否存过线程（未打开过的演示会话由 `seededFor` 再生） |

**写入时机**：`newThread` / `openSession` / `send`（用户行落库）/ `applyReply`
（回复完成）/ `streamError` / `openWorkspace`。`persistSessions` 对当前会话取
**live** `a.thread`（可能领先 `threads` 缓存），其余会话取 `threads` 缓存。

**Open-workspace 对话框**（`workspaceDialog`，`overlay.Dialog` 宽 560）：
`Directory` 文本框（`input.InputGroup`，预填当前根）+ "Recent workspaces" 列表
（`ui.ButtonBase` 整行可点：folder 图标 + 目录名 + 完整路径）+ Actions 里 `Open`
按钮（提交输入框路径）。入口：侧栏切换行、⌘K "Open workspace…"。

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
| `workspace` | Show workspace tree | Repo | `showRepo=true, paneTab=0` |
| `workspace-open` | Open workspace… | Workspace | `openWsDialog()` |
| `reload-workspace` | Reload workspace | Repo | `reloadWorkspace()` + toast “Workspace reloaded” |
| `diff` | Show working diff | Repo | `showRepo=true, paneTab=1, codeTab=0` |
| `source` | Show file source | Repo | `showRepo=true, paneTab=1, codeTab=1` |
| `branch` | Switch branch… | Repo | `showRepo=true, paneTab=1` + toast |

`runCommand(id) string` **不接收 context**（便于单测）：执行状态变更并返回需弹出的 toast 文案（无则返回 `""`）。调用方 `palette` 负责 `c.Toast(msg)`。

---

## 12. 交互清单（验收点）

- [ ] 输入一句话回车 → 追加用户行 + 思考/正文 + 工具行，列表滚到底。
- [ ] `New chat` / ⌘N → 清空到欢迎页；能力卡示例可填入草稿；starter chip 可即发。
- [ ] 会话列表切到 `c4` / `c1` → 内容随之变化；切走再切回保留草稿。
- [ ] 侧栏顶部显示当前工作区名；列表只含该工作区的会话；点切换行（或 ⌘K
  "Open workspace…"）打开对话框，选最近目录或输入路径可切换；切换后树/git/会话
  全部重根，重启后恢复上次工作区与会话（`workspace.json` / `sessions.json`）。
- [ ] ⌘B / ⌘J 折叠左栏 / 右栏；标题栏两个按钮同效。
- [ ] ⌘K 打开面板；9 条命令各自生效。
- [ ] Workspace 标签：树列出真实目录（目录在前、噪音目录跳过）；展开子目录懒加载；
  点文件在下方预览源码；`plus` 按钮把文件附加到对话（composer 出现 chip，发送后
  内容进入 LLM 消息）；刷新按钮重载；状态栏显示工作区名。
- [ ] Repository 标签（真实 git）：列出真实分支/变更/历史；行内按钮 stage/unstage、
  组按钮全部暂存/取消；选中变更驱动 Diff（暂存= HEAD vs index，未暂存= index vs 工作区）；
  提交输入写标题后 Commit 真实提交；切分支/建分支生效；刷新按钮重收状态。
- [ ] Settings：正文与分隔线/滚动条留白（`PaddingX(22)`）；Reasoning 在 Providers
  分区且为 Off/Low/Medium/High 四档；Agent 分区不再有推理项；分区切换高度不变。
- [ ] 代码查看：预览高 340、仓库底部 Diff/源码区至少 220 且随空间生长；`.go`/`.json`
  出现 Raw/Fmt 分段，Fmt 显示 gofmt/美化内容（不写盘）；`.jsx`/`.zsh` 等映射高亮。
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
| 仓库 | `git.BranchSelector`、`git.ChangesList`、`git.CommitInput`、`git.CommitList`、`git.DiffViewer`、`git.GitStatusBadge` |
| 工作区树 | `data.Tree`（懒加载：`Children`/`ItemStatus`/`Load`） |
| 代码 | `code.CodeViewer` |
| 命令面板 | `navigation.CommandPalette` |
| 图标/主题 | `icons.Must`、`core.Use`、`core.Tokens` |
| 布局原语 | MyGo `ui.Row/Column/Box/Scroll/Segmented/Button/ButtonBase/PrimaryButton/Icon/Text/Avatar` |

> 约束：所有固定数据一旦传入组件就必须满足其契约（否则 MujicaUI 会 panic）。
> 例如 `ConversationList` 的 ID、`ChangesList` 的文件、`CommandPalette` 的 `Command.ID`。

---

## 14. 测试（各 `*_test.go`）

在无窗口环境下用 `ui.Render(view, w, h, scale)` / `ui.NewTester(view, w, h)` 驱动。

1. **`TestViewRenders`** —— 对 `showRepo ∈ {false,true}` × `codeTab ∈ {0,1}` 共 4 种组合渲染，
   断言非 nil（捕捉布局 panic / 组件契约缺失）。
2. **`TestSendAndNewChat`** —— `send()` 追加恰 3 行、清空草稿、末行是 `rowTools`；
   `newThread()` 后行数为 0 且欢迎页可渲染。
3. **`TestSessionSwitch`** —— `openSession("c4")` 载入 2 行；改动 `c4` 草稿、`saveSession`、
   切回 `c9`、再切回 `c4` → 草稿保留。
4. **`TestPaletteCommands`** —— 逐个执行 10 条命令且每步可渲染；`workspace`/`diff`/`source`
   分别落到正确的 `showRepo`/`paneTab`/`codeTab`，`workspace-open` 打开工作区对话框。
5. **`TestSettingsDialogStableHeight` / `TestSettingsBodyHeight`**（`settings_nav_test.go`）——
   正文高度由窗高固定：切换分区正文矩形与标题 Y 不变，且随窗口变矮而变矮；钳制函数单测。
6. **`TestFormatSource` / `TestFmtViewToggle` / `TestPreviewFormatToggle`**
   （`workspace_test.go`）—— gofmt 规范化 `.go`、`json.Indent` 美化、解析失败/其它语言
   不可格式化；`fmtView` 原始→格式化切换、缓存键随内容失效；预览区 Raw/Fmt 渲染。
7. **`TestSettingsPersistence*` / `TestSettingsSaveOnClose`**（`config_test.go`）——
   持久化往返（含 `maxTokField`/seededModel 预置）、瞬态字段不落盘、缺失/损坏回退默认、
   Escape 关闭对话框即保存。
7. **`TestListDir` / `TestWorkspaceLazyLoad` / `TestWorkspacePreview` /
   `TestWorkspaceTreeRender` / `TestWorkspaceCommands`**（`workspace_test.go`）——
   用 `t.TempDir()` 造真实目录：列举排序（目录在前、大小写不敏感）与忽略规则；
   懒加载状态机（Unloaded→Loading→Ready/Failed）、文件是叶子；预览读取/语言映射/
   256 KiB 截断/缺失文件错误；`NewTester` 打开根目录 → 树列出文件 → 点击行加载预览；
   workspace / reload-workspace 命令。
8. **`TestParseStatus` / `TestParseBranchesAndLog` / `TestVCSRealRepo` /
   `TestRepositoryPaneAttach`**（`vcs_test.go`）—— porcelain 解析（分支头、
   `MM` 双条、重命名、删除）；用真实 git 命令在临时目录建仓库后跑完整版本管理闭环：
   未跟踪 → stage → 提交 → 再修改 → 建分支/切分支，断言每步的文件/版本/历史；
   `NewTester` 渲染 Repository 标签并点击附加按钮。
9. **`TestAttachFile`**（`vcs_test.go`）—— 附加去重、发送后 chips 清空、可见行带
   `Attached:` 注记、`llmText` 携带文件内容。
10. **`TestWsPrefsRoundtrip` / `TestSessionsRoundtrip` / `TestOpenWorkspace` /
    `TestSidebarFiltersSessions` / `TestWorkspaceDialog` / `TestHomeDirDefault`**
    （`wsstore_test.go`）—— `workspace.json` 往返（当前根 + recents 置顶）；
    `sessions.json` 往返（含在途会话的行、全部会话绑定工作区）；切换工作区的
    校验/重根/会话恢复/recents；侧栏只列当前工作区的会话（渲染断言其它工作区
    会话不出现）；对话框渲染 + 点 Open 真实切换；默认根 = 用户主目录。
11. **`TestDialogCloses`**（`dialogclose_test.go`）—— Escape 与背景点击关闭 Settings，
   且两条关闭路径都写出 `settings.json`（`configPath` 指向临时目录，不碰真实配置）。

---

## 15. 已知限制与后续

**限制**

- Agent 回复走 pi-ai-go 真实 LLM，但为**单轮补全**：历史由 `historyMessages()` 从
  对话行重建（仅 user/assistant 文本行），工具调用结果暂不回灌到 LLM 历史。
- 配置持久化到用户配置目录 `MujicaUI-agent-demo/settings.json`（含 API Key 明文，
  文件 0600 / 目录 0700）；API Key 也可留空走 Provider 的环境变量。
- 无窗口（测试）环境 `send()` 落定占位文本，不做真实网络调用。
- git 走真实命令，但只覆盖本地操作（status/diff/log/add/restore/commit/checkout）：
  无 push / pull / fetch，不做合并冲突处理；`git restore --staged` 需要 git ≥ 2.23。
- 附件折叠进 LLM 消息时单文件上限 64 KiB（超出截断并注明）；预览上限 256 KiB；
  二进制文件显示占位文案。
- 语法高亮只支持 CodeViewer 内置的 7 种语言（go / javascript / typescript /
  python / json / shell / sql），`.md` / `.yaml` / `.html` 等按纯文本渲染；
  格式化仅进程内两种（`.go` gofmt、`.json` 美化），且只影响显示、不写盘。
- **不引入 LSP**（评估过 terax-clone 的做法：Go 侧 spawn 语言服务器、JSON-RPC
  over stdio 桥接给 CodeMirror 编辑器）。Atlas 的 `code.CodeViewer` 是只读视图，
  没有 hover / 补全 / 快速修复的宿主 UI；gopls 生命周期也让无窗口 CI 不可复现。
  若未来需要语言智能，第一步是把诊断接到 `code.ProblemsPanel`，而不是整编辑器。
- 工作区树根默认用户主目录，可从对话框切换（无系统原生目录选择器，路径需手输
  或从 recents 选）；忽略列表固定，预览上限 256 KiB（更大文件截断显示）。
- 会话与对话记录持久化为本地 JSON（`sessions.json`）；线程的交互式组件状态
  （计划/命令/Diff 卡的临时选择）不持久化，恢复后按行类型静态呈现；附件 chips
  为会话内临时态。
- `StatusBar` 的上下文用量、变更数为固定文案。
- 「Export / Import」为演示提示，无实际 IO。

**可扩展方向**

- git 补齐远端操作（push / pull / fetch，`git.GitListResult.Action` 的 discard），
  处理合并冲突与 detached HEAD。
- 工作区根目录选择器接入系统原生对话框、多工作区同时打开、树内文件过滤搜索、
  预览文件写回保存。
- 在仓库面板加入 `code.ProblemsPanel` / `code.OutputPanel` / `code.Terminal`；
  若引入 LSP，先把诊断（gopls 等）接进 ProblemsPanel（参考 terax-clone 的
  `internal/lsp` JSON-RPC 会话管理）。
- 借助 `core.Settings{Light/Dark}` 做运行时亮暗切换按钮。
- 用 `chat.ConversationSearch` 给会话栏加搜索，`chat.ConversationItem` 支持重命名/置顶/删除。
- 接入 pi-ai-go 的 `agent` 层完整工具循环（read/write/bash 等沙箱工具），把
  `send()` 的单轮补全升级为多轮 Agent 循环，并在 `rowTools`/`rowCommand` 里呈现工具事件。
- 将 API Key 迁移到系统 Keychain / 加密存储（当前为明文 JSON）。