package ovpnbackend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirstVersionLine(t *testing.T) {
	t.Parallel()
	require.Equal(t, "OpenVPN 2.6.14 x86_64", firstVersionLine("OpenVPN 2.6.14 x86_64\nlibrary versions: OpenSSL\n"))
	require.Equal(t, "OpenVPN 2.5.0", firstVersionLine("  OpenVPN 2.5.0  \n"))
	require.Equal(t, "", firstVersionLine("   \n  \n"))
	require.Equal(t, "only", firstVersionLine("only"))
}

func TestLooksLikeOpenVPNVersion(t *testing.T) {
	t.Parallel()
	require.True(t, looksLikeOpenVPNVersion("OpenVPN 2.6.14 x86_64-pc-linux-gnu"))
	require.True(t, looksLikeOpenVPNVersion("  openvpn 2.4.0  "))
	require.False(t, looksLikeOpenVPNVersion(""))
	require.False(t, looksLikeOpenVPNVersion("not a version string"))
	require.False(t, looksLikeOpenVPNVersion("OpenSSL 3.0.0"))
}

func TestParsePIDFileContent(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1234, parsePIDFileContent([]byte("1234\n")))
	require.Equal(t, 42, parsePIDFileContent([]byte("  42  ")))
	require.Equal(t, 0, parsePIDFileContent([]byte("")))
	require.Equal(t, 0, parsePIDFileContent([]byte("not-a-pid")))
	require.Equal(t, 0, parsePIDFileContent([]byte("12abc")))
}

func TestReadPIDFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pid")
	require.Equal(t, 0, readPIDFile(path))
	require.NoError(t, os.WriteFile(path, []byte("9999\n"), 0o600))
	require.Equal(t, 9999, readPIDFile(path))
}

func TestWriteFile0600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.conf")
	require.NoError(t, writeFile0600(path, "secret-conf\n"))
	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "secret-conf\n", string(b))
}

func TestProcessAlive(t *testing.T) {
	t.Parallel()
	require.False(t, processAlive(0))
	require.False(t, processAlive(-1))
	require.True(t, processAlive(os.Getpid()))
}

func TestFindOpenVPNEmptyOrPath(t *testing.T) {
	// Not parallel: exercises PATH/common locations on this host.
	p := FindOpenVPN()
	if p == "" {
		t.Log("openvpn not installed; FindOpenVPN returned empty")
		return
	}
	st, err := os.Stat(p)
	require.NoError(t, err)
	require.False(t, st.IsDir())
}

func TestNewHostBackendTempDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b, err := NewHostBackend(HostOptions{
		ConfDir:    filepath.Join(dir, "conf"),
		RuntimeDir: filepath.Join(dir, "run"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	require.DirExists(t, filepath.Join(dir, "conf"))
	require.DirExists(t, filepath.Join(dir, "run"))

	require.NoError(t, b.WriteFile(filepath.Join(dir, "conf", "x.conf"), "verb 3\n"))
}
