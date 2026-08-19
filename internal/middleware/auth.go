package middleware

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"
)

// --- Sentinel errors ---

var (
	ErrMissingToken    = errors.New("auth: missing token")
	ErrInvalidToken    = errors.New("auth: invalid token")
	ErrExpiredToken    = errors.New("auth: token expired")
	ErrInvalidAPIKey   = errors.New("auth: invalid api key")
	ErrMissingAPIKey   = errors.New("auth: missing api key")
	ErrUnknownAlg      = errors.New("auth: unknown algorithm")
)

// --- API Key auth ---

// APIKeyConfig configures API key authentication.
type APIKeyConfig struct {
	// Keys is the set of valid API keys.
	// Use a map for O(1) lookup; values are ignored (map as set).
	Keys map[string]struct{}

	// Header is the request header that carries the key.
	// Default: "X-API-Key"
	Header string

	// AllowQuery allows the key to be passed as ?api_key=... query param.
	// Checked only if the header is absent.
	AllowQuery bool
}

// APIKey returns a middleware that validates API keys.
// Requests without a valid key receive 401 Unauthorized.
func APIKey(cfg APIKeyConfig) Func {
	header := cfg.Header
	if header == "" {
		header = "X-API-Key"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(header)

			if key == "" && cfg.AllowQuery {
				key = r.URL.Query().Get("api_key")
			}

			if key == "" {
				authError(w, ErrMissingAPIKey, http.StatusUnauthorized)
				return
			}

			if !validAPIKey(cfg.Keys, key) {
				authError(w, ErrInvalidAPIKey, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// validAPIKey reports whether key is in the valid set.
// Uses constant-time comparison to prevent timing attacks.
func validAPIKey(keys map[string]struct{}, key string) bool {
	for valid := range keys {
		if subtle.ConstantTimeCompare([]byte(valid), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

// --- JWT auth ---

// JWTConfig configures JWT authentication.
type JWTConfig struct {
	// Algorithm is the expected signing algorithm: "HS256" or "RS256".
	Algorithm string

	// Secret is the HMAC secret for HS256. Ignored for RS256.
	Secret []byte

	// PublicKey is the RSA public key for RS256. Ignored for HS256.
	PublicKey *rsa.PublicKey

	// Issuer, if non-empty, is validated against the "iss" claim.
	Issuer string

	// SkipExpiry disables expiry validation (useful in tests).
	SkipExpiry bool
}

// jwtHeader is the decoded JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// jwtClaims is the decoded JWT payload.
type jwtClaims struct {
	Sub string `json:"sub"`
	Iss string `json:"iss"`
	Exp int64  `json:"exp"`
	Nbf int64  `json:"nbf"`
	Iat int64  `json:"iat"`
}

// JWT returns a middleware that validates Bearer JWTs.
// Requests without a valid token receive 401 Unauthorized.
func JWT(cfg JWTConfig) Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearer(r)
			if err != nil {
				authError(w, err, http.StatusUnauthorized)
				return
			}

			if err := verifyJWT(token, cfg); err != nil {
				authError(w, err, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractBearer pulls the token from Authorization: Bearer <token>.
func extractBearer(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", ErrMissingToken
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", ErrInvalidToken
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return "", ErrMissingToken
	}
	return token, nil
}

// verifyJWT validates the token's structure, signature, and claims.
func verifyJWT(token string, cfg JWTConfig) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ErrInvalidToken
	}

	// Decode header — but do NOT trust its alg field.
	// We always use the algorithm from our config.
	headerJSON, err := base64url.DecodeString(parts[0])
	if err != nil {
		return ErrInvalidToken
	}
	var hdr jwtHeader
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		return ErrInvalidToken
	}
	// Reject tokens whose header alg doesn't match our config.
	// This blocks the "alg:none" attack and algorithm confusion attacks.
	if !strings.EqualFold(hdr.Alg, cfg.Algorithm) {
		return ErrUnknownAlg
	}

	// Verify signature over "header.payload".
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64url.DecodeString(parts[2])
	if err != nil {
		return ErrInvalidToken
	}

	switch strings.ToUpper(cfg.Algorithm) {
	case "HS256":
		if err := verifyHS256(signingInput, sig, cfg.Secret); err != nil {
			return err
		}
	case "RS256":
		if err := verifyRS256(signingInput, sig, cfg.PublicKey); err != nil {
			return err
		}
	default:
		return ErrUnknownAlg
	}

	// Decode and validate claims.
	payloadJSON, err := base64url.DecodeString(parts[1])
	if err != nil {
		return ErrInvalidToken
	}
	var claims jwtClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return ErrInvalidToken
	}

	now := time.Now().Unix()

	if !cfg.SkipExpiry && claims.Exp > 0 && now > claims.Exp {
		return ErrExpiredToken
	}

	if claims.Nbf > 0 && now < claims.Nbf {
		return ErrInvalidToken
	}

	if cfg.Issuer != "" && claims.Iss != cfg.Issuer {
		return ErrInvalidToken
	}

	return nil
}

// verifyHS256 checks the HMAC-SHA256 signature.
func verifyHS256(input string, sig, secret []byte) error {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(input))
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return ErrInvalidToken
	}
	return nil
}

// verifyRS256 checks the RSA-SHA256 signature.
func verifyRS256(input string, sig []byte, pub *rsa.PublicKey) error {
	if pub == nil {
		return ErrInvalidToken
	}
	h := crypto.SHA256.New()
	_, _ = h.Write([]byte(input))
	digest := h.Sum(nil)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest, sig); err != nil {
		return ErrInvalidToken
	}
	return nil
}

// --- base64url helper ---

// base64url wraps base64.RawURLEncoding for cleaner call sites.
var base64url = base64.RawURLEncoding

// --- ParseRSAPublicKey parses a PEM-encoded RSA public key. ---

// ParseRSAPublicKey parses a PEM-encoded PKIX RSA public key.
// Use this to load keys from config files or environment variables.
func ParseRSAPublicKey(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: failed to decode PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("auth: not an RSA public key")
	}
	return rsaPub, nil
}

// --- shared error helper ---

// authError writes a JSON error response.
func authError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	msg := err.Error()
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}` + "\n"))
}