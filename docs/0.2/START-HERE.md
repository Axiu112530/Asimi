# DeskPilot AI 0.2 · Windows portable

Extract the whole ZIP on a local Windows drive. Keep `DeskPilotAI.exe`, the two helper executables, and their `.config` files together. Start the app as a standard user. Windows 11 x64 with WebView2 Runtime and .NET Framework 4.x is the target. See [Microsoft WebView2](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) for the Evergreen Runtime.

Choose **Models** to configure a BYOK provider and model, or skip setup for offline use. Agent Tasks use configured USD rates for their request budget. The DeepSeek preset contains a peak-price snapshot which can be edited. The test and send actions each disclose the provider endpoint before a request.

Choose a workspace, open **Tasks**, enter a goal and optionally add relevant file paths. Review the exact outbound context. Approve each file change or registered command individually. Command results appear in the timeline and can be reviewed in the next model request.

完整保留解压目录，使用普通用户启动。真实截图和 Notepad 请通过应用内的明确观察和审批流程验收。出现不确定结果时，先检查目标当前状态，再恢复任务。

See [Developer Agent](DEVELOPER-AGENT.md), [Agent workflow](AGENT-ORCHESTRATION.md), [Security](SECURITY-REVIEW.md), and [Computer acceptance](COMPUTER-MANUAL-ACCEPTANCE.md).
