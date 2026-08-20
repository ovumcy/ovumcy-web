none

Test-only change: the current-user identity DTO regression
(`TestGetCurrentUserReturnsMinimalIdentityShape`) now pins the decoded JSON key
set by equality instead of by presence, so a field added to
`GET /api/v1/users/current` fails the suite until it is deliberately
classified. The response itself is unchanged, and the existing sensitive-field
denylist and raw-body markers stay in place as defense in depth.
