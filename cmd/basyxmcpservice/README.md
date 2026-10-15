# BaSyx MCP service

This local stdio MCP service exposes documentation and the operations in the
checkout's `cmd/*/openapi.yaml` contracts. It uses the official Go MCP SDK and
does not need an LLM, a database, or a running BaSyx instance for documentation
and operation discovery. Live calls need a configured BaSyx target.

## Start

From the repository root:

```sh
go run ./cmd/basyxmcpservice -config ./cmd/basyxmcpservice/config.yaml
```

The process speaks MCP over stdin/stdout; logs go to stderr. It waits for an MCP
client, so running it in a terminal does not open a chat interface. An example
for interactive inspection is:

```sh
go build -o basyx-mcp ./cmd/basyxmcpservice
npx @modelcontextprotocol/inspector ./basyx-mcp -repository /absolute/path/to/basyx-go-components
```

On Windows, build with `-o basyx-mcp.exe` and use `./basyx-mcp.exe` in the Inspector
command. Keep generated binaries outside the working tree when preparing a PR.

Configure an MCP host with the absolute binary path and arguments. This generic
configuration shape is supported by hosts that use `mcpServers`; consult the
host's configuration documentation:

```json
{
  "mcpServers": {
    "basyx": {
      "command": "/absolute/path/to/basyx-mcp",
      "args": [
        "-config", "/absolute/path/to/basyx-go-components/cmd/basyxmcpservice/config.yaml",
        "-repository", "/absolute/path/to/basyx-go-components"
      ]
    }
  }
}
```

`-repository` overrides the configured source directory. Otherwise `repository`
is relative to the process working directory. Set `sourceRevision` to the
matching release or commit for attributable documentation results; the default
`workspace` deliberately does not claim a version. Restart after changing the
documentation or contracts: the service snapshots them at startup.

## Tools

| Tool | Purpose |
| --- | --- |
| `search_docs` | Search Markdown in `docu/`, `examples/`, this service directory, and root README, CONTRIBUTING and CHANGELOG files. Returns bounded excerpts and line numbers. |
| `get_doc` | Read indexed documents in pages of at most 200 lines. |
| `search_api_operations` | Search or list all operations; filter by component and paginate with offset/limit. |
| `get_api_operation` | Inspect an operation's parameters, body and responses. Use `schema_ref` to read referenced schemas; `schema_names` lists their names. |
| `call_api` | Execute an operation against an explicitly configured environment/component. |

All identifiers are names from the checked-out contracts. Component names match
service directory names, for example `submodelrepositoryservice`.

Example `call_api` arguments:

```json
{
  "environment": "demo",
  "component": "submodelrepositoryservice",
  "operation_id": "GetSubmodelById",
  "path_params": { "submodelIdentifier": "urn:example:submodel:1" }
}
```

Path identifiers described as base64 in the contract are encoded automatically:
pass their original UTF-8 identifiers. Other path values, including idShortPath,
are URL-escaped; path separators, percent escapes and traversal segments are
rejected. Query parameters are a map of string arrays and use the contract's
wire encoding, including any base64url encoding. For example:

```json
{ "query_params": { "limit": ["10"], "cursor": ["returned-cursor"] } }
```

Use `body` for JSON. Other media types require a string body and `content_type`,
or a standard-base64 `body_base64` for binary content. Multipart uploads use
`content_type: "multipart/form-data"`, optional string fields in `body`, and
`files` entries containing `filename`, `content_type` and `base64`. OpenAPI
request validation runs before sending a request. The generic tools do not
automatically interpret every deployment-specific extension or construct
semantic references from natural-language identifiers.

Responses preserve the HTTP status, selected headers (including ETag), and
either parsed JSON/text in `body` or binary data in `body_base64`. HTTP 4xx/5xx
sets the MCP result's `isError`. Redirects are returned without being followed.
Responses above `maxResponseBytes` fail explicitly; they are not silently
truncated. The same limit bounds encoded request bodies. Paginate large lists.

## Targets and security

The sample configuration enables reads from a local Submodel Repository. Adjust
the URL to the actual API root, including its context path. Add other component
names under an environment to make their APIs callable. Discovery includes
unconfigured components, while calls require an explicit target.

`allowWrites: false` permits GET, HEAD and OPTIONS only. Set it to true for a
target to enable other methods, including POST-based queries and operations.
The server does not automatically retry API calls.

`tokenEnv` names a process environment variable containing a BaSyx access token.
When configured, an empty variable fails the call instead of falling back to an
anonymous request. Do not put secrets into the YAML or pass Authorization headers
through tools. Bearer credentials are sent only to the configured target;
redirect following and automatic HTTP proxy environment settings are disabled.
Use HTTPS for remote targets. BaSyx remains responsible for OIDC/ABAC enforcement.

This is a local, single-user stdio adapter operating with its configured backend
identity. It is not a multi-user HTTP gateway and does not exchange MCP access
tokens for delegated BaSyx credentials. Do not expose it through an unauthenticated
network-to-stdio bridge. Loaded contracts and documentation must come from a
trusted checkout. External schema URLs are rejected; the repository's shared
Part 1/Part 2 schema aliases resolve to its local V3.2.0 copies. Deployment-time
Swagger additions (such as conditional security-management or event endpoints)
are outside the checked-in contract catalog.

Markdown files above 256 KiB and symbolic links are not indexed. Indexed paths
are the only documents `get_doc` can return. No general filesystem, shell or
arbitrary-URL tool is exposed.

## Skills

The repository includes `.agents/skills/basyx-advisor/SKILL.md` and
`.agents/skills/basyx-api/SKILL.md`. The advisor answers documentation questions
and gives sourced feedback. The API skill discovers and uses the entire catalog
through the three general API tools. A connected MCP server is required for
their tool workflows. Skill support and discovery depend on the chosen host.

## Verification

```sh
go clean -testcache
go test -v ./internal/mcpservice/... ./cmd/basyxmcpservice
go test -v ./internal/submodelrepository/integration_tests
```

The MCP integration tests connect an SDK client to the server and use local HTTP
fixtures. They cover repository contracts, documentation, schema validation,
credential injection, write restrictions, redirects, response bounds and uploads.
