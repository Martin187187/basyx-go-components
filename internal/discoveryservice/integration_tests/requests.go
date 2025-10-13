package bench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/eclipse-basyx/basyx-go-components/internal/common"
	"github.com/eclipse-basyx/basyx-go-components/internal/common/model"
	"github.com/eclipse-basyx/basyx-go-components/internal/common/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RequestClient centralizes request helpers with endpoint-aligned names.
type RequestClient struct {
	BaseURL string
}

func NewRequestClient() *RequestClient {
	return &RequestClient{BaseURL: testenv.BaseURL}
}

// POST /lookup/shells/{aasId}
func (c *RequestClient) PostLookupShellsExpect(t testing.TB, aasID string, links []model.SpecificAssetId, expect int) {
	t.Helper()
	url := fmt.Sprintf("%s/lookup/shells/%s", c.BaseURL, common.EncodeString(aasID))
	_ = testenv.PostJSONExpect(t, url, links, expect)
}

func (c *RequestClient) PostLookupShells(t testing.TB, aasID string, links []model.SpecificAssetId) {
	t.Helper()
	c.PostLookupShellsExpect(t, aasID, links, http.StatusCreated)
}

// GET /lookup/shells/{aasId}
func (c *RequestClient) GetLookupShellsExpect(t testing.TB, aasID string, expect int) []model.SpecificAssetId {
	t.Helper()
	url := fmt.Sprintf("%s/lookup/shells/%s", c.BaseURL, common.EncodeString(aasID))
	raw := testenv.GetExpect(t, url, expect)
	if expect != http.StatusOK {
		return nil
	}
	var got []model.SpecificAssetId
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal GetLookupShells response: %v", err)
	}
	return got
}

func (c *RequestClient) GetLookupShells(t testing.TB, aasID string, expect int) []model.SpecificAssetId {
	t.Helper()
	return c.GetLookupShellsExpect(t, aasID, expect)
}

// DELETE /lookup/shells/{aasId}
func (c *RequestClient) DeleteLookupShellsExpect(t testing.TB, aasID string, expect int) {
	t.Helper()
	url := fmt.Sprintf("%s/lookup/shells/%s", c.BaseURL, common.EncodeString(aasID))
	_ = testenv.DeleteExpect(t, url, expect)
}

func (c *RequestClient) DeleteLookupShells(t testing.TB, aasID string) {
	t.Helper()
	c.DeleteLookupShellsExpect(t, aasID, http.StatusNoContent)
}

// POST /lookup/shellsByAssetLink?limit=&cursor= (renamed from SearchBy)
func (c *RequestClient) LookupShellsByAssetLink(
	t testing.TB,
	pairs []model.SpecificAssetId,
	limit int,
	cursor string,
	expect int,
) model.GetAllAssetAdministrationShellIdsByAssetLink200Response {
	t.Helper()
	url := fmt.Sprintf("%s/lookup/shellsByAssetLink?limit=%d", c.BaseURL, limit)
	if cursor != "" {
		url += "&cursor=" + cursor
	}
	body := make([]map[string]string, 0, len(pairs))
	for _, p := range pairs {
		body = append(body, map[string]string{"name": p.Name, "value": p.Value})
	}
	raw := testenv.PostJSONExpect(t, url, body, expect)
	var out model.GetAllAssetAdministrationShellIdsByAssetLink200Response
	if expect == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal LookupShellsByAssetLink response: %v", err)
		}
	}
	return out
}

func PostLookupShellsSearchRawExpect(t *testing.T, body any, expect int) {
	t.Helper()
	url := fmt.Sprintf("%s/lookup/shells/search", testenv.BaseURL)
	buf, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equalf(t, expect, resp.StatusCode, "search raw post got %d body=%s", resp.StatusCode, string(buf))
}

func ensureContainsAll(t *testing.T, got []model.SpecificAssetId, want map[string][]string) {
	actual := testenv.BuildNameValuesMap(got)
	for k, wantVals := range want {
		gotVals := actual[k]
		assert.Equalf(t, wantVals, gotVals, "mismatch for name=%s", k)
	}
	assert.Subset(t, keys(actual), keys(want), "response contained extra names not expected; got=%v want=%v", keys(actual), keys(want))
	assert.Subset(t, keys(want), keys(actual), "response missing expected names; got=%v want=%v", keys(actual), keys(want))
}

func keys(m map[string][]string) (ks []string) {
	for k := range m {
		ks = append(ks, k)
	}
	return
}

func assertNoNames(t *testing.T, got []model.SpecificAssetId, forbidden ...string) {
	t.Helper()
	set := map[string]struct{}{}
	for _, s := range got {
		set[s.Name] = struct{}{}
	}
	for _, name := range forbidden {
		_, exists := set[name]
		require.Falsef(t, exists, "stale key %q still present; response=%v", name, got)
	}
}

func sortedStrings(in ...string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
