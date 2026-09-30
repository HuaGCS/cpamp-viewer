# CPAMP upstream baseline and Viewer 2.5.0 adaptations

The shared UI base remains CPA Manager Plus `v1.12.8`, revision
`7c4cbeadaa801613e98ea6874b902844f09e59c6` (MIT). Viewer 2.5.0 selectively adapts
read-only presentation and API capabilities from `v1.14.1`, revision
`aa8c5e9886b42ec82d78a419a1e1f59700ca90c5`, released 2026-09-26.

## Visible contract

The six routes are Dashboard, Usage Analytics, Request Monitoring, Quota,
public Access Guard Key Quota and Usage Status. The Viewer uses its own server-side API projection
and credentials. Public clients receive names, approved statistics and anonymous
`view_*` IDs.

- Request Monitoring separates requested, CPA-routed and upstream response models.
  A mismatch requires both routed and response models and unequal trimmed values.
  An ordinary requested alias routed to another model is not a mismatch by itself.
- `response_model`, optional request/response service tiers, and strict boolean
  `generate` / `stream` values survive projection. Missing values remain unknown.
  A legacy `service_tier` is not evidence of two separately observed tiers.
- Readable account names take priority over opaque or redacted sources. The mobile
  monitoring refresh controls and Devin provider recognition are included.
- Missing-price attention uses the fixed upstream read endpoint
  `GET /v0/management/model-prices/runtime-models` through a 60-second shared cache.
  The browser receives a bounded safe list and counts. It has no price sync/edit
  action. An older CPAMP lacking this endpoint yields `available:false`.

- Historical analytics coverage is projected from strict known enums, integer
  counts and timestamps. Monitoring and Usage Analytics warn when raw detail is
  missing while preserved aggregates remain available. Range changes cannot reuse
  the previous range's coverage evidence. Unknown upstream limitation text is
  reduced to a fixed generic flag, not exposed as diagnostics.
- Meta/Muse source recognition and Meta API-key provider counts are supported.
  Devin uses read-included/creation-separate cache semantics in shared helpers;
  already normalized Manager analytics are not normalized again.
- Usage Status calls fixed `GET /v0/management/usage/maintenance`, validates its
  schema and publishes only counters, optional timestamps, storage bytes,
  readiness booleans and known activity enums. It uses a 30-second shared cache,
  five-second budget, failure cooldown and stale-success retention. Validated GET
  avoids assumptions about reverse proxies rewriting HEAD 204 to 200.

Raw session/parent-session IDs, access-token hashes, client IPs, User-Agent,
response metadata, trace identifiers and credential bodies remain outside the
public DTO. The display's masking switch is not a permission boundary.

## Quota behavior retained from 2.3.3

The Codex quota page uses a fixed server-side CPA wrapper for
`GET https://chatgpt.com/backend-api/wham/usage`. Complete ordinary, Spark and
code-review inventories replace passive snapshots, including valid empty results.
Each window retains its actual duration and observation time. Queries use the
existing CPAMP connection, a shared 60-second cache and bounded concurrency.

Access Guard reads only the server-configured publication scope. Its 15-second
cache, safe public DTO and fixed read route remain independent of management
controls.

## Non-Codex quota snapshots

Meta/Muse, Devin, xAI and existing supported providers use the fixed read-only
`POST /v0/management/quota-snapshots/query`, with accounts constructed only from
authenticated auth-file metadata, batches up to 200 and a shared 60-second cache.
Codex retains the complete fixed wham query above. No credential-body download,
Meta DCA exchange or quota snapshot write is added.

Snapshot windows retain null percentages, actual reset times, model scope and
stale markers. xAI weekly metadata without a percentage stays visible as unknown;
zero-value empty monthly windows are omitted. A valid empty inventory clears prior
windows; failed reads preserve stale success or fall back to provider-matched
header observations. Browser callers cannot select accounts, URLs or payloads.

## Compatibility

Current and historical upstream data may lack response-model or request-flag
fields. The Viewer continues showing those events without inventing missing data
or mismatch evidence. CPAMP must itself collect the new fields for them to appear.
Database migrations, credential management, model-price editing, quota resets and
Manager updates remain operations of the original management application.

Archive creation, verification, raw cleanup, compaction, import sessions and raw
JSONL exports are not public Viewer operations. A status page is not maintenance
authorization. No new deployment environment variables or secret files are needed.

## Delivery

Version `2.5.0` and `latest` are published from the same tested linux/amd64 image.
`latest` is a moving tag; the fixed tag preserves this release. Evidence and test
limits are recorded in `docs/RELEASES/2.5.0.md`.
