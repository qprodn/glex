# glex — Identity & Access

The domain concerned with **who can operate the system and how their identity is
established**. This context covers the back-office user base being ported from
RuoYi-Vue-Plus's `sys_user`, built fresh on Kratos — one module at a time,
starting with the System User.

## Language

**System User** (系统用户):
A back-office operator who logs into the system and works within it; the unit of
identity and accountability being ported from RuoYi's `sys_user`. Two system
users are told apart by the roles assigned to them, not by a tier flag on the
record itself.
_Avoid_: User (ambiguous on its own), Account, Member, Admin.

**Role**:
A named bundle of permissions that can be granted to a System User. Modeling and
scope of roles is decided per-module; not part of the System User MVP.
_Avoid_: Group, Profile, Tier.
