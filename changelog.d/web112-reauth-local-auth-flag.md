### Security

- **Breaking (API shape): a settings password check no longer accepts a password sign-in would
  refuse.** Signing in with a password is refused for an account whose local sign-in is turned off,
  even when a password hash is still stored for it. The settings actions that ask for the current
  password again — clearing data, deleting the account, regenerating the recovery code, turning
  two-factor on or off, and linking or unlinking a single-sign-on identity — checked only that a
  hash existed, so on such an account they accepted a password that could no longer sign in. They
  now answer that account the way they answer one with no local password at all: the same refusal
  a wrong password gets, at the same cost, drawn from the same attempt budget. Changing the password
  is unaffected; it already sends such an account to the single-sign-on step-up. None of the
  application's own sign-up, sign-in or settings flows leave an account in this state; it arises
  only from data changed outside them.
