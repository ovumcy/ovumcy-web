### Fixed

- **The settings re-auth checks that gate password change, clear-data, and account deletion now spend
  the same bcrypt-shaped work on every early-return path — no local password set, a blank field, a
  mismatched confirmation — as they do on a real wrong-password compare.** Previously those paths
  returned before touching bcrypt at all, which made them measurably faster and let response timing
  tell apart "this account has no local password" from "wrong password" for anyone already holding a
  session.
