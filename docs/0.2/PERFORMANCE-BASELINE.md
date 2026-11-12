# DeskPilot 0.2 性能基线

2026-11-12，在 Windows 11 25H2 x64 上测量最终解压版。Go 项目是有意设置错误的小型隔离样例。这是一次本机采样，未控制系统缓存，也未推断普遍性能。

| 指标 | 观测 |
| --- | --- |
| WebView 导航起点至工作台可操作 | 148 ms，重启 230 ms |
| 任务页切换 | 14 ms |
| 生成审阅上下文 | 三轮分别 62、60、71 ms，包含本地有界扫描和渲染 |
| DeepSeek Agent 响应 | 三轮分别 2,225、2,137、2,117 ms |
| DeepSeek SSE 首 token 和增量 | 多轮 JSON SSE 首 token 749 ms，8 个内容增量，完成 875 ms |
| DeepSeek Responses Vision | 合成图像 1,186 ms |
| 审批、事务写入、独立回读 | 74 ms |
| 已审批的 `go test` | 1,464 ms，含定向执行、结果落库及界面观测 |
| 三轮业务闭环及进程退出 | 9,634 ms |
| 重启验证及退出 | 793 ms |
| 采样主程序峰值工作集 | 49.0 MiB，重启 42.2 MiB。该值不含 WebView2 子进程 |

截图采集的真实桌面耗时、单独 UIA 查询耗时、SQLite 查询分布、独立 Diff 生成耗时、大项目和长会话资源占用仍待采集。任务界面使用状态事件和短合并延时刷新。确认发送时重新生成摘要并复核散列，保留审批期间的变更检测。运行时日志核验了存储关闭、窗口移除、Wails 退出顺序及数据库可独占重开。

[真实 Agent 各阶段计时](evidence/live-1f1bd16fdccc491694d4a6e9f8a95f14/stage-12.json) · [进程计时与内存](evidence/live-1f1bd16fdccc491694d4a6e9f8a95f14/summary.json) · [Provider 计时](evidence/provider-live.json)
