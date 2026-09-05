none

Test-only fix, with one behaviour-preserving change in the tool: assemble's
"nothing to release" is now a sentinel error rather than only a message, and
`TestAssembleCarriesTheRepositoryBacklogVerbatim` recognises it to skip — with
a named reason — when the repository legitimately has no backlog, the state a
release assembly leaves behind and `TestAssembleRefusesAnEmptyRelease` requires
assemble to refuse. The fixture fails, rather than skipping, when the fragment
directory itself cannot be read. The error text is unchanged.
