none

Test-only change with no user-visible effect: the auth page-view builder harness
encoded a query string into its request URL, but `buildLoginPageData`,
`buildRegisterPageData` and `buildForgotPasswordPageData` take no `fiber.Ctx`,
so no assertion standing on that harness could ever observe a query read. The
`query` parameter is gone, and the two cases named for a query fallback are
renamed after the flash-absence contract they actually pin.

The exclusion itself now sits where it can fail: `/login`, `/register` and
`/forgot-password` are each requested through the real app with
`?email=…&error=…`, and the rendered page must carry neither the query email in
its own email field nor a server-error block raised by the query. Both halves
were red-checked on their own against a page handler temporarily taught to read
the query; with the handlers as they ship, the package is green.
