package workshop

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type hardCase struct {
	Name           string `json:"name"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Data           string `json:"data,omitempty"`
	ExpectedStatus int    `json:"expectedStatus"`
	Token          *struct {
		User     string `json:"user"`
		Password string `json:"password"`
	} `json:"token,omitempty"`
}

// TestWorkshophard runs hard-level cases defined in tests/hard/testcases.json.
func TestWorkshophard(t *testing.T) {
	truncateDatabase(t)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	casesPath := filepath.Join("tests", "hard", "testcases.json")
	raw, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var cases []hardCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse cases: %v", err)
	}

	baseURL := getenvDefault("WORKSHOP_BASE_URL", "http://localhost:6004")
	tokenURL := getenvDefault("WORKSHOP_TOKEN_URL", "http://localhost:8080/realms/basyx/protocol/openid-connect/token")

	client := httpClient()

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var body []byte
			if c.Data != "" {
				path := c.Data
				if !filepath.IsAbs(path) {
					path = filepath.Join(repoRoot, path)
				}
				var err error
				body, err = os.ReadFile(path)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
			}

			req, err := http.NewRequest(c.Method, baseURL+c.Path, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			if c.Token != nil {
				tok := fetchToken(t, tokenURL, c.Token.User, c.Token.Password)
				req.Header.Set("Authorization", "Bearer "+tok)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != c.ExpectedStatus {
				rb, _ := io.ReadAll(resp.Body)
				t.Fatalf("status %d != %d; body: %s", resp.StatusCode, c.ExpectedStatus, string(rb))
			}
		})
	}
}
