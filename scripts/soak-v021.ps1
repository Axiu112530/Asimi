param(
    [Parameter(Mandatory)][string]$Bin,
    [ValidateRange(120,14400)][int]$Seconds = 7200
)

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$Repo/validation/phase0/env.ps1"
$Exe = (Resolve-Path -LiteralPath (Join-Path $Repo "$Bin/DeskPilotAI.exe")).Path
$Id = [Guid]::NewGuid().ToString('N')
$Run = Join-Path $Repo ".local-validation/v021/soak-$Id"
$Evidence = Join-Path $Repo "docs/0.2.1/evidence/soak-$Id"
$Profile = Join-Path $Run 'profile'
$Workspace = Join-Path $Profile 'mvp-fixture'
New-Item -ItemType Directory -Force -Path $Workspace,$Evidence | Out-Null
[IO.File]::WriteAllText((Join-Path $Workspace 'go.mod'), "module example.com/soakfixture`n`ngo 1.26.0`n")
[IO.File]::WriteAllText((Join-Path $Workspace 'main.go'), "package main`n`nfunc main() {}`n")
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
Remove-Item Env:DEEPSEEK_API_KEY -ErrorAction SilentlyContinue
$FixtureExe = Join-Path $Run 'providerfixture.exe'
& go build -trimpath -o $FixtureExe ./validation/phase2b/providerfixture
if ($LASTEXITCODE) { throw 'Fixture build failed' }

Add-Type -Namespace DeskPilotAcceptance -Name Resources -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern uint GetGuiResources(System.IntPtr process, uint flag);
'@
$Fixture = $Process = $null
$Ready = Join-Path $Run 'ready.txt'
$Timer = [Diagnostics.Stopwatch]::StartNew()
$Report = Join-Path $Evidence 'report.json'
$SampleFile = Join-Path $Evidence 'samples.jsonl'
$Interval = [Math]::Max(3,[Math]::Min(30,[Math]::Floor($Seconds / 12)))
try {
    $Fixture = Start-Process -FilePath $FixtureExe -ArgumentList @('--ready-file',('"{0}"' -f $Ready)) -WindowStyle Hidden -PassThru
    for ($i=0; $i -lt 100 -and !(Test-Path -LiteralPath $Ready); $i++) { Start-Sleep -Milliseconds 50 }
    $URL = Get-Content -Raw -LiteralPath $Ready
    if ($URL -notmatch '^http://127\.0\.0\.1:[1-9]\d{0,4}/v1$') { throw 'Fixture must be loopback' }
    $SoakArguments = @('--data-dir',('"{0}"' -f $Profile),'--acceptance','--acceptance-stage',19,'--acceptance-report',('"{0}"' -f $Report),'--acceptance-fixture',$URL,'--acceptance-soak-seconds',$Seconds)
    $Process = Start-Process -FilePath $Exe -WorkingDirectory $Run -ArgumentList $SoakArguments -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $Evidence 'stderr.log')
    $null = $Process.Handle
    [ordered]@{requestedSeconds=$Seconds;binarySha256=(Get-FileHash -LiteralPath $Exe).Hash;mainPid=$Process.Id;fixturePid=$Fixture.Id;startedAt=(Get-Date -Format o)} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $Evidence 'run.json') -Encoding utf8
    "Soak started ($Seconds seconds), evidence $Evidence"
    while (!$Process.WaitForExit([int]($Interval*1000)) -and $Timer.Elapsed.TotalSeconds -lt ($Seconds + 90)) {
        $Process.Refresh()
        $Descendants = [Collections.Generic.List[object]]::new()
        $Parents = [Collections.Generic.Queue[int]]::new()
        $Parents.Enqueue($Process.Id)
        while ($Parents.Count -and $Descendants.Count -lt 32) {
            $Parent = $Parents.Dequeue()
            foreach ($Child in Get-CimInstance Win32_Process -Filter "ParentProcessId=$Parent") {
                $Descendants.Add($Child)
                $Parents.Enqueue([int]$Child.ProcessId)
            }
        }
        $WebView = @($Descendants | Where-Object Name -EQ 'msedgewebview2.exe')
        $WebViewWorkingSet = ($WebView | Measure-Object WorkingSetSize -Sum).Sum
        $Helpers = @($Descendants | Where-Object Name -In @('DeskPilotControlHost.exe','DeskPilotUIAWorker.exe'))
        $DatabaseSize = (Get-Item -LiteralPath (Join-Path $Profile 'deskpilot.db')).Length
        $FileCount = @(Get-ChildItem -LiteralPath $Profile -File -Force | Where-Object Name -Match '(\.tmp$|\.sqlite-journal$|\.db-wal$)').Count
        $Sample = [ordered]@{
            elapsedMs=$Timer.ElapsedMilliseconds;mainWorkingSetBytes=$Process.WorkingSet64;mainPrivateBytes=$Process.PrivateMemorySize64;
            mainHandles=$Process.HandleCount;mainThreads=$Process.Threads.Count;gdiHandles=[DeskPilotAcceptance.Resources]::GetGuiResources($Process.Handle,0);
            userHandles=[DeskPilotAcceptance.Resources]::GetGuiResources($Process.Handle,1);webViewChildren=$WebView.Count;webViewWorkingSetBytes=$WebViewWorkingSet;
            helperChildren=$Helpers.Count;sqliteBytes=$DatabaseSize;profileTopLevelTransientFiles=$FileCount
        }
        Add-Content -LiteralPath $SampleFile -Value ($Sample | ConvertTo-Json -Compress) -Encoding utf8
    }
    if (!$Process.HasExited) { $null = $Process.CloseMainWindow(); throw 'Soak exceeded its wall-time limit' }
    if (!(Test-Path -LiteralPath $Report)) { throw "Missing soak report ($($Process.ExitCode))" }
    $Result = Get-Content -Raw -LiteralPath $Report | ConvertFrom-Json
    if ($Process.ExitCode -ne 0 -or !$Result.passed) { throw "Soak failed: $($Result.error)" }
    if ($Result.elapsedMs -lt ($Seconds * 1000) -or $Result.counts.completedTurns -ne 105 -or $Result.counts.completedTasks -ne 20) { throw 'Soak duration or workload did not meet its configured target' }
    $Handle = [IO.File]::Open((Join-Path $Profile 'deskpilot.db'),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
    $Handle.Dispose()
    $Log = [IO.File]::ReadAllText((Join-Path $Profile 'runtime.log'))
    if (!$Log.Contains('lifecycle.storage_closed') -or !$Log.Contains('lifecycle.run_returned')) { throw 'Unclean soak shutdown' }
    [IO.File]::WriteAllText((Join-Path $Evidence 'runtime.log'),$Log)
    [ordered]@{passed=$true;durationMs=$Result.elapsedMs;requestedSeconds=$Seconds;counts=$Result.counts;processSamples=@(Get-Content -LiteralPath $SampleFile).Count;exclusiveDatabaseReopen=$true;binarySha256=(Get-FileHash -LiteralPath $Exe).Hash} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Evidence 'summary.json') -Encoding utf8
    Get-Content -Raw -LiteralPath (Join-Path $Evidence 'summary.json')
} finally {
    if ($Process) {
        if (!$Process.HasExited) { $null = $Process.CloseMainWindow(); if (!$Process.WaitForExit(5000)) { $Process.Kill(); $Process.WaitForExit() } }
        $Process.Dispose()
    }
    if ($Fixture) { if (!$Fixture.HasExited) { $Fixture.Kill(); $Fixture.WaitForExit() }; $Fixture.Dispose() }
}
