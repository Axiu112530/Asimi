# DeskPilot AI — Roadmap and Gates

The Community 0.2.1 candidate and its outstanding distribution and manual acceptance gates are tracked in [Community release preparation](0.2.1/COMMUNITY-RELEASE.md). The phase matrix below records the earlier roadmap.

Phase 1 and Phase 2A were explicitly authorized and are implemented. Their
statuses are recorded below. Phase 2B and later require explicit approval after
reviewing outputs and unresolved conditions.
Each increment preserves local execution, BYOK, independent safety and bilingual UI.

| Phase | Input and work | Exit acceptance | Stop condition |
|---|---|---|---|
| 0 — Feasibility/foundation | Supplied brief + current workspace; scoped environment, native/UIA/provider/i18n probes and design | Seven docs, actual evidence, dependency/license assessment, explicit GO/CONDITIONAL GO/NO-GO | Stop after report; do not build product automatically |
| 1 — Minimal Agent loop | Approved Phase 0/conditions and scoped Phase 1 design | Task → mock analysis/plan → human approval → one permitted tool → observation → independent verdict; deny/cancel/timeout paths; local history; bilingual shell | Stop for review before adding tool breadth |
| 2 — Windows MVP | Accepted Phase 1 and approved MVP plan | Model center/BYOK/explicit capability probes, computer/UIA, files, PowerShell, Git, security center, task records, SQLite/DPAPI and complete UI languages | Stop on failing security or core user scenarios |
| 3 — Reliability/security | Feature-complete scoped MVP + failure corpus | Crash recovery/checkpoints, audit, display/DPI/app matrices, permission/race/injection tests, bounded cost and measured stop/recovery behavior | No unattended operation or release until gates pass |
| 4 — Open-source release | Stable tested build, chosen license and release approval | English docs/Chinese entry, reproducible signed installer, dependency notices/SBOM, demo, upgrade/uninstall checks, approved GitHub Release | No automatic publish/push; explicit release authorization |
| 5 — Commercial extensions | Stable Community + validated customer needs | Separate Pro/Business design, licensing and enterprise policy review; no weakening Community safety | Billing/account/workflow implementation needs its own approval |

## Suggested Phase 1 slice (not implemented)

Use Mock Provider, one disposable project directory and a deterministic
`file.create` tool that refuses overwrite and writes only a supplied fixture
payload. Show exact path/content in the plan/action approval, classify L2 and
require consent for this initial slice. Verify file bytes/hash via an approved
read tool. The mock's completion text is never sufficient. Keep agent imports
free of Windows/transport APIs. SQLite holds task/step/evidence and settings;
DPAPI dummy-key tests can remain isolated until actual BYOK work.

Implement defaults: English, live Chinese settings, independent response-language
enum, bounded steps/time, single sequential tool, pause/cancel and a stop lane.
Negative acceptance: denied request writes nothing; cancelled request schedules
nothing; stale approval is rejected; tool success with mismatched bytes does not
complete task; UI language switching retains task ID/history. No real model call
is necessary for this slice, so paid API keys must not be requested implicitly.

## Conditions and ownership

Before approving Phase 1: review proposed contracts, permissions and the report's
open conditions. A controlled Mock/file slice does not need multi-monitor input;
the unresolved hardware/UIA application matrix must stay disabled/experimental
until its Phase 2/3 acceptance is recorded. If choosing GUI actions for Phase 1
instead, close the relevant application/focus/DPI conditions first.

Before Windows MVP: complete all 11 computer actions, authenticated helper IPC,
real directory grants/reparse checks, PowerShell/Git command restrictions, local
key ACLs, approved provider transport tests and full i18n coverage.
Before release: test Windows 10 separately (this host does not prove it), Windows
11, mixed-DPI monitors, supported app versions, disk failure and upgrade rollback.
Assign implementation/test owners at Phase 1 approval; no fictitious assignments
or completed product checkboxes are implied by this roadmap.

## Phase 1 status — functional prototype delivered

The user subsequently authorized the complete workbench around the same narrow
execution boundary. That slice is now implemented: UI/navigation/settings, offline
Mock/clarification, task planning/state/events, exact approval, one safe create
tool, independent verification, cancel, nine SQLite entities and history/security
views. No real model or computer-control capability was enabled.

Unified functional acceptance is recorded in PHASE-1-ACCEPTANCE. It includes a real
approved file with external byte/hash verification, rejection/cancellation/no
overwrite/path refusal, unsupported request failure, clarification isolation and
restart restoration. Phase 0 report/evidence integrity remains unchanged.

Phase 1 stops after its report. Phase 2 still requires explicit authorization and
a concrete incremental plan. Recommended priorities: investigate Wails/WebView2
shutdown error 1412 without hiding it, strengthen lifecycle/crash recovery and
storage-failure behavior, then approve only one new capability at a time. A real
provider would require separate endpoint/data/cost consent and a DPAPI implementation;
desktop/runtime breadth would require its own application/DPI/security gates.

## Phase 2A status

The reliability increment adds v2 file intents, native file witnesses,
restart reconciliation, SQLite fault handling and a window-first shutdown.
The 1412 diagnostic is characterized for the pinned Wails/WebView2 environment.
The Phase 2A conclusion is **CONDITIONAL GO**. Phase 2B awaits approval.
[Phase 2A report](PHASE-2A-REPORT.md)

## Phase 2B authorized increment

Phase 2B is authorized for user-triggered BYOK text/vision only: Responses and
Chat Completions/custom gateways, DPAPI, SSE, local image attachments, Playground,
usage and price snapshots. This does not authorize real-model tool execution or
new desktop/shell/Git capabilities. Native Anthropic/Gemini adapters remain future
work. Automated protocol fixtures and live-provider acceptance are separate gates;
see [Phase 2B acceptance](PHASE-2B-ACCEPTANCE.md). Phase 2C requires explicit review
and authorization, not automatic continuation after automated tests.

## Phase 2C observation and analysis

Phase 2C adds selected-display capture, local preview and explicit screen
analysis using BYOK. The acceptance report separates provider live probes,
local synthetic fixtures, and actual display hardware coverage. Phase 2D
requires a separate decision. [Phase 2C acceptance](PHASE-2C-ACCEPTANCE.md)

## Phase 2D · 当前工程范围

已实现专用 host、受限 UIA 检查、窗口指纹、逐动作 Proposal 与审批、SQLite v4 journal、UIA 语义操作、验证、停止与未知结果恢复。Notepad 保留只读，raw SendInput、坐标点击、快捷键和模型自动动作未开放。最终门禁及实测证据见 [Phase 2D Report](PHASE-2D-REPORT.md) 与 [Acceptance](PHASE-2D-ACCEPTANCE.md)。扩大应用白名单与下一阶段需要用户确认。
## MVP 0.1

MVP 0.1 在已验收范围内为 CONDITIONAL GO。真实 Provider、Notepad、应用观察及受控安装仍待完成。详见 [工程报告](MVP-0.1-REPORT.md)。

## DeskPilot 0.2

0.2 完成真实 DeepSeek 驱动的 Go 修复闭环和逐动作审批。下一轮优先验收真实桌面视觉与 Notepad，扩大项目和运行时兼容矩阵，并评估受限电脑动作规划和 TxF 的安全替代。
[0.2 产品报告](0.2/PRODUCT-REPORT.md) · [Community 发布准备](0.2/COMMUNITY-RELEASE.md)
