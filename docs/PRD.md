# DeskPilot AI — Product Requirements

Status: Phase 1 functional prototype, 2026-11-10 (Asia/Shanghai). Full product
requirements below include future MVP scope. The Phase 1 addendum distinguishes
current behavior; evidence is in [PHASE-1-REPORT](PHASE-1-REPORT.md) and the preserved
[PHASE-0-REPORT](PHASE-0-REPORT.md).

## Purpose and users

DeskPilot AI is a Windows 10/11 x64 desktop agent that understands a task, observes
the user's computer, proposes a plan, executes approved tools locally and verifies
the outcome. It combines General Computer Assistant and Developer Assistant
roles. Workflow Agent and enterprise Security Center capabilities grow later.
It must complete work rather than only explain how a person could do it.

Primary users: individual developers setting up projects and resolving build
problems; technical users performing repetitive desktop tasks; privacy-conscious
BYOK users who need visible control over every sensitive side effect. Enterprise
administrators are a later audience, not a first-release account requirement.

## Representative scenarios

| Scenario | Intended outcome | Required evidence and boundaries |
|---|---|---|
| Inspect an unfamiliar project | Summarize authorized files and a proposed plan | Directory grant, file references, no computer-wide indexing |
| Fix a development issue | Reviewable patch and relevant test result | File diff, command preview, test exit/output, no automatic push |
| Set up a development tool | Approved installation and verified version | L3 exact installer/source/arguments, UAC handled by user |
| Operate an editor | Locate a control, enter text and verify it | UIA pattern/readback or before/after observation, focus/PID guard |
| Perform a desktop action | Approved change in the selected application | Fresh observation and objective postcondition, pause on ambiguity |
| Change language | Immediate translated UI, restored next launch | English first start, persisted explicit choice, unchanged task history |

## v0.1 scope

DeskPilot v0.1 — Windows Developer & Computer Assistant includes Wails v3 + Vue 3
+ TypeScript UI, English default/Chinese switching, model configuration and BYOK,
capability registry/probes, screenshots, mouse/keyboard, basic UIA, scoped file,
PowerShell and Git tools, planning, bounded Agent loop, independent verification,
approval, pause/cancel/stop, local task history, SQLite and Windows DPAPI.

The model never calls OS APIs directly. Mandatory dispatch is Agent Runtime →
Tool Registry → Security Engine → Permission Check → Tool Runtime → Windows API.
Native deterministic tools are preferred, followed by UIA, then visual fallback.
An API reporting compatibility must not be treated as evidence of vision/tools.

Explicit exclusions: macOS/Linux implementation, voice, multiple collaborating
agents, cloud remote control, enterprise identity, plugin marketplace, unattended
background operations, local inference, complete Office automation, and billing.
No Phase 1 functionality is implemented during Phase 0.

## Experience and acceptance

The desktop UI exposes task, proposed plan, pending approval, current tool,
verification evidence and a persistent stop control. Denial/timeouts retain a
readable task state. Ambiguous results are inconclusive, never automatically
successful. Every task has bounded actions/time/retries/cost and a cancel path.
No progress animation can substitute for tool evidence.

All user-facing copy (including errors, menus, tooltips and accessibility labels)
uses locale keys. First start is en-US regardless of OS language. Settings allows
live zh-CN switching and independent Agent Response Language: follow app, auto,
English, Simplified Chinese. Explicit task language takes priority. History is
preserved in its original language. Dates/numbers use Intl and stored UTC times.

Local-first means execution and orchestration are local. User-approved model
requests can transmit content to the configured provider; this is visible before
upload. No real paid inference, silent model tests or raw screenshot retention
is allowed by default. Secrets never enter UI history, logs or exported bundles.

## Business boundaries

| Edition | Boundary |
|---|---|
| Community | Local BYOK; basic visual/computer/development tools, essential safety approvals and task records |
| Pro, later | Advanced planning, long-task recovery, advanced reusable workflows, project context, routing, replay and automation |
| Business, later | Team management/policies, shared workflows, enterprise audit and internal deployment support |

Safety controls, cancellation and key protection must remain in Community.
Core interfaces must not import future billing/account packages. Editions may
add planners/workflows/policies through interfaces, not fork the execution safety
path. No payment system, pricing commitment or licensing choice is made here.

## Success and release gates

Phase 1 succeeds when one approved scoped tool has an independently verified
outcome with Mock Provider and visible cancel/deny behavior. v0.1 requires all
listed capabilities and Windows/i18n/security matrices in TEST-STRATEGY. Reliability
and latency measurements must precede any commercial SLA. A model's self-report
is never the acceptance oracle. Every phase has an explicit stop gate in ROADMAP.

## Phase 1 implemented scope — 2026-11-10

The functional prototype now has a native Wails workbench, eight working navigation
routes, conversation/message history, structured task inspector, precise approvals,
verification evidence, operation/audit history, saved settings and light/dark/system
themes. The only executable capability is `filesystem.file.create`: one explicit
basename/content pair in the application's flat workspace, maximum 65536 UTF-8
bytes, no replacement. The offline Mock handles create, capabilities, explanations
and explicit multi-turn clarification. Unsupported requests fail honestly without
a tool operation; an informational answer is distinct from accomplishing an
unsupported action.

Computer, Developer and Workflows are useful scope/status pages with disabled
future actions. Models lists Mock plus eight disabled provider profiles; there is
no API-key entry or external provider transport. Desktop capture/input/UIA,
PowerShell, Git and arbitrary file access remain unavailable in this build.
Community essential safety remains mandatory, with no paid feature or account gate.

Current acceptance evidence and limitations are recorded in
[PHASE-1-REPORT](PHASE-1-REPORT.md) and [PHASE-1-ACCEPTANCE](PHASE-1-ACCEPTANCE.md).
This addendum records implementation; the preceding full MVP scope remains the
longer-term product requirement, not a claim that Phase 2 is complete.
