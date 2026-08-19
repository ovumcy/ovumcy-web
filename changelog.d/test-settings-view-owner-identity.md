none

Test-only guard: the settings-page view builder's three owner reads — the
persisted settings row, the export summary and the custom symptom catalogue —
are now observed to receive the authenticated owner's id. The collaborator
doubles previously discarded that argument, so replacing all three reads with a
constant owner left the focused service suite green; the doubles now record it
and a single case pins every operand against an owner id that is neither zero
nor the first row's. No production change: the forwarding was already correct.
