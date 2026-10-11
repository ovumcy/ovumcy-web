none

Correction to the unreleased running-period catch-up (web-295 entry), which already describes the
corrected behaviour: the catch-up now counts a period from its real first day when a long spotting tail
separates that day from the one ticked. Its history read stopped periodLength plus the cluster gap
before the ticked day, so a light day after the tail could pass for the period's start and fill days
of a period that had already run its length. The read now reaches further back while the period may
continue before it.
