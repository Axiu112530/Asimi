param(
    [string] $OutputDirectory = 'build/bin/asimi-0.2.1'
)

$ErrorActionPreference = 'Stop'
$Phase1Root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
. "$Phase1Root/validation/phase0/env.ps1"
Push-Location $Phase1Root
try {
    Push-Location frontend
    try {
        & npm.cmd run typecheck
        if ($LASTEXITCODE) { throw 'TypeScript check failed' }
        & npm.cmd run build
        if ($LASTEXITCODE) { throw 'Frontend build failed' }
    } finally { Pop-Location }
    $ControlOutput = [IO.Path]::GetFullPath((Join-Path $Phase1Root $OutputDirectory))
    if (-not $ControlOutput.StartsWith($Phase1Root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Build output must be within the repository'
    }
    New-Item -ItemType Directory -Force -Path $ControlOutput | Out-Null
    $ControlFramework = Join-Path ([Environment]::GetFolderPath('Windows')) 'Microsoft.NET/Framework64/v4.0.30319'
    $ControlCompiler = Join-Path $ControlFramework 'csc.exe'
    $ControlHost = Join-Path $ControlOutput 'DeskPilotControlHost.exe'
    $ControlWorker = Join-Path $ControlOutput 'DeskPilotUIAWorker.exe'
    $ControlHostSource = Join-Path $Phase1Root 'internal\computer\native\ControlHost.cs'
    $ControlWorkerSource = Join-Path $Phase1Root 'internal\computer\native\UIAWorker.cs'
    & $ControlCompiler /nologo /target:winexe /platform:x64 "/out:$ControlHost" /r:System.Windows.Forms.dll /r:System.Drawing.dll /r:Accessibility.dll $ControlHostSource
    if ($LASTEXITCODE) { throw 'Control fixture build failed' }
    $ControlHostHash = (Get-FileHash -LiteralPath $ControlHost -Algorithm SHA256).Hash.ToLowerInvariant()
    $ControlPinSource = Join-Path $ControlOutput 'ControlHostBuild.cs'
    [IO.File]::WriteAllText($ControlPinSource, ('internal static class ControlHostBuild { internal const string Sha256 = "' + $ControlHostHash + '"; }'))
    try {
        $ControlWorkerRefs = @(
            '/r:System.Web.Extensions.dll',
            '/r:System.Runtime.Serialization.dll',
            ('/r:' + (Join-Path $ControlFramework 'WPF/UIAutomationClient.dll')),
            ('/r:' + (Join-Path $ControlFramework 'WPF/UIAutomationTypes.dll')),
            ('/r:' + (Join-Path $ControlFramework 'WPF/WindowsBase.dll'))
        )
        & $ControlCompiler /nologo /target:exe /platform:x64 "/out:$ControlWorker" @ControlWorkerRefs $ControlWorkerSource $ControlPinSource
        if ($LASTEXITCODE) { throw 'UI Automation worker build failed' }
    } finally {
        Remove-Item -LiteralPath $ControlPinSource -ErrorAction SilentlyContinue
    }
    # Fixed, hash-checked runtime config prevents untrusted sidecar activation.
    [IO.File]::WriteAllText($ControlHost + '.config', '<configuration/>', [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText($ControlWorker + '.config', '<configuration/>', [Text.UTF8Encoding]::new($false))
    $ControlWorkerHash = (Get-FileHash -LiteralPath $ControlWorker -Algorithm SHA256).Hash.ToLowerInvariant()
    $ControlLDFlags = "-H=windowsgui -X deskpilot/internal/computer.hostSHA256=$ControlHostHash -X deskpilot/internal/computer.workerSHA256=$ControlWorkerHash"
    $ControlDesktop = Join-Path $ControlOutput 'Asimi.exe'
    $AsimiResource = Join-Path $Phase1Root 'rsrc_asimi_windows_amd64.syso'
    $Wails = Join-Path $Phase1Root '.phase0-tools/bin/wails3.exe'
    $ResourceInput = Join-Path $Phase1Root 'build/windows'
    if (Test-Path -LiteralPath $AsimiResource) { throw 'Asimi build resource already exists. Check for an active build.' }
    try {
        & $Wails generate syso -arch amd64 -out $AsimiResource -manifest (Join-Path $ResourceInput 'asimi.exe.manifest') -info (Join-Path $ResourceInput 'asimi-info.json') -icon (Join-Path $ResourceInput 'asimi.ico')
        if ($LASTEXITCODE) { throw 'Asimi Windows resource generation failed' }
        & go build -trimpath -ldflags $ControlLDFlags -o $ControlDesktop .
        if ($LASTEXITCODE) { throw 'Asimi desktop build failed' }
    } finally {
        Remove-Item -LiteralPath $AsimiResource -ErrorAction SilentlyContinue
    }
    Get-Item -LiteralPath $ControlDesktop,$ControlHost,$ControlWorker | Select-Object FullName,Length
} finally { Pop-Location }
