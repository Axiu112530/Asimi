# Asimi 0.2.1 发布报告

## 当前命名与文档

产品名称统一为 Asimi，Windows 启动文件为 Asimi.exe。当前构建入口为 scripts/build.ps1，打包入口为 scripts/package-asimi.ps1。

| 交付 | 位置 |
| --- | --- |
| 便携 ZIP | build/bin/Asimi-0.2.1-win-x64.zip |
| 归档校验 | build/bin/Asimi-0.2.1-win-x64.zip.sha256 |
| 应用 | build/bin/asimi-0.2.1/Asimi.exe |
| 逐文件校验 | build/bin/asimi-0.2.1/SHA256SUMS.txt |

应用与项目文档采用 MIT。当前打包脚本随包交付 LICENSE、第三方声明、安装和使用指南、隐私说明与校验清单。README 包含安装、模型配置、项目任务、数据存储、源码构建、反馈和许可说明。[MIT 许可证](../../LICENSE) · [协议说明](LICENSE-DECISION.md) · [安装指南](INSTALLATION-GUIDE.md)

## 当前改动的验证范围

本次更新涉及文档、展示名称、Windows 产品属性及构建产物命名，已完成以下检查。

- TypeScript、Vite 和 Windows 原生构建通过。
- 语言检查覆盖 1,204 个键与 454 处文字引用，中英文键、插值及回退一致。原有 844 个基线键通过兼容检查，三组模块化翻译通过检查。
- README、贡献、隐私、许可和发布说明的 52 个本地链接目标存在，9 个修改过的 PowerShell 脚本语法检查通过。
- 两次隔离的普通启动和关闭通过，窗口标题、Windows 产品名、关联及原生窗口图标核对成功。退出后数据库可以独占重开。
- 新便携包包含 13 个成员，包括 LICENSE。解压后的成员清单、逐文件散列、ZIP 散列和 EXE 产品属性均已核对。

当前归档与 EXE 校验值记录在打包摘要中。[打包摘要](evidence/package.json)

## 历史候选包验收

上一轮品牌迁移候选包完成了 278 项原生断言，包括三份旧配置副本、回环 Agent、Helper 独立测试及运行退出检查，另有两次普通启动。相关历史汇总与原始散列保留在验收摘要中。[历史最终核对](evidence/final-checks.json) · [历史验收记录](RELEASE-ACCEPTANCE.md)

这些结果适用于当时验收的二进制、依赖与主机环境。重新构建会生成新的二进制和归档散列，每轮发布应记录其实际执行的检查范围。

真实桌面视觉、空白记事本和其他 Windows 版本需要在目标环境继续验收。额外 AI 供应商和自定义模型也需要单独测试。[已知限制](KNOWN-LIMITATIONS.md) · [安全审查](SECURITY-REVIEW.md)

## 数据兼容与发布输入

默认数据目录为 %LOCALAPPDATA%\Asimi，数据库和进程锁分别为 asimi.db 和 asimi.lock。旧配置按升级指南备份并迁入后，应用会检查并转换旧数据库文件名。升级前关闭应用并备份实际数据目录。[升级指南](UPGRADE-GUIDE.md)

工具链、缓存、本地测试配置、数据库、原始日志和构建归档保存在被 Git 忽略的路径。源码仓库保留构建输入、测试输入、依赖锁文件及经过检查的文档。[品牌清单](BRANDING-INVENTORY.md) · [清理清单](CLEANUP-INVENTORY.md) · [贡献指南](../../CONTRIBUTING.md)
