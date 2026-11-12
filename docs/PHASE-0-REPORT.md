# DeskPilot AI — Phase 0 Technical Validation Report

Date: **2026-11-10, Asia/Shanghai**. Scope: feasibility and project foundation only.

## Decision: CONDITIONAL GO

**中文结论：核心技术路径已通过本机最小验证，可在用户确认并接受下列条件后，
进入限定为 Mock Provider + 单一受控文件工具的 Phase 1。没有完成完整产品，也没有
验证真实付费模型。多屏混合 DPI、真实应用完整 UIA 覆盖、UIA 长期稳定性、全局急停
与生产安全执行链仍需独立验收。Phase 0 到此停止，不自动进入 Phase 1。**

Conditions for Phase 1 approval:

1. Accept the proposed module/permission/provider contracts and limit the first
   slice to one approved local fixture file tool and Mock Provider. GUI automation
   and live inference remain gated rather than silently inheriting feasibility.
2. Implement the non-bypassable registry → security → permission → executor path,
   objective verification and independent stop/cancel lane in that slice; the
   Phase 0 probes are not a security implementation.
3. Carry the reproducible WebView2 shutdown diagnostic into Phase 1 lifecycle
   investigation. No production stability claim is justified by two successful
   exits. Complete GUI/UIA/DPI/hardware conditions before enabling those features.

No demonstrated fundamental Go/Wails blocker requires NO-GO for this controlled
slice. Unconditional GO for the complete Windows MVP would be unsupported.

## 1. Environment and change boundaries

Evidence: [environment.json](evidence/environment.json),
[Go archive metadata](evidence/go-download.json),
[Wails doctor](evidence/wails-doctor.log).

| Item | Actual observation |
|---|---|
| Workspace | Initially empty, no `.git`, branch or uncommitted changes to inspect; no `.codegraph`; no existing files overwritten |
| Windows | x64, 25H2, build **26200.9711**; runtime branding Windows 11 家庭版; registry ProductName remains Windows 10 Home |
| PowerShell | 7.6.9; .NET Framework compiler available; .NET SDK absent from PATH |
| Node / npm / Git | **24.21.0 / 12.2.0 / 2.55.0.windows.5**, unchanged |
| Go initially | Not on PATH; absent at the two standard Go paths checked |
| Go for probes | **1.26.10**, verified official ZIP extracted under `.phase0-tools/go` |
| Wails | **v3.1.0**, project-local CLI compiled from pinned Go module |
| WebView2 | **157.0.4341.42**, doctor and actual environment creation agree |
| Privileges | Probe process did not have an administrator token; no UAC bypass |
| Display | One physical monitor, **2880×1800** virtual desktop, origin (0,0) |
| Packaging | NSIS, MakeAppx, MSIX tooling and SignTool absent; signing unconfigured |

Do not label this a Windows 10 compatibility run solely from registry ProductName.
Windows 10 is an untested release target. The Go ZIP SHA-256 was checked against
the official download metadata before extraction; Go module checksums also passed.

Initial sandbox network calls failed on the existing local proxy socket. Approved
execution outside that sandbox reached the official Go/npm registries. Go's first
direct IPv6 attempt timed out; setting the **child process only** to use the
existing system proxy resolved downloads. No user/global proxy, PATH, environment,
Go, Node or npm installation was changed. A bounded diagnostic SQLite ZIP download
also succeeded; no indefinite retry loop was used.

Only the workspace, relevant tool commands, Windows development/runtime metadata,
standard relevant app paths and controlled application windows were inspected.
No paid model calls, repository initialization/commit/push, installation of global
tools, bulk deletion or raw screenshot persistence occurred. Tool/cache artifacts
remain project-local and ignored.

## 2. Wails v3 feasibility

**PASS for this host's build/runtime path; production lifecycle remains partial.**

The current official module metadata identified v3.1.0 (tag time 2026-11-01) with
Go 1.26.0/toolchain 1.26.10. This was verified from the registry rather than assumed
from an old alpha recommendation. The [installation documentation](https://v3.wails.io/getting-started/installation/)
confirms the Go/WebView2 prerequisites; the downloaded module and binary identify
the exact tested version.

`wails3 doctor` exited 0 and reported readiness. The isolated Wails binary compiled
with CGO_ENABLED=0 and embedded Vue assets; WebView2 logged successful environment
creation. Two independent processes each exited 0 after completing 12 DOM/config
assertions against a fresh per-run profile/database. The probe uses a narrow Go
asset handler for settings/results; generated service bindings, installers, menus,
updater and application recovery are not yet verified.

Both process stderr logs contain **Failed to unregister class Chrome_WidgetWin_0,
Error = 1412** at shutdown. This did not change exit codes or assertion results,
but its lifetime/resource implications remain unverified. Do not suppress or
describe it as proven harmless. Evidence:
[build log](evidence/wails-build.log), [binary modules](evidence/wails-binary-modules.log),
[process exits](evidence/wails-run.json), [stage 1 stderr](evidence/wails-stage1.stderr.log),
[stage 2 stderr](evidence/wails-stage2.stderr.log).

## 3. Go screenshot, input and DPAPI

Evidence: [native.json](evidence/native.json), [native runner log](evidence/native-run.log).

- A dedicated Go thread selected PER_MONITOR_AWARE_V2. Win32/GDI captured the
  visible fixture's **900×560 physical pixel** rectangle. GetDIBits returned data,
  nonblack pixels were observed and PNG encode/decode dimensions matched in memory.
  No full-desktop image, raw image file or actual upload was created.
- A correctly sized **40-byte x64 INPUT** structure sent absolute virtual-desktop
  movement and left click only after fixture PID/child/foreground/hit-test guards.
  The button's changed label verified the click; the Edit readback verified
  `DeskPilot验证` entered by Unicode SendInput. No clipboard was used.
- Current-user DPAPI protected/unprotected a dummy key; readback matched and wrong
  entropy was rejected. No real API key was read or logged.

Two implementation findings were corrected and retested. Hidden child startup
left the fixture invisible, so the hit-test guard correctly aborted before input
to the underlying user window. Explicitly showing only the identified fixture
fixed the cause. `GetWindowText` cannot read another process's Edit value; the
probe switched to bounded WM_GETTEXT on its known control, as [Microsoft documents](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getwindowtextw).
Two vet warnings in the direct COM pointer probe were fixed using typed native
pointers; the final vet run is clean.

Limits: only move/click/simple Unicode were tested. Double/right click, drag,
scroll, key chords, IME/emoji, held modifiers, protected/HDR/minimized/GPU capture,
memory leaks and performance are unverified. Screenshot correctness is limited to
fixture dimensions/nonblack data and PNG roundtrip, not a complete pixel oracle.

## 4. UI Automation

**PASS for controlled patterns; PARTIAL for application coverage and Go traversal.**

Direct Go initialized COM MTA, created IUIAutomation and released it on a locked
OS thread. It did **not** exercise a complete native Go COM tree/event binding.
A .NET Framework C# helper on a separate MTA worker successfully found the fixture
tree (12 descendants), read Button type/properties and Text, set/read Value,
invoked a Button with changed-state verification and selected/read a ListBox item.
The parent enforces process deadlines; no new .NET SDK was installed.

Additional read-only discovery opened fresh marked windows on the workspace or
fixture text, bounded traversal to depth 4/150 nodes, retained only counts/pattern
availability, and closed those windows. No real-app controls were invoked.

| Application | Controlled result | Interpretation |
|---|---|---|
| Notepad | 1 window, 7 nodes, 3 Buttons/Invoke patterns, no Edit at this depth | Window/chrome reachable; document editing not validated |
| Explorer | 1 workspace window, 53 nodes, 19 Buttons, 2 Edits, 1 Text; 26 Invoke/8 Value/5 Selection patterns | Structured discovery works; operations not exercised |
| Windows Terminal | 1 marked NoProfile sleep tab, 16 nodes, 3 Buttons, 1 Text; 4 Invoke/2 Selection patterns | Window discovery works; terminal text/command operation not validated |
| VS Code | No running process; neither standard per-user nor machine executable path found; `code` absent from PATH | Not tested; portable/custom installations were not searched |

Evidence: [fixture UIA](evidence/uia.json), [application UIA](evidence/uia-apps.json).
An earlier existing Explorer shell root exposed zero immediate children; the
controlled folder window test resolved that observation without assuming UIA
itself was broken. Depth-limited snapshots cannot prove complete app support.

Recommendation: retain the Go computer abstraction; compare direct COM and an
isolated helper during Phase 2. Prefer helper isolation if provider hangs or
lifetime problems affect Go runtime reliability. One successful helper run does
not establish shipping stability, IPC authentication, resource bounds or an
approved .NET deployment policy. See architecture/threading and security gates.

## 5. DPI and multi-monitor conclusion

**PARTIAL.** EnumDisplayMonitors and virtual metrics agreed on one 2880×1800 display.
GetScaleFactorForMonitor returned **180%**. The DPI-unaware WinForms fixture reports
96 DPI; its 450×280 logical form was captured at 900×560 physical pixels. These
values are awareness/API dependent; do not equate fixture DPI, monitor scale and
one universal conversion factor. The physically addressed click passed on this
actual configuration.

The pure mapping tests passed negative-origin/crop geometry and absolute-input
extrema, rejecting nonfinite/out-of-bounds points. They are synthetic tests, not
proof of a second monitor. Mixed DPI, hot-plug, monitor movement and topology
changes remain hardware acceptance gates. Canonical signed physical coordinates,
per-observation crop transforms, PMv2 and stale-observation rejection are specified
in ARCHITECTURE; no system scaling settings were changed.

## 6. Provider, i18n and security design conclusions

**Provider:** use neutral text/image/tool/result types with separate Chat
Completions and Responses adapters. Capabilities are tri-state, keyed by provider,
model, base URL and revision, and carry evidence timestamps. Unknown vision/tools
fail closed. Seven Go tests passed mock conformance, cancellation, vision gating,
offline JSON shape, strict actions and coordinate cases. No live endpoint,
streaming, tool-call response parser, pricing or real model capability was tested.
No vendor SDK/runtime structure is exposed to the proposed Agent interface.

**i18n:** Vue I18n global Composition API supports immediate en-US/zh-CN switching.
Three Node tests, TypeScript check and Vite build passed. Actual Wails tests
proved default English, Chinese DOM/ARIA/title/tooltip/status text, missing-key
English fallback, English return, fixture width checks and SQLite restoration on
next process. A synthetic context object remained unchanged; real task context,
native menus, notifications/dialogs, long pseudo-locales and screen readers remain
product acceptance work. UI and Agent response languages are independently designed.
Evidence: [frontend tests](evidence/frontend-tests.log),
[stage 1](evidence/wails-stage1.json), [stage 2](evidence/wails-stage2.json).

**Key storage:** current-user DPAPI blobs in a per-user vault with ACLs and key
references; decrypt in Go only immediately before a permitted request. Dummy-key
roundtrip is proven. Different-account/profile tests, ACL enforcement and key
redaction across real error paths remain unverified.

**Agent safety:** L0 observe / L1 ordinary / L2 scoped write / L3 high risk. Mandatory
independent dispatch and action-bound approval, directory grants, shell preview,
upload consent, privacy controls, injection resistance, pause/cancel/stop and UAC
boundaries are designed in SECURITY. They are not implemented by the fixture.

## 7. Dependencies and licenses

This is an engineering inventory, not a completed redistribution audit or project
license choice. Runtime and development-tool obligations must be separated.

| Dependency | Tested version / status | License/source and assessment |
|---|---|---|
| Go | 1.26.10 local SDK; native/Wails compile | BSD-3-Clause; [Go LICENSE](https://go.dev/LICENSE) |
| Wails | v3.1.0 CLI + host runtime | MIT; [pinned LICENSE](https://github.com/wailsapp/wails/blob/v3.1.0/LICENSE), local module inspected |
| Vue / Vue I18n | 3.5.45 / 11.4.18; Node/build/WebView pass | MIT, installed package metadata/lock |
| TypeScript | 7.0.2; typecheck pass | Apache-2.0, installed package metadata/lock; build tool |
| Vite | 8.3.7; bundle pass | MIT, installed package metadata/lock; build tool |
| modernc.org/sqlite | 1.44.3; real settings persistence, CGO=0 | BSD-3-Clause wrapper/port; downloaded LICENSE inspected; full transitive notices still needed |
| SQLite engine | Used through modernc | [Public domain](https://www.sqlite.org/copyright.html); do not confuse engine license with Go wrapper/dependencies |
| go-ole | 1.3.0, Wails runtime binary dependency | MIT; local LICENSE inspected; not evidence of a validated direct UIA binding |
| x/sys | 0.48.0, Wails runtime dependency | BSD-3-Clause; Go module license inventory before release |
| WebView2 | 157.0.4341.42 preinstalled | Microsoft runtime terms, not MIT by association with Wails; review installer/redistribution route before release |
| Windows/.NET UIA | System .NET Framework assemblies | Existing Windows components used for a probe; helper runtime/deployment decision remains open |
| RobotGo | Not installed or compiled | Current [upstream license](https://github.com/go-vgo/robotgo/blob/master/LICENSE) is Apache-2.0; native/CGO cost not justified by successful Win32 path |
| Node/npm/Git | Existing tools, versions above | Toolchain inventory; no bundling decision or global upgrade made |

[npm license/integrity inventory](evidence/npm-licenses.json) captures installed
package metadata, not an independent license audit. `package-lock.json`, `go.sum`,
[module verification](evidence/go-mod-verify.log) and binary module info are retained.
Wails CLI downloads more build-only packages than the runtime binary links; do
not treat its entire module graph as a shipped application SBOM. No vulnerability
scan, complete transitive license audit, signing or release approval is claimed.

## 8. Tests and remaining issues

The [final verification summary](evidence/final-verification.json) records the
fresh test/build results, both successful process exits and zero remaining probe
processes. All seven documents exist and their local Markdown links resolve.

| Verification | Result | Evidence |
|---|---|---|
| Official SDK archive checksum | PASS | go-download.json plus validated download command |
| Wails CLI compile/version/doctor | PASS, v3.1.0, doctor exit 0 | wails-install.log / wails-doctor.log |
| Native Go compile + input/capture/DPAPI/COM | PASS, guarded fixture | native.json / native-run.log |
| UIA fixture + scoped real-app discovery | PASS within stated scope | uia.json / uia-apps.json |
| Go contracts, fresh noncached run | 7/7 PASS | contracts.log; initial failures in contracts-red.log |
| go vet | PASS after COM pointer correction | go-vet.log (empty means no diagnostics) |
| Vue locale/SSR tests | 3/3 PASS | frontend-tests.log |
| TypeScript / Vite | PASS | frontend-tests.log |
| Go module checksum verification | PASS, all modules verified | go-mod-verify.log |
| Wails actual runtime/restart | 2 processes × 12 checks PASS, exit 0 | wails-run.json / stage assertion JSON |
| Shutdown lifecycle | OPEN: class-unregister 1412 diagnostic | both stage stderr logs |
| Windows 10 / mixed-DPI / comprehensive input/UIA | NOT RUN / PARTIAL | matrix in TEST-STRATEGY |
| Real model/security pipeline/stop/ACL/installer | NOT IMPLEMENTED OR NOT RUN | intentional phase boundary |

Known issues are scoped conditions, not hidden passes: Wails shutdown diagnostics;
Notepad editing region and Terminal text semantics; portable VS Code availability;
Go direct COM traversal/event lifetime; authenticated helper IPC/soak; multi-screen
hardware; full i18n surfaces; DPAPI account/ACL tests; production permission engine,
global stop and process-tree termination; signed redistribution and complete notices.

## 9. Deliverables and next decision

The seven requested documents are [PRD](PRD.md), [ARCHITECTURE](ARCHITECTURE.md),
[SECURITY](SECURITY.md), [I18N](I18N.md), [ROADMAP](ROADMAP.md),
[TEST-STRATEGY](TEST-STRATEGY.md) and this report. Supporting artifacts: English
README with [Chinese entry](README.zh-CN.md), isolated reproducible probes and
scoped evidence logs/JSON. All changes exist to support those Phase 0 conclusions;
no production module tree, billing or full app was added.

Suggested next approval: the single Mock Provider / scoped file.create /
independent byte-verification slice in ROADMAP, with cancellation and bilingual UI.
Review and explicitly authorize Phase 1 before implementation. **Phase 0 stops here.**
