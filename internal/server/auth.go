package server

import (
	"fmt"
	"net/http"
)

// BasicAuthMiddleware creates a middleware that requires Basic Authentication
func BasicAuthMiddleware(username, password, realm string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != username || pass != password {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		}
	}
}

// AuthMiddleware creates the authentication middleware (Basic Auth only)
func AuthMiddleware(enableAuth bool, username, password, realm string) func(http.HandlerFunc) http.HandlerFunc {
	if !enableAuth {
		return func(next http.HandlerFunc) http.HandlerFunc {
			return next
		}
	}

	return BasicAuthMiddleware(username, password, realm)
}
