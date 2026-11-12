# Configuration architecture

The resolver applies effective defaults to missing preferences on read. Existing SQLite migrations v1–v6 and existing rows remain compatible. The application stores explicit overrides in the existing AI settings row. The configuration view labels defaults, detected data, presets, and user-defined values. A verified DeepSeek profile records its connection timestamp in profile metadata. Saving an edited profile invalidates that verification. Connection success updates the credential and default selection together, using Windows DPAPI for the key.

| Agent setting | Default and maximum |
| --- | ---: |
| Requests per task | 5 |
| Steps per plan | 6 |
| Context bytes | 32,000 |
| Output tokens per request | 1,536 |
| Request timeout | 60 seconds |
| Task deadline | 900 seconds |
| Estimated budget | USD 0.10 |

New runs snapshot these limits. Existing runs without a snapshot resolve to the 0.2 caps. Reviewed contexts bind the effective policy and locale to the review hash. The provider request timeout can be shorter than the task policy. Confirmed reset restores chat and Agent limits while preserving usable selections, credentials and history. When the stored preference document is unreadable, reset starts the preference document over and leaves provider and history tables intact.

The advanced panel also allows lowering the existing four-attachment and 20 MiB per-image defaults. The per-image limit applies to outbound attachments, including images from conversation history, and screenshots sent to a provider. Existing local attachment history remains readable and the independent screen capture and transport safety caps continue to apply.
