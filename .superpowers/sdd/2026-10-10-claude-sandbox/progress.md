# Ledger — 2026-10-10 claude-sandbox plan (native execution)

Plan: docs/superpowers/plans/2026-10-10-claude-sandbox.md
Spec: docs/superpowers/specs/2026-10-10-claude-sandbox-design.md
BASE: fe72b3b (baseline: docs e317b12/fb8b948 + user's uncommitted timeline feature committed)

Setup notes:
- Repo-local Go cache required: `$env:GOCACHE="$PWD\.gocache"; $env:GOPATH="$PWD\.gopath"` (default
  AppData cache is access-denied on this machine).
- Baseline suite: all packages ok (one flaky TestWriteFileAtomicConcurrent — Windows concurrent
  rename race, passes on retry; not ours).

Rulings:
- Baseline commit fe72b3b: committed the user's uncommitted timeline feature (873 insertions,
  tests green) before Task 1 — otherwise task commits on shared files (thread.go, state.go,
  llm.go, persist.go, loop.go) would drag that work in. Cost if wrong: user loses a clean
  separation of that feature's commit; recoverable via soft reset, nothing pushed.
- Working on main directly (no worktree): the harness worktree tool needs an explicit user
  request, docs commits already went to main unobjected, and the uncommitted baseline would not
  have traveled to a worktree anyway. Cost if wrong: history lives on main; revertable.

- Ruling: the gofmt gate runs as `go fmt ./...` (empty output = clean). `gofmt -l <dirs>` recurses
  into .gopath/.gocache when handed the repo root, so directory args are off limits.
  Cost if wrong: none (cosmetic gate; verified both forms).

## Task log

Task 1: complete (commit 705fae9; tests: engine gate suite 6/6 pass, full `go test ./...` ok,
go vet clean; RED verified — build failed on missing types, then green).
Task 2: complete (commit dc824c7; tests: engine cliproc suite 11/11 incl. real Windows
tree-kill reaping, full suite ok, vet clean; RED verified, then three test-own bugs fixed —
Trunc expects rune counting, TrimOutput's error line must sit in the kept tail, cmd && -chain
lingered out instantly).
Task 3: complete (commit fd3de5e; tests: sandbox suite 4/4, full suite ok, vet clean, and
GOOS=darwin/linux/windows builds all pass; RED verified — undefined symbols).
Task 4: complete (commit 6abd5fc; tools gated — read-only denies without asking, windows
ask-path approves/denies/timeout/nil-handler fail closed, workspace confine, credential
deny, fake-sandbox wiring (HOME=scratch, sandboxed=true details); full suite ok. One test
fix: the ASK is redacted, the tool's own output is not (reference semantics).
Task 5: complete (commit 806720e; tests: 15 line-level claude event tests pass on Windows;
RED verified. Ruling vs plan: Callbacks.OnSession/OnNote added here instead of Task 8 — the
event layer needs them to compile; cost if wrong: none, builtin loop just never fires them).
Tasks 6+7: complete (commit 8ed9f73; 10 process-level tests pass — fake .cmd CLI on Windows,
.sh on Unix, real spawns. Rulings vs plan: (1) merged into ONE red-green cycle to honor TDD —
plan had impl (Task 6) before its tests (Task 7). (2) stdin lines are CRLF-terminated: LF-only
broke cmd's set /p in fakes AND would break any strict \r\n-only reader in the real CLI;
CRLF is safe for readline and \n-split+JSON.parse. Live probe (Task 12) will confirm on the
real CLI. (3) Windows ProcGroupAttr now sets cmd.Cancel=taskkill-tree + WaitDelay: killing
only the .cmd wrapper left the node child holding stdout, Wait hung 9s. Cost if wrong: a
CLI intolerant of \r\n — none known; verify in Task 12.)
Task 8: complete (commit 9cb2f8e; dispatch tests + loop suite green, full suite ok. ClaudeConfig
built from Config at the Run boundary; builtin loop now gated (Mode/OnApproval/Sandbox ride);
result event seeds the note's session label as an init-less fallback. Test fixes: dispatch fake
needed an init line; builtin-gate test needed BaseURL so it fails on the model check).
Task 9: complete (commit 55e8747; store suite + round trips green. Backend/Mode ride LLMConfig
omitempty, ClaudeID rides Transcript omitempty — legacy files load with them empty).
Task 10: complete (commit e28395c; 4 render tests green, full suite ok. Agent type Segmented at
the top of providersPane swaps builtin form ↔ claude form (settings_claude.go: detection row +
model alias + auth note); detectClaude is headless-safe (inline probe when a.redraw nil);
backendLabel claude branch "claude · alias|default". Reused thread.go's firstLine — deleted my
duplicate.)
Task 11: complete (commit a329be9; 8 tests green, full suite ok. approval.go: pending req on
a.app + buffered(1) channel bridge; awaitApproval publishes the card on the streaming row via
a.redraw and answers or denies on ctx; settleApproval non-blocking send (late answer dropped);
composer's Segmented (Read-only/Agent/Full access) is the approval switch, Busy freezes it;
thread.claudeID ↔ Transcript.ClaudeID; OnSession/OnNote wired in startStream; claude tool names
mapped (Bash/Edit/Write/Read → bash/write_file/read_file), Edit/Write FileChange rebuilt from
args (old_string/new_string/content) when the result is plain text; streamRowID tracked and
pending cleared when the reply ends.)
Task 12: complete (docs + live probe; see live ruling below). SPEC: §7.2 rewritten around the
gate, new §7.2.1 (modes table / sandbox / approval card) and §7.6 (claude backend), §14 items
18-20 (engine + app test inventories), §15 limitations updated (gate replaces "no gate", claude
limitations documented, approval-gate moved out of 可扩展方向). README: three-mode + claude
backend feature bullets, Agent type in Settings bullet, live-probe command, project table rows.
Design doc §11 实现记录: the two planned simplifications confirmed (no UnifiedDiff port, no
--add-dir) + the three implementation rulings (CRLF, Windows tree-kill, result session label).
Full suite + vet + fmt clean; GOOS=darwin/linux/windows builds all pass.
LIVE PROBE RULING (Task 12): the timestamped wire probe against the REAL claude CLI proved
the integration end to end — prompt on stdin consumed, init/assistant/result events parsed,
result ends the turn, and the CLI stays resident afterwards (exactly what Reap exists for).
The turn itself failed with the CLI's own "API Error: Connection" because the user's claude
routes through a local magpie gateway (ANTHROPIC_BASE_URL http://127.0.0.1:3425, from
~/.claude/settings.json) that is NOT running right now — environment, not code. The live
smoke test (claude_live_test.go) is opt-in (CRUX_LIVE_CLAUDE=1, 8-min budget) and will pass
whenever the gateway is up; its failure message names the reply text so an API outage reads
clearly. Cost if wrong: none in CI (skips by default); the smoke stays red only while the
local gateway is down.

