none

Tests only: request-level tests whose verdict depends on the server's "today" now send both timezone signals (the `X-Ovumcy-Timezone` header and the `ovumcy_tz` cookie), and the ones that deliberately send fewer are named as fallback tests.
