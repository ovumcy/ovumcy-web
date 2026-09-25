none

Test-only: pins the "every password_hash writer bumps auth_session_version"
invariant with a declaration-resolved guard (internal/db), and adds a real
concurrent lock-contention case for the login rehash CAS on both drivers. No
user-visible change.
