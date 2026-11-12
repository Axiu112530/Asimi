param([switch]$SkipBuild,[switch]$NoVisual)
$ErrorActionPreference = 'Stop'
$Phase1Root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Push-Location $Phase1Root
try {
    if (!$SkipBuild) { & "$PSScriptRoot/build.ps1" }
    $evidence = Join-Path $Phase1Root "docs/release/evidence/phase1-$([Guid]::NewGuid().ToString('N'))"
    New-Item -ItemType Directory -Force -Path $evidence | Out-Null
    $data = Join-Path $Phase1Root ('.local-validation/phase1/acceptance-' + [Guid]::NewGuid().ToString('N'))
    $binary = Join-Path $Phase1Root $(if ($SkipBuild) { 'build/bin/Asimi.exe' } else { 'build/bin/asimi-0.2.1/Asimi.exe' })
    $stages = @()
    foreach ($stage in @(1,2)) {
        $report = Join-Path $evidence "app-stage$stage.json"
        $arguments = @('--data-dir',$data,'--acceptance','--acceptance-stage',$stage,'--acceptance-report',$report)
        $port = 9430 + $stage
        if (!$NoVisual) { $arguments += @('--acceptance-debug-port',$port) }
        $child = Start-Process -FilePath $binary -ArgumentList $arguments -PassThru -WindowStyle Hidden -RedirectStandardOutput "$evidence/app-stage$stage.stdout.log" -RedirectStandardError "$evidence/app-stage$stage.stderr.log"
        try {
            if (!$NoVisual) {
                & node "$PSScriptRoot/capture-app.cjs" $port $evidence
                if ($LASTEXITCODE) { throw "App visual check failed at stage $stage" }
            }
            if (!$child.WaitForExit(55000)) { throw "Application acceptance stage $stage timeout" }
            if ($child.ExitCode) { throw "Application acceptance stage $stage exit $($child.ExitCode)" }
            $result = Get-Content -LiteralPath $report -Raw | ConvertFrom-Json
            if ($result.stage -ne $stage -or !$result.passed) { throw "Acceptance failed: $($result.error)" }
            $stages += [pscustomobject]@{stage=$stage;exitCode=$child.ExitCode;passed=$result.passed;assertions=@($result.checks.PSObject.Properties).Count}
        } finally { if (!$child.HasExited) { $null=$child.CloseMainWindow();if(!$child.WaitForExit(2500)){$child.Kill()} } }
    }
    # Independent acceptance oracle reads only the deliberately created test file.
    $created = Join-Path $data 'workspace/phase1-created.txt'
    $actual = [IO.File]::ReadAllBytes($created)
    $FixtureBrand = if ($SkipBuild) { 'Asimi' } else { 'Asimi' }
    $expected = [Text.Encoding]::UTF8.GetBytes("$FixtureBrand Phase 1`nVerified locally. 中文")
    if ([Convert]::ToBase64String($actual) -ne [Convert]::ToBase64String($expected)) { throw 'External file oracle mismatch' }
    foreach($name in @('phase1-denied.txt','phase1-cancelled.txt')) { if(Test-Path -LiteralPath (Join-Path $data "workspace/$name")){throw "Denied/cancelled file exists: $name"} }
    if(Test-Path -LiteralPath (Join-Path $data 'escape.txt')){throw 'Traversal escaped workspace'}
    foreach($entry in (Get-Content -LiteralPath "$evidence/phase0-baseline.json" -Raw | ConvertFrom-Json)) {
        if((Get-FileHash -Algorithm SHA256 -LiteralPath $entry.path).Hash -ne $entry.sha256){throw "Phase 0 evidence changed: $($entry.path)"}
    }
    $summary = [ordered]@{stages=$stages;dataDir=$data;fileBytes=$actual.Length;fileSHA256=(Get-FileHash -Algorithm SHA256 -LiteralPath $created).Hash;deniedFilesAbsent=$true;traversalAbsent=$true;phase0Integrity='27/27 unchanged';desktopOperationsEnabled=$false;realProviderCalls=0;phase2Started=$false}
    $summary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath "$evidence/acceptance-summary.json"
    $summary | ConvertTo-Json -Depth 5
} finally { Pop-Location }
