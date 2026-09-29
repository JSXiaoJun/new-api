package channel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contextTestAdaptor implements only the Adaptor methods used by
// DoApiRequest. The embedded interface keeps this fixture focused on request
// construction instead of requiring no-op implementations for every format.
type contextTestAdaptor struct {
	Adaptor
	baseURL string
}

func (a *contextTestAdaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL, nil
}

func (a *contextTestAdaptor) SetupRequestHeader(_ *gin.Context, _ *http.Header, _ *relaycommon.RelayInfo) error {
	return nil
}

func TestDoApiRequestPropagatesInboundContextCancellation(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)

	upstreamStarted := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// net/http only notices a client disconnect once the request body has
		// been consumed, so drain it before waiting on the request context.
		_, _ = io.ReadAll(r.Body)
		close(upstreamStarted)
		select {
		case <-r.Context().Done():
			close(upstreamCanceled)
		case <-time.After(250 * time.Millisecond):
			w.WriteHeader(http.StatusGatewayTimeout)
		}
	}))
	defer upstream.Close()

	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequestWithContext(requestContext, http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{}`)))

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	requestErrCh := make(chan error, 1)
	go func() {
		resp, err := DoApiRequest(&contextTestAdaptor{baseURL: upstream.URL}, ctx, info, bytes.NewReader([]byte(`{}`)))
		if resp != nil {
			_ = resp.Body.Close()
		}
		requestErrCh <- err
	}()

	select {
	case <-upstreamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
	cancel()

	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request context was not canceled")
	}

	select {
	case err := <-requestErrCh:
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "request error should preserve context.Canceled: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled request")
	}
}
