# 06: A finished Refund records one notice

**What to build:** The first time a Refund ends refunded or failed, the caller gets one Business Notification naming the Refund and the Payment. That includes a result the Channel returns immediately. An empty refund result address at creation means no notice, including when the address is filled in later.

**Blocked by:** 03 Refund under a Pay Application; 04 Channel Notification records the paid notice and names the Pay Application.

**Status:** ready-for-agent

- [ ] A Refund's first transition to refunded records one Business Notification when the refund result address copied onto the Refund is non-empty.
- [ ] A Refund's first transition to failed records one Business Notification on the same condition.
- [ ] Both outcomes do this on every path, including a synchronous result from the Channel.
- [ ] The notice names the Refund and the Payment, and it is unsigned.
- [ ] No notice is recorded when the copied refund result address is empty. Filling that address on the Pay Application later does not create a notice for that Refund.
- [ ] A second transition does not record a second notice.
