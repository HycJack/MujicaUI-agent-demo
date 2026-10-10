# Claude Code 后端 + 沙箱/审批体系 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 Crux(test/crux-agent)接入两类 agent 后端选择(内置 pi-ai 循环 / Claude Code CLI)与 DSH 式三档权限模式(Read Only / Agent / Full Access),含审批卡、沙箱与 claude 会话续接。

**Architecture:** 全部协议件从 mygo-agent 参考实现移植进 Crux 的三层结构:`internal/engine`(权限门、沙箱、claude stream-json 适配器)、`internal/store`(字段扩展)、`internal/app`(设置表单、模式选择器、审批卡、派发)。UI 渲染沿用现有 row/step 模型,零新增渲染概念(审批卡是过程时间线里的一种步骤)。

**Tech Stack:** Go 1.27 · pi-ai-go v0.3.0 · MujicaUI(chat/agent/core) · MyGo ui · Claude Code CLI 2.1.x(stream-json 双向协议)

**Spec:** `docs/superpowers/specs/2026-10-10-claude-sandbox-design.md`(本计划从 spec 立论,执行者两份都要读)

**参考实现源码**(移植时逐行对照,路径前缀 `REF = C:\Users\huangyicao\Downloads\hyperframes-test\mygo-agent`):

| 参考文件 | 内容 |
| --- | --- |
| `REF/internal/harness/approvals.go` | ApprovalContext / 超时不变量 |
| `REF/internal/harness/cli/cli.go` | Trunc/ShortArgs/TrimOutput/Redact/CompactedNotice |
| `REF/internal/harness/cli/reap.go` | Reap(收割) |
| `REF/internal/harness/cli/proc_unix.go, proc_windows.go` | 进程组属性 |
| `REF/internal/providers/sandbox/{darwin,linux,other}.go` | 沙箱平台后端 |
| `REF/internal/harness/claude/claude.go` | claude 适配器全量(637 行) |
| `REF/internal/harness/claude/claude_test.go` | 行级测试蓝本 |

## Global Constraints

- AGENTS.md 约定:**文件 < 500 行**;`go doc` 查签名再写,**不猜**;改完渲染测试兜底。
- 每任务收尾:`gofmt -l .` 无输出、`go vet ./...` 干净、`go test ./...` 全绿,然后 commit(仓库:`test/crux-agent`,已有未提交的用户改动,**只 `git add` 本任务自己的文件**)。
- goroutine 不直接碰状态:一律 `a.redraw(...)`(MyGo 约定,spec §5.3)。
- 无窗口环境(测试)必须确定性:不 spawn 真网络进程;live 测试用环境变量门控。
- 颜色走 `tokens(c)`,不自造色值;图标名先核对 `icons/svg`(已核实可用:`terminal` `pencil` `file-text` `bot` `brain` `circle-check` `circle-x` `shield` 不确定是否存在——用前 `go doc .../icons Names` 查)。
- 拒绝文案是**给模型读的工具结果**(不是 panic、不是静默);审批无 "always allow";10 分钟超时即拒绝。
- 已验证事实:Windows 上 `exec.LookPath("claude")` → `C:\Users\huangyicao\AppData\Roaming\npm\claude.cmd`,Go 可直接 spawn(`--version` 返回 `2.1.25 (Claude Code)`)。
- **对 spec 的两处执行期简化**(实施时按此办,完成后在 spec 文档追加 changelog 注记):
  1. 不移植 `UnifiedDiff`:MujicaUI `agent.FileChangeCard(Path, Old, New)` 自带 before/after 渲染,Edit 的 old/new 字符串直接喂卡即可。
  2. 不做 `--add-dir` 询问(outside-dir 授予);claude 工作区边界就是 cwd(工作区根),越界读取由 CLI 自拒。

## Review Focus

审阅者重点盯这五类失败:
1. **审批死锁/泄漏**:`OnApproval` 是否所有路径(批准/拒绝/超时/取消/无 handler)都返回且 `a.pending` 清干净;channel 是否带缓冲避免 UI 线程阻塞。
2. **stream-json 阴招**:partial 与完整 assistant 重复发文本;result 后 CLI 常驻不退;control_request 晾死;`compact_metadata` snake_case。
3. **终止语义**:一句没说就被切断的运行必须报错,不能当干净回合(ctx 取消但已有事件 → 干净停止)。
4. **fail-closed**:模式解析垃圾值 → Read Only;`OnApproval == nil` 的 ask → 拒绝;未知 control subtype → error 应答。
5. **Windows 真实性**:claude.cmd wrapper 的进程树必须被 `taskkill /T` 收掉;`.cmd` fake CLI 测试在 Windows 必须真的跑(不能 `!windows` 跳过)。

## File Structure

```
internal/engine/
  gate.go              [新] Mode 枚举/解析、三档解析表、Approval 类型、ApprovalContext
  cliproc.go           [新] Reap/Trunc/ShortArgs/TrimOutput/Redact/CompactedNotice(移植)
  cliproc_windows.go   [新] taskkill /T 杀树;cliproc_unix.go [新] 进程组 SIGTERM
  sandbox.go           [新] Boundary + Sandbox 接口(含 Available)
  sandbox_darwin.go    [新] Seatbelt(移植)  sandbox_linux.go [新] bwrap(移植)
  sandbox_windows.go   [新] 不可用声明(诚实报错文案)
  claude.go            [新] RunClaude:spawn/参数/prompt/收割/终止语义
  claude_events.go     [新] run 状态 + handle/controlRequest/toolUse/toolResult
  loop.go              [改] Config 扩展 + 后端派发;Callbacks 增 OnSession/OnNote
  tools.go             [改] Tools(workdir, Gate):过门/路径限制/凭证拒绝/沙箱执行
  测试:gate_test.go cliproc_test.go sandbox_test.go tools_test.go[改]
       claude_events_test.go claude_proc_test.go loop_test.go[改] claude_live_test.go
internal/store/
  settings.go          [改] LLMConfig + Backend/Mode
  sessions.go          [改] Transcript + ClaudeID
internal/app/
  settings_claude.go   [新] Agent 类型分段 + claude 表单 + CLI 检测
  approval.go          [新] approvalReq/awaitApproval/settleApproval + 审批卡渲染
  llm.go               [改] engineConfig 扩展、OnApproval 桥、OnSession/OnNote 落地
  thread.go            [改] composer 模式选择器、审批步骤挂载、claude 工具名映射
  state.go             [改] thread.claudeID、a.pending、a.claudeBin
  persist.go           [改] 转录读写带 claudeID;loadSettings 归一 backend
  测试:approval_test.go settings_claude_test.go llm_test.go[增] thread_test.go[增]
docs/superpowers/{specs,plans}/…  README.md[改] SPEC.md[改]
```

---

## Task 1:engine 权限门(gate.go)

**Files:** `internal/engine/gate.go`(新)、`internal/engine/gate_test.go`(新)

**Why first:** 后续所有任务(builtin 过门、claude can_use_tool)都消费这些类型;零依赖。

- [ ] **写失败测试** `gate_test.go`:

```go
package engine

import (
	"context"
	"testing"
	"time"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":            ModeAgent, // 缺省 = agent(旧 settings 无此字段)
		"agent":       ModeAgent,
		"read-only":   ModeReadOnly,
		"readonly":    ModeReadOnly,
		"full":        ModeFull,
		"full-access": ModeFull,
		"nonsense":    ModeReadOnly, // fail closed:拼错不是许可
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestModeString(t *testing.T) {
	if ModeReadOnly.String() != "read-only" || ModeAgent.String() != "agent" || ModeFull.String() != "full" {
		t.Fatal("Mode.String round-trip broken")
	}
}

// TestGateResolve pins the three-mode table (spec §2.1): sandboxOK is the
// "OS sandbox backend exists" input that decides agent-mode bash.
func TestGateResolve(t *testing.T) {
	type tc struct{ tool string; mode Mode; sandboxOK bool; want Permission }
	cases := []tc{
		{"read_file", ModeReadOnly, false, PermAllow},
		{"read_file", ModeAgent, true, PermAllow},
		{"read_file", ModeFull, true, PermAllow},
		{"write_file", ModeReadOnly, false, PermDeny},
		{"write_file", ModeAgent, true, PermAllow},
		{"write_file", ModeFull, true, PermAllow},
		{"bash", ModeReadOnly, false, PermDeny},
		{"bash", ModeAgent, true, PermAllow},  // mac/linux: OS sandbox
		{"bash", ModeAgent, false, PermAsk},   // windows: 审批卡即沙箱
		{"bash", ModeFull, false, PermAllow},
		{"nonesuch", ModeFull, true, PermDeny}, // unknown tool fails closed
	}
	for _, c := range cases {
		if got := gateResolve(c.tool, c.mode, c.sandboxOK); got != c.want {
			t.Errorf("gateResolve(%s,%v,%v) = %v, want %v", c.tool, c.mode, c.sandboxOK, got, c.want)
		}
	}
}

func TestDenialText(t *testing.T) {
	s := denialText("bash", ModeReadOnly)
	if !strings.Contains(s, "read-only") || !strings.Contains(s, "bash") {
		t.Fatalf("denialText: %q", s)
	}
}

func TestApprovalContext(t *testing.T) {
	ctx, cancel := ApprovalContext(context.Background(), 20*time.Millisecond)
	defer cancel()
	<-ctx.Done()
	if ApprovalReason(ctx) != "approval timed out" {
		t.Fatalf("reason: %q", ApprovalReason(ctx))
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	ctx2, _ = ApprovalContext(ctx2, 0) // 0 → DefaultApprovalTimeout
	cancel2()
	<-ctx2.Done()
	if ApprovalReason(ctx2) != "cancelled" {
		t.Fatalf("reason: %q", ApprovalReason(ctx2))
	}
}
```

- [ ] `go test ./internal/engine -run 'TestParseMode|TestModeString|TestGateResolve|TestDenialText|TestApprovalContext'` 确认**编译失败/测试失败**。
- [ ] **实现** `gate.go`(骨架,移植自 `REF/internal/harness/approvals.go` 的超时件):

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Mode is the three-level approval selector (spec §2.1), DSH-style: the
// mode itself is the "does this need approval" switch.
type Mode int

const (
	ModeReadOnly Mode = iota
	ModeAgent
	ModeFull
)

func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "agent":
		return ModeAgent
	case "read-only", "readonly":
		return ModeReadOnly
	case "full", "full-access", "fullaccess":
		return ModeFull
	default:
		return ModeReadOnly // a typo is not permission
	}
}

func (m Mode) String() string {
	switch m {
	case ModeReadOnly:
		return "read-only"
	case ModeFull:
		return "full"
	}
	return "agent"
}

// Permission is what the gate decided for one prepared call.
type Permission string

const (
	PermAllow Permission = "allow"
	PermDeny  Permission = "deny"
	PermAsk   Permission = "ask"
)

// gateResolve is spec §2.1's table. sandboxOK says an OS sandbox backend
// exists on this platform — the input that turns agent-mode bash from
// "sandboxed, no ask" into "the approval card is the sandbox".
func gateResolve(tool string, mode Mode, sandboxOK bool) Permission {
	switch tool {
	case "read_file":
		return PermAllow
	case "write_file", "bash":
		switch mode {
		case ModeReadOnly:
			return PermDeny
		case ModeAgent:
			if tool == "bash" && !sandboxOK {
				return PermAsk // Windows: approval card is the boundary
			}
			return PermAllow
		default:
			return PermAllow
		}
	default:
		return PermDeny // unknown tool fails closed
	}
}

// denialText is the model-readable refusal (never a panic, never silent).
func denialText(tool string, mode Mode) string {
	return fmt.Sprintf(
		"%s mode: the %s tool is not available; ask the user to change the approval mode",
		mode, tool)
}

// --- approval plumbing (ported from REF/internal/harness/approvals.go) ---

const DefaultApprovalTimeout = 10 * time.Minute

var ErrApprovalTimedOut = errors.New("approval timed out")

type ApprovalRequest struct {
	Tool    string
	Summary string // bounded, redacted one-line ask
	Reason  string // fixed vocabulary
}

type ApprovalDecision struct {
	Approved bool
	Reason   string // the denial's reason, fed back to the model
}

type ApprovalFunc func(ctx context.Context, req ApprovalRequest) ApprovalDecision

func ApprovalContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	return context.WithTimeoutCause(ctx, timeout, ErrApprovalTimedOut)
}

func ApprovalReason(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), ErrApprovalTimedOut) {
		return "approval timed out"
	}
	return "cancelled"
}
```

- [ ] 跑测试至绿;`gofmt -l .`、`go vet ./...`、`go test ./...`。
- [ ] Commit: `git add internal/engine/gate.go internal/engine/gate_test.go && git commit -m "engine: three-mode permission gate with approval timeout"`

---

## Task 2:engine 共享工具(cliproc.go,Windows 杀树)

**Files:** `internal/engine/cliproc.go`、`cliproc_windows.go`、`cliproc_unix.go`(新)、`cliproc_test.go`(新)

- [ ] **写失败测试** `cliproc_test.go`(移植 `REF/internal/harness/cli/output_test.go` 与 `redact_test.go` 的核心断言):

```go
package engine

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestTrimOutputHeadTail(t *testing.T) {
	var head, tail strings.Builder
	for i := 0; i < 100; i++ {
		head.WriteString("head line\n")
		tail.WriteString("tail line\n")
	}
	s := head.String() + "THE ERROR\n" + tail.String()
	got := TrimOutput(s, 600)
	if !strings.Contains(got, "THE ERROR") || !strings.Contains(got, "truncated") {
		t.Fatalf("TrimOutput lost the tail that carries the error: %q", got[:80])
	}
}

func TestTrimOutputCRLF(t *testing.T) {
	if got := TrimOutput("done\r\n", 64); got != "done" {
		t.Fatalf("CRLF not trimmed: %q", got)
	}
}

func TestRedactSecrets(t *testing.T) {
	cases := []string{
		`curl -H "Authorization: Bearer sk-abcdef1234567890" host`,
		`export API_KEY=supersecretvalue1`,
		`token: ghp_0123456789abcdefghij`,
	}
	for _, s := range cases {
		got := Redact(s)
		if got == s || strings.Contains(got, "sk-abcdef") || strings.Contains(got, "supersecret") || strings.Contains(got, "ghp_01234") {
			t.Fatalf("Redact leaked: %q", got)
		}
	}
	if got := Redact("plain path /usr/local no secrets"); got != "plain path /usr/local no secrets" {
		t.Fatalf("Redact over-eager: %q", got)
	}
}

func TestCompactedNotice(t *testing.T) {
	if s := CompactedNotice("claude", 25900, 5253); !strings.Contains(s, "25.9k") || !strings.Contains(s, "5.3k") {
		t.Fatalf("CompactedNotice: %q", s)
	}
}

// TestReapKillsLingerer: a CLI that announced the result but ignores
// stdin-close must be gone within the grace (spec §4.1, "result 后常驻").
func TestReapKillsLingerer(t *testing.T) {
	old := ReapGrace
	ReapGrace = 150 * time.Millisecond
	defer func() { ReapGrace = old }()
	cmd := lingerCommand(3 * time.Second) // platform helper below
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, killed := Reap(cmd, nil)
	if !killed {
		t.Fatal("the lingering CLI was not killed")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Reap took %v — the grace did not bound it", d)
	}
}
```

`lingerCommand` 放在 `cliproc_test.go` 里按平台给:`windows` → `exec.Command("cmd", "/C", "ping -n 4 127.0.0.1 > nul")`;`!windows` → `exec.Command("sh", "-c", "sleep 3")`(同一文件里 `runtime.GOOS` 分支即可,不必 build tag)。

- [ ] 跑测试确认失败(函数不存在)。
- [ ] **实现**:
  - `cliproc.go`:从 `REF/internal/harness/cli/cli.go` **逐行移植** `Trunc`、`ShortArgs`、`TrimOutput`(连同 `runeFloor`/`isContinuation`)、`Redact`(连同 `secretPatterns`)、`CompactedNotice`、`CompactTokens`、`ShortSession`;包名改 `engine`,删 `execCmd` 别名依赖。
  - `Reap` 从 `REF/internal/harness/cli/reap.go` 移植,但 kill 路径抽成平台函数:

```go
// cliproc.go(节选)
var ReapGrace = 3 * time.Second

func Reap(cmd *exec.Cmd, stdin io.Closer) (waitErr error, killed bool) {
	if stdin != nil {
		_ = stdin.Close()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	settled := func() (error, bool) {
		select {
		case err := <-done:
			return err, false
		case <-time.After(ReapGrace):
			return nil, true
		}
	}
	err, overdue := settled()
	if !overdue {
		return err, false
	}
	terminateTree(cmd) // platform: group SIGTERM / taskkill /T /F
	if err, overdue := settled(); !overdue {
		return err, true
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	err, _ = settled()
	return err, true
}
```

  - `cliproc_unix.go`(`//go:build !windows`):`ProcGroupAttr(cmd *exec.Cmd)` 调 `setProcGroup`(照抄 `REF/internal/harness/cli/proc_unix.go`,若其用 syscall.Setpgid 则原样);`terminateTree(cmd)` = `cmd.Cancel != nil → cmd.Cancel()` 否则 `cmd.Process.Kill()`。
  - `cliproc_windows.go`(`//go:build windows`):`ProcGroupAttr` no-op;`terminateTree` = `exec.Command("taskkill", "/PID", itoa(pid), "/T", "/F").Run()`,失败回退 `cmd.Process.Kill()`(claude.cmd 是批处理 wrapper,不杀树会留 node 残尸——本仓对参考的明确改进)。
- [ ] 测试至绿(Windows 上 `TestReapKillsLingerer` 必须真跑);gofmt/vet/test。
- [ ] Commit: `git add internal/engine/cliproc*.go internal/engine/cliproc_test.go && git commit -m "engine: port CLI helpers; tree-kill reaping on Windows"`

---

## Task 3:engine 沙箱层(sandbox*.go)

**Files:** `internal/engine/sandbox.go`、`sandbox_darwin.go`、`sandbox_linux.go`、`sandbox_windows.go`、`sandbox_test.go`(新)

- [ ] **写失败测试** `sandbox_test.go`:

```go
package engine

import "testing"

// On every platform the interface must answer Available() honestly:
// windows says no (the gate then turns agent-mode bash into an ask).
func TestSandboxAvailabilityHonest(t *testing.T) {
	sb := NewSandbox()
	if sb.Available() && !sandboxReallyWorksHere() {
		t.Fatal("sandbox claims available on a platform without a backend")
	}
}

func sandboxReallyWorksHere() bool {
	switch runtime.GOOS {
	case "darwin":
		return true // sandbox-exec ships with the OS
	case "linux":
		_, err := exec.LookPath("bwrap")
		return err == nil
	}
	return false
}

// The boundary's fields ride the interface unchanged (spec §3).
func TestBoundaryShape(t *testing.T) {
	b := Boundary{Workdir: "/w", Scratch: "/s", Network: "deny"}
	if b.Network != "deny" || b.Workdir == "" || b.Scratch == "" {
		t.Fatal("boundary shape")
	}
}
```

- [ ] **实现**:
  - `sandbox.go`(可移植):

```go
package engine

import (
	"context"
	"os/exec"
)

// Boundary is the execution boundary for one sandboxed command (spec §3):
// workdir read-write, a fresh scratch read-write (the child's HOME and
// TMPDIR), network denied.
type Boundary struct {
	Workdir string
	Scratch string
	Network string // "deny"
}

// Sandbox wraps one command invocation in a boundary. A platform without
// a backend reports Available() == false — the gate then downgrades
// agent-mode bash to an approval card (spec §2.1), never silently
// unsandboxed execution.
type Sandbox interface {
	Available() bool
	Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error)
}

// NewSandbox returns this platform's provider.
// (per-platform file implements it, like REF/internal/providers/sandbox)
```

  - `sandbox_darwin.go`(`//go:build darwin`):移植 `REF/internal/providers/sandbox/darwin.go` 全量(`New()` 改名 `NewSandbox()`;`sandbox struct` 实现 `Available() bool` = LookPath("sandbox-exec") 成功;`Command` 原样,import 路径 `mygo-agent/internal/harness` → 本包类型)。`canonical`、`seatbeltProfile` 照抄。
  - `sandbox_linux.go`(`//go:build linux`):移植 `REF/internal/providers/sandbox/linux.go` 同法;`Available()` = LookPath("bwrap") 成功。
  - `sandbox_windows.go`(`//go:build windows`):

```go
//go:build windows

package engine

import (
	"context"
	"fmt"
	"os/exec"
)

type windowsSandbox struct{}

func NewSandbox() Sandbox { return windowsSandbox{} }

func (windowsSandbox) Available() bool { return false }

// Command is never reached: the gate reads Available() and downgrades
// agent-mode bash to an approval card. If something calls it anyway, the
// honesty rule answers with the remedy, not silent unsandboxed execution.
func (windowsSandbox) Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error) {
	return nil, nil, fmt.Errorf("no OS sandbox backend on Windows; agent mode asks per command — switch to Full Access to run unsandboxed")
}
```

- [ ] 测试至绿。**交叉编译验证**(本机是 Windows,darwin/linux 测试跑不了,编译必须过):
  `GOOS=darwin go build ./... && GOOS=linux go build ./... && GOOS=windows go build ./...`
- [ ] gofmt/vet/test;Commit: `git add internal/engine/sandbox*.go internal/engine/sandbox_test.go && git commit -m "engine: sandbox boundary + platform backends (seatbelt/bwrap/windows-unavailable)"`

---

## Task 4:builtin 工具过门(tools.go)

**Files:** `internal/engine/tools.go`(改)、`internal/engine/tools_test.go`(改)

**要点:** `Tools` 签名改为 `Tools(workdir string, g Gate)`;`Gate` 携带模式/沙箱/审批回调。`loop.go` 此任务先原地适配(`Tools(cfg.Workdir, Gate{})` 会变 Read-Only——所以 Task 8 前先让 loop.go 传一个临时等价 `Gate{Mode: ModeFull}`,保持现有测试绿;Task 8 再接真值)。

- [ ] **写失败测试**(加进 `tools_test.go`;用 `TestGate` 前缀):

```go
// gateTools builds gated tools with a recording approval callback.
func gateTools(t *testing.T, mode Mode, decide ApprovalFunc) ([]core.AgentTool, *[]string) {
	var asked []string
	if decide == nil {
		decide = func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
			asked = append(asked, req.Tool)
			return ApprovalDecision{Approved: false, Reason: "the user denied this call"}
		}
	}
	dir := t.TempDir()
	sb := NewSandbox()
	return Tools(dir, Gate{Mode: mode, Sandbox: sb, OnApproval: decide, ApprovalTimeout: 50 * time.Millisecond}), &asked
}

func call(t *testing.T, tools []core.AgentTool, name, args string) (string, bool) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name != name {
			continue
		}
		res, err := tool.Execute(context.Background(), "call1", json.RawMessage(args), nil)
		if err != nil {
			t.Fatal(err)
		}
		text := ""
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(core.TextContent); ok {
				text = tc.Text
			}
		}
		return text, res.IsError
	}
	t.Fatalf("tool %q not found", name)
	return "", false
}

func TestGateReadOnlyDeniesBashAndWrite(t *testing.T) {
	tools, asked := gateTools(t, ModeReadOnly, nil)
	if text, isErr := call(t, tools, "bash", `{"command":"echo hi"}`); !isErr || !strings.Contains(text, "read-only") {
		t.Fatalf("bash under read-only: err=%v text=%q", isErr, text)
	}
	if _, isErr := call(t, tools, "write_file", `{"path":"a.txt","content":"x"}`); !isErr {
		t.Fatal("write_file under read-only was allowed")
	}
	if _, isErr := call(t, tools, "read_file", writeTemp(t, "hello")); !isErr {
		// read_file stays allowed in read-only (that's the point of the mode)
		_ = isErr
	}
	if len(*asked) != 0 {
		t.Fatalf("read-only must deny without asking: %v", *asked)
	}
}

func TestGateWindowsAskApprovesBash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows fallback path")
	}
	tools, asked := gateTools(t, ModeAgent, func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		return ApprovalDecision{Approved: true}
	})
	if text, isErr := call(t, tools, "bash", `{"command":"echo gated"}`); isErr || !strings.Contains(text, "gated") {
		t.Fatalf("approved bash did not run: err=%v text=%q", isErr, text)
	}
	if len(*asked) != 1 || (*asked)[0] != "bash" {
		t.Fatalf("asked = %v", *asked)
	}
}

func TestGateWindowsAskDeniedBlocksBash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows fallback path")
	}
	tools, _ := gateTools(t, ModeAgent, nil)
	if text, isErr := call(t, tools, "bash", `{"command":"echo nope"}`); !isErr || !strings.Contains(text, "denied") {
		t.Fatalf("denied bash ran anyway: err=%v text=%q", isErr, text)
	}
}

func TestGateNilApprovalDeniesAsk(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ask path only on windows")
	}
	tools, _ := gateTools(t, ModeAgent, ApprovalFunc(nil))
	if _, isErr := call(t, tools, "bash", `{"command":"echo x"}`); !isErr {
		t.Fatal("nil OnApproval must deny an ask (headless fail-closed)")
	}
}

func TestGateApprovalTimeoutDenies(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ask path only on windows")
	}
	tools, _ := gateTools(t, ModeAgent, func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		<-ctx.Done() // never answers
		return ApprovalDecision{Approved: true}
	})
	if text, isErr := call(t, tools, "bash", `{"command":"echo x"}`); !isErr || !strings.Contains(text, "approval timed out") {
		t.Fatalf("timeout must deny: err=%v text=%q", isErr, text)
	}
}

func TestGateWriteConfinedToWorkdir(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.txt")
	tools, _ := gateTools(t, ModeReadOnly, nil) // mode set below manually
	tools = Tools(dir, Gate{Mode: ModeAgent, Sandbox: NewSandbox()})
	if _, isErr := call(t, tools, "write_file", fmt.Sprintf(`{"path":%q,"content":"x"}`, outside)); !isErr {
		t.Fatal("agent mode allowed a write outside the workspace")
	}
	if _, isErr := call(t, tools, "write_file", `{"path":"inside.txt","content":"x"}`); isErr {
		t.Fatal("agent mode refused a workspace write")
	}
}

func TestGateCredentialReadsDenied(t *testing.T) {
	if !credentialDenied(filepath.Join(homeOf(t), ".ssh", "id_rsa"), homeOf(t)) {
		t.Fatal("credential path not denied")
	}
	if credentialDenied(filepath.Join(homeOf(t), "notes.txt"), homeOf(t)) {
		t.Fatal("ordinary path denied")
	}
}
```

(测试辅助 `writeTemp`/`homeOf` 就地实现;`homeOf` = `os.UserHomeDir` 失败回退 t.TempDir。)

- [ ] 跑新测试至失败。
- [ ] **实现** `tools.go` 改造:

```go
// Gate carries the permission context into the toolset (spec §2).
type Gate struct {
	Mode            Mode
	Sandbox         Sandbox // nil → treated as unavailable
	OnApproval      ApprovalFunc
	ApprovalTimeout time.Duration
}

func Tools(workdir string, g Gate) []core.AgentTool {
	if g.Mode == ModeFull {
		// full access: no gate, the original behavior
		return ungatedTools(workdir)
	}
	sb := g.Sandbox
	sandboxOK := sb != nil && sb.Available()
	return []core.AgentTool{
		{Name: "bash", …Parameters 照旧…,
			Execute: func(ctx context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return gatedBash(ctx, workdir, params, g, sb, sandboxOK)
			}},
		{Name: "read_file", …, Execute: … gatedRead(workdir, params, g)},
		{Name: "write_file", …, Execute: … gatedWrite(workdir, params, g)},
	}
}
```

  - `ungatedTools` = 现有三工具原样(重命名现 `Tools` 函数体)。
  - `gatedBash`:`gateResolve("bash", g.Mode, sandboxOK)` →
    - deny:`textResult(denialText("bash", g.Mode), true)`
    - ask:`ApprovalContext(ctx, g.ApprovalTimeout)` → `g.OnApproval`(nil → 直接拒绝,reason "no approval handler is configured")→ 迟到/超时决定不算(照参考:`actx.Err() != nil` 后覆盖 decision);批准 → 跑 `toolBash` 原逻辑;拒绝 → `textResult("permission denied (bash): "+reason, true)`
    - allow:Agent 且 sandboxOK → `runSandboxed`;否则(Full)→ `toolBash` 原逻辑。
  - `runSandboxed(ctx, sb, workdir, command)`:`os.MkdirTemp("", "crux-sbx-*")` + defer RemoveAll;`os.MkdirAll(scratch/tmp)`;`sb.Command(ctx, Boundary{…}, shell, flag, command)`(shell/flag 来自现有 `ShellCommand()`);`cmd.Dir = workdir`;`cmd.Env = append(os.Environ(), "HOME="+scratch, "TMPDIR="+scratch+"/tmp")`;`out, err := cmd.CombinedOutput()`;被 ctx 切断时结果标注 "killed — outcome unknown"。
  - `gatedWrite`:`gateResolve` deny → 拒绝;allow(Agent)→ `resolveToolPath` 后 **confine**:`abs` 不在 workdir 之下 → `textResult("permission denied (write_file): the path is outside the workspace", true)`;通过 → `toolWrite` 原逻辑。Full → 原逻辑。
  - `gatedRead`:Agent/ReadOnly 下 `credentialDenied(abs, home)` → 拒绝(文案点名凭证目录);其余原逻辑。Full 不检查。
  - `credentialDenied(path, home string) bool`:前缀匹配 `home/.ssh`、`.aws`、`.gnupg`、`.kube`、`.docker`(经 `filepath.Clean` + 尾分隔符归一)。
  - `loop.go` 临时改一行:`Tools(cfg.Workdir, Gate{Mode: ModeFull})`,保持现网行为与既有测试绿(Task 8 换真值)。
- [ ] 测试至绿(Windows 上 ask 路径全真跑);gofmt/vet/test。
- [ ] Commit: `git add internal/engine/tools.go internal/engine/tools_test.go internal/engine/loop.go && git commit -m "engine: gate the builtin tools — modes, workspace confine, credential deny, sandboxed bash"`

---

## Task 5:claude 行级事件处理(claude_events.go)

**Files:** `internal/engine/claude_events.go`(新)、`internal/engine/claude_events_test.go`(新)

**蓝本:** `REF/internal/harness/claude/claude.go` 行 181–562 的 `run`/`controlRequest`/`handle`/`toolUse`/`toolResult`/`contentText`/`block`,适配到 Crux 的回调面。**行级测试先行**(不 spawn 进程,Windows 也跑——参考的 claude_test.go 就是这个模式)。

- [ ] **写失败测试** `claude_events_test.go`(移植 `REF claude_test.go` 的纯行级用例,断言改成 Crux 回调):

```go
package engine

// collect builds a run whose events land in plain callbacks.
type collected struct {
	texts   []string      // every OnText payload (per-turn accumulated)
	tools   []string      // "name args" per OnToolStart
	ends    map[string]string
	sess    []string
	notes   []string
	approve ApprovalFunc
}

func newCollected(approve ApprovalFunc) (*collected, *claudeRun) {
	cc := &collected{ends: map[string]string{}, approve: approve}
	cb := Callbacks{
		OnText:      func(t string) { cc.texts = append(cc.texts, t) },
		OnToolStart: func(id, name, args string) { cc.tools = append(cc.tools, name+" "+args) },
		OnToolEnd:   func(id, res string, isErr bool, d time.Duration) { cc.ends[id] = res },
		OnSession:   func(id string) { cc.sess = append(cc.sess, id) },
		OnNote:      func(t string) { cc.notes = append(cc.notes, t) },
	}
	r := &claudeRun{cfg: ClaudeConfig{Mode: ModeAgent}, cb: cb, wrote: []map[string]any{}}
	return cc, r
}

func TestClaudeSessionAndText(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"system","subtype":"init","session_id":"sess-abc123"}`)
	r.handle(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello "}]}}`)
	if len(cc.sess) != 1 || cc.sess[0] != "sess-abc123" {
		t.Fatalf("session: %v", cc.sess)
	}
	if len(cc.texts) != 1 || cc.texts[0] != "Hello " {
		t.Fatalf("text: %v", cc.texts)
	}
}

// Partial deltas then the complete assistant message: only the un-streamed
// suffix may go out (the CLI warns partials may duplicate the message).
func TestClaudePartialDedup(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}}`)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}}`)
	r.handle(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`)
	joined := strings.Join(cc.texts, "")
	if joined != "Hello world" {
		t.Fatalf("dedup: %q", joined)
	}
}

// A tool_use resets the per-turn accumulation (engine Callbacks semantics:
// the text after tool results is a NEW segment).
func TestClaudeToolUseResetsText(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"interim"}}}`)
	r.handle(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}`)
	r.handle(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"file"}]}}`)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"final"}}}`)
	if got := cc.texts; len(got) != 2 || got[0] != "interim" || got[1] != "final" {
		t.Fatalf("segments: %v", got)
	}
	if len(cc.tools) != 1 || !strings.Contains(cc.tools[0], "Bash") {
		t.Fatalf("tools: %v", cc.tools)
	}
	if cc.ends["tu1"] != "file" {
		t.Fatalf("ends: %v", cc.ends)
	}
}

func TestClaudeResultNote(t *testing.T) {
	cc, r := newCollected(nil)
	r.cfg.SessionID = "sess-abc123"
	r.handle(`{"type":"result","subtype":"success","is_error":false,"duration_ms":12000,"total_cost_usd":0.0042,"result":"done"}`)
	if len(cc.notes) != 1 || !strings.Contains(cc.notes[0], "$0.0042") || !strings.Contains(cc.notes[0], "sess-abc") {
		t.Fatalf("note: %v", cc.notes)
	}
	if !r.sawResult {
		t.Fatal("result line did not set sawResult — the run would never settle")
	}
}

func TestClaudeResultNoteTokens(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"result","subtype":"success","is_error":false,"duration_ms":2345,"total_cost_usd":0.0042,"usage":{"input_tokens":110,"output_tokens":17}}`)
	if len(cc.notes) != 1 || !strings.Contains(cc.notes[0], "127 tokens") {
		t.Fatalf("note: %v", cc.notes)
	}
}

// compact_metadata is snake_case; reading it as compactMetadata decodes to
// nothing and the note silently loses its counts.
func TestClaudeCompactNote(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"auto","pre_tokens":25900,"post_tokens":5253}}`)
	if len(cc.notes) != 1 || !strings.Contains(cc.notes[0], "25.9k") {
		t.Fatalf("compact: %v", cc.notes)
	}
}

// Read-only answers can_use_tool itself with a mode-named denial — no
// human is asked.
func TestClaudeReadOnlyDeniesTool(t *testing.T) {
	cc, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		t.Fatal("read-only must not ask")
		return ApprovalDecision{}
	})
	r.cfg.Mode = ModeReadOnly
	r.handle(`{"type":"control_request","request_id":"rq1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	if len(r.wrote) != 1 {
		t.Fatalf("wrote: %v", r.wrote)
	}
	resp := r.wrote[0]
	if resp["subtype"] != "success" || resp["request_id"] != "rq1" {
		t.Fatalf("resp: %v", resp)
	}
	inner := resp["response"].(map[string]any)
	if inner["behavior"] != "deny" || !strings.Contains(inner["message"].(string), "read-only") {
		t.Fatalf("inner: %v", inner)
	}
}

// Agent mode forwards to the approval callback; an approved call answers
// allow + updatedInput echo.
func TestClaudeApprovalRoundTrip(t *testing.T) {
	cc, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		if req.Tool != "Bash" || !strings.Contains(req.Summary, "ls") {
			t.Fatalf("req: %+v", req)
		}
		return ApprovalDecision{Approved: true}
	})
	r.handle(`{"type":"control_request","request_id":"rq2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "allow" || inner["updatedInput"] == nil {
		t.Fatalf("inner: %v", inner)
	}
}

// A denial carries the reason back to the model.
func TestClaudeApprovalDenied(t *testing.T) {
	cc, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		return ApprovalDecision{Reason: "the user denied this call"}
	})
	r.handle(`{"type":"control_request","request_id":"rq3","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"a.go"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "deny" || inner["message"] != "the user denied this call" {
		t.Fatalf("inner: %v", inner)
	}
}

// A decision after the deadline is not a decision.
func TestClaudeApprovalTimeoutIsDenial(t *testing.T) {
	cc, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		<-ctx.Done()
		return ApprovalDecision{Approved: true} // too late
	})
	r.cfg.ApprovalTimeout = 20 * time.Millisecond
	r.handle(`{"type":"control_request","request_id":"rq4","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "deny" || !strings.Contains(inner["message"].(string), "timed out") {
		t.Fatalf("inner: %v", inner)
	}
}

// Any other server-initiated request gets an error reply — leaving one
// unanswered stalls the CLI forever.
func TestClaudeUnsupportedControlAnswered(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"control_request","request_id":"rq5","request":{"subtype":"set_chat_model","model":"x"}}`)
	if len(r.wrote) != 1 || r.wrote[0]["subtype"] != "error" {
		t.Fatalf("wrote: %v", r.wrote)
	}
}

// Nil OnApproval: ask settles as a denial (headless fail-closed).
func TestClaudeNilApproval(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"control_request","request_id":"rq6","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "deny" {
		t.Fatalf("inner: %v", inner)
	}
}
```

- [ ] 跑至失败(类型不存在)。
- [ ] **实现** `claude_events.go`(结构对照参考行 181–562):

```go
package engine

// claudeRun carries one Claude Code run's state between lines. The wire
// contract is REF/internal/harness/claude/claude.go; events land in the
// engine's Callbacks (Crux semantics: OnText carries the turn's own
// accumulated buffer; a tool_use starts a new segment).
type claudeRun struct {
	cfg      ClaudeConfig
	cb       Callbacks
	stdin    io.WriteCloser // nil in unit tests; writeControl captures then
	mode     Mode
	sawEvent bool
	sawResult bool
	// streamed is the text the partial deltas already emitted, so the
	// complete assistant message only contributes its un-streamed suffix.
	streamed strings.Builder
	// text is the current segment's accumulated text (engine semantics).
	text strings.Builder
	// wrote records control responses when there is no CLI (unit tests).
	wrote []map[string]any
}
```

  - `handle(line string)`:`""` 早退;`sawEvent = true`;顶层解码结构照抄参考行 309–334(`compact_metadata` snake_case 注释保留);`control_request` → `controlRequest`;`result` → `sawResult = true` + note(行 435–451 的文本组装,`session` 用 `cfg.SessionID` 的 `ShortSession`);`system` init/compact_boundary;`stream_event` text_delta → `text.WriteString` + `cb.OnText(text.String())`;`assistant` 文本块 dedup(参考行 398–418 逐行照搬,但发的是**累计后**的 `text.String()`,dedup 逻辑作用于 `streamed` 与块的 whole 文本,再把差量并进 `text`)——注意:参考发的是 delta,Crux 发累计,所以实现为:`suffix` 求法同参考;`text.WriteString(suffix)`;`cb.OnText(text.String())`。`tool_use` → `text.Reset()` + `cb.OnToolStart(blk.ID, blk.Name, string(blk.RawInput))`(不做 toolUse 的卡片分类——那是 UI 层按名字解析 args 的事);`user` tool_result → `cb.OnToolEnd(id, contentText(raw), isErr, 0)`。
  - `controlRequest(line string)`:参考行 210–281 照搬,`harness.ApprovalSummary(call)` 换成本包 `approvalSummary(tool, rawInput)`(见下);`harness.ApprovalContext` → 本包;错误回执文案 "unsupported by crux"。
  - `approvalSummary(tool string, raw json.RawMessage) string`:简化移植 `REF/internal/harness/permissions.go` 的 `ApprovalSummary`:`bash`/`Bash` → `"$ " + Trunc(Redact(command), 160)`;`file_path`/`path` 有值 → `name + " " + Trunc(Redact(path), 160)`;其余 → `Trunc(name+" "+ShortArgs(raw), 160)`。
  - `writeControl` / `contentText` / `block`:参考行 285–299、528–562 原样。
  - `ClaudeConfig` 定义放 `claude.go`(Task 6);本任务先在 `claude_events.go` 顶部给出**完整**结构以便编译:

```go
type ClaudeConfig struct {
	Bin             string
	Model           string // alias: sonnet/opus/…; empty = CLI default
	SystemPrompt    string
	Mode            Mode
	SessionID       string // --resume
	Workdir         string
	ApprovalTimeout time.Duration
	OnApproval      ApprovalFunc
}
```

- [ ] 行级测试全绿(Windows 真跑);gofmt/vet/test。
- [ ] Commit: `git add internal/engine/claude_events.go internal/engine/claude_events_test.go && git commit -m "engine: claude stream-json event handling with approval control requests"`

---

## Task 6:RunClaude(spawn/收割/终止语义)

**Files:** `internal/engine/claude.go`(新)

**蓝本:** `REF/internal/harness/claude/claude.go` 行 40–179。

- [ ] **实现** `RunClaude`(先实现,Task 7 用进程级测试钉它):

```go
package engine

// RunClaude drives one turn of the Claude Code CLI (spec §4): spawns the
// CLI in stream-json mode, writes the prompt as one user message, folds
// events into cb, answers control requests, and settles the turn on the
// result line. One fresh CLI per turn; resume rides --resume.
func RunClaude(ctx context.Context, cfg ClaudeConfig, prompt string, cb Callbacks) error {
	if strings.TrimSpace(cfg.Bin) == "" {
		return fmt.Errorf("claude: no claude CLI found on PATH — install Claude Code or check the settings")
	}
	// Mode mapping (spec §4.1): read-only/agent stay in default mode and
	// differ in how the host answers can_use_tool; full bypasses asking.
	perm := "default"
	if cfg.Mode == ModeFull {
		perm = "bypassPermissions"
	}
	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		"--input-format", "stream-json", "--include-partial-messages",
		"--permission-mode", perm}
	if m := strings.TrimSpace(cfg.Model); m != "" {
		args = append(args, "--model", m)
	}
	if cfg.SessionID != "" {
		args = append(args, "--resume", cfg.SessionID)
	}
	if cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.SystemPrompt)
	}
	cmd := exec.CommandContext(ctx, cfg.Bin, args...)
	cmd.Dir = cfg.Workdir
	ProcGroupAttr(cmd)
	// stdin/stdout pipes + stderr builder 照参考行 89–101
	// prompt 一行写 stdin:参考行 103–114 的 JSON 形状原样
	// scanner 循环:参考行 122–133 原样(sawResult 后 break——CLI 常驻等下一条输入)
	// waitErr, killed := Reap(cmd, stdin)
	// 终止语义:参考行 136–160 原样翻译:
	//   ctx 取消且 sawEvent → nil(已到内容成立)
	//   否则 ctx 取消 → fmt.Errorf("claude: stopped before it answered — %s", Trunc(claudeReason(...), 400))
	//   sawEvent && (waitErr == nil || killed) → nil
	//   stderr 尾部非空 → 错误
	// claudeReason:参考行 163–179 原样
}
```

  - `run` 结构与 handle 的挂接:`r := &claudeRun{cfg: cfg, ctx: ctx, stdin: stdin, mode: cfg.Mode, cb: cb, wrote: …}`——`claudeRun` 需补 `ctx context.Context` 字段(Task 5 已建其余字段)。
  - `cb` 为 nil 字段时全部 nil-safe(参考的 `send` 模式:包一层 `if r.cb.OnText != nil`)。
- [ ] `go build ./...` 编译过;既有测试不回归。
- [ ] Commit: `git add internal/engine/claude.go && git commit -m "engine: RunClaude — spawn, prompt, reap, termination semantics"`

---

## Task 7:进程级 fake CLI 测试(双平台)

**Files:** `internal/engine/claude_proc_test.go`(新)

**要点:** 参考 claude_test.go 的进程级用例全带 `//go:build !windows`;Crux 主开发机是 Windows,**必须两平台都真跑**:fake CLI 用 `.cmd`(Windows)/`.sh`(Unix)双版本。

- [ ] **写测试**(先写,预期失败/超时):

```go
package engine

// fakeClaudeScript writes a platform fake CLI that echoes the given body
// lines, optionally reading stdin lines into files. It returns the script
// path. Windows: a .cmd batch (echo handles the JSON quotes fine);
// Unix: a .sh with +x.
func fakeClaudeScript(t *testing.T, body []string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "fake-claude.cmd")
		var b strings.Builder
		b.WriteString("@echo off\r\n")
		for _, line := range body {
			b.WriteString(line + "\r\n")
		}
		os.WriteFile(path, []byte(b.String()), 0o755)
		return path
	}
	path := filepath.Join(dir, "fake-claude.sh")
	body := "#!/bin/sh\n" + strings.Join(body, "\n") + "\n"
	os.WriteFile(path, []byte(body), 0o755)
	return path
}
```

  (实际实现时注意:`body` 里的 Windows 特有行(`set /p`)与 Unix 特有行(`read`)由各用例自己给——helper 接收的就是平台相关脚本体;用例内 `if runtime.GOOS == "windows" { body = cmdLines } else { body = shLines }`。)
- [ ] **用例**(每个都双平台真跑):
  1. `TestClaudeSpawnFlags`:fake CLI 把参数落盘(cmd: `echo %* > args.txt`;sh: `printf '%s\n' "$@" > args.txt`),发一行 result;断言 `--include-partial-messages`、`--permission-mode default`(ModeAgent)、`--resume sess-1`(有 SessionID)、`--append-system-prompt`(有 SystemPrompt)、`--model sonnet` 各自在;ModeFull → `bypassPermissions`。
  2. `TestClaudePromptOnStdin`:fake CLI 读第一行 stdin 落盘(cmd: `set /p p=` 后 `echo %p% > prompt.txt`;sh: `read p; echo "$p" > prompt.txt`),发 result;断言落盘行是 `{"type":"user",…,"text":"hello"…}` 的 JSON(可用 `json.Unmarshal` 到 map 校验 text)。
  3. `TestClaudeApprovalWireRoundTrip`:fake CLI 发 init → assistant tool_use → control_request(can_use_tool) → **读下一行 stdin**(即宿主的 control_response)落盘 → result;宿主 `OnApproval` 批准;断言:run 无错、`OnSession` 收到 id、落盘的响应行 `json.Unmarshal` 后 `response.behavior == "allow"`。拒绝变体:`behavior == "deny"`。
  4. `TestClaudeReapsResidentCLI`:fake CLI 发 result 后 `ping -n 10 127.0.0.1 > nul`(sh: `sleep 10`);`ReapGrace` 缩到 150ms(导出变量,defer 还原);断言 Run 在 ~1s 内返回且无错(sawEvent 成立 → killed 也算干净)。
  5. `TestClaudeStopBeforeAnswer`:fake CLI `ping -n 10 …` 不输出;ctx 200ms 取消;断言错误含 `stopped before it answered`。
  6. `TestClaudeStderrTailIsError`:fake CLI 直接往 stderr 写 `boom` 退出(`1>&2` 用 cmd: `echo boom 1>&2 & exit /b 1`;sh: `echo boom >&2; exit 1`);断言错误含 `boom`。
- [ ] 逐个跑至绿(Windows 全真跑;注意 cmd 的 `%` 字面量与 JSON 无冲突,`&`/`|`/`<`/`>` 不出现在我们的 JSON 行里)。
- [ ] gofmt/vet/test;Commit: `git add internal/engine/claude_proc_test.go && git commit -m "engine: process-level claude tests via .cmd/.sh fake CLIs (both platforms)"`

---

## Task 8:engine.Run 派发(loop.go)

**Files:** `internal/engine/loop.go`(改)、`internal/engine/loop_test.go`(改)

- [ ] **写失败测试**(loop_test.go 增):

```go
// Backend "claude" dispatches to RunClaude: the last user message becomes
// the prompt, claudeID rides the config, callbacks pass through.
func TestRunDispatchesClaude(t *testing.T) {
	bin := fakeClaudeScript(t, echoResultLines) // helper from claude_proc_test.go
	var sess, note string
	err := Run(context.Background(), Config{
		Backend: "claude", ClaudeBin: bin, Mode: ModeFull,
		Workdir: t.TempDir(), Model: "sonnet",
	}, []Message{{Role: RoleUser, Text: "hello", At: time.Now()}, {Role: RoleAssistant, Text: "old", At: time.Now()}},
		Callbacks{OnSession: func(id string) { sess = id }, OnNote: func(s string) { note = s }})
	if err != nil {
		t.Fatal(err)
	}
	if sess == "" || note == "" {
		t.Fatalf("session=%q note=%q", sess, note)
	}
}

// No user message to send is an error, not a silent empty turn.
func TestRunClaudeNeedsPrompt(t *testing.T) {
	err := Run(context.Background(), Config{Backend: "claude", ClaudeBin: "whatever"}, []Message{{Role: RoleAssistant, Text: "only"}}, Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("err: %v", err)
	}
}

// Built-in path still runs with the gate wired (read-only denies bash
// before any LLM call — observable through Tools, see tools_test; here we
// only pin that Run builds gated tools without panicking on a nil model
// by checking the claude branch is not taken).
func TestRunDefaultBackendIsBuiltin(t *testing.T) {
	if (&Config{}).backend() != "builtin" {
		t.Fatal("empty Backend must mean builtin")
	}
}
```

- [ ] **实现** `loop.go`:

```go
// Config carries everything the loop needs (spec §4.4): which backend,
// how to steer it, where the tools operate, and the permission context.
type Config struct {
	Provider     string
	Model        string
	APIKey       string
	BaseURL      string
	SystemPrompt string
	Thinking     string
	Workdir      string

	Backend         string        // "" | "builtin" → pi-ai; "claude" → CLI
	Mode            Mode          // three-level approval selector
	OnApproval      ApprovalFunc  // nil → ask settles as denial
	ApprovalTimeout time.Duration // 0 → DefaultApprovalTimeout
	ClaudeBin       string        // resolved claude CLI path (tests: fake)
	ClaudeSessionID string        // --resume
}

// Callbacks gains (both optional, nil-safe):
	OnSession func(sessionID string) // claude init event → persist for resume
	OnNote    func(text string)      // compact/result one-line notes
```

  - `func (c *Config) backend() string { if c.Backend == "claude" { return "claude" }; return "builtin" }`
  - `Run` 顶部派发:

```go
func Run(ctx context.Context, cfg Config, history []Message, cb Callbacks) error {
	if cfg.backend() == "claude" {
		prompt, at, ok := lastUserMessage(history)
		if !ok {
			return fmt.Errorf("claude: nothing to send — the prompt is empty")
		}
		_ = at
		return RunClaude(ctx, ClaudeConfig{
			Bin: cfg.ClaudeBin, Model: cfg.Model, SystemPrompt: cfg.SystemPrompt,
			Mode: cfg.Mode, SessionID: cfg.ClaudeSessionID, Workdir: cfg.Workdir,
			ApprovalTimeout: cfg.ApprovalTimeout, OnApproval: cfg.OnApproval,
		}, prompt, cb)
	}
	// builtin: pi-ai loop as today, with the gate now real:
	loopCfg := agent.AgentLoopConfig{
		…
		Tools: Tools(cfg.Workdir, Gate{
			Mode: cfg.Mode, Sandbox: NewSandbox(),
			OnApproval: cfg.OnApproval, ApprovalTimeout: cfg.ApprovalTimeout,
		}),
		…
	}
	…
}
```

  - `lastUserMessage`:`for i := len-1; …` 找最后一个 RoleUser,返回 (Text, At, ok)。
- [ ] 测试至绿(含既有 loop_test 不回归);gofmt/vet/test。
- [ ] Commit: `git add internal/engine/loop.go internal/engine/loop_test.go && git commit -m "engine: dispatch Run by backend; builtin loop runs gated tools"`

---

## Task 9:store 字段扩展与迁移

**Files:** `internal/store/settings.go`、`internal/store/sessions.go`(改)、`internal/store/store_test.go`(增)

- [ ] **写失败测试**:
  - `TestSettingsBackendModeRoundTrip`:Save→Load 回读 `Backend:"claude"`、`Mode:"read-only"`。
  - `TestSettingsDefaults`:旧 JSON(无 backend/mode 字段)Load → 空串(归一在 app 层),`Transcript` 旧文件 Load → `ClaudeID == ""`。
  - `TestTranscriptClaudeIDRoundTrip`。
- [ ] **实现**:
  - `LLMConfig` 增:
    `Backend string \`json:"backend,omitempty"\``、`Mode string \`json:"mode,omitempty"\``
  - `Transcript` 增:`ClaudeID string \`json:"claudeID,omitempty"\``
- [ ] 测试至绿;gofmt/vet/test。
- [ ] Commit: `git add internal/store && git commit -m "store: backend/mode on settings, claudeID on transcripts"`

---

## Task 10:app 设置表单(Agent 类型)+ backendLabel

**Files:** `internal/app/settings_claude.go`(新)、`internal/app/settings.go`(改:providersPane 顶部分段 + 分支)、`internal/app/settings_claude_test.go`(新)

- [ ] **写失败测试**(渲染级,`ui.Render`/`ui.NewTester`):

```go
func TestProvidersPaneAgentTypeSwitch(t *testing.T) {
	a := testApp()
	a.settingsOpen, a.settingsTab = true, "providers"
	view := a.view
	// 默认内置表单
	tr := ui.NewTester(view, 1280, 820)
	if !tr.HasText("API key") {
		t.Fatal("builtin form should show the API key field")
	}
	// 切到 Claude Code(点分段第二段;Segmented 是 radio group,点文本)
	…点 "Claude Code CLI"…
	tr.Frame()
	if tr.HasText("API key") {
		t.Fatal("claude form must not show the API key field")
	}
	if !tr.HasText("claude") {
		t.Fatal("claude form should name the CLI")
	}
	if a.llm.Backend != "claude" {
		t.Fatalf("backend = %q", a.llm.Backend)
	}
}
```

(具体点击方式按 Tester 能力:`tr.Find`/坐标点击;若 Segmented 文本点不中,用键盘箭头 + 焦点;再不行退化为直接调 `a.llm.Backend = "claude"` 后断言表单切换——**至少要钉住两个表单的互斥渲染**。)

- [ ] **实现**:
  - `settings.go` `providersPane` 顶部:

```go
// Agent type: which backend drives the loop (spec §5.1). Crux keeps the
// pi-ai built-in loop and the Claude Code CLI as the two kinds.
kindIdx := 0
if a.llm.Backend == "claude" {
	kindIdx = 1
}
if seg := ui.Segmented(c, &kindIdx, "Built-in LLM loop", "Claude Code CLI").Label("Agent type"); seg.Changed() {
	if kindIdx == 1 {
		a.llm.Backend = "claude"
	} else {
		a.llm.Backend = "builtin"
	}
}
…
if a.llm.Backend == "claude" {
	a.claudeProvidersPane(c) // settings_claude.go
	return
}
// …existing provider/base/key/model/reasoning/test form unchanged…
```

  - `settings_claude.go`:
    - `providerView` 增字段:`claudeProbing bool; claudePath, claudeVer, claudeErr string`(定义在 settings.go 的 providerView 里)。
    - `claudeProvidersPane`:
      - FormField "Claude CLI":检测行(`a.provView.claudePath` 非空 → `✓ path (ver)`,空 → "not found on PATH")+"Detect" 按钮 → `a.detectClaude()`。
      - FormField "Model":`input.InputGroup(&a.llm.Model, …{Placeholder: "sonnet — empty uses the CLI default", Label: "Model"})`。
      - FormField "Auth":说明文本 "The CLI uses its own sign-in (claude login); no key is stored here."
      - 系统提示仍在 Agent 分区(不动)。
    - `detectClaude()`:`redraw == nil` → 直接填 `a.claudeBin`(newApp 已 LookPath)版本留空;否则 goroutine `exec.Command(a.claudeBin, "--version").Output()` → `a.redraw` 落 `claudeVer/claudeErr`。防重入:`claudeProbing`。
    - `state.go` `newApp()` 增:`if p, err := exec.LookPath("claude"); err == nil { a.claudeBin = p }`(import os/exec;测试环境无害)。
  - `backendLabel()`(settings.go):`backend == "claude"` → `"claude · " + (Model 或 "default")`;否则原逻辑。
- [ ] 渲染测试至绿;既有 settings 测试不回归(`settings_nav_test.go` 等)。
- [ ] gofmt/vet/test;Commit: `git add internal/app/settings_claude.go internal/app/settings.go internal/app/settings_claude_test.go internal/app/state.go && git commit -m "app: agent-type selector, claude settings form, backend label"`

---

## Task 11:模式选择器 + 审批卡 + 双后端接线

**Files:** `internal/app/approval.go`(新)、`internal/app/llm.go`(改)、`internal/app/thread.go`(改)、`internal/app/state.go`(改)、`internal/app/persist.go`(改)、`internal/app/approval_test.go`(新)

- [ ] **写失败测试** `approval_test.go`:

```go
func TestApprovalCardRendersAndSettles(t *testing.T) {
	a := testApp()
	// hand-built pending approval on the current streaming row
	rowID := seedStreamingRow(a) // helper: appends rowReasoned row, returns id
	ch := make(chan ApprovalDecision, 1)
	a.pending = &approvalReq{rowID: rowID, req: engine.ApprovalRequest{Tool: "bash", Summary: "$ cargo test", Reason: "agent mode requires approval"}, ch: ch, at: time.Now()}
	tr := ui.NewTester(a.view, 1280, 820)
	if !tr.HasText("cargo test") {
		t.Fatal("the approval card does not show the ask")
	}
	…click "Allow once"…
	tr.Frame()
	if a.pending != nil {
		t.Fatal("pending not cleared after the choice")
	}
	select {
	case d := <-ch:
		if !d.Approved {
			t.Fatal("allow did not approve")
		}
	default:
		t.Fatal("decision never reached the engine")
	}
}

func TestApprovalDenyCarriesReason(t *testing.T) { …click "Deny"… assert !d.Approved && d.Reason != "" }

// The card lives INSIDE the current reply's process timeline (spec §5.3).
func TestApprovalStepInsideTimeline(t *testing.T) { …seedStreamingRow + pending… tr.HasText("bash") via the step row… }

// awaitApproval's engine-side timeout: the UI never answered.
func TestAwaitApprovalTimeoutDenies(t *testing.T) {
	a := testApp(); a.redraw = func(fn func()) { fn() } // synchronous redraw
	ctx, cancel := engine.ApprovalContext(context.Background(), 30*time.Millisecond)
	defer cancel()
	d := a.awaitApproval(ctx, "row", engine.ApprovalRequest{Tool: "bash"})
	if d.Approved || d.Reason != "approval timed out" {
		t.Fatalf("d: %+v", d)
	}
	if a.pending != nil {
		t.Fatal("timeout left the card pending")
	}
}
```

  另加 composer 测试(`thread_test.go` 或就近):

```go
func TestComposerModeSelectorPersists(t *testing.T) {
	a := testApp()
	tr := ui.NewTester(a.view, 1280, 820)
	…点 "Full Access" 段…
	tr.Frame()
	if engine.ParseMode(a.llm.Mode) != engine.ModeFull {
		t.Fatalf("mode = %q", a.llm.Mode)
	}
	// Busy 时切换被丢弃(下一帧回到当前模式)
	a.llm.Busy = true
	…点 "Read Only"…
	tr.Frame()
	if engine.ParseMode(a.llm.Mode) != engine.ModeFull {
		t.Fatal("a mid-run mode switch was applied")
	}
}
```

- [ ] **实现**:
  - `state.go`:`thread` 增 `claudeID string`;`app` 增 `pending *approvalReq`、`claudeBin string`。
  - `approval.go`:

```go
package app

// approvalReq is the one pending approval card (spec §5.3): the engine
// goroutine blocks on ch; the UI thread renders the card and settles it.
// Tools run sequentially, so one card at a time is the whole state.
type approvalReq struct {
	rowID string
	req   engine.ApprovalRequest
	ch    chan engine.ApprovalDecision
	at    time.Time
}

// awaitApproval runs on the ENGINE goroutine: it parks the card in app
// state through a.redraw and blocks until the UI settles it or ctx ends
// (timeout/cancel = denial, spec §2.2).
func (a *app) awaitApproval(ctx context.Context, rowID string, req engine.ApprovalRequest) engine.ApprovalDecision {
	ch := make(chan engine.ApprovalDecision, 1)
	ar := &approvalReq{rowID: rowID, req: req, ch: ch, at: time.Now()}
	park := func() { a.pending = ar; a.thread.list.ScrollToEnd() }
	if a.redraw == nil {
		park()
	} else {
		a.redraw(park)
	}
	select {
	case d := <-ch:
		return d
	case <-ctx.Done():
		clear := func() { if a.pending == ar { a.pending = nil } }
		if a.redraw == nil {
			clear()
		} else {
			a.redraw(clear)
		}
		return engine.ApprovalDecision{Reason: engine.ApprovalReason(ctx)}
	}
}

// settleApproval runs on the UI thread (a click in the card).
func (a *app) settleApproval(approved bool, reason string) {
	ar := a.pending
	if ar == nil {
		return
	}
	a.pending = nil
	ar.ch <- engine.ApprovalDecision{Approved: approved, Reason: reason}
}

// approvalStep renders the pending card inside the reply's process
// timeline. "Allow always" is treated as allow-once — durable authority
// only ever comes from configuration (spec §2.2, the no-always rule).
func (a *app) approvalStep(c *ui.Context, r *row) {
	ar := a.pending
	if ar == nil || ar.rowID != r.id || !r.streaming {
		return
	}
	open := true
	res := muiagent.PermissionPrompt(c, &open, muiagent.PermissionPromptOptions{
		Title:       ar.req.Tool,
		Description: ar.req.Summary,
		Scope:       "this call only",
	})
	switch res.Choice {
	case muiagent.PermissionAllowOnce, muiagent.PermissionAllowAlways:
		a.settleApproval(true, "")
	case muiagent.PermissionDeny:
		a.settleApproval(false, "the user denied this call")
	}
}
```

  - `thread.go`:
    - `processSteps` 末尾挂 `a.approvalStep(c, r)`(排在 steps 循环后、气泡内)。
    - composer 底部行(backendLabel 旁)加模式分段(每帧拷贝,Busy 丢弃):

```go
modeIdx := int(engine.ParseMode(a.llm.Mode))
if seg := ui.Segmented(c, &modeIdx, "Read Only", "Agent", "Full Access").Label("Approval mode"); seg.Changed() && !a.llm.Busy {
	a.llm.Mode = engine.Mode(modeIdx).String()
	a.saveSettings()
}
```

    - claude 工具名映射:`toolIcon`/`toolTitle`/`toolSummary` 增 `Bash`→terminal/"Ran command"、`Edit`/`Write`→pencil/"Edited file"、`Read`→file-text/"Read file";`appendTurnTool` 解析 claude 风格 args:`Bash` → `run.Command = input.command`;`Write` → `change = FileChange{Path: file_path, Old: 现文件内容(capped 读), New: content}`;`Edit` → `change = FileChange{Path: file_path, Old: old_string, New: new_string}`。
  - `llm.go`:
    - `engineConfig()` 增:`Backend: a.llm.Backend`(空→"builtin" 归一)、`Mode: engine.ParseMode(a.llm.Mode)`、`ClaudeBin: a.claudeBin`、`ClaudeSessionID: a.thread.claudeID`。
    - `startStream` 的 goroutine 里:
      - `cfg.OnApproval = func(ctx, req) engine.ApprovalDecision { return a.awaitApproval(ctx, s.curID, req) }`
      - Callbacks 增:`OnSession: func(id) { a.redraw(… a.thread.claudeID = id; a.markDirty(a.sessionID); a.persistSessions() …) }`;`OnNote: func(text) { a.redraw(… s.setRow(func(r){ r.note = text }) …) }`
      - defer 的收尾 redraw 里加 `a.pending = nil`(中止时卡必清;引擎侧 awaitApproval 也会因 ctx 取消返回,channel 无人读也无妨——带缓冲)。
    - `row` 增 `note string` 字段;`actions()` 时间旁渲染 `r.note`(muted 11px,非空时)。
  - `persist.go`:`persistDirtyTranscripts` 的 `ts := store.Transcript{Mode: storeMode(t.mode)}` 增 `ClaudeID: t.claudeID`;`loadTranscript` 回填 `t.claudeID`;`loadSettings` 归一 `cfg.Backend == "" → "builtin"`、`cfg.Mode == "" → "agent"`。
  - `state.go` row 结构体补 note;`storeMsg` 若序列化需要(不持久化 note 也行——note 是回合摘要,重启丢了无伤;**不持久化**,注释说明)。
- [ ] 测试至绿;**全量** `go test ./...` 不回归(headless send 路径 `redraw==nil` → finishStream 立即落定,审批永不触发,既有测试保持确定性)。
- [ ] gofmt/vet/test;Commit: `git add internal/app && git commit -m "app: approval cards in the timeline, mode selector, claude session wiring"`

---

## Task 12:live probe + 文档回写 + 全量验证

**Files:** `internal/engine/claude_live_test.go`(新)、`README.md`、`SPEC.md`(改)、spec 文档追加注记

- [ ] **live probe**(默认跳过):

```go
//go:build !race

func TestLiveClaudeSmoke(t *testing.T) {
	if os.Getenv("CRUX_LIVE_CLAUDE") != "1" {
		t.Skip("set CRUX_LIVE_CLAUDE=1 to run the real CLI")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("no claude CLI installed")
	}
	var got string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err = Run(ctx, Config{Backend: "claude", ClaudeBin: bin, Mode: ModeFull, Workdir: t.TempDir()},
		[]Message{{Role: RoleUser, Text: "Reply with exactly: pong (no tools)", At: time.Now()}},
		Callbacks{OnText: func(s string) { got = s }, OnSession: func(id string) {}, OnNote: func(string) {}})
	if err != nil {
		t.Fatalf("live run: %v", err)
	}
	if !strings.Contains(strings.ToLower(got), "pong") {
		t.Fatalf("reply: %q", got)
	}
}
```

  本机验证一次:`$env:CRUX_LIVE_CLAUDE="1"; go test ./internal/engine -run TestLiveClaudeSmoke -v`(真烧一次最便宜的调用)。
- [ ] **README.md**:Features 增两条(Claude Code 后端 + 三档审批模式/沙箱语义表);Quick start 增 claude 前置(`npm i -g @anthropic-ai/claude-code` 或 native install + `claude login`);Known limits 增:中途切后端 claude 首轮全新、regenerate 对 claude 是"重问"(其会话上下文有重复)、Windows Agent 模式 bash = 逐命令审批。
- [ ] **SPEC.md**:§7 工具边界改写(过门后的三档表);新增 §X "Claude Code 后端" 与 §Y "审批与沙箱"(引用 spec 设计文档路径);交互清单补验收点(审批卡/模式切换/claude 卡片)。
- [ ] **spec 文档注记**:`docs/superpowers/specs/2026-10-10-claude-sandbox-design.md` 末尾追加 "实施注记" 小节:两处简化(UnifiedDiff 不移植、--add-dir 不做)+ Windows taskkill 杀树改进已落地。
- [ ] **全量验证**:
  - `gofmt -l .` → 空
  - `go vet ./...` → 干净
  - `go test ./...` → 全绿(Windows)
  - `GOOS=darwin go build ./... && GOOS=linux go build ./...` → 编译过
  - (可选,有图形环境)`go run .` 手动烟测:Settings 切 Claude Code → 发一句话 → 三档切换 → 审批卡 Allow/Deny → Stop
- [ ] Commit: `git add -A internal/engine/claude_live_test.go README.md SPEC.md docs/superpowers/specs/ && git commit -m "docs+live: claude backend and approval-mode docs, live smoke test"`

---

## 任务依赖图

```
1 gate ──┬─→ 4 tools 过门 ──┐
2 cliproc ┴─→ 5 claude events ─→ 6 RunClaude ─→ 7 进程级测试 ─→ 8 loop 派发 ─→ 11 app 接线 ─→ 12 收尾
3 sandbox ─→ 4                                    9 store ─→ 10 settings UI ─→ 11
```

(1、2、3、9 相互独立,可并行;11 依赖 8+9+10。)
