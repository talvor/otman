package cli_test

import "testing"

func TestVersionWithoutConfig(t *testing.T) {
	runGolden(t, goldenCase{
		name:  "version",
		steps: []step{{args: []string{"--version"}}},
	})
}
