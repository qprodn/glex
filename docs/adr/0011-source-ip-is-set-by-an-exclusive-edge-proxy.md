# Trust source IP only because an exclusive edge proxy rewrites it

The deployed glex service is not directly reachable: every request crosses an
edge proxy that removes any client-supplied forwarding header and writes the
authoritative source IP. The application therefore reads the forwarded source
IP without maintaining its own trusted-proxy list. This is a security boundary,
not a convenience assumption—exposing the application port or allowing the
proxy to preserve an inbound `X-Forwarded-For` value would let attackers evade
the IP component of login throttling and must block deployment.
