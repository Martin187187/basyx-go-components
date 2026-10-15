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
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerCallTool(server *mcp.Server, svc *service) error {
	input, err := jsonschema.For[callInput](nil)
	if err != nil {
		return fmt.Errorf("MCP-TOOL-INPUTSCHEMA: %w", err)
	}
	resolved, err := input.Resolve(nil)
	if err != nil {
		return fmt.Errorf("MCP-TOOL-RESOLVESCHEMA: %w", err)
	}
	output, err := jsonschema.For[callResult](nil)
	if err != nil {
		return fmt.Errorf("MCP-TOOL-OUTPUTSCHEMA: %w", err)
	}
	server.AddTool(&mcp.Tool{Name: "call_api", InputSchema: input, OutputSchema: output,
		Description: "Execute a discovered BaSyx operation against a configured environment. Inspect its schema first. Non-GET/HEAD/OPTIONS requires allowWrites. HTTP failures set isError and preserve status/body; redirects are never followed."},
		func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result := &mcp.CallToolResult{}
			args, err := decodeCallInput(request.Params.Arguments, resolved, svc.config.MaxResponseBytes)
			if err != nil {
				result.SetError(err)
				return result, nil
			}
			response, err := svc.call(ctx, args)
			if err != nil {
				result.SetError(err)
				return result, nil
			}
			data, err := json.Marshal(response)
			if err != nil {
				result.SetError(fmt.Errorf("MCP-TOOL-OUTPUT: %w", err))
				return result, nil
			}
			result.IsError = response.Status >= 400
			result.StructuredContent = json.RawMessage(data)
			result.Content = []mcp.Content{&mcp.TextContent{Text: string(data)}}
			return result, nil
		})
	return nil
}

func decodeCallInput(data []byte, schema *jsonschema.Resolved, maximum int64) (callInput, error) {
	var input callInput
	if int64(len(data)) > maximum*2+65536 {
		return input, fmt.Errorf("MCP-TOOL-SIZE: arguments exceed the configured payload limit")
	}
	var shape any
	if err := json.Unmarshal(data, &shape); err != nil {
		return input, fmt.Errorf("MCP-TOOL-JSON: %w", err)
	}
	if err := schema.Validate(shape); err != nil {
		return input, fmt.Errorf("MCP-TOOL-VALIDATE: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("MCP-TOOL-DECODE: %w", err)
	}
	return input, nil
}
