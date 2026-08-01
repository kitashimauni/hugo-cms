package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const cloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

var environmentVariableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type cloudflarePagesProvider struct {
	accountID  string
	project    string
	token      string
	baseURL    string
	httpClient *http.Client
}

type cloudflareEnvelope struct {
	Success    bool                   `json:"success"`
	Errors     []cloudflareAPIMessage `json:"errors"`
	Result     json.RawMessage        `json:"result"`
	ResultInfo cloudflareResultInfo   `json:"result_info"`
}

type cloudflareAPIMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareResultInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

type cloudflareDeployment struct {
	ID                string                      `json:"id"`
	ShortID           string                      `json:"short_id"`
	URL               string                      `json:"url"`
	IsSkipped         bool                        `json:"is_skipped"`
	SkipReason        string                      `json:"skip_reason"`
	DeploymentTrigger cloudflareDeploymentTrigger `json:"deployment_trigger"`
	LatestStage       cloudflareDeploymentStage   `json:"latest_stage"`
}

type cloudflareDeploymentTrigger struct {
	Metadata cloudflareDeploymentMetadata `json:"metadata"`
}

type cloudflareDeploymentMetadata struct {
	Branch     string `json:"branch"`
	CommitHash string `json:"commit_hash"`
}

type cloudflareDeploymentStage struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func NewCloudflarePagesProvider(providerConfig config.CloudflarePagesConfig) (PreviewDeploymentProvider, error) {
	return newCloudflarePagesProvider(providerConfig, cloudflareAPIBaseURL, &http.Client{Timeout: 15 * time.Second}, os.LookupEnv)
}

func newCloudflarePagesProvider(providerConfig config.CloudflarePagesConfig, baseURL string, client *http.Client, lookupEnv func(string) (string, bool)) (*cloudflarePagesProvider, error) {
	operation := "configure"
	accountID := strings.TrimSpace(providerConfig.AccountID)
	project := strings.TrimSpace(providerConfig.ProjectName)
	tokenEnv := strings.TrimSpace(providerConfig.APITokenEnv)
	if accountID == "" || strings.ContainsAny(accountID, `/\\`) {
		return nil, providerInputError(operation, "invalid Cloudflare account ID")
	}
	if project == "" || strings.ContainsAny(project, `/\\`) {
		return nil, providerInputError(operation, "invalid Cloudflare Pages project name")
	}
	if !environmentVariableNamePattern.MatchString(tokenEnv) {
		return nil, providerInputError(operation, "invalid Cloudflare API token environment variable name")
	}
	token, ok := lookupEnv(tokenEnv)
	if !ok || strings.TrimSpace(token) == "" {
		return nil, &PreviewProviderError{Kind: PreviewProviderNotConfigured, Operation: operation, Err: fmt.Errorf("Cloudflare API token environment variable is not set")}
	}
	if client == nil {
		return nil, providerInputError(operation, "HTTP client is required")
	}
	parsedBaseURL, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, providerInputError(operation, "invalid Cloudflare API base URL")
	}
	return &cloudflarePagesProvider{
		accountID:  accountID,
		project:    project,
		token:      token,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
	}, nil
}

func (provider *cloudflarePagesProvider) Trigger(ctx context.Context, branch, commitSHA string) (PreviewDeployment, error) {
	if err := validatePreviewBranch(branch); err != nil {
		return PreviewDeployment{}, providerInputError("trigger", err.Error())
	}
	if err := validateCommitSHA(commitSHA); err != nil {
		return PreviewDeployment{}, providerInputError("trigger", err.Error())
	}

	for page := 1; page <= 100; page++ {
		query := url.Values{}
		query.Set("env", "preview")
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")
		var deployments []cloudflareDeployment
		info, err := provider.request(ctx, http.MethodGet, provider.deploymentsPath(), query, nil, &deployments, "trigger")
		if err != nil {
			return PreviewDeployment{}, err
		}
		for _, deployment := range deployments {
			metadata := deployment.DeploymentTrigger.Metadata
			if metadata.Branch == branch && strings.EqualFold(metadata.CommitHash, commitSHA) {
				return provider.previewDeployment(deployment, "trigger")
			}
		}
		if len(deployments) == 0 || info.TotalPages == 0 || page >= info.TotalPages {
			break
		}
	}
	return PreviewDeployment{}, &PreviewProviderError{
		Kind:      PreviewProviderNotFound,
		Operation: "trigger",
		Retryable: true,
		Err:       fmt.Errorf("deployment for pushed commit is not visible yet"),
	}
}

func (provider *cloudflarePagesProvider) Status(ctx context.Context, deploymentID string) (PreviewDeployment, error) {
	if err := validateDeploymentID(deploymentID); err != nil {
		return PreviewDeployment{}, providerInputError("status", err.Error())
	}
	var deployment cloudflareDeployment
	_, err := provider.request(ctx, http.MethodGet, provider.deploymentPath(deploymentID), nil, nil, &deployment, "status")
	if err != nil {
		return PreviewDeployment{}, err
	}
	if deployment.ID != deploymentID {
		return PreviewDeployment{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: "status", Err: fmt.Errorf("Cloudflare returned a different deployment ID")}
	}
	return provider.previewDeployment(deployment, "status")
}

func (provider *cloudflarePagesProvider) URL(ctx context.Context, deploymentID string) (string, error) {
	deployment, err := provider.Status(ctx, deploymentID)
	if err != nil {
		return "", err
	}
	if deployment.Status != PreviewDeploymentReady || deployment.URL == "" {
		return "", &PreviewProviderError{Kind: PreviewProviderConflict, Operation: "url", Retryable: true, Err: fmt.Errorf("deployment is not ready")}
	}
	return deployment.URL, nil
}

func (provider *cloudflarePagesProvider) Delete(ctx context.Context, deploymentID string) error {
	if err := validateDeploymentID(deploymentID); err != nil {
		return providerInputError("delete", err.Error())
	}
	query := url.Values{}
	query.Set("force", "true")
	_, err := provider.request(ctx, http.MethodDelete, provider.deploymentPath(deploymentID), query, nil, nil, "delete")
	return err
}

func (provider *cloudflarePagesProvider) Retry(ctx context.Context, deploymentID string) (PreviewDeployment, error) {
	if err := validateDeploymentID(deploymentID); err != nil {
		return PreviewDeployment{}, providerInputError("retry", err.Error())
	}
	var deployment cloudflareDeployment
	_, err := provider.request(ctx, http.MethodPost, provider.deploymentPath(deploymentID)+"/retry", nil, bytes.NewReader(nil), &deployment, "retry")
	if err != nil {
		return PreviewDeployment{}, err
	}
	return provider.previewDeployment(deployment, "retry")
}

func (provider *cloudflarePagesProvider) deploymentsPath() string {
	return "/accounts/" + url.PathEscape(provider.accountID) + "/pages/projects/" + url.PathEscape(provider.project) + "/deployments"
}

func (provider *cloudflarePagesProvider) deploymentPath(deploymentID string) string {
	return provider.deploymentsPath() + "/" + url.PathEscape(deploymentID)
}

func (provider *cloudflarePagesProvider) request(ctx context.Context, method, path string, query url.Values, body io.Reader, result interface{}, operation string) (cloudflareResultInfo, error) {
	requestURL := provider.baseURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return cloudflareResultInfo{}, &PreviewProviderError{Kind: PreviewProviderInvalidInput, Operation: operation, Err: err}
	}
	request.Header.Set("Authorization", "Bearer "+provider.token)
	request.Header.Set("Accept", "application/json")

	response, err := provider.httpClient.Do(request)
	if err != nil {
		return cloudflareResultInfo{}, &PreviewProviderError{Kind: PreviewProviderUnavailable, Operation: operation, Retryable: true, Err: fmt.Errorf("Cloudflare API request failed")}
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, 4<<20)
	var envelope cloudflareEnvelope
	if err := json.NewDecoder(limited).Decode(&envelope); err != nil {
		return cloudflareResultInfo{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: operation, Retryable: response.StatusCode >= 500, Err: fmt.Errorf("invalid Cloudflare API response")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		return cloudflareResultInfo{}, providerHTTPError(operation, response.StatusCode, envelope.Errors, provider.token)
	}
	if result != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return cloudflareResultInfo{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: operation, Err: fmt.Errorf("invalid Cloudflare deployment payload")}
		}
	}
	return envelope.ResultInfo, nil
}

func previewDeploymentFromCloudflare(deployment cloudflareDeployment) PreviewDeployment {
	status := PreviewDeploymentQueued
	switch deployment.LatestStage.Status {
	case "failure", "canceled":
		status = PreviewDeploymentFailed
	case "success":
		status = PreviewDeploymentReady
	case "active", "idle":
		if deployment.LatestStage.Name == "queued" {
			status = PreviewDeploymentQueued
		} else {
			status = PreviewDeploymentBuilding
		}
	}
	if deployment.IsSkipped {
		status = PreviewDeploymentFailed
	}
	result := PreviewDeployment{
		ID:        deployment.ID,
		Branch:    deployment.DeploymentTrigger.Metadata.Branch,
		CommitSHA: deployment.DeploymentTrigger.Metadata.CommitHash,
		Status:    status,
	}
	if status == PreviewDeploymentReady {
		result.URL = deployment.URL
	}
	if status == PreviewDeploymentFailed {
		result.FailureReason = deployment.SkipReason
		if result.FailureReason == "" {
			result.FailureReason = "Cloudflare Pages deployment failed"
		}
	}
	return result
}

func (provider *cloudflarePagesProvider) previewDeployment(deployment cloudflareDeployment, operation string) (PreviewDeployment, error) {
	result := previewDeploymentFromCloudflare(deployment)
	if result.ID == "" {
		return PreviewDeployment{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: operation, Err: fmt.Errorf("Cloudflare deployment ID is missing")}
	}
	if err := validatePreviewBranch(result.Branch); err != nil {
		return PreviewDeployment{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: operation, Err: fmt.Errorf("Cloudflare deployment branch is invalid")}
	}
	if err := validateCommitSHA(result.CommitSHA); err != nil {
		return PreviewDeployment{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: operation, Err: fmt.Errorf("Cloudflare deployment commit is invalid")}
	}
	if result.Status == PreviewDeploymentReady {
		parsed, err := url.Parse(result.URL)
		expectedHost := strings.ToLower(deployment.ShortID + "." + provider.project + ".pages.dev")
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || !strings.EqualFold(parsed.Hostname(), expectedHost) || deployment.ShortID == "" {
			return PreviewDeployment{}, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: operation, Err: fmt.Errorf("Cloudflare returned a non-immutable deployment URL")}
		}
	}
	return result, nil
}

func providerHTTPError(operation string, statusCode int, messages []cloudflareAPIMessage, token string) error {
	kind := PreviewProviderUnavailable
	retryable := statusCode >= 500
	switch statusCode {
	case http.StatusUnauthorized:
		kind = PreviewProviderUnauthorized
	case http.StatusForbidden:
		kind = PreviewProviderForbidden
	case http.StatusNotFound:
		kind = PreviewProviderNotFound
	case http.StatusConflict:
		kind = PreviewProviderConflict
	case http.StatusTooManyRequests:
		kind = PreviewProviderRateLimited
		retryable = true
	}
	detail := fmt.Sprintf("Cloudflare API returned HTTP %d", statusCode)
	if len(messages) > 0 && messages[0].Code != 0 {
		detail += fmt.Sprintf(" (code %d)", messages[0].Code)
	}
	// Do not include the provider's response message verbatim. It can contain
	// request-derived values; keeping only the numeric API code prevents token
	// or URL leakage through logs and JSON error responses.
	detail = strings.ReplaceAll(detail, token, "***")
	return &PreviewProviderError{Kind: kind, Operation: operation, Retryable: retryable, Err: fmt.Errorf("%s", detail)}
}

func providerInputError(operation, message string) error {
	return &PreviewProviderError{Kind: PreviewProviderInvalidInput, Operation: operation, Err: fmt.Errorf("%s", message)}
}

func validateDeploymentID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid deployment ID")
	}
	return nil
}
