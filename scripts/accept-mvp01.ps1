param([string]$Bin = 'build/bin/mvp01-portable', [switch]$Regression, [switch]$Extracted, [switch]$Version02Evidence, [switch]$Version021Evidence, [switch]$AsimiEvidence)
$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$Repo/validation/phase0/env.ps1"
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$ExeName = if ($AsimiEvidence) { 'Asimi.exe' } else { 'DeskPilotAI.exe' }
$Binary = Join-Path $Repo "$Bin/$ExeName"
$RunID = [Guid]::NewGuid().ToString('N')
$RunRoot = if ($AsimiEvidence) { '.local-validation/asimi' } else { '.local-validation/mvp01' }
$Run = Join-Path $Repo "$RunRoot/$RunID"
$EvidenceName = if ($Extracted) { 'extracted' } else { 'native' }
$Evidence = Join-Path $Repo "docs/mvp01-evidence/$EvidenceName"
if ($Version02Evidence) { $Evidence = Join-Path $Repo 'docs/0.2/evidence/regression-01' }
if ($Version021Evidence) { $Evidence = Join-Path $Repo "docs/0.2.1/evidence/regression-$([Guid]::NewGuid().ToString('N'))" }
if ($AsimiEvidence) { $Evidence = Join-Path $Repo "docs/release/evidence/regression-$RunID" }
New-Item -ItemType Directory -Force -Path $Run,$Evidence | Out-Null
$MvpProfile = Join-Path $Run 'mvp'
$FixtureRoot = Join-Path $MvpProfile 'mvp-fixture'
New-Item -ItemType Directory -Force -Path $FixtureRoot,(Join-Path $FixtureRoot 'node_modules') | Out-Null
[IO.File]::WriteAllText((Join-Path $FixtureRoot 'sample.txt'), "original`n")
[IO.File]::WriteAllText((Join-Path $FixtureRoot 'go.mod'), "module fixture`n`ngo 1.26.0`n")
[IO.File]::WriteAllText((Join-Path $FixtureRoot 'sample_test.go'), "package fixture`nimport `"testing`"`nfunc TestFixture(t *testing.T) { if 2+2 != 4 { t.Fatal(`"sum`") } }`n")
[IO.File]::WriteAllText((Join-Path $FixtureRoot 'node_modules/ignored.txt'), 'exclude')
$Markers = @('lifecycle.navigation_completed','lifecycle.shutdown_requested','lifecycle.storage_closed',
    'lifecycle.registry_closed','lifecycle.native_close_requested','lifecycle.window_closing','lifecycle.native_window_removed',
    'lifecycle.wails_shutdown','lifecycle.wails_post_shutdown','lifecycle.run_returned')
$Fixture = $null
$Rows = @()
$FixtureURL = ''
try {
    if ($Regression) {
        $FixtureExe = Join-Path $Run 'providerfixture.exe'
        & go build -trimpath -o $FixtureExe ./validation/phase2b/providerfixture
        if ($LASTEXITCODE) { throw 'Loopback fixture build failed' }
        $Ready = Join-Path $Run 'loopback-url.txt'
        $Audit = Join-Path $Run 'screen-audit.jsonl'
        $Fixture = Start-Process -FilePath $FixtureExe -ArgumentList @('--ready-file', ('"{0}"' -f $Ready),'--screen-audit-file',('"{0}"' -f $Audit)) -WindowStyle Hidden -PassThru
        for ($i=0; $i -lt 100 -and !(Test-Path -LiteralPath $Ready); $i++) { Start-Sleep -Milliseconds 50 }
        $FixtureURL = Get-Content -Raw -LiteralPath $Ready
        if ($FixtureURL -notmatch '^http://127\.0\.0\.1:[1-9]\d{0,4}/v1$') { throw 'Fixture must be loopback' }
    }
    $Stages = if ($Regression) { @(1,2,3,5,6,7,8,9,10) } else { @(9,10) }
    foreach ($Stage in $Stages) {
        $ProfileName = switch ($Stage) { 1 {'legacy'} 2 {'legacy'} 3 {'fresh'} 5 {'provider'} 6 {'provider'} 7 {'screen'} 8 {'control'} default {'mvp'} }
        $Profile = Join-Path $Run $ProfileName
        $Report = Join-Path $Evidence "stage-$Stage.json"
        $Stdout = Join-Path $Evidence "stage-$Stage.stdout.log"
        $Stderr = Join-Path $Evidence "stage-$Stage.stderr.log"
        $Runtime = Join-Path $Profile 'runtime.log'
        $Prev = if (Test-Path -LiteralPath $Runtime) { (Get-Content -Raw -LiteralPath $Runtime).Length } else { 0 }
        $Args = @('--data-dir',('"{0}"' -f $Profile),'--acceptance','--acceptance-stage',$Stage,'--acceptance-report',('"{0}"' -f $Report))
        if ($Stage -in @(5,6,7)) { $Args += @('--acceptance-fixture',$FixtureURL) }
        $Timer = [Diagnostics.Stopwatch]::StartNew()
        $Process = Start-Process -FilePath $Binary -WorkingDirectory $Run -ArgumentList $Args -Environment @{DEEPSEEK_API_KEY=$null} -WindowStyle Hidden -PassThru -RedirectStandardOutput $Stdout -RedirectStandardError $Stderr
        try {
            $null = $Process.Handle
            if (!$Process.WaitForExit(95000)) { $null = $Process.CloseMainWindow(); throw "Stage $Stage timed out" }
            if (!(Test-Path -LiteralPath $Report)) { throw "Stage $Stage produced no report (exit $($Process.ExitCode))" }
            $Result = Get-Content -Raw -LiteralPath $Report | ConvertFrom-Json
            if ($Result.stage -ne $Stage -or !$Result.passed -or $Process.ExitCode -ne 0) { throw "Stage $Stage failed ($($Process.ExitCode)): $($Result.error)" }
            $RuntimeText = (Get-Content -Raw -LiteralPath $Runtime).Substring($Prev)
            $Cursor = -1
            foreach ($Marker in $Markers) {
                $Cursor = $RuntimeText.IndexOf(('"msg":"{0}"' -f $Marker),$Cursor+1,[StringComparison]::Ordinal)
                if ($Cursor -lt 0) { throw "Stage $Stage lifecycle marker missing or out of order: $Marker" }
            }
            $Db = [IO.File]::Open((Join-Path $Profile 'deskpilot.db'),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
            $Db.Dispose()
            [IO.File]::WriteAllText((Join-Path $Evidence "stage-$Stage.runtime.log"),$RuntimeText)
            $Diagnostics1412 = [regex]::Matches($RuntimeText+(Get-Content -Raw -LiteralPath $Stderr),'Chrome_WidgetWin_0[^\r\n]*1412').Count
            $Rows += [ordered]@{stage=$Stage; assertions=@($Result.checks.PSObject.Properties).Count; exitCode=$Process.ExitCode; elapsedMs=$Timer.ElapsedMilliseconds; orderedShutdown=$true; exclusiveDbReopen=$true; diagnostic1412=$Diagnostics1412}
            "Stage $Stage passed ($($Rows[-1].assertions) assertions)"
        } finally { $Process.Dispose() }
    }
    $Text = [IO.File]::ReadAllText((Join-Path $FixtureRoot 'sample.txt'))
    if ($Text -cne "updated by approved fixture`n") { throw 'Independent workspace file oracle failed' }
    foreach ($Name in @('must-not-create.txt','must-not-replay.txt')) { if (Test-Path -LiteralPath (Join-Path $FixtureRoot $Name)) { throw "Unexpected file: $Name" } }
    if ($Regression) {
        $Screen = Get-Content -Raw -LiteralPath (Join-Path $Evidence 'stage-7.json') | ConvertFrom-Json
        $Sent = @(Get-Content -LiteralPath $Audit | ForEach-Object { $_ | ConvertFrom-Json })
        if ($Sent.Count -ne 2 -or $Sent[0].sha256 -ne $Screen.details.sentImage.sha256 -or $Sent[1].imageParts -ne 0) { throw 'Screen fixture did not receive the approved image and image-free followup' }
    }
    $Summary = [ordered]@{stages=$Rows; binarySHA256=(Get-FileHash -LiteralPath $Binary).Hash; workspaceOracle=$true; fixture='ISOLATED_WORKSPACE'; externalModelCalls=0; diagnostic1412Conclusion='CHARACTERIZED'}
    $Summary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Evidence 'summary.json') -Encoding utf8
    if ($AsimiEvidence) { $Summary | ConvertTo-Json -Depth 5 }
} finally {
    if ($Fixture) { if (!$Fixture.HasExited) { $Fixture.Kill(); $Fixture.WaitForExit() }; $Fixture.Dispose() }
}
