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

// Package mcpservice exposes BaSyx documentation and OpenAPI operations over MCP.
package mcpservice

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const defaultMaxBytes = 1024 * 1024

// Config selects the source checkout and the explicitly configured API targets.
type Config struct {
	Repository       string                       `yaml:"repository"`
	SourceRevision   string                       `yaml:"sourceRevision"`
	TimeoutSeconds   int                          `yaml:"timeoutSeconds"`
	MaxResponseBytes int64                        `yaml:"maxResponseBytes"`
	Environments     map[string]map[string]Target `yaml:"environments"`
}

// Target binds a component to its API root and optional backend credentials.
type Target struct {
	URL         string `yaml:"url"`
	TokenEnv    string `yaml:"tokenEnv"`
	AllowWrites bool   `yaml:"allowWrites"`
}

// LoadConfig reads a strict YAML configuration. An empty path uses defaults.
func LoadConfig(filename string) (Config, error) {
	var cfg Config
	if filename == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(filepath.Clean(filename)) // #nosec G304 -- the local operator supplies this configuration path, not an MCP caller.
	if err != nil {
		return cfg, fmt.Errorf("MCP-CONFIG-READ: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("MCP-CONFIG-DECODE: %w", err)
	}
	return cfg, nil
}

func (cfg *Config) normalize() error {
	if cfg.Repository == "" {
		cfg.Repository = "."
	}
	if cfg.SourceRevision == "" {
		cfg.SourceRevision = "workspace"
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxBytes
	}
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 300 {
		return fmt.Errorf("MCP-CONFIG-TIMEOUT: timeoutSeconds must be between 1 and 300")
	}
	if cfg.MaxResponseBytes < 1024 || cfg.MaxResponseBytes > 16*defaultMaxBytes {
		return fmt.Errorf("MCP-CONFIG-LIMIT: maxResponseBytes must be between 1024 and 16777216")
	}
	return nil
}

func targetURL(target Target) (*url.URL, error) {
	u, err := url.Parse(target.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("MCP-CONFIG-URL: target must be an absolute HTTP(S) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("MCP-CONFIG-URL: credentials, query strings and fragments are not allowed in target URLs")
	}
	if strings.ContainsAny(target.TokenEnv, "\r\n") {
		return nil, fmt.Errorf("MCP-CONFIG-TOKEN: invalid token environment variable")
	}
	return u, nil
}
