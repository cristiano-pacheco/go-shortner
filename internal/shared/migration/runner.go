package migration

import (
	"fmt"
	"io/fs"
	"strings"

	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	sharederrs "github.com/cristiano-pacheco/go-shortner/internal/shared/errs"
	"go.uber.org/fx"
)

type Params struct {
	fx.In

	Files []bricksmigration.FileSystem `group:"migration_filesystems"`
}

func NewRunner(params Params) (*bricksmigration.Runner, error) {
	seen := make(map[string]bool)

	for _, filesystem := range params.Files {
		if err := collectFiles(filesystem, seen); err != nil {
			return nil, err
		}
	}
	// A version must identify one migration across all module contributions.
	versions := make(map[string]string)

	for name := range seen {
		base := strings.TrimSuffix(strings.TrimSuffix(name, ".up.sql"), ".down.sql")

		version, _, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("%w: %s", sharederrs.ErrMigrationFilenameInvalid, name)
		}

		if previous, exists := versions[version]; exists && previous != base {
			return nil, fmt.Errorf("%w: %s", sharederrs.ErrMigrationVersionDuplicate, version)
		}

		versions[version] = base
	}

	return bricksmigration.NewRunner(params.Files), nil
}

func collectFiles(filesystem bricksmigration.FileSystem, seen map[string]bool) error {
	entries, err := fs.ReadDir(filesystem.FS, ".")
	if err != nil {
		return fmt.Errorf("%w: %w", sharederrs.ErrMigrationRead, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if seen[name] {
			return fmt.Errorf("%w: %s", sharederrs.ErrMigrationFileDuplicate, name)
		}

		seen[name] = true
	}

	return nil
}
