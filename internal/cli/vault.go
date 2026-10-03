package cli

import (
	"errors"

	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// openVault opens the configured Vault, bootstrapping .otman/ on first
// use. There is deliberately no init command: the Vault directory must
// exist, and everything otman needs inside it is created here. The caller
// must Close the Vault, which releases the lock.
func (a *app) openVault(s resolved) (*vault.Vault, []output.Problem, error) {
	if !s.Vault.IsSet() {
		return nil, nil, invalid("no_vault", "no Vault configured", nil,
			"run 'otman config set vault PATH', or pass --vault or set "+config.Vault.EnvVar)
	}
	path := s.Vault.Value
	details := map[string]any{"path": path, "source": string(s.Vault.Source)}
	v, warnings, err := vault.Open(path)
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return nil, nil, invalid("vault_not_found", "Vault directory "+path+" does not exist", details,
			"create the directory, or point otman at your Vault with 'otman config set vault PATH'")
	case errors.Is(err, vault.ErrNotDir):
		return nil, nil, invalid("vault_not_directory", "Vault path "+path+" is not a directory", details,
			"point otman at your Vault directory with 'otman config set vault PATH'")
	case err != nil:
		return nil, nil, ioError(err)
	}
	return v, append(s.Warnings, warnings...), nil
}
