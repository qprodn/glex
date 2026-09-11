# Merchant config is one encrypted JSON blob; its schema lives in Go

Each Channel's credential shape is essentially different (WeChat Pay: mchid,
APIv3 key, certificate serial, merchant private key; Alipay: appid plus key
mode or certificate mode; Stripe: secret and webhook secrets). Decided
2026-09-10 at pay module MVP planning:

- A Merchant Account stores its channel config as a single JSON document,
  encrypted whole per ADR 0018, instead of structured columns. The schema
  of each channel's document is defined once in Go beside its adapter and
  is the single source for the back office: it renders the dynamic form,
  validates writes, and produces the stored masked echo (`****ab12`) that
  list and detail views serve — displaying never requires decrypting.
- Considered and rejected: structured columns — every future channel
  (Stripe, UnionPay, then service-provider mode) becomes a table migration
  and a wider sparse row; a generic key-value table — loses the grouping,
  ordering and validation the form needs anyway.
- Consequence: adding a channel is code, not schema. Losing the master key
  still loses the configs (ADR 0018); the masked echo survives only as a
  hint of what was set.
