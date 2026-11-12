# Contributing to Asimi Community

Use GitHub issues for public bug reports and GitHub pull requests for proposed changes. Report vulnerabilities through the maintainer's private contact channel. [Security reporting](SECURITY.md)

Use Windows 11 x64 and the repository's pinned Go, Wails, and frontend dependencies. Use the release build and packaging commands documented in the acceptance report. Run Go and frontend regression tests before submitting changes. [Release acceptance](docs/release/RELEASE-ACCEPTANCE.md)

Keep model planning separate from permission checks and effects. For file, command, or computer changes, include tests for changed targets, invalid approvals, interruption, and independent verification. Use dedicated fixture projects and the test host. User desktop captures and Notepad edits require the interactive flow documented in the historical acceptance guide. [Computer acceptance archive](docs/0.2.1/COMPUTER-ACCEPTANCE.md)

Keep SQLite migrations additive. Preserve existing fixtures, reports, locale keys, and interpolation parameters. Record test evidence as mock, loopback, native, or external provider. Share redacted errors and diagnostics. Keep keys, project source, and screenshots out of public issue reports.

Commit source, reproducible build and test inputs, dependency lockfiles, and reviewed documentation. Keep local test profiles, databases, toolchains, caches, generated binaries, raw acceptance logs, and archives in the ignored workspace paths. Reviewed release summaries and the locale baselines used by tests are eligible for version control. If the maintainer approves a release, attach the portable ZIP and its SHA256 file to the GitHub release. Check the staged file list before committing.

The existing repository history includes early acceptance artifacts. Review their exposure before publishing the source history. [Release security review](docs/release/SECURITY-REVIEW.md)
