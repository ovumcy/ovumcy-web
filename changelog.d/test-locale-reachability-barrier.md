none

Test-only barrier: the locale catalogue now fails the suite when it carries a
key no shipped Go file or template can reach. No user-visible change — the
sweep reports zero on this tree, which is why it is enabled without a baseline.
