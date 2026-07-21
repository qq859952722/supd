package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/supdorg/supd/internal/errors"
)

// REQ-I-002: 认证端点

// AuthVerifyRequest POST /api/auth/verify 请求体
type AuthVerifyRequest struct {
	Token string `json:"token"`
}

// AuthVerifyResponse POST /api/auth/verify 响应体
type AuthVerifyResponse struct {
	Valid bool `json:"valid"`
}

// handleAuthVerify POST /api/auth/verify
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	var req AuthVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	if req.Token == "" {
		respondError(w, errors.ErrAuthRequired, "token is required")
		return
	}

	valid := true

	// 如果有authProvider，使用它验证
	if s.authProvider != nil {
		valid = s.authProvider.VerifyToken(req.Token)
	} else if s.config != nil && s.config.Settings.AuthMode != "none" && s.config.Settings.AuthMode != "" {
		// 如果没有provider但有认证配置，直接比较token
		valid = subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.config.Settings.AuthToken)) == 1
	}

	respondJSON(w, http.StatusOK, AuthVerifyResponse{Valid: valid})
}
