# DeskPilot AI — Phase 1 Functional Prototype Report

Date: 2026-11-10, Asia/Shanghai. Version: 0.1.0 functional prototype.

## Outcome

**Implemented and functionally accepted within the authorized Mock + one controlled
file-create scope. Phase 2 has not started.** This is an interactive native desktop
application with a real approved local execution path, not a static/mock UI.

中文摘要：已实现完整工作台、多语言设置、Mock 规划与澄清、任务状态机、精确审批、
唯一的隔离文件创建工具、独立验证、取消、SQLite 与历史/安全中心。真实模型、电脑
控制、终端、Git 和任意文件访问均未开放。实际文件已创建并由独立验收程序核对内容
和 SHA-256。已停在 Phase 1；Wails/WebView2 的 1412 退出诊断仍未解决，不宣称可发行。

## 1. Run the application

- Binary: [`build/bin/DeskPilotAI.exe`](../build/bin/DeskPilotAI.exe).
- Start from PowerShell: `./scripts/run.ps1`.
- Rebuild: `./scripts/build.ps1` (uses the existing local SDK/caches).
- Default data: `%LOCALAPPDATA%/DeskPilotAI`; isolated file directory: `workspace/`.
- For an explicitly isolated profile: `./scripts/run.ps1 -DataDir <local-directory>`.

An ordinary launch loads neither the acceptance script nor a debugging port.
The binary is a Windows GUI executable, not an installed/signed release package.

Supported demo input:

```text
Create file "notes.txt" with content "Hello from DeskPilot"
创建文件“笔记.txt”，内容为“你好，DeskPilot”
What demo capabilities are available?
Explain the file creation plan.
```

Guided JSON is also supported: `{"fileName":"notes.txt","content":"Hello"}`.
Missing information produces a clarification. Continue with an explicit field,
e.g. `content: "Hello"`, or one standalone quoted value for the missing field.
Unrelated quoted commands are rejected rather than merged into an old draft.
Review the actual file/path/content/hash preview and approve explicitly. Existing
files cannot be replaced; use a different name for another demonstration.

## 2. Implemented functions and honest boundaries

| Area | Actual behavior |
|---|---|
| Desktop shell | Wails v3 native GUI, sidebar/navigation, top Mock/provider/model/status/language and always-present Stop all |
| Assistant | Welcome actions, Enter send/Shift+Enter newline, nonempty input, multi-turn persisted messages/timestamps, safe Markdown/code blocks |
| Task inspector | Collapsible plan/current step/status, exact approval preview, tool record, verifier bytes/hash/content result and final summary |
| Mock Agent | Neutral request/result contract, deterministic English/Chinese parsing, capabilities/explain/clarification, result-driven Summarize after real tool outcome |
| Runtime | Central state transitions, private task controls, one-tool dispatch loop, expiry, cancellation, durable outcomes and native change events |
| File tool | Single validated basename/content in pinned local workspace, 64 KiB cap, no-replace commit, private independent readback |
| Settings | Saved app/Agent language, light/dark/system theme, welcome/last-conversation startup, Models/Security links and About runtime information |
| Models | Mock enabled, eight real-provider placeholders disabled with UNKNOWN capabilities and no API-key input |
| Security | Real pending/history approvals, execution/audit records, current task permissions, fixed local-only privacy/stop policy |
| History | Persistent conversations and tasks; selection reopens actual messages/plan/result |
| Computer/Developer/Workflows | Functional navigation and capability/status descriptions, explicitly unavailable/coming-soon actions |

Unsupported screenshot/PowerShell requests now become FAILED with
`UNSUPPORTED_REQUEST`, not a green completed task. Capabilities and explanation
questions may complete as read-only answers; they do not claim tool effects.

Not implemented: real model transport/capability probes, DPAPI credential storage
implementation, computer screenshot/input/UIA, terminal/PowerShell/Git, arbitrary
file read/write/delete, workflows, global OS hotkey, unattended recovery, accounts,
payments, installer/signing or Phase 2. No fake success or secret-entry workflow
is presented for these capabilities.

## 3. Source structure and responsibilities

```text
main.go                         native composition, narrow binding, embedded assets
internal/domain/types.go         neutral business DTOs, reserved vault interface
internal/app/service.go          explicit business dispatcher, settings/control
internal/app/runtime.go          plan/state/approval/cancel/verification decisions
internal/app/store.go            SQLite migration/repositories
internal/provider/mock.go        offline proposal/clarification/summary, no OS/network
internal/tools/registry_windows.go  mandatory safety pipeline and private NT IO
frontend/src/                    Vue render components, state/events, safe Markdown
frontend/src/locales/            en-US / zh-CN
scripts/                        project-local build/run/acceptance tools
validation/phase1/               own-WebView acceptance and locale checks
```

Phase 0's Mock was a disposable contract probe offering screenshot proposals, not
a usable product runtime. Its source remains untouched. Phase 1 retains the
provider-neutral `Generate(ctx, request)` seam and extends typed requests/results
for conversations and outcome summaries, narrowing executable proposals to the
one authorized file tool. No Phase 0 capture/input probe is linked into this app.

Render components use Vue/TypeScript directly, reusing the verified toolchain
without adding an unverified SFC plugin or UI framework. Design uses system fonts,
local vector icons, a restrained accent, clear scroll regions and a compact sidebar
at narrow widths. Agent-facing code never imports native capture/input APIs.

## 4. Persistence

Schema version 1, initialized transactionally through `PRAGMA user_version`.
Foreign keys, WAL, busy timeout and one connection keep state writes ordered.

| Entity/table | Persistent information |
|---|---|
| AppSettings / app_settings | App and Agent language, theme, startup |
| ProviderProfiles / provider_profiles | Mock and disabled future profiles/capabilities |
| Conversations / conversations | IDs, title, creation/update times |
| Messages / messages | Role, original text, conversation/task IDs, timestamps |
| TaskSessions / task_sessions | State, description/plan snapshot, draft, response language |
| TaskSteps / task_steps | Ordered step IDs/status/results/errors |
| ApprovalRecords / approval_records | Immutable operation, decision/expiry/consumption |
| ToolExecutions / tool_executions | Executed tool/path, outcome and verification |
| AuditEvents / audit_events | Stable event code, task, minimal parameters/timestamp |

Indexed IDs/status/timestamps support actual history/security views. Original user
messages and exact approval content remain persisted locally; SQLite does not
encrypt conversation data. No real key or screenshot exists in the product data.
Interrupted tasks recover as PAUSED, unused approvals become INVALIDATED and nothing
resumes automatically. A crash between file commit and durable result may require
manual review; it is not guessed to be either success or no-op.

## 5. Business service boundary

Only `main.DesktopService.Call(method,payloadJSON)` is registered in ordinary Wails
launches. Its dispatcher supports these explicit business methods:

| Group | Methods |
|---|---|
| Application | GetAppInfo, GetSettings, UpdateSettings |
| Conversation | CreateConversation, SendMessage, GetConversation, ListConversations |
| Task | CreateTask, GetTask, ListTasks, CancelTask, StopAll |
| Approval | GetPendingApprovals, GetApprovalHistory, ApproveAction, RejectAction |
| Provider | ListProviders, ListModels, GetModelCapabilities |
| Security | GetSecurityStatus, GetAuditEvents, GetToolExecutions |

Requests decode strictly; unsupported methods/fields fail with stable codes.
There is no state setter, raw SQL/shell call, arbitrary tool name execution, file
reader or OS API binding. Replies are `{data}` or `{error:{code,params}}`; Vue owns
localized error descriptions. Native `deskpilot:changed` events refresh business
state; there is no product polling loop. UI language changes retain existing task
IDs, grants and original message text.

## 6. Safety execution chain

1. Mock proposes a typed single-file request; it has no OS/network authority.
2. Task planner calls Registry.Prepare. Security validates basename/size/UTF-8,
   pinned workspace and the immutable normalized operation/hash/expiry.
3. App persists PENDING approval and WAITING_APPROVAL task. Nothing is written.
4. Human approve/reject acts on an ID, not client-supplied altered arguments.
5. Registry.Execute revalidates and invokes the private Permission Engine callback.
   A transaction compares every operation field and CASes APPROVED → CONSUMED.
6. Private executor writes/flushed an owned temporary handle, rechecks cancellation/
   expiry and atomically commits without replacement using root-relative NT IO.
7. Separate read handle verifies actual identity/content/byte count/SHA-256.
8. Runtime independently checks verifier fields, passes actual outcome to Mock
   summary, rechecks current cancellation, and persists the final verdict.

Drive/ancestor/workspace handles reject reparses and block rename while in use.
Approval callbacks run outside the registry mutex, and filesystem/model work runs
outside the task-control mutex. This prevents the lock inversion found during
integration and keeps cancel independent. Cancellation cleans only owned uncommitted
temp files; after atomic commit it records Created=true and preserves the file.
The private readback is not registered as a model-accessible read tool.

## 7. Internationalization

English default independent of Windows language; live Chinese switching; SQLite
restoration; separate follow-app/auto/en-US/zh-CN response language; explicit task
language priority. **396 matching keys**, placeholder parity and 194 literal key
uses checked, with missing-key fallback to English. Dates/times/numbers use Intl.
Resource keys include errors/audits/status/approval/tooltips/ARIA and all routes.
Native acceptance exercised English UI + Chinese Agent and the converse without
rewriting old history. Render evidence covers both locales and light/dark at
1380×820, 1000×700 and 900×620.

## 8. Verification evidence

The [final verification record](phase1-evidence/final-verification.json) identifies
the delivered binary hash and consolidated check results.

| Check | Result / evidence |
|---|---|
| Focused Go safety/contracts | **30 top-level tests**, 21 named subcases, zero failures; [log](phase1-evidence/safety-tests.log) |
| Go static/module integrity | Clean vet; all cached modules verified; [vet](phase1-evidence/go-vet.log), [modules](phase1-evidence/go-mod-verify.log) |
| Locale source/resource checks | 396 equal keys, parameter parity/fallback, [JSON](phase1-evidence/i18n-check.json) |
| TypeScript/Vite/native GUI build | Exit 0; [build log](phase1-evidence/build.log) |
| Unified native functional process | **58 assertions**, exit 0; [stage 1](phase1-evidence/app-stage1.json) |
| Restart/persistence process | **10 assertions**, exit 0; [stage 2](phase1-evidence/app-stage2.json) |
| Fresh English welcome | **3 assertions**, exit 0; [stage 3](phase1-evidence/app-stage3.json) |
| Independent file oracle | Exact **42 UTF-8 bytes**, hash below, denied/cancelled/traversal files absent; [summary](phase1-evidence/acceptance-summary.json) |
| Phase 0 preservation | Original report + all evidence **27/27 SHA-256 unchanged**; [baseline](phase1-evidence/phase0-baseline.json) |
| Own-WebView layout/theme | 3 widths × English/Chinese, light/dark; no document horizontal overflow; [English](phase1-evidence/visual-stage3.json), [Chinese](phase1-evidence/visual-stage2.json) |

Independent fixture SHA-256:
`d15cca7fd9b170c4e06cccfa733f0a46309fdc51af91ad8a67990df67e32f933`.

Safety cases include real non-admin junction tests, post-approval target creation,
immutable approval replay/tamper, verifier mismatch, cancel before/after commit,
expiry, interrupted recovery, explicit multilingual continuation and unsupported
command isolation. Peer review identified and corrected misleading unsupported
COMPLETED status, unreachable PENDING steps and quoted-command draft merging.

Own-WebView screenshots are rendered application evidence, not desktop capture.
The harness is CLI-gated and uses native business calls/DOM events plus loopback
CDP; it does not use physical mouse/keyboard or enable product computer controls.
The host sandbox denies necessary ancestor metadata handles, so scoped file/native
tests ran outside that sandbox with project-local profiles/temp files, not as an
administrator. No Phase 0 suite was rerun and no global tool upgrade occurred.

## 9. Known issues and explicit limits

- **Wails/WebView2 shutdown error 1412 remains reproducible.** All acceptance
  processes exit 0, but `Chrome_WidgetWin_0` class unregister logs remain in stderr
  and local runtime logging. Nothing was filtered or upgraded to hide it. See
  [stage 1 stderr](phase1-evidence/app-stage1.stderr.log). Treat lifecycle/resource
  investigation as a next-stage priority; no production stability guarantee.
- This host is Windows 11 25H2/build 26200.9711. No new Windows 10 matrix, full
  DPI/accessibility/visual regression, race/fuzz/load/long-run tests were done.
- Same-user malicious processes/admin compromise are outside the logical policy
  guarantee. File verification is point-in-time; it is not ongoing integrity
  monitoring after handles close.
- Cancellation prevents later scheduling and reports in-flight commits honestly;
  synchronous kernel IO is not forcibly interrupted and no stop-latency SLA exists.
- No signed installer, distribution license selection, full dependency security/
  license audit, encrypted history or general crash recovery was added.
- The deterministic Mock accepts documented grammar/JSON; it is not a general
  natural-language model. UI and business capability pages disclose that boundary.

## 10. Next decision

Review the executable and [acceptance checklist](PHASE-1-ACCEPTANCE.md). Suggested
Phase 2 order: resolve/characterize lifecycle diagnostics, reinforce crash/storage
failure behavior, then choose one separately authorized provider or native tool
increment with explicit consent and matching security tests. Real model costs,
data egress and computer control require new authorization. **Phase 1 stops here.**
