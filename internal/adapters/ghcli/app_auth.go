package ghcli

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AppTokenSource struct {
	hostname, apiVersion  string
	appID, installationID int64
	key                   *rsa.PrivateKey
	mu                    sync.Mutex
	token                 string
	expires               time.Time
}

func NewAppTokenSource(hostname, apiVersion string, appID, installationID int64, privateKey []byte) (*AppTokenSource, error) {
	block, _ := pem.Decode(privateKey)
	if block == nil {
		return nil, fmt.Errorf("github app private key is not PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, xerr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if xerr != nil {
			return nil, fmt.Errorf("parse GitHub App private key: %w", err)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("GitHub App private key is not RSA")
		}
	}
	return &AppTokenSource{hostname: hostname, apiVersion: apiVersion, appID: appID, installationID: installationID, key: key}, nil
}

func (s *AppTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Until(s.expires) > 5*time.Minute {
		return s.token, nil
	}
	jwt, err := s.jwt()
	if err != nil {
		return "", err
	}
	base := "https://api.github.com"
	if s.hostname != "" && s.hostname != "github.com" {
		base = "https://" + s.hostname + "/api/v3"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", base, s.installationID), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", s.apiVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("create GitHub App installation token: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return "", err
	}
	s.token, s.expires = result.Token, result.ExpiresAt
	return s.token, nil
}

func (s *AppTokenSource) jwt() (string, error) {
	now := time.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": s.appID})
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode(header) + "." + encode(payload)
	hash := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + encode(sig), nil
}
