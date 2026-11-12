# Asimi 0.2.1 release report

[English](RELEASE-REPORT.md) · [简体中文](RELEASE-REPORT.zh-CN.md)

## Product naming and documentation

The application, Go module, frontend package, native helpers, event channel and newly generated file names use Asimi. The default profile is %LOCALAPPDATA%\Asimi, with asimi.db and asimi.lock.

| Artifact | Location |
| --- | --- |
| Portable ZIP | build/bin/Asimi-0.2.1-win-x64.zip |
| Archive checksum | build/bin/Asimi-0.2.1-win-x64.zip.sha256 |
| Executable | build/bin/asimi-0.2.1/Asimi.exe |
| Per-file checksums | build/bin/asimi-0.2.1/SHA256SUMS.txt |

README, contribution, privacy and licensing documents default to English and link to separate Simplified Chinese versions. Source and documentation use MIT. Distribution includes LICENSE, third-party notices and user guides. [License](../../LICENSE) · [Licensing guide](LICENSE-DECISION.md)

## Verification

The naming update is checked through Go regression tests, locale and frontend checks, Windows builds, isolated normal launches and archive validation. The commit records the completed checks. Current archive and executable hashes are generated during packaging. [Package summary](evidence/package.json)

## Historical acceptance

The earlier candidate completed 278 native assertions, including three legacy-profile copies, loopback Agent tasks, helper checks and shutdown, plus two ordinary launch cycles. These records identify the original binaries and environment. Rebuilt binaries have their own hashes and checks. [Historical summary](evidence/final-checks.json) · [Acceptance scope](RELEASE-ACCEPTANCE.md)

Real-desktop visual analysis, blank Notepad and additional Windows versions require target-machine acceptance. Additional providers and custom models also require testing. [Known limitations](KNOWN-LIMITATIONS.md)

## Profile upgrades

Back up and copy older profile contents into the new location before opening it. Asimi checks the legacy lock, SQLite integrity and WAL checkpoint before converting an old database filename. An existing asimi.db takes precedence. Existing recovery-file prefixes remain recognizable. [Upgrade guide](UPGRADE-GUIDE.md)

Source control includes build inputs, test inputs, lockfiles and reviewed documents. Toolchains, caches, profiles, databases, raw logs and binary archives remain in ignored paths. [Naming inventory](BRANDING-INVENTORY.md) · [Cleanup inventory](CLEANUP-INVENTORY.md)
