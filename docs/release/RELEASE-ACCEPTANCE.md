# Asimi 0.2.1 acceptance

[English](RELEASE-ACCEPTANCE.md) · [简体中文 archive](RELEASE-ACCEPTANCE.zh-CN.md)

The original native acceptance environment was Windows 11 Home 25H2 x64, build 26200, with WebView2 157.0.4341.46. Each run used isolated workspaces and profiles.

## Current naming regression

Current builds use Asimi.exe, AsimiControlHost.exe and AsimiUIAWorker.exe. Ordinary-launch checks assert the Asimi window and PE identity, icons, independent working directory, default Asimi profile, normal shutdown and exclusive reopening of asimi.db.

Go migration regressions cover legacy database conversion, preservation of an existing current database, an active legacy lock, WAL data and corrupt input. English and Chinese locale checks preserve their keys and interpolation. Build and archive checks verify the executable identity, member list and checksums.

The commit and package summary identify checks completed for a particular build. [Current release report](RELEASE-REPORT.md) · [Package summary](evidence/package.json)

## Historical candidate

| Area | Recorded coverage |
| --- | --- |
| Build | Go tests, vet, module verification, frontend checks and Windows build |
| Interface | First run, language switching, offline mode, settings and restart |
| Normal startup | Two cycles, window and executable icons, profile path and database close |
| Upgrades | Three copied profiles, schema 6, retained settings, DPAPI ciphertext and existing records |
| Windows regression | Nine stages and 193 assertions |
| Loopback Agent | Two stages and 34 assertions, real fixture edits, readback and targeted Go tests |
| Helpers | Protocol and target checks, SetValue, Invoke, independent readback and pipe closure |
| Final archive | Recorded member and checksum validation |
| External services | Zero external model calls during that candidate's Asimi acceptance |

The historical candidate completed 278 native assertions in total. Historical evidence applies to its original file hashes. Earlier two-hour stability and real-provider reports are linked in the archive. [Historical final checks](evidence/final-checks.json) · [Earlier acceptance](../0.2.1/ACCEPTANCE-REPORT.md)

## Manual scope

Real desktop visual analysis, a user-opened blank Notepad and other Windows versions need acceptance on the target desktop. [Known limitations](KNOWN-LIMITATIONS.md)
