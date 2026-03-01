package services

func ResolveAuthErrorSource(flashAuthError string, queryError string) string {
	return firstNonEmptyTrimmed(flashAuthError, queryError)
}

func ResolveAuthPageEmail(flashEmail string, queryEmail string) string {
	email := NormalizeAuthEmail(flashEmail)
	if email == "" {
		email = NormalizeAuthEmail(queryEmail)
	}
	return email
}
