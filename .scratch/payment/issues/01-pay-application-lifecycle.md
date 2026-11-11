# 01: Pay Application lifecycle

**What to build:** A back-office operator can create a Pay Application bound to exactly one enabled Merchant Account, tell live Pay Applications apart by a unique display name, change that name and either result address later, and disable or withdraw the Pay Application. The Pay Application's own number and its Merchant Account binding stay fixed. There is no app key and no secret.

**Blocked by:** None (can start immediately).

**Status:** done

- [x] Creating a Pay Application binds it to exactly one Merchant Account that is enabled and not withdrawn.
- [x] Creation is refused when that Merchant Account is disabled or withdrawn.
- [x] Several Pay Applications may bind the same Merchant Account.
- [x] The Pay Application has its own number and a display name. The display name is unique among Pay Applications that are not withdrawn.
- [x] Changing the display name keeps the same number.
- [x] A withdrawn Pay Application's display name can be used by another Pay Application.
- [x] The Pay Application has a payment result address and a refund result address. Either address may be left empty at creation, and either may be changed later.
- [x] The Merchant Account binding cannot be changed after creation.
- [x] An operator can disable a Pay Application, and can withdraw it so it leaves the live set.
- [x] List, create, update, status, and withdraw are separate permissions, in the existing pay permission style.
- [x] Merchant Account credentials, masks, and the Connectivity Test stay where they are.
