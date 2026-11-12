param([string]$DataDir)
$ErrorActionPreference = 'Stop'
$Phase1Root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$binary = Join-Path $Phase1Root 'build/bin/asimi-0.2.1/Asimi.exe'
if (!(Test-Path -LiteralPath $binary)) { & "$PSScriptRoot/build.ps1" }
if ($DataDir) { & $binary --data-dir $DataDir } else { & $binary }
