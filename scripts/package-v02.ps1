param([string]$Bin = 'build/bin/v02-portable')

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Output = Join-Path $Repo 'build/bin'
$Directory = [IO.Path]::GetFullPath((Join-Path $Repo $Bin))
if (!$Directory.StartsWith($Output+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Portable directory must be inside build/bin' }
$Files = @('Asimi.exe','AsimiControlHost.exe','AsimiControlHost.exe.config','AsimiUIAWorker.exe','AsimiUIAWorker.exe.config')
foreach ($Name in $Files) {
    if (!(Test-Path -LiteralPath (Join-Path $Directory $Name) -PathType Leaf)) { throw "Missing $Name" }
}
$Docs = @('START-HERE.md','DEVELOPER-AGENT.md','AGENT-ORCHESTRATION.md','SECURITY-REVIEW.md','COMPUTER-MANUAL-ACCEPTANCE.md')
foreach ($Name in $Docs) { Copy-Item -LiteralPath (Join-Path $Repo "docs/0.2/$Name") -Destination (Join-Path $Directory $Name) }
$Files += $Docs
$ManifestPath = Join-Path $Directory 'SHA256SUMS.txt'
$Manifest = @($Files | ForEach-Object { '{0}  {1}' -f (Get-FileHash -LiteralPath (Join-Path $Directory $_)).Hash,$_ })
[IO.File]::WriteAllLines($ManifestPath,$Manifest,[Text.UTF8Encoding]::new($false))
$Archive = Join-Path $Output 'Asimi-0.2-win-x64.zip'
$Paths = @($Files | ForEach-Object { Join-Path $Directory $_ }) + @($ManifestPath)
Compress-Archive -LiteralPath $Paths -DestinationPath $Archive -Force
$Extracted = Join-Path $Output 'v02-extracted'
Expand-Archive -LiteralPath $Archive -DestinationPath $Extracted -Force
foreach ($Row in (Get-Content -LiteralPath (Join-Path $Extracted 'SHA256SUMS.txt'))) {
    $Hash,$Name = $Row -split '  ',2
    if ((Get-FileHash -LiteralPath (Join-Path $Extracted $Name)).Hash -ne $Hash) { throw "Archive member mismatch: $Name" }
}
$Evidence = Join-Path $Repo 'docs/0.2/evidence'
New-Item -ItemType Directory -Force -Path $Evidence | Out-Null
[ordered]@{passed=$true;fileCount=$Files.Count+1;archiveSha256=(Get-FileHash -LiteralPath $Archive).Hash;mainSha256=(Get-FileHash -LiteralPath (Join-Path $Extracted 'Asimi.exe')).Hash} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $Evidence 'package.json') -Encoding utf8
Get-Content -LiteralPath (Join-Path $Evidence 'package.json')
