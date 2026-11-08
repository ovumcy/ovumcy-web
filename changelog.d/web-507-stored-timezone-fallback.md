### Fixed

- The dashboard's "today" now follows the timezone saved in your settings when the browser sends no timezone header or cookie (a fresh browser, or the first page after signing out and back in), instead of the server's timezone. A day saved from that page could previously land on the neighbouring date.
