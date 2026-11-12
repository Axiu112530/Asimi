# Upgrade to Asimi 0.2.1

The default profile is **`%LOCALAPPDATA%\Asimi`**, with database **`asimi.db`**. Earlier versions used `%LOCALAPPDATA%\DeskPilotAI` and `deskpilot.db`.

## Move an older default profile

1. In the old app, record the actual directory shown under Settings → About.
2. Close all instances of both versions and back up the entire profile to a private local location.
3. Copy its contents into `%LOCALAPPDATA%\Asimi` while the new directory is empty. Preserve attachments and the workspace as well as the database. Keep your backup.
4. Extract the new program separately and launch `Asimi.exe` under the same Windows account on the same computer.
5. When only `deskpilot.db` exists, Asimi checks the legacy lock, verifies SQLite integrity, checkpoints WAL data and renames the database to `asimi.db`.
6. Verify your provider, model, language, theme and history before resuming tasks.

An existing `asimi.db` takes precedence. If both names exist, Asimi uses the current database and leaves the old file available for recovery. Choose a separate empty profile when recovering another history.

## Custom profiles

Continue using an existing custom directory with the launch flag. Database filename migration also applies there.

```powershell
.\Asimi.exe --data-dir "C:\path\to\existing-profile"
```

DPAPI credentials can require re-entry after moving accounts or computers. File and action records retain their target identities, so inspect recovery status and prepare fresh approvals as needed. New temporary and recovery files use `.asimi-`, and the legacy prefix remains recognized for existing records.

## Rollback

Close Asimi and restore your complete pre-upgrade backup into a separate profile before launching the old executable. Retain the post-upgrade profile separately for troubleshooting.

[Installation](INSTALLATION-GUIDE.md) · [Privacy](../../PRIVACY.md)
