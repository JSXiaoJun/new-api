package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchUpstreamRatiosPreservesPerSecondBillingMode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"success":true,"data":[{"model_name":"zz-sync-per-second","quota_type":1,"model_price":0.25,"billing_mode":"per_second"}]}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := fmt.Sprintf(`{"upstreams":[{"name":"test-upstream","base_url":%q}]}`, upstream.URL)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/ratio_sync/fetch", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	FetchUpstreamRatios(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Differences map[string]map[string]dto.DifferenceItem `json:"differences"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	modeDifference := response.Data.Differences["zz-sync-per-second"][billing_setting.BillingModeField]
	assert.Equal(t, "per_second", modeDifference.Upstreams["test-upstream"])
}
