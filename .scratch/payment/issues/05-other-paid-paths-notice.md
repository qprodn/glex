# 05: Other paid paths record the notice without Payment Completed

**What to build:** A Payment that first becomes paid because someone read it, looked it up, queried it, the expiry sweep reached it, or close found it already paid still produces one Business Notification. Those paths do not publish Payment Completed. Money already in flight keeps settling after the Pay Application or its Merchant Account is stopped.

**Blocked by:** 04 Channel Notification records the paid notice and names the Pay Application.

**Status:** ready-for-agent

- [ ] A read of an expired pending Payment, lookup by Business Order Number, an active query, the expiry sweep, and close each record one Business Notification when that path is what first moves the Payment to paid and the copied payment result address is non-empty.
- [ ] None of those paths publish Payment Completed.
- [ ] Close records that notice only when the Channel says the Payment is already paid.
- [ ] An empty copied address records no notice, and a later fill-in of the Pay Application's payment result address does not create one for that Payment.
- [ ] A Payment already started on a disabled or withdrawn Pay Application, or on a Pay Application whose Merchant Account is disabled or withdrawn, still settles through these paths.
