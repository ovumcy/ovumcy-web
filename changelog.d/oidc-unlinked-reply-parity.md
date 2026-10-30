### Security

An OIDC sign-in from an identity that is not linked to any account now gets
the same reply in full — status, redirect and message — whether or not a
local account holds the email the provider asserts; the two cases share one
error, and the unused "cannot link a new identity" message is gone from all
six languages. The operator's audit line still tells the two apart, but
now through `cause="link_required"` or `cause="account_unavailable"`: the
link-required line's `reason` is `sso sign-in unavailable`, no longer
`sso link confirmation unavailable`, so a log filter on the old reason
should match the new `cause` instead.
