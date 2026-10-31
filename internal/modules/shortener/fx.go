package shortener

import (
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/handler"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/router"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/service"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"go.uber.org/fx"
)

// Module owns link management and public resolution as they are implemented.
var Module = fx.Module("shortener", fx.Provide(
	fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
	fx.Annotate(service.NewCodeGeneratorService, fx.As(new(port.CodeGenerator))),
	usecase.NewLinkCreateUseCase,
	handler.NewLinkHandler,
	router.NewLinkRouter,
	fx.Annotate((*router.LinkRouter).ManagementRoute, fx.ResultTags(`group:"management_routes"`)),
	fx.Annotate(provideMigrationFileSystem, fx.ResultTags(`group:"migration_filesystems"`)),
))
