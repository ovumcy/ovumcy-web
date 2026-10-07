### Fixed

The average period length no longer counts a period that is still running. A
period marked today counted as one day long, and a period entered at onboarding
that has not finished counted only up to today, so a single fresh mark could
shorten the average and every projected period on the dashboard, the calendar,
the stats page and the stats API. The running period now joins the average once
it has ended: a day without bleeding is logged after it, or today is past its
start plus the period length set in settings. When it is the only period
recorded, the period length from settings is used, as before.
