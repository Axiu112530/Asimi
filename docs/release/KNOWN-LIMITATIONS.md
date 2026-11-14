# Asimi 0.2.1 known limitations

| Item | Status | Scope |
| --- | --- | --- |
| Public source and binary distribution | PENDING | Asimi uses MIT. Public release still depends on the owner reviewing the history and packaging the distributable with the application and third-party notices. |
| Dedicated test window and other desktop targets | CONDITIONAL | A real DeepSeek vision request and Windows UIA verification passed on the dedicated fixture. Empty Notepad and arbitrary applications remain unverified. |
| Windows versions beyond the tested Windows 11 25H2 x64 environment | PENDING | The release evidence applies to its recorded host. |
| Additional provider configurations | CONDITIONAL | Custom endpoints use the advanced editor and need their own connection check. |
| Computer action suggestions and Harness desktop tasks | CONDITIONAL | The supported model-assisted target is the dedicated test window. Managed tasks execute one approved semantic action at a time; general multi-application autonomous planning remains pending. See the [implementation report](../reports/2026-11-14-perception-action-completion.zh-CN.md) for the tested build. |
| Read-only Git inspection | CONDITIONAL | Supported Git formats are shown in project inspection. The working copy remains subject to identity and hash checks before approved edits. |
| WebView2 shutdown diagnostic 1412 | CONDITIONAL | Retains the existing CHARACTERIZED conclusion. |
| Authenticated connector accounts | CONDITIONAL | Public GitHub was tested read-only. Private services and writes use local protocol fixtures. Account credentials are manual tokens. OAuth refresh and generic MCP effect verification are pending. |
