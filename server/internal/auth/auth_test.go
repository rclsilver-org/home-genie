package auth

import (
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	encoded, err := HashPassword("the right password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword("the right password", encoded)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("the right password was rejected")
	}

	ok, err = VerifyPassword("the wrong password", encoded)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("a wrong password was accepted")
	}
}

// The salt must be drawn per call, otherwise identical passwords across
// accounts would share a hash and leak that fact.
func TestHashesAreSalted(t *testing.T) {
	first, err := HashPassword("the same password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("the same password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical: the salt is not random")
	}
}

// The parameters are read back from the hash rather than assumed, so that
// raising the constants later does not lock existing accounts out. Altering
// them after the fact must therefore change the outcome.
func TestVerifyHonoursTheStoredParameters(t *testing.T) {
	encoded, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}

	tampered := strings.Replace(encoded, "m=65536", "m=32768", 1)
	if tampered == encoded {
		t.Fatal("the memory parameter is not where the test expects it")
	}

	ok, err := VerifyPassword("secret", tampered)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("a hash whose parameters were altered still verified")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, encoded := range []string{
		"",
		"not a digest at all",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA", // wrong variant
		"$argon2id$v=19$m=65536,t=3,p=2$!!!$aGFzaA",   // unreadable salt
	} {
		if _, err := VerifyPassword("secret", encoded); err == nil {
			t.Errorf("VerifyPassword(%q) should have failed", encoded)
		}
	}
}

func TestNewTokenIsPrefixedUniqueAndHashed(t *testing.T) {
	plain, hashed, err := NewToken(DeviceTokenPrefix)
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if !strings.HasPrefix(plain, DeviceTokenPrefix) {
		t.Errorf("token %q lacks the %q prefix", plain, DeviceTokenPrefix)
	}
	if strings.Contains(hashed, plain) || hashed == plain {
		t.Fatal("the stored value contains the token in clear")
	}
	if got := HashToken(plain); got != hashed {
		t.Fatal("HashToken does not reproduce the stored hash")
	}

	other, _, err := NewToken(DeviceTokenPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if other == plain {
		t.Fatal("two draws produced the same token")
	}
}

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer hnd_abc":  "hnd_abc",
		"bearer hnd_abc":  "hnd_abc", // the scheme is case-insensitive
		"Bearer  hnd_abc": "hnd_abc",
		"Basic hnd_abc":   "",
		"hnd_abc":         "",
		"Bearer":          "",
		"":                "",
	}
	for header, want := range cases {
		if got := BearerToken(header); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}
