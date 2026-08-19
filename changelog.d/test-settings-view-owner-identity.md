none

Test-only guard: the settings-page view builder's five owner reads — the
persisted settings row, the export summary, the custom symptom catalogue, the
webhook URL projection and the .ics feed status — are now observed to receive
the authenticated owner's id. The collaborator doubles previously discarded that
argument, and the two status builders were not even supplied, so substituting a
constant owner at any one of the five reads left the focused service suite
green; the doubles now record it and a single case pins every operand against a
non-zero owner id that no fixture supplies by default. No production change: the
forwarding was already correct at all five sites.
