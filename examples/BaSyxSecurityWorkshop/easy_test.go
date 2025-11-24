package workshop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type simpleToken struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type simpleCase struct {
	Name           string       `json:"name"`
	Method         string       `json:"method"`
	Path           string       `json:"path"`
	ExpectedStatus int          `json:"expectedStatus"`
	Data           string       `json:"data,omitempty"`
	Token          *simpleToken `json:"token,omitempty"`
}

// TestWorkshopEasy runs the easy-level cases from tasks/easy/testcases.json.
func TestWorkshopEasy(t *testing.T) {
	truncateDatabase(t)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	casesPath := filepath.Join("tests", "easy", "testcases.json")
	b, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var cases []simpleCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatalf("parse cases: %v", err)
	}

	baseURL := getenvDefault("WORKSHOP_BASE_URL", "http://localhost:6004")
	tokenURL := getenvDefault("WORKSHOP_TOKEN_URL", "http://localhost:8080/realms/basyx/protocol/openid-connect/token")

	client := &http.Client{Timeout: 10 * time.Second}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var body []byte
			if c.Data != "" {
				path := c.Data
				if !filepath.IsAbs(path) {
					path = filepath.Join(repoRoot, path)
				}
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

func fetchToken(t *testing.T, tokenURL, user, password string) string {
	t.Helper()
	reqBody := fmt.Sprintf("grant_type=password&client_id=basyx-ui&username=%s&password=%s", user, password)
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("token req: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status %d: %s", resp.StatusCode, string(b))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tr.AccessToken == "" {
		t.Fatalf("empty access token")
	}
	return tr.AccessToken
}
