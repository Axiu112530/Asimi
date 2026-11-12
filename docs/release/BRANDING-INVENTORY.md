# Asimi naming inventory

| Surface | Current naming |
| --- | --- |
| Application, windows, dialogs and localized interface | Asimi |
| Windows product metadata | Asimi, Asimi.exe, version 0.2.1 |
| Go module and imports | asimi and asimi/internal/... |
| Frontend package | asimi |
| Frontend setup files | provider-setup.ts, provider-setup.css and provider-setup locale resources |
| Default profile | %LOCALAPPDATA%\Asimi |
| Database and profile lock | asimi.db and asimi.lock |
| Native helpers | AsimiControlHost.exe and AsimiUIAWorker.exe with matching configurations and hash checks |
| Application update events | asimi:changed |
| New temporary and recovery files | .asimi- prefix |
| Portable output | build/bin/asimi-0.2.1 and Asimi-0.2.1-win-x64.zip |
| Default repository documentation | English with separately linked Simplified Chinese documents |

Legacy database and recovery-file names are recognized when reading older profiles and interrupted records. Historical reports identify their original artifacts. [Upgrade guide](UPGRADE-GUIDE.md) · [Acceptance](RELEASE-ACCEPTANCE.md)
