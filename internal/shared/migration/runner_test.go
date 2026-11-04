package migration_test

import (
	"testing"
	"testing/fstest"

	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	"github.com/stretchr/testify/require"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/migration"
)

func TestRunnerRejectsDuplicateMigrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		first   string
		second  string
		message string
	}{
		{"duplicate name", "001_create_links.up.sql", "001_create_links.up.sql", "duplicate migration file"},
		{"duplicate version", "001_create_links.up.sql", "001_create_users.up.sql", "duplicate migration version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			files := []bricksmigration.FileSystem{
				bricksmigration.New(fstest.MapFS{test.first: &fstest.MapFile{Data: []byte("SELECT 1;")}}),
				bricksmigration.New(fstest.MapFS{test.second: &fstest.MapFile{Data: []byte("SELECT 1;")}}),
			}
			_, err := migration.NewRunner(migration.Params{Files: files})
			require.ErrorContains(t, err, test.message)
		})
	}
}
