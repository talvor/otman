package cli_test

import "testing"

func TestVersionWithoutConfig(t *testing.T) {
	runGolden(t, goldenCase{
		name:  "version",
		steps: []step{{args: []string{"--version"}}},
	})
}

const fullConfig = `vault = "/from/config"
actor = "config-actor"
project = "CFG"
`

// Flags beat OTM_* environment, which beats user config, key by key.
func TestConfigPrecedence(t *testing.T) {
	runGolden(t, goldenCase{
		name:   "config-precedence",
		config: fullConfig,
		steps: []step{
			{args: []string{"config", "show", "--json"}},
			{
				args: []string{"config", "show", "--json"},
				env:  map[string]string{"OTM_VAULT": "/from/env", "OTM_ACTOR": "env-actor", "OTM_PROJECT": "ENV"},
			},
			{
				args: []string{"config", "show", "--json", "--vault", "/from/flag", "--actor", "flag-actor", "--project", "FLG"},
				env:  map[string]string{"OTM_VAULT": "/from/env", "OTM_ACTOR": "env-actor", "OTM_PROJECT": "ENV"},
			},
			{
				args: []string{"config", "show", "--json", "--actor", "flag-actor"},
				env:  map[string]string{"OTM_PROJECT": "ENV"},
			},
		},
	})
}

// With no config file and no environment, every setting is unset, and a
// relative --vault is resolved against the working directory.
func TestConfigShowUnsetAndRelativeVault(t *testing.T) {
	runGolden(t, goldenCase{
		name: "config-show-unset",
		steps: []step{
			{args: []string{"config", "show", "--json"}},
			{args: []string{"config", "show", "--json", "--vault", "vault"}},
		},
	})
}

// Without XDG_CONFIG_HOME, config lives under $HOME/.config.
func TestConfigHomeFallback(t *testing.T) {
	runGolden(t, goldenCase{
		name: "config-home-fallback",
		steps: []step{
			{args: []string{"config", "set", "actor", "phillip", "--json"}, env: map[string]string{"XDG_CONFIG_HOME": ""}},
			{args: []string{"config", "show", "--json"}, env: map[string]string{"XDG_CONFIG_HOME": ""}},
		},
	})
}

func TestConfigSetThenShowJSON(t *testing.T) {
	runGolden(t, goldenCase{
		name: "config-set-show-json",
		steps: []step{
			{args: []string{"config", "set", "vault", "$WORK/vault", "--json"}},
			{args: []string{"config", "set", "actor", "phillip", "--json"}},
			{args: []string{"config", "set", "project", "OTM", "--json"}},
			{args: []string{"config", "show", "--json"}},
		},
	})
}
