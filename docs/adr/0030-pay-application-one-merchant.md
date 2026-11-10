# A Pay Application binds one Merchant Account and notifies its caller

Pay collected money by naming a Merchant Account on each Payment, and a
Business Order Number was not unique. Callers that need their own result
address and their own order numbers had no identity to hang them on.
Decided 2026-11-10:

- A **Pay Application** is that caller. It is named by its own number — no
  separate app key — and by a display name, unique among Pay Applications
  that are not withdrawn. The display name may change; the number does not.
  At creation it binds exactly one Merchant Account, and that binding does
  not change. Several Pay Applications may bind the same Merchant Account.
  It may be disabled or withdrawn; either stops new Payments and Refunds,
  while Payments already started still settle. A disabled or withdrawn
  Merchant Account stops new Payments and Refunds on every Pay Application
  bound to it.
- A Business Order Number identifies at most one Payment of that Pay
  Application, including one that is closed, expired, or failed. Initiating
  again returns that Payment when the amount and Pay Method are the same,
  and is refused when either differs. The subject and expiry stay as first
  recorded. The Payment's own number remains the Channel's merchant order
  number. Channel and Pay Method stay orthogonal.
- The Pay Application carries a payment result address and a refund result
  address. Either may be empty. A **Business Notification** is sent when a
  Payment first becomes paid, or when a Refund first ends refunded or
  failed, whichever path made that so. It is not sent for a failed, closed,
  or expired Payment. Automatic delivery uses the address copied onto the
  Payment or Refund at its creation; an empty address means no notice, and
  filling the address later does not backfill. The body names the Business
  Order Number and the Payment, or the Refund and the Payment. It is not
  signed. The caller confirms by reading that record in this service. There
  is no external read API in this decision.
- Automatic delivery retries on a fixed schedule — 15s, 15s, 30s, 3min,
  30min, 30min, 30min, 60min after the first attempt — nine attempts in
  all, then stops. A 2xx response completes an attempt; the body is not
  read. Each attempt leaves a record. An exhausted notice stays visible in
  the back office. An operator may send it again to the address the Pay
  Application carries now, one attempt per call, and may repeat that while
  it stays exhausted. An empty current address refuses the call. A 2xx ends
  the exhausted state. Manual delivery does not restart the automatic
  schedule. Disable or withdrawal of the Pay Application does not stop an
  exhausted notice from being sent again.
- **Payment Completed** stays the Channel Notification path only, and only
  when that path is what first moves the Payment to paid (ADR 0022). The
  event names the Pay Application. The other paths that settle a Payment as
  paid send a Business Notification and do not publish the event.

Channel Notifications are unchanged (ADR 0020): the channel still calls
`/pay/notify/{channel}/{merchant_id}`, the signature is still the
authentication, and the evidence row is still write-once. Money stays int64
minor units (ADR 0019). Merchant credentials stay encrypted (ADR 0018,
ADR 0021).

Considered and rejected: choosing the Merchant Account at initiation, which
cannot give two callers on one account different result addresses; one Pay
Application owning many Merchant Accounts or one channel-row per Pay Method,
which collapses Channel and Pay Method; an app key and a signed body, which
this service does not need while the caller can read the Payment here; an
external read API; publishing Payment Completed from query, read, expiry
sweep, and close; refusing manual resend of an exhausted notice.

## Consequences

- `CreatePayment` names a Pay Application, not a Merchant Account. The
  initiate path that closes other pending Payments for the same Business
  Order Number is replaced by the return-or-refuse rule above. The code and
  `pay_payment` still implement the old rule until that change lands.
- Uniqueness of (Pay Application, Business Order Number) is a partial unique
  index. The channel order number is still the Payment number, so two Pay
  Applications on one Merchant Account may use the same Business Order
  Number and produce two Payments.
- Business Notification state has to survive a restart: the attempt count
  and the next time are stored, and due automatic attempts are scanned by a
  Scheduled Job (ADR 0023). The first attempt is made after the state change
  is durable, not inside the caller's transaction and not inside the
  Channel Notification transaction. A lost first attempt is picked up by the
  job.
- Payment Completed's JSON gains the Pay Application number as an additive
  field (ADR 0022). Subscribers that ignore unknown fields keep working.
