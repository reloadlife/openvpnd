package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options controls Check / Apply.
type Options struct {
	// Repo is owner/name (default reloadlife/openvpnd).
	Repo string
	// Version is empty/latest or a tag like v0.2.0.
	Version string
	// CurrentVersion is the running binary version (version.Version).
	CurrentVersion string
	// BinaryName is the primary binary to install (openvpnd or openvpnctl).
	// Empty → basename of TargetPath / os.Executable.
	BinaryName string
	// TargetPath is where to install the primary binary. Empty → os.Executable().
	TargetPath string
	// UpdateSibling also replaces the other binary in the same directory when present
	// in the release (default true).
	UpdateSibling *bool
	// GOOS / GOARCH override runtime (tests).
	GOOS   string
	GOARCH string
	// Client is optional preconfigured API client (tests inject httptest).
	Client *Client
	// Stdout for messages; default os.Stdout.
	Stdout io.Writer
	// Stderr for diagnostics; default os.Stderr.
	Stderr io.Writer
}

// Result is the outcome of Check or Apply.
type Result struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	// Installed maps binary name → path written (Apply only).
	Installed map[string]string
	// AlreadyLatest is true when no download was needed.
	AlreadyLatest bool
}

func (o Options) stdout() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

func (o Options) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

func (o Options) client() *Client {
	if o.Client != nil {
		if o.Repo != "" && o.Client.Repo == "" {
			o.Client.Repo = o.Repo
		}
		return o.Client
	}
	return &Client{Repo: o.Repo}
}

func (o Options) goos() string {
	if o.GOOS != "" {
		return o.GOOS
	}
	return runtime.GOOS
}

func (o Options) goarch() string {
	if o.GOARCH != "" {
		return o.GOARCH
	}
	return runtime.GOARCH
}

func (o Options) updateSibling() bool {
	if o.UpdateSibling == nil {
		return true
	}
	return *o.UpdateSibling
}

// Check reports whether a newer (or requested) release exists without downloading binaries.
func Check(ctx context.Context, opts Options) (*Result, error) {
	rel, err := opts.client().FetchRelease(ctx, opts.Version)
	if err != nil {
		return nil, err
	}
	res := &Result{
		CurrentVersion:  opts.CurrentVersion,
		LatestVersion:   rel.TagName,
		UpdateAvailable: IsNewer(rel.TagName, opts.CurrentVersion),
	}
	// explicit --version request: treat as "available" if different from current
	if strings.TrimSpace(opts.Version) != "" && !strings.EqualFold(opts.Version, "latest") {
		res.UpdateAvailable = CompareVersions(rel.TagName, opts.CurrentVersion) != 0 || IsDev(opts.CurrentVersion)
	}
	if !res.UpdateAvailable {
		res.AlreadyLatest = true
	}
	return res, nil
}

// Apply downloads, verifies, and atomically installs release binaries.
func Apply(ctx context.Context, opts Options) (*Result, error) {
	target, err := opts.resolveTarget()
	if err != nil {
		return nil, err
	}
	binName := opts.BinaryName
	if binName == "" {
		binName = filepath.Base(target)
	}

	rel, err := opts.client().FetchRelease(ctx, opts.Version)
	if err != nil {
		return nil, err
	}
	res := &Result{
		CurrentVersion:  opts.CurrentVersion,
		LatestVersion:   rel.TagName,
		UpdateAvailable: IsNewer(rel.TagName, opts.CurrentVersion),
		Installed:       make(map[string]string),
	}

	// skip if already on this tag (unless current is dev or explicit different version)
	explicit := strings.TrimSpace(opts.Version) != "" && !strings.EqualFold(opts.Version, "latest")
	if !explicit && !IsDev(opts.CurrentVersion) && CompareVersions(rel.TagName, opts.CurrentVersion) == 0 {
		res.AlreadyLatest = true
		res.UpdateAvailable = false
		return res, nil
	}
	if !explicit && !IsDev(opts.CurrentVersion) && !IsNewer(rel.TagName, opts.CurrentVersion) {
		res.AlreadyLatest = true
		res.UpdateAvailable = false
		return res, nil
	}

	wantBins := []string{"openvpnd", "openvpnctl"}
	plan, err := PlanAssets(rel, opts.goos(), opts.goarch(), wantBins)
	if err != nil {
		return nil, err
	}

	var sums map[string]string
	if plan.Checksums != nil {
		raw, err := opts.client().Download(ctx, plan.Checksums.BrowserDownloadURL, 1<<20)
		if err != nil {
			return nil, fmt.Errorf("download SHA256SUMS: %w", err)
		}
		sums = ParseSHA256SUMS(raw)
	}

	files := make(map[string][]byte) // binary name → content

	if plan.Archive != nil {
		archiveData, err := opts.client().Download(ctx, plan.Archive.BrowserDownloadURL, 256<<20)
		if err != nil {
			return nil, fmt.Errorf("download archive: %w", err)
		}
		if sums != nil {
			if h, ok := LookupChecksum(sums, plan.Archive.Name); ok {
				if err := VerifySHA256(archiveData, h); err != nil {
					return nil, fmt.Errorf("archive checksum: %w", err)
				}
			}
		}
		want := map[string]bool{"openvpnd": true, "openvpnctl": true}
		extracted, err := ExtractFilesFromTarGz(archiveData, want)
		if err != nil {
			return nil, fmt.Errorf("extract archive: %w", err)
		}
		for name, data := range extracted {
			// prefer per-file checksum when listed
			if sums != nil {
				if h, ok := LookupChecksum(sums, name); ok {
					if err := VerifySHA256(data, h); err != nil {
						return nil, fmt.Errorf("%s checksum: %w", name, err)
					}
				}
			}
			files[name] = data
		}
	} else {
		for name, asset := range plan.Binaries {
			data, err := opts.client().Download(ctx, asset.BrowserDownloadURL, 256<<20)
			if err != nil {
				return nil, fmt.Errorf("download %s: %w", name, err)
			}
			if sums != nil {
				if h, ok := LookupChecksum(sums, asset.Name); ok {
					if err := VerifySHA256(data, h); err != nil {
						return nil, fmt.Errorf("%s checksum: %w", name, err)
					}
				} else if h, ok := LookupChecksum(sums, name); ok {
					if err := VerifySHA256(data, h); err != nil {
						return nil, fmt.Errorf("%s checksum: %w", name, err)
					}
				} else {
					return nil, fmt.Errorf("SHA256SUMS present but no entry for %s", name)
				}
			}
			files[name] = data
		}
	}

	primary, ok := files[binName]
	if !ok {
		return nil, fmt.Errorf("release does not contain binary %q", binName)
	}
	if err := AtomicInstall(target, primary, 0o755); err != nil {
		return nil, fmt.Errorf("install %s: %w", target, err)
	}
	res.Installed[binName] = target

	if opts.updateSibling() {
		for _, other := range []string{"openvpnd", "openvpnctl"} {
			if other == binName {
				continue
			}
			data, ok := files[other]
			if !ok {
				continue
			}
			sib := SiblingPath(target, other)
			if _, err := os.Stat(sib); err != nil {
				if os.IsNotExist(err) {
					continue // do not create a missing companion binary
				}
				return nil, err
			}
			if err := AtomicInstall(sib, data, 0o755); err != nil {
				return nil, fmt.Errorf("install %s: %w", sib, err)
			}
			res.Installed[other] = sib
		}
	}

	res.UpdateAvailable = true
	return res, nil
}

func (o Options) resolveTarget() (string, error) {
	if o.TargetPath != "" {
		return filepath.Abs(o.TargetPath)
	}
	return ResolveExecutable()
}

// Run is the CLI entry: --check prints status; otherwise apply and print restart hints.
func Run(ctx context.Context, opts Options, checkOnly bool) error {
	out := opts.stdout()
	if checkOnly {
		res, err := Check(ctx, opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Current: %s\n", displayVer(res.CurrentVersion))
		fmt.Fprintf(out, "Latest:  %s\n", res.LatestVersion)
		if res.UpdateAvailable {
			fmt.Fprintln(out, "Update available")
		} else {
			fmt.Fprintln(out, "Already up to date")
		}
		return nil
	}

	res, err := Apply(ctx, opts)
	if err != nil {
		return err
	}
	if res.AlreadyLatest {
		fmt.Fprintf(out, "Already up to date (%s)\n", displayVer(res.LatestVersion))
		return nil
	}
	for name, path := range res.Installed {
		fmt.Fprintf(out, "Installed %s %s → %s\n", name, res.LatestVersion, path)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "If the daemon is managed by systemd, restart it:")
	fmt.Fprintln(out, "  sudo systemctl restart openvpnd")
	fmt.Fprintln(out, "Then verify:")
	fmt.Fprintln(out, "  openvpnd version")
	fmt.Fprintln(out, "  openvpnctl version")
	return nil
}

func displayVer(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}
