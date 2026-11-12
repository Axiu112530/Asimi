# Community 0.2.1 security review

- The provider connection test uses the fixed official HTTPS endpoint and sends a short text prompt. The password input clears on submission. DPAPI storage and profile/default updates are transactional after success. The UI and reports expose status and model identifiers. The explicit custom provider workflow retains the existing transport and consent rules.
- Git inspection is bounded and read-only. It declines unsupported metadata and configurations. File actions bind the exact live base hash, target identity, preview, expiry and approval. Command actions stay allowlisted and use separate approval and network confirmation where required.
- Computer suggestions originate from an explicitly selected observation and a separately approved image send. The new path is limited to the isolated TestHost. The app validates the structured reply against the observation and target, creates a separate proposal, and uses the existing execution and independent verification path. Actual desktop and empty Notepad need interactive acceptance.
- The existing Stop, shutdown and recovery paths govern consumed or interrupted approvals. Use isolated data directories and redacted evidence in automated runs.

Report suspected issues via the private channel described in the repository security policy. See the acceptance report for actual tests and release blockers.

Independent source review identified and rechecked three fixes. Git attributes fall back to bounded verified index data, lowered plan limits appear in reviewed provider input, and historical outbound images obey the active size limit. Regression tests cover each case, including viewing a historical image locally after lowering its send limit.
