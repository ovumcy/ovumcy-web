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

A new sweep in `internal/services` fails when a production identifier there
names a role the product does not declare, so the old name cannot return
unnoticed.
