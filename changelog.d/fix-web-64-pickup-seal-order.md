none

Internal ordering fix only: GET /register/welcome now seals the auth session
and the recovery-code reveal before it spends the single-use pickup token,
instead of after. The failure mode it closes is a crypto/codec seal error that
no request can provoke, so there is nothing an operator or a user notices in
normal operation.
