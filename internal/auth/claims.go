// Package auth 提供网关共享的 JWT 声明结构。
//
// admin 和 server 包都基于同一个 jwt_secret 签发与验证 token，
// 此处统一 Claims 结构避免双向定义偏移。
package auth

import "github.com/golang-jwt/jwt/v5"

// Claims 是网关签发的 JWT payload。
//
// Admin=true 表示后台登录获得的管理员 token，可访问 Admin API；
// Proxy=true 表示通过 /api/auth/proxy-token 生成的代理调用 token，仅用于 /proxy 与 /tunnel；
// 两者互斥（管理员 token 默认也能访问 /proxy /tunnel，因为 server 层只要求其中之一）。
type Claims struct {
	Admin bool `json:"admin"`
	Proxy bool `json:"proxy,omitempty"`
	jwt.RegisteredClaims
}
