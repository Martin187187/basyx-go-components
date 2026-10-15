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

package integration_tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eclipse-basyx/basyx-go-components/internal/mcpservice"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const fixtureSpec = `openapi: 3.0.3
info: {title: Test API, version: '1.0'}
paths:
  /submodels/{submodelIdentifier}:
    parameters:
      - name: submodelIdentifier
        in: path
        required: true
        description: Unique id (UTF8-BASE64-URL-encoded)
        schema: {type: string}
    get:
      operationId: GetSubmodel
      summary: Read a submodel
      parameters:
        - name: limit
          in: query
          schema: {type: integer, minimum: 1, maximum: 10}
      responses:
        '200': {description: OK}
    put:
      operationId: PutSubmodel
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [id]
              properties:
                id: {type: string}
      responses:
        '204': {description: Updated}
  /upload:
    post:
      operationId: Upload
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required: [file]
              properties:
                file: {type: string, format: binary}
      responses:
        '204': {description: Uploaded}
`

func fixture(t *testing.T, target mcpservice.Target) (mcpservice.Config, *mcp.ClientSession) {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "README.md", "# BaSyx\n\nA digital twin platform.\n")
	writeFixture(t, root, "docu/security.md", "# OIDC\n\nConfigure OIDC authentication for BaSyx.\n")
	writeFixture(t, root, "cmd/testservice/openapi.yaml", fixtureSpec)
	cfg := mcpservice.Config{Repository: root, Environments: map[string]map[string]mcpservice.Target{
		"demo": {"testservice": target},
	}}
	return cfg, connect(t, cfg)
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0750))
	require.NoError(t, os.WriteFile(filename, []byte(content), 0600))
}

func connect(t *testing.T, cfg mcpservice.Config) *mcp.ClientSession {
	t.Helper()
	server, err := mcpservice.New(t.Context(), cfg)
	require.NoError(t, err)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, serverSession.Close()) })
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, wantError bool) map[string]any {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	content, err := json.Marshal(result.Content)
	require.NoError(t, err)
	require.Equal(t, wantError, result.IsError, string(content))
	if wantError {
		return nil
	}
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))
	return output
}

func TestDocumentationAndDiscovery(t *testing.T) {
	_, session := fixture(t, mcpservice.Target{URL: "http://localhost:5004"})
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 5)
	result := call(t, session, "search_docs", map[string]any{"query": "OIDC"}, false)
	require.Contains(t, fmt.Sprint(result["matches"]), "docu/security.md")
	result = call(t, session, "get_doc", map[string]any{"document_id": "docu/security.md"}, false)
	require.Contains(t, result["text"], "authentication")
	call(t, session, "get_doc", map[string]any{"document_id": "../go.mod"}, true)
	result = call(t, session, "search_api_operations", map[string]any{"query": "submodel"}, false)
	require.Contains(t, fmt.Sprint(result["matches"]), "GetSubmodel")
	result = call(t, session, "get_api_operation", map[string]any{"component": "testservice", "operation_id": "GetSubmodel"}, false)
	require.Contains(t, fmt.Sprint(result), "submodelIdentifier")
}

func TestCallsUseConfiguredTargetAndCredentials(t *testing.T) {
	t.Setenv("BASYX_MCP_TEST_TOKEN", "test-token")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/submodels/dXJuOnRlc3Q", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("limit"))
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"revision-1"`)
		_, err := w.Write([]byte(`{"id":"urn:test"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(backend.Close)
	target := mcpservice.Target{URL: backend.URL + "/api"}
	target.TokenEnv = "BASYX_MCP_TEST_TOKEN"
	_, session := fixture(t, target)
	result := call(t, session, "call_api", map[string]any{
		"environment": "demo", "component": "testservice", "operation_id": "GetSubmodel",
		"path_params":  map[string]string{"submodelIdentifier": "urn:test"},
		"query_params": map[string][]string{"limit": {"2"}},
	}, false)
	require.Equal(t, float64(200), result["status"])
	require.Equal(t, "urn:test", result["body"].(map[string]any)["id"])
	require.Contains(t, fmt.Sprint(result["headers"]), "revision-1")
}

func TestInvalidCallsNeverReachBackend(t *testing.T) {
	var requests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(backend.Close)
	_, session := fixture(t, mcpservice.Target{URL: backend.URL})
	cases := []map[string]any{
		{"operation_id": "PutSubmodel", "body": map[string]string{"id": "test"}},
		{"query_params": map[string][]string{"limit": {"99"}}},
		{"query_params": map[string][]string{"unexpected": {"x"}}},
		{"headers": map[string]string{"Authorization": "Bearer attacker"}},
		{"path_params": map[string]string{"submodelIdentifier": ".."}},
		{"environment": "unknown"},
		{"operation_id": "unknown"},
	}
	for _, changes := range cases {
		args := map[string]any{"environment": "demo", "component": "testservice", "operation_id": "GetSubmodel",
			"path_params": map[string]string{"submodelIdentifier": "urn:test"}}
		for key, value := range changes {
			args[key] = value
		}
		call(t, session, "call_api", args, true)
	}
	require.Zero(t, requests.Load())
}

func TestWritesValidateBodyAndSupportMultipart(t *testing.T) {
	var requests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		requests.Add(1)
		if r.URL.Path == "/upload" {
			reader, err := r.MultipartReader()
			require.NoError(t, err)
			part, err := reader.NextPart()
			require.NoError(t, err)
			require.Equal(t, "file", part.FormName())
			require.Equal(t, "test.txt", part.FileName())
			data, err := io.ReadAll(part)
			require.NoError(t, err)
			require.Equal(t, "hello", string(data))
			require.NoError(t, part.Close())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(backend.Close)
	_, session := fixture(t, mcpservice.Target{URL: backend.URL, AllowWrites: true})
	args := map[string]any{"environment": "demo", "component": "testservice", "operation_id": "PutSubmodel",
		"path_params": map[string]string{"submodelIdentifier": "urn:test"}, "body": map[string]any{"bad": true}}
	call(t, session, "call_api", args, true)
	require.Zero(t, requests.Load())
	args["body"] = map[string]string{"id": "urn:test"}
	call(t, session, "call_api", args, false)
	call(t, session, "call_api", map[string]any{"environment": "demo", "component": "testservice", "operation_id": "Upload",
		"content_type": "multipart/form-data", "files": map[string]any{"file": map[string]string{
			"filename": "test.txt", "content_type": "text/plain", "base64": "aGVsbG8="}}}, false)
	require.Equal(t, int32(2), requests.Load())
}

func TestBackendErrorsKeepHTTPDetails(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, err := w.Write([]byte(`{"message":"permission denied"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(backend.Close)
	_, session := fixture(t, mcpservice.Target{URL: backend.URL})
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "call_api", Arguments: map[string]any{
		"environment": "demo", "component": "testservice", "operation_id": "GetSubmodel",
		"path_params": map[string]string{"submodelIdentifier": "urn:test"},
	}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.Contains(t, string(data), `"status":403`)
	require.Contains(t, string(data), "permission denied")
}

func TestContractReferencesCannotFetchRemoteData(t *testing.T) {
	root := t.TempDir()
	var requests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(remote.Close)
	spec := strings.Replace(fixtureSpec, "schema: {type: string}", "schema: {$ref: '"+remote.URL+"/schema.yaml'}", 1)
	writeFixture(t, root, "cmd/testservice/openapi.yaml", spec)
	_, err := mcpservice.New(t.Context(), mcpservice.Config{Repository: root})
	require.ErrorContains(t, err, "MCP-CATALOG-REFERENCE")
	require.Zero(t, requests.Load())
}

func TestMissingConfiguredTokenNeverCallsBackend(t *testing.T) {
	var requests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(backend.Close)
	t.Setenv("BASYX_MCP_TEST_MISSING_TOKEN", "")
	target := mcpservice.Target{URL: backend.URL}
	target.TokenEnv = "BASYX_MCP_TEST_MISSING_TOKEN"
	_, session := fixture(t, target)
	call(t, session, "call_api", map[string]any{"environment": "demo", "component": "testservice",
		"operation_id": "GetSubmodel", "path_params": map[string]string{"submodelIdentifier": "urn:test"}}, true)
	require.Zero(t, requests.Load())
}

func TestRedirectsAndOversizedResponses(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leaked.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(other.Close)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") == "1" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		_, err := w.Write([]byte(strings.Repeat("x", 2000)))
		require.NoError(t, err)
	}))
	t.Cleanup(backend.Close)
	cfg, _ := fixture(t, mcpservice.Target{URL: backend.URL})
	cfg.MaxResponseBytes = 1024
	session := connect(t, cfg)
	args := map[string]any{"environment": "demo", "component": "testservice", "operation_id": "GetSubmodel",
		"path_params": map[string]string{"submodelIdentifier": "urn:test"}, "query_params": map[string][]string{"limit": {"1"}}}
	result := call(t, session, "call_api", args, false)
	require.Equal(t, float64(302), result["status"])
	require.Zero(t, leaked.Load())
	delete(args, "query_params")
	call(t, session, "call_api", args, true)
}

func TestRepositoryContractsAndSchemas(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	session := connect(t, mcpservice.Config{Repository: root})
	result := call(t, session, "search_api_operations", map[string]any{"limit": 100}, false)
	require.Greater(t, result["total"].(float64), float64(300))
	inspected := 0
	for {
		for _, item := range result["matches"].([]any) {
			operation := item.(map[string]any)
			details := call(t, session, "get_api_operation", map[string]any{
				"component": operation["component"], "operation_id": operation["operation_id"],
			}, false)
			require.Equal(t, operation["operation_id"], details["operation_id"])
			inspected++
		}
		next, more := result["next_offset"]
		if !more {
			break
		}
		result = call(t, session, "search_api_operations", map[string]any{"limit": 100, "offset": next}, false)
	}
	require.Equal(t, int(result["total"].(float64)), inspected)
	args := map[string]any{"component": "submodelrepositoryservice", "operation_id": "GetSubmodelById"}
	result = call(t, session, "get_api_operation", args, false)
	badRequest := result["responses"].(map[string]any)["400"].(map[string]any)
	require.Contains(t, badRequest, "content")
	response := result["responses"].(map[string]any)["200"].(map[string]any)
	schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	args["schema_ref"] = schema["$ref"]
	result = call(t, session, "get_api_operation", args, false)
	require.Contains(t, fmt.Sprint(result["schema"]), "submodelElements")
}

func TestBackendJSONNumbersArePreserved(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			data, err := io.ReadAll(io.LimitReader(r.Body, 4096))
			require.NoError(t, err)
			require.Contains(t, string(data), "9007199254740993")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"counter":9007199254740993}`))
		require.NoError(t, err)
	}))
	t.Cleanup(backend.Close)
	_, session := fixture(t, mcpservice.Target{URL: backend.URL, AllowWrites: true})
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "call_api", Arguments: map[string]any{
		"environment": "demo", "component": "testservice", "operation_id": "GetSubmodel",
		"path_params": map[string]string{"submodelIdentifier": "urn:test"},
	}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	content := result.Content[0].(*mcp.TextContent).Text
	require.Contains(t, content, "9007199254740993")
	call(t, session, "call_api", map[string]any{"environment": "demo", "component": "testservice", "operation_id": "PutSubmodel",
		"path_params": map[string]string{"submodelIdentifier": "urn:test"},
		"body":        map[string]any{"id": "urn:test", "counter": json.Number("9007199254740993")}}, false)
}
