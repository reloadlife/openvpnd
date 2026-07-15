package features_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
)

// TestEveryBuiltinPresetExpands ensures each shipped feature_set ID produces conf material.
func TestEveryBuiltinPresetExpands(t *testing.T) {
	require.NotEmpty(t, features.Builtin)
	// Known builtin IDs (guards against accidental rename/drop).
	wantIDs := map[string]bool{
		"explicit_exit_notify": true,
		"mssfix":               true,
		"verb_4":               true,
		"fast_io":              true,
		"udp_stuffing":         true,
		"udp_stuffing_env":     true,
		"auth_script_template": true,
		"tls_modern":           true,
		"comp_lzo_no":          true,
	}
	gotIDs := map[string]bool{}
	for _, p := range features.Builtin {
		gotIDs[p.ID] = true
		t.Run(p.ID, func(t *testing.T) {
			extra, plugins, env := features.Expand([]string{p.ID}, nil, "", nil, nil)
			// Every builtin should contribute conf comments and/or plugins/env.
			if p.ExtraDirectives != "" {
				require.NotEmpty(t, extra, "preset %s should expand extra", p.ID)
				require.Contains(t, extra, "# feature:"+p.ID)
			}
			if len(p.EnvVars) > 0 {
				require.NotEmpty(t, env, "preset %s should expand env", p.ID)
			}
			if len(p.Plugins) > 0 {
				require.NotEmpty(t, plugins, "preset %s should expand plugins", p.ID)
			}
			// Practical presets: at least one of extra/env/plugins non-empty.
			require.True(t,
				strings.TrimSpace(extra) != "" || len(env) > 0 || len(plugins) > 0,
				"preset %s expanded to nothing", p.ID,
			)
		})
	}
	for id := range wantIDs {
		require.True(t, gotIDs[id], "missing builtin preset %s", id)
	}
}

func TestExpandUnknownPresetComment(t *testing.T) {
	extra, _, _ := features.Expand([]string{"no_such_feature"}, nil, "", nil, nil)
	require.Contains(t, extra, "unknown feature_set: no_such_feature")
}

func TestExpandDedupePluginsAndEnv(t *testing.T) {
	extra, plugins, env := features.Expand(
		[]string{"mssfix"},
		[]db.FeaturePreset{{
			ID: "mssfix", // custom override
			ExtraDirectives: "mssfix\n# custom\n",
			Plugins: []db.Plugin{
				{Path: "/opt/a.so", Args: []string{"x"}},
				{Path: "/opt/a.so", Args: []string{"x"}},
			},
			EnvVars: []db.EnvVar{{Name: "A", Value: "1"}, {Name: "A", Value: "2"}},
		}},
		"base-extra\n",
		[]db.Plugin{{Path: "/opt/a.so", Args: []string{"x"}}},
		[]db.EnvVar{{Name: "B", Value: "b"}},
	)
	require.Contains(t, extra, "base-extra")
	require.Contains(t, extra, "mssfix")
	// one plugin path+args
	require.Len(t, plugins, 1)
	// env A last-wins + B
	names := map[string]string{}
	for _, e := range env {
		names[e.Name] = e.Value
	}
	require.Equal(t, "2", names["A"])
	require.Equal(t, "b", names["B"])
}

func TestListMergedCustomOverridesBuiltin(t *testing.T) {
	merged := features.ListMerged([]db.FeaturePreset{{
		ID: "mssfix", Description: "custom mssfix", ExtraDirectives: "mssfix\n",
	}})
	var found bool
	for _, p := range merged {
		if p.ID == "mssfix" {
			found = true
			require.Equal(t, "custom mssfix", p.Description)
		}
	}
	require.True(t, found)
}
