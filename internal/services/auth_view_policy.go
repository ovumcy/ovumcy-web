package services

func ResolveAuthErrorSource(flashAuthError string) string {
	return firstNonEmptyTrimmed(flashAuthError)
}

func ResolveAuthPageEmail(flashEmail string) string {
	return NormalizeAuthEmail(flashEmail)
}
