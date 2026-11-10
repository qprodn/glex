# 03: Refund under a Pay Application

**What to build:** A caller can refund part or all of a paid Payment, in several Refunds that together do not exceed what was paid. The Payment stays paid. A stopped Pay Application or Merchant Account refuses a new Refund. The refund result address is copied onto the Refund when it is created.

**Blocked by:** 02 Initiate and read a Payment by Pay Application.

**Status:** ready-for-agent

- [ ] A Refund can return part or all of a paid Payment. Several Refunds may belong to that Payment, and together they do not exceed the amount paid.
- [ ] A Refund leaves the Payment's status unchanged and only increases the refunded amount. Paid stays the Payment's terminal state.
- [ ] The refund result address on the Pay Application is copied onto the Refund at creation.
- [ ] A new Refund is refused when the Pay Application or its Merchant Account is disabled or withdrawn.
