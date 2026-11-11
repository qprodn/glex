Status: done

## Problem Statement

调用方发起收款时必须点名一个商户账号，业务单号在商户账号下可以有多笔，再次发起会关掉尚未支付的旧单再开一笔新的。两个调用方如果要共用一个商户账号、各自收到自己的结果、各自用自己的业务单号，没有一个身份可以挂这些东西。渠道回调丢失时，支付单可以被查单、读取、过期扫描或关单收成已支付，但进程内的支付完成事件只有渠道回调会发，调用方也没有一份会重试的结果通知。

## Solution

增加支付应用。一个支付应用在创建时绑定恰好一个商户账号，调用方用支付应用的编号发起支付和退款。一个支付应用内，一个业务单号最多一笔支付单。支付单第一次变为已支付，或退款单第一次变为已退款或失败时，向该支付应用的结果地址发业务通知，并按固定间隔重试。支付完成事件仍只在渠道回调第一次收成已支付时发布，并带上支付应用的编号。

## User Stories

1. As a back-office operator, I want to create a Pay Application bound to one enabled Merchant Account, so that a caller can collect into that account.
2. As a back-office operator, I want creation to refuse a disabled or withdrawn Merchant Account, so that a Pay Application cannot be bound to an account that cannot collect.
3. As a back-office operator, I want each Pay Application to bind exactly one Merchant Account, so that a payment does not have to choose an account later.
4. As a back-office operator, I want the Merchant Account binding to stay fixed after creation, so that an existing Pay Application cannot be pointed at a different account.
5. As a back-office operator, I want several Pay Applications to bind the same Merchant Account, so that two callers can collect into one account.
6. As a back-office operator, I want a display name on the Pay Application, so that I can tell callers apart in the back office.
7. As a back-office operator, I want that display name to be unique among Pay Applications that are not withdrawn, so that two live callers do not share a label.
8. As a back-office operator, I want to change the display name, so that a rename does not require a new Pay Application.
9. As a back-office operator, I want the Pay Application's own number to stay the same when I rename it, so that callers keep using the same id.
10. As a back-office operator, I want a withdrawn Pay Application's display name to be usable again, so that a replacement caller can take the old label.
11. As a back-office operator, I want a payment result address and a refund result address on the Pay Application, so that the caller can be told about outcomes.
12. As a back-office operator, I want either address to be left empty, so that I can create a Pay Application before the caller has an address.
13. As a back-office operator, I want to change either address later, so that I can correct a wrong endpoint.
14. As a back-office operator, I want to disable a Pay Application, so that it stops new Payments and Refunds without withdrawing it.
15. As a back-office operator, I want to withdraw a Pay Application, so that it leaves the live set.
16. As a back-office operator, I want a disabled or withdrawn Pay Application to refuse new Payments and Refunds, so that a stopped caller cannot collect.
17. As a back-office operator, I want Payments already started on a disabled or withdrawn Pay Application to keep settling, so that money already in flight is not abandoned.
18. As a back-office operator, I want a disabled or withdrawn Merchant Account to stop new Payments and Refunds on every Pay Application bound to it, so that one account switch covers all of its callers.
19. As a back-office operator, I want Merchant Account credentials, masks, and the Connectivity Test to stay where they are, so that binding a Pay Application does not change how accounts are configured.
20. As a caller, I want to start a Payment by Pay Application id, Business Order Number, amount, and Pay Method, so that I do not choose a Merchant Account myself.
21. As a caller, I want the Channel to be the one on that Merchant Account, so that the Pay Method is chosen per Payment and stays distinct from the Channel.
22. As a caller, I want the Payment's own number to be the order number the Channel receives, so that there is still one number for one Payment.
23. As a caller, I want a repeated start with the same Pay Application, Business Order Number, amount, and Pay Method to return the existing Payment, so that a retry does not open a second one.
24. As a caller, I want that repeat to keep the first subject and expiry, so that a retry cannot silently change what was collected.
25. As a caller, I want a repeated start whose amount or Pay Method differs to be refused, so that one Business Order Number cannot mean two different collections.
26. As a caller, I want a closed, expired, or failed Payment to keep occupying its Business Order Number, so that I must mint a new number to collect again.
27. As a caller, I want a new Business Order Number to open a new Payment, so that a genuine second collection is still possible.
28. As a caller, I want two Pay Applications on one Merchant Account to use the same Business Order Number and get two Payments, so that each caller's numbers are its own.
29. As a caller, I want a returned pending Payment that has no channel payload yet to finish prepay on that same Payment, so that a crash between record and prepay can be retried.
30. As a caller, I want a non-positive amount to be refused, so that a Payment still cannot be created for nothing.
31. As a caller, I want to read a Payment by Pay Application and Business Order Number and get at most one, so that the number is a lookup key inside that Pay Application.
32. As a caller, I want to create a Refund against a paid Payment for part or all of what was paid, so that several Refunds can return the amount without exceeding it.
33. As a caller, I want a Refund to leave the Payment's status unchanged and only increase the refunded amount, so that paid stays the Payment's terminal state.
34. As a caller, I want a new Payment or Refund to be refused when the Pay Application or its Merchant Account is disabled or withdrawn, so that I cannot start work the operator has stopped.
35. As a caller, I want a Business Notification when a Payment first becomes paid, so that I learn the outcome without polling.
36. As a caller, I want that notice whether the Payment was settled by a Channel Notification, a read, an active query, the expiry sweep, or close, so that a lost Channel Notification does not hide a paid Payment.
37. As a caller, I want no Business Notification when a Payment fails, closes, or expires, so that I see those outcomes by reading the Payment.
38. As a caller, I want a Business Notification when a Refund first ends refunded or failed, including a result the Channel returns immediately, so that every finished Refund is announced once.
39. As a caller, I want the payment notice to name the Business Order Number and the Payment, so that I can find the row.
40. As a caller, I want the refund notice to name the Refund and the Payment, so that I can find both rows.
41. As a caller, I want the notice to be unsigned, so that I do not treat the body as proof.
42. As a caller, I want to confirm a notice by reading the Payment or Refund inside this service, so that the row stays the money truth.
43. As a caller, I want no automatic notice when the result address was empty at creation, so that an unfinished Pay Application does not call anyone.
44. As a caller, I want a later fill-in of that address not to send a notice for a record created while it was empty, so that old rows do not suddenly call out.
45. As a caller, I want automatic delivery to use the address copied at creation, so that a later edit of the Pay Application does not redirect a notice already in flight.
46. As a caller, I want a failed automatic delivery to be retried on a fixed schedule up to nine attempts, so that a short outage does not drop the notice.
47. As a caller, I want an HTTP 2xx to count as acceptance and any other outcome to count as failure, so that the response body is not a second protocol.
48. As a caller, I want the first attempt to happen only after the Payment or Refund change is durable, and outside my transaction and outside the Channel Notification transaction, so that a notice cannot roll back the money state or block the Channel's acknowledgement.
49. As a caller, I want a missed first attempt to be picked up later, so that a process restart does not lose the notice.
50. As a caller, I want an exhausted notice to stop automatic delivery, so that a dead address is not called forever.
51. As a back-office operator, I want each delivery attempt recorded, so that I can see what was sent and how it ended.
52. As a back-office operator, I want exhausted Business Notifications listed with the last result, so that I can see what stopped.
53. As a back-office operator, I want successful attempts kept in the record but not in that exhausted list, so that the list is the work queue.
54. As a back-office operator, I want to manually send an exhausted notice again, so that I can reach the caller after I fix the address or the caller comes back.
55. As a back-office operator, I want that manual send to use the Pay Application's current result address of the matching kind, so that a corrected address is what gets called.
56. As a back-office operator, I want a manual send to be refused when that current address is empty, so that I cannot fire a notice at nowhere.
57. As a back-office operator, I want manual send to be refused for a notice that is still retrying automatically or that has already been accepted, so that I only act on exhausted failures.
58. As a back-office operator, I want each manual call to make one attempt, so that one click is one delivery.
59. As a back-office operator, I want a manual 2xx to leave the notice accepted, so that the queue drops it.
60. As a back-office operator, I want a failed manual send to leave the notice exhausted, so that I can call the resend again.
61. As a back-office operator, I want manual sends not to restart the nine automatic attempts, so that the automatic schedule stays finished.
62. As a back-office operator, I want manual send to remain available after the Pay Application is disabled or withdrawn, so that an in-flight notice can still be delivered.
63. As an in-process subscriber, I want Payment Completed only when a Channel Notification is what first moves a Payment to paid, so that I do not treat query, read, expiry sweep, or close as a second publisher.
64. As an in-process subscriber, I want that event to name the Pay Application as well as the Payment, so that two Pay Applications on one Merchant Account are distinguishable without another read of the event payload alone.
65. As an in-process subscriber, I want a repeated Channel Notification not to publish Payment Completed again and not to open a second Business Notification, so that redelivery stays idempotent.
66. As an in-process subscriber, I want absence of Payment Completed not to mean the Payment is unpaid, so that I still read the Payment before moving money.
67. As the Channel, I want to keep calling the existing notify address for the Merchant Account, so that several Pay Applications on that account do not change my callback URL.
68. As the Channel, I want my signature to remain the authentication and my raw notification to remain a write-once record, so that Business Notifications do not replace Channel Notifications.

## Implementation Decisions

- Authority is ADR 0030. ADR 0018, ADR 0019, ADR 0020, ADR 0021, and ADR 0022 stay in force. ADR 0022 is amended only to keep the publish scope and to add the Pay Application number on Payment Completed.
- A Pay Application is a new record: its own number, a display name, one Merchant Account id fixed at creation, enabled or disabled, a payment result address, a refund result address, and the usual audit and withdrawal columns. The display name is unique among rows that are not withdrawn. Addresses may be empty strings. There is no app key and no secret.
- A Payment gains the Pay Application id and the payment result address copied at creation. Its Merchant Account and Channel remain a snapshot of that Pay Application's Merchant Account. Its own number remains the Channel merchant order number. The Business Order Number is unique per Pay Application among rows that are not withdrawn, including terminal Payments. Uniqueness across Merchant Accounts is not required.
- A Refund gains the refund result address copied at creation. Several Refunds may still belong to one paid Payment, together not exceeding the paid amount. A Refund does not change the Payment status.
- A Business Notification is a separate record from a Channel Notification. One notice per Payment for the paid transition, and one notice per Refund for its first terminal outcome. It stores the copied address used for automatic delivery, the Business Order Number, the Payment, the Refund when relevant, the status (waiting, accepted, exhausted), the automatic attempt count, and the next attempt time. Each automatic or manual attempt is its own record: whether it was manual, the HTTP status if any, the error if any, and whether it was accepted.
- No notice row is created when the copied address is empty.
- Create Payment takes the Pay Application id, Business Order Number, subject, amount, Pay Method, and expiry. It does not take a Merchant Account id. The lock is per Pay Application and Business Order Number. Same amount and Pay Method returns the existing Payment and, if it is still pending and has no channel payload, runs prepay on that Payment. A different amount or Pay Method is refused. Subject and expiry are not updated.
- Create Refund and Create Payment are refused when the Pay Application or its Merchant Account is disabled or withdrawn.
- The first transition of a Payment to paid creates the Business Notification when the copied address is non-empty. That includes Channel Notification intake, a read of an expired pending Payment, lookup by Business Order Number, active query, the expiry sweep, and close when the Channel says the Payment is already paid. Payment failure, close, and expiry do not.
- Payment Completed is published only from Channel Notification intake, and only when that intake's compare-and-swap is what moves the Payment from pending to paid. The payload gains the Pay Application number as an additive field. The other paid paths do not publish it. A repeat Channel Notification does not publish again and does not create a second notice.
- A Refund's first transition to refunded or to failed creates its Business Notification on every path, including a synchronous Channel result.
- The payment body names the Business Order Number and the Payment. The refund body names the Refund and the Payment. Neither body is signed. Neither body is proof.
- Automatic delivery makes the first attempt after the state change is durable, outside the caller's transaction and outside the Channel Notification transaction. Later attempts wait 15 seconds, 15 seconds, 30 seconds, 3 minutes, 30 minutes, 30 minutes, 30 minutes, then 60 minutes. Nine attempts total. HTTP 2xx accepts the attempt and the response body is ignored. Anything else fails the attempt. The attempt count and next time are stored. A Scheduled Job scans notices that are waiting and due; its period is no coarser than 15 seconds. A process death before the first attempt leaves a due row for that job.
- Manual resend is a back-office operation on an exhausted notice. It delivers once to the Pay Application's current address of the same kind. An empty current address is refused. Notices that are waiting or already accepted are refused. HTTP 2xx moves the notice to accepted. Failure leaves it exhausted. The automatic schedule is not restarted. Disable or withdrawal of the Pay Application does not forbid this resend.
- Channel Notification intake, its path, its signature authentication, its write-once evidence, and its acknowledgement dialect stay as they are. The Channel still calls the Merchant Account's notify address.
- Back-office permissions follow the existing pay permission style: separate actions for Pay Application list, create, update, status, and withdraw, and for listing exhausted Business Notifications and resending one.
- The in-process create and read contracts take a Pay Application id. Lookup by Business Order Number is scoped to one Pay Application and returns at most one Payment.
- The current initiate behavior that closes other pending Payments for the same Business Order Number and inserts a new one is replaced by the return-or-refuse rule.

## Testing Decisions

- Test external behavior of the domain operations. Assert the records, the refusal, the event, and the notice. Do not assert SQL, HTTP framework wiring, or adapter request bytes.
- Use the existing pay domain-service seam: in-memory repositories and a scripted Channel, the same style as today's initiation, settlement, Merchant Account, and Channel Notification intake tests. Four call surfaces:
  1. Pay Application create, rename, address edit, bind refusal, disable, and withdraw.
  2. Payment initiation's return-or-refuse rule, and the read, query, expiry sweep, and close paths that settle paid with a Business Notification and without Payment Completed.
  3. Channel Notification intake: the first paid transition publishes Payment Completed naming the Pay Application and records one Business Notification; a repeat does neither again.
  4. Business Notification delivery: nine automatic attempts, HTTP 2xx acceptance, exhaustion, and manual resend against the current address.
- Do not add an HTTP-handler seam or a database seam for these rules. Existing Channel adapter tests and the existing Channel Notification HTTP tests stay, and this work does not change their contract.
- The application lock is covered only as the key moving to Pay Application plus Business Order Number. The return-or-refuse behavior itself is asserted at the domain seam, not again above it.

## Out of Scope

- A separate channel attempt record, and encoding Pay Method into the Channel code.
- Wallet, transfer, profit sharing, reconciliation, and refund approval.
- An app key, a signed Business Notification, and a read API for a caller outside this service.
- Changing Merchant Account encryption, money as int64 minor units, or the Channel Notification evidence log.
- Implementing Stripe or UnionPay.
- Publishing Payment Completed from read, query, expiry sweep, or close.
- Restarting the nine automatic attempts from a manual resend.

## Further Notes

- ADR 0030 is the decision. The design note next to this spec is the table and API sketch. The running code still closes pending Payments and inserts a new one; that path is part of this work, not a preserved behavior.
- A caller outside this service can receive the HTTP body and still cannot confirm it here. That is accepted until an open read API exists, which this spec does not add.
