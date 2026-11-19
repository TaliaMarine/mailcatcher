package auth

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CombinedMiddleware creates a Gin middleware that supports both OIDC sessions
// and Bearer token authentication.
//
// Public paths (/docs, /openapi.json) are always allowed through.
// API paths (/api/) accept a valid Bearer token as an alternative to OIDC.
// All other paths (WebSocket, SPA) fall through to OIDC only.
func CombinedMiddleware(oidcMiddleware gin.HandlerFunc, authToken string, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if path == "/docs" || strings.HasPrefix(path, "/docs/") ||
			path == "/openapi.json" || path == "/openapi.yaml" {
			c.Next()
			return
		}

		if strings.HasPrefix(path, "/api/") && authToken != "" {
			if header := c.GetHeader("Authorization"); strings.HasPrefix(header, "Bearer ") {
				token := strings.TrimPrefix(header, "Bearer ")
				if subtle.ConstantTimeCompare([]byte(token), []byte(authToken)) == 1 {
					c.Set("auth_method", "token")
					c.Next()
					return
				}
				logger.Info("token authentication failed", "path", path, "remote_addr", c.ClientIP())
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
		}

		if oidcMiddleware != nil {
			oidcMiddleware(c)
			return
		}

		// No OIDC, no valid token — reject API/WS requests.
		if strings.HasPrefix(path, "/api/") || path == "/ws" {
			logger.Info("unauthenticated request rejected", "path", path, "remote_addr", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		// Non-API paths with no auth configured — SPA is served publicly.
		c.Next()
	}
}
