$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$Evidence = Join-Path $Repo 'docs/release/evidence'
$Package = Get-Content -Raw -LiteralPath (Join-Path $Evidence 'package.json') | ConvertFrom-Json
$Archive = Join-Path $Repo 'build/bin/Asimi-0.2.1-win-x64.zip'
$Build = Join-Path $Repo 'build/bin/asimi-0.2.1'
$Extracted = Join-Path $Repo ('build/bin/' + $Package.extracted)
if ((Get-FileHash -LiteralPath $Archive).Hash -ne $Package.archiveSha256 -or
    ((Get-Content -Raw -LiteralPath ($Archive + '.sha256')).Trim() -ne "$($Package.archiveSha256)  $(Split-Path -Leaf $Archive)")) { throw 'Archive checksum mismatch' }
foreach ($Directory in @($Build,$Extracted)) {
    $Entries = @(Get-ChildItem -LiteralPath $Directory -Force)
    if ($Entries.Count -ne $Package.fileCount -or @($Entries | Where-Object { $_.PSIsContainer -or $_.Name -notin $Package.members }).Count) {
        throw 'Unexpected release files'
    }
    foreach ($Row in Get-Content -LiteralPath (Join-Path $Directory 'SHA256SUMS.txt')) {
        $Hash,$Name = $Row -split '  ',2
        if ($Name -notin $Package.members -or (Get-FileHash -LiteralPath (Join-Path $Directory $Name)).Hash -ne $Hash) { throw 'Member checksum mismatch' }
    }
    if ((Get-FileHash -LiteralPath (Join-Path $Directory 'Asimi.exe')).Hash -ne $Package.mainSha256) { throw 'Unexpected main binary hash' }
}

function Get-Run([string]$Pattern,[string]$File = 'summary.json') {
    foreach ($Directory in Get-ChildItem -LiteralPath $Evidence -Directory -Filter $Pattern | Sort-Object LastWriteTime -Descending) {
        $Summary = Join-Path $Directory.FullName $File
        if (!(Test-Path -LiteralPath $Summary)) { continue }
        $Run = Get-Content -Raw -LiteralPath $Summary | ConvertFrom-Json
        if ($Run.binarySha256 -eq $Package.mainSha256) { return @{name=$Directory.Name;run=$Run} }
    }
    throw "Evidence missing for $Pattern"
}

$Ordinary = Get-Run 'ordinary-*' 'ordinary-launch.json'
$Native = Get-Run 'native-*'
$Regression = Get-Run 'regression-*'
$Agent = Get-Run 'agent-loopback-*'
if (!$Ordinary.run.passed -or !$Ordinary.run.nativeWindowIcon -or !$Ordinary.run.associatedIcon -or
    $Ordinary.run.nativeWindowTitle -cne 'Asimi' -or $Ordinary.run.productName -cne 'Asimi' -or
    !$Ordinary.run.isolatedLocalAppData -or $Ordinary.run.defaultProfileDirectory -cne 'Asimi') { throw 'Ordinary launch check failed' }
if ($Native.run.legacySourceProfilesUnchanged -ne 3 -or @($Native.run.profileComparisons.PSObject.Properties).Count -ne 3) { throw 'Missing legacy profile checks' }
foreach ($Comparison in $Native.run.profileComparisons.PSObject.Properties.Value) {
    if (!$Comparison.passed -or $Comparison.schemaBefore -ne 6 -or $Comparison.schemaAfter -ne 6 -or
        $Comparison.integrity -ne 'ok' -or $Comparison.foreignKeyViolations -ne 0) { throw 'Legacy profile check failed' }
}
foreach ($Check in @($Native,$Regression,$Agent)) {
    foreach ($Result in Get-ChildItem -LiteralPath (Join-Path $Evidence $Check.name) -File -Filter '*.json' | Where-Object Name -Match '^(stage-\d+|1[458]-.*)\.json$') {
        $Stage = Get-Content -Raw -LiteralPath $Result.FullName | ConvertFrom-Json
        if (!$Stage.passed -or @($Stage.checks.PSObject.Properties | Where-Object Value -NE $true).Count) { throw 'Native stage check failed' }
    }
}
if (!$Regression.run.workspaceOracle -or !$Agent.run.sourceOracle -or !$Agent.run.gitIndexUnchanged -or
    !$Agent.run.existingLocalChangePreserved -or !$Agent.run.testFileUnchanged) { throw 'Workspace oracle failed' }
$ExternalCalls = $Native.run.externalConnectionChecks + $Regression.run.externalModelCalls + $Agent.run.externalCalls
if ($ExternalCalls -ne 0) { throw 'Unexpected external requests' }
if ((Get-Content -Raw -LiteralPath (Join-Path $Evidence 'native-worker-smoke.log')) -notmatch 'pipe close verified') { throw 'Missing UIA worker evidence' }
$Secrets = Get-Content -Raw -LiteralPath (Join-Path $Evidence 'secret-scan.json') | ConvertFrom-Json
if ($Secrets.scopes.archive.status -ne 'VERIFIED' -or $Secrets.scopes.archive.sha256 -ne $Package.archiveSha256) { throw 'Archive secret scan is missing or mismatched' }

$Residue = @(Get-CimInstance Win32_Process -Filter "Name='Asimi.exe' OR Name='AsimiControlHost.exe' OR Name='AsimiUIAWorker.exe' OR Name='providerfixture.exe' OR Name='agentfixture.exe' OR Name='msedgewebview2.exe'" | Where-Object {
    ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($Repo + [IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) -or
    ($_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -match '\.local-validation[\\/]asimi[\\/]')
})
if ($Residue.Count) { throw 'Release verification process residue detected' }

Push-Location $Repo
try {
    $Head = (git rev-parse HEAD).Trim()
    $Branch = (git branch --show-current).Trim()
    if ($Branch -ne 'codex/phase2b-byok' -or $Head -ne '4accd35e6b7a801274e034adc103f59df37e686c' -or @(git diff --cached --name-only).Count) { throw 'Git baseline differs' }
    git diff --check 2>$null
    if ($LASTEXITCODE) { throw 'Git diff whitespace check failed' }
    $Deleted = @(git diff --name-only --diff-filter=D)
    if ($Deleted.Count) { throw 'Tracked files were deleted' }
    git -c core.quotePath=false status --short --untracked-files=all | Set-Content -LiteralPath (Join-Path $Evidence 'git-status.txt')
    git diff --stat 2>$null | Set-Content -LiteralPath (Join-Path $Evidence 'git-diff-stat.txt')
    git -c core.quotePath=false ls-files --others --exclude-standard | Set-Content -LiteralPath (Join-Path $Evidence 'git-untracked.txt')
} finally { Pop-Location }

$Preserved = [ordered]@{
    'Asimi-MVP-0.1-win-x64.zip' = 'docs/mvp01-evidence/package.json'
    'Asimi-0.2-win-x64.zip' = 'docs/0.2/evidence/package.json'
    'Asimi-0.2.1-win-x64.zip' = 'docs/0.2.1/evidence/package.json'
}
foreach ($ArchiveName in $Preserved.Keys) {
    $Baseline = Get-Content -Raw -LiteralPath (Join-Path $Repo $Preserved[$ArchiveName]) | ConvertFrom-Json
    if ((Get-FileHash -LiteralPath (Join-Path $Repo "build/bin/$ArchiveName")).Hash -ne $Baseline.archiveSha256) { throw 'A historical archive changed' }
}

[ordered]@{
    passed=$true; archiveSha256=$Package.archiveSha256; binarySha256=$Package.mainSha256; archiveFiles=$Package.fileCount;
    nativeAssertions=($Native.run.stages | Measure-Object assertions -Sum).Sum;
    regressionAssertions=($Regression.run.stages | Measure-Object assertions -Sum).Sum;
    agentAssertions=($Agent.run.stages | Measure-Object assertions -Sum).Sum;
    externalModelCalls=$ExternalCalls; legacyProfiles=$Native.run.legacySourceProfilesUnchanged; ordinaryCycles=$Ordinary.run.cycles.Count;
    diagnostics1412=(@($Native.run.stages) + @($Regression.run.stages) | Measure-Object diagnostic1412 -Sum).Sum;
    diagnostic1412Conclusion='CHARACTERIZED'; processResidue=0; oldArchivesPreserved=$Preserved.Count;
    branch=$Branch; head=$Head; staged=0; trackedDeletions=$Deleted;
    applicationLicensePresent=(Test-Path -LiteralPath (Join-Path $Repo 'LICENSE'));
    native=$Native.name; regression=$Regression.name; agent=$Agent.name; ordinary=$Ordinary.name
} | ConvertTo-Json -Depth 5 | Tee-Object -FilePath (Join-Path $Evidence 'final-checks.json')
