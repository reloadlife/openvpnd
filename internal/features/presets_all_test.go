package features_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
)

// TestEveryBuiltinPresetExpands ensures each shipped feature_set ID produces conf material.
func TestEveryBuiltinPresetExpands(t *testing.T) {
	require.NotEmpty(t, features.Builtin)
	for _, p := range features.Builtin {
		t.Run(p.ID, func(t *testing.T) {
			extra, plugins, env := features.Expand([]string{p.ID}, nil, "", nil, nil)
			// udp_stuffing is comment-only template; others should emit directives or be non-empty notes
			if p.ExtraDirectives != "" {
				require.NotEmpty(t, extra, "preset %s should expand extra", p.ID)
				require.Contains(t, extra, "# feature:"+p.ID)
			}
			_ = plugins
			_ = env
		})
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
