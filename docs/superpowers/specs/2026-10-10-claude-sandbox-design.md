# Crux 接入 Claude Code 后端与沙箱/审批体系 — 设计文档

> 日期:2026-10-10 · 状态:已与用户确认的设计,待实施
> 参考实现:`mygo-agent`(hyperframes-test/mygo-agent)的 `internal/harness/claude`、
> `internal/harness/{sandbox,permissions,approvals,diff}.go`、`internal/providers/sandbox`、
> `internal/harness/cli`。
> 本文档是该特性的设计契约;实施后行为变更需回写 `SPEC.md`。

## 1. 目标与非目标

**目标**

1. **Claude Code 成为 Crux 的一类 agent 后端**(与"内置 LLM 循环"平级):通过
   `claude -p` 双向 stream-json 控制协议驱动真实对话——流式文本、工具调用卡、
   Edit/Write diff 卡、会话 `--resume` 续接、`can_use_tool` 审批直达审批卡。
2. **接入沙箱/审批体系**(参考 mygo-agent 的安全模型,采用 DSH 三档可选语义):
   composer 上三档模式选择器 **Read Only / Agent / Full Access**,模式本身即
   "是否需要审批"的开关;macOS/Linux 的 Agent 模式走真 OS 沙箱(Seatbelt /
   bubblewrap),Windows 无 OS 沙箱后端,**审批卡即沙箱边界**。

**非目标**(明确不做)

- 不移植 mygo-agent 的 harness 整层:MCP 服务器、agent 档案(agents.md)、
  panel/relay 多 Agent 协作、memory/ChatLog、委托工具、逐工具 rules 选择器
  (`permissions.rules`)——Crux 只取权限门 + 沙箱 + 审批流 + claude 适配器。
- 不实现 Windows OS 级沙箱(受限令牌/AppContainer)。
- 不给审批卡加 "always allow" 持久授权(与参考一致:持久授权只来自配置,
  Crux 本期不提供 rules 配置面)。

**与参考实现的两处明确偏离**(用户已确认)

1. Windows 上 Agent 模式的 bash **不报错**,降级为逐命令审批卡
   (mygo-agent 在无沙箱平台上"诚实报错";Crux 采用 DSH 语义:三档可选,
   无 OS 沙箱时审批卡就是边界)。
2. Windows 上停止 claude 运行用 `taskkill /PID <pid> /T /F` **杀整棵进程树**
   (claude.cmd 是 wrapper,只杀主进程会留 node 残尸;参考实现 Windows 无进程组
   只杀 leader)。

## 2. 权限门(三档模式)

### 2.1 模式与解析

`internal/engine/gate.go`:

```go
type Mode int
const (
    ModeReadOnly Mode = iota // 只读
    ModeAgent                // 工作区写 + 沙箱/审批
    ModeFull                 // 全权,不设防
)
```

模式是**应用级**状态,持久化在 `settings.json`(`"mode"`:"read-only"/"agent"/
"full",缺省 `agent`),composer 底部分段控件随时可切,下一轮生效。

**动作映射**(对齐 mygo-agent `permissions.md` 的封闭目录):

| 工具 | 动作 |
| --- | --- |
| `bash` | `shell.exec` |
| `read_file` | `file.read` |
| `write_file` | `file.write |

**解析表**(无 rules,只有模式默认值):

| 模式 | bash | write_file | read_file |
| --- | --- | --- | --- |
| Read Only | 拒绝(文案点名模式,模型可读:"read-only mode: the bash tool is not available; ask the user to change the approval mode") | 拒绝(同上) | 允许;**凭证目录拒绝**(`~/.ssh`、`~/.aws`、`~/.gnupg`、`~/.kube`、`~/.docker`,与沙箱 denylist 同一份清单) |
| Agent | macOS/Linux:**OS 沙箱执行**;Windows:**ask(审批卡)** | 允许;**路径必须落在工作区内**(应用层检查,跨平台真实生效),越界拒绝 | 允许(凭证目录仍拒绝) |
| Full | 直接执行 | 允许(不检查) | 允许(不检查) |

拒绝文案经工具结果回给模型(不是崩溃、不是静默空操作),模型可据此改道。

### 2.2 审批流不变量(照搬 mygo-agent `approvals.md`)

- 每张卡只对**本次调用**生效;没有 "always allow"。
- **10 分钟**无响应自动拒绝(`ApprovalContext`,`context.WithTimeoutCause`,
  超时原因写进拒绝文案)。
- 运行被停止(cancel)时,未决卡按拒绝落定(原因 "cancelled")。
- `OnApproval == nil`(headless/测试)时 ask 按拒绝落定。
- 模型输出永远无法创造授权。

```go
type ApprovalRequest struct {
    Tool    string // 工具名(builtin: bash/write_file;claude: Bash/Edit/Write/…)
    Summary string // 有界、脱敏的一行呈现(命令文本/路径)
    Reason  string // 固定词汇:"claude asks to use this tool" / "agent mode requires approval"
}
type ApprovalDecision struct {
    Approved bool
    Reason   string // 拒绝原因回给模型;批准时空
}
```

## 3. 沙箱层

`internal/engine/sandbox.go` + 平台文件(自 mygo-agent `providers/sandbox`
原样移植,接口归 engine):

```go
type Boundary struct {
    Workdir string // 工作区:读写
    Scratch string // 每次调用新建的临时目录:读写,子进程 HOME 与 TMPDIR
    Network string // "deny"
}
type Sandbox interface {
    Command(ctx, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error)
}
```

- **darwin**:`/usr/bin/sandbox-exec` + 生成的 `(deny default)` SBPL——全域可读
  (凭证 denylist 更specific 覆盖拒绝)、Workdir/Scratch 读写、网络默认拒绝。
- **linux**:`bwrap`(PATH 上有才支持)——`--unshare-pid/ipc/uts/net`、
  `--ro-bind / /`、凭证目录 tmpfs 掩埋、`--bind` Workdir/Scratch、`--cap-drop ALL`。
- **windows**:`Unavailable()` 返回真——不是错误路径,是**能力声明**:权限门读到
  "无 OS 沙箱" 就把 bash 降级为 ask(§2.1)。诚实规则保留为:Windows 上**绝不**
  静默无沙箱执行 Agent 模式命令——要么审批卡,要么用户切 Full Access。
- 沙箱执行的子进程环境:HOME/TMPDIR 指向 Scratch(缓存与临时文件落在边界内),
  Scratch 在命令结束后删除;命令被杀/超时的结果标注"结果未知"而非干净失败。

只有**内置后端 Agent 模式的 bash** 走沙箱;claude 后端不套进程沙箱
(与参考一致:claude 的 Agent 模式审批卡即边界)。

## 4. Claude Code 适配器

`internal/engine/claude.go` + `claude_events.go`(合计约 600 行,遵守 <500 行/文件
拆两片;无 UI 依赖)。

### 4.1 进程协议(自 mygo-agent `cli-backends.md` 移植,含全部已踩坑)

- **Spawn**:`claude -p --output-format stream-json --verbose --input-format
  stream-json --include-partial-messages --permission-mode <perm> --model <别名>`,
  `--resume <claudeID>`(有会话时)、`--append-system-prompt <系统提示>`(非空时);
  `cmd.Dir = 工作区`。Windows 经 `exec.LookPath("claude")` 解析到 `claude.cmd`
  (已验证可 spawn;native 安装的 `claude.exe` 同样命中)。
- **模式映射**:Read Only / Agent → `--permission-mode default`(差别在宿主侧
  应答行为:前者一律拒绝,后者转发审批卡);Full → `bypassPermissions`
  (CLI 不再发 can_use_tool)。
- **Prompt**:stdin 写一行
  `{"type":"user","message":{"role":"user","content":[{"type":"text","text":…}]}}`。
- **收尾**:`result` 事件即回合结束,但 CLI 常驻等下一条输入——**读到 result
  主动 break 并关 stdin**,否则回合永不落定(参考第一课)。
- **收割**(移植 `cli.Reap`):关 stdin → 3s 宽限 → 组 SIGTERM(unix)/
  **`taskkill /T /F` 杀树(windows)** → leader kill,`Wait` 必须总能返回。
- **终止语义**:ctx 取消且已有事件 → 干净停止(已到内容保留);**一句没说
  就被切断的运行必须报错**("stopped before it answered — <原因>"),不能当
  干净回合;stderr 尾部成为失败文案。

### 4.2 事件映射(进现有 `engine.Callbacks`,UI 渲染零改动)

| stream-json 帧 | Callbacks |
| --- | --- |
| `stream_event`/`content_block_delta`/`text_delta` | `OnText`(按本轮累计;**partial 与完整 assistant 消息可能重复**——只发未覆盖后缀,`tool_use` 处重置累计) |
| `assistant`/`tool_use` | `OnToolStart`:`Bash`→命令卡(截断 200 字符);`Edit`/`Write`→`UnifiedDiff(old,new)` 预览的 FileChange 数据(Write 先读旧文件);`Read`→工具卡;其余→名称+摘要参数 |
| `user`/`tool_result` | `OnToolEnd`(输出 16 KiB 封顶,头尾保留式截断) |
| `system/init`(`session_id`) | 新增 `OnSession(id)`:落盘到会话转录,崩溃中途也能续接 |
| `system/compact_boundary`(**snake_case** `compact_metadata`)与 `result`(`duration_ms`/`total_cost_usd`/`usage`) | 新增 `OnNote(text)`:一行 muted 备注("claude compacted its context · 25.9k → 5.3k tokens…" / "Done in 12.3s · $0.0042 · 127 tokens") |

### 4.3 控制请求(can_use_tool)

`control_request` 到达 stdin 流:

- `subtype == "can_use_tool"`:
  - Read Only:直接回 deny(文案点名模式),不问人;
  - Agent:`ApprovalContext`(10 分钟)内调 `Config.OnApproval` → 审批卡;
    批准回 `{"behavior":"allow","updatedInput":<回显 input>}`,拒绝回
    `{"behavior":"deny","message":<原因>}`;
  - 超时/取消的"迟到决定不算决定"——按拒绝落定,原因写明。
- **其它任何 server-initiated subtype**:一律回 `{"subtype":"error","request_id":…,
  "error":"unsupported by crux"}`——晾着不答会让 CLI 永久卡死(参考的硬规则)。

应答以 `{"type":"control_response","response":…}` 写回 stdin。

### 4.4 派发与历史

`engine.Config` 增 `Backend`("builtin"|"claude",缺省 builtin)、`Mode`、
`OnApproval`、`ClaudeSessionID`;`engine.Run` 按后端派发:

- **claude**:prompt = 本轮用户消息(附件折叠同现有逻辑);历史行**不回传**
  ——CLI 的会话文件自管上下文,续接靠 `--resume <claudeID>`。切换后端或无
  claudeID 时首轮全新(限制,README 注明)。
- **builtin**:pi-ai 循环照旧,唯工具 `Execute` 外包一层权限门(§2),
  Agent 模式 bash 经沙箱执行(§3)。

## 5. UI 层

### 5.1 Settings:Agent 类型(Providers 分区顶部)

分段控件 `内置 LLM 循环 | Claude Code CLI`,写 `settings.json` 的
`"backend"` 字段(缺省 builtin,旧文件平滑取默认):

- 选 **Claude Code** 时表单切换:**CLI 检测**(goroutine 跑
  `claude --version`,显示路径 + 版本或"未找到")、**模型别名**
  (sonnet / opus / haiku / 自定义,复用现有 `Model` 字段存自由文本)、
  系统提示(继续有效);Key / Base URL / Reasoning 隐藏。
- 选 **内置** 时表单原样。

### 5.2 composer:模式选择器 + 后端标签

- 底部行新增 `ui.Segmented` 三档 `Read Only | Agent | Full Access`
  (DSH 风格;MujicaUI 的 `chat.ModeSelector` 是回答风格,语义不符,不用),
  写应用状态并持久化;Busy 时禁切。
- 后端标签同步显示 `claude · sonnet` 或 `deepseek · <model>`。

### 5.3 审批卡(过程时间线内)

- `app` 增待决状态 `pending *approvalReq{req, ch chan ApprovalDecision,
  at time.Time}`;引擎侧 `OnApproval` 经 `a.redraw` 挂入状态后**阻塞在 channel
  或 ctx**(全程序唯一一张待决卡;goroutine 不碰状态,决定经 channel 回引擎,
  符合本仓线程约定)。
- 渲染:当前回复行的过程时间线里新增**审批步骤**(与工具步同级),
  `agent.PermissionPrompt{Title:"<Tool>?", Description:Summary, Scope:"本次调用"}`;
  "Allow always" 按一次处理(不落任何持久授权);`Choice` 到达即清 pending、
  回写 channel。
- 审批等待中 bash 命令卡显示等待态;停止按钮照常可用(= 拒绝并中止整轮)。
- 超时由引擎侧 `ApprovalContext` 强制(10 分钟),UI 不做倒计时,只显示等待
  已持续时长。

### 5.4 claude 专属渲染

- `Edit`/`Write` → `FileChangeCard`(UnifiedDiff 计算 old/new);
- `Bash` → `CommandExecutionCard`(审批在执行前,卡片先显示等待);
- result 备注 → 回复完成后 actions 行旁一行 muted 文本;
- 重新生成:删除本回复及其后行、以同一 prompt 重发(claude 会话继续追加,
  上下文有重复但功能成立;限制注明)。

## 6. 持久化(`internal/store`)

| 文件 | 变更 |
| --- | --- |
| `settings.go` | `LLMConfig` 增 `Backend string \`json:"backend,omitempty"\``、`Mode string \`json:"mode,omitempty"\``;旧文件缺字段 → builtin/agent 默认(零值归一,无需重写) |
| `sessions.go` | 转录 schema 增 `ClaudeID string \`json:"claudeID,omitempty"\``;`OnSession` 即落盘(崩溃中途可续接) |

## 7. 测试(全平台无窗口可跑)

1. **行级**(移植 mygo-agent `claude_test.go` 纯函数用例,不 spawn 进程,
   Windows 也跑):stream-json 事件映射(含 Edit diff 预览)、result 备注
   (含 usage)、read-only 直接拒绝、**超时即拒绝**(迟到决定不算)、
   partial 去重(tool_use 后重置)、compact 计数(snake_case)、
   其它 system 帧不出备注。
2. **进程级**:fake CLI 用 Windows `.cmd` 与 Unix `.sh` 双版本(echo JSON 行),
   两平台都跑完整闭环:spawn → prompt 落 stdin → can_use_tool 审批应答在
   线上 → result → 常驻 CLI 被 reap 干净落定;参数用例钉住
   `--include-partial-messages`/`--permission-mode`/`--resume`/`--append-system-prompt`
   的旗标组合。
3. **gate**:三档解析全表、read-only 拒绝文案、write 越界拒绝、凭证目录拒绝
   (read 与 bash 同一清单)、Windows 降级 ask、`OnApproval == nil` → ask 拒绝。
4. **UI 渲染**(`ui.Render`/`ui.NewTester`,渲染测试兜底):模式选择器切换、
   审批卡出现/点击回决定/超时清卡、claude 设置表单两形态、headless send
   双后端确定性落定。
5. **live probe**:`CRUX_LIVE_CLAUDE=1` 才跑真 `claude` CLI(最小 prompt +
   事件流断言;默认跳过,CI 无凭据也绿)。

## 8. 交付物清单

```
internal/engine/claude.go          适配器:spawn/prompt/收割/终止语义
internal/engine/claude_events.go   事件解析:handle/controlRequest/toolUse/toolResult
internal/engine/gate.go            Mode/动作映射/解析表/Approval 类型与超时
internal/engine/sandbox.go         Boundary/Sandbox 接口/windows 能力声明
internal/engine/sandbox_darwin.go  Seatbelt profile(移植)
internal/engine/sandbox_linux.go   bwrap 参数(移植)
internal/engine/diff.go            UnifiedDiff(移植)
internal/engine/cliproc.go         Reap/Trunc/TrimOutput/Redact(移植)
internal/engine/cliproc_windows.go taskkill /T 杀树;cliproc_unix.go 进程组
internal/engine/loop.go            Config 扩展 + Run 派发
internal/engine/tools.go           工具过门/路径限制/凭证拒绝
internal/app/llm.go                双后端派发 + 审批桥 + claudeID 落盘
internal/app/thread.go             模式选择器 + 审批步骤渲染
internal/app/settings.go           Agent 类型表单
internal/app/state.go              pending approval 状态
internal/app/persist.go            claudeID 转录读写
internal/store/settings.go         backend/mode 字段
internal/store/sessions.go         ClaudeID 字段
docs/superpowers/specs/…           本设计文档
测试:engine 行级/进程级、gate、app 渲染、live probe
README.md / SPEC.md                特性、三档语义表、限制(切后端/重新生成)回写
```

## 9. 验收标准

- [ ] Settings 选 Claude Code + composer 发送 → 真 `claude` 进程驱动回复:
      流式正文、Bash 命令卡、Edit/Write diff 卡、完成备注(耗时/费用/token)。
- [ ] claude 会话重启后 `--resume` 续接(claudeID 落盘)。
- [ ] 三档模式对两个后端都真实生效:
      Read Only(builtin bash/write 拒绝;claude can_use_tool 全拒);
      Agent(mac/linux 沙箱 / windows 审批卡;write 限制工作区);
      Full(不设防,claude bypassPermissions)。
- [ ] 审批卡:Allow once / Deny、10 分钟自动拒绝、停止即拒绝、无 always。
- [ ] 停止按钮:builtin 中止循环;claude 被 reap(Windows 无 node 残尸)。
- [ ] `go test ./...` 全绿(Windows 与 Unix)、`gofmt -l .` 无输出、
      `go vet ./...` 干净;渲染测试覆盖新面板状态。
- [ ] 旧 settings.json / 会话文件无破坏性迁移。

## 10. 风险与对策

| 风险 | 对策 |
| --- | --- |
| `claude.cmd` 批处理 wrapper 的进程树清理 | Windows 版收割用 `taskkill /T /F`;进程级测试覆盖 |
| stream-json 协议阴招(partial 重复、compact snake_case、control 晾死、result 后常驻) | 全部照搬参考的已验证处理 + 行级测试钉死 |
| 审批回调死锁(UI 不回、卡挂着) | 引擎侧 10 分钟硬超时 + cancel 路径拒绝;唯一待决卡,新请求排队语义 = 顺序执行(工具本就串行) |
| LookPath 找不到 claude(native 安装路径不同) | 启动时与设置页检测并显示;错误文案给出装法 |
| 会话中途切后端历史断层 | claudeID 为空时全新会话;README/SPEC 注明 |
