# DeskPilot AI — Phase 2C 验收

2026-11-11，Asia/Shanghai。**CONDITIONAL GO**。 [工程报告](PHASE-2C-REPORT.md) · [最终验证](phase2c-evidence/final-verification.json)

| 验收项 | 结果 |
|---|---|
| DeepSeek 文本 SSE | PASS，真实 HTTP 200，文本增量、结束状态、请求 ID 和用量已验证。 |
| DeepSeek 图片理解 | PASS，真实合成图返回正确标题、颜色与形状。请求中的图片为程序生成的 PNG。 |
| 显示器与 GDI | PASS，实机枚举单屏，合成 GDI 验证负坐标和像素读回，重复运行句柄数稳定。 |
| 缓存与上传授权 | PASS，拒绝未确认捕获、伪造及过期 ID、旧预览凭证、目标配置变更及重复发送。关闭服务清空缓存。 |
| 裁剪、遮挡与持久化 | PASS，裁剪尺寸与遮挡像素检查通过。Go 测试核对上传内容等于确认预览，SQLite 不含截图数据，追问没有重发截图。 |
| 结构化结果与失败处理 | PASS，合法 OCR 和 UI 结构通过，非法坐标、置信度、未知字段及附加内容使用文字回退。现有网络中断、取消、无 Usage 和工具拒绝测试通过。 |
| Wails 窗口流程 | PASS，36 项新断言覆盖两个入口、本地 Blob 预览、裁剪、遮挡、JPEG、确认上传、结构化结果和追问。中英文及明暗主题布局检查通过。 |
| Phase 1、2A、2B | PASS，六个定向 Go 包通过，含原审批、文件验证、崩溃恢复、Provider、DPAPI 和取消。 |
| 窗口优先退出 | PASS，六个原生进程通过 136 项断言，全部退出 0。SQLite 可独占重新打开，1412 计数为 0。 |
| 实际桌面和硬件矩阵 | PENDING，用户主动验收真实截图。真实多屏、混合 DPI、HDR、全屏及复杂界面识别仍需覆盖。 |

原生流程使用 `./scripts/accept-phase2c.ps1` 和项目隔离数据目录。验收中的 Wails 像素来自合成驱动，回环请求使用专用测试密钥。 [原生结果](phase2c-evidence/native-2c7b976d15f24b428decf15e0bdcf70e/summary.json)

本地定向结果见 [Go 测试](phase2c-evidence/directed-tests.log)、[前端回归](phase2c-evidence/frontend-tests.log) 与 [双语检查](phase2c-evidence/i18n-check.json)。真实请求见 [DeepSeek 记录](phase2c-evidence/deepseek-live.jsonl)。

## 用户桌面验收

1. 打开「模型」中的 DeepSeek 预设，确认 `deepseek-flash` 和端点，在应用内用 DPAPI 保存 API Key。
2. 在「电脑」或「助手」选择显示器，确认范围并截图。先核对本地预览、尺寸、方向和比例，按需裁剪或遮挡。
3. 核对 API 主机、模型、图片大小及费用提示，再点「发送给 AI」。检查文字、界面元素、疑点和建议与可见内容是否一致。
4. 在同一会话追问，然后检查 Usage 和历史。关闭、丢弃或等待预览过期，确认下一次分析需要新的截图。

使用无敏感内容的窗口复核混排、小字号及实际错误文字。Phase 2D 等待用户确认。
