# DeskPilot AI 中文入口

当前已交付 Phase 1 功能雏形：原生工作台、英文/中文设置、Mock 规划与澄清、精确
审批、唯一的隔离文件创建、独立验证、取消、SQLite 与历史。真实模型、桌面控制、
终端、Git 和工作流均未启用。Phase 0 报告、证据和探针完整保留。

- [启动桌面程序](../build/bin/DeskPilotAI.exe)，或运行 `./scripts/run.ps1`。
- [Phase 1 实现报告](PHASE-1-REPORT.md)
- [Phase 1 验收清单](PHASE-1-ACCEPTANCE.md)
- 示例：`创建文件“笔记.txt”，内容为“你好，DeskPilot”`，随后核对并审批。
- 文件只会写入应用数据目录的 workspace；不覆盖已有文件，不请求真实 API Key。

- [实际验证报告与结论](PHASE-0-REPORT.md)
- [产品需求](PRD.md)
- [架构与 Provider/Computer 协议](ARCHITECTURE.md)
- [安全威胁模型和审批规则](SECURITY.md)
- [英文默认、中文热切换及持久化方案](I18N.md)
- [分阶段路线与停止条件](ROADMAP.md)
- [测试策略和未完成测试矩阵](TEST-STRATEGY.md)

主要工程文档采用英文；报告同时提供中文摘要。安装包、商业计费、真实模型调用、
通用自主 Agent 均不属于当前实现。系统版本以实际 build 为准，不能只看注册表
ProductName；多屏、不同应用控件覆盖及发行签名需要各自独立验收。
