package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerAuthenticator matches a single static token via either the
// Authorization: Bearer <token> header or an ?access_token= query param
// (needed for browser WebSocket which can't set arbitrary headers).
type BearerAuthenticator struct {
	Token string
}

func NewBearer(token string) *BearerAuthenticator { return &BearerAuthenticator{Token: token} }

func (b *BearerAuthenticator) Authenticate(r *http.Request) (Principal, error) {
	if b.Token == "" {
		return Anonymous, ErrUnauthenticated
	}
	tok := ""
	if h := r.Header.Get("Authorization"); h != "" {
		if v, ok := strings.CutPrefix(h, "Bearer "); ok {
			tok = v
		}
	}
	if tok == "" {
		tok = r.URL.Query().Get("access_token")
	}
	if tok == "" {
		return Anonymous, ErrUnauthenticated
	}
	if subtle.ConstantTimeCompare([]byte(tok), []byte(b.Token)) != 1 {
		return Anonymous, ErrUnauthenticated
	}
	return Principal{ID: "bearer", Scopes: []string{"read", "write"}}, nil
}
