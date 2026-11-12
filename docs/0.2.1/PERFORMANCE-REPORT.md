# DeskPilot Community 0.2.1 performance

The measurement host is Windows 11 25H2 x64. These are fixture measurements with warm local caches and concurrent validation activity. Other Windows versions and machine profiles require separate measurements.

## Bounded workspace scan

| Generated files | Files visited | Bytes read | Analysis time | Project inspection time |
| ---: | ---: | ---: | ---: | ---: |
| 250 | 250 | 5,772 | 29.997 ms | 8.772 ms |
| 2,000 | 1,000 | 23,000 | 120.141 ms | 16.063 ms |
| 11,000 | 1,000 | 23,000 | 119.460 ms | 13.858 ms |

All three runs indicated truncation because the search result or inventory caps applied. At 2,000 and 11,000 files, the independent 1,000-file scan cap also applied. The fixture used identical small Go files and a Go manifest. [Raw measurements](evidence/workspace-performance.json)

## Stability workload

The short loopback run completed 105 successful streaming turns in five conversations, five cancelled requests, 20 completed local tasks, and five pause/resume/cancel cycles over 120.016 seconds. The closed database could be reopened exclusively. The test used generated files and a loopback fixture. [Short-run summary](evidence/soak-3bed3cea59dc49e9b2e751cd940e8b3b/summary.json)

The final executable completed the full loopback workload over 7,200.020 seconds on November 12, 2026, from 09:56 to 11:56 China Standard Time. It completed 105 streaming turns in five conversations, five cancellations, 20 local tasks, and five pause, resume and cancel cycles. The app closed normally. An exclusive database reopen and a separate read-only SQLite integrity check passed with zero foreign key violations. [Run summary](evidence/soak-9d57debd74ac4ac092656314cefa9e8c/summary.json) · [Shutdown log](evidence/soak-9d57debd74ac4ac092656314cefa9e8c/runtime.log)

The 233 process samples recorded a main working set of 44.4–46.9 MiB and 479–552 main process handles. The GDI count was 11 throughout, USER handles ranged from 30 to 34, and six WebView2 child processes were present at every sample. Their combined working sets ranged from 384.0 to 518.7 MiB and may count shared memory more than once. Seven Go runtime snapshots recorded 14–16 goroutines and 3.5–4.7 MiB of heap in use. No helper process appeared in the stability samples. [Process samples](evidence/soak-9d57debd74ac4ac092656314cefa9e8c/samples.jsonl) · [Go runtime snapshots](evidence/soak-9d57debd74ac4ac092656314cefa9e8c/report.json)

The workload is driven from the native WebView acceptance script through the product's Wails dispatcher. It covers streaming requests, cancellation, task state, persistence and native shell resources. The separate frontend and native regression exercises conversation rendering, approval controls and UIA against the TestHost. A two-hour continuous interactive conversation-rendering or UIA workload remains PENDING.

To reproduce, run `./scripts/soak-v021.ps1 -Bin build/bin/<extracted-directory> -Seconds 7200` from the repository root. The runner writes timestamped sample rows and a separate final report after orderly shutdown.
