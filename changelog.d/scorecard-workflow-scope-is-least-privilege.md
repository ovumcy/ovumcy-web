### Security

- **The Scorecard workflow no longer grants `read-all` at workflow scope.** The scope only reaches
  a job that does not inherit from it: `analysis` already declares its own full permission set
  (`security-events: write`, `id-token: write`, `contents: read`, `actions: read`), and a job-level
  `permissions:` block replaces the workflow-level one rather than adding to it. Workflow scope is
  now `{}`.
