# Screen capture privacy

The Computer and Assistant capture actions open a local dialog. Display
enumeration returns geometry and names. Pixels are sampled only when the user
confirms the selected display and clicks Capture. The model receives a
screenshot only after the dialog displays that preview and the user confirms
the destination and Send action.

Before sending, review the visible text and the provider, model and API host.
Crop to the relevant area or cover private messages, passwords, tokens and
payment details. A solid mask changes the image bytes before upload. The
application does not try to detect sensitive text automatically.

The backend issues a random capture ID and an independent upload token. The
token refers to the exact encoded preview, conversation, provider configuration,
model and image detail. A different preview, modified destination or expired
cache entry requires another review. A confirmed attempt consumes the token.
It follows the existing unknown-outcome policy on connection loss.

Capture pixels remain in the Go process and the current WebView preview.
The UI revokes its object URL on replace, expiry, discard or close. Go clears
mutable buffers on removal, consume, expiry or shutdown and drops the worker's
request image references after the HTTP operation. Runtime strings, graphics
buffers and operating-system paging prevent a secure-memory-erasure guarantee.

SQLite v3 retains confirmed capture dimensions, timestamp, provider/model,
hash and request IDs in message metadata. The preview is not an ordinary
attachment and has no persistent restore path. Usage and text replies can
contain information derived from the screen. Clearing the conversation removes
its visible analysis and capture metadata under the existing history policy.

The live validation CLI reads `DEEPSEEK_API_KEY` only in explicit live mode.
It sends fixed text and a generated image to the official endpoint. The
production app uses the Phase 2B DPAPI vault. Test evidence records outcomes
and reported metrics, without credentials or screenshot pixels.
