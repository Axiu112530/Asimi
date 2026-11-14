param(
    [switch] $LocalCandidate,
    [string] $CompilerPath
)

$ErrorActionPreference = 'Stop'
$Repo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Push-Location $Repo
try {
    $Commit = & git rev-parse --verify HEAD
    if ($LASTEXITCODE -ne 0) { throw 'Cannot identify the source commit.' }
    $Status = @(& git status --porcelain --untracked-files=normal)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot check the Git working tree.' }
    if ($Status.Count -and !$LocalCandidate) {
        throw 'Commit the intended release source first, or use -LocalCandidate for a local build.'
    }
    $Info = Get-Content -LiteralPath 'build/windows/asimi-info.json' -Raw | ConvertFrom-Json
    $Version = $Info.info.'0409'.ProductVersion
    if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Expected a numeric Asimi Windows product version.' }
    $ReleaseName = if ($LocalCandidate) { "$Version-local-$(Get-Date -Format 'yyyyMMdd-HHmmss')" } else { $Version }
    $Relative = "build/releases/$ReleaseName"
    $Output = Join-Path $Repo $Relative
    if (Test-Path -LiteralPath $Output) { throw "Build output already exists: $Output" }
    $Assets = Join-Path $Output 'assets'
    $Runtime = Join-Path $Output 'work/runtime'
    $Evidence = Join-Path $Output 'work/evidence'
    New-Item -ItemType Directory -Path $Assets -Force | Out-Null

    & (Join-Path $PSScriptRoot 'build.ps1') -OutputDirectory "$Relative/work/runtime"
    & (Join-Path $PSScriptRoot 'package-asimi.ps1') -OutputDirectory $Assets -RuntimeDirectory $Runtime -EvidenceDirectory $Evidence -DiscardVerificationCopy
    & (Join-Path $PSScriptRoot 'build-installer.ps1') -CompilerPath $CompilerPath -OutputDirectory $Assets -PayloadDirectory $Runtime

    $Names = @("Asimi-$Version-Setup-x64.exe", "Asimi-$Version-win-x64.zip")
    foreach ($Name in $Names) {
        $Artifact = Join-Path $Assets $Name
        $Expected = (Get-FileHash -LiteralPath $Artifact -Algorithm SHA256).Hash + "  $Name"
        if ((Get-Content -LiteralPath "$Artifact.sha256" -Raw).Trim() -ne $Expected) {
            throw "Checksum mismatch: $Name"
        }
    }
    [ordered]@{
        edition = 'community'
        version = $Version
        commit = $Commit
        workingTreeModified = [bool]$Status.Count
        localCandidate = [bool]$LocalCandidate
        platform = 'windows'
        arch = 'amd64'
        assets = @($Names | ForEach-Object {
            @{ file = $_; sha256 = (Get-FileHash -LiteralPath (Join-Path $Assets $_)).Hash }
        })
    } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Output 'build-info.json') -Encoding utf8
    Write-Output "Assets: $Assets"
} finally {
    Pop-Location
}
