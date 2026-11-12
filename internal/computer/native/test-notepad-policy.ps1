param([string]$OutputDirectory = 'build/notepad-policy')
$ErrorActionPreference = 'Stop'
$notepadRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$notepadOutput = [IO.Path]::GetFullPath((Join-Path $notepadRoot $OutputDirectory))
if (!$notepadOutput.StartsWith($notepadRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Output must stay inside repository' }
New-Item -ItemType Directory -Force -Path $notepadOutput | Out-Null
$notepadFramework = Join-Path ([Environment]::GetFolderPath('Windows')) 'Microsoft.NET/Framework64/v4.0.30319'
$notepadCompiler = Join-Path $notepadFramework 'csc.exe'
$notepadPin = Join-Path $notepadOutput 'ControlHostBuild.cs'
# No host is launched. The sentinel hash makes fixture execution unavailable.
[IO.File]::WriteAllText($notepadPin, 'internal static class ControlHostBuild { internal const string Sha256 = "unavailable"; }')
$notepadRefs = @('/r:System.Web.Extensions.dll','/r:System.Runtime.Serialization.dll',
    ('/r:' + (Join-Path $notepadFramework 'WPF/UIAutomationClient.dll')),
    ('/r:' + (Join-Path $notepadFramework 'WPF/UIAutomationTypes.dll')),
    ('/r:' + (Join-Path $notepadFramework 'WPF/WindowsBase.dll')))
$notepadWorkerSource = Join-Path $PSScriptRoot 'UIAWorker.cs'
$notepadTestSource = Join-Path $PSScriptRoot 'NotepadPolicyTests.cs'
$notepadTest = Join-Path $notepadOutput 'NotepadPolicyTests.exe'
$notepadWorker = Join-Path $notepadOutput 'AsimiUIAWorker.exe'
& $notepadCompiler /nologo /target:exe /platform:x64 /main:NotepadPolicyTests "/out:$notepadTest" @notepadRefs $notepadWorkerSource $notepadTestSource $notepadPin
if ($LASTEXITCODE) { throw 'Policy harness compilation failed' }
& $notepadTest
if ($LASTEXITCODE) { throw 'Worker Notepad policy failed' }
& $notepadCompiler /nologo /target:exe /platform:x64 "/out:$notepadWorker" @notepadRefs $notepadWorkerSource $notepadPin
if ($LASTEXITCODE) { throw 'Worker compilation failed' }
function InvokeNotepadProtocol($request, [string]$expected) {
    $notepadStart = [Diagnostics.ProcessStartInfo]::new()
    $notepadStart.FileName = $notepadWorker
    $notepadStart.UseShellExecute = $false
    $notepadStart.CreateNoWindow = $true
    $notepadStart.RedirectStandardInput = $true
    $notepadStart.RedirectStandardOutput = $true
    $notepadProcess = [Diagnostics.Process]::Start($notepadStart)
    try {
        $notepadRead = $notepadProcess.StandardOutput.ReadToEndAsync()
        $notepadProcess.StandardInput.Write(($request | ConvertTo-Json -Depth 8 -Compress))
        $notepadProcess.StandardInput.Close()
        if (!$notepadProcess.WaitForExit(5000)) { $notepadProcess.Kill(); throw 'Protocol check timed out' }
        $notepadReply = $notepadRead.Result | ConvertFrom-Json
        if ($notepadReply.effect -ne 'NOT_STARTED' -or $notepadReply.code -ne $expected) { throw ('Unexpected protocol result: ' + $notepadRead.Result) }
    } finally { $notepadProcess.Dispose() }
}
$notepadWindow = @{id='never-native';app='notepad';hwnd='1';pid=[uint32]4294967294;creationTime='1';imagePath='invalid';sessionId=1;integrity=8192;className='Notepad';title='invalid';bounds=@{x=0;y=0;width=10;height=10};foreground=$false;observedAt='invalid'}
$notepadElement = @{id='editor';name='';automationId='TextEditor';controlType='Document';bounds=@{x=0;y=0;width=5;height=5};enabled=$true;offscreen=$false;focused=$false;password=$false;processId=7;nativeHandle='0';patterns=@('Value');parentPath='root';runtimeId='1.2';depth=1;blank=$true;valueWritable='true'}
InvokeNotepadProtocol @{op='set_value';window=$notepadWindow;element=$notepadElement;text='Harmless sample'} 'CONTROL_INPUT_REJECTED'
InvokeNotepadProtocol @{op='invoke';window=$notepadWindow;element=$notepadElement} 'CONTROL_ACTION_DENIED'
$notepadElement.valueWritable = $true
InvokeNotepadProtocol @{op='set_value';window=$notepadWindow;element=$notepadElement;text='Harmless sample'} 'CONTROL_STALE_TARGET'
'Worker protocol: invalid writable flag and Notepad Invoke denied; impossible process rejected. No native application effects.'
