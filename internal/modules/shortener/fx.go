package shortener

import (
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"go.uber.org/fx"
)

// Module owns link management and public resolution as they are implemented.
var Module = fx.Module("shortener", fx.Provide(
	fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
	fx.Annotate(provideMigrationFileSystem, fx.ResultTags(`group:"migration_filesystems"`)),
))
