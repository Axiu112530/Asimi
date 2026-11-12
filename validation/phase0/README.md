# Disposable Phase 0 probes

These files test feasibility, not product functionality. Do not reuse their raw
syscall wrappers or fixture HTTP endpoints as a production security boundary.

## Toolchain preparation

The first run downloaded the official Go **1.26.10 windows-amd64 ZIP**, verified
the SHA-256 against official metadata (see `docs/evidence/go-download.json`) and
extracted it under `.phase0-tools/go`. No global installation or PATH change was
made. A fresh checkout needs the same verified archive from
[Go downloads](https://go.dev/dl/). Dot-source `env.ps1` in a dedicated PowerShell
to select project-local tools/caches. For network downloads, this machine needed
the existing system proxy applied to the child process; do not change user/machine
proxy settings. Network and interactive desktop access may require a host sandbox
exception. No administrator/UAC bypass is part of these probes.

```powershell
. ./validation/phase0/env.ps1
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
./.phase0-tools/bin/wails3.exe doctor
Push-Location validation/phase0/frontend
npm.cmd ci --ignore-scripts --no-audit --no-fund
npm.cmd test
npm.cmd run typecheck
npm.cmd run build
Pop-Location
Push-Location validation/phase0/wails
go mod download
go mod verify
Pop-Location
./validation/phase0/run-native.ps1
./validation/phase0/run-wails.ps1
```

`run-native.ps1 -IncludeApps` additionally opens Notepad on its own fixture text,
Explorer on this workspace and a marked Terminal tab running only a bounded
NoProfile sleep command. It reads control types/pattern availability only, and
closes only newly created marked windows. It does not invoke controls in these
apps. Standard VS Code paths are checked, without installing or scanning for it.

The native runner compiles Go and a .NET Framework fixture with the existing
Windows `csc.exe`. A temporary visible fixture is required for hit testing.
Input is guarded by HWND/PID/child membership, foreground and pointer hit tests;
images stay in memory. The fixture closes after the test. The UIA helper uses a
dedicated MTA worker with a process watchdog. Go COM creation/release is measured
separately from the managed helper's actual pattern operations.

`run-wails.ps1` compiles and launches two Wails processes against the same fresh
per-run SQLite database and project-local WebView profile, with a 20s internal
watchdog. It proves English default → live Chinese → persisted Chinese at next
launch → English. It writes its assertion JSON and process exit statuses under
`docs/evidence`. It does not call a model or run an Agent loop.

`go test -count=1 -v .` inside this directory tests the neutral mock/encoder/action/
coordinate contracts. Those encoders have no HTTP transport or credentials; dummy
image bytes only validate JSON shape. Do not claim real provider conformance.

Build outputs, transient SQLite/WebView state, toolchains and caches are ignored.
No cleanup deletion, Git initialization/commit or system settings change is
required. Logs may contain local build paths/machine diagnostics; inspect before
sharing publicly. No user screenshots or API keys are intentionally retained.
