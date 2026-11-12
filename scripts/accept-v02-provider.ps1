param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [string]$ReportPath
)

$ErrorActionPreference = 'Stop'
$binary = (Resolve-Path -LiteralPath $ExePath).Path
if (!$ReportPath) {
    $stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
    $ReportPath = Join-Path $PSScriptRoot "../docs/0.2/evidence/provider-live-$stamp.json"
}
$report = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($ReportPath)
if (Test-Path -LiteralPath $report) {
    throw 'Choose a new report path so the previous run remains available.'
}
New-Item -ItemType Directory -Force -Path ([IO.Path]::GetDirectoryName($report)) | Out-Null
$hash = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash

# The credential is inherited from this process and never passed on the command line.
$child = Start-Process -FilePath $binary -ArgumentList @('--acceptance-provider', ('"{0}"' -f $report)) -WindowStyle Hidden -PassThru
try {
    if (!$child.WaitForExit(115000)) {
        if (!$child.HasExited) {
            $child.Kill()
            $null = $child.WaitForExit(5000)
        }
        throw "Provider acceptance timed out in process $($child.Id)."
    }
    if (!(Test-Path -LiteralPath $report -PathType Leaf)) {
        throw "Provider acceptance exited with code $($child.ExitCode) without a report."
    }
    $result = Get-Content -LiteralPath $report -Raw | ConvertFrom-Json
    [pscustomobject]@{
        report = $report
        binarySHA256 = $hash
        exitCode = $child.ExitCode
        status = $result.status
        externalCalls = $result.externalCalls
    } | ConvertTo-Json
    if ($child.ExitCode -ne 0 -or !$result.passed) {
        throw "Provider acceptance did not pass ($($result.errorCode))."
    }
} finally {
    $child.Dispose()
}
