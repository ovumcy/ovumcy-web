package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func parseCredentials(c fiber.Ctx) (credentialsInput, error) {
	credentials := credentialsInput{}
	if err := c.Bind().Body(&credentials); err != nil {
		return credentialsInput{}, err
	}

	email, password, err := services.NormalizeCredentialsInput(credentials.Email, credentials.Password)
	if err != nil {
		return credentialsInput{}, err
	}
	credentials.Email = email
	credentials.Password = password
	credentials.ConfirmPassword = strings.TrimSpace(credentials.ConfirmPassword)
	credentials.RememberMe = credentials.RememberMe || services.ParseBoolLike(c.FormValue("remember_me"))

	return credentials, nil
}
