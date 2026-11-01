none

Tests only: the detailed-titles disarm guard refuses raw SQL by where the selector column's name is
spelled rather than by tracing where the SQL flows, so SQL passed as a parameter, returned beside an
error, or reached through an interface or a generic call is refused without interprocedural analysis.
