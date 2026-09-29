none

Internal refactor, no behaviour change on any route. The settings password
re-auth had two implementations: the settings actions drew the settings.reauth
budget through `VerifyReauthPassword`, while turning two-factor off hand-rolled
its own budget check, password compare, failure booking and reset against
totp.disable. Both now go through one verify step, `SettingsService.VerifyReauth`,
parameterized by the budget (`ReauthBudget`), and the password change draws the
same budgeted verify around its own compare. The verify step never clears the
count: the settings actions still reset right after it, and turning two-factor
off still resets only once the disable has been written. Statuses, error keys,
security-log lines and attempt counts are unchanged.
