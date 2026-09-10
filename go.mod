module github.com/rclsilver-org/home-notifications

go 1.25.0

// migrate is held at v4.19.1: v4.20.1 requires Go >= 1.25.11, which nixpkgs
// 25.05 does not carry (its go_1_25 is 1.25.5). Raising it means either a
// downloaded toolchain or a newer channel.
require (
	github.com/golang-migrate/migrate/v4 v4.19.1
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.58.0
)

require (
	github.com/coder/websocket v1.8.15
	github.com/coreos/go-oidc/v3 v3.21.0
	golang.org/x/crypto v0.47.0
	golang.org/x/term v0.39.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
