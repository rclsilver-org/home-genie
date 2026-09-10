// Package oidctest provides a minimal OIDC issuer for tests: a discovery
// document, a JWKS, and the ability to sign identity tokens.
//
// It is a package rather than a _test.go file because both the verifier's
// own tests and the API tests need it — the same reason httptest exists.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Issuer is a throwaway identity provider.
type Issuer struct {
	*httptest.Server
	key   *rsa.PrivateKey
	keyID string
}

// Claims are what an identity token carries.
type Claims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Audience          string `json:"aud"`
	Expiry            int64  `json:"exp"`
	IssuedAt          int64  `json:"iat"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Name              string `json:"name,omitempty"`
	Email             string `json:"email,omitempty"`
}

// New starts an issuer that shuts down with the test.
func New(t *testing.T) *Issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &Issuer{key: key, keyID: "test-key"}

	mux := http.NewServeMux()
	issuer.Server = httptest.NewServer(mux)
	t.Cleanup(issuer.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer.URL,
			"authorization_endpoint":                issuer.URL + "/auth",
			"token_endpoint":                        issuer.URL + "/token",
			"jwks_uri":                              issuer.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "kid": issuer.keyID, "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})

	return issuer
}

// Valid returns claims that verify, for the given audience.
func (i *Issuer) Valid(audience, subject, username, name string) Claims {
	now := time.Now()
	return Claims{
		Issuer: i.URL, Subject: subject, Audience: audience,
		Expiry: now.Add(time.Hour).Unix(), IssuedAt: now.Unix(),
		PreferredUsername: username, Name: name,
	}
}

// Sign serialises claims into a signed identity token.
func (i *Issuer) Sign(t *testing.T, claims Claims) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: i.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", i.keyID),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
