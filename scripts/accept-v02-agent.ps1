param([string]$Bin = 'build/bin/v02-portable', [ValidateSet('Loopback','Live')][string]$Mode = 'Loopback', [switch]$Version021Evidence, [string]$CommunityProfile, [switch]$AsimiEvidence)

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$Repo/validation/phase0/env.ps1"
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$ExeName = if ($AsimiEvidence) { 'Asimi.exe' } else { 'DeskPilotAI.exe' }
$Exe = (Resolve-Path -LiteralPath (Join-Path $Repo "$Bin/$ExeName")).Path
$RunID = [Guid]::NewGuid().ToString('N')
$Run = Join-Path $Repo ".local-validation/v02/$RunID"
$Evidence = Join-Path $Repo "docs/0.2/evidence/$($Mode.ToLowerInvariant())-$RunID"
if ($Version021Evidence) { $Evidence = Join-Path $Repo "docs/0.2.1/evidence/agent-$($Mode.ToLowerInvariant())-$RunID" }
if ($AsimiEvidence) {
    if ($Mode -ne 'Loopback') { throw 'Asimi release Agent acceptance uses the loopback fixture' }
    $Run = Join-Path $Repo ".local-validation/asimi/$RunID"
    $Evidence = Join-Path $Repo "docs/release/evidence/agent-loopback-$RunID"
}
$GitFixture = $Version021Evidence -or $AsimiEvidence
$Profile = Join-Path $Run 'profile'
$Workspace = Join-Path $Profile 'mvp-fixture'
New-Item -ItemType Directory -Force -Path $Workspace,$Evidence | Out-Null
if ($CommunityProfile) {
    if (!$Version021Evidence -or $Mode -ne 'Live') { throw 'Community profile requires 0.2.1 live acceptance' }
    $CommunityProfile = $CommunityProfile -replace '^(?:\.[\\/])?\.v021-data[\\/]', '.local-validation/v021/'
    $Source = (Resolve-Path -LiteralPath (Join-Path $Repo $CommunityProfile)).Path
    $Isolated = [IO.Path]::GetFullPath((Join-Path $Repo '.local-validation/v021')) + [IO.Path]::DirectorySeparatorChar
    if (!$Source.StartsWith($Isolated,[StringComparison]::OrdinalIgnoreCase)) { throw 'Expected an isolated Community fixture profile' }
    $SourceDB = Join-Path $Source 'deskpilot.db'
    if ((Test-Path -LiteralPath ($SourceDB + '-wal')) -and (Get-Item -LiteralPath ($SourceDB + '-wal')).Length) { throw 'Community fixture database needs a clean shutdown' }
    Copy-Item -LiteralPath $SourceDB -Destination (Join-Path $Profile 'deskpilot.db')
}
$env:GOCACHE = Join-Path $Repo '.phase0-cache/go-build'
$env:TMP = $Run
$env:TEMP = $Run
[IO.File]::WriteAllText((Join-Path $Workspace 'go.mod'), "module fixture`n`ngo 1.26.0`n")
[IO.File]::WriteAllText((Join-Path $Workspace 'calc.go'), "package fixture`n`nfunc Add(a, b int) int {`n`treturn a - b`n}`n")
[IO.File]::WriteAllText((Join-Path $Workspace 'calc_test.go'), "package fixture`n`nimport `"testing`"`n`nfunc TestAdd(t *testing.T) {`n`tif got := Add(2, 3); got != 5 { t.Fatalf(`"Add(2,3) = %d; want 5`", got) }`n}`n")
$TestHash = (Get-FileHash -LiteralPath (Join-Path $Workspace 'calc_test.go')).Hash
$Fixture = $null
$FixtureURL = ''
$Results = @()
$Stage = if ($Mode -eq 'Live') { 12 } else { 11 }
$LocalComment = '// Workspace author note: this function is part of the example API.'
if ($GitFixture) {
    $env:GIT_CONFIG_NOSYSTEM = '1'
    $env:GIT_CONFIG_GLOBAL = (Join-Path $Run 'empty-gitconfig')
    [IO.File]::WriteAllText($env:GIT_CONFIG_GLOBAL,'')
    & git -C $Workspace init --quiet --initial-branch=community-fixture --template=
    if ($LASTEXITCODE) { throw 'Git fixture initialization failed' }
    & git -C $Workspace -c core.autocrlf=false add -- go.mod calc.go calc_test.go
    if ($LASTEXITCODE) { throw 'Git fixture index creation failed' }
    $Calc = Join-Path $Workspace 'calc.go'
    [IO.File]::WriteAllText($Calc,([IO.File]::ReadAllText($Calc)).Replace('func Add',"$LocalComment`nfunc Add"))
    [IO.File]::WriteAllText((Join-Path $Workspace 'untracked-notes.txt'),'Untracked fixture note.')
    $IndexHash = (Get-FileHash -LiteralPath (Join-Path $Workspace '.git/index')).Hash
}

try {
    if ($Mode -eq 'Loopback') {
        $FixtureExe = Join-Path $Run 'agentfixture.exe'
        & go build -trimpath -o $FixtureExe ./validation/v02/agentfixture
        if ($LASTEXITCODE) { throw 'Loopback planner fixture build failed' }
        $Ready = Join-Path $Run 'ready.txt'
        $Fixture = Start-Process -FilePath $FixtureExe -ArgumentList @('--ready-file',('"{0}"' -f $Ready)) -WindowStyle Hidden -PassThru
        for ($i=0; $i -lt 100 -and !(Test-Path -LiteralPath $Ready); $i++) { Start-Sleep -Milliseconds 50 }
        $FixtureURL = Get-Content -LiteralPath $Ready -Raw
        if ($FixtureURL -notmatch '^http://127\.0\.0\.1:[1-9]\d{0,4}/v1$') { throw 'Invalid loopback endpoint' }
    }
    foreach ($CurrentStage in @($Stage,13)) {
        $Report = Join-Path $Evidence "stage-$CurrentStage.json"
        $Stderr = Join-Path $Evidence "stage-$CurrentStage.stderr.log"
        $ProcessArgs = @('--acceptance','--acceptance-stage',$CurrentStage,'--data-dir',('"{0}"' -f $Profile),'--acceptance-report',('"{0}"' -f $Report))
        if ($CurrentStage -eq 11) { $ProcessArgs += @('--acceptance-fixture',$FixtureURL) }
        if ($CurrentStage -eq 12) {
            $ProcessArgs += $(if ($CommunityProfile) { '--acceptance-community-profile-agent' } else { '--acceptance-live-agent' })
        }
        if ($GitFixture) { $ProcessArgs += '--acceptance-git-fixture' }
        $Runtime = Join-Path $Profile 'runtime.log'
        $PreviousLength = if (Test-Path -LiteralPath $Runtime) { ([IO.File]::ReadAllText($Runtime)).Length } else { 0 }
        $Timer = [Diagnostics.Stopwatch]::StartNew()
        $ChildEnvironment = if ($CurrentStage -eq 12 -and !$CommunityProfile) { @{} } else { @{ DEEPSEEK_API_KEY = $null } }
        $Process = Start-Process -FilePath $Exe -WorkingDirectory $Run -ArgumentList $ProcessArgs -Environment $ChildEnvironment -WindowStyle Hidden -PassThru -RedirectStandardError $Stderr
        try {
            $null = $Process.Handle
            $Peak = 0L
            while (!$Process.WaitForExit(150) -and $Timer.Elapsed.TotalSeconds -lt 190) {
                $Process.Refresh()
                $Peak = [Math]::Max($Peak,$Process.WorkingSet64)
            }
            if (!$Process.HasExited) { $null = $Process.CloseMainWindow(); throw "Stage $CurrentStage timed out" }
            if (!(Test-Path -LiteralPath $Report)) { throw "Stage $CurrentStage exited without evidence ($($Process.ExitCode))" }
            $Value = Get-Content -LiteralPath $Report -Raw | ConvertFrom-Json
            if ($Process.ExitCode -ne 0 -or !$Value.passed) { throw "Stage $CurrentStage failed: $($Value.error). Evidence at $Report" }
            $Log = ([IO.File]::ReadAllText($Runtime)).Substring($PreviousLength)
            $Index = -1
            foreach ($Mark in @('lifecycle.navigation_completed','lifecycle.storage_closed','lifecycle.native_close_requested','lifecycle.window_closing','lifecycle.native_window_removed','lifecycle.wails_shutdown','lifecycle.run_returned')) {
                $Index = $Log.IndexOf(('"msg":"{0}"' -f $Mark),$Index+1,[StringComparison]::Ordinal)
                if ($Index -lt 0) { throw "Lifecycle marker missing: $Mark" }
            }
            [IO.File]::WriteAllText((Join-Path $Evidence "stage-$CurrentStage.runtime.log"),$Log)
            $Handle = [IO.File]::Open((Join-Path $Profile 'deskpilot.db'),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
            $Handle.Dispose()
            $Results += [ordered]@{stage=$CurrentStage;elapsedMs=$Timer.ElapsedMilliseconds;assertions=@($Value.checks.PSObject.Properties).Count;appPeakWorkingSetBytes=$Peak;databaseExclusiveReopen=$true;orderedShutdown=$true}
        } finally { $Process.Dispose() }
    }
    if ((Get-FileHash -LiteralPath (Join-Path $Workspace 'calc_test.go')).Hash -ne $TestHash) { throw 'Agent altered the regression test' }
    if (!(Get-Content -LiteralPath (Join-Path $Workspace 'calc.go') -Raw).Contains('a + b')) { throw 'Independent source oracle failed' }
    if ($GitFixture) {
        if ((Get-FileHash -LiteralPath (Join-Path $Workspace '.git/index')).Hash -ne $IndexHash) { throw 'Git index changed during the product workflow' }
        if (!(Get-Content -LiteralPath (Join-Path $Workspace 'calc.go') -Raw).Contains($LocalComment)) { throw 'Existing local file change was lost' }
    }
    $ReportValue = Get-Content -LiteralPath (Join-Path $Evidence "stage-$Stage.json") -Raw | ConvertFrom-Json
    $Summary = [ordered]@{mode=$Mode;binarySHA256=(Get-FileHash -LiteralPath $Exe).Hash;stages=$Results;externalCalls=$(if ($Mode -eq 'Live') { @($ReportValue.rounds).Count } else { 0 });sourceOracle=$true;testFileUnchanged=$true;evidence=$Evidence;diagnostic1412Conclusion='CHARACTERIZED'}
    if ($GitFixture) { $Summary.gitIndexUnchanged=$true; $Summary.existingLocalChangePreserved=$true }
    if ($CommunityProfile) { $Summary.connectedDPAPIProfile=$true }
    $Summary | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $Evidence 'summary.json') -Encoding utf8
    $Summary | ConvertTo-Json -Depth 8
} finally {
    if ($Fixture) { if (!$Fixture.HasExited) { $Fixture.Kill(); $Fixture.WaitForExit() }; $Fixture.Dispose() }
}
