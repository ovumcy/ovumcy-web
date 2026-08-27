none

An internal rename plus a new test barrier; no behaviour, wire format, schema or
operator-visible surface changes.

The day-read seam between `internal/api` and the day/symptom repositories was
called `ViewerService`, with a `ForViewer` suffix on each of its four methods,
and the dashboard's collaborator interface was `DashboardViewerProvider`. That
named a role the product declares absent: the security invariants state the
product is owner-role-only with no viewer or partner role, and `internal/models`
declares exactly one role constant. The name outlived its subject — release
1.4.0 removed the never-shipped non-owner "viewer" sanitization path and left
the naming behind. Every method reads nothing but the acting owner's id, so the
seam is now `OwnerDayReadService` with `ForOwner` methods, and the dashboard
collaborator is `DashboardDayLogProvider`.

The seam itself is unchanged and deliberately kept: `internal/api` is
transport-only and may never hold a repository handle, so a thin boundary is
what the architecture asks for. The nil-owner refusals in front of each read are
untouched.

`scripts/archcheck`, the whole-tree architecture check CI already runs, gained a
fourth rule: no identifier in any package may be named after a role the product
does not have, so the old name cannot return unnoticed — in the day-read layer
or in any other. The roles it treats as absent are anchored on the role line in
`docs/architecture.md` and on the role constants in `internal/models`, so the
check cannot drift from what the product documents. `CONTRIBUTING.md` and
`docs/architecture.md` describe the new rule alongside the three existing ones.
