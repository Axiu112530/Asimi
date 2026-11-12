param([Parameter(Mandatory)][string]$Bin, [switch]$AsimiEvidence)

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ExeName = if ($AsimiEvidence) { 'Asimi.exe' } else { 'DeskPilotAI.exe' }
$WindowTitle = if ($AsimiEvidence) { 'Asimi' } else { 'DeskPilot AI' }
$Exe = (Resolve-Path -LiteralPath (Join-Path $Repo "$Bin/$ExeName")).Path
$RunID = [Guid]::NewGuid().ToString('N')
$Working = Join-Path $Repo ".local-validation/v021/ordinary-$RunID"
$Evidence = Join-Path $Repo 'docs/0.2.1/evidence'
$Data = Join-Path $Working 'profile'
$ChildEnvironment = @{DEEPSEEK_API_KEY=$null}
if ($AsimiEvidence) {
    $Working = Join-Path $Repo ".local-validation/asimi/ordinary-$RunID"
    $Evidence = Join-Path $Repo "docs/release/evidence/ordinary-$RunID"
    $LocalData = Join-Path $Working 'LocalAppData'
    $Data = Join-Path $LocalData 'DeskPilotAI'
    $ChildEnvironment.LOCALAPPDATA = $LocalData
    $Metadata = [Diagnostics.FileVersionInfo]::GetVersionInfo($Exe)
    if ($Metadata.ProductName -cne 'Asimi Community' -or $Metadata.ProductVersion -notmatch '^0\.2\.1(?:\.0)?$') { throw 'Asimi Community PE identity mismatch' }
    Add-Type -AssemblyName System.Drawing
    $Icon = [Drawing.Icon]::ExtractAssociatedIcon($Exe)
    if (!$Icon) { throw 'Asimi EXE has no associated icon' }
    $Icon.Dispose()
}
$Runtime = Join-Path $Data 'runtime.log'
New-Item -ItemType Directory -Path $Working -Force | Out-Null
if ($AsimiEvidence) { New-Item -ItemType Directory -Path $LocalData,$Evidence -Force | Out-Null }

function Read-RuntimeLog {
    if (!(Test-Path -LiteralPath $Runtime)) { return '' }
    $Stream = [IO.File]::Open($Runtime,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::ReadWrite)
    try {
        $Reader = [IO.StreamReader]::new($Stream)
        try { return $Reader.ReadToEnd() } finally { $Reader.Dispose() }
    } finally { $Stream.Dispose() }
}

Add-Type -Namespace DeskPilotAcceptance -Name OwnWindow -MemberDefinition @'
private delegate bool WindowCallback(System.IntPtr window, System.IntPtr state);
[System.Runtime.InteropServices.DllImport("user32.dll")]
private static extern bool EnumWindows(WindowCallback callback, System.IntPtr state);
[System.Runtime.InteropServices.DllImport("user32.dll")]
private static extern uint GetWindowThreadProcessId(System.IntPtr window, out uint processId);
[System.Runtime.InteropServices.DllImport("user32.dll", CharSet=System.Runtime.InteropServices.CharSet.Unicode)]
private static extern int GetWindowText(System.IntPtr window, System.Text.StringBuilder title, int length);
[System.Runtime.InteropServices.DllImport("user32.dll")]
private static extern bool PostMessage(System.IntPtr window, uint message, System.IntPtr wParam, System.IntPtr lParam);
[System.Runtime.InteropServices.DllImport("user32.dll")]
private static extern System.IntPtr SendMessageTimeout(System.IntPtr window, uint message, System.UIntPtr wParam, System.IntPtr lParam, uint flags, uint timeoutMs, out System.UIntPtr result);
public static bool Close(uint ownedProcessId, string expectedTitle, bool requireIcon) {
    System.IntPtr found = System.IntPtr.Zero;
    EnumWindows(delegate(System.IntPtr window, System.IntPtr state) {
        uint processId;
        GetWindowThreadProcessId(window, out processId);
        if (processId != ownedProcessId) return true;
        var title = new System.Text.StringBuilder(128);
        GetWindowText(window, title, title.Capacity);
        if (title.ToString() == expectedTitle) {
            found = window;
            return false;
        }
        return true;
    }, System.IntPtr.Zero);
    if (found == System.IntPtr.Zero) return false;
    if (requireIcon) {
        System.UIntPtr icon;
        var sent = SendMessageTimeout(found, 0x007f, new System.UIntPtr(1u), System.IntPtr.Zero, 0x0002, 1500, out icon);
        if (sent == System.IntPtr.Zero || icon == System.UIntPtr.Zero) return false;
    }
    return PostMessage(found, 0x0010, System.IntPtr.Zero, System.IntPtr.Zero);
}
'@

$Rows = foreach ($Cycle in 1..2) {
    $Previous = (Read-RuntimeLog).Length
    $Timer = [Diagnostics.Stopwatch]::StartNew()
    $Launch = @{
        FilePath=$Exe; WorkingDirectory=$Working; Environment=$ChildEnvironment; WindowStyle='Hidden'; PassThru=$true
    }
    if (!$AsimiEvidence) { $Launch.ArgumentList = @('--data-dir',('"{0}"' -f $Data)) }
    $App = Start-Process @Launch
    try {
        $null = $App.Handle
        do {
            Start-Sleep -Milliseconds 100
            $App.Refresh()
            $Log = (Read-RuntimeLog).Substring($Previous)
        } until ($App.HasExited -or $Log.Contains('lifecycle.navigation_completed') -or $Timer.Elapsed.TotalSeconds -gt 20)
        if ($App.HasExited -or !$Log.Contains('lifecycle.navigation_completed')) { throw 'Ordinary launch did not load its WebView' }
        if (![DeskPilotAcceptance.OwnWindow]::Close($App.Id,$WindowTitle,$AsimiEvidence.IsPresent) -or !$App.WaitForExit(15000) -or $App.ExitCode -ne 0) { throw 'Expected native window or icon did not verify, or the app did not close normally' }
        $Log = ([IO.File]::ReadAllText($Runtime)).Substring($Previous)
        [IO.File]::WriteAllText((Join-Path $Evidence "ordinary-$Cycle.runtime.log"),$Log)
        if ($Log -notmatch '"bindings":1' -or !$Log.Contains('lifecycle.storage_closed') -or !$Log.Contains('lifecycle.run_returned')) { throw 'Ordinary lifecycle or service isolation failed' }
        $Database = [IO.File]::Open((Join-Path $Data 'deskpilot.db'),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
        $Database.Dispose()
        [ordered]@{cycle=$Cycle;elapsedMs=$Timer.ElapsedMilliseconds;oneProductBinding=$true;databaseExclusiveReopen=$true;exitCode=$App.ExitCode}
    } finally {
        if (!$App.HasExited) {
            $null = [DeskPilotAcceptance.OwnWindow]::Close($App.Id,$WindowTitle,$false)
            if (!$App.WaitForExit(5000)) { $App.Kill(); $App.WaitForExit() }
        }
        $App.Dispose()
    }
}
$Result = [ordered]@{passed=$true;binarySha256=(Get-FileHash -LiteralPath $Exe).Hash;cycles=@($Rows);independentWorkingDirectory=$true}
if ($AsimiEvidence) {
    $Result.nativeWindowTitle = $WindowTitle
    $Result.productName = $Metadata.ProductName
    $Result.productVersion = $Metadata.ProductVersion
    $Result.associatedIcon = $true
    $Result.nativeWindowIcon = $true
    $Result.defaultProfileDirectory = Split-Path $Data -Leaf
    $Result.isolatedLocalAppData = $true
}
$Result | ConvertTo-Json -Depth 4 | Tee-Object -FilePath (Join-Path $Evidence 'ordinary-launch.json')
