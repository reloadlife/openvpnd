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
		Description: "Template for UDP stuffing / obfuscation with a custom OpenVPN build",
		ExtraDirectives: "# UDP stuffing / custom fork options — set binary_name to your patched openvpn\n" +
			"# and plugins[] to your .so, then add fork-specific directives below.\n" +
			"# Example (replace with your fork's real options):\n" +
			"# stuffing-enable\n" +
			"# stuffing-size 100\n",
		Builtin: true,
		Notes:   "Requires a custom OpenVPN binary (multi-binary registry) and usually a plugin path",
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
