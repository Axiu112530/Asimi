$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/env.ps1"
$evidence = Join-Path $Phase0Root 'docs/evidence'
$binary = Join-Path $PSScriptRoot 'bin/wails-probe.exe'
$runDir = Join-Path $PSScriptRoot ('bin/wails-run-' + [Guid]::NewGuid().ToString('N'))
Push-Location "$PSScriptRoot/wails"
try {
    & go build -o $binary .
    if ($LASTEXITCODE) { throw "Wails build failed: $LASTEXITCODE" }
    $runs = @()
    foreach ($stage in @(1,2)) {
        $child = Start-Process -FilePath $binary -ArgumentList @($stage,$evidence,$runDir) -PassThru -WindowStyle Hidden -RedirectStandardOutput "$evidence/wails-stage$stage.stdout.log" -RedirectStandardError "$evidence/wails-stage$stage.stderr.log"
        if (!$child.WaitForExit(25000)) { $child.Kill(); throw "Wails stage $stage timed out" }
        $runs += [pscustomobject]@{stage=$stage;exitCode=$child.ExitCode}
        if ($child.ExitCode) { Get-Content -LiteralPath "$evidence/wails-stage$stage.stderr.log" -Tail 20; throw "Wails stage $stage failed: $($child.ExitCode)" }
        Get-Content -LiteralPath "$evidence/wails-stage$stage.json"
    }
    [pscustomobject]@{freshDatabase=$true;separateProcesses=$true;runs=$runs} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath "$evidence/wails-run.json"
} finally { Pop-Location }
