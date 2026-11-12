# DeskPilot AI — Test Strategy

Status: product test strategy with preserved Phase 0 inventory and the executed
Phase 1 focused checks in the final section.
Run tests appropriate to changed behavior; do not scan the whole computer or run
unrelated suites. Test records distinguish passed, failed, partial and not run.

## Test layers

| Layer | Cases and oracle | Environment |
|---|---|---|
| Go unit/contract | Provider-neutral DTOs, unknown capability deny, strict tool schemas, finite coordinates, budgets/state transitions | Mock-only, no credentials/network |
| Provider adapter contracts | Recorded sanitized JSON/SSE, tool results, partial streams, errors, 429/timeout, vision/structured output unsupported | Local fake server or offline fixtures, independent protocol cases |
| Security unit/integration | Denial produces no side effects; exact grants; stale/replayed approvals; file/junction races; endpoint redirects | Dedicated temporary workspace, no real secrets |
| Storage | Migrations, transaction rollback, language restore, task checkpoint, busy/disk-full/corruption | Disposable SQLite databases |
| Windows native | GDI capture/readback, input observed in own fixture, foreground/hit test, DPI mapping, COM lifetime, DPAPI | Windows interactive non-admin session |
| UIA | Button/Edit/Text properties, Invoke/Value/Selection postconditions, stale elements, provider hang/kill/restart | Controlled fixture then selected target apps |
| Vue/i18n | English default, live zh/en, key/parameter parity, fallback, semantic DOM/ARIA, config failures | Node + actual WebView2 |
| Task acceptance | Objective result evidence, denied/cancelled/failed/inconclusive states | Mock first; approved real provider later |
| Release | Installer, signatures/notices, standard-user ACLs, upgrade/uninstall, rollback | Clean Windows 10/11 VMs and physical display matrix |

Use a model only for complementary semantic interpretation; deterministic checks
and independent observations own completion. Test a deliberately false model
success against a failing postcondition. No live cost tests run without explicit
endpoint/model/budget/data approval. Provider declarations and `/models` output
must not be used as vision/tool test oracles.

## Phase 0 commands and evidence

Run from repository root in a dedicated PowerShell process; `env.ps1` changes
only process environment and uses project-local Go/cache directories.

```powershell
. ./validation/phase0/env.ps1
Push-Location validation/phase0
go test -count=1 -v .
go vet .
Pop-Location
Push-Location validation/phase0/frontend
npm.cmd test
npm.cmd run typecheck
npm.cmd run build
Pop-Location
./validation/phase0/run-native.ps1 -IncludeApps
./validation/phase0/run-wails.ps1
```

Native integration tests briefly show their own disposable window and move the
pointer. A foreground/target mismatch aborts before click/text. Screenshots are
PNG-encoded/decoded in memory; persist only dimensions/size/nonblack status, not
pixel data. Stop interacting with that fixture during the short test. The UIA
helper is bounded to 12s and killed by its parent after 15s if necessary. No input
is sent to unrelated apps. The native test is a feasibility check rather than a
production input race guarantee.

Seven Go test functions cover provider neutral mock, cancellation, tri-state
vision gate, distinct wire shapes, malformed/unknown/trailing actions, crop mapping
with negative monitor origin and bad coordinates. Initial red evidence exists;
the always-reject stub already passed the invalid-coordinate test, so the positive
mapping test is the independent check against blanket rejection. Adapter image
bytes are dummy shape data, not a live valid-image API conformance test.

Frontend tests cover resource parity, SSR translation/reactivity/fallback and
nonempty fixture messages. Native Wails assertions cover live DOM, document title,
ARIA/tooltip/status text, missing-key fallback, both-language document width,
synthetic context retention and two-process SQLite persistence. This is not a
full product visual/accessibility test or a real notification/dialog test.

Logs and JSON in `docs/evidence/` record the executed subset. Build and exit
statuses must be read alongside self-reported fixture checks. Keep `go.sum` and
`package-lock.json`; use `go mod verify`/`npm ci` for future reproducibility.

## Required Windows matrix and known gaps

| Axis | Required cases | Phase 0 limit |
|---|---|---|
| OS | Windows 10 and Windows 11 x64, standard user | One host/build only; registry ProductName is not OS support proof |
| Displays | 1/2/3 monitors, negative origin, mixed 100/125/150/200%, hot plug and window move | One physical display; negative origin only synthetic |
| Surfaces | Classic/WinUI/Electron, minimized/occluded, HDR, protected content | Visible controlled WinForms fixture; no protected/HDR claim |
| Inputs | All action kinds, Unicode/emoji/IME, held modifiers, user interference | Click/move and simple Unicode; other actions/IME remain |
| UIA targets | Notepad, Explorer, Terminal, VS Code if installed | Report separates installed/running/reachable/pattern-tested |
| COM | MTA owner thread, balanced Release/events, cancellation and memory soak | Direct Go object create/release; managed helper patterns, no lifetime soak |
| DPAPI | Correct user, wrong entropy/corruption, different account/profile, ACLs | Same-user dummy key and wrong entropy only |

Do not infer unavailable from `processCount=0`, or infer UIA failure from a shell
root with zero children. A target with inaccessible controls needs a scoped
application-specific observation and documented fallback. Do not change system
scaling or accessibility settings simply to make a test pass.

## Security acceptance and stop behavior

For each mutating tool, inject denial/expiry/cancellation after planning and before
dispatch; filesystem/window/process state must remain unchanged. Use a fake clock
for expiry, synthetic observations for stale geometry, unique target IDs for HWND
reuse and an independent event latch for stop latency. Exercise unexpected disk
failure, partial child output and ambiguous external side effects without retries
that duplicate them. Global-stop latency and helper resource bounds are measured
release criteria; Phase 0 has not established a latency SLA.

Full CI, race testing, fuzzing, coverage targets and vulnerability/license SBOM
checks should be added to the product's actual packages after Phase 1 approval.
The CGO-disabled probe was not race-tested; do not silently switch global compilers
or claim a race pass. A self-reviewed spike is not an independent security audit.

## Phase 1 focused validation

Product checks are separate from the preserved Phase 0 suite:

```powershell
. ./validation/phase0/env.ps1
go test -count=1 -v ./internal/...
go vet ./...
go mod verify
node validation/phase1/check-i18n.mjs
./scripts/build.ps1
./scripts/accept-phase1.ps1 -SkipBuild
```

Use a project-local TEMP directory for file tests. This execution host's sandbox
denies required metadata handles on ancestor `C:/dir`; scoped tests therefore
ran outside that sandbox, under the same non-admin account. The application does
not request administrator privileges or weaken reparse checks for that host rule.

The focused suite has 30 top-level tests and 21 named subcases across app/provider/
tools, covering security invariants and the parser/state defects found during
review. The native driver uses business bindings plus DOM events **inside the
app's own WebView**. It does not invoke Win32 mouse/keyboard/screenshot controls.
Loopback CDP captures only that WebView's render in explicitly enabled acceptance
mode. Tests create a fresh isolated app profile, verify the intended bytes/hash
through an independent PowerShell file oracle, restart it, and compare all 27
preserved Phase 0 report/evidence hashes.

Functional run: 58 assertions in the first process and 10 restoration assertions
in the second, both exit 0. A separate fresh welcome preview has 3 assertions.
Literal-key/fallback checks, TypeScript, Vite, Go vet/module verification also pass.
Logs/results live under `docs/phase1-evidence`, including the still-reproducible
1412 teardown diagnostic. Peer review corrections were verified by targeted tests
and native acceptance. No broad UI regression, performance/load/race/fuzz suite,
cross-OS matrix, real provider call or desktop-control retest was performed.

## Phase 2A directed checks

Set `TEMP` and `TMP` for the test process to an existing project directory
under `.local-validation/phase2a`. Use the pinned local SDK and run the affected packages.

```powershell
. ./validation/phase0/env.ps1
go test -count=1 -v ./internal/app ./internal/tools
go vet . ./internal/app ./internal/tools ./validation/phase2a/lifecycleprobe ./validation/phase2a/lifecycledb
./scripts/build.ps1
./scripts/probe-phase2a-lifecycle.ps1 -Variant app-pending -Runs 3
./scripts/probe-phase2a-lifecycle.ps1 -Variant app-acceptance -Runs 3
```

Process fault fixtures exit abruptly at seven checkpoints, reopen the same
isolated profile and independently check file effects, approval and audit
history. SQLite tests inject a failed deferred-constraint commit, competing
writer, read-only file, page limit, incomplete database and interrupted migration.
The lifecycle harness records stdout/stderr and attributable process lifetimes
for both minimal Wails and full app variants. Pending-close uses the test WebView's
own Wails window-close action and verifies SQLite after process exit. The old
Phase 1 tests and evidence remain available. [Phase 2A acceptance](PHASE-2A-ACCEPTANCE.md)

## Phase 2B directed verification

Use the pinned project environment and a TEMP/TMP directory under `.local-validation/phase2b`.
Network tests use httptest on loopback only. They must never obtain a real API key
or call an external model. Managed hosts may require a scoped execution approval
for loopback sockets and the same non-admin ancestor handles as Phase 2A.

```powershell
. ./validation/phase0/env.ps1
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
go test -count=1 ./internal/ai ./internal/credential ./internal/app ./internal/tools ./internal/provider
go vet . ./internal/ai ./internal/credential ./internal/app ./internal/tools ./internal/provider
node validation/phase1/check-i18n.mjs
node validation/phase2b/check-compatibility.mjs
node frontend/src/ai-store.test.mjs
./scripts/build.ps1
./scripts/accept-phase2b.ps1 -SkipBuild -Visual
```

The new acceptance harness writes unique Phase 2B evidence directories, uses an
explicit local provider simulator and checks restart plus original Phase 1 stages
without overwriting their archived results. Its image test proves selected bytes
reach the adapter, not model image understanding. DPAPI tests use dummy values.
Different-user decryption and real-provider text/vision require manual user action.
Do not re-run old acceptance scripts into historical evidence directories.
The final automated run, build SHA-256 and live acceptance status are recorded
in [Phase 2B verification](phase2b-evidence/final-verification.json).

## Phase 2C directed verification

- Go coverage uses the existing app/ai/credential/provider/tools packages plus
  `internal/screen`. The screen tests use generated pixels for GDI handle
  checks and synthetic topology for negative coordinates and mixed DPI.
  Setting `DESKPILOT_SCREEN_NATIVE_TEST=1` also enumerates the local displays.
- Wails acceptance uses an explicit synthetic driver and a loopback HTTP
  fixture. It exercises the actual preview and consent UI without collecting
  desktop images. Its evidence contains measurements and outcomes only.
- `go run ./validation/phase2c/live -live` explicitly performs up to two real
  requests with `DEEPSEEK_API_KEY` and may incur charges. This CLI is separate
  from the automated suite. The image is generated in memory. A real desktop
  image is tested only through user capture and confirmation in the app.

See [Phase 2C acceptance](PHASE-2C-ACCEPTANCE.md) for commands and evidence.

## Phase 2D · 定向验证

Fake Driver 测试覆盖审批哈希、单次 CAS、并发审批、过期、拒绝、目标变化、输入限制、Stop、SQLite 执行意图失败和最终记录失败。子进程在意图提交、效果返回、验证返回、最终事务之后退出，重启核查 journal 及消费历史。

真实 Windows 验证仅针对专用测试 host，区分 worker 原生 smoke 与 Wails 端到端 Vue 审批。`scripts/accept-phase2d.ps1` 使用新的隔离目录和 evidence，核验生命周期顺序、SQLite 释放和旧证据 SHA256，并复跑 Phase 1/2B/2C 自有 fixture。Notepad 写入等待用户手动验收，未运行任意真实桌面输入。
## MVP 0.1

MVP 0.1 验收区分假驱动、回环协议、合成图像和原生 Windows。运行 `scripts/accept-mvp01.ps1 -Regression` 验证独立包。详见 [验收记录](MVP-0.1-ACCEPTANCE.md)。
