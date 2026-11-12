# Upgrade to Asimi Community 0.2.1

DeskPilot 0.2 and 0.2.1 profiles use the same default data directory, `%LOCALAPPDATA%\DeskPilotAI`. Keep the directory name so Asimi can reopen your settings, history, credentials and recovery records.

1. In the existing app, note the data directory shown under Settings → About. Close all instances and back up that directory to a private local location.
2. Extract Asimi into a new program folder and launch `Asimi.exe` using the same Windows account on the same computer. For an existing custom profile, run `.\Asimi.exe --data-dir "C:\path\to\existing-profile"`.
3. Confirm the provider, default model, language, theme and history in Asimi. Review unfinished tasks and approvals before resuming work. Credentials protected by Windows DPAPI may need to be entered again if the profile moves to a different account or computer.
4. If you need to roll back, close Asimi and restore the pre-upgrade backup before starting the earlier executable. Keep a separate copy of the post-upgrade profile for troubleshooting.

[Install Asimi](INSTALLATION-GUIDE.md)
