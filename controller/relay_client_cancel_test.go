package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newRetryTestContext(t *testing.T) (*gin.Context, context.CancelFunc) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx, cancel := context.WithCancel(req.Context())
	t.Cleanup(cancel)
	c.Request = req.WithContext(ctx)
	return c, cancel
}

func TestShouldRetryStopsAfterClientDisconnect(t *testing.T) {
	// A canceled upstream call surfaces as a retryable 500 do-request failure
	apiErr := types.NewErrorWithStatusCode(errors.New("context canceled"),
		types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)

	c, cancel := newRetryTestContext(t)
	assert.True(t, shouldRetry(c, apiErr, 3), "a live client still gets retries")

	cancel()
	assert.False(t, shouldRetry(c, apiErr, 3), "a disconnected client must not be retried on another channel")
}
