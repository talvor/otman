package config_test

import (
	"errors"
	"testing"

	"github.com/talvor/otman/internal/config"
)

// These tests sit below the cli.Run seam only because no command takes @me
// yet. Once one does (assignee filters, claim), cover @me through golden
// cases instead and delete this file.
func TestResolveUserMe(t *testing.T) {
	file, err := config.Load(t.TempDir() + "/missing.toml")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   config.Inputs
		want string
	}{
		{"flag", config.Inputs{Flags: map[string]string{"actor": "flag-actor"}, Env: map[string]string{"OTM_ACTOR": "env-actor"}}, "flag-actor"},
		{"env", config.Inputs{Env: map[string]string{"OTM_ACTOR": "env-actor"}}, "env-actor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.File = file
			got, err := config.Resolve(c.in).ResolveUser("@me")
			if err != nil || got != c.want {
				t.Fatalf("ResolveUser(@me) = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestResolveUserMeFromConfig(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	if _, _, err := config.Set(path, config.Actor, "config-actor"); err != nil {
		t.Fatal(err)
	}
	file, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.Resolve(config.Inputs{File: file}).ResolveUser("@me")
	if err != nil || got != "config-actor" {
		t.Fatalf("ResolveUser(@me) = %q, %v; want config-actor", got, err)
	}
}

// With no actor anywhere, @me fails: there is no git or OS identity
// fallback, even when USER and git-style variables are present.
func TestResolveUserMeWithoutActor(t *testing.T) {
	env := map[string]string{"USER": "os-user", "GIT_AUTHOR_NAME": "git-user", "LOGNAME": "os-user"}
	_, err := config.Resolve(config.Inputs{Env: env}).ResolveUser("@me")
	if !errors.Is(err, config.ErrNoActor) {
		t.Fatalf("ResolveUser(@me) error = %v; want ErrNoActor", err)
	}
}

func TestResolveUserPassesNamesThrough(t *testing.T) {
	got, err := config.Resolve(config.Inputs{}).ResolveUser("alice")
	if err != nil || got != "alice" {
		t.Fatalf("ResolveUser(alice) = %q, %v; want alice", got, err)
	}
}
