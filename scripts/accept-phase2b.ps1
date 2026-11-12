param([switch]$SkipBuild, [switch]$Visual)
$ErrorActionPreference = 'Stop'
$Phase2BRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$Phase2BRoot/validation/phase0/env.ps1"
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$runId = [Guid]::NewGuid().ToString('N')
$dataRoot = Join-Path $Phase2BRoot ".local-validation/phase2b/native-$runId"
$evidence = Join-Path $Phase2BRoot "docs/phase2b-evidence/native-$runId"
New-Item -ItemType Directory -Force -Path $dataRoot,$evidence | Out-Null
$env:TEMP = $dataRoot
$env:TMP = $dataRoot
Push-Location $Phase2BRoot
$fixture = $null
try {
    if (!$SkipBuild) { & "$PSScriptRoot/build.ps1" }
    $fixtureBinary = Join-Path $Phase2BRoot 'build/bin/phase2b/providerfixture.exe'
    & go build -trimpath -o $fixtureBinary ./validation/phase2b/providerfixture
    if ($LASTEXITCODE) { throw 'Fixture build failed' }
    $ready = Join-Path $dataRoot 'fixture-url.txt'
    $fixture = Start-Process -FilePath $fixtureBinary -ArgumentList @('--ready-file',('"{0}"' -f $ready)) -WindowStyle Hidden -PassThru -RedirectStandardError "$evidence/fixture.stderr.log" -RedirectStandardOutput "$evidence/fixture.stdout.log"
    for ($i=0; $i -lt 100 -and !(Test-Path -LiteralPath $ready); $i++) { Start-Sleep -Milliseconds 50 }
    $fixtureURL = Get-Content -LiteralPath $ready -Raw
    if (!$fixtureURL.StartsWith('http://127.0.0.1:')) { throw 'Fixture must use loopback' }
    $binary = Join-Path $Phase2BRoot $(if ($SkipBuild) { 'build/bin/Asimi.exe' } else { 'build/bin/asimi-0.2.1/Asimi.exe' })
    $results = @()
    foreach ($stage in @(5,6,1,2)) {
        $profile = if ($stage -ge 5) { Join-Path $dataRoot 'byok' } else { Join-Path $dataRoot 'phase1-regression' }
        $report = Join-Path $evidence "stage-$stage.json"
        $arguments = @('--data-dir',('"{0}"' -f $profile),'--acceptance','--acceptance-stage',$stage,'--acceptance-report',('"{0}"' -f $report))
        if ($stage -ge 5) { $arguments += @('--acceptance-fixture',$fixtureURL) }
        if ($Visual -and $stage -eq 6) { $arguments += @('--acceptance-debug-port','9456') }
        $started = [DateTime]::UtcNow
        $child = Start-Process -FilePath $binary -ArgumentList $arguments -WindowStyle Hidden -PassThru -RedirectStandardOutput "$evidence/stage-$stage.stdout.log" -RedirectStandardError "$evidence/stage-$stage.stderr.log"
        if ($Visual -and $stage -eq 6) {
            & node "$PSScriptRoot/capture-app.cjs" 9456 $evidence
            if ($LASTEXITCODE) { throw 'Own-WebView rendering check failed' }
        }
        if (!$child.WaitForExit(60000)) {
            $null = $child.CloseMainWindow()
            if (!$child.WaitForExit(10000)) { throw "Application still running: PID $($child.Id); no forced cleanup performed" }
            throw "Stage $stage timed out"
        }
        $result = Get-Content -LiteralPath $report -Raw | ConvertFrom-Json
        $diagnostics = @(Select-String -LiteralPath "$evidence/stage-$stage.stderr.log" -Pattern 'Chrome_WidgetWin_0.*1412').Count
        Copy-Item -LiteralPath "$profile/runtime.log" -Destination "$evidence/stage-$stage.runtime.log"
        $dbHandle = [IO.File]::Open((Join-Path $profile 'asimi.db'),[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::None)
        $dbHandle.Dispose()
        $runtimeText = Get-Content -LiteralPath "$evidence/stage-$stage.runtime.log" -Raw
        $cursor = -1
        foreach($marker in @('lifecycle.shutdown_requested','lifecycle.storage_closed','lifecycle.registry_closed','lifecycle.native_close_requested','lifecycle.window_closing','lifecycle.native_window_removed','lifecycle.wails_shutdown','lifecycle.wails_post_shutdown','lifecycle.run_returned')) {
            $cursor = $runtimeText.IndexOf(('"msg":"{0}"' -f $marker),$cursor+1,[StringComparison]::Ordinal)
            if($cursor -lt 0){throw "Stage $stage lifecycle marker missing or out of order: $marker"}
        }
        $results += [ordered]@{stage=$stage;pid=$child.Id;startedUtc=$started.ToString('o');exitObservedUtc=[DateTime]::UtcNow.ToString('o');exitCode=$child.ExitCode;passed=$result.passed;assertions=@($result.checks.PSObject.Properties).Count;diagnostic1412=$diagnostics;databaseExclusiveRead=$true;orderedShutdown=$true}
        if ($child.ExitCode -ne 0 -or !$result.passed) { throw "Stage $stage failed: $($result.error)" }
        $child.Dispose()
    }
    $key = 'fixture-secret'
    $dbBytes = [IO.File]::ReadAllBytes((Join-Path $dataRoot 'byok/asimi.db'))
    if ([Text.Encoding]::UTF8.GetString($dbBytes).Contains($key)) { throw 'Dummy credential plaintext present in SQLite' }
    $previous = @(Get-Content -LiteralPath '.local-validation/phase2b/prior-evidence-baseline.json' -Raw | ConvertFrom-Json)
    foreach ($entry in $previous) {
        if ((Get-FileHash -LiteralPath $entry.path -Algorithm SHA256).Hash -ne $entry.sha256) { throw "Earlier evidence changed: $($entry.path)" }
    }
    $summary = [ordered]@{phase='2B';modelSource='LOOPBACK_PROTOCOL_FIXTURE_NOT_REAL_MODEL';externalModelCalls=0;binarySHA256=(Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash;dataRoot=$dataRoot;results=$results;priorEvidencePreserved=$previous.Count;credentialPlaintextAbsent=$true}
    $summary | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath "$evidence/summary.json" -Encoding utf8
    $summary | ConvertTo-Json -Depth 6
    "Evidence: $evidence"
} finally {
    if ($fixture -and !$fixture.HasExited -and (Test-Path -LiteralPath "$dataRoot/fixture-url.txt")) {
        $fixtureURL = Get-Content -LiteralPath "$dataRoot/fixture-url.txt" -Raw
        $shutdownURL = $fixtureURL.Substring(0,$fixtureURL.Length-3) + '/shutdown'
        try { Invoke-WebRequest -Uri $shutdownURL -UseBasicParsing -TimeoutSec 5 | Out-Null; $null=$fixture.WaitForExit(5000) } catch { Write-Warning 'Local fixture did not confirm graceful shutdown' }
    }
    if ($fixture) { $fixture.Dispose() }
    Pop-Location
}
