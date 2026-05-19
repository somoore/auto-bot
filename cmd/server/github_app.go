package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type githubAppClient struct {
	appID             string
	installationID    string
	privateKey        *rsa.PrivateKey
	allowedRepos      map[string]struct{}
	defaultRepo       string
	prCommentsEnabled bool
	httpClient        *http.Client
}

type githubPullRequestFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch"`
}

func newGitHubAppClientFromEnv() (*githubAppClient, error) {
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	installationID := strings.TrimSpace(os.Getenv("GITHUB_APP_INSTALLATION_ID"))
	privateKeyPEM := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if privateKeyPEM == "" && strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE")) != "" {
		raw, err := os.ReadFile(strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE")))
		if err != nil {
			return nil, fmt.Errorf("read GITHUB_APP_PRIVATE_KEY_FILE: %w", err)
		}
		privateKeyPEM = strings.TrimSpace(string(raw))
	}
	defaultRepo := normalizeRepoSpecifier(os.Getenv("GITHUB_DEFAULT_REPO"))
	if appID == "" && installationID == "" && privateKeyPEM == "" && defaultRepo == "" {
		return nil, nil
	}
	if appID == "" || installationID == "" || privateKeyPEM == "" {
		return nil, fmt.Errorf("GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, and GITHUB_APP_PRIVATE_KEY are required for GitHub App agent access")
	}
	privateKey, err := parseGitHubAppPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	allowedRepos := parseAllowedRepos(os.Getenv("GITHUB_ALLOWED_REPOS"))
	if defaultRepo != "" && len(allowedRepos) == 0 {
		allowedRepos[defaultRepo] = struct{}{}
	}
	return &githubAppClient{
		appID:             appID,
		installationID:    installationID,
		privateKey:        privateKey,
		allowedRepos:      allowedRepos,
		defaultRepo:       defaultRepo,
		prCommentsEnabled: strings.EqualFold(os.Getenv("GITHUB_PR_COMMENTS_ENABLED"), "true"),
		httpClient:        &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (client *githubAppClient) Configured() bool {
	return client != nil && client.appID != "" && client.installationID != "" && client.privateKey != nil
}

func (client *githubAppClient) PRCommentsEnabled() bool {
	return client != nil && client.prCommentsEnabled
}

func (client *githubAppClient) FetchPullRequestFiles(ctx context.Context, repo string, pullRequestNumber int) ([]githubPullRequestFile, string, error) {
	owner, name, err := client.validateRepo(repo)
	if err != nil {
		return nil, "", err
	}
	if pullRequestNumber <= 0 {
		return nil, "", fmt.Errorf("pull_request_number is required")
	}
	token, err := client.installationToken(ctx, repo, map[string]string{
		"contents":      "read",
		"metadata":      "read",
		"pull_requests": "read",
	})
	if err != nil {
		return nil, "", err
	}
	var files []githubPullRequestFile
	for page := 1; page <= 5; page++ {
		var batch []githubPullRequestFile
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(name), pullRequestNumber, page)
		if err := client.doGitHubJSON(ctx, token, http.MethodGet, path, nil, &batch); err != nil {
			return nil, "", err
		}
		files = append(files, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return files, fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, name, pullRequestNumber), nil
}

func (client *githubAppClient) CreatePullRequestReview(ctx context.Context, repo string, pullRequestNumber int, body string) error {
	owner, name, err := client.validateRepo(repo)
	if err != nil {
		return err
	}
	if pullRequestNumber <= 0 {
		return fmt.Errorf("pull_request_number is required")
	}
	if strings.TrimSpace(body) == "" {
		return nil
	}
	token, err := client.installationToken(ctx, repo, map[string]string{
		"metadata":      "read",
		"pull_requests": "write",
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"event": "COMMENT",
		"body":  truncateString(body, 60000),
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", url.PathEscape(owner), url.PathEscape(name), pullRequestNumber)
	return client.doGitHubJSON(ctx, token, http.MethodPost, path, payload, nil)
}

func (client *githubAppClient) installationToken(ctx context.Context, repo string, permissions map[string]string) (string, error) {
	if !client.Configured() {
		return "", fmt.Errorf("GitHub App client is not configured")
	}
	_, repoName, err := client.validateRepo(repo)
	if err != nil {
		return "", err
	}
	jwt, err := client.jwt()
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"repositories": []string{repoName},
		"permissions":  permissions,
	}
	var response struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	path := "/app/installations/" + url.PathEscape(client.installationID) + "/access_tokens"
	if err := client.doGitHubJSON(ctx, jwt, http.MethodPost, path, body, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", fmt.Errorf("GitHub App installation token response did not include a token")
	}
	return response.Token, nil
}

func (client *githubAppClient) doGitHubJSON(ctx context.Context, token string, method string, path string, body any, out any) error {
	var requestBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("GitHub %s %s failed: status=%s body=%s", method, path, response.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, response.Body) //nolint:errcheck
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func (client *githubAppClient) validateRepo(repo string) (string, string, error) {
	repo = normalizeRepoSpecifier(repo)
	if repo == "" {
		repo = client.defaultRepo
	}
	repo = normalizeRepoSpecifier(repo)
	if repo == "" {
		return "", "", fmt.Errorf("repo must use owner/name")
	}
	if len(client.allowedRepos) > 0 {
		if _, ok := client.allowedRepos[repo]; !ok {
			return "", "", fmt.Errorf("refusing GitHub access for repo %q outside GITHUB_ALLOWED_REPOS", repo)
		}
	}
	owner, name, _ := strings.Cut(repo, "/")
	return owner, name, nil
}

func (client *githubAppClient) jwt() (string, error) {
	now := time.Now().UTC()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadMap := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": client.appID,
	}
	payloadRaw, err := json.Marshal(payloadMap)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	signingInput := header + "." + payload
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, client.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseGitHubAppPrivateKey(value string) (*rsa.PrivateKey, error) {
	value = strings.TrimSpace(value)
	if decoded, err := hex.DecodeString(value); err == nil && strings.HasPrefix(string(decoded), "-----BEGIN") {
		value = string(decoded)
	}
	value = strings.ReplaceAll(value, `\n`, "\n")
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("GitHub App private key must be PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("GitHub App private key must be RSA")
	}
	return key, nil
}

func parseAllowedRepos(value string) map[string]struct{} {
	repos := map[string]struct{}{}
	for _, item := range strings.Split(value, ",") {
		if repo := normalizeRepoSpecifier(item); repo != "" {
			repos[repo] = struct{}{}
		}
	}
	return repos
}

func githubSetupConfigured() bool {
	return strings.TrimSpace(os.Getenv("GITHUB_APP_ID")) != "" &&
		strings.TrimSpace(os.Getenv("GITHUB_APP_INSTALLATION_ID")) != "" &&
		(strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY")) != "" || strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE")) != "")
}

func githubAppIDForDocs() string {
	id := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	if id == "" {
		return ""
	}
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return "configured"
	}
	return "configured"
}
