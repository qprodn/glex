# 07: Automatic delivery

**What to build:** A Business Notification is delivered to the result address copied when the Payment or Refund was created, after that change is durable and outside the caller's transaction and the Channel Notification transaction. Failed deliveries retry on a fixed schedule up to nine attempts, then stop. Each attempt is recorded. A process death before the first attempt does not lose the notice.

**Blocked by:** 04 Channel Notification records the paid notice and names the Pay Application.

**Status:** done

- [x] Automatic delivery uses the address stored on the notice, which was copied at creation. A later edit of the Pay Application does not redirect a notice already in flight.
- [x] The payment body names the Business Order Number and the Payment. The refund body names the Refund and the Payment. Neither body is signed.
- [x] The first attempt happens only after the Payment or Refund change is durable, outside the caller's transaction and outside the Channel Notification transaction.
- [x] A missed first attempt is picked up later by a Scheduled Job. The job scans notices that are waiting and due, and its period is no coarser than 15 seconds.
- [x] Later attempts wait 15 seconds, 15 seconds, 30 seconds, 3 minutes, 30 minutes, 30 minutes, 30 minutes, then 60 minutes. Nine attempts total.
- [x] HTTP 2xx counts as acceptance and the response body is ignored. Any other outcome counts as failure.
- [x] The automatic attempt count and the next attempt time are stored. Each automatic attempt records whether it was accepted, the HTTP status if any, and the error if any.
- [x] After nine failed automatic attempts the notice is exhausted and automatic delivery stops.
