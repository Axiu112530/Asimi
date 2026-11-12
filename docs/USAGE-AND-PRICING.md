# Usage and pricing — Phase 2B

Every admitted real-provider request gets a local request ID, provider/model IDs,
conversation/message association, status and timestamps. The same request path is
used by Assistant, connection test and Playground. Failed and cancelled requests
are records too; they may have incurred provider charges even when no usage was
returned.

Input, output, total, cached input and reasoning tokens are nullable. A missing
provider field remains null. Source is PROVIDER_REPORTED when supplied by the
provider and UNAVAILABLE otherwise; the UI does not turn an absent count into an
observed zero. ESTIMATED is reserved for clearly identified future token estimation.
First token latency is unknown until an output delta is observed. HTTP status,
total duration and provider request ID are recorded when available.

Prices are user configured per million tokens, with currency and effective date.
No live model price is hardcoded. Each request retains the price configuration
used for its estimate. Subsequent profile edits do not rewrite historical cost.

When input/output token counts and a price are available:

```text
uncached input = input tokens - cached input tokens
cost = (uncached input * input price
        + cached input * cached input price
        + output tokens * output price) / 1,000,000
```

When the provider reports cached input but its separate rate is not configured,
the estimate remains unknown. Cached tokens are a subset, not extra input.
Missing prices/counts or inconsistent counts leave cost unknown. Special image/request/tool billing is not inferred
from token counts. This estimate is not a supplier invoice or a hard spending cap.

The Usage view aggregates local records by provider and model and retains
currency boundaries. Unknown token/cost contributions remain visible; known
subtotals cannot establish complete totals when some requests lack usage. Today's
view uses the user's local calendar date. Request warnings and output/context
limits help control cost but cannot cap a provider account bill.
