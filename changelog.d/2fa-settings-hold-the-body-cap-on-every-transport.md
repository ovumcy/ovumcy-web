none

Tests only: the 2FA enroll and disable endpoints are swept at and one byte past the request-body
cap over plain and gzip JSON, with the compressed form body and the JSON refusals pinned beside
them. A small test-helper extension lets the requests carry Content-Encoding.
