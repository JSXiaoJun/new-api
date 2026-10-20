package common

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http/httptrace"
	"sync"
	"time"
)

// Sides that can end a relay attempt abnormally.
const (
	TerminationEndedByClient   = "client"
	TerminationEndedByUpstream = "upstream"
	TerminationEndedByGateway  = "gateway"
)

// Phases of one upstream HTTP attempt.
const (
	TerminationPhaseSending   = "sending"
	TerminationPhaseAwaiting  = "awaiting_response"
	TerminationPhaseReceiving = "receiving"
)

// UpstreamAttempt records how far one upstream HTTP attempt progressed. The
// transport reports request and response progress from its own goroutines,
// so every field is guarded by mu.
type UpstreamAttempt struct {
	mu             sync.Mutex
	frozen         bool
	startedAt      time.Time
	requestSentAt  time.Time
	responseAt     time.Time
	failedAt       time.Time
	failure        error
	clientCanceled bool
}

type upstreamAttemptSnapshot struct {
	startedAt      time.Time
	requestSentAt  time.Time
	responseAt     time.Time
	failedAt       time.Time
	failure        error
	clientCanceled bool
}

// Trace attaches transport hooks that mark when the request was fully written
// and when the first response byte arrived.
func (a *UpstreamAttempt) Trace(ctx context.Context) context.Context {
	if a == nil {
		return ctx
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				a.MarkRequestSent()
			}
		},
		GotFirstResponseByte: a.MarkResponse,
	})
}

// MarkRequestSent records that the whole request reached the upstream
// transport.
func (a *UpstreamAttempt) MarkRequestSent() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.requestSentAt.IsZero() && !a.frozen {
		a.requestSentAt = time.Now()
	}
}

// Freeze stops later transport progress from changing the attempt, after the
// client abandoned it and its reply continues only in the background.
func (a *UpstreamAttempt) Freeze() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.frozen = true
}

// MarkResponse records that upstream started answering.
func (a *UpstreamAttempt) MarkResponse() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.responseAt.IsZero() && !a.frozen {
		a.responseAt = time.Now()
	}
}

// Fail records the first transport failure of the attempt. clientCanceled
// reports that the inbound request context was canceled, which only happens
// when the client connection went away.
func (a *UpstreamAttempt) Fail(err error, clientCanceled bool) {
	if a == nil || err == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failure != nil {
		return
	}
	a.failure = err
	a.failedAt = time.Now()
	a.clientCanceled = clientCanceled
}

// RespondedBefore reports whether upstream had started answering before t.
func (a *UpstreamAttempt) RespondedBefore(t time.Time) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.responseAt.IsZero() && a.responseAt.Before(t)
}

// Responded reports whether upstream started answering at all.
func (a *UpstreamAttempt) Responded() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.responseAt.IsZero()
}

// RequestSent reports whether the whole request reached the upstream
// transport.
func (a *UpstreamAttempt) RequestSent() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.requestSentAt.IsZero() || !a.responseAt.IsZero()
}

// Failure returns the first transport failure recorded for the attempt.
func (a *UpstreamAttempt) Failure() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.failure
}

func (a *UpstreamAttempt) snapshot() upstreamAttemptSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return upstreamAttemptSnapshot{
		startedAt:      a.startedAt,
		requestSentAt:  a.requestSentAt,
		responseAt:     a.responseAt,
		failedAt:       a.failedAt,
		failure:        a.failure,
		clientCanceled: a.clientCanceled,
	}
}

// RelayTermination describes which side ended an attempt that did not finish
// normally. It holds classification facts only; error text stays with the
// caller, which decides who may see it.
type RelayTermination struct {
	EndedBy     string `json:"ended_by"`
	Phase       string `json:"phase"`
	Reason      string `json:"reason"`
	RequestSent bool   `json:"request_sent"`
	ElapsedMs   int64  `json:"elapsed_ms"`
}

// UpstreamAttempt returns the attempt started by the last MarkUpstreamStart.
func (info *RelayInfo) UpstreamAttempt() *UpstreamAttempt {
	if info == nil {
		return nil
	}
	return info.upstreamAttempt
}

// Termination classifies how the last upstream attempt ended. It returns nil
// when the attempt finished normally or never reached the upstream transport.
func (info *RelayInfo) Termination() *RelayTermination {
	if info == nil || info.upstreamAttempt == nil {
		return nil
	}
	s := info.upstreamAttempt.snapshot()
	t := &RelayTermination{
		Phase:       TerminationPhaseSending,
		RequestSent: !s.requestSentAt.IsZero() || !s.responseAt.IsZero(),
	}
	if t.RequestSent {
		t.Phase = TerminationPhaseAwaiting
	}
	if !s.responseAt.IsZero() {
		t.Phase = TerminationPhaseReceiving
	}

	// A stream that started is classified by why the scanner stopped; reads
	// that fail after the gateway closed the body are only side effects.
	if ss := info.StreamStatus; ss != nil {
		if !info.UpstreamEndTime.IsZero() && !info.UpstreamEndTime.Before(s.startedAt) {
			t.ElapsedMs = info.UpstreamEndTime.Sub(s.startedAt).Milliseconds()
		}
		outcome := ss.OutcomeSnapshot()
		// Upstream already delivered its terminal event, so whatever closed the
		// transport afterwards did not cut the response short.
		if outcome.Response == ResponseOutcomeCompleted || outcome.Response == ResponseOutcomeIncomplete {
			return nil
		}
		switch outcome.EndReason {
		case StreamEndReasonClientGone, StreamEndReasonPingFail:
			if errors.Is(ss.EndError, context.DeadlineExceeded) {
				t.EndedBy, t.Reason = TerminationEndedByGateway, "timeout"
			} else {
				t.EndedBy, t.Reason = TerminationEndedByClient, string(outcome.EndReason)
			}
		case StreamEndReasonTimeout:
			t.EndedBy, t.Reason = TerminationEndedByGateway, "stream_timeout"
		case StreamEndReasonPanic:
			t.EndedBy, t.Reason = TerminationEndedByGateway, "panic"
		case StreamEndReasonScannerErr:
			switch {
			case isTimeoutError(ss.EndError):
				t.EndedBy, t.Reason = TerminationEndedByGateway, "timeout"
			case errors.Is(ss.EndError, bufio.ErrTooLong):
				t.EndedBy, t.Reason = TerminationEndedByGateway, "line_too_long"
			default:
				t.EndedBy, t.Reason = TerminationEndedByUpstream, "connection_error"
			}
		default:
			switch {
			case outcome.Response == ResponseOutcomeFailed:
				t.EndedBy, t.Reason = TerminationEndedByUpstream, "response_failed"
			case outcome.ExpectsTerminal && outcome.Response == ResponseOutcomeUnknown && outcome.EndReason != StreamEndReasonDone:
				t.EndedBy, t.Reason = TerminationEndedByUpstream, "missing_terminal"
			default:
				return nil
			}
		}
		return t
	}

	if s.failure == nil {
		return nil
	}
	t.ElapsedMs = s.failedAt.Sub(s.startedAt).Milliseconds()
	switch {
	case s.clientCanceled:
		t.EndedBy, t.Reason = TerminationEndedByClient, "canceled"
	case isTimeoutError(s.failure):
		t.EndedBy, t.Reason = TerminationEndedByGateway, "timeout"
	default:
		t.EndedBy, t.Reason = TerminationEndedByUpstream, "connection_error"
	}
	return t
}

// isTimeoutError covers the relay client's own deadlines (RelayTimeout and
// the transport's response header timeout).
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
