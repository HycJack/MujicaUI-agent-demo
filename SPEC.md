# crux-agent（Crux）— 规格文档（Spec）

> 一个用 MyGo 原生 UI 工具包 + MujicaUI 组件库实现的 **Codex 风格编码 Agent 桌面应用**。
> 全窗口 GPU 自绘：无 WebView、无 HTML、无 JavaScript。

本文档描述 `crux-agent` 模块的完整规格：目标、架构、状态模型、界面、交互、主题、
构建与测试。它既是实现说明，也是后续迭代的契约。

---

## 1. 概述

Crux 把一次编码会话摊开成三块：

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
- 单一窗口，`go run .` 即起；对话走 pi-ai-go 的 **agent 循环**（多轮 + 工具执行），
  思考、脚本执行、文件编辑都以真实卡片落入对话流。
- 提供 **Settings 模态框**（`overlay.Dialog`）：左侧源列表切换 Providers（后端/模型/Key/URL/
  推理分档，支持 OpenAI 兼容端点与拉取模型列表）与 Agent（系统提示 + 固定工具集说明）
  两个分区。配置保存在 app 状态、每次调用时传入，并**在对话框打开期间实时持久化**
  （值一变即写盘，杀进程也不丢；见 `persist.go` / `settingsDialogs`）。
- 首跑无任何种子会话：会话列表为空、显示欢迎页，与真实 agent 控制台一致。

### 1.2 非目标

- git 走**真实命令**（status / diff / log / stage / unstage / commit / checkout），
  但不做 push / pull / fetch 等远端操作，也不处理合并冲突。
- LLM 配置持久化到用户家目录 `~/.crux-agent/`（`settings.json`）；工作区选择与全部会话/对话记录
  持久化到同目录（`workspace.json` / `sessions.json`）；"Export / Import" 等仍为
  演示性提示。
- 工作区树对真实文件系统**只读**（列目录 + 抽屉查看 + 附加到对话）；agent 的
  bash / read_file / write_file 工具**直接作用于真实文件系统**（无审批闸门，
  见 §7.2 的边界说明）。

---

## 2. 运行与构建

模块路径：仓库根目录；根 `main.go` 只做引导（调用 `app.Run()`）。数据层
`internal/store`、逻辑层 `internal/engine`、Markdown 解析 `internal/md`、
UI 层 `internal/app`。

```sh
go run .            # 运行（打开窗口）
go build -o crux.exe .   # 构建可执行文件
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
| 标题 | `Crux — coding agent` |
| 初始尺寸 | 1280 × 820 |
| 最小尺寸 | 1024 × 640 |
| `StateKey` | `crux-app`（记忆窗口位置/状态） |
| 内容 | `ui.View(a.view)` |

启动时另起一个 goroutine，每秒调用 `win.Update(func(){})`，让状态栏的 “live” 指示与
时间相关渲染保持心跳。

### 2.2 `mygo.json`

```json
{
  "name": "crux-agent",
  "identifier": "com.example.cruxagent",
  "version": "0.1.0",
  "out": "build"
}
```

---

## 3. 依赖

`go.mod`：

```
module crux-agent

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
- **pi-ai-go** —— 统一多模型 LLM SDK：`agent.AgentLoop` 做**多轮 agent 循环**（工具执行 +
  事件流），`piai.Complete` 做连接测试；`piai.GetProviders` / `GetModels` / `GetModel` 读内置
  模型注册表；`core.DefaultExecutionEnv` 提供以工作区为根的文件/命令执行环境；
  `_ "pi-ai-go/providers"` 的 `init()` 注册全部内置 Provider（OpenAI / Anthropic / Google /
  Bedrock / DeepSeek / GLM / Kimi 等）。以独立模块 `github.com/HycJack/pi-ai-go` 引入。

---

## 4. 模块结构

代码按**三层**组织——数据处理（`internal/store`）、逻辑处理（`internal/engine`）、
UI（根目录 `package main`）；数据层与逻辑层**不 import 任何 UI 包**，UI 层通过
显式转换与回调桥接它们。

### 4.1 数据层 `internal/store`（无 UI 依赖）

| 文件 | 职责 |
| --- | --- |
| `store.go` | 家目录布局 `~/.crux-agent/`（`Dir`/`SettingsPath`/`WorkspacePrefsPath`/`SessionsPath`/`TranscriptPath`（id 消毒防目录逃逸））、`WriteFileAtomic`（临时文件 + rename，坏 ACL 目标自愈）、`Migrate` 旧目录迁移链（`%AppData%\MujicaUI-agent-demo` → `~/.mujicaui-agent-demo` → `~/.crux-agent`，copyIfMissing 不覆盖新文件，旧目录保留）。 |
| `settings.go` | `LLMConfig`（**仅持久化字段**：provider/model/apiKey/baseUrl/systemPrompt/thinking）、`LoadSettings`/`SaveSettings`。 |
| `workspace.go` | `WsPrefs{current, recents}` 与其读写。 |
| `sessions.go` | 转录 schema：`Message`/`ToolCall`（role/kind/mode 持久化为字符串，工具卡数据拍平）、`SessionMeta`/`SessionIndex`（仅元数据）、`Transcript`、`LoadIndex`/`SaveIndex`/`LoadTranscript`/`SaveTranscript`、`MigrateLegacyIndex`（旧单文件格式一次性拆分，原文件留 `.bak`）。 |

### 4.2 逻辑层 `internal/engine`（无 UI 依赖）

| 文件 | 职责 |
| --- | --- |
| `loop.go` | **agent 循环封装**：`Config`（provider/model/key/baseURL/systemPrompt/thinking/workdir）、`Run(ctx, cfg, history, callbacks)`——解析模型、组装 `agent.AgentLoopConfig`、消费事件流并折叠为**回调**（`OnThinking`/`OnText` 传**本轮累计全文**，每轮 message 重置，由调用方拼接分段；`OnToolStart`/`OnToolEnd` 带耗时）、`ResolveModel`（OpenAI 兼容端点直接构建 + 注册表查询 + BaseURL 覆盖）、`ThinkDefault`。 |
| `tools.go` | **agent 工具集**：`bash`（平台 shell 执行，输出 8 KiB 截断）、`read_file`（64 KiB 截断）、`write_file`（Details 携带 old/new 供评审卡）；全部以 workdir 为根，`write_file` 自动建父目录。 |
| `registry.go` | provider 注册表门面：`Providers`/`Models`/`ModelInfoOf`（UI 只见 `ModelInfo`）、`TestConnection`（单轮连通性检查）。 |

### 4.3 Markdown 解析 `internal/md`（纯数据处理）

| 文件 | 职责 |
| --- | --- |
| `md.go` | 块级解析 `Parse(src) []Block`：标题/散文段（跨段合并）/列表/引用/表格/代码围栏/分隔线。 |

### 4.4 UI 层 `internal/app`（package app）

以下文件均位于 `internal/app/`；仓库根的 `main.go` 只调用 `app.Run()`。

| 文件 | 职责 |
| --- | --- |
| `app.go` | 引导 `Run()`：`store.Migrate()`、`newApp()`、`loadSettings()`、store 路径接线、窗口创建、`a.redraw = win.Update`、心跳 goroutine、`mygo.App.Run()`。 |
| `state.go` | UI 状态模型：`app` / `thread` / `row`（含 `tools []*toolRun` 合并卡块）/ `repo`（含 `vcs`）/ `session` / `LLMSettings`（内嵌 `store.LLMConfig` + 瞬态字段）；`settingsOpen`/`settingsTab` 与 `closeModals`/`openProviders`/`openAgent`。**无种子数据**——首跑即空会话 + 欢迎页。 |
| `shell.go` | 外壳布局：标题栏（含 Provider/Agent 设置入口与目录树直达按钮）、workspace→session 两级会话树（`sidebar`）、工作区、状态栏；快捷键。 |
| `thread.go` | 对话线程：`threadView`、`renderRow`（一条回复一个气泡：等待提示 + 思考/工具折叠块 + 可选中正文）、`actions`（复制/重新生成/时间）、`composer`、`regenerate`。 |
| `llm.go` | **引擎适配器**：`engineConfig()`（设置 → engine.Config 投影）、`historyMessages()`（行 → engine.Message）、`send`（附件折叠）、`startStream`（goroutine 跑 `engine.Run`，回调经 `a.redraw` 落回行状态：思考/文本增量刷新当前行，`appendTurnTool` 进该行的卡片块）、`finishToolRow`/`streamError`/中止处理。 |
| `persist.go` | **持久化胶水**：行 ↔ `store.Message` 转换（`storeMsg`/`loadMsg`，role/kind/mode 映射）、`persistSessions`（索引 + 脏标记转录增量写）、`loadSessions`/`loadTranscript`（懒加载）、`saveWsPrefs`/`loadWsPrefs`、`loadSettings`/`saveSettings`（写后快照 `savedSettings` 供脏检查）、`newThread`/`openSession`/`saveSession`/`openWorkspace`/`restoreSession`。 |
| `wsdialog.go` | Open-workspace 对话框（目录输入 + recents）。 |
| `settings.go` | **Settings 模态框**：`settingsDialogs`（**打开期间实时保存** `saveSettingsIfChanged`）+ `settingsBody`、`providersPane`（provider/model/Key/BaseURL + **Reasoning 四档**，OpenAI 兼容端点、`fetchModels`、Test connection 经 `engine.TestConnection`）、`agentPane`（系统提示 + 固定工具集说明 + 停止/状态）。 |
| `mdview.go` | 可选中 Markdown 渲染：块缓存（`ui.Local`）+ 每帧重建，全元素 `.Selectable()`，行内 Span/链接双形式，代码块用 `chat.CodeBlock`（解析在 `internal/md`）。 |
| `welcome.go` | 新会话欢迎页：能力卡 + starter chips + composer。 |
| `workspace.go` | **工作区**：真实目录树的状态与 IO——`listDir`（目录在前、忽略噪音）、`loadWsDir` 懒加载（goroutine + `a.redraw`）、`wsEnsureLoaded`（Outline 行内触发）、`readCapped` 文件预览（256 KiB 上限）、`attachFile` 附加到对话、`reloadWorkspace`。 |
| `drawer.go` | **代码抽屉**：右侧 `overlay.Drawer`（宽 720）大尺寸查看器——树点击打开文件内容（Raw/Fmt + 附加），仓库面板 `maximize` 把 Diff/源码放大进来；内容高度按窗高推导（抽屉内容区是 Scroll，grow 会塌）。 |
| `vcs.go` | **真实 git 后端**：`runGit`（15s 超时）、`parseStatus`/`parseBranches`/`parseLog`（porcelain 解析）、`collectVCS` 快照、`fileVersions`（HEAD / index / worktree 三方取版本，二进制探测）、`vcsAction`（stage/unstage）、`commitStaged`（含 amend）、`checkoutBranch`/`createBranch`、`loadSelectedDiff`。 |
| `repo.go` | 右栏检视器：Workspace 标签（`ui.Outline` 目录树：自定义行 + 右键菜单附加/查看/复制路径）与 Repository 标签（真实分支切换、暂存/未暂存变更、提交输入、历史、Diff / 源码）。 |
| `tokens.go` | 主题接入：`tokens(c)`、`useTheme(c)`、`tokensT` 别名。 |
| `commands.go` | ⌘K 命令面板与 `runCommand`（含 providers / agent / workspace / reload-workspace 命令）。 |
| `*_test.go` | UI 层测试（见 §14）；`internal/store`、`internal/engine`、`internal/md` 各有自己的包内测试。 |
| `mygo.json` | 打包元数据。 |
| `resources/icon.png` | 应用图标。 |

渲染约定：`app.view` 是 MyGo 的视图函数——**一帧一次**，由 MyGo 在输入后、
`Window.Update` 后、动画中重复调用。视图是状态的纯函数；事件处理器改写状态，下一帧呈现结果。

---

## 5. 数据模型（`state.go`）

### 5.1 `app` —— 顶层状态

```go
type app struct {
    thread    thread                   // 当前会话的对话线程
    repo      repo                     // 仓库检视器状态
    ws        workspace                // 工作区（目录树）
    fdraw     fileDrawer               // 大尺寸代码抽屉（drawer.go）
    conv      chat.ChatConversation    // 当前会话（供 ConversationItem 等使用）
    sessTree  ui.OutlineState[string]  // 侧栏会话树（workspace→session）展开状态
    sessionID string                   // 当前会话 id
    sessions  []session                // 会话索引（跨工作区全量，树上按工作区分组）
    threads   map[string]thread        // 按会话缓存的工作线程（切走再切回保留草稿）

    // workspace store（persist.go）
    recents      []string // 最近打开的工作区，最新在前（上限 6）
    wsDialogOpen bool     // Open-workspace 对话框
    wsPathField  string   // 对话框里的目录输入框
    wsCursor     int      // 目录树选中行游标（-1 = 无）

    // shell
    navOpen     bool                    // 左栏是否展开
    paletteOpen bool                    // ⌘K 命令面板是否打开
    nextID      int                     // 新建会话的自增序号
}
```

持久化路径字段（`configPath` / `sessionsPath` / `wsPrefsPath`）为空时禁用对应
存储 —— `newApp()` 只设 `configPath`，`sessionsPath`/`wsPrefsPath` 由 `app.Run`
接线，因此测试里的 `newApp()` 从不碰盘。

`newApp()` 构造初始状态：**无种子会话**（`sessionID=""`、`sessions=nil`、空线程），
`navOpen=true`，`repo.branch="main"`；工作区默认 `store.HomeDir()`（用户主目录，**不是**
可执行文件所在目录）。`main.go` 随后 `loadWsPrefs()` → `loadSessions()` →
`restoreSession()`；没有持久化会话时界面停在欢迎页，点 "New chat" 才创建会话。

### 5.2 `row` / `kind` / `toolRun` —— 对话行

```go
type kind int
const (
    rowPlain     kind = iota // 纯文本（MarkdownView）
    rowReasoned              // 思考块 + StreamingText
    rowTools                 // 工具调用卡（ToolCallCard）
    rowTyping                // "thinking…" 指示
    rowCommand               // 终端运行卡（CommandExecutionCard）
    rowDiff                  // 文件变更评审卡（FileChangeCard）
)

// step 是回复过程时间线的一项：一段思考、一条中间正文消息或一次工具调用，
// 按发生顺序（§7.3.2）。
type step struct {
    think string   // 思考段全文（其它步为空）
    text  string   // 中间正文消息（其它步为空）
    tool  *toolRun // 工具步的调用（思考/正文步为 nil）
    open  bool     // 该步的折叠态（不持久化；正文步不用）
}

type row struct {
    id        string
    role      chat.MessageRole
    kind      kind
    text      string
    llmText   string     // LLM 视角文本（附件内容折叠）
    thinkText string     // 全部思考段拼接（持久化形态）
    at        time.Time
    doneAt    time.Time  // 流式结束时刻（完成态组头的时长）
    tool      *toolRun   // 旧版单工具行（合并前存储的转录）
    tools     []*toolRun // 本回复的工具调用，按执行顺序
    steps     []step     // 过程时间线：思考段与工具调用按发生顺序交错
    procOpen  bool       // 过程区（组头）折叠态：流式中默认展开，完成自动收起
    streaming bool       // 回复仍在流式（正文按纯文本渲染）
}

type toolRun struct {
    callID, name, args, result, errMsg string
    state  agent.AgentState
    dur    time.Duration
    run      muiagent.CommandRun   // bash 卡
    decision muiagent.FileDecision // write_file 评审状态
    change   muiagent.FileChange   // write_file 的 Path/Old/New
}
```

> 工具行由 agent 循环的真实事件驱动：`bash` → `rowCommand`、`write_file` →
> `rowDiff`、其余（`read_file` 等）→ `rowTools`。

### 5.3 `thread` —— 对话线程状态

```go
type thread struct {
    rows   []row
    list   chat.MessageListState
    draft  string
    mode   chat.ChatMode
    model  string
    think  bool
    ctx    []chat.ContextItem
    plan   agent.FileDecision   // FileChangeCard 的抉择
    do     agent.FileDecision
    cmd    agent.CommandRun      // 终端运行数据
    diff   []agent.FileDecision  // MultiFileDiffReview 的抉择
    run    []agent.CommandRun
}
```

### 5.4 `repo` —— 检视器状态

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

    srcFmt fmtView // Repository 源码标签的 Raw/Fmt
}
```

`vcsState`（`vcs.go`）持有真实 git 的收集结果：`loaded/loading/err`、`flash`
（一次性 toast）、`committing`（提交 Busy 锁）、`branch`/`branches`/`files`/`entries`
（`entries` 与 `files` 平行，携带重命名旧路径等原始信息）/`commits`，以及选中变更的
`diffKey`/`diffFrom`/`diffTo`/`diffWt`/`diffErr`/`diffLoading`。

工作区树本身在 `workspace`（`workspace.go`）：`root`（`newApp()` 时取 `homeDir()`）、
`nodes map[string]wsNode`（已列目录：子项/状态/错误；文件不注册）、
`outline ui.OutlineState[string]`（展开/选中状态；行是自定义构建的）。

### 5.5 `vcsFile` —— 变更条目模型

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

### 5.6 `session` —— 会话索引

`session{id,title,updated,pinned,ws}` —— `ws` 是会话所属工作区（绝对目录），
会话栏只展示当前工作区的会话。**无种子**：首跑列表为空，"New chat" 创建第一条。

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

- `layout.TitleBar(c, "Crux", {Leading, Trailing})`。
- Leading：`bot` 图标 + "Crux" 字标 + 会话栏折叠按钮（`menu`）。
- Trailing：当前模型名 + Provider/Agent 设置 + **`folder` 按钮（直达右侧目录树面板：
  `showRepo=true, paneTab=0`）** + `git-pull-request` 检视器开关 + 头像。

### 6.2 会话栏 `sidebar` —— workspace → session 两级树

**会话树**：第一层是 workspace 目录名，其会话嵌套在下面（Codex 风格按项目分组）：

- **Workspaces 头行**：标题 + `plus` 图标按钮（`openWsDialog()`）。
- **会话树**：`ui.Outline[string]`（虚拟化，`Grow(1).MinHeight(0)`），键为
  `"ws:<path>"` / `"sess:<id>"`：
  - 根 = 当前工作区在前、recents 在后（`sessionTreeRoots`）；
  - `Children("ws:<path>")` = 该工作区的会话（`sessionTreeChildren`），会话为叶子；
  - **workspace 行**：folder 图标 + 目录名（粗体）+ 会话数徽标；当前根用
    `AccentText` 高亮；**点击 = 展开/折叠**（翻转 `sessTree.Open`），不切换工作区；
  - **session 行**：`message-square` 图标 + 标题；当前会话 `AccentText`；点击
    `openSession(id)`，会话属于其它工作区时**先 `openWorkspace(ws)` 再打开**；
  - 当前工作区节点在 `restoreSession` / `newThread` 时自动展开。
- **"Open workspace…" 行**（树下方固定一行，手输路径入口）+ **`New chat`
  PrimaryButton**（在当前工作区建会话）。

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
  `threads[id]` 则恢复，否则给空线程（欢迎页），随后 `persistSessions()`。
- **`saveSession()`** —— 把当前 `thread` 存回 `threads[sessionID]`。

---

## 7. 对话线程（`thread.go` + `llm.go` + `internal/engine`）

### 7.1 `send()` → agent 循环

`send()` 现由 `llm.go` 提供，走 `internal/engine` 封装的 pi-ai-go **agent 循环**
（多轮 + 工具执行）：

1. 取 `strings.TrimSpace(draft)`，空或 `a.llm.Busy` 则返回。
2. 附件折叠进 `llmText`（见 §10）后追加用户行。
3. 追加一条**空的 `rowReasoned` 助理行（`streaming=true`）**——整条回复（所有轮次的
   思考、工具调用、正文）都进这一行，`list.ScrollToEnd()`，`persistSessions()`。
4. `startStream(userIdx, rowID)`：置 `a.llm.Busy=true`，建 `context.WithCancel` 存到
   `a.cancelFn`，起一个 goroutine：
   - `a.engineConfig()` 把设置投影成 `engine.Config`（含 `Workdir: a.ws.root`），
     `a.historyMessages()` 把行转成 `engine.Message` 历史。
   - `engine.Run(ctx, cfg, history, callbacks)` 在引擎内解析模型、组装
     `AgentLoopConfig{Model, SystemPrompt, Tools: engine.Tools(workdir),
     ToolExecution: ToolExecSequential, ExecEnv: NewDefaultExecutionEnvWithDir(workdir),
     SimpleStreamOptions}` 并消费事件流，折叠为回调（行 id 只在 goroutine 侧铸造，
     UI 线程只见捕获的字符串，无共享索引竞争）：
     - `OnThinking(text)`：**本轮思考**（引擎每轮 message 重置缓冲）→ `a.redraw` →
       `row.setThink`：同轮增长原地更新最后思考段，工具后的新轮思考追加新段
       （时间线交错，§7.3.2），`thinkText` 同步为全部段拼接。
     - `OnText(text)`：**本轮正文**（引擎每轮 message 重置缓冲；模型看过工具结果后
       的中间回复就在这里）→ `row.setText`：同轮增长原地更新最后正文段，工具后的
       新轮正文追加新段，`r.text` 同步为全部段按空行拼接（持久化/历史的形态
       不变）。
     - `OnToolStart(callID, name, args)`：`appendTurnTool` 把工具追加进**当前回复行
       的 `tools` 卡片块与时间线 `steps`**（bash 预填 `CommandRun`），状态 Running。
     - `OnToolEnd(callID, result, isErr, dur)`：`finishToolRow` 按 callID 在各行
       `tools`（含旧版单工具字段）里找到卡片，落定结果/错误/耗时（isErr 置
       `AgentFailed`，否则 `AgentDone`）；bash 填 `CommandRun`（输出 + 退出码），
       write_file 从 Details 解析 old/new 填 `FileChange`。
   - 完成后（defer）`a.llm.Busy=false`、清 `cancelFn`、`endStreamingRow`（清
     `streaming`，正文从流式纯文本切换为 Markdown 渲染）、`ScrollToEnd`、
     `persistSessions()`；`engine.Run` 返回错误且非 ctx 取消时 `streamErrorRow`
     落错误文案。

> **一条回复一个气泡**：思考 + 工具调用进该行的**过程时间线**（`steps`，§7.3.2
> ——组头一行 + 思考段/工具步按发生顺序逐行折叠），正文在过程区下方；流式期间
> 正文走 `StreamingText`（纯文本 + 光标，无内容时先显示 `TypingIndicator`
> 等待点），完成后走自研 `mdView`（标题/列表/表格/代码块全量渲染，
> **全元素可拖选复制**）。
> 线程约定：UI 线程持有全部状态。goroutine 只缓冲局部字符串，经
> `a.redraw(fn)`（即 `win.Update`）把 `fn` 调度回 UI 线程再改状态；闭包**不能**捕获
> `strings.Builder`（`win.Update` 不等待 `fn`，闭包可能在 goroutine 退出后执行）。
> `a.redraw==nil`（无窗口 / 测试）时 `send()` 同步落定占位文本，测试保持确定性。

### 7.2 工具集（`internal/engine/tools.go`）

三个工具，全部以**当前工作区为根**真实执行，并全部经过**权限三档门**
（§7.2.1）——门决定放行 / 拒绝 / 出审批卡；输出与内容都有上限防止刷爆上下文：

| 工具 | 行为 | 卡片 |
| --- | --- | --- |
| `bash` | 平台 shell（Windows `cmd /C`，其它 `sh -c`）执行命令行，stdout+stderr 合并返回，8 KiB 截断；Details 带 `exit` 退出码 | `CommandExecutionCard`（命令/输出/退出码/耗时，可点停止 → `a.cancel()`） |
| `read_file` | 读文件（相对路径解析到工作区根），64 KiB 截断 | `ToolCallCard`（args/result JSON） |
| `write_file` | 写文件（自动建父目录），Details 携带 `{path, old, new}` | `FileChangeCard`（before/after diff 评审记录；写入已发生，卡片是留档） |

**默认系统提示词**（`state.go` `defaultSettings`，可被 Settings 改写并持久化）：
移植自 DSH 的组装版系统提示词（`snapshots/web/fresh-round-trip/
system-prompt.expected.md`）——身份两行 + 逐条规则，**只保留映射到 Crux
工具集的规则**（bash 退出码检查、read_file 优先于 cat、write_file 先读后写、
最终回复展示主结果 + 代码块 + 用户语言），DSH 侧引用 Crux 没有的工具的行
（glob/grep/后台任务/web_search/goal/workflow/subagent/present/@-路径引用）整行
丢弃，不保留会诱导模型调用不存在工具的指引。加载升级：存的是**旧默认逐字
原文**（`legacySystemPrompt`）则替换为新默认；自定义提示词原样保留。

### 7.2.1 权限三档门与审批卡（`gate.go` / `tools.go` / `approval.go`）

DSH 风格的三级权限模式是**唯一的审批开关**，由 composer 底部的
`ui.Segmented`（Read-only / Agent / Full access）选择，存进
`settings.json` 的 `mode` 字段；`engine.ParseMode` 把落盘字符串解析成
`Mode`（空 = agent 默认，拼错 = read-only——**拼写错误不是权限**）：

| 模式 | bash | write_file | read_file |
| --- | --- | --- | --- |
| **Read-only** | 拒绝 | 拒绝 | 放行 |
| **Agent** | 沙箱可用→入沙箱；沙箱不可用→出审批卡 | 限定工作区内 | 放行（凭据目录拒绝） |
| **Full access** | 放行 | 放行 | 放行 |

- **门表**（`gateResolve`）：未知工具一律拒绝；`gatedBash` 按模式分派，
  `gatedWrite` 做 `withinWorkdir` 路径收束，`gatedRead` 做凭据目录
  （`.ssh` / `.aws` / `.gnupg` / `.kube` / `.docker` / `.config/gh` /
  `.netrc`）拒绝。
- **沙箱层**（`sandbox.go`）：macOS = seatbelt profile、Linux = bubblewrap，
  均为一次性 `Command(ctx, name, args...)` 包装（边界 = 工作区 + 临时
  scratch + 断网）；**Windows 无 OS 级沙箱**（`Available()==false`），
  Agent 模式下的 bash 落到**逐条审批卡**——卡就是边界。
- **审批卡**（`approval.go`）：`ApprovalFunc` 桥（`a.awaitApproval`）把
  引擎的 ask 变成挂在当前 streaming 行上的卡片（工具名 + 已脱敏的单行
  摘要 + Allow once / Always allow / Deny）。决策写回引擎的
  **缓冲 (1) 通道**——晚到的答案不是答案；10 分钟未决 = 超时拒绝。
  "Always allow" 等同 Allow once（**不持久化任何按工具放行规则**）。
  停止回复 / 回复结束会连同卡片一起清掉待决请求。

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
- **Test connection**：`testConnection()` 走 `engine.TestConnection` 单轮连通性检查。

**OpenAI 兼容端点**：引擎的 `ResolveModel()` 对 `openai-compatible` 直接构建
`piai.Model{ID, Name, Provider: piai.ProviderOpenAI, API: piai.APIOpenAICompletions,
BaseURL: 去尾斜杠的用户 URL, ContextWindow: 8192}`（不走注册表）；Base URL 或模型为空报错。
推理层级对自定义端点默认 `none`（注册表查不到推理能力）。

**Agent 分区 `agentPane(c)`**：系统提示（TextArea）+ 固定工具集说明（bash /
read_file / write_file，只读展示）+ 停止/状态行。**采样不暴露**——温度 / 最大
token / 流式开关已移除，一律走 provider 默认（引擎的 stream options 只带 APIKey 与
Reasoning 档位）。

**选 session 永远回到对话**：`openSession` 先 `closeModals()` 再切转录，
所以开着设置框点左侧 session 也能正常切换并关掉对话框；`newThread` 同样 `closeModals()`。

**持久化（`persist.go` + `internal/store`）**：保存是**实时**的——`settingsDialogs`
在对话框打开的每一帧把 `json.Marshal(a.llm)` 与上次落盘快照 `savedSettings` 比较，
漂移即写盘（`saveSettingsIfChanged`）；open→closed 转变仍强制最终保存（`Done` / ✕ /
遮罩 / Escape 任一关闭路径都覆盖）。这样**开着对话框杀进程 / 直接关窗口也不会丢配置**。
文件写入家目录 `~/.crux-agent/settings.json`（`Busy`/`LastError`/
`ProviderOK`/`ConnectionErr` 标 `json:"-"` 不落盘；文件 0600、目录 0700）。
启动时 `main.go` 调 `loadSettings()` 合并加载：文件缺失/损坏回退默认并记录快照。
`configPath` 为空（拿不到家目录）时持久化整体禁用。

### 7.2 `threadView(c)`

- 外层 `ui.Box(Grow 1, MinHeight 0, Border, Clip)`。
- `len(rows)==0` → `welcome(c,k)`。
- 否则 `chat.ChatContainer(&a.thread.list, n, opts, rowFn)`：
  - `List{ID, Date, Label}` —— 稳定 id、时间（驱动日期分隔）、无障碍标签。
  - `Input` → `composer(c)`。
  - 每行 `renderRow`。

### 7.3 `renderRow(c, r)`

**一条 AI 回复 = 一个气泡**（`rowReasoned`）：过程区（思考 + 工具，见 §7.3.2）
在正文上方；正文在块下方——流式期间 `StreamingText`（纯文本 + 光标，等待首
token 时显示 `TypingIndicator`），完成后 `mdView`（标题/有序无序列表/表格/代码
块，**可拖选复制**）。

| kind | 组件 |
| --- | --- |
| `rowTyping` | `chat.TypingIndicator(c, "Crux")` |
| `rowReasoned` | `MessageBubble` → [等待提示（streaming 且无思考/工具/正文时 `TypingIndicator`）+ **过程区 `processBlock`（§7.3.2，时间线：组头一行 + 思考段/工具步按发生顺序逐行折叠）**] + 正文（streaming→`StreamingText`；完成→**`mdView` 可选中 Markdown**）+ `actions` |
| `rowTools` / `rowCommand` / `rowDiff` | **旧版独立工具行**（合并前存储的转录仍可显示），渲染分支保留 |
| 默认（用户/纯文本行） | `MessageBubble` → `ui.RichText(Span).Selectable()`（原文逐字、可选中）+ `actions` |

助理消息的 `MessageBubbleOptions{Name:"Crux"}`。

### 7.3.1 可选中 Markdown（`internal/md` / `mdview.go`）

MujicaUI 的 `chat.MarkdownView` 构建的文本元素**不可选中**（且解析器在 internal 包，
无法外部定制），Crux 自带适配自 mygo-agent 参考渲染器的实现：

- `md.Parse(src) []md.Block`（`internal/md`，纯数据处理）：块级解析——标题、**散文段
  （连续段落与空行合并为一个 run，拖选可跨段）**、列表（含有序）、引用、表格、
  围栏/缩进代码、分隔线。
- `mdView(c, src)`（`mdview.go`）：块缓存挂行元素（`ui.Local`，src 变化重解析），
  每帧重建原生元素；**所有文本 `.Selectable()`**（标题/段落/列表项/引用/表格单元格），
  拖选 + Ctrl+C 可复制；行内 `code`/`**bold**`/`~~strike~~` 走构造器 Span，含
  `[link](url)` 的段落走元素形式保链接可点；代码块用 `chat.CodeBlock`（自带复制/折叠）。

### 7.3.2 过程区时间线（`processBlock`，参考 DeepSeek Harness）

演进：v1 单层 `ThinkingBlock` 展开无限长；v2 三层折叠分了两摞、无类别图标；
v3 引入时间线但步骤嵌在**限高滚动容器**里——"工具全挤在 thinking 里"；v4 把
过程区移出气泡成消息流独立块；v5 加入中间正文段。v6 收拢回**气泡内**（气泡是
整条回复的容器），保留 v4/v5 的关键结构——步骤在气泡内**按发生顺序直接铺开**
（不套外层滚动容器）、每步独立折叠、每类工具独立图标与状态标：

```
┌─ Crux 气泡（rowReasoned = 整条回复）────────────────────┐
│ ① 组头（procOpen，一行）                                │
│    ▸ [活动图标] 摘要 · 时长                              │
│ ② 步骤行（展开时按发生顺序在气泡内铺开，不套滚动容器）：   │
│    ▸ 🧠 Thinking · 第一行预览…                          │
│    ▸ ✓/✗ ⌨ Ran command · tail -40 …                    │
│    💬 The quota error is at line 3…（中间正文，可见）     │
│    ▸ 📄 Read file · jobs/export.sh                      │
│ ③ 答案正文（最后一个正文段；等待时 TypingIndicator）      │
│ ④ actions                                              │
└─────────────────────────────────────────────────────────┘
```

**数据模型**（`state.go`）：`row.steps []step`（思考段 | `*toolRun`，按发生顺序）。
流式期间由引擎回调构建：

- `OnThinking` → `row.setThink(text)`：最后一步是思考段且 text 是它的延伸
  （同轮增量）→ 原地更新；否则**追加新思考段**（引擎每轮 message 重置思考，
  工具之后的新一轮思考 = 新段——交错由此而来）。`thinkText` = 全部段拼接
  （持久化形态；顺带修复旧实现多轮思考互相覆盖的问题）。
- `OnToolStart` → `appendTurnTool`：追加工具步，与 `tools` 共享同一
  `*toolRun` 指针（`OnToolEnd` 的结果更新两处都可见）。
- 重载（`loadMsg`）不持久化交错结构：重建为 [思考段（全文）， 工具步…]，
  全部收起。`steps` 为 nil 的行渲染时惰性物化一次（同一规则）。

**每步一行（DSH ToolRow / ReasoningRow 形态）**——`data.Collapsible` 自定义
Trigger，行内 `[状态图标] [工具图标] 标题 · 摘要 · 时长`：

| 步类型 | 图标（`icons.Must`，已核对存在） | 标题 | 摘要（单行截断） | 展开体 |
| --- | --- | --- | --- | --- |
| 思考段 | `brain` | Thinking | 段落首行（流式中实时跟随末段） | 限高滚动全文（Selectable） |
| bash | `terminal` | Ran command | 命令首行（`run.Command`） | `CommandExecutionCard`（停止按钮中止循环） |
| read_file | `file-text` | Read file | `path` 参数 | `ToolCallCard`（Args/Result 二级折叠） |
| write_file | `pencil` | Wrote file | `path` 参数 | `FileChangeCard`（评审） |
| 其余 | `bot` | 工具名 | `path` 或 args 首行 | `ToolCallCard` |
| **正文段** | 无 | —（**可见文本，不折叠**） | — | 段落 Markdown（`mdView`） |

**中间正文（v5）**：模型看过工具结果后会在下一轮输出正文（中间回复；引擎内
工具结果由 `agent.AgentLoop` 回灌——模型看得到，缺的只是展示）。`OnText` 每轮
重置 → `row.setText` 维护**正文步**：同轮增长原地更新，工具后的新轮正文追加
新段；`r.text` = 全部段按空行拼接（持久化/历史形态不变）。**最后一个正文段**
是最终答案（渲染进回答气泡：流式 `StreamingText` → 完成 `mdView`）；**更早的
正文段**是过程叙述——以可见文本行出现在时间线的工具行之间，随组折叠收起；
工具在其后开始时，原"最后段"自动转为时间线行。无正文步的行（重载/错误/
测试直造）气泡回退 `r.text`。

- 运行中：行首 `core.Spinner`；失败：摘要后缀红色 "failed"；行尾
  `agent.AgentDuration(dur)`（`tnum` 等宽数字）。
- 标题 `FontWeight(500)`，摘要/时长 `k.TextMuted`，失败 `k.Danger`。

**组头（`procHeader`）**：图标取**当前/首要活动类别**（DSH
`PROCESS_ICONS[top]`）：流式中 = 最后一个运行中工具的图标（无则 brain），
完成 = 第一类工具的图标（无工具 = brain）。文案与时长沿用 v2：流式 =
"Running bash…"+ 实时计时（`c.Now()−r.at`，`c.After` 跳秒）；完成 = 前三类
活动无计数摘要 + `doneAt−r.at`。

**折叠时机**（沿用 v2，对齐 DSH `collapseTiming=completion`）：新回复
`procOpen: true`；`endStreamingRow` 置 `streaming=false` 时同时置
`procOpen=false, doneAt=now`——完成即收起成一行摘要。每步 `open` 与
`procOpen` 都不持久化，重载即收起。

**状态标（v6）**：每个工具步行首带状态图标——`circle-check`（Success，成功）、
`circle-x`（Danger，失败）或 `core.Spinner`（运行中），一眼分辨成败；失败行尾
再缀红色 "failed"。`finishToolRow` 在 `isErr` 时置 `state = AgentFailed`（此前
只写 errMsg、状态停留在 Done，导致收起行看不出错误）。

**气泡时机**（v6）：气泡是**整条回复的容器**，rowReasoned 恒渲染——等待首
token 时 `TypingIndicator`；流式中 = 过程区（组头展开）+ 末段
`StreamingText`；完成 = 折叠的组头一行 + 末段 `mdView` + actions。更早的
正文段留在过程时间线里。

**限高边界**（沿用 v4）：步骤行本身**不限高**——在气泡内按发生顺序铺开；每步
的**展开体**封顶：思考段全文 `ui.Scroll().Shrink(1).MaxHeight(280)`（长思考
内部滚动），工具卡自带封顶（`AgentCode` maxHeight 240/320）。组头展开**不**
包外层滚动容器。

**验收**（`proclayers_test.go`）：`setThink`/`setText` 交错语义（同轮增长 vs
新段、多段拼接）；工具步共享指针（结果更新两处可见）；渲染断言（收起态不构建
正文、组头单行、展开后步骤按发生顺序出现在**气泡之外**、每类工具图标行、
思考首行预览、中间正文出现在工具行之间、末段进气泡、正文不被 200 行思考
推出视口）。

### 7.4 `actions(c, r)` —— 每条消息下的操作行

自绘小图标按钮（`ui.ButtonBase` 22×22 + `icons.Must`），**不用** `chat.MessageActions`
（其赞/踩/朗读/分享/编辑按钮 Crux 不用）：

- **复制**（所有消息）：`c.WriteClipboard(r.text)` + toast。
- **重新生成**（仅助理行）：`regenerate(r)` —— 忙时 toast 提示；否则**删掉该回复及
  其后所有行**、追加新的 streaming 回复行（id 带 `regenSeq` nonce，避免元素状态串用）、
  `startStream(idx-1, id)` 重跑同一历史。
- **时间**：`r.at.Format("15:04")`，muted 小字。

### 7.5 `composer(c)`

```
Column(Gap 8)
├── chat.ContextChips(&ctx, {Max:2})        可移除上下文芯片
├── chat.PromptComposer(&draft, {
│       Placeholder,
│       Actions:() -> chat.SendButton({Shortcut:"Enter", Disabled: draft 空})
│   })  → Submitted() 也触发 send
│       （chat/agent 模式选择器已移除——Crux 固定 Agent 模式）
└── Row(Wrap)
    ├── ui.Segmented("Read-only / Agent / Full access")   权限三档（§7.2.1）
    │       ——忙时（Busy）冻结不改值；选择即落盘 settings.mode
    ├── 后端标签（Agent 设置页设定；claude 后端 = "claude · 别名"）
    └── chat.TokenCounter(6400, 8000)
```

### 7.6 Claude Code 后端（`internal/engine/claude*.go`）

Settings → Providers 的 **Agent type** 分段选择器把 Crux 变成 Claude Code
CLI 的桌面壳：选 "Claude Code CLI" 后表单换成 CLI 探测行
（`exec.LookPath` + 按需 `claude --version` 探针 goroutine）、模型别名输入
（空 = CLI 默认）与"CLI 自己登录"的注记。`engine.Config.Backend`
（`"builtin"` / `"claude"`）派发 `engine.Run`：

- **spawn**：`claude -p --output-format stream-json --verbose
  --input-format stream-json --include-partial-messages
  --permission-mode default|bypassPermissions [--model 别名]
  [--resume 会话] [--append-system-prompt 提示词]`；prompt 是 stdin 上的
  一行 user JSON（**CRLF 行尾**——任何行读取器都兼容）；审批由
  `can_use_tool` control_request 走 `control_response`
  （allow + updatedInput 回显 / deny + 原因）。`result` 事件收尾但 CLI
  常驻 → 断扫描、关 stdin、`Reap` 兜底杀树。
- **事件**（`claude_events.go`）：init→`OnSession`（存进转录的
  `claudeID`，下一轮 `--resume`）；stream_event 文本增量→`OnText`
  （后缀去重防 partial/complete 重复）；tool_use→`OnToolStart`
  （**Bash/Edit/Write/Read 工具名映射**到 bash/write_file/read_file
  卡片体系；Edit/Write 的 FileChange 数据从 args 取
  old_string/new_string/content）；tool_result→`OnToolEnd`；
  result→`OnNote`（耗时 · 费用 · token · 会话号一行，挂在回复行尾，
  不持久化）；compact 边界→`OnNote`（压缩通知）。
- **终止语义**：ctx 取消且已见过事件 = 正常结束；取消且一个事件都没见
  = "stopped before it answered"；Windows 下取消走 **taskkill /T /F
  整树击杀**（npm 装的 claude 是 .cmd 包装，只杀 leader 会留下攥着
  stdout 管道的 node 子进程）。
- **会话衔接**：一轮 = 最后一条用户消息整句重发（CLI 自持历史）；
  转录存 `claudeID` 跨轮 resume；**regenerate = 删回复行重发**（CLI 侧
  历史无从对齐，文档化的取舍）；中途切换后端 = 新的 claude 会话。

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
   `refresh-cw` 图标按钮（`reloadWorkspace()`：清空 `nodes` 与树状态、关闭代码抽屉）。
   根默认是**用户主目录**（`homeDir()`），可从侧栏的工作区列表或 ⌘K "Open workspace…"
   改为任意目录（见 §9.4）。
2. **目录树**（占满标签页，预览框已移除）：`ui.Outline[string]`（虚拟化、只构建可见行、
   **自定义行内容**），`Element.Grow(1).MinHeight(0)`，选择游标 `a.wsCursor`
   （`outline.List.Selected` 指向它，单击才有 `Changed`）：
   - `Roots` = `[root]`；`Children`：文件 → `nil`（叶子）；目录 → 已列子路径，
     未列出返回**空非 nil 切片**（显示展开箭头）；
   - **行内容 `wsTreeRow`**：类型图标（目录 `folder`、代码文件 `file-code`、其余
     `file`）+ 文件名 + 状态（列目录中 `core.Spinner`、失败 `circle-alert` 危险色）；
     行带 `Label("tree:<path>")` 供测试点击；
   - **懒加载**：行构建时 `wsEnsureLoaded(path)` —— 目录已展开且 `DataUnloaded` 就
     `loadWsDir`（状态同步翻成 Loading，每目录只问一次；有窗口时 goroutine +
     `a.redraw`，无窗口同步）；
   - **右键菜单（文件）**：MyGo 原生 `row.ContextMenu` —— "Add to conversation"
     （`attachFile` + toast）/ "Open in viewer"（`openFileDrawer`）/ "Copy path"
     （剪贴板 + toast）；
   - `.Changed()`（单击选中）→ `selectWsNode(path, false)`；`.Submitted()`（Enter/双击）→
     `selectWsNode(path, true)`。文件 → `openFileDrawer`（大抽屉，见下）；目录仅在
     submitted 时翻转展开。

> 上游缺口：`ui.List`/`ui.Outline` 的行内 `HandleInput` 收不到指针事件（listRow
> 处理器优先），所以行内右键用 MyGo 原生 `Element.ContextMenu`（`menuTarget` 走
> `flagContextMenu`），而不是 `overlay.ContextMenu`（依赖 HandleInput，在行内失效）。

**代码抽屉（`drawer.go`）**：点击树中文件打开右侧 `overlay.Drawer`（宽 720、
`DrawerRight`，Escape / 头部 ✕ 关闭）：标题为工作区相对路径，工具行有截断标注、
`Raw/Fmt` 分段（可格式化语言）与 "Attach to conversation" 按钮，查看器
`code.CodeViewer` / `git.DiffViewer` 填满抽屉。抽屉内容区是 Scroll（grow 会塌），
因此查看器高度按窗高推导（`wh - 130`）显式给定。仓库面板底部标题行的 `maximize`
按钮把当前 Diff（diff 模式）或源码（文件模式）放大进同一抽屉。

**格式化视图（`fmtView` + `formatSource`）**：`Raw/Fmt` 分段切换只影响显示、
**不写回磁盘**（工作区树保持只读）。`formatSource` 进程内完成——`.go` 走
`go/format`（gofmt），`.json` 走 `json.Indent` 两空格美化；其它语言无格式化
（分段不出现），源码解析失败时显示原文。`fmtView` 缓存格式化结果（键 =
语言+NUL+内容），只在内容变化时重算，不逐帧跑 gofmt。

**附加到对话（`attachFile`）**：把文件加入 composer 的 `chat.ContextChips`
（`thread.ctx`，`ContextItem{ID:"file:<相对路径>", Kind:ContextFile}`）。入口有三处：
代码抽屉的 "Attach to conversation" 按钮、Repository 底部面板的 `plus` 按钮、
ChangesList 上的双击/Enter（`Submitted()`）。重复附加去重（toast "Already attached"），
上限 8 个。`send()` 时把附件内容折叠进消息：可见行只追加 `Attached: \`a\`, \`b\``，
LLM 消息（`row.llmText`，`historyMessages` 优先取用）携带 `--- 路径 ---` + 内容
（单文件 64 KiB 上限，读取失败写明原因），发送后清空 chips。

**目录列举规则（`listDir`）**：跳过 `.git`、`.gocache`、`.gopath`、`.mygo`、`node_modules`、
`__pycache__`、`.venv`、`venv`、`dist`、`build`、`target`、`.DS_Store`、`desktop.ini`；
目录排前、文件排后，各按大小写不敏感名称排序。子目录在父目录列出时注册为
`DataUnloaded`，展开时才真正读盘（懒加载）。

**文件读取（`readCapped`）**：最多读 256 KiB，超出置抽屉的 `truncated`；读失败置
抽屉的 `err`。`previewLang` 按扩展名映射高亮语言（`.go`→go、`.sh/.bash/.zsh`→shell、
`.js/.jsx/.mjs/.cjs`→javascript、`.ts/.tsx`→typescript、`.py`、`.json`、`.sql`），
其余纯文本（高亮器对未知语言安全回退）。

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
   - 标题行：`git.GitStatusBadge(status)` + 路径 + `maximize` 放大到代码抽屉 +
     `plus` 附加按钮。
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

对话内的所有卡片（思考 / 工具调用 / 命令执行 / 文件变更）均由 agent 循环的
**真实事件**驱动；测试与截图用 `convfixture_test.go` 手工构造代表性转录。
右栏 Repository 标签已全部接真实 git。

### 9.4 工作区存储与切换（`persist.go` + `internal/store`）

**默认根**：`store.HomeDir()`（`os.UserHomeDir()`，取不到时回退进程目录）——工作区
**不再**跟随可执行文件的启动目录。

**`openWorkspace(path) string`**（返回 toast 文案）：TrimSpace → `filepath.Abs` →
`os.Stat` 校验是目录（否则 toast "Not a directory: …"）→ 与当前相同则仅关对话框 →
否则：`saveSession` + `persistSessions` → 旧根进 recents → `a.ws = newWorkspace(abs)`
+ `reloadWorkspace()`（树与预览重置）→ `a.repo.vcs = vcsState{}`（git 面板对新根
重新收集）→ `restoreSession()`（恢复该工作区最新会话，无会话则空线程到欢迎页）→
`saveWsPrefs` + `persistSessions` → toast "Workspace: <目录名>"。

**`restoreSession()`**：按存储顺序找第一个 `ws == 当前根` 的会话，选中并恢复其
线程（`threads` 命中用缓存，否则空线程）；找不到则 `sessionID=""` +
空线程（欢迎页）。

**持久化文件**（**用户家目录** `~/.crux-agent/` 下——Windows 即
`C:\Users\<你>\.crux-agent\`，**不是** `%AppData%`，也不是 exe 所在目录；
0600/0700，路径字段为空即禁用——测试不碰盘。旧版 `%AppData%\MujicaUI-agent-demo\`
与 `~/.mujicaui-agent-demo/` 的数据启动时由 `store.Migrate()` 自动迁移：不覆盖
新文件，旧目录保留作备份）：

| 文件 | 内容 |
| --- | --- |
| `settings.json` | LLM 配置（API Key 明文，0600） |
| `workspace.json` | `{current, recents[]}` —— 当前工作区 + 最近列表（去重、上限 6，载入时当前根置顶） |
| `sessions.json` | `{sessions:[{id,title,updated,pinned,workspace,mode,threaded}]}` —— 会话**索引**（仅元数据，不含对话内容） |
| `sessions/<id>.json` | `{mode, rows[]}` —— **每个会话一个转录文件**（含合并的工具调用数据），打开会话时懒加载；`persistSessions` 只写索引 + 脏标记的转录（`saveSession`/`send`/流结束置脏）；旧的单文件格式（索引内嵌 rows）启动时一次性迁移到本布局，原文件保留为 `sessions.json.bak` |

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

- [ ] **首跑无种子**：会话列表为空、显示欢迎页；`New chat` 创建第一条会话。
- [ ] 输入一句话回车 → 用户行 + **一条助手消息**：思考与工具调用折叠在同一气泡的
  可收起块里（bash 终端卡 / 写文件评审卡 / 读文件工具卡），正文流式输出、完成后按
  **Markdown 渲染**（列表、表格、代码块）；列表滚到底。
- [ ] `New chat` / ⌘N → 清空到欢迎页；能力卡示例可填入草稿；starter chip 可即发。
- [ ] 会话列表切换 → 内容随之变化；切走再切回保留草稿。
- [ ] **侧栏是 workspace→session 两级树**：当前工作区节点自动展开、其会话嵌套在
  下（当前根与会话用强调色高亮）；点 workspace 行只展开/折叠（显示会话数徽标），
  点其它工作区下的会话会**先切工作区再打开会话**；末行 "Open workspace…" +
  `New chat` 按钮；重启后恢复上次工作区与会话（`workspace.json` / `sessions.json`）。
- [ ] 标题栏 `folder` 按钮**直达右侧目录树面板**；`git-pull-request` 开关整个检视器。
- [ ] 代码抽屉：树中点击文件滑出右侧大抽屉（宽 720），内容填满、Raw/Fmt 可切换、
  可附加到对话，Escape/✕ 关闭；仓库面板 `maximize` 把 Diff/源码放大进抽屉；
  刷新工作区会关闭抽屉。
- [ ] ⌘B / ⌘J 折叠左栏 / 右栏；标题栏按钮同效。
- [ ] ⌘K 打开面板；9 条命令各自生效。
- [ ] Workspace 标签：树列出真实目录（目录在前、噪音目录跳过）；展开子目录懒加载
  （加载中转圈、失败警示）；点文件滑出抽屉查看；**文件行右键 → "Add to conversation"**
  直接附加（composer 出现 chip，发送后内容进入 LLM 消息），菜单另有
  "Open in viewer" / "Copy path"；刷新按钮重载；状态栏显示工作区名。
- [ ] Repository 标签（真实 git）：列出真实分支/变更/历史；行内按钮 stage/unstage、
  组按钮全部暂存/取消；选中变更驱动 Diff（暂存= HEAD vs index，未暂存= index vs 工作区）；
  提交输入写标题后 Commit 真实提交；切分支/建分支生效；刷新按钮重收状态。
- [ ] Settings：正文与分隔线/滚动条留白（`PaddingX(22)`）；Reasoning 在 Providers
  分区且为 Off/Low/Medium/High 四档；Agent 分区只有系统提示 + 工具说明（无温度/
  限额/流式开关）；分区切换高度不变；**配置在对话框打开期间实时落盘**
  （改完直接杀进程，重启后配置仍在）。
- [ ] 代码查看：树点击打开大抽屉（宽 720、内容填满）；`.go`/`.json` 出现 Raw/Fmt
  分段，Fmt 显示 gofmt/美化内容（不写盘）；`.jsx`/`.zsh` 等映射高亮；仓库面板
  `maximize` 放大 Diff/源码。
- [ ] 助理消息：复制写入剪贴板；重新生成有反馈。
- [ ] 终端运行卡点停止 → agent 循环中止（Busy 清除）。

---

## 13. 上游组件映射

| 区域 | MujicaUI 组件 |
| --- | --- |
| 外壳/标题/状态 | `layout.TitleBar`、`layout.StatusBar` |
| 会话树/目录树 | MyGo `ui.Outline`（自定义行 + 原生 `Element.ContextMenu`） |
| 对话容器/列表 | `chat.ChatContainer`、`chat.MessageList`(+Options) |
| 消息体 | `chat.MessageBubble`、自研 `mdView`（可选中 Markdown）、`chat.StreamingText`、`chat.ThinkingBlock`、`chat.TypingIndicator` |
| 消息操作 | 自绘 `ui.ButtonBase` 图标按钮（copy / regenerate）+ 时间戳 |
| 输入区 | `chat.PromptComposer`、`chat.SendButton`、`chat.ContextChips`、`chat.TokenCounter` |
| Markdown | 自研 `mdView`（`internal/md` 解析 + `mdview.go` 渲染，全元素 Selectable）+ `chat.CodeBlock` |
| 欢迎 | `chat.WelcomeScreen`、`chat.SuggestionChips`、`chat.CapabilityCard` |
| Agent 卡 | `agent.ToolCallCard`、`agent.FileChangeCard`、`agent.CommandExecutionCard`、`agent.MultiFileDiffReview` |
| 仓库 | `git.BranchSelector`、`git.ChangesList`、`git.CommitInput`、`git.CommitList`、`git.DiffViewer`、`git.GitStatusBadge` |
| 代码 | `code.CodeViewer` |
| 命令面板 | `navigation.CommandPalette` |
| 图标/主题 | `icons.Must`、`core.Use`、`core.Tokens` |
| 布局原语 | MyGo `ui.Row/Column/Box/Scroll/Segmented/Button/ButtonBase/PrimaryButton/Icon/Text/Avatar` |

> 约束：所有固定数据一旦传入组件就必须满足其契约（否则 MujicaUI 会 panic）。
> 例如 `ChangesList` 的文件、`CommandPalette` 的 `Command.ID`。

---

## 14. 测试（`internal/app` 与各内部包的 `*_test.go`）

在无窗口环境下用 `ui.Render(view, w, h, scale)` / `ui.NewTester(view, w, h)` 驱动。

1. **`TestViewRenders`** —— 对 `showRepo ∈ {false,true}` × `codeTab ∈ {0,1}` 共 4 种组合渲染，
   断言非 nil（捕捉布局 panic / 组件契约缺失）。
2. **`TestSendAndNewChat`** —— `send()` 追加恰 2 行（用户 + 助理占位）、清空草稿、
   末行 `rowReasoned` 且 headless 落定占位文本；`newThread()` 后行数为 0 且欢迎页可渲染。
3. **`TestSessionSwitch`** —— 连建两个会话、在第一个里改草稿并 `saveSession`，
   切走再切回 → 草稿保留；切换关闭打开中的设置对话框。
4. **`TestPaletteCommands`** —— 逐个执行 10 条命令且每步可渲染；`workspace`/`diff`/`source`
   分别落到正确的 `showRepo`/`paneTab`/`codeTab`，`workspace-open` 打开工作区对话框。
5. **`TestSettingsDialogStableHeight` / `TestSettingsBodyHeight`**（`settings_nav_test.go`）——
   正文高度由窗高固定：切换分区正文矩形与标题 Y 不变，且随窗口变矮而变矮；钳制函数单测。
6. **`TestSettingsPersistence*` / `TestSettingsSaveOnClose` / `TestSettingsLiveSave`**
   （`config_test.go`）—— 持久化往返、瞬态字段不落盘、缺失/损坏回退默认、
   Escape 关闭对话框即保存、**对话框开着时值一漂移即写盘**。
7. **`TestListDir` / `TestWorkspaceLazyLoad` / `TestWorkspacePreview` /
   `TestWorkspaceTreeRender` / `TestTreeFileContextMenu` / `TestWorkspaceCommands`**
   （`workspace_test.go`）—— 用 `t.TempDir()` 造真实目录：列举排序（目录在前、
   大小写不敏感）与忽略规则；懒加载状态机（Unloaded→Loading→Ready/Failed）、文件是
   叶子；抽屉读取/语言映射/256 KiB 截断/缺失文件错误；`NewTester` 打开根目录 →
   树列出文件 → 点击行滑出抽屉；**文件行右键 → 菜单 "Add to conversation" 真实附加**；
   workspace / reload-workspace 命令（reload 关闭抽屉）。
8. **`TestFormatSource` / `TestFmtViewToggle` / `TestPreviewFormatToggle` /
   `TestSidebarWorkspaceList`**（`workspace_test.go`）—— gofmt 规范化 `.go`、
   `json.Indent` 美化、解析失败/其它语言不可格式化；`fmtView` 原始→格式化切换、
   缓存键随内容失效；抽屉 Raw/Fmt 渲染；**侧栏会话树**：当前工作区自动展开、
   其它工作区折叠，点 workspace 行只展开不切换，点其下会话先切工作区再打开。
9. **`TestParseStatus` / `TestParseBranchesAndLog` / `TestVCSRealRepo` /
   `TestRepositoryPaneAttach`**（`vcs_test.go`）—— porcelain 解析（分支头、
   `MM` 双条、重命名、删除）；用真实 git 命令在临时目录建仓库后跑完整版本管理闭环：
   未跟踪 → stage → 提交 → 再修改 → 建分支/切分支，断言每步的文件/版本/历史；
   `NewTester` 渲染 Repository 标签并点击附加按钮。
10. **`TestAttachFile`**（`vcs_test.go`）—— 附加去重、发送后 chips 清空、可见行带
   `Attached:` 注记、`llmText` 携带文件内容。
11. **`TestWsPrefsRoundtrip` / `TestSessionsRoundtrip` / `TestOpenWorkspace` /
    `TestSidebarFiltersSessions` / `TestWorkspaceDialog` / `TestHomeDirDefault`**
    （`wsstore_test.go`）—— `workspace.json` 往返（当前根 + recents 置顶）；
    **会话索引 + 每会话转录文件**往返（索引不含 rows、转录懒加载、打开即从
    `sessions/<id>.json` 恢复）、**旧单文件格式一次性迁移**（转录拆分、原文件
    留 .bak）；切换工作区的校验/重根/会话恢复/recents；侧栏树上其它工作区默认
    折叠、其会话不出现；对话框渲染 + 点 Open 真实切换；默认根 = 用户主目录。
12. **`TestDialogCloses`**（`dialogclose_test.go`）—— Escape 与背景点击关闭 Settings，
   且两条关闭路径都写出 `settings.json`（`configPath` 指向临时目录，不碰真实配置）。
13. **`TestAgentCardsRender` / `TestAgentTurnsMergeIntoOneRow`**（`agenttools_test.go`）——
    三类工具卡片行 + 思考行 headless 渲染；**一次回复合并为一行**（工具调用进该行
    卡片块、不建新行、完成后清 streaming）。
    **`TestToolsBash` / `TestToolsReadWrite` / `TestShellWrapper` /
    `TestStreamOptionsSampling` / `TestResolveModelOpenAICompat` / `TestThinkDefault` /
    `TestRunResolveError`**（`internal/engine`）—— bash 真实执行（echo 回显、失败
    命令带非零退出码）；read/write 往返（缺失文件报错、Details 携带 old/new、覆盖时
    Old 为旧内容）；平台 shell 包装；采样不暴露、Reasoning 档位映射、OpenAI 兼容
    端点解析、未知 provider 快速失败。
14. **`TestChatPaneLayout`**（`layoutfixed_test.go`）—— 用 `newConversationApp()`
   夹具（`convfixture_test.go`：一条含思考 + 三个工具卡 + Markdown 列表/表格正文
   的合并回复）回归 Row/Stretch 布局塌陷；首帧末行可见、上滚后首行与 Markdown
   元素可见。
15. **`TestProcSummary` / `TestEndStreamFoldsProcess` / `TestProcessLayers` /
    `TestThinkTimeline`**（`proclayers_test.go`）—— 过程区组头摘要纯函数全分支；
    完成即收起并钉住 doneAt；渲染断言（收起态不构建正文、组头单行、展开后
    按发生顺序逐行出现、每类工具图标行、思考首行预览、正文不被 200 行思考
    推出视口）；`setThink` 交错语义（同轮增长原地更新、工具后新段追加、
    thinkText 多段拼接）。
16. **`TestMdViewRender` / `TestMessageActionsCopyAndTime` /
   `TestRegenerateRow` / `TestWaitingIndicatorRenders`**（`mdview_test.go`）——
   渲染不 panic；**每条消息的复制按钮**（点击 → 剪贴板为该行文本）与时间显示；
   **重新生成**（删回复及其后行、追加新 streaming 行、id 换新、忙时拒绝）；
   等待首 token 时显示打字点。**`TestParseBlocks`**（`internal/md`）——
   解析器块形（散文跨段合并、有序/无序列表、表格、围栏、标题、引用、分隔线）。
17. **`TestDirMigration` / `TestSettingsRoundtrip` / `TestWsPrefsRoundtrip` /
   `TestSessionsRoundtrip` / `TestLegacyIndexMigration` /
   `TestTranscriptPathSanitize`**（`internal/store`）—— 旧目录迁移链
   （不覆盖新文件、旧目录保留）；settings / workspace / 会话索引 + 每会话转录的
   落盘往返与损坏回退；旧单文件索引一次性拆分（转录落盘、索引去 rows、原文件留
   `.bak`、二次运行幂等）；转录路径 id 消毒防目录逃逸。
 18. **权限门 / 沙箱 / Claude 后端**（`internal/engine`）—— `gate_test.go`
    （模式解析、门表全分支、nil/超时审批失败关闭）；`sandbox_test.go`
    （边界构造、凭据目录拒绝、平台可用性）；`tools_test.go`（三档分派、
    工作区收束、fake 沙箱执行 HOME=scratch）；`cliproc_test.go`（Reap 杀
    lingerer、脱敏、截断）；`claude_events_test.go`（15 个行级事件测试）；
    `claude_proc_test.go`（**进程级** fake CLI——Windows .cmd / Unix .sh，
    真实 spawn：标志位、prompt-on-stdin、审批 wire 往返、常驻 CLI 收割、
    取消终止、stderr 错误尾）；`claude_live_test.go`（**真实 CLI 冒烟**，
    `CRUX_LIVE_CLAUDE=1` 开启，否则跳过）。
 19. **`TestProvidersPaneAgentTypeSwitch` / `TestClaudePaneContent` /
    `TestBackendLabelClaude` / `TestDetectClaudeHeadless`**
    （`settings_claude_test.go`）—— Agent type 分段切换换表单（API key
    消失、CLI 探测行出现、切回还原）；claude 表单内容；后端标签跟随；
    headless 探测不 panic。
 20. **`TestComposerModeSelector` / `TestApprovalCardAllow` /
    `TestApprovalCardDeny` / `TestAwaitApprovalHeadless` /
    `TestAwaitApprovalBridge` / `TestClaudeToolNameMapping` /
    `TestEngineConfigCarriesGate` / `TestClaudeIDPersists`**
    （`approval_test.go`）—— composer 模式选择器（切换落值、忙时冻结）；
    审批卡生命周期（渲染、点击落定通道、拒绝带原因）；headless 桥
    （无 redraw 上下文触发即拒绝）；claude 工具名映射与 Edit/Write 从
    args 重建 FileChange；engineConfig 携带门全量；claudeID 转录往返。

---

## 15. 已知限制与后续

**限制**

- Agent 走 pi-ai-go 的 **agent 循环**（多轮 + 工具执行）；跨轮历史由
  `historyMessages()` 从对话行重建（仅 user/assistant 文本行），工具调用与结果
  不回灌到下一轮的 LLM 历史（每轮工具上下文在循环内部完整，跨 send 丢失）。
- 工具经**权限三档门**（§7.2.1）：macOS / Linux 有 OS 沙箱，**Windows 无**——
  Agent 档下 bash 走逐条审批卡（卡即边界）；"Always allow" 不持久化
  （等同 Allow once）；审批 10 分钟未决即拒绝。
- **Claude 后端**：一轮 = 最后一条用户消息整句重发（CLI 自持历史，
  `claudeID` 跨轮 resume）；**regenerate = 删回复行重发**，CLI 侧已发生的
  侧效不回滚；中途切换后端 = 新 claude 会话；回复行尾的 note（费用 /
  会话号 / 压缩通知）不持久化。
- 配置持久化到家目录 `~/.crux-agent/settings.json`（含 API Key 明文，
  文件 0600 / 目录 0700，对话框打开期间实时写盘）；API Key 也可留空走 Provider
  的环境变量。
- 无窗口（测试）环境 `send()` 落定占位文本，不做真实网络调用。
- git 走真实命令，但只覆盖本地操作（status/diff/log/add/restore/commit/checkout）：
  无 push / pull / fetch，不做合并冲突处理；`git restore --staged` 需要 git ≥ 2.23。
- 附件折叠进 LLM 消息时单文件上限 64 KiB（超出截断并注明）；预览上限 256 KiB；
  二进制文件显示占位文案。
- 语法高亮只支持 CodeViewer 内置的 7 种语言（go / javascript / typescript /
  python / json / shell / sql），`.md` / `.yaml` / `.html` 等按纯文本渲染；
  格式化仅进程内两种（`.go` gofmt、`.json` 美化），且只影响显示、不写盘。
- **不引入 LSP**（评估过 terax-clone 的做法：Go 侧 spawn 语言服务器、JSON-RPC
  over stdio 桥接给 CodeMirror 编辑器）。Crux 的 `code.CodeViewer` 是只读视图，
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
- 工具结果回灌跨轮历史、`EventToolExecUpdate` 流式输出、更多工具
  （list_dir / grep / edit 补丁式修改）、审批卡按工具 / 按会话的持久化
  放行规则、Windows OS 级沙箱接入。
- 将 API Key 迁移到系统 Keychain / 加密存储（当前为明文 JSON）。