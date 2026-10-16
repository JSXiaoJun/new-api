package common

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamStatus_SetEndReason_FirstWins(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	s.SetEndReason(StreamEndReasonDone, nil)
	s.SetEndReason(StreamEndReasonTimeout, nil)
	s.SetEndReason(StreamEndReasonClientGone, fmt.Errorf("context canceled"))

	assert.Equal(t, StreamEndReasonDone, s.EndReason)
	assert.Nil(t, s.EndError)
}

func TestStreamStatus_SetEndReason_WithError(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	expectedErr := fmt.Errorf("read: connection reset")
	s.SetEndReason(StreamEndReasonScannerErr, expectedErr)

	assert.Equal(t, StreamEndReasonScannerErr, s.EndReason)
	assert.Equal(t, expectedErr, s.EndError)
}

func TestStreamStatus_SetEndReason_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.SetEndReason(StreamEndReasonDone, nil)
}

func TestStreamStatus_SetEndReason_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	reasons := []StreamEndReason{
		StreamEndReasonDone,
		StreamEndReasonTimeout,
		StreamEndReasonClientGone,
		StreamEndReasonScannerErr,
		StreamEndReasonHandlerStop,
		StreamEndReasonEOF,
		StreamEndReasonPanic,
		StreamEndReasonPingFail,
	}

	var wg sync.WaitGroup
	for _, r := range reasons {
		wg.Add(1)
		go func(reason StreamEndReason) {
			defer wg.Done()
			s.SetEndReason(reason, nil)
		}(r)
	}
	wg.Wait()

	assert.NotEqual(t, StreamEndReasonNone, s.EndReason)
}

func TestStreamStatus_RecordError_Basic(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	s.RecordError("bad json")
	s.RecordError("another bad json")
	s.RecordError("client gone")

	assert.True(t, s.HasErrors())
	assert.Equal(t, 3, s.TotalErrorCount())
	assert.Len(t, s.Errors, 3)
}

func TestStreamStatus_RecordError_CapAtMax(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	for i := range 30 {
		s.RecordError(fmt.Sprintf("error_%d", i))
	}

	assert.Equal(t, maxStreamErrorEntries, len(s.Errors))
	assert.Equal(t, 30, s.TotalErrorCount())
}

func TestStreamStatus_RecordError_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.RecordError("should not panic")
}

func TestStreamStatus_RecordError_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.RecordError(fmt.Sprintf("error_%d", idx))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, s.TotalErrorCount())
	assert.LessOrEqual(t, len(s.Errors), maxStreamErrorEntries)
}

func TestStreamStatus_HasErrors_Empty(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()
	assert.False(t, s.HasErrors())
	assert.Equal(t, 0, s.TotalErrorCount())
}

func TestStreamStatus_HasErrors_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.False(t, s.HasErrors())
	assert.Equal(t, 0, s.TotalErrorCount())
}

func TestStreamStatus_IsNormalEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason StreamEndReason
		normal bool
	}{
		{StreamEndReasonDone, true},
		{StreamEndReasonEOF, true},
		{StreamEndReasonHandlerStop, true},
		{StreamEndReasonTimeout, false},
		{StreamEndReasonClientGone, false},
		{StreamEndReasonScannerErr, false},
		{StreamEndReasonPanic, false},
		{StreamEndReasonPingFail, false},
		{StreamEndReasonNone, false},
	}
	for _, tt := range tests {
		s := NewStreamStatus()
		s.SetEndReason(tt.reason, nil)
		assert.Equal(t, tt.normal, s.IsNormalEnd(), "reason=%s", tt.reason)
	}
}

func TestStreamStatus_IsNormalEnd_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.True(t, s.IsNormalEnd())
}

func TestStreamStatus_Summary(t *testing.T) {
	t.Parallel()

	s := NewStreamStatus()
	s.SetEndReason(StreamEndReasonDone, nil)
	summary := s.Summary()
	assert.Contains(t, summary, "reason=done")
	assert.NotContains(t, summary, "soft_errors")

	s2 := NewStreamStatus()
	s2.SetEndReason(StreamEndReasonTimeout, nil)
	s2.RecordError("bad json")
	s2.RecordError("write failed")
	summary2 := s2.Summary()
	assert.Contains(t, summary2, "reason=timeout")
	assert.Contains(t, summary2, "soft_errors=2")
}

func TestStreamStatus_Summary_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.Equal(t, "StreamStatus<nil>", s.Summary())
}

type timeoutTestError struct{}

func (timeoutTestError) Error() string   { return "i/o timeout" }
func (timeoutTestError) Timeout() bool   { return true }
func (timeoutTestError) Temporary() bool { return true }

func TestRelayInfoTerminationAttributesTheEndingSide(t *testing.T) {
	t.Parallel()

	type attemptState struct {
		requestSent bool
		response    bool
		failure     error
		canceled    bool
	}
	streamWith := func(reason StreamEndReason, endErr error, mark func(*StreamStatus)) *StreamStatus {
		s := NewStreamStatus()
		s.SetEndReason(reason, endErr)
		if mark != nil {
			mark(s)
		}
		return s
	}
	tests := []struct {
		name    string
		attempt attemptState
		stream  *StreamStatus
		want    *RelayTermination
	}{
		{
			name:    "client leaves while upstream has not answered",
			attempt: attemptState{requestSent: true, failure: context.Canceled, canceled: true},
			want:    &RelayTermination{EndedBy: TerminationEndedByClient, Phase: TerminationPhaseAwaiting, Reason: "canceled", RequestSent: true},
		},
		{
			name:    "client leaves before the request body is written",
			attempt: attemptState{failure: context.Canceled, canceled: true},
			want:    &RelayTermination{EndedBy: TerminationEndedByClient, Phase: TerminationPhaseSending, Reason: "canceled"},
		},
		{
			name:    "upstream closes the connection without answering",
			attempt: attemptState{requestSent: true, failure: io.EOF},
			want:    &RelayTermination{EndedBy: TerminationEndedByUpstream, Phase: TerminationPhaseAwaiting, Reason: "connection_error", RequestSent: true},
		},
		{
			name:    "relay timeout while waiting for headers",
			attempt: attemptState{requestSent: true, failure: timeoutTestError{}},
			want:    &RelayTermination{EndedBy: TerminationEndedByGateway, Phase: TerminationPhaseAwaiting, Reason: "timeout", RequestSent: true},
		},
		{
			name:    "client leaves during the stream",
			attempt: attemptState{requestSent: true, response: true, failure: errors.New("read on closed body")},
			stream:  streamWith(StreamEndReasonClientGone, context.Canceled, nil),
			want:    &RelayTermination{EndedBy: TerminationEndedByClient, Phase: TerminationPhaseReceiving, Reason: "client_gone", RequestSent: true},
		},
		{
			name:    "upstream stalls past the streaming timeout",
			attempt: attemptState{requestSent: true, response: true},
			stream:  streamWith(StreamEndReasonTimeout, nil, nil),
			want:    &RelayTermination{EndedBy: TerminationEndedByGateway, Phase: TerminationPhaseReceiving, Reason: "stream_timeout", RequestSent: true},
		},
		{
			name:    "upstream resets the stream",
			attempt: attemptState{requestSent: true, response: true},
			stream:  streamWith(StreamEndReasonScannerErr, io.ErrUnexpectedEOF, nil),
			want:    &RelayTermination{EndedBy: TerminationEndedByUpstream, Phase: TerminationPhaseReceiving, Reason: "connection_error", RequestSent: true},
		},
		{
			name:    "oversized line is a gateway limit",
			attempt: attemptState{requestSent: true, response: true},
			stream:  streamWith(StreamEndReasonScannerErr, bufio.ErrTooLong, nil),
			want:    &RelayTermination{EndedBy: TerminationEndedByGateway, Phase: TerminationPhaseReceiving, Reason: "line_too_long", RequestSent: true},
		},
		{
			name:    "stream ends at EOF without its terminal event",
			attempt: attemptState{requestSent: true, response: true},
			stream:  streamWith(StreamEndReasonEOF, nil, (*StreamStatus).RequireTerminal),
			want:    &RelayTermination{EndedBy: TerminationEndedByUpstream, Phase: TerminationPhaseReceiving, Reason: "missing_terminal", RequestSent: true},
		},
		{
			name:    "client leaving after the completion event is not a termination",
			attempt: attemptState{requestSent: true, response: true},
			stream:  streamWith(StreamEndReasonClientGone, context.Canceled, (*StreamStatus).MarkCompleted),
		},
		{
			name:    "stream timeout after an incomplete terminal is not a termination",
			attempt: attemptState{requestSent: true, response: true},
			stream: streamWith(StreamEndReasonTimeout, nil, func(s *StreamStatus) {
				s.MarkIncomplete("max_output_tokens")
			}),
		},
		{
			name:    "a failure event still counts after the completion event",
			attempt: attemptState{requestSent: true, response: true},
			stream: streamWith(StreamEndReasonDone, nil, func(s *StreamStatus) {
				s.MarkCompleted()
				s.MarkFailed("server_error", "", 0)
			}),
			want: &RelayTermination{EndedBy: TerminationEndedByUpstream, Phase: TerminationPhaseReceiving, Reason: "response_failed", RequestSent: true},
		},
		{
			name:    "completed stream is not a termination",
			attempt: attemptState{requestSent: true, response: true},
			stream: streamWith(StreamEndReasonEOF, nil, func(s *StreamStatus) {
				s.RequireTerminal()
				s.MarkCompleted()
			}),
		},
		{
			name:    "non-stream response read without failure is not a termination",
			attempt: attemptState{requestSent: true, response: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := &RelayInfo{}
			info.MarkUpstreamStart()
			attempt := info.UpstreamAttempt()
			require.NotNil(t, attempt)
			if tt.attempt.requestSent {
				attempt.MarkRequestSent()
			}
			if tt.attempt.response {
				attempt.MarkResponse()
			}
			attempt.Fail(tt.attempt.failure, tt.attempt.canceled)
			if tt.stream != nil {
				info.StreamStatus = tt.stream
				info.MarkUpstreamEnd()
			}

			got := info.Termination()
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.GreaterOrEqual(t, got.ElapsedMs, int64(0))
			got.ElapsedMs = 0
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRelayInfoTerminationKeepsTheFirstFailureAndResetsPerAttempt(t *testing.T) {
	t.Parallel()
	info := &RelayInfo{}
	info.MarkUpstreamStart()
	info.UpstreamAttempt().Fail(io.EOF, false)
	info.UpstreamAttempt().Fail(context.Canceled, true)
	assert.Equal(t, TerminationEndedByUpstream, info.Termination().EndedBy)

	info.ResetUpstreamAttempt()
	assert.Nil(t, info.Termination())

	info.MarkUpstreamStart()
	assert.Nil(t, info.Termination())
	assert.WithinDuration(t, time.Now(), info.UpstreamStartTime, time.Second)
}
