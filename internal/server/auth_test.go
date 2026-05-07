package server

import (
	"net/http"
	"testing"
	"time"

	"chijie/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthVerifiesBearerJWT(t *testing.T) {
	secret := "jwt-secret"
	auth := NewAuth(secret)
	token := signedAdminToken(t, secret)

	req, err := http.NewRequest("POST", "/proxy", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	if !auth.VerifyRequest(req) {
		t.Fatalf("expected bearer jwt to pass request auth")
	}
}

func TestAuthVerifiesProxyBearerJWT(t *testing.T) {
	secret := "jwt-secret"
	auth := NewAuth(secret)
	token := signedProxyToken(t, secret)

	if !auth.VerifyAuthorization("Bearer " + token) {
		t.Fatalf("expected proxy bearer jwt to pass")
	}
}

func TestAuthRejectsMissingBearerPrefix(t *testing.T) {
	secret := "jwt-secret"
	auth := NewAuth(secret)
	token := signedProxyToken(t, secret)

	if auth.VerifyAuthorization(token) {
		t.Fatalf("expected raw token without bearer prefix to fail")
	}
}

func signedProxyToken(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &auth.Claims{
		Proxy: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	})
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenString
}

func signedAdminToken(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &auth.Claims{
		Admin: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	})
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenString
}
