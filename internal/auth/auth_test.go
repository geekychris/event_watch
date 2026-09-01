package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBearer_HeaderOK(t *testing.T) {
	b := NewBearer("s3cr3t")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	p, err := b.Authenticate(req)
	if err != nil || p.ID != "bearer" {
		t.Fatalf("want ok, got p=%+v err=%v", p, err)
	}
}

func TestBearer_QueryOK(t *testing.T) {
	b := NewBearer("s3cr3t")
	req := httptest.NewRequest("GET", "/?access_token=s3cr3t", nil)
	_, err := b.Authenticate(req)
	if err != nil {
		t.Fatalf("want ok, got %v", err)
	}
}

func TestBearer_Reject(t *testing.T) {
	b := NewBearer("s3cr3t")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if _, err := b.Authenticate(req); err == nil {
		t.Fatal("want reject")
	}
}

func TestMiddleware_Blocks401(t *testing.T) {
	b := NewBearer("s3cr3t")
	h := Middleware(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestMiddleware_Passes(t *testing.T) {
	b := NewBearer("s3cr3t")
	h := Middleware(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		_, _ = io.WriteString(w, p.ID)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "bearer") {
		t.Fatalf("want 200/bearer, got %d %q", rec.Code, rec.Body.String())
	}
}
