# Channel notification intake lives in the pay domain module

Accepting a Channel Notification sequences merchant resolution, signature
verification, the write-once evidence row, merchant-and-channel binding,
CAS, the dialect ACK, and a best-effort Payment Completed publish. That
sequence lived in the pay application module, in front of a three-method
Notification module whose implementation only forwarded to the notify-log
repository. Tests of binding and of the event reached past the operation
into an unexported step. Decided 2026-10-31: **the sequence is one
operation on a pay domain module.** The application method forwards that
one call. The HTTP handler stays the only caller.

What keeps the module deep, and what stays put:

- **Outcomes stay those already shipped.** An unknown merchant fails before
  any evidence row. An unverified delivery is recorded and rejected. A
  failed evidence insert does not advance and is not ACKed. A failed
  advance is not ACKed, and the evidence row remains so the channel's
  redelivery can finish the state change. An unmatchable target, or a
  target whose merchant or channel is not the one that verified, does not
  advance and is still ACKed. A failed processed-flag update and a failed
  publish are still ACKed. A redelivery runs the advance again; the CAS
  applies no second state change (ADR 0020).
- **Payment and Refund keep their CAS.** The intake calls them. Query,
  close, and expire-sweep still publish no Payment Completed.
- **The success ACK body is the Channel adapter's wire dialect.** The
  intake asks the resolved gateway for it only on a delivery it accepts.
  `ChannelGateway` gains that one method; it does not learn the pipeline.
- **The domain module does not import `internal/eventbus`.** It publishes
  through a port defined in the pay module; the composition root adapts
  `eventbus.Producer` onto it. Publishing stays best-effort and never fails
  the ACK (ADR 0022).
- **No transaction is added.** ADR 0005 keeps the transaction root in the
  application. This path is not one transaction: the evidence row must
  survive a failed advance (ADR 0020). The application still holds no
  repository. The notify-log repository sits inside the domain module, and
  the public three-method Notification interface goes away.
- **The request and the result live on the domain module.** One pair of
  types, forwarded as-is. Binding and Payment Completed are tested through
  that operation. The HTTP tests keep path parsing, the body cap, signature
  verification, and the ACK bytes on the wire. Payment CAS tests and each
  adapter's `ParseNotification` tests stay where they are.

Considered and rejected: leaving the sequence in the application behind
the thin Notification module. The rules stayed smeared across the seam.
Folding the CAS into the intake was also rejected: `ApplyChannelState` and
`ApplyRefundState` already have other callers. Moving this path's
transaction root into the domain was rejected: one transaction would roll
the evidence row back when the advance fails.
