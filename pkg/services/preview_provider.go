package services

import (
	"context"
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
)

type PreviewDeploymentStatus string

const (
	PreviewDeploymentQueued   PreviewDeploymentStatus = "queued"
	PreviewDeploymentBuilding PreviewDeploymentStatus = "building"
	PreviewDeploymentReady    PreviewDeploymentStatus = "ready"
	PreviewDeploymentFailed   PreviewDeploymentStatus = "failed"
)

type PreviewDeployment struct {
	ID            string                  `json:"id"`
	Branch        string                  `json:"branch"`
	CommitSHA     string                  `json:"commit_sha"`
	Status        PreviewDeploymentStatus `json:"status"`
	URL           string                  `json:"url,omitempty"`
	FailureReason string                  `json:"failure_reason,omitempty"`
}

// PreviewDeploymentProvider is deliberately independent from HTTP handlers
// and Git. Trigger observes/starts the provider deployment for an already
// pushed branch and commit. Implementations must return the immutable URL for
// that exact commit, never a mutable branch alias.
type PreviewDeploymentProvider interface {
	Trigger(ctx context.Context, branch, commitSHA string) (PreviewDeployment, error)
	Status(ctx context.Context, deploymentID string) (PreviewDeployment, error)
	URL(ctx context.Context, deploymentID string) (string, error)
	Delete(ctx context.Context, deploymentID string) error
	Retry(ctx context.Context, deploymentID string) (PreviewDeployment, error)
}

type PreviewProviderErrorKind string

const (
	PreviewProviderNotConfigured PreviewProviderErrorKind = "not_configured"
	PreviewProviderInvalidInput  PreviewProviderErrorKind = "invalid_input"
	PreviewProviderUnauthorized  PreviewProviderErrorKind = "unauthorized"
	PreviewProviderForbidden     PreviewProviderErrorKind = "forbidden"
	PreviewProviderNotFound      PreviewProviderErrorKind = "not_found"
	PreviewProviderConflict      PreviewProviderErrorKind = "conflict"
	PreviewProviderRateLimited   PreviewProviderErrorKind = "rate_limited"
	PreviewProviderUnavailable   PreviewProviderErrorKind = "unavailable"
	PreviewProviderInvalidReply  PreviewProviderErrorKind = "invalid_response"
)

type PreviewProviderError struct {
	Kind      PreviewProviderErrorKind
	Operation string
	Retryable bool
	Err       error
}

func (err *PreviewProviderError) Error() string {
	if err == nil {
		return "preview provider error"
	}
	if err.Err == nil {
		return fmt.Sprintf("preview provider %s failed (%s)", err.Operation, err.Kind)
	}
	return fmt.Sprintf("preview provider %s failed (%s): %v", err.Operation, err.Kind, err.Err)
}

func (err *PreviewProviderError) Unwrap() error { return err.Err }

func IsPreviewProviderError(err error, kind PreviewProviderErrorKind) bool {
	var providerErr *PreviewProviderError
	return errors.As(err, &providerErr) && providerErr.Kind == kind
}

func NewPreviewDeploymentProvider(runtime config.SiteRuntime) (PreviewDeploymentProvider, error) {
	switch runtime.PreviewDeployment.Provider {
	case "":
		return nil, &PreviewProviderError{Kind: PreviewProviderNotConfigured, Operation: "configure"}
	case "cloudflare_pages":
		return NewCloudflarePagesProvider(runtime.PreviewDeployment.CloudflarePages)
	default:
		return nil, &PreviewProviderError{
			Kind:      PreviewProviderInvalidInput,
			Operation: "configure",
			Err:       fmt.Errorf("unsupported provider %q", runtime.PreviewDeployment.Provider),
		}
	}
}
