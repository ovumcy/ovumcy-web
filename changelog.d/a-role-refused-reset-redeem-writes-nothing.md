none

Tests only: a password-reset redeem for an account whose role cannot hold a web
session is refused as an invalid reset token before the password and recovery
code are rewritten, so the account never ends up with a freshly rotated recovery
code that nobody was shown. That ordering held, but no regression named it:
without the role check inside reset-token resolution, the redeem would rotate
both secrets before the session refusal that follows. A regression now proves
the refusal on the stored row and anchors it with the same token succeeding
once the account is an owner again.
