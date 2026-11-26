package plugin

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Secrets are the user's site credentials, kept in secrets.toml in the data
// directory (readable by the owner only), never in the database or logs:
//
//	[safebooru]
//	api_key = "..."
//	user_id = "..."
type Secrets map[string]map[string]string

// LoadSecrets reads the file; a missing file is empty.
func LoadSecrets(path string) (Secrets, error) {
	s := Secrets{}
	_, err := toml.DecodeFile(path, &s)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	return s, err
}

// Save writes the file atomically with owner-only permissions.
func (s Secrets) Save(path string) error {
	var buf bytes.Buffer
	buf.WriteString("# Site credentials for Vitrine. Keep this file private.\n")
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secrets-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	// CreateTemp already makes it 0600 on Unix; Windows has no Unix modes
	// (the user's profile folder ACLs keep it private there).
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
