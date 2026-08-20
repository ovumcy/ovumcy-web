none

Test-only. The account-erasure completeness test no longer carries a
hand-written list of the five user-scoped tables — a list that mirrored
`DeleteAccountAndRelatedData` and so could only ever agree with it. It now
derives the tables from the live schema through GORM's migrator (every table
carrying a `user_id` column), requires the fixture to have seeded each of them
before the erasure, and asserts each is empty for the deleted account
afterwards. Production behaviour is unchanged: the five tables the code erases
today are exactly the five the derivation finds. What changes is that a later
migration adding a user-scoped table without a matching delete now turns the
test red instead of staying green.
