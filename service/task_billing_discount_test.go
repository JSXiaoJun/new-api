package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecalculateTaskQuotaByTokensUsesSubmittedBillingSnapshot(t *testing.T) {
	task := &model.Task{
		TaskID: "task_discount_snapshot",
		Quota:  300,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				ModelRatio:  2,
				GroupRatio:  0.5,
				OtherRatios: map[string]float64{"duration": 3},
			},
		},
	}

	RecalculateTaskQuotaByTokens(context.Background(), task, 100)

	assert.Equal(t, 300, task.Quota)
}

func TestRecalculateTaskQuotaByTokensKeepsDiscountedChargeNonzero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const userQuota, tokenQuota, preConsumedQuota = 10000, 5000, 100
	seedUser(t, userID, userQuota)
	seedToken(t, tokenID, userID, "sk-discounted-fraction", tokenQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumedQuota, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelRatio:      0.1,
		GroupRatio:      0.1,
		OriginModelName: "discounted-fractional-model",
	}

	RecalculateTaskQuotaByTokens(ctx, task, 1)

	assert.Equal(t, 1, task.Quota)
	assert.Equal(t, userQuota+preConsumedQuota-1, getUserQuota(t, userID))
	assert.Equal(t, tokenQuota+preConsumedQuota-1, getTokenRemainQuota(t, tokenID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumedQuota-1, log.Quota)
}
