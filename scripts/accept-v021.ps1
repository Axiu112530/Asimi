param(
    [Parameter(Mandatory)][string]$Bin,
    [string]$LegacyProvider,
    [string]$LegacyAgent,
    [string]$LegacyAgent02,
    [switch]$Live,
    [switch]$LiveOnly,
    [switch]$AsimiEvidence
)
$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ExeName = if ($AsimiEvidence) { 'Asimi.exe' } else { 'DeskPilotAI.exe' }
$Exe = (Resolve-Path -LiteralPath (Join-Path $Repo "$Bin/$ExeName")).Path
$Id = [Guid]::NewGuid().ToString('N')
$Run = Join-Path $Repo ".local-validation/v021/$Id"
$Evidence = Join-Path $Repo "docs/0.2.1/evidence/native-$Id"
if ($AsimiEvidence) {
    if ($Live -or $LiveOnly) { throw 'Asimi release acceptance uses offline and loopback fixtures' }
    $Run = Join-Path $Repo ".local-validation/asimi/$Id"
    $Evidence = Join-Path $Repo "docs/release/evidence/native-$Id"
}
if ($LiveOnly -and !$Live) { throw 'LiveOnly requires Live' }
if ($Live -and [string]::IsNullOrWhiteSpace($env:DEEPSEEK_API_KEY)) { throw 'DeepSeek credential is unavailable' }
New-Item -ItemType Directory -Force -Path $Run,$Evidence | Out-Null
$Stages = [Collections.Generic.List[object]]::new()
$Sources = [Collections.Generic.List[object]]::new()
foreach ($Mode in @('offline','live')) {
    if (($Mode -eq 'live' -and !$Live) -or ($Mode -eq 'offline' -and $LiveOnly)) { continue }
    $Profile = Join-Path $Run $Mode
    $Fixture = Join-Path $Profile 'mvp-fixture'
    New-Item -ItemType Directory -Force -Path $Fixture | Out-Null
    [IO.File]::WriteAllText((Join-Path $Fixture 'go.mod'), "module example.com/communityfixture`n`ngo 1.26.0`n")
    [IO.File]::WriteAllText((Join-Path $Fixture 'main.go'), "package main`n`nfunc main() {}`n")
    foreach ($Stage in $(if ($Mode -eq 'offline') { @(14,15) } else { @(16,17) })) {
        $Stages.Add(@{stage=$Stage;profile=$Profile;mode=$Mode})
    }
}
foreach ($Sample in @(@{mode='provider';path=$LegacyProvider},@{mode='agent';path=$LegacyAgent},@{mode='agent-02';path=$LegacyAgent02})) {
    if (!$Sample.path) { continue }
    $Original = (Resolve-Path -LiteralPath $Sample.path).Path
    $LegacyDatabase = Join-Path $Original 'deskpilot.db'
    $Handle = [IO.File]::Open($LegacyDatabase,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
    $Handle.Dispose()
    if ((Test-Path -LiteralPath ($LegacyDatabase + '-wal')) -and (Get-Item -LiteralPath ($LegacyDatabase + '-wal')).Length) { throw 'Close and checkpoint the old profile before copying it' }
    $Profile = Join-Path $Run ('upgrade-' + $Sample.mode)
    $Sources.Add(@{mode=$Sample.mode;path=$LegacyDatabase;hash=(Get-FileHash -LiteralPath $LegacyDatabase).Hash;destination=(Join-Path $Profile 'deskpilot.db')})
    New-Item -ItemType Directory -Path $Profile | Out-Null
    Copy-Item -LiteralPath $LegacyDatabase -Destination $Profile
    $Attachments = Join-Path $Original 'attachments'
    if (Test-Path -LiteralPath $Attachments -PathType Container) { Copy-Item -LiteralPath $Attachments -Destination $Profile -Recurse }
    $Stages.Add(@{stage=18;profile=$Profile;mode=$Sample.mode})
}

$Fixture = $null
$FixtureURL = ''
$Inspector = Join-Path $Repo 'validation/asimi/upgrade-profile.py'
try {
    if ($AsimiEvidence -and $LegacyProvider) {
        . "$Repo/validation/phase0/env.ps1"
        $env:GOPROXY = 'off'
        $env:GOSUMDB = 'off'
        $ProviderSource = ($Sources | Where-Object mode -eq 'provider').path
        $Port = & python $Inspector $ProviderSource
        if ($LASTEXITCODE -ne 0 -or $Port -notmatch '^[1-9]\d{0,4}$') { throw 'Could not determine the existing fixture port' }
        $FixtureExe = Join-Path $Run 'providerfixture.exe'
        Push-Location $Repo
        try {
            & go build -trimpath -o $FixtureExe ./validation/phase2b/providerfixture
            if ($LASTEXITCODE) { throw 'Loopback fixture build failed' }
        } finally { Pop-Location }
        $Ready = Join-Path $Run 'loopback-url.txt'
        $Fixture = Start-Process -FilePath $FixtureExe -ArgumentList @('--ready-file',('"{0}"' -f $Ready),'--listen-port',$Port) -Environment @{DEEPSEEK_API_KEY=$null} -WindowStyle Hidden -PassThru
        for ($i=0; $i -lt 100 -and !(Test-Path -LiteralPath $Ready) -and !$Fixture.HasExited; $i++) { Start-Sleep -Milliseconds 50 }
        if (!(Test-Path -LiteralPath $Ready)) { throw 'The existing loopback fixture port is unavailable' }
        $FixtureURL = (Get-Content -Raw -LiteralPath $Ready).Trim()
        if ($FixtureURL -cne "http://127.0.0.1:${Port}/v1") { throw 'Unexpected loopback fixture endpoint' }
    }

    $Rows = foreach ($Entry in $Stages) {
        $Stage = $Entry.stage
        $Profile = $Entry.profile
        $Name = "$Stage-$($Entry.mode)"
        $Report = Join-Path $Evidence "$Name.json"
        $Runtime = Join-Path $Profile 'runtime.log'
        $Previous = if (Test-Path -LiteralPath $Runtime) { ([IO.File]::ReadAllText($Runtime)).Length } else { 0 }
        $Params = @('--data-dir',('"{0}"' -f $Profile),'--acceptance','--acceptance-stage',$Stage,'--acceptance-report',('"{0}"' -f $Report))
        if ($Stage -eq 16) { $Params += '--acceptance-community-live' }
        if ($AsimiEvidence) {
            $FixtureTag = switch ($Entry.mode) {
                'provider' { "asimi-provider:$FixtureURL" }
                'agent' { 'asimi-agent' }
                'agent-02' { 'asimi-agent-02' }
                default { 'asimi' }
            }
            $Params += @('--acceptance-fixture',$FixtureTag)
        } elseif ($Stage -eq 18) { $Params += @('--acceptance-fixture',$Entry.mode) }
        $Timer = [Diagnostics.Stopwatch]::StartNew()
        $Stderr = Join-Path $Evidence "$Name.stderr.log"
        $ChildEnvironment = if ($Stage -eq 16) { @{} } else { @{ DEEPSEEK_API_KEY = $null } }
        $Process = Start-Process -FilePath $Exe -WorkingDirectory $Run -ArgumentList $Params -Environment $ChildEnvironment -WindowStyle Hidden -PassThru -RedirectStandardError $Stderr
        try {
            $null = $Process.Handle
            $Peak = 0L
            while (!$Process.WaitForExit(125) -and $Timer.Elapsed.TotalSeconds -lt 85) {
                $Process.Refresh()
                $Peak = [Math]::Max($Peak,$Process.WorkingSet64)
            }
            if (!$Process.HasExited) { $null = $Process.CloseMainWindow(); throw "Acceptance stage $Name timed out" }
            if (!(Test-Path -LiteralPath $Report)) { throw "Stage $Name exited without evidence ($($Process.ExitCode))" }
            $Value = Get-Content -LiteralPath $Report -Raw | ConvertFrom-Json
            if ($Process.ExitCode -ne 0 -or $Value.stage -ne $Stage -or !$Value.passed) { throw "Stage $Name failed: $($Value.error). Evidence at $Report" }
            $Log = ([IO.File]::ReadAllText($Runtime)).Substring($Previous)
            $Index = -1
            foreach ($Marker in @('lifecycle.navigation_completed','lifecycle.storage_closed','lifecycle.native_close_requested','lifecycle.window_closing','lifecycle.native_window_removed','lifecycle.wails_shutdown','lifecycle.run_returned')) {
                $Index = $Log.IndexOf(('"msg":"{0}"' -f $Marker),$Index+1,[StringComparison]::Ordinal)
                if ($Index -lt 0) { throw "Stage $Name missing lifecycle marker: $Marker" }
            }
            [IO.File]::WriteAllText((Join-Path $Evidence "$Name.runtime.log"),$Log)
            $Database = [IO.File]::Open((Join-Path $Profile 'deskpilot.db'),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
            $Database.Dispose()
            [ordered]@{stage=$Stage;mode=$Entry.mode;elapsedMs=$Timer.ElapsedMilliseconds;assertions=@($Value.checks.PSObject.Properties).Count;sampledPeakMainWorkingSetBytes=$Peak;exclusiveDatabaseReopen=$true;orderedShutdown=$true;diagnostic1412=[regex]::Matches($Log+(Get-Content -Raw -LiteralPath $Stderr),'Chrome_WidgetWin_0[^\r\n]*1412').Count}
        } finally {
            if (!$Process.HasExited -and $AsimiEvidence) {
                $null = $Process.CloseMainWindow()
                if (!$Process.WaitForExit(3000)) { $Process.Kill(); $Process.WaitForExit() }
            }
            $Process.Dispose()
        }
    }
} finally {
    if ($Fixture) {
        if (!$Fixture.HasExited) { $Fixture.Kill(); $Fixture.WaitForExit() }
        $Fixture.Dispose()
    }
}
$ProfileChecks = [ordered]@{}
foreach ($Source in $Sources) {
    if ((Get-FileHash -LiteralPath $Source.path).Hash -ne $Source.hash) { throw 'Original profile changed during upgrade acceptance' }
    if ($AsimiEvidence) {
        $Check = & python $Inspector $Source.path --compare $Source.destination
        if ($LASTEXITCODE) { throw "Stable profile state did not survive upgrade ($($Source.mode))" }
        $ProfileChecks[$Source.mode] = $Check | ConvertFrom-Json
    }
}
$Summary = [ordered]@{binarySha256=(Get-FileHash -LiteralPath $Exe).Hash;stages=@($Rows);legacySourceProfilesUnchanged=$Sources.Count;externalConnectionChecks=[int]$Live.IsPresent;diagnostic1412Conclusion='CHARACTERIZED'}
if ($AsimiEvidence) { $Summary.profileComparisons = $ProfileChecks }
if ($Live) { $Summary.isolatedLiveProfile = ".local-validation/v021/$Id/live" }
$Summary | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $Evidence 'summary.json') -Encoding utf8
$Summary | ConvertTo-Json -Depth 6
