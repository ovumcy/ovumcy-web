none

Tests only. The published claim that every state-mutating `/api/v1/*` endpoint chains
`handler.OwnerOnly` had no test that could fail: with no second role in the product,
`AuthRequired` answers the same 403 first, so the existing role matrix stayed green with
`OwnerOnly` deleted from a route. A new guard reads the real route table instead of the
response and asserts the middleware is in each such route's chain. The control itself was
already correct — no route was missing `OwnerOnly`, and no product code changed.
