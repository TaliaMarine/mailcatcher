package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSafeReturnURL(t *testing.T) {
	cases := map[string]string{
		"":                       "/",
		"/mailboxes/3":           "/mailboxes/3",
		"/search?q=a":            "/search?q=a",
		"//evil.example":         "/",
		"https://evil.example":   "/",
		"/\\evil.example":        "/",
		"/ok\r\nSet-Cookie: x=y": "/",
	}
	for in, want := range cases {
		if got := safeReturnURL(in); got != want {
			t.Errorf("safeReturnURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func testAuth() *Auth { return &Auth{signingKey: []byte(strings.Repeat("k", 32))} }

func TestSealOpen(t *testing.T) {
	a := testAuth()
	in := session{User: User{Subject: "s1", Email: "a@b.c"}, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	v, err := a.seal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out session
	if err := a.open(v, &out); err != nil || out.Subject != "s1" {
		t.Fatalf("open = %+v, %v", out, err)
	}

	// Tampered payload and foreign key are rejected.
	if err := a.open("x"+v, &out); err == nil {
		t.Error("tampered value accepted")
	}
	other := &Auth{signingKey: []byte(strings.Repeat("z", 32))}
	if err := other.open(v, &out); err == nil {
		t.Error("value signed with another key accepted")
	}
	if err := a.open("nodot", &out); err == nil {
		t.Error("malformed value accepted")
	}
}

func TestMiddlewareRejectsMissingAndExpiredSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a := testAuth()
	a.logger = nopLogger()

	r := gin.New()
	r.Use(a.Middleware())
	r.GET("/api/x", func(c *gin.Context) {
		u, _ := UserFromContext(c.Request.Context())
		c.String(200, u.Subject)
	})
	r.GET("/page", func(c *gin.Context) { c.String(200, "page") })

	do := func(path, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := do("/api/x", ""); w.Code != 401 {
		t.Errorf("API without session = %d", w.Code)
	}
	w := do("/page", "")
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/auth/login?return_url=%2Fpage") {
		t.Errorf("page without session = %d %s", w.Code, w.Header().Get("Location"))
	}

	good, _ := a.seal(session{User: User{Subject: "u1"}, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if w := do("/api/x", good); w.Code != 200 || w.Body.String() != "u1" {
		t.Errorf("valid session = %d %q", w.Code, w.Body.String())
	}
	expired, _ := a.seal(session{User: User{Subject: "u1"}, ExpiresAt: time.Now().Add(-time.Minute).Unix()})
	if w := do("/api/x", expired); w.Code != 401 {
		t.Errorf("expired session = %d", w.Code)
	}
	noSub, _ := a.seal(session{ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if w := do("/api/x", noSub); w.Code != 401 {
		t.Errorf("session without subject = %d", w.Code)
	}
}

func TestSameOrigin(t *testing.T) {
	req := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://mail.example/api", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !SameOrigin(req(""), "") {
		t.Error("no Origin must pass")
	}
	if !SameOrigin(req("http://mail.example"), "") {
		t.Error("same host must pass")
	}
	if SameOrigin(req("http://evil.example"), "") {
		t.Error("foreign origin must fail")
	}
	if !SameOrigin(req("https://public.example"), "public.example") {
		t.Error("BASE_URL host must pass")
	}
}
