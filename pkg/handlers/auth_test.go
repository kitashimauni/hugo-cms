package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hugo-cms/pkg/config"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func newSessionTestRouter() *gin.Engine {
	router := gin.New()
	store := cookie.NewStore(
		[]byte(strings.Repeat("a", 64)),
		[]byte(strings.Repeat("b", 32)),
	)
	router.Use(sessions.Sessions("test-session", store))
	return router
}

func sessionCookie(t *testing.T, router *gin.Engine, values map[string]interface{}) *http.Cookie {
	t.Helper()

	router.GET("/seed-session", func(c *gin.Context) {
		session := sessions.Default(c)
		for key, value := range values {
			session.Set(key, value)
		}
		if err := session.Save(); err != nil {
			t.Fatalf("save test session: %v", err)
		}
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/seed-session", nil))
	result := recorder.Result()
	defer result.Body.Close()
	cookies := result.Cookies()
	if len(cookies) == 0 {
		t.Fatal("session seed did not return a cookie")
	}
	return cookies[0]
}

func TestAuthRequiredFailsClosedWithoutAllowlist(t *testing.T) {
	originalUsers := config.AllowedGitHubUsers
	originalAllowAll := config.AllowAllGitHubUsers
	t.Cleanup(func() {
		config.AllowedGitHubUsers = originalUsers
		config.AllowAllGitHubUsers = originalAllowAll
	})
	config.AllowedGitHubUsers = nil
	config.AllowAllGitHubUsers = false

	router := newSessionTestRouter()
	cookie := sessionCookie(t, router, map[string]interface{}{
		"access_token": "token",
		"github_user":  "octocat",
	})
	router.GET("/protected", AuthRequired, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.AddCookie(cookie)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("AuthRequired status = %d, want %d", recorder.Code, http.StatusFound)
	}
}

func TestAuthRequiredAllowsConfiguredUser(t *testing.T) {
	originalUsers := config.AllowedGitHubUsers
	originalAllowAll := config.AllowAllGitHubUsers
	t.Cleanup(func() {
		config.AllowedGitHubUsers = originalUsers
		config.AllowAllGitHubUsers = originalAllowAll
	})
	config.AllowedGitHubUsers = []string{"octocat"}
	config.AllowAllGitHubUsers = false

	router := newSessionTestRouter()
	cookie := sessionCookie(t, router, map[string]interface{}{
		"access_token": "token",
		"github_user":  "OctoCat",
	})
	router.GET("/protected", AuthRequired, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.AddCookie(cookie)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("AuthRequired status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestCSRFProtectionRejectsInvalidSessionTokenType(t *testing.T) {
	router := newSessionTestRouter()
	cookie := sessionCookie(t, router, map[string]interface{}{
		"csrf_token": 42,
	})
	router.POST("/protected", CSRFProtection, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/protected", nil)
	request.Header.Set("X-CSRF-Token", "token")
	request.AddCookie(cookie)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("CSRFProtection status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestGetCSRFTokenReplacesInvalidSessionToken(t *testing.T) {
	router := newSessionTestRouter()
	cookie := sessionCookie(t, router, map[string]interface{}{
		"csrf_token": 42,
	})
	router.GET("/csrf", GetCSRFToken)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/csrf", nil)
	request.AddCookie(cookie)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GetCSRFToken status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if strings.Contains(recorder.Body.String(), `"csrf_token":42`) {
		t.Fatal("GetCSRFToken returned an invalid token from the session")
	}
}

func TestRequestBodyLimit(t *testing.T) {
	router := gin.New()
	router.POST("/limited", RequestBodyLimit(4), func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/limited", strings.NewReader("12345"))
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("RequestBodyLimit status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}
