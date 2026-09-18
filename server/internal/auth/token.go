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
//
// A label, when given, is carried in front of the secret: `sonarr:hnp_…`.
// Found in a producer's configuration file six months later, a token that
// says whose it is can be replaced without guessing, and a leaked one can be
// revoked without revoking the other five to find out which.
func NewToken(prefix, label string) (plain, hashed string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("drawing a token: %w", err)
	}

	secret := prefix + base64.RawURLEncoding.EncodeToString(raw)
	// Only the secret is hashed. The label is a note to a human and never
	// authenticates anything, so renaming a producer must not invalidate its
	// token — and a caller sending the wrong label is not thereby let in.
	hashed = HashToken(secret)

	if slug := Slug(label); slug != "" {
		return slug + ":" + secret, hashed, nil
	}
	return secret, hashed, nil
}

// SecretOf returns the part of a token that authenticates: everything after
// the last colon, or the whole of it when there is none.
//
// The colon-less form is what every token issued before labels existed looks
// like, and those are still in producers' configurations. They keep working
// because the whole string is then the secret, exactly as it was stored.
func SecretOf(token string) string {
	if at := strings.LastIndex(token, ":"); at >= 0 {
		return token[at+1:]
	}
	return token
}

// Slug reduces a name to what can sit in front of a token: a colon would end
// the label early and a space would not survive a shell.
func Slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			// One separator, never doubled and never leading.
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
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
