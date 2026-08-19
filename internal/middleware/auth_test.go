package middleware

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func apiKeySet(keys ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

func buildHS256Token(secret []byte, exp int64, issuer string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := map[string]any{"sub": "user1", "iat": time.Now().Unix()}
	if exp != 0 {
		claims["exp"] = exp
	}
	if issuer != "" {
		claims["iss"] = issuer
	}
	payloadBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return header + "." + payload + "." + sig
}

func buildRS256Token(priv *rsa.PrivateKey, exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := map[string]any{"sub": "user1", "iat": time.Now().Unix()}
	if exp != 0 {
		claims["exp"] = exp
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

func publicKeyPEM(pub *rsa.PublicKey) []byte {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func requestWithHeader(key, value string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(key, value)
	return r
}

// --- API Key ---

func TestAPIKey_ValidKey(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("secret-key-1")}
	h := Chain(okHandler(), APIKey(cfg))
	req := requestWithHeader("X-API-Key", "secret-key-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKey_InvalidKey(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("secret-key-1")}
	h := Chain(okHandler(), APIKey(cfg))
	req := requestWithHeader("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAPIKey_MissingKey(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("secret-key-1")}
	h := Chain(okHandler(), APIKey(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing api key") {
		t.Fatalf("expected missing api key, got %q", rec.Body.String())
	}
}

func TestAPIKey_CustomHeader(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("my-key"), Header: "X-Custom-Auth"}
	h := Chain(okHandler(), APIKey(cfg))
	req := requestWithHeader("X-Custom-Auth", "my-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKey_QueryParam(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("query-key"), AllowQuery: true}
	h := Chain(okHandler(), APIKey(cfg))
	req := httptest.NewRequest("GET", "/?api_key=query-key", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKey_QueryParamDisabled(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("query-key"), AllowQuery: false}
	h := Chain(okHandler(), APIKey(cfg))
	req := httptest.NewRequest("GET", "/?api_key=query-key", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAPIKey_MultipleKeys(t *testing.T) {
	cfg := APIKeyConfig{Keys: apiKeySet("key-a", "key-b", "key-c")}
	h := Chain(okHandler(), APIKey(cfg))
	for _, key := range []string{"key-a", "key-b", "key-c"} {
		req := requestWithHeader("X-API-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("key %q: expected 200, got %d", key, rec.Code)
		}
	}
}

// --- JWT HS256 ---

func TestJWT_HS256_Valid(t *testing.T) {
	secret := []byte("super-secret")
	token := buildHS256Token(secret, time.Now().Add(time.Hour).Unix(), "")
	cfg := JWTConfig{Algorithm: "HS256", Secret: secret}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestJWT_HS256_Expired(t *testing.T) {
	secret := []byte("super-secret")
	token := buildHS256Token(secret, time.Now().Add(-time.Hour).Unix(), "")
	cfg := JWTConfig{Algorithm: "HS256", Secret: secret}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("expected expired error, got %q", rec.Body.String())
	}
}

func TestJWT_HS256_WrongSecret(t *testing.T) {
	token := buildHS256Token([]byte("correct"), time.Now().Add(time.Hour).Unix(), "")
	cfg := JWTConfig{Algorithm: "HS256", Secret: []byte("wrong")}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWT_HS256_Issuer_Valid(t *testing.T) {
	secret := []byte("secret")
	token := buildHS256Token(secret, time.Now().Add(time.Hour).Unix(), "my-service")
	cfg := JWTConfig{Algorithm: "HS256", Secret: secret, Issuer: "my-service"}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestJWT_HS256_Issuer_Invalid(t *testing.T) {
	secret := []byte("secret")
	token := buildHS256Token(secret, time.Now().Add(time.Hour).Unix(), "other")
	cfg := JWTConfig{Algorithm: "HS256", Secret: secret, Issuer: "my-service"}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWT_MissingHeader(t *testing.T) {
	cfg := JWTConfig{Algorithm: "HS256", Secret: []byte("secret")}
	h := Chain(okHandler(), JWT(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing token") {
		t.Fatalf("expected missing token, got %q", rec.Body.String())
	}
}

func TestJWT_MalformedToken(t *testing.T) {
	cfg := JWTConfig{Algorithm: "HS256", Secret: []byte("secret")}
	h := Chain(okHandler(), JWT(cfg))
	for _, bad := range []string{"notatoken", "only.two", ""} {
		req := requestWithHeader("Authorization", "Bearer "+bad)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: expected 401, got %d", bad, rec.Code)
		}
	}
}

func TestJWT_AlgNoneAttack(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker","exp":9999999999}`))
	token := header + "." + payload + "."

	cfg := JWTConfig{Algorithm: "HS256", Secret: []byte("secret")}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("alg:none must be rejected, got %d", rec.Code)
	}
}

// --- JWT RS256 ---

func TestJWT_RS256_Valid(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := buildRS256Token(priv, time.Now().Add(time.Hour).Unix())
	cfg := JWTConfig{Algorithm: "RS256", PublicKey: &priv.PublicKey}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestJWT_RS256_WrongKey(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := buildRS256Token(priv, time.Now().Add(time.Hour).Unix())
	cfg := JWTConfig{Algorithm: "RS256", PublicKey: &other.PublicKey}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWT_RS256_Expired(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	token := buildRS256Token(priv, time.Now().Add(-time.Hour).Unix())
	cfg := JWTConfig{Algorithm: "RS256", PublicKey: &priv.PublicKey}
	h := Chain(okHandler(), JWT(cfg))
	req := requestWithHeader("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- ParseRSAPublicKey ---

func TestParseRSAPublicKey_Valid(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemBytes := publicKeyPEM(&priv.PublicKey)
	pub, err := ParseRSAPublicKey(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pub.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("parsed key does not match original")
	}
}

func TestParseRSAPublicKey_Invalid(t *testing.T) {
	_, err := ParseRSAPublicKey([]byte("not a pem"))
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}
