param([string]$Bin = 'build/bin/v021-portable')

$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Output = Join-Path $Repo 'build/bin'
$Directory = [IO.Path]::GetFullPath((Join-Path $Repo $Bin))
if (!$Directory.StartsWith($Output+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Build directory must be inside build/bin' }
$Files = @('DeskPilotAI.exe','DeskPilotControlHost.exe','DeskPilotControlHost.exe.config','DeskPilotUIAWorker.exe','DeskPilotUIAWorker.exe.config')
$Docs = @('START-HERE.md','UPGRADE.md','THIRD-PARTY-NOTICES.txt','KNOWN-LIMITATIONS.md','SECURITY-REVIEW.md','COMPUTER-ACCEPTANCE.md')
foreach ($Name in $Docs) {
    Copy-Item -LiteralPath (Join-Path $Repo "docs/0.2.1/$Name") -Destination (Join-Path $Directory $Name)
}
$Files += $Docs
$Manifest = foreach ($Name in $Files) {
    '{0}  {1}' -f (Get-FileHash -LiteralPath (Join-Path $Directory $Name) -Algorithm SHA256).Hash,$Name
}
$ManifestPath = Join-Path $Directory 'SHA256SUMS.txt'
[IO.File]::WriteAllLines($ManifestPath,$Manifest,[Text.UTF8Encoding]::new($false))
$Archive = Join-Path $Output 'DeskPilot-Community-0.2.1-win-x64.zip'
$Paths = @($Files | ForEach-Object { Join-Path $Directory $_ }) + @($ManifestPath)
Compress-Archive -LiteralPath $Paths -DestinationPath $Archive -Force
$Extracted = Join-Path $Output "v021-extracted-$([Guid]::NewGuid().ToString('N'))"
Expand-Archive -LiteralPath $Archive -DestinationPath $Extracted
$Members = @(Get-ChildItem -LiteralPath $Extracted -Force)
if ($Members.Count -ne $Paths.Count -or @($Members | Where-Object { $_.PSIsContainer }).Count) { throw 'Unexpected archive members' }
foreach ($Row in Get-Content -LiteralPath (Join-Path $Extracted 'SHA256SUMS.txt')) {
    $Hash,$Name = $Row -split '  ',2
    if ($Name -notin $Files -or (Get-FileHash -LiteralPath (Join-Path $Extracted $Name) -Algorithm SHA256).Hash -ne $Hash) { throw "Archive member mismatch: $Name" }
}
$Evidence = Join-Path $Repo 'docs/0.2.1/evidence'
New-Item -ItemType Directory -Force -Path $Evidence | Out-Null
$Result = [ordered]@{passed=$true;fileCount=$Paths.Count;archiveSha256=(Get-FileHash -LiteralPath $Archive).Hash;mainSha256=(Get-FileHash -LiteralPath (Join-Path $Extracted 'DeskPilotAI.exe')).Hash;extracted=(Split-Path -Leaf $Extracted)}
[IO.File]::WriteAllText($Archive+'.sha256',('{0}  {1}' -f $Result.archiveSha256,(Split-Path -Leaf $Archive))+"`n",[Text.UTF8Encoding]::new($false))
$Result | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $Evidence 'package.json') -Encoding utf8
$Result | ConvertTo-Json
