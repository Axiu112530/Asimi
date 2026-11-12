# DeskPilot 发布准备

DeskPilot 0.2 是 Windows 本地桌面与开发助手，提供 BYOK 聊天、工作区、模型计划、精确 Diff 和单动作审批。可离线使用项目浏览、已有固定工具及环境诊断。安全规则由本地后端执行，Community 版保留同样的审批、恢复、审计和隐私边界。

仓库准备了 [贡献指南](../../CONTRIBUTING.md)、[漏洞报告指南](../../SECURITY.md)、[安装](../INSTALLATION-GUIDE.md)、[架构](AGENT-ORCHESTRATION.md)、[路线图](../ROADMAP.md) 和 [安全说明](SECURITY-REVIEW.md)。发布内容包括 Windows x64 便携包、两个固定 Helper、配置文件和 SHA-256 清单。WebView2 和 .NET Framework 由 Windows 或 Microsoft 运行时提供。

当前仓库未发现根目录 LICENSE。许可证由项目所有者决定。社区分发前应明确许可和第三方依赖通知，再由项目所有者发起发布。

Provider 继续采用适配器接口。Skills 保留能力描述、输入输出和注册信息。工作流与执行审批分离，可用于将来扩展模板和团队策略。0.2 的运行不依赖账号、在线授权或远程控制服务。
