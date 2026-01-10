package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRespondError(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		code       string
		message    string
	}{
		{
			name:       "Unauthorized error",
			httpStatus: http.StatusUnauthorized,
			code:       ErrCodeUnauthorized,
			message:    "Not authenticated",
		},
		{
			name:       "Bad request error",
			httpStatus: http.StatusBadRequest,
			code:       ErrCodeBadRequest,
			message:    "Invalid input",
		},
		{
			name:       "Internal error",
			httpStatus: http.StatusInternalServerError,
			code:       ErrCodeInternalError,
			message:    "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			RespondError(c, tt.httpStatus, tt.code, tt.message)

			if w.Code != tt.httpStatus {
				t.Errorf("RespondError() status = %d, want %d", w.Code, tt.httpStatus)
			}

			var resp APIError
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if resp.Status != "error" {
				t.Errorf("RespondError() status field = %q, want %q", resp.Status, "error")
			}
			if resp.Code != tt.code {
				t.Errorf("RespondError() code = %q, want %q", resp.Code, tt.code)
			}
			if resp.Message != tt.message {
				t.Errorf("RespondError() message = %q, want %q", resp.Message, tt.message)
			}
		})
	}
}

func TestRespondSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	data := map[string]interface{}{
		"id":   123,
		"name": "test",
	}
	RespondSuccess(c, data)

	if w.Code != http.StatusOK {
		t.Errorf("RespondSuccess() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("RespondSuccess() status = %q, want %q", resp["status"], "ok")
	}
	if resp["data"] == nil {
		t.Error("RespondSuccess() data is nil")
	}
}

func TestRespondOK(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	RespondOK(c, "Operation successful")

	if w.Code != http.StatusOK {
		t.Errorf("RespondOK() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("RespondOK() status = %q, want %q", resp["status"], "ok")
	}
	if resp["log"] != "Operation successful" {
		t.Errorf("RespondOK() log = %q, want %q", resp["log"], "Operation successful")
	}
}

func TestErrorHelpers(t *testing.T) {
	tests := []struct {
		name           string
		helperFunc     func(*gin.Context, string)
		message        string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "ErrorUnauthorized with message",
			helperFunc:     ErrorUnauthorized,
			message:        "Custom unauthorized",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   ErrCodeUnauthorized,
		},
		{
			name:           "ErrorUnauthorized without message",
			helperFunc:     ErrorUnauthorized,
			message:        "",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   ErrCodeUnauthorized,
		},
		{
			name:           "ErrorForbidden",
			helperFunc:     ErrorForbidden,
			message:        "Access denied",
			expectedStatus: http.StatusForbidden,
			expectedCode:   ErrCodeForbidden,
		},
		{
			name:           "ErrorBadRequest",
			helperFunc:     ErrorBadRequest,
			message:        "Invalid input",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   ErrCodeBadRequest,
		},
		{
			name:           "ErrorNotFound",
			helperFunc:     ErrorNotFound,
			message:        "Resource not found",
			expectedStatus: http.StatusNotFound,
			expectedCode:   ErrCodeNotFound,
		},
		{
			name:           "ErrorInternal",
			helperFunc:     ErrorInternal,
			message:        "Server error",
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   ErrCodeInternalError,
		},
		{
			name:           "ErrorConflict",
			helperFunc:     ErrorConflict,
			message:        "Already exists",
			expectedStatus: http.StatusConflict,
			expectedCode:   ErrCodeConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			tt.helperFunc(c, tt.message)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s status = %d, want %d", tt.name, w.Code, tt.expectedStatus)
			}

			var resp APIError
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if resp.Code != tt.expectedCode {
				t.Errorf("%s code = %q, want %q", tt.name, resp.Code, tt.expectedCode)
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	HealthCheck(c)

	if w.Code != http.StatusOK {
		t.Errorf("HealthCheck() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("HealthCheck() status = %q, want %q", resp["status"], "ok")
	}
	if resp["timestamp"] == nil {
		t.Error("HealthCheck() timestamp is nil")
	}
	if resp["uptime"] == nil {
		t.Error("HealthCheck() uptime is nil")
	}
	if resp["version"] == nil {
		t.Error("HealthCheck() version is nil")
	}
}

func TestGetOverallStatus(t *testing.T) {
	if got := getOverallStatus(true); got != "ok" {
		t.Errorf("getOverallStatus(true) = %q, want %q", got, "ok")
	}
	if got := getOverallStatus(false); got != "degraded" {
		t.Errorf("getOverallStatus(false) = %q, want %q", got, "degraded")
	}
}

func TestGetHugoStatusMessage(t *testing.T) {
	if got := getHugoStatusMessage(true); got != "Hugo server is running" {
		t.Errorf("getHugoStatusMessage(true) = %q, want %q", got, "Hugo server is running")
	}
	if got := getHugoStatusMessage(false); got != "Hugo server is not running" {
		t.Errorf("getHugoStatusMessage(false) = %q, want %q", got, "Hugo server is not running")
	}
}

func TestIsDirAccessible(t *testing.T) {
	// Test with existing directory
	if !isDirAccessible(".") {
		t.Error("isDirAccessible(\".\") should return true")
	}

	// Test with non-existing directory
	if isDirAccessible("/nonexistent/path/12345") {
		t.Error("isDirAccessible(\"/nonexistent/path/12345\") should return false")
	}
}

func TestIsGitRepoHealthy(t *testing.T) {
	// Test with non-git directory
	if isGitRepoHealthy("/tmp") {
		t.Error("isGitRepoHealthy(\"/tmp\") should return false for non-git dir")
	}

	// Test with the actual repo (should be true if running from repo root)
	// This test may vary depending on execution context
}

func TestStartTime(t *testing.T) {
	// Verify startTime was initialized
	if startTime.IsZero() {
		t.Error("startTime should not be zero")
	}
	if startTime.After(time.Now()) {
		t.Error("startTime should not be in the future")
	}
}
