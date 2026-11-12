# DeskPilot AI MVP 0.1 安装与便携运行

Community 0.2.1 用户请从 [便携版快速开始](0.2.1/START-HERE.md) 或 [0.2 升级指南](0.2.1/UPGRADE.md) 开始。以下保留 MVP 0.1 安装说明。

目标平台为 Windows 11 x64。此次原生验收平台为 Windows 11 25H2 x64。其他 Windows 版本需另行验证。包内包括 `DeskPilotAI.exe`、`DeskPilotControlHost.exe`、`DeskPilotUIAWorker.exe`、两个固定 `.config`、此指南及文件散列清单。

1. 将 ZIP 全部解压到有写入权限的本地目录。程序和两个 Helper 必须放在同一个目录，保留两个 `.config` 文件。
2. 确认机器安装 Microsoft Edge WebView2 Evergreen Runtime x64 和 Windows 的 .NET Framework 4.x 运行时。WebView2 缺失时从 Microsoft 获取 x64 安装程序。参考 [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) 与 [.NET Framework 与 Windows 版本](https://learn.microsoft.com/en-us/dotnet/framework/install/on-windows-and-server)。
3. 以普通用户打开 `DeskPilotAI.exe`。首次启动时可使用离线模式。配置 BYOK 后仅在确认发送时测试连接或访问模型。

便携目录包含应用文件。默认用户数据位于 `%LOCALAPPDATA%\DeskPilotAI`，包含数据库、DPAPI 密钥密文、日志及 WebView2 profile。迁移同一 Windows 用户的数据前先正常退出应用并备份整个目录。DPAPI 密文依赖 Windows 用户上下文。

| 症状 | 排查 |
| --- | --- |
| 无法启动或空白窗口 | 检查 WebView2 Runtime 和 `%LOCALAPPDATA%\DeskPilotAI\runtime.log`。确认目录与数据库可读写。 |
| Computer 控件显示不可用 | 检查 Helper、固定配置和 `SHA256SUMS.txt`。确认以普通用户运行且目标窗口符合支持范围。 |
| 工作区拒绝选择 | 使用本地固定盘上的实际目录。网络共享、路径别名和重解析目录不可注册。 |
| 文件替换拒绝或结果不明 | 确认本地 NTFS 事务能力，关闭编辑该文件的进程后重新读取。先核查实际内容，再重新生成和审批 Diff。 |
| 命令找不到运行时 | 在 Environment 检查 PATH 发现结果。按官方安装步骤操作并重启 DeskPilot。需要管理员权限的安装由用户主动完成。 |
| Provider 请求失败 | 在 Models 核查 URL、协议、模型 ID、凭据和该模型能力，再主动测试。诊断时不要分享密钥、截图或源代码。 |
