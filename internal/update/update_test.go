package update_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/update"
)

func TestCompareVersions(t *testing.T) {
	require.Equal(t, 0, update.CompareVersions("v1.0.0", "1.0.0"))
	require.Equal(t, -1, update.CompareVersions("v0.1.0", "v0.2.0"))
	require.Equal(t, 1, update.CompareVersions("v0.2.0", "v0.1.0"))
	require.True(t, update.IsNewer("v0.2.0", "v0.1.0"))
	require.False(t, update.IsNewer("v0.1.0", "v0.1.0"))
	require.True(t, update.IsNewer("v0.1.0", "dev"))
	require.True(t, update.IsDev("dev"))
	require.True(t, update.IsDev(""))
	require.Equal(t, -1, update.CompareVersions("1.0.0-rc1", "1.0.0"))
}

func TestParseSHA256SUMS(t *testing.T) {
	raw := []byte(`
# comment
3eb54b0c65c94b7e2d58201552fce18d44ceaca2f0a4f35b02d01cbe583cb0ed  dist/openvpnd_v0.1.0_linux_amd64.tar.gz
5766394006e51559209d52e252870227e451e43135a1598a07de60b0eb371d92  openvpnd
755c5e916b491e451a63a42e71b775980d5f65161b78be58c539e4c310305a7c *openvpnctl
`)
	sums := update.ParseSHA256SUMS(raw)
	require.Equal(t, "3eb54b0c65c94b7e2d58201552fce18d44ceaca2f0a4f35b02d01cbe583cb0ed", sums["openvpnd_v0.1.0_linux_amd64.tar.gz"])
	h, ok := update.LookupChecksum(sums, "openvpnd")
	require.True(t, ok)
	require.Equal(t, "5766394006e51559209d52e252870227e451e43135a1598a07de60b0eb371d92", h)
	require.NoError(t, update.VerifySHA256([]byte("x"), update.SHA256Hex([]byte("x"))))
	require.Error(t, update.VerifySHA256([]byte("x"), strings.Repeat("0", 64)))
}

func TestSelectArchiveName(t *testing.T) {
	require.Equal(t, "openvpnd_v0.1.0_linux_amd64.tar.gz", update.SelectArchiveName("v0.1.0", "linux", "amd64"))
	require.Equal(t, "openvpnd_v0.1.0_linux_arm64.tar.gz", update.SelectArchiveName("0.1.0", "linux", "arm64"))
}

func TestParseReleaseJSON(t *testing.T) {
	_, err := update.ParseReleaseJSON([]byte(`{}`))
	require.Error(t, err)
	rel, err := update.ParseReleaseJSON([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"SHA256SUMS","browser_download_url":"http://x/SHA256SUMS"}]}`))
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", rel.TagName)
	require.NotNil(t, update.FindAsset(rel, "SHA256SUMS"))
}

func TestExtractFilesFromTarGz(t *testing.T) {
	archive := buildTarGz(t, map[string][]byte{
		"openvpnd":   []byte("daemon-bin"),
		"openvpnctl": []byte("ctl-bin"),
		"README.md":  []byte("skip-me"),
	})
	files, err := update.ExtractFilesFromTarGz(archive, map[string]bool{"openvpnd": true, "openvpnctl": true})
	require.NoError(t, err)
	require.Equal(t, []byte("daemon-bin"), files["openvpnd"])
	require.Equal(t, []byte("ctl-bin"), files["openvpnctl"])
	require.NotContains(t, files, "README.md")
}

func TestAtomicInstall(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "openvpnd")
	require.NoError(t, update.AtomicInstall(dest, []byte("hello"), 0o755))
	b, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "hello", string(b))
	require.NoError(t, update.AtomicInstall(dest, []byte("world"), 0o755))
	b, err = os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "world", string(b))
}

func TestCheckAndApplyWithHTTPTestServer(t *testing.T) {
	const tag = "v0.2.0"
	daemonBody := []byte("#!/bin/sh\necho openvpnd-v0.2.0\n")
	ctlBody := []byte("#!/bin/sh\necho openvpnctl-v0.2.0\n")
	archive := buildTarGz(t, map[string][]byte{
		"openvpnd":   daemonBody,
		"openvpnctl": ctlBody,
	})
	archiveName := update.SelectArchiveName(tag, "linux", "amd64")
	sums := fmt.Sprintf("%s  %s\n%s  openvpnd\n%s  openvpnctl\n",
		sha256hex(archive), archiveName, sha256hex(daemonBody), sha256hex(ctlBody))

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/reloadlife/openvpnd/releases/latest":
			writeJSON(w, releasePayload(tag, srvURL, archiveName))
		case r.URL.Path == "/repos/reloadlife/openvpnd/releases/tags/"+tag:
			writeJSON(w, releasePayload(tag, srvURL, archiveName))
		case r.URL.Path == "/download/"+archiveName:
			_, _ = w.Write(archive)
		case r.URL.Path == "/download/SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	client := &update.Client{
		HTTPClient: srv.Client(),
		APIBaseURL: srv.URL,
		Repo:       "reloadlife/openvpnd",
	}
	ctx := context.Background()

	checkRes, err := update.Check(ctx, update.Options{
		CurrentVersion: "v0.1.0",
		Client:         client,
	})
	require.NoError(t, err)
	require.True(t, checkRes.UpdateAvailable)
	require.Equal(t, tag, checkRes.LatestVersion)

	checkRes, err = update.Check(ctx, update.Options{
		CurrentVersion: "v0.2.0",
		Client:         client,
	})
	require.NoError(t, err)
	require.False(t, checkRes.UpdateAvailable)
	require.True(t, checkRes.AlreadyLatest)

	dir := t.TempDir()
	target := filepath.Join(dir, "openvpnd")
	require.NoError(t, os.WriteFile(target, []byte("old-daemon"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "openvpnctl"), []byte("old-ctl"), 0o755))

	applyRes, err := update.Apply(ctx, update.Options{
		CurrentVersion: "v0.1.0",
		BinaryName:     "openvpnd",
		TargetPath:     target,
		GOOS:           "linux",
		GOARCH:         "amd64",
		Client:         client,
	})
	require.NoError(t, err)
	require.False(t, applyRes.AlreadyLatest)
	require.Equal(t, target, applyRes.Installed["openvpnd"])
	require.Contains(t, applyRes.Installed, "openvpnctl")

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, daemonBody, got)
	got, err = os.ReadFile(filepath.Join(dir, "openvpnctl"))
	require.NoError(t, err)
	require.Equal(t, ctlBody, got)

	applyRes, err = update.Apply(ctx, update.Options{
		CurrentVersion: "v0.2.0",
		BinaryName:     "openvpnd",
		TargetPath:     target,
		GOOS:           "linux",
		GOARCH:         "amd64",
		Client:         client,
	})
	require.NoError(t, err)
	require.True(t, applyRes.AlreadyLatest)
}

func TestApplyBareBinariesWithChecksum(t *testing.T) {
	const tag = "v0.3.0"
	daemonBody := []byte("bare-openvpnd")
	sums := fmt.Sprintf("%s  openvpnd\n", sha256hex(daemonBody))

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/openvpnd/releases/tags/v0.3.0":
			writeJSON(w, map[string]any{
				"tag_name": tag,
				"assets": []map[string]any{
					{"name": "openvpnd", "browser_download_url": srvURL + "/download/openvpnd", "size": len(daemonBody)},
					{"name": "SHA256SUMS", "browser_download_url": srvURL + "/download/SHA256SUMS", "size": len(sums)},
				},
			})
		case "/download/openvpnd":
			_, _ = w.Write(daemonBody)
		case "/download/SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	dir := t.TempDir()
	target := filepath.Join(dir, "openvpnd")
	falseVal := false
	res, err := update.Apply(context.Background(), update.Options{
		Repo:           "acme/openvpnd",
		Version:        "v0.3.0",
		CurrentVersion: "v0.1.0",
		BinaryName:     "openvpnd",
		TargetPath:     target,
		UpdateSibling:  &falseVal,
		GOOS:           "linux",
		GOARCH:         "amd64",
		Client: &update.Client{
			HTTPClient: srv.Client(),
			APIBaseURL: srv.URL,
			Repo:       "acme/openvpnd",
		},
	})
	require.NoError(t, err)
	require.Equal(t, target, res.Installed["openvpnd"])
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, daemonBody, got)
}

func TestApplyChecksumMismatch(t *testing.T) {
	const tag = "v0.4.0"
	body := []byte("payload")
	badSums := fmt.Sprintf("%s  openvpnd\n", strings.Repeat("a", 64))

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/reloadlife/openvpnd/releases/latest":
			writeJSON(w, map[string]any{
				"tag_name": tag,
				"assets": []map[string]any{
					{"name": "openvpnd", "browser_download_url": srvURL + "/download/openvpnd"},
					{"name": "SHA256SUMS", "browser_download_url": srvURL + "/download/SHA256SUMS"},
				},
			})
		case "/download/openvpnd":
			_, _ = w.Write(body)
		case "/download/SHA256SUMS":
			_, _ = w.Write([]byte(badSums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	_, err := update.Apply(context.Background(), update.Options{
		CurrentVersion: "v0.1.0",
		BinaryName:     "openvpnd",
		TargetPath:     filepath.Join(t.TempDir(), "openvpnd"),
		GOOS:           "linux",
		GOARCH:         "amd64",
		Client: &update.Client{
			HTTPClient: srv.Client(),
			APIBaseURL: srv.URL,
			Repo:       "reloadlife/openvpnd",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum")
}

func TestRunCheckOutput(t *testing.T) {
	const tag = "v1.0.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"tag_name": tag, "assets": []any{}})
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := update.Run(context.Background(), update.Options{
		CurrentVersion: "v0.9.0",
		Client: &update.Client{
			HTTPClient: srv.Client(),
			APIBaseURL: srv.URL,
			Repo:       "reloadlife/openvpnd",
		},
		Stdout: &buf,
	}, true)
	require.NoError(t, err)
	require.Contains(t, buf.String(), "Update available")
	require.Contains(t, buf.String(), tag)
}

func TestPlanAssets(t *testing.T) {
	rel := &update.Release{
		TagName: "v0.1.0",
		Assets: []update.Asset{
			{Name: "openvpnd_v0.1.0_linux_amd64.tar.gz", BrowserDownloadURL: "http://x/a"},
			{Name: "SHA256SUMS", BrowserDownloadURL: "http://x/s"},
		},
	}
	plan, err := update.PlanAssets(rel, "linux", "amd64", []string{"openvpnd", "openvpnctl"})
	require.NoError(t, err)
	require.NotNil(t, plan.Archive)
	require.NotNil(t, plan.Checksums)

	rel2 := &update.Release{
		TagName: "v0.1.0",
		Assets: []update.Asset{
			{Name: "openvpnd", BrowserDownloadURL: "http://x/d"},
			{Name: "openvpnctl", BrowserDownloadURL: "http://x/c"},
		},
	}
	plan, err = update.PlanAssets(rel2, "linux", "amd64", []string{"openvpnd", "openvpnctl"})
	require.NoError(t, err)
	require.Nil(t, plan.Archive)
	require.Contains(t, plan.Binaries, "openvpnd")
}

func releasePayload(tag, base, archiveName string) map[string]any {
	return map[string]any{
		"tag_name": tag,
		"assets": []map[string]any{
			{"name": archiveName, "browser_download_url": base + "/download/" + archiveName},
			{"name": "SHA256SUMS", "browser_download_url": base + "/download/SHA256SUMS"},
		},
	}
}

func buildTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
