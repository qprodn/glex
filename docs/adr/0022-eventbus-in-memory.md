# Eventbus modeled on coze-studio: thin contracts over an in-memory queue, best-effort delivery

glex's async surface is blank — no resident workers, no queues, no pub/sub;
ADR 0020 explicitly left "forwarding outcomes to the business" as a reserved
evolution. Upcoming business development wants modules to react to each
other's facts without the publisher knowing its consumers. We surveyed both
open-source Coze backends at source level (coze-studio's
`backend/infra/eventbus`, coze-loop's `backend/infra/mq`) and decided to
model glex's eventbus on **coze-studio's**. Decided 2026-09-13; revised
2026-09-14, before any code was written: the v1 backend switched from Redis
Streams to a pure in-memory queue — glex is a single-service back office and
v1 carries no new deployment or ops surface for events.

- The contract set is copied verbatim from coze-studio:
  `Producer.Send/BatchSend(ctx, body []byte, ...SendOpt)`,
  `ConsumerService.RegisterConsumer(nameServer, topic, group, handler,
  ...ConsumerOpt)`, `ConsumerHandler.HandleMessage(ctx, *Message) error`,
  `Message{Topic, Group, Body []byte}` — no transport envelope; the JSON
  body is the payload. One deliberate deviation: the contracts live in
  `internal/eventbus` the way `internal/lock` does, because depguard forbids
  domain importing infra; implementations live in
  `internal/infra/eventbus/impl/<backend>`. The factory seam is
  eventbusinfra.ProducerFactory — coze-studio dispatches these by its
  `COZE_MQ_TYPE` config; v1 needs no switch because memory is the only
  backend, so it is selected unconditionally and the config knob appears
  with the second backend.
- The **memory backend**: each registered consumer group gets a bounded
  queue (default 1024) with one dispatch goroutine consuming it serially —
  within-group FIFO ordering comes free. Publishing enqueues to every
  group's queue; a full queue fails `Send` loudly (the publisher gets the
  error and logs a Warn breadcrumb per ADR 0011) rather than blocking the
  publisher or dropping silently.
- Assembly goes through **fx, not coze-studio's package-level
  `SetDefaultSVC` singleton** (its most criticized trait): the infra module
  provides per-topic producers and the consumer service, each subscribing
  module provides its own subscription list into a value group, and one
  registration invoke inside the eventbus module registers them all — the
  same no-service-locator rule crossdomain already follows. This
  introduces the repo's **first resident background goroutines**:
  consumer loops start on an fx StartHook; Stop cancels the dispatch
  context (in-flight handlers see the cancellation and must return, as
  everywhere in the service) and waits for them, while queued events die
  with the process.
- Contract style follows coze-studio's practice: the publisher owns typed,
  json-tagged event structs in its own domain entities; cross-module
  subscribers decode into structs of their own. The JSON shape is the
  contract and no cross-module import ever happens — which is precisely what
  keeps depguard's cross-module ban workable for events. Topics are few per
  module with an **Event Type** discriminating inside one; topic, group, and
  type constants are owned by the publishing module.
- Failure semantics live in the bus, since there is no broker to requeue:
  a handler error is retried in-process 3 times (1s / 5s / 30s backoff).
  The bus is payload-opaque, so it cannot tell a poison body from a
  transient fault — a permanently failing handler (an undecodable body
  included) burns its retry budget before the terminal outcome: exactly
  one `Error` log carrying the full payload, then the event is dropped —
  that log line **is** the dead letter (ADR 0007: the bus is the handling
  site).
- **Delivery is best-effort, stated plainly**: a restart loses every queued
  and in-flight event; within a live process delivery is at-least-once
  (retries can duplicate side effects). Handlers are therefore idempotent by
  convention (CAS, as everywhere else in glex) and tolerate misses. Events
  are catalysts, not records: a subscriber reacts by looking the fact up,
  never by trusting the payload as the truth. Publishing happens from the
  application layer after the state change is durable, never inside `InTx`.
  A lost publish (full queue) is Warn-logged at the publishing site and the
  intake still succeeds — the state change is already durable; a later
  notification asserting the same fact may re-publish, but nothing
  guarantees it, and that is accepted.
- Tracer bullet: pay publishes **Payment Completed** after the PAID advance
  in channel-notification handling; subscribers treat it as a prompt to look
  at the Payment — the Payment's own state stays the single money truth.
  One example subscriber consumes it end to end, including the drop path.

Considered and rejected: watermill (unifies an MQ zoo we do not have —
abstraction tax for a single in-process backend); river/asynq (job-queue
semantics and a new dependency surface, not event semantics); NSQ as an
exact coze-studio replica (a new deployment component for a single-service
back office); Redis Streams (the original choice — broker-backed
redelivery, crash-safe once the stream write returns; revised to in-memory
at design review because no event class yet demands crash-safe delivery,
and it stays the designated upgrade path through the `impl/<backend>`
factory); a PG outbox (transactional publish, nothing lost — revisit
together with Redis Streams the moment an event class must never be lost).

## Consequences

- Subscribers must tolerate both duplicates and misses; money-critical
  flows key off record state (CAS), events only prompt the lookup.
- A stalled consumer group surfaces as `Send` errors at its publishers and
  as delivery-attempt `Error` logs — visible failure, not a silent backlog.
- The repo's first resident goroutines start under fx lifecycle; drain on
  stop covers in-flight handlers only.
- Event JSON evolves additively only: new fields must be optional to old
  subscribers, and json tags are mandatory — coze-studio's tagless event
  structs are the cautionary tale. Conventional leading fields: snowflake
  event id, `occurred_at`, `Type`.
