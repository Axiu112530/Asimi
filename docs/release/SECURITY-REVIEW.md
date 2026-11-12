# Asimi release security review

[English](SECURITY-REVIEW.md) · [简体中文 archive](SECURITY-REVIEW.zh-CN.md)

The product uses local permission checks, per-action approvals, target identity validation and independent verification. Providers receive reviewed model context and separately confirmed images. Saved credentials use Windows DPAPI.

## Naming and profile migration

The current module, event channel, helpers, database and process lock use Asimi naming. New profiles use %LOCALAPPDATA%\Asimi. Legacy database conversion checks the older process lock, SQLite integrity and WAL checkpoint before an atomic rename. An existing current database takes precedence. Legacy recovery-file names remain recognized so existing witnesses can be reconciled.

Hash-pinned helper binaries and their fixed runtime configurations are rebuilt together after a naming change. Frontend subscriptions use the same renamed event as the backend.

## Distribution and reporting

The source uses MIT. Portable archives include the application license, applicable third-party notices, privacy information and checksums. Raw logs and private profiles stay in ignored paths. Earlier acceptance summaries identify their original artifacts and environment. [Release report](RELEASE-REPORT.md) · [Known limitations](KNOWN-LIMITATIONS.md)

Report vulnerabilities privately with an isolated reproduction and redacted diagnostics. [Security reporting](../../SECURITY.md)
