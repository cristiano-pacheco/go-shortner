package migration

import (
	"fmt"
	"io/fs"
	"strings"

	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	"go.uber.org/fx"
)

type Params struct {
	fx.In
	Files []bricksmigration.FileSystem `group:"migration_filesystems"`
}

func NewRunner(params Params) (*bricksmigration.Runner, error) {
	seen := make(map[string]bool)
	for _, filesystem := range params.Files {
		entries, err := fs.ReadDir(filesystem.FS, ".")
		if err != nil {
			return nil, fmt.Errorf("read migration files: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if seen[name] {
				return nil, fmt.Errorf("duplicate migration file: %s", name)
			}
			seen[name] = true
		}
	}
	// A version must identify one migration across all module contributions.
	versions := make(map[string]string)
	for name := range seen {
		base := strings.TrimSuffix(strings.TrimSuffix(name, ".up.sql"), ".down.sql")
		version, _, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename: %s", name)
		}
		if previous, exists := versions[version]; exists && previous != base {
			return nil, fmt.Errorf("duplicate migration version: %s", version)
		}
		versions[version] = base
	}
	return bricksmigration.NewRunner(params.Files), nil
}
