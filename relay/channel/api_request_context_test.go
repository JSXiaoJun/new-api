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

// The upstream transport, not timing guesses, decides which side ended an
// attempt: these cases drive a real connection into each disconnect phase.
func TestDoApiRequestRecordsWhichSideEndedTheAttempt(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		upstream    func(w http.ResponseWriter, r *http.Request, started chan<- struct{})
		cancelAfter bool
		readBody    bool
		want        relaycommon.RelayTermination
	}{
		{
			name: "client disconnects while upstream has not answered",
			upstream: func(_ http.ResponseWriter, r *http.Request, started chan<- struct{}) {
				_, _ = io.ReadAll(r.Body)
				close(started)
				<-r.Context().Done()
			},
			cancelAfter: true,
			want: relaycommon.RelayTermination{
				EndedBy: relaycommon.TerminationEndedByClient, Phase: relaycommon.TerminationPhaseAwaiting,
				Reason: "canceled", RequestSent: true,
			},
		},
		{
			name: "upstream drops the connection before answering",
			upstream: func(_ http.ResponseWriter, r *http.Request, started chan<- struct{}) {
				_, _ = io.ReadAll(r.Body)
				close(started)
				panic(http.ErrAbortHandler)
			},
			want: relaycommon.RelayTermination{
				EndedBy: relaycommon.TerminationEndedByUpstream, Phase: relaycommon.TerminationPhaseAwaiting,
				Reason: "connection_error", RequestSent: true,
			},
		},
		{
			name: "upstream drops the connection mid-body",
			upstream: func(w http.ResponseWriter, r *http.Request, started chan<- struct{}) {
				_, _ = io.ReadAll(r.Body)
				close(started)
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"partial":`))
				w.(http.Flusher).Flush()
				panic(http.ErrAbortHandler)
			},
			readBody: true,
			want: relaycommon.RelayTermination{
				EndedBy: relaycommon.TerminationEndedByUpstream, Phase: relaycommon.TerminationPhaseReceiving,
				Reason: "connection_error", RequestSent: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.upstream(w, r, started)
			}))
			defer upstream.Close()

			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequestWithContext(requestContext, http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{}`)))
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

			done := make(chan struct{})
			go func() {
				defer close(done)
				resp, err := DoApiRequest(&contextTestAdaptor{baseURL: upstream.URL}, ctx, info, bytes.NewReader([]byte(`{}`)))
				if err != nil {
					return
				}
				if tt.readBody {
					_, _ = io.ReadAll(resp.Body)
				}
				_ = resp.Body.Close()
			}()

			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for upstream request")
			}
			if tt.cancelAfter {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for the attempt to end")
			}

			got := info.Termination()
			require.NotNil(t, got)
			got.ElapsedMs = 0
			assert.Equal(t, tt.want, *got)
		})
	}
}
