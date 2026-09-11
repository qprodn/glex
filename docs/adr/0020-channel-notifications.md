# Channel notifications: a second auth-exempt route set; the signature is the auth

Payment channels push outcomes to us over plain HTTPS with no login
session — glex's only auth-exempt paths so far are login and logout. Decided
2026-09-10 at pay module MVP planning:

- `/pay/notify/{channel}/{merchant_id}` joins the exempt route set (the
  merchant discriminator rides in the path because verification needs the
  merchant's keys before the payload can be trusted). The channel's request
  signature is the authentication: an unverified payload is dropped and
  logged (`Warn`), never processed. A minimal Redis fixed-window limiter
  guards these endpoints — there is no general rate-limit middleware to
  reuse, so a small one is built for exactly this route set.
- Every accepted notification is first recorded write-once in
  `pay_notify_log` — raw body, signature headers, verify and processing
  outcome — unique on (channel, notify_id), which absorbs the channels'
  guaranteed redelivery. State transitions then run as CAS from the
  previous state, so a duplicate or out-of-order notification (a close
  receipt after success, say) is a no-op, not an error. The log is the
  evidence stream for disputes and the anchor for a future reconciliation
  feature; it sits in the operlog skip set (no Principal, high volume —
  the row itself is the record).
- The channel is answered in its own ACK dialect (WeChat v3 JSON
  `{"code":"SUCCESS"}`, Alipay the literal `success`) after durable
  processing; any other answer makes the channel retry, which the dedup key
  absorbs.
- Considered and rejected: routing callbacks through the authenticated API —
  impossible, the channel is the caller; processing before logging — a crash
  in between would destroy the only evidence of what the channel asserted.
