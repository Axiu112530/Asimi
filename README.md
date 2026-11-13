# Asimi

[English](README.md) · [简体中文](README.zh-CN.md)

Asimi is a local-first AI desktop assistant for everyday questions, writing, image analysis and help with tasks on your computer.

Use **Settings → General → Interface mode** to open developer tools for projects, code review and test runs.

**Version 0.2.1 is a trial release candidate for Windows x64.** Native acceptance has been performed on Windows 11 25H2 x64. Other Windows versions require validation on the target machine.

[Installation](docs/release/INSTALLATION-GUIDE.md) · [User guide](docs/release/USER-GUIDE.md) · [Known limitations](docs/release/KNOWN-LIMITATIONS.md) · [Changelog](CHANGELOG.md) · [MIT License](LICENSE)

## Features

| Capability | Workflow |
| --- | --- |
| AI conversations | Connect a built-in provider with your own API key or configure a compatible endpoint. Stream replies and keep conversation history locally. |
| Project understanding | Select a workspace and inspect its structure, relevant source files, Git status and available runtimes. |
| Agent tasks | Describe a goal, review the context being sent to the model, then inspect the proposed plan, file diffs and commands. |
| Approved execution | Approve file changes and commands individually. Review actual execution, file readback, test output and recovery status. |
| Screenshot analysis | Select a display or region, inspect the preview, cover sensitive content and separately confirm transmission to an image-capable model. |
| Computer interaction | Inspect supported targets, review the exact action and approve it. Model-assisted control currently has its verified scope in the dedicated test window. |
| Local configuration | Keep settings, conversations, task history and audit records on your machine. Use English or Simplified Chinese and switch themes. |

The task timeline records proposals, approvals, execution and verification. Use **Stop all** in the top bar to cancel active tasks.

## Requirements

| Component | Requirement |
| --- | --- |
| Operating system | Windows x64, with native acceptance recorded on Windows 11 25H2 |
| Interface runtime | Microsoft Edge WebView2 Evergreen Runtime |
| Computer helpers | Windows .NET Framework 4.x |
| Permissions | Run under a standard Windows user account |
| AI service | Your own provider account, API key and available service credit |

Portable builds contain the application and its helpers. Source builds additionally require Go, Node.js, npm and the Wails CLI.

## Install and start

1. Download `Asimi-0.2.1-Setup-x64.exe` and its matching SHA256 file from the same release.
2. Verify the checksum, open the installer, and choose a language and destination. The default is `%LOCALAPPDATA%\Programs\Asimi`.
3. Open **Asimi** from Start after installation. The installer can also create a desktop shortcut.
4. Connect a model on first launch or select **Set up later** to use the local features.

The installer keeps program files and user data in separate directories. Uninstalling preserves the profile in `%LOCALAPPDATA%\Asimi`. [Releases](https://github.com/Axiu112530/Asimi/releases) · [Installation guide](docs/release/INSTALLATION-GUIDE.md)

## Connect an AI provider

Choose Kimi, GLM, DeepSeek, Qwen, Tencent Hy, OpenAI, Claude, or Gemini in first-run setup or **AI providers**. Enter your API key and select **Connect and save**. The test sends a short text request and may consume provider credit. A successful check saves the key with Windows DPAPI and selects the default model.

Use **Chat settings** to switch models. Image attachments and screenshot analysis require a model with image input. Asimi currently accepts still images. For region-specific or other compatible endpoints, use **AI providers → Advanced provider settings**.

Provider charges are billed to your service account. Available models, pricing and data-retention rules are governed by that provider. Local project browsing and history are available before you connect a model. [Privacy](PRIVACY.md)

## Work on a project

1. Select a workspace and confirm its path, Git status and detected runtimes.
2. Create an Agent task, such as “Explain this module and propose a small fix.”
3. Review the selected source files and test-output preview before confirming transmission.
4. Inspect each file diff, command, working directory and network scope before approving the corresponding action.
5. Read the execution, file-readback and test results before judging whether the task is complete.

Start with a disposable example project while learning the approval flow. Existing local Git changes appear in the review. If an action has an uncertain result, inspect the actual file or target window before preparing a follow-up operation. [User guide](docs/release/USER-GUIDE.md)

## Local data and privacy

The default profile directory is **`%LOCALAPPDATA%\Asimi`**. Its main database is **`asimi.db`**, and its process lock is **`asimi.lock`**. Check **Settings → About** for the actual profile location.

| Data | Storage or transmission |
| --- | --- |
| Settings, conversations, tasks, audit records and imported attachments | Stored in the local profile |
| Saved API keys | Protected using the current Windows account's DPAPI |
| AI conversations | The current prompt and relevant history are sent to the selected provider |
| Agent context | Selected project source and relevant test output are sent after preview and confirmation |
| Screenshots | Sent after preview, redaction and separate confirmation |

Other local content can contain readable private data. Close all instances before backing up a profile. Moving it to another Windows account or computer may require entering API keys again.

For an older profile, follow the upgrade guide to back up and copy it into the new directory. When a selected profile contains only the old database filename, Asimi checks the legacy process lock, verifies and checkpoints SQLite, and renames the database to `asimi.db`. An existing `asimi.db` takes precedence. [Privacy notice](PRIVACY.md) · [Upgrade guide](docs/release/UPGRADE-GUIDE.md)

## Build from source

### Prepare the toolchain

Build on Windows x64. The validated toolchain uses **Go 1.26.10**, **Node.js 24.x** and **Wails 3.1.0**. Frontend dependency versions are pinned in `frontend/package-lock.json`.

1. Install Git, Node.js and npm.
2. Download `go1.26.10.windows-amd64.zip` from the official Go downloads page and verify its published SHA256. Extract the archive's `go` directory into `.phase0-tools/go` inside your checkout.
3. Ensure WebView2 and Windows .NET Framework 4.x are available. The build script uses `Framework64/v4.0.30319/csc.exe` to compile the helpers. [Go downloads](https://go.dev/dl/) · [Wails installation](https://v3.wails.io/getting-started/installation/)

Use a new PowerShell window. The environment script selects tool and cache directories for that process.

```powershell
git clone https://github.com/Axiu112530/Asimi.git
Set-Location Asimi

# Extract Go into .phase0-tools/go before this step.
. .\validation\phase0\env.ps1
go version

go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
go mod download
go mod verify

Push-Location frontend
npm.cmd ci --ignore-scripts --no-audit --no-fund
Pop-Location

.\scripts\build.ps1
```

The complete output is in `build/bin/asimi-0.2.1/`. Launch its `Asimi.exe`, or use the run script.

```powershell
.\scripts\run.ps1
```

For a separate development profile, supply a local data directory.

```powershell
.\scripts\run.ps1 -DataDir .\build\bin\asimi-dev-profile
```

### Verify and package

After preparing the toolchain, installing frontend dependencies and building once, run the relevant checks.

```powershell
go test ./... -count=1
go vet ./...
go mod verify
node .\validation\phase1\check-i18n.mjs
node .\validation\v021\check-locales.mjs

Push-Location frontend
npm.cmd run typecheck
npm.cmd run build
Pop-Location

.\scripts\package-asimi.ps1
```

The package includes the application license, third-party notices, installation and user guides, privacy notice and per-file checksums. The script also generates a separate ZIP SHA256 file. Native computer-action acceptance requires an interactive Windows desktop. [Acceptance scope](docs/release/RELEASE-ACCEPTANCE.md)

## Repository layout

| Path | Purpose | Version-control handling |
| --- | --- | --- |
| `main.go`, `internal/` | Desktop entry point, AI, persistence and execution logic | Commit source |
| `frontend/` | Vue 3 and TypeScript interface with English and Chinese resources | Commit source and lockfile |
| `build/windows/` | Windows icons, version metadata and manifest | Commit build inputs |
| `scripts/`, `validation/` | Reproducible build, packaging and test inputs | Commit scripts and fixtures |
| `docs/` | Guides, architecture, licensing and reviewed acceptance summaries | Commit reviewed documents |
| `.local-validation/` and earlier stage profiles | Isolated test state | Ignored |
| `.phase0-tools/`, `.phase0-cache/` | Local tools and caches | Ignored |
| `build/bin/`, `frontend/dist/`, `frontend/node_modules/` | Executables, archives, bundles and dependencies | Ignored |

Keep personal profiles, databases, API keys and raw diagnostic records locally. Close related processes before removing disposable fixtures. Removing tools or caches requires preparing them again for the next build. Back up your everyday application profile before clearing personal history. [Contributing](CONTRIBUTING.md)

## Current limitations

- This release candidate has Windows verification for its recorded environment.
- Real-desktop visual analysis and blank Notepad scenarios require user-initiated acceptance on the target machine.
- Additional providers and custom models need their own connection and capability checks.
- Operations can require recovery or renewed approval. Inspect the task timeline and actual target state. [Known limitations](docs/release/KNOWN-LIMITATIONS.md)

## Issues, contributions and repository access

Report reproducible problems and feature requests through Issues. Remove credentials, private source, personal paths and sensitive screenshot content before posting. Use the private reporting process for vulnerabilities. [Issues](https://github.com/Axiu112530/Asimi/issues) · [Security reporting](SECURITY.md)

Fork the repository and open a Pull Request for a proposed change. Maintainers review and merge contributions and manage direct write access. [Contribution guide](CONTRIBUTING.md)

## License

Asimi source code and project documentation use the **MIT License**. The copyright notice is in the root LICENSE file.

MIT permits personal and commercial use, copying, modification and redistribution. Copies or substantial portions must retain the copyright and permission notices. Its AS IS warranty and liability provisions apply. MIT itself does not require publication of all modified source. Dependencies retain their own licenses. [MIT text](LICENSE) · [License guide](docs/release/LICENSE-DECISION.md) · [Third-party notices](docs/release/THIRD-PARTY-NOTICES.md)

Model-provider accounts, charges and data handling follow the provider's terms. GitHub repository write permissions are managed independently.

## Documentation

- [Installation](docs/release/INSTALLATION-GUIDE.md), [usage](docs/release/USER-GUIDE.md) and [upgrades](docs/release/UPGRADE-GUIDE.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Release report](docs/release/RELEASE-REPORT.md) and [acceptance](docs/release/RELEASE-ACCEPTANCE.md)
- [Chinese README](README.zh-CN.md) and [historical Chinese documentation index](docs/README.zh-CN.md)
