/*******************************************************************************
* Copyright (C) 2026 the Eclipse BaSyx Authors and Fraunhofer IESE
*
* Permission is hereby granted, free of charge, to any person obtaining
* a copy of this software and associated documentation files (the
* "Software"), to deal in the Software without restriction, including
* without limitation the rights to use, copy, modify, merge, publish,
* distribute, sublicense, and/or sell copies of the Software, and to
* permit persons to whom the Software is furnished to do so, subject to
* the following conditions:
*
* The above copyright notice and this permission notice shall be
* included in all copies or substantial portions of the Software.
*
* THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
* EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
* MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
* NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
* LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
* OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
* WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*
* SPDX-License-Identifier: MIT
******************************************************************************/

package mcpservice

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type catalog struct {
	operations map[string]*apiOperation
	ordered    []*apiOperation
	components map[string]bool
}

type apiOperation struct {
	OperationSummary
	spec     *openapi3.T
	pathItem *openapi3.PathItem
	value    *openapi3.Operation
}

// OperationSummary identifies an operation within one component contract.
type OperationSummary struct {
	Component   string `json:"component"`
	OperationID string `json:"operation_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Summary     string `json:"summary"`
	Version     string `json:"version"`
	Source      string `json:"source"`
}

type operationSearchInput struct {
	Query     string `json:"query,omitempty" jsonschema:"Search words, operation ID or API path; empty lists operations"`
	Component string `json:"component,omitempty" jsonschema:"Optional service directory name, such as submodelrepositoryservice"`
	Offset    int    `json:"offset,omitempty" jsonschema:"Zero-based result offset for pagination"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum matches from 1 to 100; defaults to 20"`
}

type operationSearchResult struct {
	Matches    []OperationSummary `json:"matches"`
	Total      int                `json:"total"`
	NextOffset *int               `json:"next_offset,omitempty"`
}

type operationInput struct {
	Component   string `json:"component" jsonschema:"Component returned by search_api_operations"`
	OperationID string `json:"operation_id" jsonschema:"Exact operation_id returned by search_api_operations"`
	SchemaRef   string `json:"schema_ref,omitempty" jsonschema:"Optional local schema reference from this contract, such as #/components/schemas/Submodel"`
}

type operationResult struct {
	OperationSummary
	Description string                        `json:"description"`
	Parameters  []*openapi3.Parameter         `json:"parameters"`
	RequestBody *openapi3.RequestBody         `json:"request_body,omitempty"`
	Responses   map[string]*openapi3.Response `json:"responses"`
	Schema      *openapi3.Schema              `json:"schema,omitempty"`
	SchemaNames []string                      `json:"schema_names,omitempty"`
}

func loadCatalog(ctx context.Context, source fs.FS) (*catalog, error) {
	files, err := fs.Glob(source, "cmd/*/openapi.yaml")
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("MCP-CATALOG-FILES: no service contracts found")
	}
	result := &catalog{operations: make(map[string]*apiOperation), components: make(map[string]bool)}
	for _, filename := range files {
		if err := result.load(ctx, source, filename); err != nil {
			return nil, err
		}
	}
	sort.Slice(result.ordered, func(i, j int) bool {
		return operationKey(result.ordered[i].Component, result.ordered[i].OperationID) <
			operationKey(result.ordered[j].Component, result.ordered[j].OperationID)
	})
	return result, nil
}

func (cat *catalog) load(ctx context.Context, source fs.FS, filename string) error {
	loader := openapi3.NewLoader()
	loader.Context = ctx
	loader.ReadFromURIFunc = func(_ *openapi3.Loader, location *url.URL) ([]byte, error) {
		return readContract(source, location)
	}
	spec, err := loader.LoadFromURI(&url.URL{Scheme: "basyx", Host: "repository", Path: "/" + filename})
	if err != nil {
		return fmt.Errorf("MCP-CATALOG-LOAD: %s: %w", filename, err)
	}
	if spec.Components == nil {
		spec.Components = &openapi3.Components{}
	}
	spec.InternalizeRefs(ctx, nil)
	component := path.Base(path.Dir(filename))
	cat.components[component] = true
	for route, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			if err := cat.add(spec, item, operation, OperationSummary{Component: component,
				OperationID: operation.OperationID, Method: method, Path: route, Summary: operation.Summary,
				Version: spec.Info.Version, Source: filename}); err != nil {
				return err
			}
		}
	}
	return nil
}

func readContract(source fs.FS, location *url.URL) ([]byte, error) {
	if location.Scheme != "basyx" || location.Host != "repository" {
		return nil, fmt.Errorf("MCP-CATALOG-REFERENCE: external schema locations are not allowed")
	}
	name := strings.TrimPrefix(location.Path, "/")
	switch {
	case strings.Contains(name, "/Part1-MetaModel-Schemas/"):
		name = "internal/common/swagger_part1_schemas/V3.2.0/openapi.yaml"
	case strings.Contains(name, "/Part2-API-Schemas/"):
		name = "internal/common/swagger_part2_schemas/V3.2.0/openapi.yaml"
	}
	if !fs.ValidPath(name) || path.Ext(name) != ".yaml" {
		return nil, fmt.Errorf("MCP-CATALOG-REFERENCE: invalid local schema location")
	}
	data, err := fs.ReadFile(source, name)
	if err != nil {
		return nil, fmt.Errorf("MCP-CATALOG-READ: %w", err)
	}
	return data, nil
}

func (cat *catalog) add(spec *openapi3.T, item *openapi3.PathItem, operation *openapi3.Operation, summary OperationSummary) error {
	key := operationKey(summary.Component, summary.OperationID)
	if summary.OperationID == "" || cat.operations[key] != nil {
		return fmt.Errorf("MCP-CATALOG-IDENTITY: missing or duplicate operation ID in %s", summary.Component)
	}
	entry := &apiOperation{OperationSummary: summary, spec: spec, pathItem: item, value: operation}
	cat.operations[key] = entry
	cat.ordered = append(cat.ordered, entry)
	return nil
}

func operationKey(component, id string) string { return component + "/" + id }

func (cat *catalog) find(component, id string) (*apiOperation, error) {
	op := cat.operations[operationKey(component, id)]
	if op == nil {
		return nil, fmt.Errorf("MCP-CATALOG-NOTFOUND: unknown component or operation_id")
	}
	return op, nil
}

func (cat *catalog) search(input operationSearchInput) (operationSearchResult, error) {
	result := operationSearchResult{Matches: []OperationSummary{}}
	limit, err := resultLimit(input.Limit, 20, 100)
	if err != nil || input.Offset < 0 {
		return result, fmt.Errorf("MCP-CATALOG-PAGINATION: offset must be nonnegative and limit between 1 and 100")
	}
	terms := searchTerms(input.Query)
	for _, op := range cat.ordered {
		if input.Component != "" && input.Component != op.Component {
			continue
		}
		if len(terms) != 0 && searchScore(terms, op.OperationID+" "+op.Summary+" "+op.Path+" "+op.value.Description) == 0 {
			continue
		}
		result.Total++
		if result.Total > input.Offset && len(result.Matches) < limit {
			result.Matches = append(result.Matches, op.OperationSummary)
		}
	}
	if input.Offset+len(result.Matches) < result.Total {
		next := input.Offset + len(result.Matches)
		result.NextOffset = &next
	}
	return result, nil
}

func (cat *catalog) get(input operationInput) (operationResult, error) {
	op, err := cat.find(input.Component, input.OperationID)
	if err != nil {
		return operationResult{}, err
	}
	result := operationResult{OperationSummary: op.OperationSummary, Description: op.value.Description,
		Parameters: op.parameters(), Responses: make(map[string]*openapi3.Response)}
	for status, response := range op.value.Responses.Map() {
		result.Responses[status] = response.Value
	}
	if op.value.RequestBody != nil {
		result.RequestBody = op.value.RequestBody.Value
	}
	if input.SchemaRef != "" {
		name := strings.TrimPrefix(input.SchemaRef, "#/components/schemas/")
		ref := op.spec.Components.Schemas[name]
		if ref == nil || name == input.SchemaRef {
			return result, fmt.Errorf("MCP-CATALOG-SCHEMA: unknown local schema reference")
		}
		result.Schema = ref.Value
	}
	for name := range op.spec.Components.Schemas {
		result.SchemaNames = append(result.SchemaNames, name)
	}
	sort.Strings(result.SchemaNames)
	return result, nil
}

func (op *apiOperation) parameters() []*openapi3.Parameter {
	parameters := make(map[string]*openapi3.Parameter)
	for _, refs := range []openapi3.Parameters{op.pathItem.Parameters, op.value.Parameters} {
		for _, ref := range refs {
			parameters[ref.Value.In+":"+ref.Value.Name] = ref.Value
		}
	}
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]*openapi3.Parameter, 0, len(keys))
	for _, key := range keys {
		result = append(result, parameters[key])
	}
	return result
}
