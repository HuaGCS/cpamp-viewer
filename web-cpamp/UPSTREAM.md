# CPA Manager Plus UI provenance

This frontend is a read-only derivative of the CPA Manager Plus web UI.

- Upstream: https://github.com/seakee/CPA-Manager-Plus
- Base release: `v1.12.8`
- Base commit: `7c4cbeadaa801613e98ea6874b902844f09e59c6`
- Selected read-only updates: `v1.14.1`
- Update commit: `aa8c5e9886b42ec82d78a419a1e1f59700ca90c5`
- License: MIT (`UPSTREAM_LICENSE`)
- Copyright: Copyright (c) 2026 Seakee

Viewer 2.5.0 adapts the upstream request/routed/response-model presentation,
readable account-source precedence, mobile monitoring refresh layout and provider
recognition, including Meta/Muse. It also adapts historical-coverage notices and
Devin cache accounting. A separate read-only usage-status page shows safe counts,
time ranges, storage size and maintenance activity without administrative actions.
Its adapters also carry safe request flags and explicitly supplied
request/response service tiers. Price-configuration attention is presented through
an independent read-only Viewer status endpoint and notice.

The Viewer retains its own server-side credential boundary, six-route shell,
API adapters, public Access Guard quota projection and complete Codex quota query.
Raw request/session identities, access-token hashes, management stores and mutation
controls are excluded from the public data and application routes.

See `docs/UPSTREAM_BASELINE.md` in the Viewer source distribution for the visible
data contract and compatibility boundaries.
