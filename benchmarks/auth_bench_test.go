package benchmarks

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"go-api-gateway/internal/middleware"
)

func buildBenchHS256Token(secret []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := map[string]any{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	payloadBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return header + "." + payload + "." + sig
}

func buildBenchRS256Token(priv *rsa.PrivateKey) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := map[string]any{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	payloadBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + payload
	h := crypto.SHA256.New()
	_, _ = h.Write([]byte(signingInput))
	digest := h.Sum(nil)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest)
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func BenchmarkAPIKey_Valid(b *testing.B) {
	cfg := middleware.APIKeyConfig{
		Keys: map[string]struct{}{"bench-key": {}},
	}
	h := middleware.Chain(baseHandler(), middleware.APIKey(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "bench-key")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkJWT_HS256_Valid(b *testing.B) {
	secret := []byte("bench-secret-key")
	token := buildBenchHS256Token(secret)
	cfg := middleware.JWTConfig{Algorithm: "HS256", Secret: secret}
	h := middleware.Chain(baseHandler(), middleware.JWT(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkJWT_RS256_Valid(b *testing.B) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := buildBenchRS256Token(priv)
	cfg := middleware.JWTConfig{Algorithm: "RS256", PublicKey: &priv.PublicKey}
	h := middleware.Chain(baseHandler(), middleware.JWT(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}
