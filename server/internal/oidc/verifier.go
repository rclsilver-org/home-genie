// Package oidc validates the identity tokens the provider issues.
//
// The application performs the Authorization Code + PKCE exchange itself,
// against a public client, and hands the resulting ID token here. That
// keeps provider passwords out of the app, keeps any client secret out of
// existence — a public client with PKCE needs none — and reduces the
// server's job to verifying a signature.
package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

// ErrDisabled is returned when no issuer is configured, so a deployment
// without an identity provider fails clearly instead of mysteriously.
var ErrDisabled = errors.New("OIDC is not configured")

// Identity is what a valid token tells us about its bearer.
type Identity struct {
	Subject       string
	Username      string
	DisplayName   string
	Email         string
	EmailVerified bool
}

// Verifier checks identity tokens against an issuer.
//
// Discovery is lazy and its result cached: the issuer runs on infrastructure
// that can itself be down, and the whole point of the local break-glass
// account is that the server keeps working when it is. Failing to reach the
// provider must therefore break only new enrolments, never startup.
type Verifier struct {
	issuer   string
	clientID string

	mu       sync.Mutex
	provider *coreoidc.Provider
	// discoveredAt bounds how long a failed discovery is retried, so a
	// provider outage does not turn every login into a slow timeout.
	lastAttempt time.Time
	lastErr     error
}

// retryAfter is how long a failed discovery is remembered.
const retryAfter = 30 * time.Second

// New builds a verifier. An empty issuer disables OIDC.
func New(issuer, clientID string) *Verifier {
	return &Verifier{
		issuer:   strings.TrimRight(issuer, "/"),
		clientID: clientID,
	}
}

// Enabled reports whether OIDC is configured.
func (v *Verifier) Enabled() bool { return v.issuer != "" && v.clientID != "" }

// Verify checks a raw ID token and returns who it belongs to.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	if !v.Enabled() {
		return Identity{}, ErrDisabled
	}

	provider, err := v.discover(ctx)
	if err != nil {
		return Identity{}, err
	}

	token, err := provider.Verifier(&coreoidc.Config{ClientID: v.clientID}).
		Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("invalid token: %w", err)
	}

	var claims struct {
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("unreadable claims: %w", err)
	}

	username := claims.PreferredUsername
	if username == "" {
		// Never fall back to the email as a username: an address can be
		// reassigned inside an organisation, whereas the subject cannot.
		username = token.Subject
	}

	return Identity{
		Subject:       token.Subject,
		Username:      username,
		DisplayName:   claims.Name,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
	}, nil
}

func (v *Verifier) discover(ctx context.Context) (*coreoidc.Provider, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.provider != nil {
		return v.provider, nil
	}
	if v.lastErr != nil && time.Since(v.lastAttempt) < retryAfter {
		return nil, v.lastErr
	}

	v.lastAttempt = time.Now()
	provider, err := coreoidc.NewProvider(ctx, v.issuer)
	if err != nil {
		v.lastErr = fmt.Errorf("reaching the issuer %s: %w", v.issuer, err)
		return nil, v.lastErr
	}

	v.provider, v.lastErr = provider, nil
	return provider, nil
}
