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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"sort"
	"strings"
)

func encodeBody(input callInput, maximum int64) ([]byte, string, error) {
	contentType := input.ContentType
	if input.BodyBase64 != "" {
		if input.Body != nil || len(input.Files) > 0 || contentType == "" {
			return nil, "", fmt.Errorf("MCP-BODY-BINARY: specify content_type and only body_base64 for binary input")
		}
		data, err := decodeBase64(input.BodyBase64, maximum)
		return data, contentType, err
	}
	if len(input.Files) > 0 || contentType == "multipart/form-data" {
		return encodeMultipart(input, maximum)
	}
	if input.Body == nil {
		return nil, contentType, nil
	}
	if contentType == "" {
		contentType = "application/json"
	}
	data, err := encodeValue(input.Body, contentType)
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maximum {
		return nil, "", fmt.Errorf("MCP-BODY-SIZE: request exceeds maxResponseBytes")
	}
	return data, contentType, nil
}

func encodeValue(value any, contentType string) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("MCP-BODY-MEDIA: invalid content_type")
	}
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("MCP-BODY-JSON: %w", err)
		}
		return data, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("MCP-BODY-TEXT: non-JSON bodies must be strings, or use body_base64")
	}
	return []byte(text), nil
}

func decodeBase64(text string, maximum int64) ([]byte, error) {
	if int64(base64.StdEncoding.DecodedLen(len(text))) > maximum+2 {
		return nil, fmt.Errorf("MCP-BODY-SIZE: binary input exceeds maxResponseBytes")
	}
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("MCP-BODY-BASE64: invalid standard base64 input")
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("MCP-BODY-SIZE: binary input exceeds maxResponseBytes")
	}
	return data, nil
}

func encodeMultipart(input callInput, maximum int64) ([]byte, string, error) {
	if input.ContentType != "" && input.ContentType != "multipart/form-data" {
		return nil, "", fmt.Errorf("MCP-BODY-MULTIPART: files require multipart/form-data")
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err := writeFields(writer, input.Body, maximum); err != nil {
		return nil, "", err
	}
	for _, name := range sortedKeys(input.Files) {
		if err := writeFilePart(writer, name, input.Files[name], maximum-int64(buffer.Len())); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("MCP-BODY-MULTIPART: %w", err)
	}
	if int64(buffer.Len()) > maximum {
		return nil, "", fmt.Errorf("MCP-BODY-SIZE: multipart request exceeds maxResponseBytes")
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func writeFields(writer *multipart.Writer, body any, maximum int64) error {
	if body == nil {
		return nil
	}
	fields, ok := body.(map[string]any)
	if !ok {
		return fmt.Errorf("MCP-BODY-FIELDS: multipart body must be an object of field values")
	}
	for _, name := range sortedKeys(fields) {
		value := fields[name]
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("MCP-BODY-FIELD: multipart field %s must be a string", name)
		}
		if strings.ContainsAny(name, "\r\n") || int64(len(text)) > maximum {
			return fmt.Errorf("MCP-BODY-FIELD: invalid or oversized multipart field")
		}
		maximum -= int64(len(text))
		if err := writer.WriteField(name, text); err != nil {
			return fmt.Errorf("MCP-BODY-FIELD: %w", err)
		}
	}
	return nil
}

func writeFilePart(writer *multipart.Writer, name string, file filePart, maximum int64) error {
	if file.Filename == "" || strings.ContainsAny(name+file.Filename+file.ContentType, "\r\n") {
		return fmt.Errorf("MCP-BODY-FILENAME: filename is required and multipart metadata must not contain line breaks")
	}
	data, err := decodeBase64(file.Base64, maximum)
	if err != nil {
		return err
	}
	contentType := file.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	headers := textproto.MIMEHeader{}
	headers.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": name, "filename": file.Filename}))
	headers.Set("Content-Type", contentType)
	part, err := writer.CreatePart(headers)
	if err != nil {
		return fmt.Errorf("MCP-BODY-PART: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("MCP-BODY-PART: %w", err)
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func applyHeaders(req *http.Request, headers map[string]string, contentType string, target Target) error {
	for name, value := range headers {
		if strings.ContainsAny(name+value, "\r\n") {
			return fmt.Errorf("MCP-CALL-HEADER: headers must not contain line breaks")
		}
		req.Header.Set(name, value)
	}
	if contentType != "" {
		if _, _, err := mime.ParseMediaType(contentType); err != nil {
			return fmt.Errorf("MCP-CALL-CONTENTTYPE: invalid content_type")
		}
		req.Header.Set("Content-Type", contentType)
	}
	if target.TokenEnv == "" {
		return nil
	}
	token := os.Getenv(target.TokenEnv)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("MCP-CALL-CREDENTIALS: configured token environment variable is empty or invalid")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}
