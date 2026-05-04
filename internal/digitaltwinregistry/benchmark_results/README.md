# Digital Twin Registry Benchmark

This benchmark script seeds Digital Twin Registry data and executes weighted mixed traffic across all endpoints from `cmd/digitaltwinregistryservice/openapi.yaml`.

## What It Does

- Uses `internal/aasregistry/benchmark_results/bodies/testbench.json` as input templates (same idea as `insert_data.py`: deep copy templates, regenerate IDs, remove `createdAt`, POST to `/shell-descriptors`).
- Tracks generated shell IDs and submodel IDs for follow-up calls.
- Generates `specificAssetIds` with these rules:
  - one plain `name`/`value` entry
  - `0..3` entries with `externalSubjectId`
  - `0..1` `PUBLIC_READABLE` entry
  - `name/value` entries are drawn from a reusable global pool of 20 unique pairs
  - 10 distinct `Edc-Bpn` values are used in generated data
  - 50% of shells include `globalAssetId`
- Builds `/lookup/shellsByAssetLink` bodies from stored `name`/`value` pairs of generated shell links.
- Uses all DTR endpoints with parameter variants.
- Supports mixed caller identities:
  - anonymous (no token, no Edc-Bpn)
  - Edc-Bpn header caller
  - admin token caller (Keycloak password grant)
- All shell-descriptor write operations (`POST/PUT/DELETE /shell-descriptors`) use admin token access.
- For lookup-by-asset-link requests, link selection is biased toward auth-compatible links
  (`PUBLIC_READABLE` and/or matching `Edc-Bpn`) to increase chance of non-empty responses.
- All requests/endpoints that support a `limit` query parameter send `limit=1000` by default.
- Logs per request:
  - operation and variant
  - auth identity used
  - status code
  - runtime in ms
  - response size
  - parsed result length (if available)
  - optional response/request bodies

## Default Weight Distribution

- `post_shell_descriptors`: `10%`
- `put_shell_by_id`: `5%`
- `get_shell_by_id`: `10%`
- `get_lookup_shells_by_id`: `40%`
- remaining endpoints are distributed across the other `35%`

You can override weights with `--weights`.

## Run

From repository root:

```bash
python internal/digitaltwinregistry/benchmark_results/benchmark.py \
  --base-url http://127.0.0.1:5004 \
  --seed-shells 10 \
  --requests 100 \
  --output-jsonl internal/digitaltwinregistry/benchmark_results/runtime_results_dtr.jsonl \
  --summary-json internal/digitaltwinregistry/benchmark_results/runtime_summary_dtr.json \
  --coverage-once
```

By default, requests are sent to `/api/v3/*`.

For deployments without a context path:

```bash
python internal/digitaltwinregistry/benchmark_results/benchmark.py \
  --base-url http://127.0.0.1:6004 \
  --api-prefix ""
```

### Auth Defaults (Example Setup)

- token URL: `http://localhost:8080/realms/basyx/protocol/openid-connect/token`
- client: `basyx-ui`
- user: `admin`
- password: `pwd`

These defaults match the DTR integration test setup and are used automatically unless overridden.

### Optional Flags

- `--weights "post_shell_descriptors=10,put_shell_by_id=5,get_lookup_shells_by_id=40"`
- `--api-prefix /api/v3` (default) or `--api-prefix ""` (disable prefix)
- `--default-limit 1000` (applied to all limit-capable requests)
- `--unique-name-values 20` (global reusable name/value pool size)
- `--unique-bpns 10` (number of unique BPNs used)
- `--edc-header-mode random|always|never`
- `--lookup-result-bias 0.8` (0..1, higher means more auth-compatible lookup links)
- `--read-identity-weights "anonymous=20,edc_header=45,admin_token=35"`
- `--admin-token-url http://localhost:8080/realms/basyx/protocol/openid-connect/token`
- `--admin-client-id basyx-ui`
- `--admin-username admin`
- `--admin-password pwd`
- `--bearer-token <token>` (uses static token as admin token, skips token fetch)
- `--store-response-body`
- `--store-request-body`
- `--response-preview-chars 2000`

## Output

- JSONL request log: one line per request
- JSON summary with global, per-operation, and per-variant metrics

## Plot Cumulative Runtime per Operation

```bash
python internal/digitaltwinregistry/benchmark_results/plot_runtime_by_operation.py \
  internal/digitaltwinregistry/benchmark_results/runtime_results_dtr.jsonl \
  --output internal/digitaltwinregistry/benchmark_results/runtime_by_operation.png
```

- X-axis: operation ID
- Y-axis: cumulative runtime (ms) per operation
- Console output includes operation ID -> operation name mapping
