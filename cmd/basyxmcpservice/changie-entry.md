# Pending Changie entry

A PR number is required before creating the release fragment. Once the actual PR
exists, run `go run github.com/miniscruff/changie@v1.26.0 new` from the repository
root with the following values and that PR number:

- Kind: Added
- Body: Add a local BaSyx MCP service with sourced documentation search and discovery, schema inspection, and configurable execution of the checked-in component APIs, plus advisor and API skills.
- Impact: Low
- PullRequest: Use the actual PR number.
- SecurityImpact: The opt-in stdio service uses configured backend credentials and existing BaSyx permissions. Targets default to read-only; other methods require explicit configuration. Calls validate OpenAPI inputs, restrict targets and headers, bound payloads, and do not follow redirects.
