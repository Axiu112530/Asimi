# DeskPilot Community 0.2.1 · Windows 快速开始

1. 将 ZIP 解压到本地目录，保留两项 Helper 及其同名配置文件。需要 Windows x64、Microsoft Edge WebView2 Evergreen Runtime 和 Windows 的 .NET Framework 4.x 运行时。打开 `DeskPilotAI.exe`。此候选包的 Windows 验收结果见发布报告。
2. 在欢迎页选择简体中文或英语。可选择 DeepSeek、粘贴自己的 API Key，然后选择「测试连接」。成功后默认使用 `deepseek-flash`。可跳过并离线使用聊天和项目浏览。其他兼容供应商可在「AI 供应商」的高级区域手动配置。
3. 在准备页选择要分析的本地项目。开发者页面显示项目识别结果和 Git 状态，环境标签显示发现的运行时与检测来源。
4. 在任务页填写目标。DeskPilot 会推荐少量相关源文件，你也可以手动指定。检查上下文预览，再确认发送。模型生成计划后，文件变更与命令各有独立审批。检查 Diff 和命令参数，批准后再阅读实际执行和回读结果。工作区允许的命令和源文件由软件按规则校验。
5. 界面上方可停止任务。重启后从任务历史查看审批、执行及恢复状态。结果不确定的动作需检查当前目标和重新审批。

DeepSeek 使用自己的账户额度。连接测试发送一条固定短消息。项目分析可发送你审核过的源文件和实际测试输出。截图外发另需单独预览确认。API Key 使用当前 Windows 账户的 DPAPI 保护，历史保存在本机应用数据目录。

`SHA256SUMS.txt` 列出了同一 ZIP 内交付文件的校验值。PowerShell 可用 `Get-FileHash .\DeskPilotAI.exe -Algorithm SHA256` 校验文件。对受信任的发布渠道，也应核对渠道单独发布的 ZIP SHA256。
