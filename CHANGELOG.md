# Changelog

## Asimi 0.3.1

- Distinguish the user's messages from Asimi's replies, including author labels and clearer styling.
- Show how many relevant local memories and skills were referenced in completed replies.
- Refine learning and skills layouts and add the Asimi logo and real Windows screenshots to both READMEs.

## Asimi 0.3.0

- Run local development environment checks from chat and open reviewed Windows actions from the composer.
- Ask for concise screen analysis and show a readable summary when a provider response is truncated.
- Start each new conversation with an empty draft and add it to history after the first message. Hide older empty conversations from history.
- Enable memory, learning, and automatic skill creation by default. Describe Asimi's role and available desktop capabilities accurately in chat.
- Apply the selected light or dark appearance to the sidebar and Windows title bar. Keep a single visible logo in the app and preserve the taskbar icon.

## Asimi 0.2.1-rc.8

- Unified the desktop app, Windows installer and favicon under the silver-blue Asimi mark.
- Updated the light and dark desktop themes to use a muted steel-blue accent.

## Asimi 0.2.1

- Renamed the default profile to %LOCALAPPDATA%\Asimi, with asimi.db and asimi.lock.
- Renamed the Go module, frontend package, helper executables, event channel and setup paths to Asimi naming.
- Added guarded legacy database filename migration and retained legacy recovery and provider metadata reads.
- Made English the default documentation language with separate Simplified Chinese documents.

- Expanded the Chinese README with installation, provider setup, project tasks, local data, source builds and troubleshooting links.
- Clarified MIT permissions, redistribution conditions, third-party terms and repository contribution permissions.
- Standardized current product labels, Windows metadata and portable archive names as Asimi.
- Updated the locale checker to include modular setup, project and computer-action translations.

- Renamed the product to Asimi and the Windows portable launcher to `Asimi.exe`.
- Updated installation, upgrade, privacy and release documentation.
- Preserved compatibility with DeskPilot 0.2 and 0.2.1 data, including the existing default profile path.

[Release report](docs/release/RELEASE-REPORT.md)

## DeskPilot 0.2.1 archive

- Three-step setup with a verified DeepSeek connection, DPAPI credential storage and an offline path.
- Effective settings with provenance, lowerable limits and confirmed reset.
- Automatic project and runtime inspection, read-only Git context and dirty-file approval warnings.
- Task-linked suggestions for the dedicated computer test host, with separate approvals and verification.
- Windows x64 portable packaging, quick start and upgrade guides.

[Previous release documentation](docs/0.2/START-HERE.md)
