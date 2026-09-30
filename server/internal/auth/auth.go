package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CookieName = "cpamp_viewer_session"

type Claims struct {
	ID        string `json:"jti"`
	ExpiresAt int64  `json:"exp"`
	CSRF      string `json:"csrf"`
}

type Manager struct {
	password string
	secret   []byte
	ttl      time.Duration
	secure   bool
	mu       sync.RWMutex
	active   map[string]int64
}

func New(password string, secret []byte, ttl time.Duration, secure bool) *Manager {
	return &Manager{password: password, secret: append([]byte(nil), secret...), ttl: ttl, secure: secure, active: make(map[string]int64)}
}

func (m *Manager) VerifyPassword(candidate string) bool {
	if len(candidate) != len(m.password) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(m.password)) == 1
}

func (m *Manager) NewSession(now time.Time) (string, Claims, error) {
	csrfBytes := make([]byte, 24)
	idBytes := make([]byte, 24)
	if _, err := rand.Read(csrfBytes); err != nil {
		return "", Claims{}, err
	}
	if _, err := rand.Read(idBytes); err != nil {
		return "", Claims{}, err
	}
	claims := Claims{
		ID:        base64.RawURLEncoding.EncodeToString(idBytes),
		ExpiresAt: now.Add(m.ttl).Unix(),
		CSRF:      base64.RawURLEncoding.EncodeToString(csrfBytes),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", Claims{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := m.sign(encoded)
	m.mu.Lock()
	for id, expiry := range m.active {
		if expiry <= now.Unix() {
			delete(m.active, id)
		}
	}
	m.active[claims.ID] = claims.ExpiresAt
	m.mu.Unlock()
	return encoded + "." + signature, claims, nil
}

func (m *Manager) ParseSession(token string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, errors.New("invalid session")
	}
	expected := m.sign(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return Claims{}, errors.New("invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, errors.New("invalid session payload")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ID == "" || claims.CSRF == "" {
		return Claims{}, errors.New("invalid session claims")
	}
	if claims.ExpiresAt <= now.Unix() {
		return Claims{}, errors.New("session expired")
	}
	m.mu.RLock()
	expiresAt, active := m.active[claims.ID]
	m.mu.RUnlock()
	if !active || expiresAt != claims.ExpiresAt {
		return Claims{}, errors.New("session revoked")
	}
	return claims, nil
}

func (m *Manager) Revoke(token string) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(m.sign(parts[0]))) {
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return
	}
	var claims Claims
	if json.Unmarshal(payload, &claims) != nil || claims.ID == "" {
		return
	}
	m.mu.Lock()
	delete(m.active, claims.ID)
	m.mu.Unlock()
}

func (m *Manager) ClaimsFromRequest(r *http.Request) (Claims, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return Claims{}, err
	}
	return m.ParseSession(cookie.Value, time.Now())
}

func (m *Manager) SetCookie(w http.ResponseWriter, token string, expiresAt int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(expiresAt, 0),
		MaxAge:   int(m.ttl.Seconds()),
	})
}

func (m *Manager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
