### Changed

Finishing setup now logs the start date you entered as a real cycle start: a
period day carrying the cycle-start mark, whether or not auto period fill is on.
The calendar, the CSV and JSON exports (`Period = Yes`, an entry in `entries`)
and the observed-cycle count now agree on that day. Accounts set up earlier are
not changed.

Correcting the start date in Settings now moves that logged mark too, with auto
period fill on or off: the old date loses the cycle start (a day left with
nothing else is removed, a day with notes or other entries keeps them) and the
new date becomes a period day carrying it, unless it is still in the future.
Marking another day as the cycle start in the same period now also moves the
stored start when the replaced mark was the stored one and the new day is not
in the future.
