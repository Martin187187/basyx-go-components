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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

type callInput struct {
	Environment string              `json:"environment" jsonschema:"Configured environment name; see server instructions"`
	Component   string              `json:"component" jsonschema:"Component returned by search_api_operations"`
	OperationID string              `json:"operation_id" jsonschema:"Exact operation_id to execute; inspect get_api_operation first"`
	PathParams  map[string]string   `json:"path_params,omitempty" jsonschema:"Path parameters. Supply raw identifiers; parameters documented as base64 are encoded automatically"`
	QueryParams map[string][]string `json:"query_params,omitempty" jsonschema:"Wire-format query parameters, including required base64url encoding; arrays use repeated values"`
	Headers     map[string]string   `json:"headers,omitempty" jsonschema:"Documented request headers, Accept, If-Match or If-None-Match; credentials cannot be supplied here"`
	ContentType string              `json:"content_type,omitempty" jsonschema:"Request media type; defaults to application/json for body"`
	Body        any                 `json:"body,omitempty" jsonschema:"JSON request body, or text for a text media type; multipart fields are an object"`
	BodyBase64  string              `json:"body_base64,omitempty" jsonschema:"Base64-encoded binary request body; exclusive with body and files"`
	Files       map[string]filePart `json:"files,omitempty" jsonschema:"Multipart file fields, keyed by the OpenAPI property name"`
}

type filePart struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Base64      string `json:"base64"`
}

type callResult struct {
	Status      int               `json:"status"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers"`
	Body        any               `json:"body,omitempty"`
	BodyBase64  string            `json:"body_base64,omitempty"`
}

func (svc *service) call(ctx context.Context, input callInput) (callResult, error) {
	op, err := svc.catalog.find(input.Component, input.OperationID)
	if err != nil {
		return callResult{}, err
	}
	target, err := svc.target(input.Environment, op)
	if err != nil {
		return callResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, svc.client.Timeout)
	defer cancel()
	req, err := svc.buildRequest(ctx, input, op, target)
	if err != nil {
		return callResult{}, err
	}
	response, err := svc.client.Do(req)
	if err != nil {
		return callResult{}, fmt.Errorf("MCP-CALL-HTTP: request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	return readResponse(response, svc.config.MaxResponseBytes)
}

func (svc *service) target(environment string, op *apiOperation) (Target, error) {
	target, ok := svc.config.Environments[environment][op.Component]
	if !ok {
		return Target{}, fmt.Errorf("MCP-CALL-TARGET: environment/component is not configured")
	}
	if op.Method != http.MethodGet && op.Method != http.MethodHead && op.Method != http.MethodOptions && !target.AllowWrites {
		return Target{}, fmt.Errorf("MCP-CALL-READONLY: this target only permits GET, HEAD and OPTIONS")
	}
	return target, nil
}

func (svc *service) buildRequest(ctx context.Context, input callInput, op *apiOperation, target Target) (*http.Request, error) {
	if err := validateParameterNames(input, op); err != nil {
		return nil, err
	}
	endpoint, pathParams, err := buildURL(input, op, target)
	if err != nil {
		return nil, err
	}
	body, contentType, err := encodeBody(input, svc.config.MaxResponseBytes)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("MCP-CALL-REQUEST: %w", err)
	}
	if err := applyHeaders(req, input.Headers, contentType, target); err != nil {
		return nil, err
	}
	validation := &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams,
		Route: &routers.Route{Spec: op.spec, Path: op.Path, PathItem: op.pathItem, Method: op.Method, Operation: op.value},
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			SkipSettingDefaults: true, RejectWhenRequestBodyNotSpecified: true}}
	if err := openapi3filter.ValidateRequest(ctx, validation); err != nil {
		return nil, fmt.Errorf("MCP-CALL-VALIDATE: %w", err)
	}
	return req, nil
}

func validateParameterNames(input callInput, op *apiOperation) error {
	allowed := map[string]map[string]bool{"path": {}, "query": {}, "header": {
		"accept": true, "if-match": true, "if-none-match": true,
	}}
	for _, parameter := range op.parameters() {
		name := parameter.Name
		if parameter.In == "header" {
			name = strings.ToLower(name)
		}
		if names := allowed[parameter.In]; names != nil {
			names[name] = true
		}
	}
	for name := range input.PathParams {
		if !allowed["path"][name] {
			return fmt.Errorf("MCP-CALL-PARAMETER: unknown path parameter %s", name)
		}
	}
	for name := range input.QueryParams {
		if !allowed["query"][name] {
			return fmt.Errorf("MCP-CALL-PARAMETER: unknown query parameter %s", name)
		}
	}
	for name := range input.Headers {
		lower := strings.ToLower(name)
		if !allowed["header"][lower] || lower == "authorization" || lower == "host" || lower == "cookie" {
			return fmt.Errorf("MCP-CALL-HEADER: header %s is not permitted", name)
		}
	}
	return nil
}

func buildURL(input callInput, op *apiOperation, target Target) (string, map[string]string, error) {
	base, err := targetURL(target)
	if err != nil {
		return "", nil, err
	}
	route := op.Path
	encoded := make(map[string]string)
	for _, parameter := range op.parameters() {
		if parameter.In != "path" {
			continue
		}
		value, exists := input.PathParams[parameter.Name]
		if !exists || value == "" || value == "." || value == ".." {
			return "", nil, fmt.Errorf("MCP-CALL-PATH: missing or invalid path parameter %s", parameter.Name)
		}
		if strings.Contains(strings.ToLower(parameter.Description), "base64") {
			value = base64.RawURLEncoding.EncodeToString([]byte(value))
		}
		if strings.ContainsAny(value, "/\\%\r\n") {
			return "", nil, fmt.Errorf("MCP-CALL-PATH: unsafe path parameter %s", parameter.Name)
		}
		encoded[parameter.Name] = value
		route = strings.ReplaceAll(route, "{"+parameter.Name+"}", url.PathEscape(value))
	}
	endpoint := strings.TrimRight(base.String(), "/") + route
	query := url.Values(input.QueryParams).Encode()
	if query != "" {
		endpoint += "?" + query
	}
	return endpoint, encoded, nil
}

func readResponse(response *http.Response, maximum int64) (callResult, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return callResult{}, fmt.Errorf("MCP-CALL-READ: %w", err)
	}
	if int64(len(data)) > maximum {
		return callResult{}, fmt.Errorf("MCP-CALL-SIZE: response exceeds maxResponseBytes; request a smaller page or representation")
	}
	result := callResult{Status: response.StatusCode, ContentType: response.Header.Get("Content-Type"), Headers: map[string]string{}}
	for _, name := range []string{"ETag", "Location", "Retry-After", "Content-Disposition"} {
		if value := response.Header.Get(name); value != "" {
			result.Headers[name] = value
		}
	}
	if len(data) == 0 {
		return result, nil
	}
	switch {
	case json.Valid(data):
		result.Body = json.RawMessage(data)
	case utf8.Valid(data) && (strings.HasPrefix(result.ContentType, "text/") || strings.Contains(result.ContentType, "xml")):
		result.Body = string(data)
	default:
		result.BodyBase64 = base64.StdEncoding.EncodeToString(data)
	}
	return result, nil
}
