package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v4"
)

func genTestKey(t *testing.T) (privPEM []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
}

func writeTempKey(t *testing.T, pemBytes []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.pem")
	if err := os.WriteFile(path, pemBytes, mode); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func TestSignAppJWT(t *testing.T) {
	t.Run("valid PEM produces parseable JWT", func(t *testing.T) {
		pemBytes := genTestKey(t)

		signed, err := SignAppJWT("Iv23liClient", pemBytes)
		if err != nil {
			t.Fatalf("SignAppJWT: %v", err)
		}

		parsed, _, err := jwt.NewParser().ParseUnverified(signed, &jwt.RegisteredClaims{})
		if err != nil {
			t.Fatalf("parse signed JWT: %v", err)
		}
		claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
		if !ok {
			t.Fatalf("claims wrong type")
		}
		if claims.Issuer != "Iv23liClient" {
			t.Errorf("Issuer = %q, want %q", claims.Issuer, "Iv23liClient")
		}
		if parsed.Method.Alg() != "RS256" {
			t.Errorf("alg = %q, want RS256", parsed.Method.Alg())
		}
	})

	t.Run("garbage PEM returns parse error", func(t *testing.T) {
		_, err := SignAppJWT("any", []byte("not a pem"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "parse RSA private key") {
			t.Errorf("error = %q, want contain 'parse RSA private key'", err)
		}
	})
}

func TestLoadPrivateKey(t *testing.T) {
	pemBytes := genTestKey(t)

	t.Run("valid key with 0600 returns bytes", func(t *testing.T) {
		path := writeTempKey(t, pemBytes, 0o600)
		got, err := LoadPrivateKey(path)
		if err != nil {
			t.Fatalf("LoadPrivateKey: %v", err)
		}
		if len(got) == 0 {
			t.Error("got empty bytes")
		}
	})

	t.Run("insecure permissions are rejected", func(t *testing.T) {
		path := writeTempKey(t, pemBytes, 0o644)
		_, err := LoadPrivateKey(path)
		if err == nil {
			t.Fatal("expected error for 0644 perms, got nil")
		}
		if !strings.Contains(err.Error(), "insecure permissions") {
			t.Errorf("error = %q, want contain 'insecure permissions'", err)
		}
	})

	t.Run("non-existent file returns stat error", func(t *testing.T) {
		_, err := LoadPrivateKey("/nope/missing.pem")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "stat private key") {
			t.Errorf("error = %q, want contain 'stat private key'", err)
		}
	})

	t.Run("garbage content is rejected", func(t *testing.T) {
		path := writeTempKey(t, []byte("not a pem at all"), 0o600)
		_, err := LoadPrivateKey(path)
		if err == nil {
			t.Fatal("expected parse error, got nil")
		}
		if !strings.Contains(err.Error(), "parse private key") {
			t.Errorf("error = %q, want contain 'parse private key'", err)
		}
	})
}
