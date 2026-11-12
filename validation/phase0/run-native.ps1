param([switch]$IncludeApps)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/env.ps1"
$bin = Join-Path $PSScriptRoot 'bin'
$evidence = Join-Path $Phase0Root 'docs/evidence'
Push-Location $PSScriptRoot
try {
    & go build -o "$bin/native.exe" .
    if ($LASTEXITCODE) { throw "Go build failed: $LASTEXITCODE" }
    & 'C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe' /nologo /target:winexe "/out:$bin/Fixture.exe" /r:System.Windows.Forms.dll /r:System.Drawing.dll /r:System.Web.Extensions.dll /r:C:\Windows\Microsoft.NET\Framework64\v4.0.30319\WPF\UIAutomationClient.dll /r:C:\Windows\Microsoft.NET\Framework64\v4.0.30319\WPF\UIAutomationTypes.dll /r:C:\Windows\Microsoft.NET\Framework64\v4.0.30319\WPF\WindowsBase.dll Fixture.cs
    if ($LASTEXITCODE) { throw "C# fixture build failed: $LASTEXITCODE" }
    $state = Join-Path $bin ('fixture-' + [Guid]::NewGuid().ToString('N') + '.json')
    $fixture = Start-Process -FilePath "$bin/Fixture.exe" -ArgumentList @('fixture', $state) -PassThru -WindowStyle Hidden
    try {
        $deadline = [DateTime]::UtcNow.AddSeconds(8)
        while (!(Test-Path -LiteralPath $state) -and [DateTime]::UtcNow -lt $deadline -and !$fixture.HasExited) { Start-Sleep -Milliseconds 100 }
        if (!(Test-Path -LiteralPath $state)) { throw 'Fixture did not initialize in 8s' }
        & "$bin/native.exe" $state "$evidence/native.json"
        if ($LASTEXITCODE) { throw "Native probe failed: $LASTEXITCODE" }
        $uia = Start-Process -FilePath "$bin/Fixture.exe" -ArgumentList @('uia', $state, "$evidence/uia.json") -PassThru -WindowStyle Hidden
        if (!$uia.WaitForExit(15000)) { $uia.Kill(); throw 'UIA helper timed out' }
        if ($uia.ExitCode) { throw "UIA helper failed: $($uia.ExitCode)" }
        Get-Content -LiteralPath "$evidence/uia.json"
        $result = Get-Content -LiteralPath "$evidence/uia.json" -Raw | ConvertFrom-Json
        foreach ($key in @('valueRoundTrip','invokeVerified','selection','textPattern','textRead')) {
            if ($result.fixture.$key -ne $true) { throw "UIA assertion failed: $key" }
        }
    } finally { if (!$fixture.HasExited) { $null = $fixture.CloseMainWindow(); if (!$fixture.WaitForExit(2000)) { $fixture.Kill() } } }
    if ($IncludeApps) {
        $apps = Start-Process -FilePath "$bin/Fixture.exe" -ArgumentList @('apps',$Phase0Root,"$evidence/uia-apps.json") -PassThru -WindowStyle Hidden
        if (!$apps.WaitForExit(35000)) { $apps.Kill(); throw 'Scoped application probe timed out' }
        if ($apps.ExitCode) { throw "Scoped application probe failed: $($apps.ExitCode)" }
        Get-Content -LiteralPath "$evidence/uia-apps.json"
    }
} finally { Pop-Location }
