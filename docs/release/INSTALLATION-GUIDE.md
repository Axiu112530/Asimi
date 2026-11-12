# Install Asimi 0.2.1

Asimi 0.2.1 is a portable Windows x64 release candidate. Use a standard Windows account. The native acceptance environment is Windows 11 25H2 x64. Asimi uses the installed Microsoft Edge WebView2 Evergreen Runtime. The computer helpers use the Windows .NET Framework 4.x runtime. [WebView2 Runtime](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution) · [.NET Framework versions](https://learn.microsoft.com/en-us/dotnet/framework/install/versions-and-dependencies)

1. Obtain `Asimi-0.2.1-win-x64.zip` and its companion `.sha256` file from the release owner. In PowerShell, run `Get-FileHash .\Asimi-0.2.1-win-x64.zip -Algorithm SHA256` and compare the result with the companion file.
2. Extract the entire ZIP into a new local folder. Keep `Asimi.exe`, both native helpers and their matching `.config` files together. The helpers are AsimiControlHost.exe and AsimiUIAWorker.exe. `SHA256SUMS.txt` records the hash of each delivered member.
3. Open `Asimi.exe`. Choose English or Simplified Chinese, then connect your provider or continue offline. Open Settings → About to find the local data directory.

To replace an older installation, follow the upgrade guide. For first use, see the user guide. [Upgrade guide](UPGRADE-GUIDE.md) · [User guide](USER-GUIDE.md)
