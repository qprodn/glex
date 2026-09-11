# Money is int64 minor units with an ISO 4217 currency code

glex had never represented money before the pay module, so whatever it
chooses becomes the repo-wide precedent. Decided 2026-09-10 at pay module
MVP planning:

- Every monetary amount is an `int64` counting the currency's minor units
  (fen for CNY, cents for USD) paired with a 3-letter ISO 4217 code.
  Floats are never used for amounts and SQL `numeric` is not introduced.
  Zero-decimal currencies (JPY) count their major unit — the rule is "minor
  units as ISO 4217 defines them for that currency".
- Per-channel unit conventions (WeChat Pay and Alipay speak fen on CNY;
  Stripe encodes minor units per currency) are absorbed inside each channel
  adapter; the domain model never converts.
- Considered and rejected: `numeric`/decimal — exact, but it drags a
  decimal type through proto, ent and every seam for arithmetic we don't do
  (amounts are only compared, summed and capped); float64 — binary
  fractions cannot hold 0.1.
- Consequence: formatting to yuan/dollars happens only at display edges.
  An author who forgets the unit is off by 100×, so monetary field names
  carry the convention: `amount` is minor units, always.
