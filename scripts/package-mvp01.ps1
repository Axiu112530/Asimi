param([string]$Bin = 'build/bin/mvp01-portable')
$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Directory = [IO.Path]::GetFullPath((Join-Path $Repo $Bin))
$OutputRoot = Join-Path $Repo 'build/bin'
if (!$Directory.StartsWith($OutputRoot + [IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Portable directory must be under build/bin' }
$Files = @('Asimi.exe','AsimiControlHost.exe','AsimiControlHost.exe.config','AsimiUIAWorker.exe','AsimiUIAWorker.exe.config')
foreach ($Name in $Files) { if (!(Test-Path -LiteralPath (Join-Path $Directory $Name) -PathType Leaf)) { throw "Missing $Name" } }
$Documents = @('INSTALLATION-GUIDE.md','USER-GUIDE.md','SECURITY-MODEL.md','PRODUCT-ARCHITECTURE.md','MVP-0.1-REPORT.md','MVP-0.1-ACCEPTANCE.md')
foreach ($Name in $Documents) { Copy-Item -LiteralPath (Join-Path $Repo "docs/$Name") -Destination (Join-Path $Directory $Name) }
$Files += $Documents
$Manifest = @($Files | ForEach-Object { "{0}  {1}" -f (Get-FileHash -LiteralPath (Join-Path $Directory $_) -Algorithm SHA256).Hash,$_; })
$ManifestPath = Join-Path $Directory 'SHA256SUMS.txt'
[IO.File]::WriteAllLines($ManifestPath,$Manifest,[Text.UTF8Encoding]::new($false))
$Paths = @($Files | ForEach-Object { Join-Path $Directory $_ }) + @($ManifestPath)
$Archive = Join-Path $OutputRoot 'Asimi-MVP-0.1-win-x64.zip'
Compress-Archive -LiteralPath $Paths -DestinationPath $Archive -Force
Get-FileHash -LiteralPath $Archive -Algorithm SHA256
