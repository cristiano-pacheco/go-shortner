# Glossário

Vocabulário do projeto. Os identificadores em inglês correspondem ao código; o [glossary map](GLOSSARY-MAP.md) aponta onde investigar cada conceito.

## Domínio

| Termo | Significado no projeto |
| --- | --- |
| Link (`LinkModel`) | Registro que associa um código a uma URL de destino, com estado e datas de ciclo de vida. |
| ID (`ID`) | Identificador numérico interno do link, usado na consulta administrativa. É distinto do código público. |
| Código (`Code`) | Identificador público único do link. Pode ser gerado ou escolhido por alias; ambos ocupam o mesmo espaço de códigos. |
| Alias (`Alias`) | Código escolhido pelo cliente na criação. É uma entrada opcional que se torna `Code`, sem campo persistido separado. |
| Destino (`Destination`) | URL HTTP ou HTTPS armazenada no link. |
| URL curta (`ShortURL`) | Valor de saída composto pela base pública e pelo código. É calculado, não persistido; sua presença na resposta não comprova uma rota de redirecionamento implementada. |
| Base pública (`PublicBaseURL`) | Base configurada para compor a URL curta, distinta do endereço de escuta do servidor (`HTTPAddr`). |
| Ativo (`Active`) | Estado explícito do link. É independente da expiração e da exclusão lógica; isoladamente não determina se o link é utilizável. |
| Expiração (`ExpiresAt`) | Data opcional de vencimento. Ausência significa sem vencimento definido; a criação exige uma data futura quando informada. |
| Exclusão lógica (`DeletedAt`) | Marca que retira o registro das consultas do repositório, preservando a linha e a reserva do código único. |
| Alterações parciais (`LinkChanges`) | Campos opcionais de atualização. `SetExpiresAt` distingue manter a expiração de defini-la ou removê-la. |
| Conflito de código (`ErrCodeConflict`) | Código já reservado. Na criação, um código automático pode ser regenerado; um alias em conflito retorna erro ao cliente. |
| Link não encontrado (`ErrLinkNotFound`) | Ausência de um registro visível ao repositório, inclusive quando excluído logicamente. |
| Paginação (`Page`, `PageSize`, `Total`) | Página baseada em 1, quantidade por página e total de registros não excluídos. Links inativos e expirados continuam na listagem administrativa. |

## Fronteiras da aplicação

| Termo | Significado no projeto |
| --- | --- |
| API de gestão (`ManagementRoute`) | Rotas sob `/api/v1`, protegidas pela chave global enviada em `X-Api-Key`. |
| Rota pública (`PublicRoute`) | Registro de rotas na raiz, fora da autenticação da API de gestão. O tipo define uma extensão do servidor, não uma funcionalidade de redirecionamento por si só. |
| Caso de uso (`usecase`) | Operação da aplicação que valida entradas e coordena portas, retornando estruturas de entrada/saída independentes de HTTP. |
| Porta (`port`) | Contrato consumido pelos casos de uso: persistência de links ou geração de códigos. |
| Repositório (`repository`) | Adaptador GORM/PostgreSQL que implementa a porta de persistência e traduz erros de armazenamento em erros do módulo. |
| Composição (`fx`) | Registro de construtores, vínculos entre portas e implementações e hooks de ciclo de vida da aplicação. |
