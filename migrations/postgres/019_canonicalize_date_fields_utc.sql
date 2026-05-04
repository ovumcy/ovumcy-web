-- PostgreSQL DATE columns store calendar days without offset metadata,
-- so existing rows are already canonical and there is nothing to
-- rewrite on this storage path. This file is recorded for version
-- parity with the sqlite migration that canonicalizes glebarez TEXT
-- DATE values whose legacy offsets may have reflected the request
-- locale at write time.

SELECT 1;
