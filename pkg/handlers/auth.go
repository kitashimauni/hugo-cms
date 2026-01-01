package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"

	"hugo-cms/pkg/config"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

func AuthRequired(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token")
	if token == nil {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		} else {
			c.Redirect(http.StatusFound, "/login")
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
	rand.Read(b)
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

	code := c.Query("code")
	token, err := config.OauthConf.Exchange(context.Background(), code)
	if err != nil {
		c.String(http.StatusInternalServerError, "OAuth Exchange Failed")
		return
	}

	session.Set("access_token", token.AccessToken)
	session.Save()

	c.Redirect(http.StatusFound, "/")
}

func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.Redirect(http.StatusFound, "/login")
}
