package auth

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// SameOrigin reports whether the request's Origin header, if any, names this
// server. Requests without an Origin header (non-browser clients) pass.
// allowedHost, when set (the host of BASE_URL), is accepted in addition to the
// request's own Host.
func SameOrigin(r *http.Request, allowedHost string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) || (allowedHost != "" && strings.EqualFold(u.Host, allowedHost))
}

// CSRFMiddleware rejects state-changing requests that a browser sends from
// another origin. This complements SameSite=Lax session cookies.
func CSRFMiddleware(allowedHost string) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !SameOrigin(c.Request, allowedHost) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cross-origin request rejected"})
				return
			}
		}
		c.Next()
	}
}
