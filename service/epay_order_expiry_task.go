package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const epayOrderExpiryTickInterval = time.Minute

var epayOrderExpiryOnce sync.Once

// StartEpayOrderExpiryTask periodically expires Epay top-up orders (Alipay,
// WeChat and any other method) that stayed pending past their checkout
// lifetime. Only the master instance runs the sweep.
func StartEpayOrderExpiryTask() {
	epayOrderExpiryOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(epayOrderExpiryTickInterval)
			defer ticker.Stop()

			runEpayOrderExpiryOnce()
			for range ticker.C {
				runEpayOrderExpiryOnce()
			}
		})
	})
}

func runEpayOrderExpiryOnce() {
	expired, err := model.ExpireOverdueEpayTopUps(time.Now().Unix())
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("epay order expiry task failed: %v", err))
		return
	}
	if expired > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("epay order expiry task expired %d orders", expired))
	}
}
