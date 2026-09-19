### Security

- **A timezone header no longer costs a zoneinfo load on every request.** The zone named by the
  `X-Ovumcy-Timezone` header or the `ovumcy_tz` cookie is resolved on every request, including
  routes with no rate limit, and each resolution used to read and parse a zoneinfo file. Loaded
  zones and names the tz database does not know are now remembered in a bounded in-process cache,
  so a repeated value costs a map lookup; which names are accepted and the fallback are unchanged.
