package credentials

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

func JWTExpiry(token string) (int64, bool) {
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return 0, false
	}
	var claims struct {
		Exp json.Number `json:"exp"`
	}
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	if err := dec.Decode(&claims); err != nil || claims.Exp == "" {
		return 0, false
	}
	if n, err := claims.Exp.Int64(); err == nil && n > 0 {
		return n, true
	}
	if f, err := claims.Exp.Float64(); err == nil && f > 0 {
		return int64(f), true
	}
	return 0, false
}
