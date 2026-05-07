package server

import (
	"fmt"
	"net/http"
	"strings"

	"chijie/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

// Auth 认证模块
type Auth struct {
	jwtSecret string
}

// NewAuth 创建认证模块
func NewAuth(jwtSecret string) *Auth {
	return &Auth{jwtSecret: jwtSecret}
}

// VerifyRequest 验证 HTTP 请求认证。
func (a *Auth) VerifyRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return a.VerifyAuthorization(r.Header.Get("Authorization"))
}

// VerifyAuthorization 验证 Authorization: Bearer <jwt>。
func (a *Auth) VerifyAuthorization(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	prefix, token, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") {
		return false
	}
	return a.verifyJWT(token)
}

func (a *Auth) verifyJWT(tokenString string) bool {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" || strings.TrimSpace(a.jwtSecret) == "" {
		return false
	}

	token, err := jwt.ParseWithClaims(tokenString, &auth.Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(a.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return false
	}
	claims, ok := token.Claims.(*auth.Claims)
	return ok && (claims.Admin || claims.Proxy)
}
