# Asimi Community 发布安全审查

独立代码审查核对了 Wails 与 PE 品牌、Windows 图标与权限清单、SQLite 与 DPAPI、Helper 目标绑定及包清单。审查发现默认启动和部分旧验收入口还指向旧 EXE。现已同步当前构建路径，并保留显式的旧包验收路径。最终可执行文件通过普通启动与原生回归。[发布验收](RELEASE-ACCEPTANCE.md)

| 范围 | 状态 | 结论 |
| --- | --- | --- |
| Windows 权限与升级 | VERIFIED | Windows 清单使用普通用户权限。默认数据目录、DPAPI、数据库和 Helper 协议使用已有兼容标识。旧凭据完成认证，已有记录保留。 |
| 审批与本地执行 | VERIFIED | 完整 Go 与 Windows 回归覆盖动作审批、哈希绑定、执行前复核、独立验证、Stop、恢复和文件身份检查。 |
| Git 和电脑 | VERIFIED | 回环 Agent 保留已有修改与索引。合成截图与专用 TestHost 覆盖确认后外发、遮挡、UIA、目标验证及结果回读。 |
| 最终 ZIP 敏感信息 | VERIFIED | 精确成员清单与有限内容扫描通过。扫描未检出凭据、私钥、数据库、个人绝对路径或禁止打包的成员。 |
| 当前工作树和 Git 历史 | CONDITIONAL | 扫描可达提交和有界工作树，未发现凭据阻断项。保留的历史及原始验证报告仍含本机工作区路径，公开源码前需审查脱敏。 |
| 公开分发 | PENDING | Community 已选择 MIT 许可证。验收摘要对应选择许可证前的候选包。正式发布需审查公开内容，并核验包含应用许可证和第三方声明的新归档。 |

扫描明细保存在本地 `docs/release/evidence/secret-scan.json`。[最终核对摘要](evidence/final-checks.json) · [第三方声明](THIRD-PARTY-NOTICES.md) · [许可证决策](LICENSE-DECISION.md)
