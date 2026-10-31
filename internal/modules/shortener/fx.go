package shortener

import (
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/handler"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/router"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/service"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
)

// Module owns link management and public resolution as they are implemented.
var Module = fx.Module("shortener", fx.Provide(
	fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
	fx.Annotate(service.NewCodeGeneratorService, fx.As(new(port.CodeGenerator))),
	usecase.NewLinkCreateUseCase,
	usecase.NewLinkDeleteUseCase,
	usecase.NewLinkGetUseCase,
	usecase.NewLinkListUseCase,
	usecase.NewLinkResolveUseCase,
	usecase.NewLinkUpdateUseCase,
	fx.Annotate(validator.NewDestinationValidator, fx.As(new(port.DestinationValidator))),
	handler.NewLinkHandler,
	handler.NewLinkRedirectHandler,
	router.NewLinkRouter,
	router.NewLinkRedirectRouter,
	fx.Annotate((*router.LinkRedirectRouter).PublicRoute, fx.ResultTags(`group:"public_routes"`)),
	fx.Annotate((*router.LinkRouter).ManagementRoute, fx.ResultTags(`group:"management_routes"`)),
	fx.Annotate(provideMigrationFileSystem, fx.ResultTags(`group:"migration_filesystems"`)),
))
