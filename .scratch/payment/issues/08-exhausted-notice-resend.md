# 08: Exhausted notices and manual resend

**What to build:** An operator can see the Business Notifications whose automatic delivery is exhausted, including the last result, and can send one of them again by hand. One call makes one attempt to the Pay Application's current result address of the matching kind. A success drops it from that list. A failure leaves it exhausted so the operator can try again. Stopping the Pay Application does not take this away.

**Blocked by:** 07 Automatic delivery.

**Status:** done

- [x] Exhausted Business Notifications are listed with the last result. Successful attempts stay on record and do not appear in that list.
- [x] Listing exhausted notices and resending one are separate permissions, in the existing pay permission style.
- [x] Manual send is refused for a notice that is still retrying automatically or that has already been accepted.
- [x] Manual send delivers once to the Pay Application's current result address of the same kind: payment result address for a payment notice, refund result address for a refund notice.
- [x] Manual send is refused when that current address is empty.
- [x] HTTP 2xx leaves the notice accepted. Any other outcome leaves it exhausted, so it can be sent again.
- [x] A manual send does not restart the nine automatic attempts.
- [x] Manual send remains available after the Pay Application is disabled or withdrawn.
- [x] Each manual call records one attempt, marked as manual, with the HTTP status if any, the error if any, and whether it was accepted.
