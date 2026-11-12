# Asimi 0.2.1 清理清单

基线为 `codex/phase2b-byok` 的 `4accd35e6b7a801274e034adc103f59df37e686c`。工作区已有上一轮源码和文档，暂存区为空。按 Git 状态、构建输入、原生验收入口和发行包清单核对下列路径。

| 分类 | 路径及类型 | 当前用途与引用 | 处理计划 |
| --- | --- | --- | --- |
| A | `main.go`、`internal/`、`frontend/src/` | 应用入口、模块、UI、Helper、安全实现、迁移、生产和测试引用 | 保留源码与稳定标识，只改品牌所需文案。 |
| A | `go.mod`、`go.sum`、`frontend/package*.json`、`scripts/`、`validation/` | 锁定依赖、构建、动态 Wails RPC 和多阶段验收入口 | 保留，扩展现有验收参数。 |
| A | `frontend/node_modules/`、`frontend/dist/`、`.phase0-tools/`、`.phase0-cache/` | 当前依赖、Go 嵌入资源、锁定工具链和离线构建缓存 | 保留供开发和重建，发行包只包含嵌入构建结果。 |
| B | `docs/phase*-evidence/`、`docs/mvp01-evidence/`、`docs/0.2/`、`docs/0.2.1/` | 历史验收、资源及翻译基线，部分由历史脚本读取 | 仓库保留，排除在 Asimi ZIP 之外。 |
| B | `build/bin/DeskPilot*.zip`、`build/bin/v021-extracted-c95889c8f01d455da2e09a678862ee4b/`、旧 Phase/MVP 构建目录 | 历史发行包和对应测试入口，0.2.1 最终解压路径由已发布到工作区的证据引用 | 保留用于历史核对与升级。 |
| B | `.phase1-data/`、`.phase2*-data/`、`.mvp01-data/`、`.v02-data/`、`.v021-data/` | 隔离的旧版数据库、测试与升级样本，包括 DPAPI 密文。验收只读复制选中的样本 | 保留有引用的基线、升级样本和最终长测数据。其他已确认的测试批次可归入本地归档，排除于 Asimi ZIP。 |
| C | 旧测试配置下的 WebView2 缓存目录 | 隔离测试产生的浏览器和 GPU 派生数据，可由 WebView2 重建 | 可清理。测试数据库、附件和浏览器设置保留。 |
| C | `build/bin/v021-extracted-*/` 中的非最终解压目录 | 上一轮独立验收解压副本。构建、Go 嵌入和运行时无引用。最终验收索引只指向上表保留的目录 | 按确切清单逐一校验位置、Git 忽略状态和成员，再清理。 |
| C | `build/bin/v021-licenses-audit/` | 上一轮生成的生产 sourcemap 审计副本，第三方通知已存档。该目录可由 Vite 重建 | 清理旧输出，当前依赖审计可重新生成临时输出。 |
| D | `build/notepad-policy/ControlHostBuild.cs`、`build/notepad-native/`、`build/phase2a-tool-tests/` | 隔离测试构建和生成的哈希固定文件。Notepad 策略和恢复回归依赖这些构建路径 | 保留，排除于发行包。 |
| D | 旧 Mock 与 MVP 页面、动态 Wails RPC、旧阶段验收分支 | 现有离线、恢复、兼容及回归入口 | 保留。没有依据删除正式代码。 |

执行顺序为先冻结清理范围，再调整品牌与 Windows 资源，构建后从全新目录验收。报告用实际哈希、退出状态和证据确认结果。

已删除 8 个旧版解压副本、1 个 Asimi 中间解压副本和 1 个 Vite 许可审计输出目录，共 239,969,865 字节。解压副本在删除前均与各自的成员散列清单匹配。旧最终解压目录、历史 ZIP、所有隔离配置和源码继续保留。D 类没有发现满足删除条件的正式代码。逐项记录位于本地 `docs/release/evidence/cleanup.json`。

后续清理删除了八个旧测试数据目录下的 952 个可重建 WebView2 缓存目录，释放 994,866,012 字节。58 个旧测试批次移至本地归档，清理后空置的 `.phase1-data/` 和 `.phase2a-data/` 已移除。升级样本、最终长测和旧脚本需要的基线文件仍在原路径。复核了五个保留的测试数据库和 Asimi ZIP 的 SHA256。当前 Asimi 测试记录和离线构建工具链保留在工作区。

GitHub 工作副本的隔离验收数据统一放在 `.local-validation/`，按阶段或版本存放。该目录由 Git 忽略。旧验收记录中的 `.版本-data/` 路径对应 `.local-validation/版本/`，例如 `.v021-data/` 对应 `.local-validation/v021/`。

本地归档位于 `build/bin/local-validation-archive-20261112-143049/`。如需复现某个旧批次，先关闭对应测试进程，再按本地 `docs/release/evidence/local-data-cleanup.json` 中的路径将该批次移入对应目录。GitHub 工作副本使用上面的新路径。
