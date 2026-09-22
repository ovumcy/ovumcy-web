none

Comment-only fix in the gitleaks allowlist: the history-only e2e TLS fixture
pair was described as a 24-hour certificate; it is a self-signed CN=127.0.0.1
certificate valid until 2036, and its key is public in history.
