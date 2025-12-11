package updater

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// GitHubReleaseChecker fetches releases from GitHub API
type GitHubReleaseChecker struct {
	repository string
	httpClient *http.Client
	cache      *releaseCache
}

// releaseCache caches release metadata to avoid rate limiting
type releaseCache struct {
	releases  map[string]*ReleaseInfo
	expiresAt map[string]time.Time
	ttl       time.Duration
}

// NewGitHubReleaseChecker creates a new GitHub release checker
func NewGitHubReleaseChecker(repository string) *GitHubReleaseChecker {
	return &GitHubReleaseChecker{
		repository: repository,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache: &releaseCache{
			releases:  make(map[string]*ReleaseInfo),
			expiresAt: make(map[string]time.Time),
			ttl:       5 * time.Minute,
		},
	}
}

// FetchRelease fetches a specific release by version
// Version can be: "auto-update" (latest stable), "dev" (latest prerelease), or "v1.2.3" (specific)
func (c *GitHubReleaseChecker) FetchRelease(ctx context.Context, version string) (*ReleaseInfo, error) {
	// Check cache first
	if cached, ok := c.cache.get(version); ok {
		return cached, nil
	}

	var release *ReleaseInfo
	var err error

	switch version {
	case "", "auto-update":
		release, err = c.FetchLatestStable(ctx)
	case "dev":
		release, err = c.FetchLatestPrerelease(ctx)
	default:
		release, err = c.fetchSpecificRelease(ctx, version)
	}

	if err != nil {
		return nil, err
	}

	// Cache the result
	c.cache.set(version, release)
	return release, nil
}

// FetchLatestStable fetches the latest stable release
func (c *GitHubReleaseChecker) FetchLatestStable(ctx context.Context) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", c.repository)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var ghRelease githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&ghRelease); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.parseGitHubRelease(&ghRelease)
}

// FetchLatestPrerelease fetches the latest prerelease (dev)
func (c *GitHubReleaseChecker) FetchLatestPrerelease(ctx context.Context) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases", c.repository)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var ghReleases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&ghReleases); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Find the first prerelease
	for _, ghRelease := range ghReleases {
		if ghRelease.Prerelease {
			return c.parseGitHubRelease(&ghRelease)
		}
	}

	return nil, fmt.Errorf("no prerelease found")
}

// fetchSpecificRelease fetches a release by tag name
func (c *GitHubReleaseChecker) fetchSpecificRelease(ctx context.Context, version string) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", c.repository, version)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var ghRelease githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&ghRelease); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.parseGitHubRelease(&ghRelease)
}

// parseGitHubRelease converts GitHub API response to ReleaseInfo
func (c *GitHubReleaseChecker) parseGitHubRelease(ghRelease *githubRelease) (*ReleaseInfo, error) {
	release := &ReleaseInfo{
		Version:      ghRelease.TagName,
		IsPrerelease: ghRelease.Prerelease,
		PublishedAt:  ghRelease.PublishedAt,
		Body:         ghRelease.Body,
		Assets:       make([]ReleaseAsset, 0),
	}

	// Download and parse checksums.txt if available
	checksums := c.downloadChecksums(ghRelease)

	// Parse assets
	for _, ghAsset := range ghRelease.Assets {
		asset := parseAssetName(ghAsset.Name)
		if asset == nil {
			continue // Skip non-binary assets
		}

		asset.DownloadURL = ghAsset.BrowserDownloadURL
		asset.Size = ghAsset.Size
		
		// Set SHA256 from checksums map
		if sha, ok := checksums[ghAsset.Name]; ok {
			asset.SHA256 = sha
		}
		
		release.Assets = append(release.Assets, *asset)
	}

	// Set release SHA256 to the ufctl binary for current platform's SHA (for backward compatibility)
	for _, asset := range release.Assets {
		if asset.Binary == "ufctl" && asset.Platform == runtime.GOOS && asset.Arch == runtime.GOARCH {
			release.SHA256 = asset.SHA256
			break
		}
	}

	return release, nil
}

// parseAssetName parses binary asset name to extract platform, arch, and binary type
// Expected format: underleaf_agent-darwin-arm64 or ufctl-linux-amd64
func parseAssetName(name string) *ReleaseAsset {
	// Match pattern: (underleaf_agent|ufctl)-{os}-{arch}
	re := regexp.MustCompile(`^(underleaf_agent|ufctl)-([^-]+)-([^-]+)(?:\.exe)?$`)
	matches := re.FindStringSubmatch(name)

	if len(matches) != 4 {
		return nil
	}

	return &ReleaseAsset{
		Name:     name,
		Binary:   matches[1],
		Platform: matches[2],
		Arch:     matches[3],
	}
}

// extractSHA256FromBody extracts SHA256 hash from release notes
// Looks for patterns like: SHA256: abc123... or sha256sum: abc123...
func extractSHA256FromBody(body string) string {
	patterns := []string{
		`SHA256:\s*([a-fA-F0-9]{64})`,
		`sha256sum:\s*([a-fA-F0-9]{64})`,
		`sha256:\s*([a-fA-F0-9]{64})`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(body); len(matches) > 1 {
			return strings.ToLower(matches[1])
		}
	}

	return ""
}

// downloadChecksums downloads and parses the checksums.txt file from a GitHub release
func (c *GitHubReleaseChecker) downloadChecksums(ghRelease *githubRelease) map[string]string {
	checksums := make(map[string]string)
	
	// Find checksums.txt asset
	var checksumsURL string
	for _, asset := range ghRelease.Assets {
		if asset.Name == "checksums.txt" {
			checksumsURL = asset.BrowserDownloadURL
			break
		}
	}
	
	if checksumsURL == "" {
		return checksums // No checksums file available
	}
	
	// Download checksums.txt
	resp, err := http.Get(checksumsURL)
	if err != nil {
		return checksums
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return checksums
	}
	
	// Parse checksums file (format: "sha256  filename" per line)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		// Split on whitespace (handles both single space and multiple spaces/tabs)
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			sha := strings.ToLower(parts[0])
			filename := parts[1]
			checksums[filename] = sha
		}
	}
	
	return checksums
}

// GetAssetForPlatform returns the asset matching the current platform
func (r *ReleaseInfo) GetAssetForPlatform(binary string) (*ReleaseAsset, error) {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	for _, asset := range r.Assets {
		if asset.Binary == binary && asset.Platform == platform && asset.Arch == arch {
			return &asset, nil
		}
	}

	return nil, fmt.Errorf("no asset found for %s on %s/%s", binary, platform, arch)
}

// Cache methods
func (c *releaseCache) get(version string) (*ReleaseInfo, bool) {
	if expires, ok := c.expiresAt[version]; ok {
		if time.Now().Before(expires) {
			return c.releases[version], true
		}
		// Expired, remove from cache
		delete(c.releases, version)
		delete(c.expiresAt, version)
	}
	return nil, false
}

func (c *releaseCache) set(version string, release *ReleaseInfo) {
	c.releases[version] = release
	c.expiresAt[version] = time.Now().Add(c.ttl)
}

// githubRelease represents GitHub API release response
type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}
