param()

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$Evidence = Join-Path $Repo 'docs/0.2.1/evidence'
$Package = Get-Content -Raw -LiteralPath (Join-Path $Evidence 'package.json') | ConvertFrom-Json
$Archive = Join-Path $Repo 'build/bin/DeskPilot-Community-0.2.1-win-x64.zip'
$Extracted = Join-Path $Repo ('build/bin/' + $Package.extracted)
if ((Get-FileHash -LiteralPath $Archive).Hash -ne $Package.archiveSha256) { throw 'Archive hash changed' }
$ExpectedSidecar = '{0}  {1}' -f $Package.archiveSha256,(Split-Path -Leaf $Archive)
if ((Get-Content -LiteralPath ($Archive + '.sha256') -Raw).Trim() -ne $ExpectedSidecar) { throw 'Archive checksum sidecar differs' }
if ((Get-FileHash -LiteralPath (Join-Path $Extracted 'DeskPilotAI.exe')).Hash -ne $Package.mainSha256) { throw 'Executable hash changed' }
$Manifest = Get-Content -LiteralPath (Join-Path $Extracted 'SHA256SUMS.txt')
foreach ($Line in $Manifest) {
    $Hash,$Name = $Line -split '  ',2
    if ([IO.Path]::GetFileName($Name) -ne $Name) { throw 'Manifest path is outside the archive root' }
    if ((Get-FileHash -LiteralPath (Join-Path $Extracted $Name)).Hash -ne $Hash) { throw "Manifest mismatch $Name" }
}
if (@(Get-ChildItem -LiteralPath $Extracted -Force).Count -ne $Package.fileCount) { throw 'Unexpected extracted files' }

function Find-VerifiedRun([string]$Pattern) {
    foreach ($Directory in Get-ChildItem -LiteralPath $Evidence -Directory -Filter $Pattern | Sort-Object LastWriteTime -Descending) {
        $Summary = Join-Path $Directory.FullName 'summary.json'
        if (!(Test-Path -LiteralPath $Summary)) { continue }
        $Data = Get-Content -Raw -LiteralPath $Summary | ConvertFrom-Json
        $BinaryHash = if ($Data.binarySha256) { $Data.binarySha256 } else { $Data.binarySHA256 }
        if ($BinaryHash -eq $Package.mainSha256) {
            foreach ($Stage in Get-ChildItem -LiteralPath $Directory.FullName -File -Filter '*.json' | Where-Object Name -Match '^(stage-|1[4-8]-)') {
                $Result = Get-Content -Raw -LiteralPath $Stage.FullName | ConvertFrom-Json
                if (!$Result.passed -or @($Result.checks.PSObject.Properties | Where-Object Value -NE $true).Count) { throw "Failed final run $($Directory.Name)" }
            }
            return @{directory=$Directory.Name;data=$Data}
        }
    }
    throw "Verified final executable evidence missing for $Pattern"
}

$Native = Find-VerifiedRun 'native-*'
$Regression = Find-VerifiedRun 'regression-*'
$Loopback = Find-VerifiedRun 'agent-loopback-*'
$Live = Find-VerifiedRun 'agent-live-*'
$Soak = Find-VerifiedRun 'soak-*'
$SoakReport = Get-Content -Raw -LiteralPath (Join-Path $Evidence "$($Soak.directory)/report.json") | ConvertFrom-Json
$SoakSamples = @(Get-Content -LiteralPath (Join-Path $Evidence "$($Soak.directory)/samples.jsonl"))
if (!$Soak.data.passed -or $Soak.data.requestedSeconds -lt 7200 -or !$Soak.data.exclusiveDatabaseReopen -or
    !$SoakReport.passed -or $SoakReport.externalModelCalls -ne 0 -or
    $SoakReport.elapsedMs -ne $Soak.data.durationMs -or $Soak.data.durationMs -lt ($Soak.data.requestedSeconds * 1000) -or
    $SoakSamples.Count -ne $Soak.data.processSamples -or $SoakSamples.Count -eq 0) { throw 'Final two-hour run is incomplete' }
foreach ($Counter in $Soak.data.counts.PSObject.Properties) {
    if ($SoakReport.counts.($Counter.Name) -ne $Counter.Value) { throw "Soak report count differs for $($Counter.Name)" }
}
$Ordinary = Get-Content -Raw -LiteralPath (Join-Path $Evidence 'ordinary-launch.json') | ConvertFrom-Json
if (!$Ordinary.passed -or $Ordinary.binarySha256 -ne $Package.mainSha256) { throw 'Ordinary launch evidence is missing' }

$Archive02 = Get-Content -Raw -LiteralPath (Join-Path $Repo 'docs/0.2/evidence/package.json') | ConvertFrom-Json
$Archive01 = Get-Content -Raw -LiteralPath (Join-Path $Repo 'docs/mvp01-evidence/package.json') | ConvertFrom-Json
if ((Get-FileHash -LiteralPath (Join-Path $Repo 'build/bin/DeskPilotAI-0.2-win-x64.zip')).Hash -ne $Archive02.archiveSha256) { throw '0.2 archive changed' }
if ((Get-FileHash -LiteralPath (Join-Path $Repo 'build/bin/DeskPilotAI-MVP-0.1-win-x64.zip')).Hash -ne $Archive01.archiveSha256) { throw 'MVP archive changed' }

$Credential = [Environment]::GetEnvironmentVariable('DEEPSEEK_API_KEY')
if (!$Credential) { throw 'Credential scan input unavailable' }
$ScanFiles = 0
$SourceFolders = @('docs/0.2.1','validation/v021','internal/configuration','frontend/src',('build/bin/' + $Package.extracted))
foreach ($Folder in $SourceFolders) {
    foreach ($File in Get-ChildItem -LiteralPath (Join-Path $Repo $Folder) -File -Recurse) {
        $Stream = [IO.File]::Open($File.FullName,[IO.FileMode]::Open,[IO.FileAccess]::Read,([IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete))
        try {
            $Reader = [IO.StreamReader]::new($Stream,[Text.Encoding]::UTF8)
            try { $Content = $Reader.ReadToEnd() } finally { $Reader.Dispose() }
        } finally { $Stream.Dispose() }
        if ($Content.Contains($Credential)) { throw 'A scanned output contains the provider credential' }
        $ScanFiles++
    }
}
$Credential = $Content = $null

$Rounds = @(Get-ChildItem -LiteralPath $Evidence -Directory -Filter 'agent-live-*' | ForEach-Object {
    $Path = Join-Path $_.FullName 'stage-12.json'
    if (Test-Path -LiteralPath $Path) { (Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json).rounds }
})
$Connections = (Get-ChildItem -LiteralPath $Evidence -Directory -Filter 'native-*' | ForEach-Object {
    $Path = Join-Path $_.FullName 'summary.json'
    if (Test-Path -LiteralPath $Path) { (Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json).externalConnectionChecks }
} | Measure-Object -Sum).Sum

Push-Location $Repo
try {
    $Head = (git rev-parse HEAD).Trim()
    $Branch = (git branch --show-current).Trim()
    $Staged = @(git diff --cached --name-only).Count
    if ($Staged -ne 0 -or $Head -ne '4accd35e6b7a801274e034adc103f59df37e686c') { throw 'Protected Git baseline changed' }
    $null = git diff --check 2>$null
    if ($LASTEXITCODE) { throw 'Git whitespace check failed' }
} finally { Pop-Location }

$AppProcesses = @(Get-CimInstance Win32_Process -Filter "Name='DeskPilotAI.exe' OR Name='DeskPilotControlHost.exe' OR Name='DeskPilotUIAWorker.exe' OR Name='providerfixture.exe'" | Where-Object {
    $_.ExecutablePath -and $_.ExecutablePath.StartsWith($Repo+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)
})
if ($AppProcesses.Count) { throw 'An isolated app or fixture process remains running' }

$Result = [ordered]@{
    passed=$true;archiveSha256=$Package.archiveSha256;binarySha256=$Package.mainSha256;archiveMembers=$Package.fileCount;
    regressionAssertions=($Regression.data.stages | Measure-Object assertions -Sum).Sum;
    communityAssertions=($Native.data.stages | Measure-Object assertions -Sum).Sum;
    loopbackAssertions=($Loopback.data.stages | Measure-Object assertions -Sum).Sum;
    liveAssertions=($Live.data.stages | Measure-Object assertions -Sum).Sum;
    finalExternalRequests=($Native.data.externalConnectionChecks+$Live.data.externalCalls);
    iterationExternalRequests=($Rounds.Count+$Connections);iterationPlannerEstimatedUSD=($Rounds | Measure-Object estimatedUsd -Sum).Sum;
    nativeEvidence=$Native.directory;regressionEvidence=$Regression.directory;loopbackEvidence=$Loopback.directory;liveEvidence=$Live.directory;soakEvidence=$Soak.directory;
    soakDurationMs=$Soak.data.durationMs;soakCounts=$Soak.data.counts;soakProcessSamples=$SoakSamples.Count;ordinaryCycles=$Ordinary.cycles.Count;
    secretScanFiles=$ScanFiles;secretScanPassed=$true;priorArchivesPreserved=$true;appAndHelperResidue=0;
    branch=$Branch;head=$Head;staged=$Staged;applicationLicensePresent=(Test-Path -LiteralPath (Join-Path $Repo 'LICENSE'));
    diagnostic1412Conclusion='CHARACTERIZED'
}
$Result | ConvertTo-Json -Depth 5 | Tee-Object -FilePath (Join-Path $Evidence 'final-checks.json')
