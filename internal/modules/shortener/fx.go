package shortener

import (
	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/cache"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/handler"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/router"
	shortenermigration "github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/migration"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/service"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
)

var persistenceModule = fx.Provide(
	fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
	fx.Annotate(cache.NewRedirectCache, fx.As(new(port.RedirectCache))),
)

var publicRoutesModule = fx.Provide(
	usecase.NewLinkResolveUseCase,
	handler.NewLinkRedirectHandler,
	router.NewLinkRedirectRouter,
	fx.Annotate((*router.LinkRedirectRouter).PublicRoute, fx.ResultTags(`group:"public_routes"`)),
)

var managementRoutesModule = fx.Provide(
	fx.Annotate(service.NewCodeGeneratorService, fx.As(new(port.CodeGenerator))),
	usecase.NewLinkCreateUseCase,
	usecase.NewLinkDeleteUseCase,
	usecase.NewLinkGetUseCase,
	usecase.NewLinkListUseCase,
	usecase.NewLinkUpdateUseCase,
	fx.Annotate(validator.NewDestinationValidator, fx.As(new(port.DestinationValidator))),
	handler.NewLinkHandler,
	router.NewLinkRouter,
	fx.Annotate((*router.LinkRouter).ManagementRoute, fx.ResultTags(`group:"management_routes"`)),
)

// PublicModule exposes only public link resolution and its adapters.
var PublicModule = fx.Module("shortener-public", persistenceModule, publicRoutesModule)

// ManagementModule exposes authenticated link management without public resolution.
var ManagementModule = fx.Module("shortener-management", persistenceModule, managementRoutesModule)

// MigrationModule publishes this module's owned schema without HTTP components.
var MigrationModule = fx.Module("shortener-migration", fx.Provide(
	fx.Annotate(func() bricksmigration.FileSystem {
		return bricksmigration.New(shortenermigration.Files)
	}, fx.ResultTags(`group:"migration_filesystems"`)),
))

// Module preserves the complete business graph for application-level integration.
// Server processes select PublicModule or ManagementModule instead.
var Module = fx.Module("shortener", persistenceModule, publicRoutesModule, managementRoutesModule, MigrationModule)
