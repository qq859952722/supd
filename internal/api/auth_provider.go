package api

import (
	"crypto/subtle"
)

// --- AuthProvider 适配器 ---

type ConfigAuthProvider struct {
	AuthToken string
}

func (p *ConfigAuthProvider) VerifyToken(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(p.AuthToken)) == 1
}
