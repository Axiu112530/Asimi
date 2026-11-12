# Asimi privacy notice

[English](PRIVACY.md) · [简体中文](PRIVACY.zh-CN.md)

Asimi stores configuration and activity records locally by default. Configuring an external AI service enables requests to your selected service address. Review the outgoing content and the provider's data policies before sending it.

## Local storage

| Content | Storage and purpose |
| --- | --- |
| Language, theme, provider and model settings | Local configuration and database state |
| Conversations and messages | Local history for continuing conversations |
| Workspaces, tasks, approvals, execution, verification and audit records | Local records for progress and recovery |
| Imported attachments and runtime logs | Local files that may contain user content or diagnostics |
| Saved API keys | Protected with the current Windows account's DPAPI |

The default profile is `%LOCALAPPDATA%\Asimi`. The database is `asimi.db`, and the process lock is `asimi.lock`. Check **Settings → About** for the actual location, or use `--data-dir` for a separate profile.

DPAPI protects saved credentials. Conversations, attachments and logs can contain readable data accessible to processes or accounts with the corresponding filesystem permissions.

## Network transmission

| Operation | Content and destination |
| --- | --- |
| Provider connection test | A short prompt sent to the configured provider, potentially consuming service credit |
| Real AI conversation | The prompt, relevant history and selected attachments sent to the selected model service |
| Agent task | Previewed project source, goals and relevant test output sent after confirmation |
| Screenshot analysis | The selected display or region sent after preview, redaction and separate confirmation |

Offline exercises use local features. Real model requests contact the configured service. Verify the service operator and address when using a custom endpoint.

## Third-party services

Providers govern their pricing, capabilities, retention and data handling. Users manage their own API keys and service charges. Requests to a custom gateway also pass through that gateway.

Asimi depends on installed WebView2 and Windows runtimes. Applicable components and notices are documented with the release. [Third-party notices](docs/release/THIRD-PARTY-NOTICES.md)

## Backups, upgrades and deletion

Close all instances using a profile before backing it up. Restoring it under another Windows account or on another computer may require entering API keys again.

For an older profile, back up and copy its contents into the new location before opening it. Asimi checks the legacy process lock, SQLite integrity and checkpoint state before renaming the old database file. When both database names exist, the current `asimi.db` is used. [Upgrade guide](docs/release/UPGRADE-GUIDE.md)

Deleting a profile removes its settings, history, tasks, logs and attachments. Verify the actual path and backup before clearing it. Development fixtures and caches have their own locations under the source checkout. [Contributing](CONTRIBUTING.md)

## Feedback and security

Redact credentials, private code, personal paths, conversations and sensitive image content before sharing diagnostics. Report vulnerabilities privately with isolated test data. [Security reporting](SECURITY.md)
