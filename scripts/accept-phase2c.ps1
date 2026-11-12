param([switch]$SkipBuild, [switch]$ScreenOnly)
$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$root/validation/phase0/env.ps1"
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$runId = [Guid]::NewGuid().ToString('N')
$dataRoot = Join-Path $root ".local-validation/phase2c/native-$runId"
$evidence = Join-Path $root "docs/phase2c-evidence/native-$runId"
New-Item -ItemType Directory -Path $dataRoot | Out-Null
$env:TEMP = $dataRoot
$env:TMP = $dataRoot

function Confirm-PrivacyScan([string]$Path) {
    $raw = [Text.Encoding]::GetEncoding(28591).GetString([IO.File]::ReadAllBytes($Path))
    $imagePattern = 'data:image/[^,\s]+;base64,|iVBORw0KGgo[A-Za-z0-9+/]{16}|/9j/[A-Za-z0-9+/]{16}|\x89PNG\x0D\x0A\x1A\x0A|\xFF\xD8\xFF[\xE0\xE1\xDB]'
    if ($raw -match $imagePattern -or $raw.Contains('fixture-secret')) {
        throw "Image bytes or dummy credential present in $Path"
    }
}

function Confirm-PreservedEvidence {
    $previous = Get-Content -LiteralPath '.local-validation/phase2c/prior-evidence-baseline.json' -Raw | ConvertFrom-Json
    if ($previous.Count -ne 277) { throw "Expected 277 earlier evidence hashes, found $($previous.Count)" }
    foreach ($entry in $previous) {
        if ((Get-FileHash -LiteralPath $entry.path -Algorithm SHA256).Hash -ne $entry.sha256) {
            throw "Earlier evidence changed: $($entry.path)"
        }
    }
    return $previous.Count
}

Push-Location $root
$fixture = $null
$fixtureURL = $null
try {
    "Run data: $dataRoot"
    $null = Confirm-PreservedEvidence
    if (!$SkipBuild) { & "$PSScriptRoot/build.ps1" }
    $fixtureBinary = Join-Path $dataRoot 'providerfixture.exe'
    & go build -trimpath -o $fixtureBinary ./validation/phase2b/providerfixture
    if ($LASTEXITCODE) { throw 'Loopback fixture build failed' }

    $ready = Join-Path $dataRoot 'fixture-url.txt'
    $auditPath = Join-Path $dataRoot 'provider-screen-audit.jsonl'
    $fixtureArgs = @('--ready-file', ('"{0}"' -f $ready), '--screen-audit-file', ('"{0}"' -f $auditPath))
    $fixtureLaunch = @{FilePath=$fixtureBinary; ArgumentList=$fixtureArgs; WindowStyle='Hidden'; PassThru=$true
        RedirectStandardOutput="$dataRoot/fixture.stdout.log"; RedirectStandardError="$dataRoot/fixture.stderr.log"}
    $fixture = Start-Process @fixtureLaunch
    for ($i = 0; $i -lt 100 -and !(Test-Path -LiteralPath $ready) -and !$fixture.HasExited; $i++) {
        Start-Sleep -Milliseconds 50
    }
    $candidate = Get-Content -LiteralPath $ready -Raw
    if ($candidate -notmatch '^http://127\.0\.0\.1:[1-9]\d{0,4}/v1$') { throw 'Fixture URL must use loopback' }
    $fixtureURL = $candidate
    $binary = Join-Path $root $(if ($SkipBuild) { 'build/bin/Asimi.exe' } else { 'build/bin/asimi-0.2.1/Asimi.exe' })
    $results = @()
    $evidenceFiles = @()
    $shutdownMarkers = @('lifecycle.navigation_completed', 'lifecycle.shutdown_requested', 'lifecycle.storage_closed',
        'lifecycle.registry_closed', 'lifecycle.native_close_requested', 'lifecycle.window_closing',
        'lifecycle.native_window_removed', 'lifecycle.wails_shutdown', 'lifecycle.wails_post_shutdown', 'lifecycle.run_returned')
    $stages = if ($ScreenOnly) { @(7) } else { @(7, 1, 2, 3, 5, 6) }
    foreach ($stage in $stages) {
        $profileName = switch ($stage) {
            7 { 'screen' }
            5 { 'phase2b-regression' }
            6 { 'phase2b-regression' }
            3 { 'phase1-fresh' }
            default { 'phase1-regression' }
        }
        $profile = Join-Path $dataRoot $profileName
        $report = Join-Path $dataRoot "stage-$stage.json"
        $stderr = Join-Path $dataRoot "stage-$stage.stderr.log"
        $stdout = Join-Path $dataRoot "stage-$stage.stdout.log"
        $stageRuntime = Join-Path $dataRoot "stage-$stage.runtime.log"
        $runtimePath = Join-Path $profile 'runtime.log'
        $previousRuntimeBytes = if (Test-Path -LiteralPath $runtimePath) { (Get-Item -LiteralPath $runtimePath).Length } else { 0 }
        $arguments = @('--data-dir', ('"{0}"' -f $profile), '--acceptance', '--acceptance-stage', $stage,
            '--acceptance-report', ('"{0}"' -f $report))
        if ($stage -in @(5, 6, 7)) { $arguments += @('--acceptance-fixture', $fixtureURL) }
        $started = [DateTime]::UtcNow
        $launch = @{FilePath=$binary; ArgumentList=$arguments; WindowStyle='Hidden'; PassThru=$true
            RedirectStandardOutput=$stdout; RedirectStandardError=$stderr}
        $child = Start-Process @launch
        try {
            # Keep the redirected child's process handle through exit on Windows PowerShell 5.1.
            $null = $child.Handle
            if (!$child.WaitForExit(95000)) {
                $null = $child.CloseMainWindow()
                if (!$child.WaitForExit(10000)) { throw "Stage $stage still running as PID $($child.Id)" }
                throw "Stage $stage timed out"
            }
            if (!(Test-Path -LiteralPath $report)) { throw "Stage $stage produced no report (exit $($child.ExitCode))" }
            Confirm-PrivacyScan $report
            $result = Get-Content -LiteralPath $report -Raw | ConvertFrom-Json
            if ($result.stage -ne $stage -or !$result.passed -or $child.ExitCode -ne 0) {
                throw "Stage $stage failed (exit $($child.ExitCode)): $($result.error)"
            }
            if ($stage -eq 7) { $screenResult = $result }
            $dbPath = Join-Path $profile 'asimi.db'
            $handle = [IO.File]::Open($dbPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
            $handle.Dispose()
            $runtime = [IO.File]::ReadAllBytes($runtimePath)
            $runtimeText = [Text.Encoding]::UTF8.GetString($runtime, $previousRuntimeBytes, $runtime.Length - $previousRuntimeBytes)
            $cursor = -1
            foreach ($marker in $shutdownMarkers) {
                $cursor = $runtimeText.IndexOf(('"msg":"{0}"' -f $marker), $cursor + 1, [StringComparison]::Ordinal)
                if ($cursor -lt 0) { throw "Stage $stage lifecycle marker missing or out of order: $marker" }
            }
            $diagnostics = [regex]::Matches($runtimeText + (Get-Content -LiteralPath $stdout -Raw) +
                (Get-Content -LiteralPath $stderr -Raw), '(?m)Chrome_WidgetWin_0[^\r\n]*1412').Count
            if ($diagnostics -ne 0) { throw "Stage $stage emitted $diagnostics Chrome_WidgetWin_0 1412 diagnostics" }
            [IO.File]::WriteAllText($stageRuntime, $runtimeText)
            foreach ($file in @($stageRuntime, $stdout, $stderr)) { Confirm-PrivacyScan $file }
            if ($stage -eq 7) {
                foreach ($file in @($dbPath, "$dbPath-wal", "$dbPath-shm")) {
                    if (Test-Path -LiteralPath $file) { Confirm-PrivacyScan $file }
                }
                $attachments = Join-Path $profile 'attachments'
                if ((Test-Path -LiteralPath $attachments) -and @(Get-ChildItem -LiteralPath $attachments -File -Recurse).Count) {
                    throw 'Screen analysis created a persisted attachment'
                }
                if (@(Get-ChildItem -LiteralPath $profile -File -Force | Where-Object Extension -In '.png','.jpg','.jpeg','.gif','.webp','.bmp').Count) {
                    throw 'Screen capture created a top-level image file'
                }
            }
            $evidenceFiles += @($report, $stageRuntime)
            $results += [ordered]@{stage=$stage; pid=$child.Id; startedUtc=$started.ToString('o')
                exitObservedUtc=[DateTime]::UtcNow.ToString('o'); exitCode=$child.ExitCode; passed=$true
                assertions=@($result.checks.PSObject.Properties).Count; diagnostic1412=$diagnostics
                databaseExclusiveRead=$true; orderedShutdown=$true}
        } finally { $child.Dispose() }
    }

    Confirm-PrivacyScan $auditPath
    $audit = @(Get-Content -LiteralPath $auditPath | ForEach-Object { $_ | ConvertFrom-Json })
    $expected = $screenResult.details.sentImage
    if ($audit.Count -ne 2 -or $audit[0].kind -ne 'screen' -or $audit[0].imageParts -ne 1 -or
        $audit[0].sha256 -ne $expected.sha256 -or $audit[0].bytes -ne $expected.byteSize -or
        $audit[0].width -ne $expected.width -or $audit[0].height -ne $expected.height -or
        $audit[1].kind -ne 'followup' -or $audit[1].imageParts -ne 0) {
        throw 'Loopback audit did not match the approved screen request and text-only follow-up'
    }
    if (!$ScreenOnly) {
        $workspace = Join-Path $dataRoot 'phase1-regression/workspace'
        $FixtureBrand = if ($SkipBuild) { 'Asimi' } else { 'Asimi' }
        $expectedText = "$FixtureBrand Phase 1" + [char]10 + 'Verified locally. ' + [char]0x4E2D + [char]0x6587
        $actualText = [IO.File]::ReadAllText((Join-Path $workspace 'phase1-created.txt'))
        if ($actualText -cne $expectedText) { throw 'Phase 1 independent file oracle mismatch' }
        foreach ($path in @('workspace/phase1-denied.txt', 'workspace/phase1-cancelled.txt', 'escape.txt')) {
            if (Test-Path -LiteralPath (Join-Path $dataRoot "phase1-regression/$path")) {
                throw "Unexpected Phase 1 regression file: $path"
            }
        }
    }
    $summary = [ordered]@{phase='2C'; modelSource='LOOPBACK_PROTOCOL_FIXTURE'; captureSource='SYNTHETIC_IN_MEMORY'
        externalModelCalls=0; binarySHA256=(Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash
        results=$results; exactApprovedImageAtProvider=$true; followupImageParts=0
        screenBytesAbsentFromSQLiteAndLogs=$true; persistedScreenAttachments=0; phase1FileOraclePassed=!$ScreenOnly
        priorEvidencePreserved=(Confirm-PreservedEvidence)}
    New-Item -ItemType Directory -Path $evidence | Out-Null
    foreach ($file in ($evidenceFiles + @($auditPath))) { Copy-Item -LiteralPath $file -Destination $evidence }
    $summary | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath "$evidence/summary.json" -Encoding utf8
    $summary | ConvertTo-Json -Depth 6
    "Evidence: $evidence"
} finally {
    if ($fixture -and !$fixture.HasExited -and $fixtureURL) {
        $shutdown = ([uri]$fixtureURL).GetLeftPart([System.UriPartial]::Authority) + '/shutdown'
        try {
            Invoke-WebRequest -Uri $shutdown -UseBasicParsing -TimeoutSec 5 | Out-Null
            $null = $fixture.WaitForExit(5000)
        } catch { Write-Warning 'Loopback fixture did not confirm graceful shutdown' }
    }
    if ($fixture) { $fixture.Dispose() }
    Pop-Location
}
