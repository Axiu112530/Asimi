param(
    [ValidateSet('minimal-window', 'minimal-quit', 'phase1', 'app-acceptance', 'app-pending')]
    [string]$Variant,
    [ValidateRange(1, 10)]
    [int]$Runs = 3,
    [string]$AppPath
)
$ErrorActionPreference = 'Stop'
$Phase2ARoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$Phase2ARoot/validation/phase0/env.ps1"
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$binRoot = Join-Path $Phase2ARoot 'build/bin/phase2a'
$runKey = "$Variant-" + (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmss') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
$evidence = Join-Path $Phase2ARoot "docs/phase2a-evidence/lifecycle-$runKey"
$dataRoot = Join-Path $binRoot "runs/$runKey"
New-Item -ItemType Directory -Force -Path $binRoot, $evidence, $dataRoot | Out-Null
if (!$AppPath) {
    if ($Variant -eq 'phase1') { $AppPath = Join-Path $binRoot 'DeskPilotAI-phase1.exe' }
    else { $AppPath = Join-Path $Phase2ARoot 'build/bin/asimi-0.2.1/Asimi.exe' }
}

function Get-WebViewSnapshot {
    # Ask WMI only about the WebView2 executable. Unrelated rows stay in memory
    # and are neither recorded nor acted on.
    Get-CimInstance -ClassName Win32_Process -Filter "Name = 'msedgewebview2.exe'" -Property ProcessId, ParentProcessId, CreationDate, CommandLine
}

function Get-AttributedWebViews($Rows, $HostPid, $Profile, $Started, $Known) {
    $related = @{}
    foreach ($row in $Rows) {
        if (!$row.CreationDate) { continue }
        $created = $row.CreationDate.ToUniversalTime()
        if ($created -lt $Started.AddSeconds(-1)) { continue }
        $identity = "$($row.ProcessId)/$($created.Ticks)"
        if ($Known.ContainsKey($identity) -or $row.ParentProcessId -eq $HostPid -or
            ($row.CommandLine -and $row.CommandLine.IndexOf($Profile, [StringComparison]::OrdinalIgnoreCase) -ge 0)) {
            $related[$identity] = $row
        }
    }
    do {
        $added = $false
        $parentIds = @($related.Values | ForEach-Object { $_.ProcessId }) + @($Known.Values | ForEach-Object { $_.pid })
        foreach ($row in $Rows) {
            if (!$row.CreationDate) { continue }
            $created = $row.CreationDate.ToUniversalTime()
            $identity = "$($row.ProcessId)/$($created.Ticks)"
            if ($created -ge $Started.AddSeconds(-1) -and !$related.ContainsKey($identity) -and $parentIds -contains $row.ParentProcessId) {
                $related[$identity] = $row
                $added = $true
            }
        }
    } while ($added)
    foreach ($id in $related.Keys) {
        if (!$Known.ContainsKey($id)) {
            $row = $related[$id]
            $Known[$id] = [ordered]@{
                pid = $row.ProcessId
                parentPid = $row.ParentProcessId
                createdUtc = $row.CreationDate.ToUniversalTime().ToString('o')
                firstObservedUtc = [DateTime]::UtcNow.ToString('o')
                profileMatched = [bool]($row.CommandLine -and $row.CommandLine.IndexOf($Profile, [StringComparison]::OrdinalIgnoreCase) -ge 0)
            }
        }
        $Known[$id]
    }
}

function Test-LifecycleOrder($LogPath) {
    $order = @('lifecycle.storage_open', 'lifecycle.run_started', 'lifecycle.navigation_completed',
        'lifecycle.shutdown_requested', 'lifecycle.storage_closed', 'lifecycle.registry_closed',
        'lifecycle.native_close_requested', 'lifecycle.window_closing', 'lifecycle.native_window_removed',
        'lifecycle.wails_shutdown', 'lifecycle.wails_post_shutdown', 'lifecycle.run_returned')
    $cursor = -1
    $raw = Get-Content -LiteralPath $LogPath -Raw
    foreach ($marker in $order) {
        $cursor = $raw.IndexOf(('"msg":"{0}"' -f $marker), $cursor + 1, [StringComparison]::Ordinal)
        if ($cursor -lt 0) { return $false }
    }
    return $true
}

Push-Location $Phase2ARoot
try {
    if ($Variant -like 'minimal-*') {
        $AppPath = Join-Path $binRoot "WailsLifecycleProbe-$runKey.exe"
        & go build -trimpath -ldflags '-H=windowsgui' -o $AppPath ./validation/phase2a/lifecycleprobe
        if ($LASTEXITCODE) { throw 'Wails lifecycle probe build failed' }
    }
    $AppPath = (Resolve-Path -LiteralPath $AppPath).Path
    $buildInfo = & go version -m $AppPath
    if ($LASTEXITCODE) { throw 'Cannot read executable build metadata' }
    $buildInfo | Set-Content -LiteralPath "$evidence/build-info.txt"
    if ($Variant -eq 'app-pending') {
        $inspect = Join-Path $binRoot "DeskPilotLifecycleDB-$runKey.exe"
        & go build -trimpath -o $inspect ./validation/phase2a/lifecycledb
        if ($LASTEXITCODE) { throw 'Lifecycle SQLite inspector build failed' }
    }
    $null = Get-WebViewSnapshot
    $samples = @()
    foreach ($i in 1..$Runs) {
        $data = Join-Path $dataRoot "run-$i"
        $profile = Join-Path $data 'webview-profile'
        New-Item -ItemType Directory -Path $data | Out-Null
        $report = Join-Path $evidence "run-$i.acceptance.json"
        $stdout = Join-Path $evidence "run-$i.stdout.log"
        $stderr = Join-Path $evidence "run-$i.stderr.log"
        if ($Variant -like 'minimal-*') {
            $arguments = @('--profile', ('"{0}"' -f $profile), '--close-path', $Variant.Substring(8))
        } else {
            $arguments = @('--data-dir', ('"{0}"' -f $data))
            $stage = if ($Variant -eq 'app-pending') { '4' } else { '1' }
            $arguments += @('--acceptance', '--acceptance-stage', $stage, '--acceptance-report', ('"{0}"' -f $report))
        }
        $started = [DateTime]::UtcNow
        $child = Start-Process -FilePath $AppPath -ArgumentList $arguments -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        $known = @{}
        $fallbackCloseRequested = $false
        $timedOut = $false
        try {
            $deadline = $started.AddSeconds(50)
            do {
                $null = Get-AttributedWebViews @(Get-WebViewSnapshot) $child.Id $profile $started $known
                if ([DateTime]::UtcNow -ge $deadline) { $timedOut = $true; break }
            } until ($child.WaitForExit(125))
        } finally {
            if (!$child.HasExited) {
                $fallbackCloseRequested = $child.CloseMainWindow()
                $null = $child.WaitForExit(3000)
            }
        }
        $ended = [DateTime]::UtcNow
        $hostStillRunning = !$child.HasExited
        $exitCode = if (!$hostStillRunning) { $child.ExitCode } else { $null }
        $immediate = @(Get-AttributedWebViews @(Get-WebViewSnapshot) $child.Id $profile $started $known)
        $releaseDeadline = $ended.AddSeconds(3)
        Start-Sleep -Milliseconds 500
        $remaining = @(Get-AttributedWebViews @(Get-WebViewSnapshot) $child.Id $profile $started $known)
        while ($remaining.Count -and [DateTime]::UtcNow -lt $releaseDeadline) {
            Start-Sleep -Milliseconds 125
            $remaining = @(Get-AttributedWebViews @(Get-WebViewSnapshot) $child.Id $profile $started $known)
        }
        $releaseChecked = [DateTime]::UtcNow
        $diagnostic = @(Select-String -LiteralPath $stderr -Pattern 'Chrome_WidgetWin_0.*1412')
        $check = if (Test-Path -LiteralPath $report) { Get-Content -LiteralPath $report -Raw | ConvertFrom-Json } else { $null }
        if (Test-Path -LiteralPath "$data/runtime.log") {
            Copy-Item -LiteralPath "$data/runtime.log" -Destination "$evidence/run-$i.runtime.log"
        }
        $appAccepted = $Variant -notin @('phase1','app-acceptance','app-pending') -or ($check -and $check.passed)
        $currentApp = $Variant -like 'app-*'
        $orderedShutdown = $null
        $databaseExclusiveRead = $null
        $pendingPersisted = $null
        $shutdownReason = $null
        if ($currentApp) {
            $orderedShutdown = Test-LifecycleOrder "$evidence/run-$i.runtime.log"
            $shutdownLog = Select-String -LiteralPath "$evidence/run-$i.runtime.log" -Pattern '"msg":"lifecycle.shutdown_requested"' | Select-Object -First 1
            if ($shutdownLog) { $shutdownReason = ($shutdownLog.Line | ConvertFrom-Json).reason }
            $dbPath = Join-Path $data 'deskpilot.db'
            try {
                $dbHandle = [IO.File]::Open($dbPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
                $dbHandle.Dispose()
                $databaseExclusiveRead = $true
            } catch { $databaseExclusiveRead = $false }
            if ($Variant -eq 'app-pending' -and $appAccepted -and $databaseExclusiveRead) {
                $observedState = & $inspect --db $dbPath --task $check.taskId --approval $check.approvalId
                $pendingPersisted = !$LASTEXITCODE -and !(Test-Path -LiteralPath (Join-Path $data "workspace/$($check.fileName)"))
                $observedState | Set-Content -LiteralPath "$evidence/run-$i.database.json"
            }
        }
        $sample = [ordered]@{
            run = $i
            hostPid = $child.Id
            startedUtc = $started.ToString('o')
            exitObservedUtc = if (!$hostStillRunning) { $ended.ToString('o') } else { $null }
            exitCode = $exitCode
            timeout = $timedOut
            fallbackCloseRequested = $fallbackCloseRequested
            hostStillRunning = $hostStillRunning
            profile = $profile
            webViewsObserved = @($known.Values | Sort-Object { $_.pid })
            webViewsImmediatelyAfterHostExit = @($immediate | ForEach-Object { $_.pid })
            webViewsRemaining = @($remaining | ForEach-Object { $_.pid })
            releaseCheckUtc = $releaseChecked.ToString('o')
            error1412Count = $diagnostic.Count
            error1412Lines = @($diagnostic | ForEach-Object { $_.Line })
            acceptancePassed = if ($check) { $check.passed } else { $null }
            orderedShutdown = $orderedShutdown
            databaseExclusiveRead = $databaseExclusiveRead
            pendingInvalidatedWithoutFile = $pendingPersisted
            shutdownReason = $shutdownReason
            passed = $exitCode -eq 0 -and !$timedOut -and !$hostStillRunning -and $known.Count -gt 0 -and $remaining.Count -eq 0 -and
                $appAccepted -and (!$currentApp -or ($orderedShutdown -and $databaseExclusiveRead -and $diagnostic.Count -eq 0)) -and
                ($Variant -ne 'app-pending' -or ($pendingPersisted -and $shutdownReason -eq 'window_close'))
        }
        $samples += $sample
        $sample | ConvertTo-Json -Depth 7 -Compress
        $child.Dispose()
        if ($hostStillRunning) { break }
    }
    $result = [ordered]@{
        variant = $Variant
        executable = $AppPath
        executableSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $AppPath).Hash
        observation = 'Win32_Process exact executable name; attribution by unique profile, launch time, and parent PID lineage'
        results = $samples
    }
    $result | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath "$evidence/summary.json"
    "Evidence: $evidence"
    if (@($samples | Where-Object { !$_.passed }).Count) { throw 'A lifecycle run failed its process or acceptance checks' }
} finally {
    Pop-Location
}
