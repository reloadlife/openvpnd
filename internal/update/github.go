package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultRepo is the GitHub repository for official releases.
	DefaultRepo = "reloadlife/openvpnd"
	// DefaultAPIBase is the GitHub REST API root.
	DefaultAPIBase = "https://api.github.com"
	userAgent      = "openvpnd-updater"
)

// Asset is a GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release is a subset of the GitHub Releases API payload.
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Client talks to the GitHub Releases API (or a mock with APIBaseURL).
type Client struct {
	HTTPClient *http.Client
	APIBaseURL string
	Repo       string
	// Token is an optional GitHub token (also read from GITHUB_TOKEN when empty).
	Token string
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) apiBase() string {
	if c != nil && c.APIBaseURL != "" {
		return strings.TrimRight(c.APIBaseURL, "/")
	}
	return DefaultAPIBase
}

func (c *Client) repo() string {
	if c != nil && c.Repo != "" {
		return c.Repo
	}
	return DefaultRepo
}

func (c *Client) token() string {
	if c != nil && c.Token != "" {
		return c.Token
	}
	return os.Getenv("GITHUB_TOKEN")
}

// FetchRelease loads latest or a specific tag from GitHub Releases.
// version empty or "latest" uses /releases/latest; otherwise /releases/tags/{version}.
func (c *Client) FetchRelease(ctx context.Context, version string) (*Release, error) {
	repo := c.repo()
	version = strings.TrimSpace(version)
	var path string
	if version == "" || strings.EqualFold(version, "latest") {
		path = fmt.Sprintf("/repos/%s/releases/latest", repo)
	} else {
		if !strings.HasPrefix(version, "v") && !strings.HasPrefix(version, "V") {
			// accept 0.1.0 as v0.1.0 for tags
			version = "v" + version
		}
		path = fmt.Sprintf("/repos/%s/releases/tags/%s", repo, version)
	}
	url := c.apiBase() + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if tok := c.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases %s: HTTP %d: %s", path, res.StatusCode, strings.TrimSpace(string(body)))
	}
	rel, err := ParseReleaseJSON(body)
	if err != nil {
		return nil, err
	}
	return rel, nil
}

// ParseReleaseJSON decodes a GitHub release JSON document (pure, testable).
func ParseReleaseJSON(data []byte) (*Release, error) {
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("parse release json: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release missing tag_name")
	}
	return &rel, nil
}

// Download fetches url into memory (size-limited).
func (c *Client) Download(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 256 << 20 // 256 MiB
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if tok := c.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("download %s: HTTP %d: %s", url, res.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("download %s: exceeds size limit %d", url, maxBytes)
	}
	return data, nil
}

// FindAsset returns the first asset whose name equals name (case-sensitive).
func FindAsset(rel *Release, name string) *Asset {
	if rel == nil {
		return nil
	}
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}
	return nil
}

// SelectArchiveName builds the preferred tarball asset name for GOOS/GOARCH.
// Example: openvpnd_v0.1.0_linux_amd64.tar.gz
func SelectArchiveName(tag, goos, goarch string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	tag = strings.TrimSpace(tag)
	if tag != "" && !strings.HasPrefix(tag, "v") && !strings.HasPrefix(tag, "V") {
		tag = "v" + tag
	}
	return fmt.Sprintf("openvpnd_%s_%s_%s.tar.gz", tag, goos, goarch)
}

// SelectReleaseAssets picks download strategy for a release.
// Prefers the platform tarball; falls back to bare binary asset names.
type AssetPlan struct {
	// Archive is set when a .tar.gz is preferred.
	Archive *Asset
	// Binaries maps binary name → asset when using bare assets.
	Binaries map[string]*Asset
	// Checksums is the SHA256SUMS asset if present.
	Checksums *Asset
}

// PlanAssets selects archive or bare binaries from the release.
func PlanAssets(rel *Release, goos, goarch string, binaryNames []string) (*AssetPlan, error) {
	if rel == nil {
		return nil, fmt.Errorf("nil release")
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	plan := &AssetPlan{Binaries: make(map[string]*Asset)}
	plan.Checksums = FindAsset(rel, "SHA256SUMS")

	wantArchive := SelectArchiveName(rel.TagName, goos, goarch)
	if a := FindAsset(rel, wantArchive); a != nil {
		plan.Archive = a
		return plan, nil
	}
	// alternate: openvpnd_0.1.0_linux_amd64 without v
	alt := SelectArchiveName(NormalizeVersion(rel.TagName), goos, goarch)
	if alt != wantArchive {
		if a := FindAsset(rel, alt); a != nil {
			plan.Archive = a
			return plan, nil
		}
	}
	// platform-specific bare names: openvpnd-linux-amd64
	for _, name := range binaryNames {
		if a := FindAsset(rel, fmt.Sprintf("%s-%s-%s", name, goos, goarch)); a != nil {
			plan.Binaries[name] = a
			continue
		}
		if a := FindAsset(rel, name); a != nil {
			plan.Binaries[name] = a
		}
	}
	if len(plan.Binaries) == 0 {
		return nil, fmt.Errorf("no release assets for %s/%s (wanted %s or bare binaries)", goos, goarch, wantArchive)
	}
	return plan, nil
}
