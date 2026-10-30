### Internal

The local OIDC provider used by the browser tests now serves a UserInfo
endpoint, and `OIDC_TEST_PROVIDER_EMAIL_CLAIMS=userinfo` makes it behave like
Authelia: the ID token carries no email and UserInfo supplies it. A new browser
test walks a first-time SSO sign-in through that mode and checks that it ends
signed in with no email or auth parameter in any app URL.
