param(
    [string]$CompilerPath,
    [string]$OutputDirectory,
    [string]$PayloadDirectory
)

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Output = Join-Path $Repo 'build/bin'
if (-not $OutputDirectory) {
    $OutputDirectory = $Output
}
$OutputDirectory = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDirectory)

$AppInfo = Get-Content -LiteralPath (Join-Path $Repo 'build/windows/asimi-info.json') -Raw | ConvertFrom-Json
$AppVersion = $AppInfo.info.'0409'.ProductVersion
$Payload = if ($PayloadDirectory) {
    $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($PayloadDirectory)
} else {
    Join-Path $Output "asimi-$AppVersion"
}
$App = Join-Path $Payload 'Asimi.exe'
$Manifest = Join-Path $Payload 'SHA256SUMS.txt'
if (-not (Test-Path -LiteralPath $App -PathType Leaf) -or -not (Test-Path -LiteralPath $Manifest -PathType Leaf)) {
    throw 'Run scripts/build.ps1 and scripts/package-asimi.ps1 before building the installer.'
}
$Version = (Get-Item -LiteralPath $App).VersionInfo
if ($Version.ProductName -cne 'Asimi' -or $Version.ProductVersion -cne $AppVersion -or $Version.OriginalFilename -cne 'Asimi.exe') {
    throw "Unexpected Asimi executable branding or version in $App."
}

$Members = @{}
foreach ($Row in Get-Content -LiteralPath $Manifest) {
    if ($Row -notmatch '^(?<Hash>[0-9a-fA-F]{64})  (?<Name>[a-zA-Z0-9][a-zA-Z0-9_.-]*)$' -or $Members.ContainsKey($Matches.Name)) {
        throw 'Invalid release checksum manifest. Run scripts/package-asimi.ps1 again.'
    }
    $Name = $Matches.Name
    $Member = Join-Path $Payload $Name
    if (-not (Test-Path -LiteralPath $Member -PathType Leaf) -or (Get-FileHash -LiteralPath $Member -Algorithm SHA256).Hash -ne $Matches.Hash) {
        throw "Release payload checksum mismatch for $Name. Run scripts/package-asimi.ps1 after the app build."
    }
    $Members[$Name] = $true
}
if (-not $Members.ContainsKey('Asimi.exe')) {
    throw 'The release checksum manifest is missing Asimi.exe. Run scripts/package-asimi.ps1 again.'
}

if (-not $CompilerPath) {
    $CompilerPath = Join-Path $Repo '.phase0-tools/innosetup-6.7.4/ISCC.exe'
    if (-not (Test-Path -LiteralPath $CompilerPath -PathType Leaf)) {
        $CompilerPath = (Get-Command ISCC.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
    }
}
if (-not $CompilerPath -or -not (Test-Path -LiteralPath $CompilerPath -PathType Leaf)) {
    throw 'Inno Setup is required. Download the tested 6.7.4 release from https://github.com/jrsoftware/issrc/releases/tag/is-6_7_4 and pass -CompilerPath with the path to ISCC.exe.'
}
$CompilerPath = (Get-Item -LiteralPath $CompilerPath).FullName

$Setup = Join-Path $OutputDirectory "Asimi-$AppVersion-Setup-x64.exe"
$Definition = Join-Path $Repo 'build/windows/asimi.iss'
$Arguments = @("/O$OutputDirectory", "/DAppVersion=$AppVersion", "/DPayloadDir=$Payload")
$ChineseMessages = Join-Path (Split-Path -Parent $CompilerPath) 'Languages/ChineseSimplified.isl'
if (Test-Path -LiteralPath $ChineseMessages -PathType Leaf) {
    $Arguments += '/DIncludeChinese'
}
& $CompilerPath @Arguments $Definition
if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $Setup -PathType Leaf)) {
    throw "Inno Setup compilation failed (exit code $LASTEXITCODE)."
}

$Hash = (Get-FileHash -LiteralPath $Setup -Algorithm SHA256).Hash
[IO.File]::WriteAllLines($Setup + '.sha256', @("$Hash  $(Split-Path -Leaf $Setup)"), [Text.UTF8Encoding]::new($false))
Write-Output "Installer: $Setup"
Write-Output "SHA-256:   $Hash"
