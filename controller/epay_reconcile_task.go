package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	epayReconcileTickInterval = 30 * time.Second
	// Orders are checked every tick for their checkout lifetime and then left
	// alone; a later callback is still accepted.
	epayReconcileWindow    = time.Duration(model.EpayOrderLifetimeSeconds) * time.Second
	epayReconcileBatchSize = 200
	epayReconcileWorkers   = 8
	// A tick stops early so a slow gateway can never stack ticks.
	epayReconcileTickBudget = 25 * time.Second
)

var (
	epayReconcileOnce  sync.Once
	epayReconcileRunMu sync.Mutex
	// Gateway queries run in parallel but credits are applied one at a time so
	// SQLite deployments do not hit SQLITE_BUSY between workers.
	epayReconcileCreditMu sync.Mutex
	epayReconcileQuery    = confirmEpayGatewayPayment
)

// StartEpayReconcileTask asks the gateway about recent unsettled Epay orders
// and credits the ones it confirms as paid, so an order is never stranded when
// every callback attempt fails. Only the master instance runs it.
func StartEpayReconcileTask() {
	epayReconcileOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(epayReconcileTickInterval)
			defer ticker.Stop()
			for range ticker.C {
				runEpayReconcileOnce(time.Now())
			}
		})
	})
}

type epayReconcileCandidate struct {
	tradeNo       string
	paymentMethod string
	money         float64
	subscription  bool
}

func runEpayReconcileOnce(now time.Time) {
	if !isEpayWebhookEnabled() {
		return
	}
	epayReconcileRunMu.Lock()
	defer epayReconcileRunMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), epayReconcileTickBudget)
	defer cancel()

	candidates, err := listEpayReconcileCandidates(now.Add(-epayReconcileWindow).Unix(), now.Unix())
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("易支付补单任务读取订单失败 error=%q", err.Error()))
		return
	}

	jobs := make(chan epayReconcileCandidate)
	var credited atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < epayReconcileWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if reconcileEpayOrder(ctx, candidate) {
					credited.Add(1)
				}
			}
		}()
	}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		jobs <- candidate
	}
	close(jobs)
	wg.Wait()
	if n := credited.Load(); n > 0 {
		logger.LogWarn(ctx, fmt.Sprintf("易支付补单任务补入 %d 笔订单，回调可能未送达，请检查回调链路", n))
	}
}

func listEpayReconcileCandidates(createdFrom int64, createdTo int64) ([]epayReconcileCandidate, error) {
	topUps, err := model.ListEpayTopUpsAwaitingGateway(createdFrom, createdTo, epayReconcileBatchSize)
	if err != nil {
		return nil, err
	}
	orders, err := model.ListPendingSubscriptionOrders(model.PaymentProviderEpay, createdFrom, createdTo, epayReconcileBatchSize)
	if err != nil {
		return nil, err
	}
	candidates := make([]epayReconcileCandidate, 0, len(topUps)+len(orders))
	for _, topUp := range topUps {
		candidates = append(candidates, epayReconcileCandidate{
			tradeNo: topUp.TradeNo, paymentMethod: topUp.PaymentMethod, money: topUp.Money,
		})
	}
	for _, order := range orders {
		candidates = append(candidates, epayReconcileCandidate{
			tradeNo: order.TradeNo, paymentMethod: order.PaymentMethod, money: order.Money, subscription: true,
		})
	}
	return candidates, nil
}

// reconcileEpayOrder reports whether the order was settled by this call.
func reconcileEpayOrder(ctx context.Context, candidate epayReconcileCandidate) bool {
	LockOrder(candidate.tradeNo)
	defer UnlockOrder(candidate.tradeNo)

	if err := epayReconcileQuery(ctx, candidate.tradeNo, "", candidate.money); err != nil {
		if !errors.Is(err, errEpayGatewayOrderUnpaid) {
			logger.LogWarn(ctx, fmt.Sprintf("易支付补单查单未通过 trade_no=%s error=%q", candidate.tradeNo, err.Error()))
		}
		return false
	}

	epayReconcileCreditMu.Lock()
	defer epayReconcileCreditMu.Unlock()
	if candidate.subscription {
		err := model.CompleteSubscriptionOrder(candidate.tradeNo, "", model.PaymentProviderEpay, candidate.paymentMethod)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("易支付补单确认已支付但订阅开通失败 trade_no=%s error=%q", candidate.tradeNo, err.Error()))
			return false
		}
		logger.LogInfo(ctx, fmt.Sprintf("易支付补单开通订阅 trade_no=%s", candidate.tradeNo))
		return true
	}

	alreadyDone, err := model.RechargeEpay(candidate.tradeNo, candidate.paymentMethod, "")
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("易支付补单确认已支付但入账失败 trade_no=%s error=%q", candidate.tradeNo, err.Error()))
		return false
	}
	if alreadyDone {
		return false
	}
	logger.LogInfo(ctx, fmt.Sprintf("易支付补单入账 trade_no=%s", candidate.tradeNo))
	return true
}
