# Login Challenge stands in front of the Credential

A login attempt spends a Login Challenge before authn checks the Credential.
The challenge is a picture the caller must read, issued by a public endpoint
and spent on the first submission that presents it. Decided 2026-11-02:

- A challenge that is missing, wrong, expired, or already spent is not a
  failed login. It does not move the username or source-IP lock counters, and
  it writes neither a Login Log nor an Oper Log. Treating it as a failed login
  would let an anonymous client lock any username after a handful of bad
  pictures, or fill both audit logs by asking for pictures.
- Issuance is not rate-limited in the application. The picture stays cheap to
  draw. A limiter would be a separate decision.
- The challenge is not bound to the source IP. The id is unguessable; binding
  it to an address drops a person whose egress changed between the picture and
  the submit.
- When the challenge store is unavailable, login fails closed, the same way a
  failure-counter outage already does.

Considered and rejected: folding a bad picture into `INVALID_CREDENTIALS` and
the lock counters; capping issuance at 30 pictures per source IP per minute.
