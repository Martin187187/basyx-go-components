package workshop

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestWorkshopIntegration reuses the AAS registry security test configs (medium level)
// and executes them against the locally running workshop compose stack.
//
// Environment overrides:
//
//	WORKSHOP_IT_CONFIG   (default internal/aasregistry/security_tests/it_config.json)
//	WORKSHOP_BASE_URL    (default http://localhost:6004)
//	WORKSHOP_TOKEN_URL   (default http://localhost:8080/realms/basyx/protocol/openid-connect/token)
func TestWorkshopIntegration(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	cfgPath := os.Getenv("WORKSHOP_IT_CONFIG")
	if cfgPath == "" {
		cfgPath = filepath.Join(repoRoot, "internal", "aasregistry", "security_tests", "it_config.json")
	}
	cases, err := loadTestConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("no test cases in %s", cfgPath)
	}

	truncateDatabase(t)

	baseURL := os.Getenv("WORKSHOP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:6004"
	}
	tokenURL := os.Getenv("WORKSHOP_TOKEN_URL")
	if tokenURL == "" {
		tokenURL = "http://localhost:8080/realms/basyx/protocol/openid-connect/token"
	}

	client := &http.Client{Timeout: 15 * time.Second}
	caseBaseDir := filepath.Dir(cfgPath)

	for i, c := range cases {
		c := c
		name := fmt.Sprintf("Case_%d_%s", i+1, c.Context)
		t.Run(name, func(t *testing.T) {
			var body []byte
			if c.Data != "" {
				var err error
				body, err = loadBody(caseBaseDir, c.Data)
				if err != nil {
					t.Fatalf("load body: %v", err)
				}
			}

			req, err := http.NewRequest(strings.ToUpper(c.Method), c.Endpoint, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			// ensure endpoint uses expected base
			if strings.HasPrefix(c.Endpoint, "http://localhost:6004") && baseURL != "http://localhost:6004" {
				req.URL, _ = url.Parse(strings.Replace(c.Endpoint, "http://localhost:6004", baseURL, 1))
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			if c.Token != nil {
				tok, err := getAccessToken(tokenURL, c.Token)
				if err != nil {
					t.Fatalf("token: %v", err)
				}
				req.Header.Set("Authorization", "Bearer "+tok)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			expectedStatus := c.ExpectedStatus
			if expectedStatus == 0 {
				expectedStatus = http.StatusOK
			}
			if resp.StatusCode != expectedStatus {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status %d != %d; body: %s", resp.StatusCode, expectedStatus, string(b))
			}

			if strings.ToUpper(c.Method) == http.MethodGet && c.ShouldMatch != "" {
				expectedBody, err := loadBody(caseBaseDir, c.ShouldMatch)
				if err != nil {
					t.Fatalf("read expected body: %v", err)
				}
				var want, got any
				if want, err = maybeJSON(string(expectedBody)); err != nil {
					t.Fatalf("parse expected: %v", err)
				}
				respBody, _ := io.ReadAll(resp.Body)
				if got, err = maybeJSON(string(respBody)); err != nil {
					t.Fatalf("parse response: %v", err)
				}
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("body mismatch")
				}
			}
		})
	}
}
