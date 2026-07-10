module github.com/ovumcy/ovumcy-web

go 1.26.5

require (
	github.com/coreos/go-oidc/v3 v3.19.0
	github.com/glebarez/sqlite v1.11.0
	github.com/gofiber/fiber/v3 v3.4.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pquerna/otp v1.5.0
	golang.org/x/crypto v0.53.0
	golang.org/x/net v0.56.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sys v0.46.0
	gorm.io/driver/postgres v1.6.0
	gorm.io/gorm v1.31.2
	pgregory.net/rapid v1.3.0
)

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/glebarez/go-sqlite v1.21.2 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/gofiber/schema v1.8.0 // indirect
	github.com/gofiber/utils/v2 v2.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.19.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.72.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.53.0 // indirect
)

// gorm.io/driver/sqlite and github.com/mattn/go-sqlite3 show up in
// `go list -m all` / `go mod graph` (and therefore in go.sum) but never in
// this file: gorm.io/gorm's own go.mod requires them directly (`go mod why -m
// gorm.io/driver/sqlite` traces .../gorm -> gorm.io/driver/sqlite ->
// github.com/mattn/go-sqlite3), so they enter ovumcy-web's module graph as a
// floor even though no package here imports them — this module's own sqlite
// support goes through github.com/glebarez/sqlite instead. `go mod tidy`
// correctly leaves both out of the require blocks above; nothing to clean up.
