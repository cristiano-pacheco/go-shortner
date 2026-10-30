package shortener

import (
	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	shortenermigration "github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/migration"
)

func provideMigrationFileSystem() bricksmigration.FileSystem {
	return bricksmigration.New(shortenermigration.Files)
}
