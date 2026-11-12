# Credential security — Phase 2B

API secrets are submitted through the narrow native business call and encrypted
with Windows current-user DPAPI. SQLite holds encrypted blobs separately from
ordinary provider/model configuration. Profiles refer to credential IDs. No
plaintext export or frontend credential-read operation is provided.

The password input is cleared after a successful save. The frontend retains only
credential presence. It does not use LocalStorage for keys. Backend decryption
occurs for the requested provider; plaintext byte buffers are cleared as soon as
practical. Go strings, HTTP header copies and runtime memory management prevent
a complete memory-erasure guarantee. Do not treat buffer clearing as protection
against a debugger in the same user account.

DPAPI uses CryptProtectData/CryptUnprotectData without LOCAL_MACHINE scope and
with UI forbidden. Failed decryption fails closed. Corrupt data is not silently
replaced with plaintext. The Windows user/profile boundary is provided by DPAPI;
the automated suite cannot substitute a corrupt blob test for an actual different
Windows-account test.

Endpoint configuration rejects embedded credentials, query strings and fragments.
HTTPS is the default; HTTP is restricted to loopback or explicitly trusted private
literal addresses. TLS certificate verification cannot be disabled. Redirects do
not forward the API key. No Windows proxy configuration is changed. The application
does not automatically connect after saving a profile or key.

Provider bodies and native transport details are not shown as raw errors. Stable
error codes are translated in the frontend. Requests and stream events contain
request/conversation/message IDs and output deltas, never credential contents.
User-authored messages and selected pictures remain sensitive local application
data; DPAPI encryption of credentials does not encrypt the complete conversation
database or attachment directory.

DPAPI protects data at rest. It does not defend against malicious software
running as the same Windows user, process memory inspection, an already unlocked
session or an authorized provider retaining transmitted data under its terms.
`store=false` is a Responses API setting, not a universal zero-retention promise.

Primary references:
[CryptProtectData](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata),
[CryptUnprotectData](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptunprotectdata).
