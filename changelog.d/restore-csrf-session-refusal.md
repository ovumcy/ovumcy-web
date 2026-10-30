### Fixed

A settings restore refused because the page's security token went stale no
longer says "Restore failed. Please try again." while every retry would be
refused the same way. It now shows the refusal message and fetches a fresh
token, so the retry it asks for can succeed. A restore refused because the
session expired now says to sign in again.
