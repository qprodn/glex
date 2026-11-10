# 02: Initiate and read a Payment by Pay Application

**What to build:** A caller starts a Payment by Pay Application id, Business Order Number, subject, amount, Pay Method, and expiry, and can read that Payment back by Pay Application and Business Order Number. The same collection returns the existing Payment. A different amount or Pay Method is refused, and a finished Payment keeps the Business Order Number. The caller does not choose a Merchant Account.

**Blocked by:** 01 Pay Application lifecycle.

**Status:** ready-for-agent

- [ ] Create Payment takes the Pay Application id, Business Order Number, subject, amount, Pay Method, and expiry. It does not take a Merchant Account id.
- [ ] The Channel is the one on that Pay Application's Merchant Account. The Merchant Account and Channel on the Payment are a snapshot of that account. The Payment's own number is the order number the Channel receives.
- [ ] The payment result address on the Pay Application is copied onto the Payment at creation.
- [ ] A repeated start with the same Pay Application, Business Order Number, amount, and Pay Method returns the existing Payment. The first subject and expiry stay as recorded.
- [ ] When that returned Payment is still pending and has no channel payload, prepay finishes on that same Payment.
- [ ] A repeated start whose amount or Pay Method differs is refused.
- [ ] A closed, expired, or failed Payment keeps occupying its Business Order Number. A new Business Order Number opens a new Payment.
- [ ] Two Pay Applications bound to one Merchant Account can use the same Business Order Number and get two Payments.
- [ ] A non-positive amount is refused.
- [ ] A new Payment is refused when the Pay Application or its Merchant Account is disabled or withdrawn.
- [ ] Reading by Pay Application and Business Order Number returns at most one Payment.
- [ ] The application lock key is the Pay Application plus the Business Order Number. The return-or-refuse rule itself is asserted at the pay domain-service seam.
