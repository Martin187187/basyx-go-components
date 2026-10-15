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

// Package main implements the local BaSyx MCP service over stdio.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/eclipse-basyx/basyx-go-components/internal/common"
	"github.com/eclipse-basyx/basyx-go-components/internal/mcpservice"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("basyxmcpservice", flag.ContinueOnError)
	configPath := flags.String("config", "", "Path to MCP YAML configuration")
	repository := flags.String("repository", "", "BaSyx checkout root; overrides repository in configuration")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return fmt.Errorf("MCP-MAIN-FLAGS: %w", err)
	}
	cfg, err := mcpservice.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *repository != "" {
		cfg.Repository = *repository
	}
	server, err := mcpservice.New(ctx, cfg)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "BaSyx MCP service ready", "transport", "stdio")
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP-MAIN-TRANSPORT: %w", err)
	}
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, stop := common.SignalContext()
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		slog.Error("MCP service stopped", "error.code", "MCP-MAIN-RUN", "error", err)
		os.Exit(1)
	}
}
