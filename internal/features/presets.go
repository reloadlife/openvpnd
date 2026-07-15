// Package features expands named OpenVPN extension presets into plugins,
// env vars, and extra conf directives (for custom builds / plugins).
package features

import (
	"strings"

	"github.com/reloadlife/openvpnd/internal/db"
)

// Builtin presets that ship with openvpnd. Custom presets live in SQLite.
var Builtin = []db.FeaturePreset{
	{
		ID:          "explicit_exit_notify",
		Description: "UDP clients: notify server on exit (reconnect-friendly)",
		ExtraDirectives: "explicit-exit-notify 1\n",
		Builtin: true,
		Notes:   "Useful on UDP client instances",
	},
	{
		ID:          "mssfix",
		Description: "Clamp TCP MSS to path MTU",
		ExtraDirectives: "mssfix\n",
		Builtin: true,
	},
	{
		ID:          "verb_4",
		Description: "Increase log verbosity",
		ExtraDirectives: "verb 4\n",
		Builtin: true,
	},
	{
		ID:          "fast_io",
		Description: "Optimize for high throughput (platform-dependent)",
		ExtraDirectives: "fast-io\n",
		Builtin: true,
	},
	{
		ID:          "udp_stuffing",
		Description: "UDP stuffing / obfuscation recipe (custom OpenVPN binary required)",
		// Directives stay commented so stock openvpn still starts; uncomment after
		// registering a forked binary and confirming the fork's option names.
		ExtraDirectives: "# UDP stuffing — REQUIRES binary_name pinned to a custom/forked openvpn\n" +
			"# (register via POST /v1/binaries, then set instance.binary_name). Stock\n" +
			"# openvpn will reject un-commented fork options.\n" +
			"#\n" +
			"# Common fork-style options (names vary by patch — adjust to match yours):\n" +
			"# stuffing-enable\n" +
			"# stuffing-size 128\n" +
			"# stuffing-interval 0\n" +
			"# obfuscate-enable\n" +
			"#\n" +
			"# Optional plugin path (prefer instance.plugins[] or a custom preset):\n" +
			"# plugin /opt/openvpn-stuffing/lib/stuffing.so mode=1\n",
		Builtin: true,
		Notes:   "binary_name must be a custom OpenVPN fork; uncomment ExtraDirectives to match your fork's ABI. Pair with udp_stuffing_env for process env.",
	},
	{
		ID:          "udp_stuffing_env",
		Description: "Env-driven UDP stuffing toggle for forks that read STUFFING_*",
		EnvVars: []db.EnvVar{
			{Name: "STUFFING_ENABLE", Value: "1"},
		},
		ExtraDirectives: "# process env: STUFFING_ENABLE=1 (applied to supervised openvpn, not conf)\n" +
			"# binary_name must be a custom OpenVPN that honors STUFFING_* env vars.\n",
		Builtin: true,
		Notes:   "Sets STUFFING_ENABLE=1 on the supervised openvpn process. Requires custom binary_name; combine with udp_stuffing for conf comments.",
	},
	{
		ID:          "auth_script_template",
		Description: "Server-side username/password verify via external script",
		// Path is an example — operators must install their own verifier.
		ExtraDirectives: "script-security 2\n" +
			"# auth-user-pass-verify path is an EXAMPLE — install your script and adjust:\n" +
			"auth-user-pass-verify /usr/local/libexec/openvpnd-auth.sh via-env\n",
		Builtin: true,
		Notes:   "Example path /usr/local/libexec/openvpnd-auth.sh — replace with a real verifier. script-security 2 required for via-env.",
	},
	{
		ID:          "tls_modern",
		Description: "Modern TLS floor: TLS 1.2+ and preferred groups",
		// tls-version-min may also be set via instance.tls_version_min; avoid enabling both
		// unless values match. tls-groups is preset-only until typed fields land.
		ExtraDirectives: "tls-version-min 1.2\n" +
			"tls-groups X25519:P-256\n",
		Builtin: true,
		Notes:   "Prefer this preset OR instance.tls_version_min, not conflicting duplicates. tls-groups needs a recent OpenVPN/OpenSSL.",
	},
	{
		ID:          "comp_lzo_no",
		Description: "Disable LZO compression negotiate (modern peers)",
		ExtraDirectives: "comp-lzo no\npush \"comp-lzo no\"\n",
		Builtin: true,
	},
}

// Expand merges instance feature_sets + plugins + env + extra with preset definitions.
// custom presets override/extend builtins by ID.
func Expand(featureIDs []string, custom []db.FeaturePreset, baseExtra string, basePlugins []db.Plugin, baseEnv []db.EnvVar) (extra string, plugins []db.Plugin, env []db.EnvVar) {
	byID := map[string]db.FeaturePreset{}
	for _, p := range Builtin {
		byID[p.ID] = p
	}
	for _, p := range custom {
		byID[p.ID] = p
	}

	var extraParts []string
	if strings.TrimSpace(baseExtra) != "" {
		extraParts = append(extraParts, strings.TrimSpace(baseExtra))
	}
	plugins = append([]db.Plugin{}, basePlugins...)
	env = append([]db.EnvVar{}, baseEnv...)

	for _, id := range featureIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		p, ok := byID[id]
		if !ok {
			// unknown preset → comment so conf shows what was requested
			extraParts = append(extraParts, "# unknown feature_set: "+id)
			continue
		}
		if strings.TrimSpace(p.ExtraDirectives) != "" {
			extraParts = append(extraParts, "# feature:"+p.ID+"\n"+strings.TrimSpace(p.ExtraDirectives))
		}
		plugins = append(plugins, p.Plugins...)
		env = append(env, p.EnvVars...)
	}

	// de-dupe plugins by path+args
	plugins = dedupePlugins(plugins)
	env = dedupeEnv(env)

	return strings.Join(extraParts, "\n\n"), plugins, env
}

// ListIDs returns builtin + custom preset IDs with descriptions.
func ListMerged(custom []db.FeaturePreset) []db.FeaturePreset {
	seen := map[string]struct{}{}
	var out []db.FeaturePreset
	for _, p := range Builtin {
		out = append(out, p)
		seen[p.ID] = struct{}{}
	}
	for _, p := range custom {
		if _, ok := seen[p.ID]; ok {
			// custom overrides builtin in list (replace)
			for i := range out {
				if out[i].ID == p.ID {
					out[i] = p
					break
				}
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

func dedupePlugins(in []db.Plugin) []db.Plugin {
	type key struct{ p, a string }
	seen := map[key]struct{}{}
	var out []db.Plugin
	for _, pl := range in {
		if pl.Path == "" {
			continue
		}
		k := key{p: pl.Path, a: strings.Join(pl.Args, "\x00")}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, pl)
	}
	return out
}

func dedupeEnv(in []db.EnvVar) []db.EnvVar {
	seen := map[string]string{}
	for _, e := range in {
		if e.Name == "" {
			continue
		}
		seen[e.Name] = e.Value
	}
	var out []db.EnvVar
	for k, v := range seen {
		out = append(out, db.EnvVar{Name: k, Value: v})
	}
	return out
}
