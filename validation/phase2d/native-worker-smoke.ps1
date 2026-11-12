param([Parameter(Mandatory=$true)][string]$Bin)
$ErrorActionPreference = 'Stop'
$hostPath = Join-Path $Bin 'DeskPilotControlHost.exe'
$workerPath = Join-Path $Bin 'DeskPilotUIAWorker.exe'
if (!(Test-Path $hostPath) -or !(Test-Path $workerPath)) { throw 'Expected fixture host and UIA worker executables' }
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class NativeSmoke {
    [StructLayout(LayoutKind.Sequential)] public struct Rect { public int left, top, right, bottom; }
    [DllImport("user32", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr hwnd, StringBuilder s, int n);
    [DllImport("user32")] public static extern bool GetWindowRect(IntPtr hwnd, out Rect rect);
    [DllImport("user32")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
}
"@
$hostInfo = New-Object Diagnostics.ProcessStartInfo
$hostInfo.FileName = $hostPath
$hostInfo.UseShellExecute = $false
$hostInfo.RedirectStandardInput = $true
$hostProc = [Diagnostics.Process]::Start($hostInfo)
try {
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 100
        $hostProc.Refresh()
        if ($hostProc.MainWindowHandle -ne [IntPtr]::Zero) { break }
    }
    if ($hostProc.MainWindowHandle -eq [IntPtr]::Zero) { throw 'Fixture host window did not appear' }
    $rect = New-Object 'NativeSmoke+Rect'
    $previous = [NativeSmoke]::SetThreadDpiAwarenessContext([IntPtr](-4))
    try { if (![NativeSmoke]::GetWindowRect($hostProc.MainWindowHandle, [ref]$rect)) { throw 'Missing window rectangle' } }
    finally { [NativeSmoke]::SetThreadDpiAwarenessContext($previous) | Out-Null }
    $class = [Text.StringBuilder]::new(256)
    [NativeSmoke]::GetClassName($hostProc.MainWindowHandle, $class, 256) | Out-Null
    $window = @{id='test';app='test_host';hwnd=[string]$hostProc.MainWindowHandle.ToInt64();pid=$hostProc.Id;creationTime=[string]$hostProc.StartTime.ToFileTimeUtc();imagePath=$hostPath;imageHash=(Get-FileHash -Algorithm SHA256 $hostPath).Hash.ToLowerInvariant();sessionId=$hostProc.SessionId;integrity=8192;className=$class.ToString();title=$hostProc.MainWindowTitle;bounds=@{x=$rect.left;y=$rect.top;width=$rect.right-$rect.left;height=$rect.bottom-$rect.top};foreground=$false;observedAt=[DateTime]::UtcNow.ToString('O')}
    function CallRawWorker([string]$payload) {
        $startInfo = New-Object Diagnostics.ProcessStartInfo
        $startInfo.FileName = $workerPath
        $startInfo.UseShellExecute = $false
        $startInfo.CreateNoWindow = $true
        $startInfo.RedirectStandardInput = $true
        $startInfo.RedirectStandardOutput = $true
        $p = [Diagnostics.Process]::Start($startInfo)
        try {
            $readOutput = $p.StandardOutput.ReadToEndAsync()
            $p.StandardInput.Write($payload)
            $p.StandardInput.Close()
            if (!$p.WaitForExit(8000)) { $p.Kill(); throw 'UIA worker timed out' }
            $readOutput.Result | ConvertFrom-Json
        } finally { $p.Dispose() }
    }
    function CallWorker($operation, $element, $value) {
        $request = @{op=$operation;window=$window}
        if ($null -ne $element) { $request.element = $element }
        if ($null -ne $value) { $request.text = $value }
        CallRawWorker ($request | ConvertTo-Json -Depth 10 -Compress)
    }
    function InspectWorker {
        $result = CallWorker 'inspect' $null $null
        if ($result.code -or $result.modal -or $result.truncated) { throw ('Unexpected inspect response ' + ($result | ConvertTo-Json -Depth 6 -Compress)) }
        $result.elements
    }
    $badRequests = @(
        '{"op":"inspect","op":"invoke"}',
        '{"op":"run_command","window":{}}',
        '{"op":"inspect","window":{},"text":"Hello"}',
        ('{"op":"inspect","window":{},"padding":"' + ('x' * 262144) + '"}')
    )
    foreach ($payload in $badRequests) {
        $r = CallRawWorker $payload
        if ($r.effect -ne 'NOT_STARTED' -or $r.code -ne 'CONTROL_INPUT_REJECTED') { throw 'Invalid protocol payload was accepted' }
    }
    $validPayload = @{op='inspect';window=$window} | ConvertTo-Json -Depth 10 -Compress
    $duplicates = $validPayload.Replace('"op":"inspect"', '"op":"inspect","op":"inspect"')
    $r = CallRawWorker $duplicates
    if ($r.effect -ne 'NOT_STARTED' -or $r.code -ne 'CONTROL_INPUT_REJECTED') { throw 'Duplicate protocol member was accepted' }
    $badPath = $window.imagePath
    $window.imagePath = 'C:\Windows\System32\notepad.exe'
    $r = CallWorker 'inspect' $null $null
    if ($r.effect -ne 'NOT_STARTED' -or $r.code -ne 'CONTROL_STALE_TARGET') { throw 'Changed image path was accepted' }
    $window.imagePath = $badPath
    $tree = InspectWorker
    $edit = $tree | Where-Object automationId -eq 'TestInput'
    $submit = $tree | Where-Object automationId -eq 'SubmitButton'
    $reset = $tree | Where-Object automationId -eq 'ResetButton'
    $label = $tree | Where-Object automationId -eq 'ResultLabel'
    if (!$edit.blank -or $edit.patterns -notcontains 'Value' -or $submit.patterns -notcontains 'Invoke' -or $label.name -ne 'Idle') { throw ('Fixture elements did not expose expected UIA patterns and initial state ' + ($tree | ConvertTo-Json -Depth 6 -Compress)) }
    $wrong = $edit.PSObject.Copy()
    $wrong.runtimeId = '1.2.3'
    $r = CallWorker 'set_value' $wrong 'Hello Asimi'
    if ($r.effect -ne 'NOT_STARTED' -or $r.code -ne 'CONTROL_STALE_TARGET') { throw 'Altered element identity was accepted' }
    $r = CallWorker 'set_value' $edit 'powershell'
    if ($r.effect -ne 'NOT_STARTED' -or $r.code -ne 'CONTROL_INPUT_REJECTED') { throw 'Command text was accepted' }
    $r = CallWorker 'set_value' $edit 'Hello Asimi'
    if ($r.effect -ne 'ISSUED') { throw ('SetValue failed ' + ($r | ConvertTo-Json -Compress)) }
    $tree = InspectWorker
    $edit = $tree | Where-Object automationId -eq 'TestInput'
    $expectedDigest = [BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes('Hello Asimi'))).Replace('-','').ToLowerInvariant()
    if ($edit.valueSha256 -ne $expectedDigest -or $edit.blank) { throw 'Separate UIA read did not verify typed value' }
    $submit = $tree | Where-Object automationId -eq 'SubmitButton'
    $r = CallWorker 'invoke' $submit $null
    if ($r.effect -ne 'ISSUED') { throw ('Submit failed ' + ($r | ConvertTo-Json -Compress)) }
    Start-Sleep -Milliseconds 100
    $tree = InspectWorker
    if (($tree | Where-Object automationId -eq 'ResultLabel').name -ne 'Submitted') { throw 'Separate UIA read did not verify Submit' }
    $reset = $tree | Where-Object automationId -eq 'ResetButton'
    $r = CallWorker 'invoke' $reset $null
    if ($r.effect -ne 'ISSUED') { throw ('Reset failed ' + ($r | ConvertTo-Json -Compress)) }
    Start-Sleep -Milliseconds 100
    $tree = InspectWorker
    if (($tree | Where-Object automationId -eq 'ResultLabel').name -ne 'Reset' -or !($tree | Where-Object automationId -eq 'TestInput').blank) { throw 'Separate UIA read did not verify Reset' }
    $staleEdit = $tree | Where-Object automationId -eq 'TestInput'
    $r = CallWorker 'type_text' $staleEdit 'Semantic text'
    if ($r.effect -ne 'ISSUED') { throw 'Semantic TypeText alias failed' }
    $r = CallWorker 'set_value' $staleEdit 'Stale overwrite'
    if ($r.effect -ne 'NOT_STARTED' -or $r.code -ne 'CONTROL_STALE_TARGET') { throw 'Changed approved input value was overwritten' }
    $window.creationTime = '1'
    $r = CallWorker 'inspect' $null $null
    if ($r.code -ne 'CONTROL_STALE_TARGET') { throw 'Altered process creation time was accepted' }
        $hostProc.StandardInput.Close()
    if (!$hostProc.WaitForExit(2500)) { throw 'Host did not close after parent pipe EOF' }
    'Fixture UIA inspection, strict protocol, identity and text guards, SetValue/TypeText, Submit, Reset, and pipe close verified.'
}
finally {
    if (!$hostProc.HasExited) { $hostProc.StandardInput.Close(); if (!$hostProc.WaitForExit(2500)) { throw 'Dedicated fixture did not confirm graceful pipe close' } }
    $hostProc.Dispose()
}




