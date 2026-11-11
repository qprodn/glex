# 02: Initiate and read a Payment by Pay Application

**What to build:** A caller starts a Payment by Pay Application id, Business Order Number, subject, amount, Pay Method, and expiry, and can read that Payment back by Pay Application and Business Order Number. The same collection returns the existing Payment. A different amount or Pay Method is refused, and a finished Payment keeps the Business Order Number. The caller does not choose a Merchant Account.

**Blocked by:** 01 Pay Application lifecycle.

**Status:** done

- [x] Create Payment takes the Pay Application id, Business Order Number, subject, amount, Pay Method, and expiry. It does not take a Merchant Account id.
- [x] The Channel is the one on that Pay Application's Merchant Account. The Merchant Account and Channel on the Payment are a snapshot of that account. The Payment's own number is the order number the Channel receives.
- [x] The payment result address on the Pay Application is copied onto the Payment at creation.
- [x] A repeated start with the same Pay Application, Business Order Number, amount, and Pay Method returns the existing Payment. The first subject and expiry stay as recorded.
- [x] When that returned Payment is still pending and has no channel payload, prepay finishes on that same Payment.
- [x] A repeated start whose amount or Pay Method differs is refused.
- [x] A closed, expired, or failed Payment keeps occupying its Business Order Number. A new Business Order Number opens a new Payment.
- [x] Two Pay Applications bound to one Merchant Account can use the same Business Order Number and get two Payments.
- [x] A non-positive amount is refused.
- [x] A new Payment is refused when the Pay Application or its Merchant Account is disabled or withdrawn.
- [x] Reading by Pay Application and Business Order Number returns at most one Payment.
- [x] The application lock key is the Pay Application plus the Business Order Number. The return-or-refuse rule itself is asserted at the pay domain-service seam.
