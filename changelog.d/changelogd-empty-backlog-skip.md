none

Test-only fix: `TestAssembleCarriesTheRepositoryBacklogVerbatim` now skips (with a
named reason) when the repository legitimately has no backlog — right after a
release assembly — instead of failing on a state `TestAssembleRefusesAnEmptyRelease`
requires `assemble` to refuse. No production behavior changed.
