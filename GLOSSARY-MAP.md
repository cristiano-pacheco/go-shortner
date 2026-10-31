# Glossary map

Mapa do [vocabulário](GLOSSARY.md) para suas fontes no código. Use a linha correspondente à tarefa; as definições ficam no glossário e as regras executáveis nas fontes vinculadas.

Os caminhos do módulo abaixo são relativos a [`internal/modules/shortener/`](internal/modules/shortener/).

## Domínio e operações

| Conceito ou tarefa | Fontes a consultar |
| --- | --- |
| Link, ID, código, destino e estado | [`model/link_model.go`](internal/modules/shortener/model/link_model.go) (`LinkModel`); [`migration/20261030000100_create_links.up.sql`](internal/modules/shortener/migration/20261030000100_create_links.up.sql) para restrições e índices. |
| Criar link, validar alias e expiração, tratar colisões | [`usecase/link_create_usecase.go`](internal/modules/shortener/usecase/link_create_usecase.go) (`LinkCreateUseCase`). |
| Gerar código automático | [`port/code_generator.go`](internal/modules/shortener/port/code_generator.go) para o contrato; [`service/code_generator_service.go`](internal/modules/shortener/service/code_generator_service.go) para a implementação. |
| Consultar link por ID | [`usecase/link_get_usecase.go`](internal/modules/shortener/usecase/link_get_usecase.go) (`LinkGetUseCase`). |
| Listar links e paginar | [`usecase/link_list_usecase.go`](internal/modules/shortener/usecase/link_list_usecase.go) para limites e saída; [`repository/link_repository.go`](internal/modules/shortener/repository/link_repository.go) (`List`) para filtro, ordenação e contagem. |
| Compor URL curta | Casos de uso de [criação](internal/modules/shortener/usecase/link_create_usecase.go), [consulta](internal/modules/shortener/usecase/link_get_usecase.go) e [listagem](internal/modules/shortener/usecase/link_list_usecase.go); [`Config.PublicBaseURL`](internal/shared/config/config.go). |
| Persistir, atualizar ou excluir logicamente | [`port/link_repository.go`](internal/modules/shortener/port/link_repository.go) para o contrato; [`repository/link_repository.go`](internal/modules/shortener/repository/link_repository.go) para filtros e mutações; [`model/link_model.go`](internal/modules/shortener/model/link_model.go) (`LinkChanges`) para alterações parciais. |
| Identificar erros de domínio | [`errs/shortener_error.go`](internal/modules/shortener/errs/shortener_error.go); [`http/chi/handler/link_handler.go`](internal/modules/shortener/http/chi/handler/link_handler.go) para tradução em respostas HTTP. |

## Transporte e composição

| Conceito ou tarefa | Fontes a consultar |
| --- | --- |
| Localizar operações HTTP implementadas | [`http/chi/router/link_router.go`](internal/modules/shortener/http/chi/router/link_router.go) para rotas do módulo; [`internal/shared/httpserver/server.go`](internal/shared/httpserver/server.go) para prefixos e rotas públicas. |
| Ler contrato JSON e parâmetros de consulta | [`http/dto/link_dto.go`](internal/modules/shortener/http/dto/link_dto.go) e [`http/chi/handler/link_handler.go`](internal/modules/shortener/http/chi/handler/link_handler.go). |
| Autenticar API de gestão | [`internal/shared/httpserver/api_key_middleware.go`](internal/shared/httpserver/api_key_middleware.go) (`APIKeyMiddleware`). |
| Configurar base pública e endereço de escuta | [`internal/shared/config/config.go`](internal/shared/config/config.go) (`Config`, `Load`). |
| Rastrear composição e recursos compartilhados | [`fx.go`](internal/modules/shortener/fx.go) no módulo; [`cmd/root.go`](cmd/root.go) no bootstrap; [`internal/shared/infrastructure/resources.go`](internal/shared/infrastructure/resources.go) para recursos externos. |

## Evidências e procedimentos

- Fluxos HTTP ponta a ponta: [`test/e2e/modules/shortner/`](test/e2e/modules/shortner/).
- Comandos e pré-requisitos: [guia de Make](docs/agents/make-commands.md).
- Issues e triagem: [issue tracker](docs/agents/issue-tracker.md) e [labels](docs/agents/triage-labels.md).
