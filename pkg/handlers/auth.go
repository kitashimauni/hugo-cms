package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"hugo-cms/pkg/config"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// GitHubUser represents the user info from GitHub API
type GitHubUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

func AuthRequired(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token")
	if token == nil {
		if strings.HasPrefix(c.Request.URL.Path, "/admin/api/") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		} else {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
		}
		return
	}
	c.Next()
}

func LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", nil)
}

func generateStateOauth() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based state (less secure but functional)
		return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return base64.URLEncoding.EncodeToString(b)
}

func GithubLogin(c *gin.Context) {
	state := generateStateOauth()
	session := sessions.Default(c)
	session.Set("oauth_state", state)
	session.Save()

	url := config.OauthConf.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func AuthCallback(c *gin.Context) {
	session := sessions.Default(c)
	retrievedState := session.Get("oauth_state")
	queryState := c.Query("state")

	if retrievedState != queryState {
		c.String(http.StatusBadRequest, "Invalid OAuth State")
		return
	}

	// Remove state from session
	session.Delete("oauth_state")

	// Use request context for proper cancellation propagation
	ctx := c.Request.Context()

	code := c.Query("code")
	token, err := config.OauthConf.Exchange(ctx, code)
	if err != nil {
		c.String(http.StatusInternalServerError, "OAuth Exchange Failed")
		return
	}

	// Fetch GitHub user info to check authorization
	user, err := fetchGitHubUser(ctx, token.AccessToken)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to fetch user info")
		return
	}

	// Check if user is allowed
	if !config.IsUserAllowed(user.Login) {
		c.HTML(http.StatusForbidden, "login.html", gin.H{
			"Error": fmt.Sprintf("User '%s' is not authorized to access this CMS", user.Login),
		})
		return
	}

	session.Set("access_token", token.AccessToken)
	session.Set("github_user", user.Login)
	session.Save()

	c.Redirect(http.StatusFound, "/admin/")
}

// HTTP client with timeout for external API calls
var httpClient = &http.Client{
	Transport: &http.Transport{
		// 接続（TCP）を確立するまでの制限
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		// TLSの証明書交換にかける制限
		TLSHandshakeTimeout: 10 * time.Second,
		// リクエストを送ってから、最初の1バイト（ヘッダー）が返ってくるまでの制限
		ResponseHeaderTimeout: 10 * time.Second,
	},
}

// fetchGitHubUser fetches the authenticated user's info from GitHub API
func fetchGitHubUser(ctx context.Context, accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// validateGitHubToken checks if the access token is still valid by calling GitHub API
func validateGitHubToken(ctx context.Context, accessToken string) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// TokenValidation middleware periodically validates the GitHub access token
// It caches the validation result to avoid excessive API calls
func TokenValidation(c *gin.Context) {
	session := sessions.Default(c)
	token, ok := session.Get("access_token").(string)
	if !ok || token == "" {
		// No token, let AuthRequired handle it
		c.Next()
		return
	}

	// Check last validation time (validate every 5 minutes)
	lastValidation, _ := session.Get("token_validated_at").(int64)
	now := time.Now().Unix()

	// Validate token every 5 minutes
	if now-lastValidation > 300 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		if !validateGitHubToken(ctx, token) {
			// Token is invalid, clear session
			session.Clear()
			session.Save()

			if strings.HasPrefix(c.Request.URL.Path, "/admin/api/") {
				ErrorUnauthorized(c, "Session expired. Please login again.")
				c.Abort()
				return
			}
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}

		// Update validation timestamp
		session.Set("token_validated_at", now)
		session.Save()
	}

	c.Next()
}

func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.Redirect(http.StatusFound, "/admin/login")
}

// GetCSRFToken returns the CSRF token for the current session
func GetCSRFToken(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("csrf_token")
	if token == nil {
		token = generateCSRFToken()
		session.Set("csrf_token", token)
		session.Save()
	}
	c.JSON(http.StatusOK, gin.H{"csrf_token": token})
}

// CSRFProtection middleware validates CSRF tokens on POST/PUT/DELETE requests
func CSRFProtection(c *gin.Context) {
	// Skip CSRF check for GET and HEAD requests
	if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
		c.Next()
		return
	}

	session := sessions.Default(c)
	sessionToken := session.Get("csrf_token")
	if sessionToken == nil {
		// Generate a new token if none exists (for better UX)
		// Client should retry after getting new token
		ErrorForbidden(c, "CSRF token expired. Please refresh and try again.")
		c.Abort()
		return
	}

	// Check header first, then form
	requestToken := c.GetHeader("X-CSRF-Token")
	if requestToken == "" {
		requestToken = c.PostForm("csrf_token")
	}

	if requestToken == "" {
		ErrorForbidden(c, "CSRF token missing from request")
		c.Abort()
		return
	}

	if requestToken != sessionToken.(string) {
		ErrorForbidden(c, "CSRF token mismatch. Please refresh and try again.")
		c.Abort()
		return
	}

	c.Next()
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based token with random component
		n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
		return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)))
	}
	return base64.URLEncoding.EncodeToString(b)
}
