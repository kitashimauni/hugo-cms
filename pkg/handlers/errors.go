package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIError represents a standardized error response
type APIError struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes
const (
	ErrCodeUnauthorized   = "UNAUTHORIZED"
	ErrCodeForbidden      = "FORBIDDEN"
	ErrCodeBadRequest     = "BAD_REQUEST"
	ErrCodeNotFound       = "NOT_FOUND"
	ErrCodeInternalError  = "INTERNAL_ERROR"
	ErrCodeConflict       = "CONFLICT"
	ErrCodeInvalidCSRF    = "INVALID_CSRF"
	ErrCodeUserNotAllowed = "USER_NOT_ALLOWED"
)

// RespondError sends a standardized error response
func RespondError(c *gin.Context, httpStatus int, code, message string) {
	c.JSON(httpStatus, APIError{
		Status:  "error",
		Code:    code,
		Message: message,
	})
}

// RespondSuccess sends a standardized success response
func RespondSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"data":   data,
	})
}

// RespondOK sends a simple success response
func RespondOK(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"log":    message,
	})
}

// Common error responses
func ErrorUnauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "Authentication required"
	}
	RespondError(c, http.StatusUnauthorized, ErrCodeUnauthorized, message)
}

func ErrorForbidden(c *gin.Context, message string) {
	if message == "" {
		message = "Access denied"
	}
	RespondError(c, http.StatusForbidden, ErrCodeForbidden, message)
}

func ErrorBadRequest(c *gin.Context, message string) {
	if message == "" {
		message = "Invalid request"
	}
	RespondError(c, http.StatusBadRequest, ErrCodeBadRequest, message)
}

func ErrorNotFound(c *gin.Context, message string) {
	if message == "" {
		message = "Resource not found"
	}
	RespondError(c, http.StatusNotFound, ErrCodeNotFound, message)
}

func ErrorInternal(c *gin.Context, message string) {
	if message == "" {
		message = "Internal server error"
	}
	RespondError(c, http.StatusInternalServerError, ErrCodeInternalError, message)
}

func ErrorConflict(c *gin.Context, message string) {
	if message == "" {
		message = "Resource already exists"
	}
	RespondError(c, http.StatusConflict, ErrCodeConflict, message)
}
