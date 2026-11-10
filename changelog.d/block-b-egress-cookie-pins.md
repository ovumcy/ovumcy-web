### Internal

- **Tests: three documented security claims now have a test each.** The logout cookie test also
  checks that signing out clears `ovumcy_oidc_link_confirm`. A test compares the Sealed column of
  the cookie table in `docs/security/cryptography.md` with the cookies the code actually seals.
  Two more tests check that raw day values (notes, temperature, cervical mucus, sexual activity,
  pregnancy test, flow, cycle factors) never reach the `.ics` calendar feed or a webhook payload.
