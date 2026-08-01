package services

import (
	"context"
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testCommitSHA = "0123456789abcdef0123456789abcdef01234567"

func TestCloudflarePagesProviderLifecycleUsesExactCommit(t *testing.T) {
	token := "provider-secret-token"
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/accounts/account-id/pages/projects/docs/deployments":
			fmt.Fprintf(writer, `{"success":true,"result":[
                  {"id":"wrong","short_id":"wrong","url":"https://wrong.docs.pages.dev","deployment_trigger":{"metadata":{"branch":"cms-preview/draft-1","commit_hash":"ffffffffffffffffffffffffffffffffffffffff"}},"latest_stage":{"name":"deploy","status":"success"}},
                  {"id":"deployment-1","url":"https://immutable.docs.pages.dev","deployment_trigger":{"metadata":{"branch":"cms-preview/draft-1","commit_hash":"%s"}},"latest_stage":{"name":"build","status":"active"}}
                ],"result_info":{"page":1,"total_pages":1}}`, testCommitSHA)
		case request.Method == http.MethodGet && request.URL.Path == "/accounts/account-id/pages/projects/docs/deployments/deployment-1":
			fmt.Fprintf(writer, `{"success":true,"result":{"id":"deployment-1","short_id":"abc12345","url":"https://abc12345.docs.pages.dev","deployment_trigger":{"metadata":{"branch":"cms-preview/draft-1","commit_hash":"%s"}},"latest_stage":{"name":"deploy","status":"success"}}}`, testCommitSHA)
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/deployment-1/retry"):
			fmt.Fprintf(writer, `{"success":true,"result":{"id":"deployment-2","deployment_trigger":{"metadata":{"branch":"cms-preview/draft-1","commit_hash":"%s"}},"latest_stage":{"name":"queued","status":"active"}}}`, testCommitSHA)
		case request.Method == http.MethodDelete && strings.HasSuffix(request.URL.Path, "/deployment-1"):
			if request.URL.Query().Get("force") != "true" {
				t.Errorf("delete force = %q", request.URL.Query().Get("force"))
			}
			fmt.Fprint(writer, `{"success":true,"result":null}`)
		default:
			http.Error(writer, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider, err := newCloudflarePagesProvider(config.CloudflarePagesConfig{
		AccountID:   "account-id",
		ProjectName: "docs",
		APITokenEnv: "CF_TOKEN",
	}, server.URL, server.Client(), func(name string) (string, bool) {
		return token, name == "CF_TOKEN"
	})
	if err != nil {
		t.Fatalf("newCloudflarePagesProvider() error = %v", err)
	}

	deployment, err := provider.Trigger(context.Background(), "cms-preview/draft-1", testCommitSHA)
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if deployment.ID != "deployment-1" || deployment.Status != PreviewDeploymentBuilding || deployment.URL != "" {
		t.Fatalf("Trigger() = %#v", deployment)
	}

	deployment, err = provider.Status(context.Background(), "deployment-1")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if deployment.Status != PreviewDeploymentReady || deployment.URL != "https://abc12345.docs.pages.dev" {
		t.Fatalf("Status() = %#v", deployment)
	}
	previewURL, err := provider.URL(context.Background(), "deployment-1")
	if err != nil || previewURL != "https://abc12345.docs.pages.dev" {
		t.Fatalf("URL() = %q, %v", previewURL, err)
	}
	retried, err := provider.Retry(context.Background(), "deployment-1")
	if err != nil || retried.ID != "deployment-2" || retried.Status != PreviewDeploymentQueued {
		t.Fatalf("Retry() = %#v, %v", retried, err)
	}
	if err := provider.Delete(context.Background(), "deployment-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(requests) != 5 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestCloudflarePagesProviderMissingDeploymentIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`)
	}))
	defer server.Close()
	provider, err := newCloudflarePagesProvider(config.CloudflarePagesConfig{AccountID: "a", ProjectName: "p", APITokenEnv: "TOKEN"}, server.URL, server.Client(), func(string) (string, bool) { return "secret", true })
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Trigger(context.Background(), "cms-preview/draft-1", testCommitSHA)
	var providerErr *PreviewProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != PreviewProviderNotFound || !providerErr.Retryable {
		t.Fatalf("Trigger() error = %#v", err)
	}
}

func TestCloudflarePagesProviderDoesNotLeakTokenInErrors(t *testing.T) {
	token := "do-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(writer, `{"success":false,"errors":[{"code":9109,"message":"invalid %s"}]}`, token)
	}))
	defer server.Close()
	provider, err := newCloudflarePagesProvider(config.CloudflarePagesConfig{AccountID: "a", ProjectName: "p", APITokenEnv: "TOKEN"}, server.URL, server.Client(), func(string) (string, bool) { return token, true })
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Trigger(context.Background(), "cms-preview/draft-1", testCommitSHA)
	if !IsPreviewProviderError(err, PreviewProviderUnauthorized) {
		t.Fatalf("Trigger() error = %v", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestCloudflarePagesProviderRequiresTokenEnvironment(t *testing.T) {
	_, err := newCloudflarePagesProvider(config.CloudflarePagesConfig{AccountID: "a", ProjectName: "p", APITokenEnv: "TOKEN"}, "https://api.example.test", http.DefaultClient, func(string) (string, bool) { return "", false })
	if !IsPreviewProviderError(err, PreviewProviderNotConfigured) {
		t.Fatalf("newCloudflarePagesProvider() error = %v", err)
	}
}

func TestPreviewDeploymentFromCloudflareStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		stage  cloudflareDeploymentStage
		skip   bool
		status PreviewDeploymentStatus
	}{
		{name: "queued", stage: cloudflareDeploymentStage{Name: "queued", Status: "active"}, status: PreviewDeploymentQueued},
		{name: "building", stage: cloudflareDeploymentStage{Name: "build", Status: "active"}, status: PreviewDeploymentBuilding},
		{name: "ready", stage: cloudflareDeploymentStage{Name: "deploy", Status: "success"}, status: PreviewDeploymentReady},
		{name: "failed", stage: cloudflareDeploymentStage{Name: "build", Status: "failure"}, status: PreviewDeploymentFailed},
		{name: "skipped", stage: cloudflareDeploymentStage{Name: "deploy", Status: "success"}, skip: true, status: PreviewDeploymentFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := previewDeploymentFromCloudflare(cloudflareDeployment{URL: "https://immutable.example", IsSkipped: test.skip, LatestStage: test.stage})
			if got.Status != test.status {
				t.Fatalf("status = %q, want %q", got.Status, test.status)
			}
			if test.status != PreviewDeploymentReady && got.URL != "" {
				t.Fatalf("non-ready deployment exposed URL %q", got.URL)
			}
		})
	}
}
