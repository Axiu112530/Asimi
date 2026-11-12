# Phase 0 validation ledger

This is an isolated feasibility spike, not product code. The supplied DeskPilot
brief authorizes these probes and the seven design documents. No Phase 1 work,
Git commits, global installs, paid inference, or changes to system configuration.

## Checks

- [x] Inspect workspace and available tool commands (empty; no Git/CodeGraph).
- [x] Check network before downloads (sandbox proxy socket denied; an approved
  execution outside the sandbox reached official registries).
- [x] Download checksum-verified Go 1.26.10 into `.phase0-tools`.
- [x] Pin and compile Wails v3.1.0; run doctor and a time-bounded WebView2 host.
- [x] Exercise Go Win32 capture/input/DPI and DPAPI against a disposable fixture.
- [x] Probe UIA patterns on that fixture; inspect only scoped target apps.
- [x] Test provider-neutral mock contracts, cancellation and coordinate mapping.
- [x] Verify Vue 3/TypeScript/Vue I18n resources, fallback and persistence.
- [x] Write all seven requested docs with evidence and remaining gates.

## Results and decisions

All planned probe categories ran. Conditional Go is documented, with no approval
to start Phase 1. The final deliverable audit checks document links and evidence.
Hidden fixture startup and cross-process GetWindowText caused guarded failures;
they were diagnosed and corrected without weakening the pointer/focus checks.
Go vet identified two COM integer/pointer conversions; typed COM pointers removed
the warnings and the runtime test passed again. Full Go UIA traversal was not
implemented; helper pattern execution and direct Go COM construction are distinct.
Wails runtime tests pass but both exits log class unregister error 1412; retain it
as an unresolved lifecycle condition. One actual display is not multi-screen proof.
No project commits or global configuration changes were made.

## Probe boundaries

Capture pixels in memory only; retain dimensions/status, never desktop images.
Input is restricted to a fixture HWND with a matching PID, class and title.
Stop on lost focus. No operations on unrelated user windows. UIA application
checks are read-only and retain control-type counts, not titles or content.
Use a C#/.NET Framework helper to evaluate UIA isolation without installing .NET.
Build artifacts/caches are project-local. Dependencies remain candidates until
build/runtime evidence exists; mixed-DPI and absent apps remain explicitly open.

## Design alternatives to assess

1. Go Win32 + isolated UIA helper (recommended if the fixture passes): no RobotGo
   CGO dependency and UIA hangs can be isolated from the Go core.
2. Go Win32 + direct Go COM: fewer processes, greater thread/lifetime burden.
3. RobotGo for input/capture: broad API, additional native toolchain/dependencies;
   evaluate metadata only unless Win32 proves insufficient.

The final architecture documents are proposals for Phase 1 approval, not an
authorization to implement the product. English docs are primary; README links
to a Chinese entry.
