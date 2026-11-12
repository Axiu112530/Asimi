# Contributing to Asimi

[English](CONTRIBUTING.md) · [简体中文](CONTRIBUTING.zh-CN.md)

Contributions can include reproducible bug reports, documentation, translations and code improvements. Source and documentation use the MIT License. Submit material you have the right to share under those terms. [MIT License](LICENSE)

## Report an issue

Include the Asimi version, Windows version and architecture, minimal reproduction steps, expected behavior and actual results. For AI-related issues, identify offline, loopback or external-provider operation and the relevant model and protocol. Redact logs, paths and screenshots before posting. [Issues](https://github.com/Axiu112530/Asimi/issues)

Report vulnerabilities privately using an isolated reproduction. [Security reporting](SECURITY.md)

## Propose a change

1. Fork the repository, clone your copy and create a working branch.
2. Keep the change focused and preserve applicable API, database and approval behavior.
3. Run relevant checks and document their results and any unverified scope.
4. Open a Pull Request against this repository for maintainer review.

Maintainers manage direct write access and decide which changes to merge.

## Development environment

Follow the README to prepare Windows x64, Go 1.26.10, Node.js 24.x, Wails 3.1.0 and the locked frontend dependencies. The environment script uses project-local tools and caches. [Source builds](README.md#build-from-source)

```powershell
. .\validation\phase0\env.ps1
go test ./... -count=1
go vet ./...
go mod verify
node .\validation\phase1\check-i18n.mjs
node .\validation\v021\check-locales.mjs

Push-Location frontend
npm.cmd run typecheck
npm.cmd run build
Pop-Location

git diff --check
```

Build the frontend before the first complete Go test run because the desktop entry point embeds `frontend/dist`. Use `scripts/build.ps1` and `scripts/package-asimi.ps1` for Windows builds and packaging. Documentation changes need content, relative-link and diff checks.

## Execution and persistence changes

- Preserve per-action approvals, target validation and independent verification.
- Cover changed targets, invalid approvals, interruption and recovery with relevant tests.
- Use disposable projects, isolated profiles and the dedicated test window. Identify mock, loopback, native and external-provider checks.
- Keep SQLite migrations upgradeable and maintain translation-key and interpolation consistency.
- New profiles use `%LOCALAPPDATA%\Asimi`, `asimi.db` and `asimi.lock`. Exercise legacy migration when changing persistence.
- Follow the interactive flow for desktop and Notepad acceptance. [Computer acceptance archive](docs/0.2.1/COMPUTER-ACCEPTANCE.md)

## Files to commit

Commit source, reproducible build and test inputs, lockfiles, Windows resources and reviewed documents. Inspect staged changes before committing.

```powershell
git diff --cached --name-status
git diff --cached --check
```

Local tools, caches, node_modules, compiled output, ZIP files, profiles, databases, raw logs and fixture archives are ignored. Keep keys and private project material locally. Reviewed acceptance summaries and test locale baselines follow the existing inclusion rules.

## Release checks

Record checks for the actual build, generate fresh checksums and confirm the archive contains LICENSE, third-party notices, privacy information and guides. Earlier reports apply to their recorded files and environments. [Acceptance](docs/release/RELEASE-ACCEPTANCE.md) · [Licensing](docs/release/LICENSE-DECISION.md)
