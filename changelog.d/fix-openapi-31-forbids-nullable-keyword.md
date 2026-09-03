### Internal

- **`docs/openapi.yaml` can no longer regain the `nullable` keyword.** The nine `nullable: true`
  occurrences were converted to the OpenAPI 3.1 form (`type: [X, "null"]`) previously, but nothing
  stopped the keyword coming back. A new test scans the spec for it and names the file and line;
  it runs automatically wherever `./scripts/...` already runs in CI.
