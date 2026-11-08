### Fixed

- The request day now follows the timezone saved in your settings when the request carries no timezone header or cookie (a fresh browser, the first page after signing out and back in, or a JSON client that sends neither), instead of the server's timezone. On the dashboard a day saved from that page could land on the neighbouring date; `/api/v1/*` callers that send neither header nor cookie now get "today", the cycle day and the stats computed for the owner's saved zone. A header or cookie still wins.
