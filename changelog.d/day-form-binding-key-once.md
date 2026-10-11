none

The day-form account binding derives its HKDF key once per handler instead of on every render and write; the values produced and accepted are byte-identical, pinned by a golden test.
