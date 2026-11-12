# Asimi Community 0.2.1 发布报告

**受控试用 VERIFIED。正式公开分发 BLOCKED。** Windows x64 便携候选版已完成品牌迁移、重新构建和独立解压验收。Asimi 使用既有本地数据位置，可从 DeskPilot 0.2 或 0.2.1 升级。现有用户的主题、语言、Provider、凭据和历史已在测试副本上复验。

| 交付 | 位置 |
| --- | --- |
| 便携 ZIP | `build/bin/Asimi-Community-0.2.1-win-x64.zip` |
| 归档校验 | `build/bin/Asimi-Community-0.2.1-win-x64.zip.sha256` |
| 应用 | `build/bin/asimi-community-0.2.1/Asimi.exe` |
| 逐文件校验 | `build/bin/asimi-community-0.2.1/SHA256SUMS.txt` |

ZIP SHA256 为 `CB0BA053C0A4477AA828C5FB94C38E0E46AD88C207630E4FC1C784C3CBC4018B`。EXE SHA256 为 `70FB396958AC2432A7B478F41A5DFDAE20515EBA18866023D23CA10F11B98154`。[发行成员和散列](evidence/package.json)

包内含 12 个文件，包括 Asimi、两个固定 Helper、各自的原生配置、五份指南和声明、一份隐私说明及逐文件校验清单。第三方声明记录 13 个已链接的 Go 模块和 9 个嵌入的前端包，以及系统运行时和图标来源。默认启动无需开发目录。[安装指南](INSTALLATION-GUIDE.md) · [第三方声明](THIRD-PARTY-NOTICES.md)

完整原生回归共 278 项断言，包含 3 份旧配置副本。另有两次普通启动和独立 UIA Worker 验收。三份历史 ZIP 的散列与历史证据一致。本轮删除 10 个可重建目录，共 239,969,865 字节。最终分支及 HEAD 与基线一致，暂存区为空，Git diff 检查通过。原始 Git 状态、差异统计及未跟踪文件清单保存在本地 `docs/release/evidence/`。[品牌清单](BRANDING-INVENTORY.md) · [清理清单](CLEANUP-INVENTORY.md) · [验收](RELEASE-ACCEPTANCE.md)

应用许可证和公开发布由项目所有者决定。公开源码前需审查历史报告里的本机路径。真实桌面视觉、空白 Notepad 和其他 Windows 版本仍待用户主动验收。[安全审查](SECURITY-REVIEW.md) · [已知限制](KNOWN-LIMITATIONS.md) · [许可证决策](LICENSE-DECISION.md)
