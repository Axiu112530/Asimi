# Install Asimi 0.2.1

Use a standard Windows account on Windows x64. Asimi needs Microsoft Edge WebView2 Evergreen Runtime and Windows .NET Framework 4.x. Native acceptance was performed on Windows 11 25H2 x64. [WebView2 Runtime](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution) · [.NET Framework versions](https://learn.microsoft.com/en-us/dotnet/framework/install/versions-and-dependencies)

1. Download `Asimi-0.2.1-Setup-x64.exe` and the matching `.sha256` file from Releases.
2. In PowerShell, run `Get-FileHash .\Asimi-0.2.1-Setup-x64.exe -Algorithm SHA256` and compare it with the published checksum.
3. Close Asimi if you are upgrading. Other applications can stay open. Open the installer, select a language, then choose an installation folder. The default is `%LOCALAPPDATA%\Programs\Asimi`. Choose **Browse** to use another folder.
4. Follow the prompts to install and create an optional desktop shortcut. Launch Asimi from Start or from the final installer screen.
5. Choose your interface language and connect a model, or select **Set up later** to browse projects and history.

The current installer is unsigned. Windows SmartScreen may show an unrecognized-app warning because it also checks publisher and file reputation. If you trust the release source and have checked the matching checksum, choose **More info → Run anyway** to continue. Signing future releases requires a publisher certificate; a signature alone does not guarantee that a new download will avoid warnings. [Microsoft SmartScreen guidance](https://learn.microsoft.com/windows/apps/package-and-deploy/smartscreen-reputation)

Use **Settings → Apps → Installed apps** to uninstall. Your settings and conversations are stored separately in `%LOCALAPPDATA%\Asimi`.

If you use the portable ZIP, verify its own checksum and extract the complete archive into a new local folder. Run `Asimi.exe` with both helpers and their matching `.config` files in the same directory.

To build the installer, run `scripts/build.ps1`, `scripts/package-asimi.ps1` and `scripts/build-installer.ps1` in that order. The installer script uses Inno Setup 6.7.4 by default from `.phase0-tools/innosetup-6.7.4/ISCC.exe`. Use `-CompilerPath` to specify the compiler location. [Inno Setup 6.7.4](https://github.com/jrsoftware/issrc/releases/tag/is-6_7_4)

[Asimi Releases](https://github.com/Axiu112530/Asimi/releases) · [Upgrade guide](UPGRADE-GUIDE.md) · [User guide](USER-GUIDE.md)
