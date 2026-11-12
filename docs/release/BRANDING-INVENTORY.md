# Asimi 品牌迁移清单

| 位置 | 迁移结果 |
| --- | --- |
| Wails 应用、窗口和选择项目对话框 | 显示 Asimi。 |
| 双语首页、侧栏、启动向导、设置、About、提示 | 统一显示 Asimi。About 提供产品描述。 |
| Windows 文件属性 | 产品名 Asimi，文件名 Asimi.exe，产品版本 0.2.1。 |
| 图标 | 沿用既有四段线条标记和深紫配色。浏览器 favicon 使用 SVG，Windows 资源使用同一图形的六档 ICO。 |
| AI 系统文案和运行日志 | 使用 Asimi。历史消息和历史证据仍显示当时的文字。 |
| 专用测试窗口 | 显示 Asimi Control Test Host。Go 和 C# 对显示标题的核对同步更新，固定二进制身份和协议继续受原有校验保护。 |
| 构建和归档 | `build/bin/asimi-0.2.1/Asimi.exe` 与 `build/bin/Asimi-0.2.1-win-x64.zip`。 |
| 默认数据目录和稳定内部标识 | 沿用 `%LOCALAPPDATA%\DeskPilotAI`、`deskpilot.db`、原 Go 模块、事件、DPAPI、Helper 文件名、协议和 SQLite v1～v6。 |
| 文档 | 新版发行说明和根文档使用 Asimi。历史报告和升级来源保留原品牌以供核对。 |

原生验收使用隔离的默认路径及旧版数据库副本，品牌核对以最终解压包为准。[发布验收](RELEASE-ACCEPTANCE.md)
