package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionRoundTripAndExpiry(t *testing.T) {
	manager := New("viewer-password", []byte("01234567890123456789012345678901"), time.Hour, false)
	now := time.Unix(1_700_000_000, 0)
	token, claims, err := manager.NewSession(now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := manager.ParseSession(token, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CSRF != claims.CSRF || parsed.ExpiresAt != claims.ExpiresAt {
		t.Fatalf("claims mismatch: %#v %#v", parsed, claims)
	}
	if _, err := manager.ParseSession(token, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired session was accepted")
	}
}

func TestCookieIsHttpOnly(t *testing.T) {
	manager := New("viewer-password", []byte("01234567890123456789012345678901"), time.Hour, true)
	token, claims, _ := manager.NewSession(time.Now())
	recorder := httptest.NewRecorder()
	manager.SetCookie(recorder, token, claims.ExpiresAt)
	cookie := recorder.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != 3 {
		t.Fatalf("unexpected cookie settings: %#v", cookie)
	}
}

func TestPasswordComparison(t *testing.T) {
	manager := New("viewer-password", []byte("01234567890123456789012345678901"), time.Hour, false)
	if !manager.VerifyPassword("viewer-password") {
		t.Fatal("valid password rejected")
	}
	if manager.VerifyPassword("viewer-passworD") || manager.VerifyPassword("short") {
		t.Fatal("invalid password accepted")
	}
}
