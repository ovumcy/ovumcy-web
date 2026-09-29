package api

import "github.com/gofiber/fiber/v3"

// bindRequestBody decodes the request BODY into out, whichever body transport
// the request declares (JSON, urlencoded form, multipart form). It never reads
// the URL query string: a credential, an auth flag or a security token that
// arrives there is not seen, so a value planted in a link cannot shadow or
// stand in for the one the body carries.
//
// Every auth input is read through it. A read that starts from the request's
// combined lookup (FormValue, Query, Bind().Query, Bind().All) is refused by
// TestAuthFieldsAreNeverReadFromTheQueryString, which derives its sites from
// this package's source.
func bindRequestBody(c fiber.Ctx, out any) error {
	return c.Bind().Body(out)
}
