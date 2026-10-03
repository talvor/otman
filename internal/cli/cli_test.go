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

// config show sources in every format: human by default on a TTY, AXI by
// default when piped, and --format overriding either.
func TestConfigShowFormats(t *testing.T) {
	env := map[string]string{"OTM_ACTOR": "env-actor"}
	runGolden(t, goldenCase{
		name:   "config-show-formats",
		config: "vault = \"/from/config\"\n",
		steps: []step{
			{args: []string{"config", "show"}, env: env, tty: true},
			{args: []string{"config", "show"}, env: env},
			{args: []string{"config", "show", "--format", "human"}, env: env},
			{args: []string{"config", "show", "--format", "axi"}, env: env, tty: true},
			{args: []string{"config", "show", "--format", "json", "--json"}, env: env},
		},
	})
}

func TestConfigSetFormats(t *testing.T) {
	runGolden(t, goldenCase{
		name: "config-set-formats",
		steps: []step{
			{args: []string{"config", "set", "project", "OTM"}, tty: true},
			{args: []string{"config", "set", "project", "OTM"}},
		},
	})
}

// A representative failure in every format: nonzero exit, empty stdout,
// diagnostic on stderr, and nothing written.
func TestErrorFormats(t *testing.T) {
	args := []string{"config", "set", "colour", "blue"}
	runGolden(t, goldenCase{
		name: "error-formats",
		steps: []step{
			{args: args, tty: true},
			{args: args},
			{args: append(args, "--json")},
		},
	})
}

func TestFormatFlagErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name: "error-format-flags",
		steps: []step{
			{args: []string{"config", "show", "--json", "--format", "axi"}},
			{args: []string{"config", "show", "--format", "xml"}},
			{args: []string{"bogus", "--json"}},
			{args: []string{"config", "show", "--nope", "--format=json"}},
		},
	})
}

func TestInvalidConfigFile(t *testing.T) {
	runGolden(t, goldenCase{
		name:   "error-invalid-config",
		config: "vault = [unterminated\n",
		steps: []step{
			{args: []string{"config", "show", "--json"}},
			{args: []string{"--version"}},
		},
	})
}

func TestHelpWithoutConfig(t *testing.T) {
	runGolden(t, goldenCase{
		name:  "help",
		steps: []step{{args: []string{"--help"}}},
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
