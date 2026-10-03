package cli

import (
	"errors"
	"path/filepath"

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
	v, warnings, err := vault.Open(path, a.opts.LockTimeout)
	switch {
	case errors.Is(err, vault.ErrLockTimeout):
		timeout := a.opts.LockTimeout
		if timeout <= 0 {
			timeout = vault.DefaultLockTimeout
		}
		lock := filepath.Join(path, vault.StateDir, "lock")
		return nil, nil, &Error{Exit: ExitConflict, Code: "lock_timeout",
			Message: "another otman command held the Vault lock " + lock + " for more than " + timeout.String(),
			Details: map[string]any{"path": lock, "timeout": timeout.String()},
			Hint:    "retry once the other otman command finishes; if none is running, find the process holding " + lock}
	case errors.Is(err, vault.ErrNotFound):
		return nil, nil, invalid("vault_not_found", "Vault directory "+path+" does not exist", details,
			"create the directory, or point otman at your Vault with 'otman config set vault PATH'")
	case errors.Is(err, vault.ErrNotDir):
		return nil, nil, invalid("vault_not_directory", "Vault path "+path+" is not a directory", details,
			"point otman at your Vault directory with 'otman config set vault PATH'")
	case err != nil:
		return nil, nil, ioError(err)
	}
	return v, append(append([]output.Problem{}, s.Warnings...), warnings...), nil
}
