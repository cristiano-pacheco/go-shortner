# HTTP API

Management routes require `X-API-Key`. Set `API_KEY` in your shell to the value configured for the server. If you used `make setup`, you can load the generated local configuration with:

```sh
set -a; . ./.env; set +a
```

Keep credentials out of shell history and scripts. Examples use management port 8080 and public port 8081. Each server exposes only its own routes plus `/health`. Replace `1` with the returned link ID. IDs need not be consecutive.

## Create a link

Create a link with a custom alias. Omit `alias` to generate a code.

```sh
curl -i -X POST http://localhost:8080/api/v1/links \
  -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' \
  -d '{"destination":"https://example.com","alias":"example"}'
```

## Get a link

Retrieve a link by its management ID.

```sh
curl -i http://localhost:8080/api/v1/links/1 -H "X-API-Key: $API_KEY"
```

## List links

Retrieve the first page of links, including inactive and expired links.

```sh
curl -i 'http://localhost:8080/api/v1/links?page=1&page_size=20' -H "X-API-Key: $API_KEY"
```

## Update a link

Change the destination and clear the expiration without changing the code.

```sh
curl -i -X PATCH http://localhost:8080/api/v1/links/1 \
  -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' \
  -d '{"destination":"https://example.org","expires_at":null}'
```

## Follow a short link

Request the public URL without an API key. The response contains a `Location` header.

```sh
curl -i http://localhost:8081/example
```

## Inspect redirect headers

Use HEAD to retrieve the redirect headers without a response body.

```sh
curl -I http://localhost:8081/example
```

## Delete a link

Soft-delete a link by its management ID. Its code remains reserved.

```sh
curl -i -X DELETE http://localhost:8080/api/v1/links/1 -H "X-API-Key: $API_KEY"
```

## Request and response rules

Creation returns 201. Get, list and update return 200, and delete returns 204. Create, get, list and update responses contain `data`; link representations include `id`, `code`, `destination`, `short_url`, `active`, `expires_at`, `created_at` and `updated_at`. IDs are positive signed 64-bit integers. The configured public base URL, not the request Host header, determines `short_url`.

A destination must be an absolute HTTP or HTTPS URL with a hostname, without user credentials, and at most 4096 bytes. Localhost and private IP addresses are allowed. The service does not probe destinations or detect phishing. An alias has 3 to 32 ASCII letters, digits, hyphens or underscores. Codes are case-sensitive, and `api` and `health` are reserved. Generated codes have eight alphanumeric characters. Deleted codes remain reserved.

An optional `expires_at` is an RFC3339 timestamp in the future. PATCH accepts `destination`, `active` and `expires_at`; omitted fields remain unchanged, while `"expires_at": null` clears expiration. Set `active` to `false` or `true` to deactivate or reactivate. The code cannot be changed. List includes inactive and expired links, ordered newest first with an ID tie-breaker. Pages start at 1, default to 20 items and accept `page_size` from 1 to 100. The response includes `items`, `page`, `page_size` and `total`.

Public GET and HEAD requests need no key. Available links return 302, `Location` and `Cache-Control: no-store`; HEAD has no response body. Unknown, inactive, expired and deleted links return 404, subject to the [cache consistency bound](architecture.md#redirect-cache) after changes. Errors use English messages in an `error` object with `code` and `message`. Invalid input returns 400, missing or invalid keys 401, missing links 404, code conflicts 409, oversized bodies 413, non-JSON content types 415 and unexpected failures 500.
