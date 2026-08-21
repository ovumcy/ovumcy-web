none

Tests only: local-password enrollment now pins the owner its credential write
lands on. The settings case asserts the recorded user id, and a second case
drives the real user repository with two owners so a write scoped to anyone but
the acting account is caught. No product code.
