package service

import (
	"errors"
	"math/rand/v2"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// walletSettlementRetryDelays spaces the background retries of a failed wallet
// settlement; each later delay gets up to 50% jitter.
var walletSettlementRetryDelays = []time.Duration{0, time.Second, 5 * time.Second, 20 * time.Second}

// walletSettlementRetryWindow bounds how long after an unknown outcome the same
// operation may be retried. Redis keeps the operation key for 10 minutes; a
// retry after that could apply a landed operation a second time.
var walletSettlementRetryWindow = 5 * time.Minute

// pendingSettlement is a wallet settlement difference that failed and has not
// been applied for certain. Every retry reuses the funding's operation id.
type pendingSettlement struct {
	delta int
	// unknown: an attempt may have been applied in Redis, so only the same
	// delta may be retried (the operation id then prevents a second apply).
	unknown bool
	// ambiguous: an SQL commit failed after the change was written, so the
	// attempt may have been applied without idempotency protection.
	ambiguous bool
	// unknownSince: when the first unknown outcome was seen.
	unknownSince time.Time
	// retrying: the background retry loop owns this difference.
	retrying bool
	// inFlight: a background attempt is writing the wallet without the lock.
	inFlight bool
	// refundRequested: the request ended abnormally and called Refund.
	refundRequested bool
	// extraAttemptSpent: the one attempt allowed after an ambiguous refund.
	extraAttemptSpent bool

	tokenId      int
	tokenKey     string
	isPlayground bool
	userId       int
	requestId    string
}

// notePendingSettlementLocked records a failed wallet settlement attempt and
// starts the background retry unless the failure may already have applied the
// change without idempotency protection.
func (s *BillingSession) notePendingSettlementLocked(delta int, err error) {
	unknown := errors.Is(err, model.ErrWalletOutcomeUnknown)
	ambiguous := !unknown && !errors.Is(err, model.ErrWalletNotApplied)
	if s.pending == nil {
		s.pending = &pendingSettlement{
			tokenId:      s.relayInfo.TokenId,
			tokenKey:     s.relayInfo.TokenKey,
			isPlayground: s.relayInfo.IsPlayground,
			userId:       s.relayInfo.UserId,
			requestId:    s.relayInfo.RequestId,
		}
	}
	s.pending.delta = delta
	if unknown && !s.pending.unknown {
		s.pending.unknownSince = time.Now()
	}
	s.pending.unknown = s.pending.unknown || unknown
	s.pending.ambiguous = s.pending.ambiguous || ambiguous
	if s.pending.ambiguous {
		common.SysError(common.LogText("wallet settlement may have been applied despite an error, reconciliation required (userId=%d, requestId=%s, delta=%d): %s",
			s.pending.userId, s.pending.requestId, delta, err.Error()))
		return
	}
	if !s.pending.retrying {
		s.pending.retrying = true
		gopool.Go(s.retryPendingSettlement)
	}
}

type settlementRetryResult int

const (
	settlementRetryContinue settlementRetryResult = iota
	settlementRetryStop                           // stop retrying, the difference stays pending
	settlementRetryDone                           // nothing left to settle
)

func (s *BillingSession) retryPendingSettlement() {
retry:
	for _, delay := range walletSettlementRetryDelays {
		if delay > 0 {
			// Jitter spreads the retries of many requests failed by one outage.
			time.Sleep(delay + time.Duration(rand.Int64N(int64(delay)/2+1)))
		}
		switch s.attemptPendingSettlement() {
		case settlementRetryDone:
			return
		case settlementRetryStop:
			break retry
		}
	}
	s.mu.Lock()
	pending := s.pending
	if pending == nil || s.fundingSettled {
		s.mu.Unlock()
		return
	}
	pending.retrying = false
	refundRequested, ambiguous := pending.refundRequested, pending.ambiguous
	userId, requestId, delta := pending.userId, pending.requestId, pending.delta
	s.mu.Unlock()
	if refundRequested && s.refundDifferenceOnce() {
		return
	}
	if !ambiguous {
		common.SysError(common.LogText("wallet settlement retries exhausted, reconciliation required (userId=%d, requestId=%s, delta=%d)",
			userId, requestId, delta))
	}
}

// retryExpired reports whether an unknown outcome is too old to retry safely.
func (p *pendingSettlement) retryExpired() bool {
	return p.unknown && time.Since(p.unknownSince) > walletSettlementRetryWindow
}

// attemptPendingSettlement retries the pending difference once. The wallet
// write runs without the session lock, so Refund never waits on it; inFlight
// keeps every other settlement of this session out meanwhile.
func (s *BillingSession) attemptPendingSettlement() settlementRetryResult {
	s.mu.Lock()
	pending := s.pending
	if pending == nil || s.fundingSettled || s.settled || s.refunded {
		s.mu.Unlock()
		return settlementRetryDone
	}
	if pending.ambiguous || pending.retryExpired() {
		s.mu.Unlock()
		return settlementRetryStop
	}
	pending.inFlight = true
	delta, funding := pending.delta, s.funding
	s.mu.Unlock()

	err := funding.Settle(delta)

	s.mu.Lock()
	defer s.mu.Unlock()
	pending.inFlight = false
	if err == nil {
		common.SysLog(common.LogText("wallet settlement applied after retry (userId=%d, requestId=%s, delta=%d)",
			pending.userId, pending.requestId, pending.delta))
		s.completePendingSettlementLocked()
		return settlementRetryDone
	}
	s.notePendingSettlementLocked(pending.delta, err)
	if pending.ambiguous {
		return settlementRetryStop
	}
	return settlementRetryContinue
}

// refundDifferenceOnce makes the single attempt allowed for a refund
// difference whose commit failed, once the request has ended abnormally.
// A full refund instead could credit the user twice if that commit landed.
func (s *BillingSession) refundDifferenceOnce() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	if pending == nil || s.fundingSettled || pending.retrying || !pending.ambiguous || pending.delta >= 0 || pending.extraAttemptSpent || pending.retryExpired() {
		return false
	}
	pending.extraAttemptSpent = true
	if err := s.funding.Settle(pending.delta); err != nil {
		common.SysError(common.LogText("wallet settlement outcome still unknown after retry, reconciliation required (userId=%d, requestId=%s, delta=%d): %s",
			pending.userId, pending.requestId, pending.delta, err.Error()))
		return false
	}
	s.completePendingSettlementLocked()
	return true
}

// completePendingSettlementLocked finishes a settlement whose funding retry
// succeeded: the token quota follows the same difference.
func (s *BillingSession) completePendingSettlementLocked() {
	pending := s.pending
	s.pending = nil
	s.fundingSettled = true
	s.settled = true
	if !pending.isPlayground {
		_ = adjustTokenQuotaForSettlement(pending.tokenId, pending.tokenKey, pending.userId, pending.delta)
	}
}
