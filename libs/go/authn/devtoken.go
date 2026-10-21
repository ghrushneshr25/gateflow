package authn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims are the minimum Gateflow principal claims (dev adapter).
type Claims struct {
	Subject       string   `json:"sub"`
	PrincipalType string   `json:"principal_type"`
	Scopes        []string `json:"scopes"`
	Issuer        string   `json:"iss"`
	Audience      []string `json:"aud"`
	ExpiresAt     int64    `json:"exp"`
}

type DevSigner struct {
	Secret []byte
	Issuer string
}

func NewDevSigner(secret string) *DevSigner {
	if secret == "" {
		secret = DefaultDevSecret
	}
	return &DevSigner{Secret: []byte(secret), Issuer: "gateflow-dev"}
}

func (s *DevSigner) Sign(c Claims, ttl time.Duration) (string, error) {
	if c.Subject == "" || c.PrincipalType == "" {
		return "", errors.New("subject and principal_type required")
	}
	if c.Issuer == "" {
		c.Issuer = s.Issuer
	}
	if len(c.Audience) == 0 {
		c.Audience = []string{"gateflow"}
	}
	if c.ExpiresAt == 0 {
		c.ExpiresAt = time.Now().Add(ttl).Unix()
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := s.sign(header + "." + body)
	return header + "." + body + "." + sig, nil
}

func (s *DevSigner) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token format")
	}
	expected := s.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return Claims{}, errors.New("invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("payload: %w", err)
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, err
	}
	if c.ExpiresAt > 0 && time.Now().Unix() > c.ExpiresAt {
		return Claims{}, errors.New("token expired")
	}
	return c, nil
}

func (s *DevSigner) sign(data string) string {
	mac := hmac.New(sha256.New, s.Secret)
	_, _ = mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
