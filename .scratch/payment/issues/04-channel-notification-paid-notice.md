# 04: Channel Notification records the paid notice and names the Pay Application

**What to build:** When a Channel Notification is what first moves a Payment to paid, subscribers receive Payment Completed naming that Pay Application, and the caller gets one Business Notification when the copied payment result address is non-empty. A repeat delivery does neither again. The Channel keeps calling the Merchant Account's existing notify address.

**Blocked by:** 02 Initiate and read a Payment by Pay Application.

**Status:** done

- [x] Payment Completed is published only when this Channel Notification's compare-and-swap is what moves the Payment from pending to paid. The payload names the Pay Application as well as the Payment.
- [x] That same first transition records one Business Notification when the payment result address copied onto the Payment is non-empty. The notice names the Business Order Number and the Payment, and it is unsigned.
- [x] A repeated Channel Notification does not publish Payment Completed again and does not record a second Business Notification.
- [x] A Payment that fails, closes, or expires records no Business Notification.
- [x] No notice is recorded when the copied payment result address is empty. Filling that address on the Pay Application later does not create a notice for that Payment.
- [x] The Channel still calls the Merchant Account's notify address. Signature authentication, the write-once raw notification, and the acknowledgement dialect stay as they are.
