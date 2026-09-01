// Package auth defines the pluggable request authenticator. Ships noop
// (default, everything allowed) and bearer (Authorization header or
// ?access_token= query). Additional providers slot in behind the interface.
package auth

import (
	"context"
	"errors"
	"net/http"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Principal struct {
	ID     string
	Scopes []string
}

// Anonymous is the principal returned by the no-op authenticator.
var Anonymous = Principal{ID: "anonymous"}

type Authenticator interface {
	Authenticate(*http.Request) (Principal, error)
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(ctxKey{}).(Principal); ok {
		return p
	}
	return Anonymous
}

// Middleware wraps h with a: verify then WithPrincipal in the request context.
// A failed check returns 401 and does not call h.
func Middleware(a Authenticator, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.Authenticate(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}
