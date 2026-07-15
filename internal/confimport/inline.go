package confimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Known OpenVPN inline XML/PEM block tags.
var InlineTags = []string{
	"ca", "cert", "key", "tls-crypt", "tls-auth", "secret", "dh", "extra-certs", "tls-crypt-v2",
}

// ExtractInlineBlocks pulls <tag>...</tag> PEM sections from OpenVPN conf/ovpn content.
// Returns a map of tag → body (trailing newline) and the content with those blocks removed.
// Case-insensitive tag matching; last block wins if a tag appears multiple times.
func ExtractInlineBlocks(content string) (blocks map[string]string, body string) {
	blocks = map[string]string{}
	body = content
	for _, tag := range InlineTags {
		open := "<" + tag + ">"
		closeT := "</" + tag + ">"
		for {
			low := strings.ToLower(body)
			i := strings.Index(low, open)
			if i < 0 {
				break
			}
			j := strings.Index(low[i+len(open):], closeT)
			if j < 0 {
				break
			}
			start := i + len(open)
			end := i + len(open) + j
			blocks[tag] = strings.TrimSpace(body[start:end]) + "\n"
			body = body[:i] + "\n" + body[end+len(closeT):]
		}
	}
	return blocks, body
}

// MaterializeOptions controls writing extracted inline PEMs to disk.
type MaterializeOptions struct {
	// DestDir is the directory for written material (created if missing, mode 0700).
	DestDir string
}

// Materialize writes pending Inline PEM blocks under DestDir, sets path fields on r,
// and removes inline-related warnings. No-op when Inline is empty.
//
// File names:
//
//	ca.crt, cert.crt|server.crt|client.crt, key.key|server.key|client.key,
//	tls-crypt.key, tls-auth.key, static.key, dh.pem, extra-certs.crt, tls-crypt-v2.key
func (r *Result) Materialize(opts MaterializeOptions) error {
	if r == nil {
		return fmt.Errorf("nil Result")
	}
	if len(r.Inline) == 0 {
		return nil
	}
	dest := strings.TrimSpace(opts.DestDir)
	if dest == "" {
		return fmt.Errorf("materialize dest dir required")
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("mkdir import dir: %w", err)
	}
	_ = os.Chmod(dest, 0o700)

	write := func(name, body string) (string, error) {
		path := filepath.Join(dest, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return "", err
		}
		_ = os.Chmod(path, 0o600)
		return path, nil
	}

	certName, keyName := "cert.crt", "key.key"
	switch strings.ToLower(r.Role) {
	case "server":
		certName, keyName = "server.crt", "server.key"
	case "client":
		certName, keyName = "client.crt", "client.key"
	}

	var err error
	if body, ok := r.Inline["ca"]; ok {
		if r.PKICaPath, err = write("ca.crt", body); err != nil {
			return err
		}
	}
	if body, ok := r.Inline["cert"]; ok {
		if r.PKICertPath, err = write(certName, body); err != nil {
			return err
		}
	}
	if body, ok := r.Inline["key"]; ok {
		if r.PKIKeyPath, err = write(keyName, body); err != nil {
			return err
		}
	}
	if body, ok := r.Inline["tls-crypt"]; ok {
		if r.TLSCryptPath, err = write("tls-crypt.key", body); err != nil {
			return err
		}
	}
	if body, ok := r.Inline["tls-auth"]; ok {
		if r.StaticKeyPath, err = write("tls-auth.key", body); err != nil {
			return err
		}
	}
	if body, ok := r.Inline["secret"]; ok {
		if r.StaticKeyPath, err = write("static.key", body); err != nil {
			return err
		}
		r.AuthMode = "static_key"
	}
	if body, ok := r.Inline["dh"]; ok {
		if r.PKIDHPath, err = write("dh.pem", body); err != nil {
			return err
		}
	}
	// Tags without first-class Result fields: write + preserve as path directives.
	if body, ok := r.Inline["extra-certs"]; ok {
		path, err := write("extra-certs.crt", body)
		if err != nil {
			return err
		}
		r.ExtraDirectives = appendDirective(r.ExtraDirectives, "extra-certs "+path)
	}
	if body, ok := r.Inline["tls-crypt-v2"]; ok {
		path, err := write("tls-crypt-v2.key", body)
		if err != nil {
			return err
		}
		r.ExtraDirectives = appendDirective(r.ExtraDirectives, "tls-crypt-v2 "+path)
	}

	r.Inline = nil
	r.clearInlineWarnings()
	return nil
}

// HasInline reports whether Parse found material that still needs Materialize.
func (r Result) HasInline() bool {
	return len(r.Inline) > 0
}

func (r *Result) clearInlineWarnings() {
	if len(r.Warnings) == 0 {
		return
	}
	out := r.Warnings[:0]
	for _, w := range r.Warnings {
		if strings.Contains(w, "inline <") {
			continue
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		r.Warnings = nil
		return
	}
	r.Warnings = out
}

func appendDirective(extra, line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return extra
	}
	if strings.TrimSpace(extra) == "" {
		return line + "\n"
	}
	return strings.TrimRight(extra, "\n") + "\n" + line + "\n"
}

// inlineWarning formats the Parse-time notice for an unmaterialized block.
func inlineWarning(tag string) string {
	return fmt.Sprintf("inline <%s> block present; will be written on create (or call Materialize)", tag)
}
