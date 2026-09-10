package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// tokenBytes is the entropy behind a bearer token. 32 bytes is well past
// anything brute-forceable, which is what allows the cheap hash below.
const tokenBytes = 32

// Prefixes make a leaked token identifiable at a glance, in a log or a
// producer's configuration file.
const (
	DeviceTokenPrefix  = "hnd_"
	PublishTokenPrefix = "hnp_"
)

// NewToken draws a token and returns it in clear, together with the hash to
// store. The clear value is shown once and never again.
func NewToken(prefix string) (plain, hashed string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("drawing a token: %w", err)
	}

	plain = prefix + base64.RawURLEncoding.EncodeToString(raw)
	return plain, HashToken(plain), nil
}

// HashToken is the one-way function under which tokens are stored.
//
// SHA-256 rather than argon2 on purpose: a token carries 256 bits of entropy
// drawn from a CSPRNG, so there is no dictionary to defend against — and it
// is verified on every single request, including every WebSocket frame that
// reconnects. Using a deliberately slow hash here would be a self-inflicted
// denial of service, not extra security.
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// BearerToken extracts the token from an Authorization header, or "" when
// the header is absent or malformed.
func BearerToken(header string) string {
	const scheme = "Bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(header[len(scheme):])
}
