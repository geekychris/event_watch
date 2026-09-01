package auth

import "net/http"

type NoopAuthenticator struct{}

func (NoopAuthenticator) Authenticate(*http.Request) (Principal, error) {
	return Anonymous, nil
}
