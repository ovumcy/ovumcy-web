none

Tests only: the guard that keeps auth inputs out of the query string now resolves fiber lookups by
declaration and their keys by constant value, so an aliased import, a constant from another package
or a constant expression is judged like a literal. The OIDC callback exemption is held to its own
key parameter, and files that build constraints keep out of the type-checked tree are still swept
by name.
