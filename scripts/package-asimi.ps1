param([string]$OutputDirectory, [string]$RuntimeDirectory, [string]$EvidenceDirectory)

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Output = if ($OutputDirectory) { $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDirectory) } else { Join-Path $Repo 'build/bin' }
$Portable = if ($RuntimeDirectory) { $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($RuntimeDirectory) } else { Join-Path $Output 'asimi-0.2.1' }
$Evidence = if ($EvidenceDirectory) { $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($EvidenceDirectory) } else { Join-Path $Repo 'docs/release/evidence' }
New-Item -ItemType Directory -Force -Path $Output,$Evidence | Out-Null

$Runtime = @('Asimi.exe','AsimiControlHost.exe','AsimiControlHost.exe.config','AsimiUIAWorker.exe','AsimiUIAWorker.exe.config')
$Docs = @('INSTALLATION-GUIDE.md','USER-GUIDE.md','UPGRADE-GUIDE.md','KNOWN-LIMITATIONS.md','THIRD-PARTY-NOTICES.md')
foreach ($Name in $Docs) {
    Copy-Item -LiteralPath (Join-Path $Repo "docs/release/$Name") -Destination (Join-Path $Portable $Name)
}
Copy-Item -LiteralPath (Join-Path $Repo 'PRIVACY.md') -Destination (Join-Path $Portable 'PRIVACY.md')
Copy-Item -LiteralPath (Join-Path $Repo 'LICENSE') -Destination (Join-Path $Portable 'LICENSE')
$Files = $Runtime + $Docs + @('PRIVACY.md','LICENSE')
$Manifest = foreach ($Name in $Files) {
    '{0}  {1}' -f (Get-FileHash -LiteralPath (Join-Path $Portable $Name) -Algorithm SHA256).Hash,$Name
}
$ManifestName = 'SHA256SUMS.txt'
[IO.File]::WriteAllLines((Join-Path $Portable $ManifestName),$Manifest,[Text.UTF8Encoding]::new($false))
$Archive = Join-Path $Output 'Asimi-0.2.1-win-x64.zip'
$Members = $Files + @($ManifestName)
Compress-Archive -LiteralPath @($Members | ForEach-Object { Join-Path $Portable $_ }) -DestinationPath $Archive -Force
$Extracted = Join-Path $Output "asimi-extracted-$([Guid]::NewGuid().ToString('N'))"
Expand-Archive -LiteralPath $Archive -DestinationPath $Extracted

$ExtractedMembers = @(Get-ChildItem -LiteralPath $Extracted -Force)
if ($ExtractedMembers.Count -ne $Members.Count -or @($ExtractedMembers | Where-Object {$_.PSIsContainer -or $_.Name -notin $Members}).Count) {
    throw 'Unexpected files in the portable archive'
}
foreach ($Row in Get-Content -LiteralPath (Join-Path $Extracted $ManifestName)) {
    $Hash,$Name = $Row -split '  ',2
    if ($Name -notin $Files -or (Get-FileHash -LiteralPath (Join-Path $Extracted $Name) -Algorithm SHA256).Hash -ne $Hash) {
        throw "Archive member hash mismatch: $Name"
    }
}
$ArchiveHash = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash
[IO.File]::WriteAllLines($Archive + '.sha256',@("$ArchiveHash  $(Split-Path -Leaf $Archive)"),[Text.UTF8Encoding]::new($false))
$Version = (Get-Item -LiteralPath (Join-Path $Extracted 'Asimi.exe')).VersionInfo
if ($Version.ProductName -cne 'Asimi' -or $Version.ProductVersion -cne '0.2.1' -or $Version.OriginalFilename -cne 'Asimi.exe') {
    throw 'Unexpected executable branding or version'
}
[ordered]@{
    passed=$true; product=$Version.ProductName; version=$Version.ProductVersion;
    archiveSha256=$ArchiveHash; mainSha256=(Get-FileHash -LiteralPath (Join-Path $Extracted 'Asimi.exe')).Hash;
    fileCount=$Members.Count; members=$Members; extracted=(Split-Path -Leaf $Extracted)
} | ConvertTo-Json | Tee-Object -FilePath (Join-Path $Evidence 'package.json')
