# Asimi 0.2.1 发布验收

验收环境为 Windows 11 Home 25H2 x64，内部版本 26200，WebView2 157.0.4341.46。原生验收从新解压目录运行，使用隔离工作目录、测试配置和本机回环服务。

| 项目 | 状态 | 证据 |
| --- | --- | --- |
| Go、前端和构建 | VERIFIED | 完整 Go 测试、`go vet`、`go mod verify`、三组 Node 测试、TypeScript 检查、Vite 和 Windows Wails 构建均通过。 |
| 双语品牌和首次使用 | VERIFIED | 英文默认和中文切换、向导、侧栏、WebView 标题、About 及产品描述、离线、设置和重启均通过。原翻译键和插值保留。 |
| 原生窗口与便携启动 | VERIFIED | 两次普通启动核对 Asimi 窗口标题、PE 产品与版本、EXE 关联图标和原生窗口图标。子进程隔离的默认 `DeskPilotAI` 路径建立成功。每次都正常退出，数据库可独占重开。 |
| DeskPilot 0.2 和 0.2.1 升级 | VERIFIED | 三份隔离测试副本使用原 schema 6，Provider、DPAPI 密文、主题、语言、消息、任务、动作及日志的已有行保留。旧测试凭据完成一次回环认证。三个原件哈希一致，副本 SQLite 完整性均通过。 |
| 原有 Windows 回归 | VERIFIED | 九个阶段共 193 项断言。覆盖回环连接和凭据、审批、快照预览与遮挡、外发确认、专用 TestHost、Stop、历史和恢复。 |
| 回环 Agent | VERIFIED | 两个阶段共 34 项断言。完成真实 Go 夹具的分析、Diff、单动作审批、修改、回读、定向 Go 测试和总结。现有用户修改、暂存索引和测试文件保留。 |
| Helper 独立测试 | VERIFIED | 从最终解压目录运行，验证严格协议、目标和文本守卫、SetValue、Invoke、独立读回和管道关闭。 |
| 包与运行退出 | VERIFIED | 12 个允许成员，11 个内容哈希和 ZIP 哈希匹配。旧版三份归档哈希匹配历史证据。测试进程退出，1412 保持 CHARACTERIZED。 |
| 本次外部服务 | CONDITIONAL | Asimi 本轮使用回环验收，共 0 次外部模型请求。上一版真实 DeepSeek 的证据保留在历史报告中。 |
| 真实桌面、空白 Notepad 与其他 Windows 版本 | PENDING | 用户主动现场验收。 |

最终包共通过 278 项原生断言，另完成两次普通启动和一次独立 Helper 验收。启动和旧版配置测试共 51 项断言。分阶段日志保存在本地 `docs/release/evidence/`。[最终核对摘要](evidence/final-checks.json)

上一版两小时稳定性证据与真实供应商测试仍可查阅。更名候选包验收使用上表列出的场景和条件。[DeskPilot 0.2.1 历史验收](../0.2.1/ACCEPTANCE-REPORT.md)
