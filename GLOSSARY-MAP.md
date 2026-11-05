# Glossary map

The [glossary](GLOSSARY.md) defines these terms. Use the links below to find their implementation.

## Domain and operations

| Concept or task | Sources |
| --- | --- |
| Link, ID, code, destination and lifecycle | [`LinkModel`](internal/modules/shortener/model/link_model.go), [schema and constraints](internal/modules/shortener/migration/20261030000100_create_links.up.sql). |
| Create links, validate aliases and expiration, handle collisions | [`LinkCreateUseCase`](internal/modules/shortener/usecase/link_create_usecase.go). |
| Generate codes | [Generator port](internal/modules/shortener/port/code_generator.go), [generator implementation](internal/modules/shortener/service/code_generator_service.go). |
| Retrieve and list links | [Get use case](internal/modules/shortener/usecase/link_get_usecase.go), [list use case](internal/modules/shortener/usecase/link_list_usecase.go), [repository](internal/modules/shortener/repository/link_repository.go). |
| Update and delete links | [Update use case](internal/modules/shortener/usecase/link_update_usecase.go), [delete use case](internal/modules/shortener/usecase/link_delete_usecase.go), [repository](internal/modules/shortener/repository/link_repository.go). |
| Resolve and cache redirects | [Resolve use case](internal/modules/shortener/usecase/link_resolve_usecase.go), [Redis adapter](internal/modules/shortener/cache/redirect_cache.go). |
| Domain errors | [Shortener errors](internal/modules/shortener/errs/shortener_error.go), [HTTP handler](internal/modules/shortener/http/chi/handler/link_handler.go). |

## Transport and composition

| Concept or task | Sources |
| --- | --- |
| HTTP routes and request contracts | [Router](internal/modules/shortener/http/chi/router/link_router.go), [server](internal/shared/httpserver/server.go), [DTOs](internal/modules/shortener/http/dto/link_dto.go). |
| Management authentication | [API key middleware](internal/shared/httpserver/api_key_middleware.go). |
| Configuration and public base URL | [Configuration](internal/shared/config/config.go). |
| Module composition and resources | [Shortener Fx module](internal/modules/shortener/fx.go), [server composition](cmd/server.go), [Bricks PostgreSQL and Redis modules](cmd/server.go). |
| Embedded migrations and execution | [Shortener Fx module](internal/modules/shortener/fx.go), [embedded SQL files](internal/modules/shortener/migration/migration.go), [migration runner](internal/shared/migration/runner.go), [migration command](cmd/migrate.go). |
| HTTP verification | [End-to-end tests](test/e2e/modules/shortner/), [Make commands](docs/agents/make-commands.md). |
