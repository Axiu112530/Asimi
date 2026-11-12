# Asimi 开源协议说明

Asimi 的应用源码和项目文档采用 MIT License。版权声明为 Copyright (c) 2026 Axiu，完整授权条件见仓库根目录的 LICENSE。本文帮助读者理解许可与分发流程，具体授权内容以 LICENSE 英文原文为准。[MIT License](../../LICENSE)

## 使用与再分发

| 行为 | MIT 下的条件 |
| --- | --- |
| 学习、运行、复制与修改 | 允许个人、内部和商业场景使用 |
| 分发源码、修改版或二进制 | 保留版权声明和完整许可声明 |
| 商业使用、再许可或销售副本 | 按 MIT 条件保留相关声明，第三方组件还需遵守自身条款 |
| 修改后公开源代码 | MIT 本身无需公开修改后的全部源码 |
| 软件保证与责任 | 按 MIT 中的 AS IS、保证排除和责任限制条款执行 |

GitHub 原仓库的写入和合并权限由仓库维护者管理。贡献者通过 Fork 和 Pull Request 提交建议，由维护者审核后决定是否合并。MIT 的使用授权与 GitHub 的仓库访问权限各自生效。[贡献指南](../../CONTRIBUTING.md)

## 第三方组件

Wails、Go 运行库、SQLite 及 Go 模块、Vue 和其他嵌入前端依赖各自遵循其许可证。再分发包含这些组件的版本时，应一起提供适用的版权与许可声明。第三方全文汇总记录了候选包内的组件及原始许可。[第三方声明](THIRD-PARTY-NOTICES.md)

WebView2、Windows .NET Framework 等系统运行时按 Microsoft 的适用条款使用。模型供应商的服务地址、API Key、计费及数据处理由所配置服务的条款决定。[隐私说明](../../PRIVACY.md)

## 发布包中的声明

当前打包入口为 scripts/package-asimi.ps1。发布目录应包含应用 LICENSE、THIRD-PARTY-NOTICES.md、PRIVACY.md 及随包指南，并生成 SHA256SUMS.txt 和 ZIP 的独立 SHA256 文件。应用许可证随便携包一同交付，便于下载者保留授权信息。

历史验收摘要记录的是当时的候选包。源码文档或品牌调整后重新构建的二进制需要生成新的校验值，并按实际完成的检查说明验证范围。[发布报告](RELEASE-REPORT.md)

MIT 条件可参阅 GitHub 的许可说明与 MIT 页面。[GitHub 许可指南](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/licensing-a-repository) · [MIT 许可介绍](https://choosealicense.com/licenses/mit/)
