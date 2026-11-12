# DeskPilot AI — Phase 2C 工程报告

2026-11-11，Asia/Shanghai。结论为 **CONDITIONAL GO**。DeepSeek 真实文本、合成图片和 Windows 工程验收通过。真实桌面内容须由用户在应用内选择显示器、预览并主动确认后验收。 [验收清单](PHASE-2C-ACCEPTANCE.md) · [最终验证](phase2c-evidence/final-verification.json)

| 模块 | 实现与证据 |
|---|---|
| Windows 截图 | 沿用 Phase 0 验证过的 BitBlt 和 GetDIBits 路线，独立封装只读驱动。枚举当前显示器，重查所选设备，使用有符号物理坐标和固定线程 PMv2 上下文。Phase 0 的 900×560 原型作为既有证据。 |
| DPI 和显示器 | 列表提供设备名、主屏标记、物理与逻辑范围、DPI 和缩放。实机枚举一块 2880×1800、173 DPI、180% 缩放的显示器。负坐标、不同分辨率和混合 DPI 的坐标边界用合成拓扑验证。 |
| 本地预览 | Assistant 与 Computer 复用同一流程。用户可选择整屏或物理像素矩形，预览后可裁剪、覆盖隐私区域、调整 PNG/JPEG 编码并丢弃。 |
| 上传权限 | 预览和发送分两步。Go 将单次确认绑定到截图 SHA-256、当前会话、配置快照、模型及图片 detail。裁剪、过期、丢弃、修改目标或重发会使旧确认失效。 |
| 视觉分析 | 原 BYOK、DPAPI、Provider Adapter、SSE、取消及用量流程处理截图请求。分析提示覆盖可见文字、应用、窗口、UI 元素、错误、建议和不确定项。越界坐标或无效结构使用原始文字并提示解析结果。 |
| 存储与历史 | SQLite v3 现有消息元数据记录尺寸、时间、哈希、请求、供应商及模型。原始图片没有附件或恢复路径。后续追问可使用文字分析。 |
| 资源 | Win32 线程恢复 DPI 上下文，释放 Bitmap 和 DC。合成 GDI 连续读回 128 次，句柄计数前后为 2。预览缓存最多两项，2 分钟过期，编码最多 12 MiB。 |
| 安全回归 | 原审批、文件提交见证、执行记录及 RECOVERY_REQUIRED 继续通过定向验证。视觉路径只提供观察及说明，模型结果没有操作入口。 |

原型证据和设计说明见 [Phase 0 原生证据](evidence/native.json)、[视觉架构](COMPUTER-VISION-ARCHITECTURE.md) 与 [截图隐私](SCREEN-CAPTURE-PRIVACY.md)。

## DeepSeek 实测

通过已有 Chat Completions Adapter 向官方端点发出恰好两次请求。模型为 `deepseek-flash`，按官方协议用内联 PNG 和 `detail=high` 发送生成的测试图。测试只临时读取环境密钥。 [DeepSeek Vision](https://api-docs.deepseek.com/guides/vision/) · [协议](https://api-docs.deepseek.com/api/create-chat-completion/)

| 测试 | 输出上限 | HTTP | 首个增量 / 总耗时 | 输入 / 输出 / 总 Token | 结果 |
|---|---:|---:|---:|---:|---|
| 文本 SSE | 128 | 200 | 1554 / 1681 ms | 16 / 8 / 24 | 8 个增量，正确返回测试标识 |
| 合成图理解 | 256 | 200 | 1296 / 1411 ms | 225 / 21 / 246 | 正确读出标题与数字，识别红圆和蓝三角 |

两次都收到提供方请求 ID 和 Usage。缓存输入 Token 均为 0。响应中无实际费用字段。应用继续按用户保存的费率快照估算费用，缺少费率时显示未知。 [实测记录](phase2c-evidence/deepseek-live.jsonl) · [DeepSeek 计价](https://api-docs.deepseek.com/quick_start/pricing/)

## Windows 验证

- 六个 Go 包通过，共 81 个顶层测试、96 个子用例。6 项 Vue Store 回归通过，Go vet、依赖校验、Vue 类型检查和桌面构建通过。 [Go 定向测试](phase2c-evidence/directed-tests.log) · [Vue 回归](phase2c-evidence/frontend-tests.log)
- 六个 Wails 进程通过 136 项断言，含 36 项屏幕分析流程断言。回环服务收到与预览哈希一致的 207 字节遮挡 PNG。追问携带 0 张图片，SQLite 和日志通过图片字节检查，截图附件数为 0。 [原生验收](phase2c-evidence/native-2c7b976d15f24b428decf15e0bdcf70e/summary.json)
- Wails 验收覆盖英文浅色、中文深色和紧凑窗口布局。所有进程按窗口优先顺序退出且退出码为 0，SQLite 可独占重新打开，1412 计数为 0。双语各 716 个键，605 个旧翻译值完整保留。277 份历史报告和证据哈希一致。 [兼容性](phase2c-evidence/compatibility.json) · [双语检查](phase2c-evidence/i18n-check.json)

验收构建在 [DeskPilotAI.exe](../build/bin/DeskPilotAI.exe)。工作区沿用包含 Phase 2B 未提交成果的分支 `codex/phase2b-byok`。

## 已知范围

合成图实测覆盖清晰英文标题和几何元素。真实桌面 OCR、中英文混排、小字号终端和复杂 UI 的识别质量待用户实测。不同显示器组合和混合 DPI 硬件尚未实测。当前路径读取可见屏幕像素，最小化窗口、HDR、受保护窗口和独占全屏不在本次通过范围。1412 保留 Phase 2A 的 CHARACTERIZED 结论。Phase 2D 等待用户决定。
