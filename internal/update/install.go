package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExtractFilesFromTarGz reads a .tar.gz and returns basename → file bytes
// for regular files. If want is non-nil, only basenames present in want are kept.
func ExtractFilesFromTarGz(data []byte, want map[string]bool) (map[string][]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	out := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base == "" || base == "." || base == ".." {
			continue
		}
		if want != nil && !want[base] {
			continue
		}
		if hdr.Size > 256<<20 {
			return nil, fmt.Errorf("tar entry %s too large", base)
		}
		var r io.Reader = tr
		if hdr.Size > 0 {
			r = io.LimitReader(tr, hdr.Size)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", base, err)
		}
		out[base] = b
	}
	return out, nil
}

// AtomicInstall writes data to dest via a same-directory temp file + rename.
func AtomicInstall(dest string, data []byte, mode os.FileMode) error {
	if dest == "" {
		return fmt.Errorf("empty install path")
	}
	if mode == 0 {
		mode = 0o755
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".openvpnd-update-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("rename to %s: %w", dest, err)
	}
	ok = true
	return nil
}

// ResolveExecutable returns the absolute, symlink-resolved path of this process.
func ResolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// SiblingPath returns dir/name next to path.
func SiblingPath(path, name string) string {
	return filepath.Join(filepath.Dir(path), name)
}
