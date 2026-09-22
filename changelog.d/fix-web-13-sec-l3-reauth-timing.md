### Fixed

- **The settings re-auth checks that gate password change, clear-data, and account deletion no longer
  answer an account with no local password faster than a wrong password.** That branch returned
  before touching bcrypt at all, so response timing told the two apart for anyone already holding a
  session; it now spends a dummy compare at the current hashing cost, and a wrong password whose
  stored hash predates that cost is topped up to match. Because that refusal now costs a real
  bcrypt, it also draws down the re-auth budget like a wrong password, so it cannot be bought
  without limit. The refusals decided by the submission
  itself — a blank field, a mismatched confirmation — deliberately stay free, and are answered
  before the account-state branch so that a blank submission costs the same whichever state the
  account is in: they disclose nothing and draw down no re-auth budget.
