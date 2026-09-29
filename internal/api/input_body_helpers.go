package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// bindRequestBody decodes the request BODY into out, and only for the three
// body transports the API declares for auth inputs: JSON, urlencoded form and
// multipart form. Any other media type is refused with
// fiber.ErrUnprocessableEntity and fills nothing. That includes application/xml
// and text/xml, the CBOR and MsgPack types and a vendor "+json" type: the
// underlying binder decodes all of them, an XML decoder keeps whatever it read
// before a syntax error, and none of them is a transport a client was told it
// may use for a password, a code or a token. A vendor "+json" type is refused
// rather than folded into JSON because the API's own JSON detection does not
// fold it either. The media type is compared without parameters and without
// regard to case, the way the binder dispatches.
//
// It never reads the URL query string: a credential, an auth flag or a security
// token that arrives there is not seen, so a value planted in a link cannot
// shadow or stand in for the one the body carries.
//
// Every auth input is read through it. A read that starts from the request's
// combined lookup (FormValue, Query, Bind().Query, Bind().All) is refused by
// TestAuthFieldsAreNeverReadFromTheQueryString, which derives its sites from
// this package's source.
func bindRequestBody(c fiber.Ctx, out any) error {
	if !hasDeclaredAuthBodyType(c) {
		return fiber.ErrUnprocessableEntity
	}
	return c.Bind().Body(out)
}

// hasDeclaredAuthBodyType reports whether the request declares JSON, a
// urlencoded form or a multipart form body.
func hasDeclaredAuthBodyType(c fiber.Ctx) bool {
	return requestMediaType(c) == fiber.MIMEApplicationJSON || hasFormBody(c)
}

// hasFormBody reports whether the request declares a urlencoded or multipart
// form body. The media type is compared without parameters and without regard
// to case, the way the body binder itself dispatches, so a mixed-case
// declaration that the binder would read as a form is read as one here too.
func hasFormBody(c fiber.Ctx) bool {
	mediaType := requestMediaType(c)
	return mediaType == fiber.MIMEApplicationForm || mediaType == fiber.MIMEMultipartForm
}

// requestMediaType returns the request's declared media type, lower-cased and
// with its parameters removed.
func requestMediaType(c fiber.Ctx) string {
	mediaType, _, _ := strings.Cut(c.Get(fiber.HeaderContentType), ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}
