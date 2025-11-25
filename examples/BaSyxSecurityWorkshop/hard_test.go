package workshop

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

type tokenCreds struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type testConfig struct {
	Context        string      `json:"context,omitempty"`
	Method         string      `json:"method"`
	Endpoint       string      `json:"endpoint"`
	Data           string      `json:"data,omitempty"`
	ShouldMatch    string      `json:"shouldMatch,omitempty"`
	ExpectedStatus int         `json:"expectedStatus,omitempty"`
	Token          *tokenCreds `json:"token,omitempty"`
}

func loadTestConfig(path string) ([]testConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg []testConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func getAccessToken(tokenURL string, creds *tokenCreds) (string, error) {
	if creds == nil {
		return "", nil
	}
	clientID := getenvDefault("WORKSHOP_CLIENT_ID", "basyx-ui")
	clientSecret := os.Getenv("WORKSHOP_CLIENT_SECRET")

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	form.Set("username", creds.User)
	form.Set("password", creds.Password)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return tokenResp.AccessToken, nil
}

func loadBody(repoRoot, path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	p := filepath.Clean(path)
	if !filepath.IsAbs(p) {
		p = filepath.Join(repoRoot, p)
	}
	return os.ReadFile(p)
}

func maybeJSON(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func truncateDatabase(t *testing.T) {
	t.Helper()
	host := getenvDefault("WORKSHOP_DB_HOST", "localhost")
	port := getenvDefault("WORKSHOP_DB_PORT", "5432")
	user := getenvDefault("WORKSHOP_DB_USER", "admin")
	pass := getenvDefault("WORKSHOP_DB_PASSWORD", "admin123")
	name := getenvDefault("WORKSHOP_DB_NAME", "basyxTestDB")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, name)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT tablename FROM pg_tables WHERE schemaname='public'")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tn string
		if err := rows.Scan(&tn); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, tn)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(tables) == 0 {
		return
	}
	stmt := "TRUNCATE TABLE " + strings.Join(tables, ",") + " CASCADE"
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
