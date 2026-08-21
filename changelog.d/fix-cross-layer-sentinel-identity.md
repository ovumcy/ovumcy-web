none

No user-visible effect today, and that is the whole reason the defect was
invisible: `ErrResetTokenAlreadyConsumed` was declared twice with byte-identical
text — once in the persistence layer, which raises it on a lost
compare-and-swap, and once in the business layer, which is where callers reach
for it. Two `errors.New` values are never `errors.Is`-equal, so the comparison
was false for an error whose message is character-for-character the one being
asked about. Nothing in the shipped request paths made that comparison, so no
flow misbehaved; the first caller to add one would have got a silent miss.

The value now lives once, in the transport-free model layer both sides already
import, and each layer re-exports it under its own name. Neither layer's
dependency set grows. A sweep over the shipped source keeps the class closed for
sentinels added later.
