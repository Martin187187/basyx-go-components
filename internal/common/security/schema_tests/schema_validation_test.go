package schema_tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

func TestAccessRuleAndQueryFilesAreValid(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	schemaPath := filepath.Join(repoRoot, "internal", "common", "security", "schema_tests", "schema.json")
	schemaLoader, err := buildCompatibleSchemaLoader(schemaPath)
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}

	testDataDir := filepath.Join(repoRoot, "internal", "common", "security", "schema_tests", "testdata")
	var filesToCheck []string
	entries, err := os.ReadDir(testDataDir)
	if err != nil {
		t.Fatalf("failed to read testdata directory %s: %v", testDataDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("nested directory found in testdata, expected only direct JSON files: %s", entry.Name())
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			filesToCheck = append(filesToCheck, filepath.Join(testDataDir, entry.Name()))
		}
	}
	if len(filesToCheck) == 0 {
		t.Fatalf("no JSON files found in %s", testDataDir)
	}

	for _, file := range filesToCheck {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("Failed to read %s: %v", file, err)
			continue
		}
		fileLoader := gojsonschema.NewBytesLoader(content)
		result, err := gojsonschema.Validate(schemaLoader, fileLoader)
		if err != nil {
			t.Errorf("Schema validation error in %s: %v", file, err)
			continue
		}
		if !result.Valid() {
			t.Errorf("%s does not conform to schema:\n", file)
			for _, desc := range result.Errors() {
				t.Errorf("- %s", desc)
			}
		}
	}
}

func buildCompatibleSchemaLoader(schemaPath string) (gojsonschema.JSONLoader, error) {
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(b, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	definitions, ok := schema["definitions"].(map[string]interface{})
	if ok {
		if standardString, ok := definitions["standardString"].(map[string]interface{}); ok {
			if pattern, ok := standardString["pattern"].(string); ok && pattern == "^(?!\\$).*" {
				standardString["pattern"] = "^(?:[^$].*|)$"
			}
		}
	}

	return gojsonschema.NewGoLoader(schema), nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	current := wd
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("repository root with go.mod not found from %s", wd)
		}
		current = parent
	}
}
