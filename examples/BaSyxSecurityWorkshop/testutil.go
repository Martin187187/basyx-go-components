package workshop

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testConfig struct {
	Context        string      `json:"context,omitempty"`
	Method         string      `json:"method"`
	Endpoint       string      `json:"endpoint"`
	Data           string      `json:"data,omitempty"`
	ShouldMatch    string      `json:"shouldMatch,omitempty"`
	ExpectedStatus int         `json:"expectedStatus,omitempty"`
	Token          *tokenCreds `json:"token,omitempty"`
}

type tokenCreds struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

func getenvDefault(key string, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// findRepoRoot walks up from cwd until it finds go.mod or fails.
func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}

// waitForRegistry waits until the registry responds (health or 200/404) at WORKSHOP_BASE_URL or default.
func waitForRegistry(t *testing.T) {
	t.Helper()

	baseURL := getenvDefault("WORKSHOP_BASE_URL", "http://localhost:6004")
	url := strings.TrimRight(baseURL, "/") + "/health"
	t.Logf("Waiting for registry health at %s", url)

	client := httpClient()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode < 500 {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("registry not healthy at %s within timeout", url)
}

// httpClient returns a reusable HTTP client with timeout.
func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
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

	client := httpClient()
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

	t.Logf("Truncating database %s on %s:%s as %s", name, host, port, user)

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
