# Communication contract

All saved connections are profile-local and revisioned. Secrets are manual account tokens protected by the existing Windows DPAPI vault; they are never included in connection/action wire JSON. There is no OAuth browser login, refresh-token flow, or arbitrary HTTP/subprocess tool.

Every operation, including reads and identity checks, requires a separately prepared, five-minute, single-use action approved with its immutable ID and SHA-256 hash. The approval binds origin, method/path, exact operation/resource, canonical payload, connection revision, and expiry. Saving configuration, rotating credentials, or revoking a connection invalidates previous approvals. Changing service kind/origin deletes the old credential. Saving a revision also clears the verified identity. Restart expires pending approval and changes interrupted execution to UNKNOWN without dispatch. CancelConnectorAction targets one action; StopAll cancels active operations and consumes all pending approvals.

Supported production cloud origins are api.github.com (GitHub), graph.microsoft.com (Graph), and slack.com (Slack). GitLab supports explicit self-hosted HTTPS origins. MCP supports an explicitly configured HTTPS origin or loopback HTTP origin at /mcp. Redirects are denied. Loopback HTTP for other built-ins requires the explicit test-only manager option, which the app does not enable.

| Kind | Operation | Exact resource | Request |
| --- | --- | --- | --- |
| GitHub / GitLab | repo.read, issues.list | owner/repository | {} |
| GitHub / GitLab | issues.create | owner/repository | {title,body} |
| GitHub / GitLab / Slack | identity.read | $identity | {} |
| Graph | identity.read, mail.list, calendar.list | me | {} |
| Graph | mail.send | me | {subject,body,to} (one bare recipient) |
| Graph | calendar.create | me | {subject,start,end,timeZone:"UTC"} (local ISO seconds in UTC) |
| Slack | messages.list | channel ID | {} |
| Slack | messages.send | channel ID | {text} |
| MCP | tools.list | $catalog | {} |
| MCP | tools.call | exact tool name | {arguments:object} |

Results are untrusted data. Account identity responses are reduced to bounded ID/displayName/verification time/revision; personal account response fields are discarded. GitHub/GitLab issue writes require a separate GET matching identifier/title/body. Slack sends require a separate exact-timestamp history read. Graph calendar writes require a separate event read matching subject/start/end. Graph mail creates an immutable-ID draft, sends it, then independently verifies the same immutable message's recipient/content and SentItems folder membership. A response status alone never establishes a completed external write. Any uncertain write/readback/cancellation becomes UNKNOWN and cannot reuse approval.

MCP supports the 2025-03-26 Streamable HTTP profile with initialization, session headers, initialized notification, JSON responses, and bounded SSE response events. It never executes server-initiated requests. Generic tool replies cannot establish independent effect verification, so tools.call remains UNKNOWN with an explicit verification-unavailable code; there is no automatic retry.

Lists return bounded first pages, not an exhaustive account export. UI history lists the newest 500 connector actions; stop and crash recovery always inspect the entire unsettled journal. Tests use loopback protocol servers for account services. The opt-in ASIMI_GITHUB_READ_ACCEPTANCE=1 test performs only public octocat/Hello-World metadata GET; no authenticated third-party account or remote write was used for acceptance.
