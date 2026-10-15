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
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode"
)

type documentation struct {
	files    map[string][]string
	chunks   []docChunk
	revision string
}

type docChunk struct {
	document string
	line     int
	text     string
}

type docSearchInput struct {
	Query string `json:"query" jsonschema:"Words to search in the checkout documentation"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum matches, from 1 to 20; defaults to 5"`
}

type docMatch struct {
	DocumentID string `json:"document_id"`
	StartLine  int    `json:"start_line"`
	Excerpt    string `json:"excerpt"`
	Score      int    `json:"score"`
}

type docSearchResult struct {
	SourceRevision string     `json:"source_revision"`
	Matches        []docMatch `json:"matches"`
}

type docInput struct {
	DocumentID string `json:"document_id" jsonschema:"Repository-relative document_id returned by search_docs"`
	StartLine  int    `json:"start_line,omitempty" jsonschema:"First line, starting at 1; defaults to 1"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum lines from 1 to 200; defaults to 100"`
}

type docResult struct {
	DocumentID     string `json:"document_id"`
	SourceRevision string `json:"source_revision"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	TotalLines     int    `json:"total_lines"`
	Text           string `json:"text"`
}

func loadDocumentation(ctx context.Context, source fs.FS, revision string) (*documentation, error) {
	docs := &documentation{files: make(map[string][]string), revision: revision}
	err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return documentationDirectory(name)
		}
		if entry.Type()&fs.ModeSymlink != 0 || path.Ext(name) != ".md" || !documentationFile(name) {
			return nil
		}
		return docs.add(source, name)
	})
	if err != nil {
		return nil, fmt.Errorf("MCP-DOCS-LOAD: %w", err)
	}
	return docs, nil
}

func documentationDirectory(name string) error {
	if name == "." || name == "docu" || name == "examples" || name == "cmd" || name == "cmd/basyxmcpservice" ||
		strings.HasPrefix(name, "docu/") || strings.HasPrefix(name, "examples/") || strings.HasPrefix(name, "cmd/basyxmcpservice/") {
		return nil
	}
	return fs.SkipDir
}

func documentationFile(name string) bool {
	return strings.Contains(name, "/") || name == "README.md" || name == "CHANGELOG.md" || name == "CONTRIBUTING.md"
}

func (docs *documentation) add(source fs.FS, name string) error {
	file, err := source.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 256*1024+1))
	if err != nil || len(data) > 256*1024 {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	docs.files[name] = lines
	for start := 0; start < len(lines); start += 30 {
		docs.chunks = append(docs.chunks, docChunk{document: name, line: start + 1,
			text: strings.Join(lines[start:min(start+30, len(lines))], "\n")})
	}
	return nil
}

func (docs *documentation) search(input docSearchInput) (docSearchResult, error) {
	result := docSearchResult{SourceRevision: docs.revision, Matches: []docMatch{}}
	terms := searchTerms(input.Query)
	if len(terms) == 0 {
		return result, fmt.Errorf("MCP-DOCS-QUERY: query must contain searchable words")
	}
	limit, err := resultLimit(input.Limit, 5, 20)
	if err != nil {
		return result, err
	}
	for _, chunk := range docs.chunks {
		score := searchScore(terms, chunk.document+"\n"+chunk.text)
		if score > 0 {
			result.Matches = append(result.Matches, docMatch{DocumentID: chunk.document,
				StartLine: chunk.line, Excerpt: excerpt(chunk.text, 1200), Score: score})
		}
	}
	sort.SliceStable(result.Matches, func(i, j int) bool { return result.Matches[i].Score > result.Matches[j].Score })
	result.Matches = result.Matches[:min(limit, len(result.Matches))]
	return result, nil
}

func (docs *documentation) get(input docInput) (docResult, error) {
	lines, ok := docs.files[input.DocumentID]
	if !ok {
		return docResult{}, fmt.Errorf("MCP-DOCS-NOTFOUND: document is not indexed")
	}
	limit, err := resultLimit(input.Limit, 100, 200)
	if err != nil {
		return docResult{}, err
	}
	start := input.StartLine
	if start == 0 {
		start = 1
	}
	if start < 1 || start > len(lines) {
		return docResult{}, fmt.Errorf("MCP-DOCS-LINE: start_line is outside the document")
	}
	end := min(start-1+limit, len(lines))
	return docResult{DocumentID: input.DocumentID, SourceRevision: docs.revision, StartLine: start,
		EndLine: end, TotalLines: len(lines), Text: strings.Join(lines[start-1:end], "\n")}, nil
}

func searchTerms(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func searchScore(terms []string, text string) int {
	lower := strings.ToLower(text)
	score := 0
	for _, term := range terms {
		score += strings.Count(lower, term)
	}
	return score
}

func resultLimit(value, fallback, maximum int) (int, error) {
	if value == 0 {
		return fallback, nil
	}
	if value < 1 || value > maximum {
		return 0, fmt.Errorf("MCP-INPUT-LIMIT: limit must be between 1 and %d", maximum)
	}
	return value, nil
}

func excerpt(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
