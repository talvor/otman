package cli_test

import "testing"

func TestVersionWithoutConfig(t *testing.T) {
	runGolden(t, goldenCase{
		name:  "version",
		steps: []step{{args: []string{"--version"}}},
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
