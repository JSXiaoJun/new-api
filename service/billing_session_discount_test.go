package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedBillingSubscription(t *testing.T, userID, subscriptionID int) {
	t.Helper()
	plan := &model.SubscriptionPlan{
		Id:            subscriptionID,
		Title:         "Discount billing plan",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		Enabled:       true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:          subscriptionID,
		UserId:      userID,
		PlanId:      plan.Id,
		AmountTotal: plan.TotalAmount,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(24 * time.Hour).Unix(),
	}).Error)
}

func TestZeroEstimatedSubscriptionChargeDoesNotCreateMinimumDeduction(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	const userID, tokenID, subscriptionID = 91, 92, 93
	seedUser(t, userID, 1_000)
	seedToken(t, tokenID, userID, "zero-subscription-token", 1_000)
	seedBillingSubscription(t, userID, subscriptionID)

	info := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "zero-subscription-token",
		RequestId: "zero-subscription-charge",
		UserSetting: dto.UserSetting{
			BillingPreference: "subscription_only",
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 0, info))
	require.NotNil(t, info.Billing)
	assert.Equal(t, BillingSourceSubscription, info.BillingSource)
	assert.Equal(t, subscriptionID, info.SubscriptionId)
	assert.Equal(t, int64(0), info.SubscriptionPreConsumed)
	assert.Equal(t, 0, info.FinalPreConsumedQuota)

	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	assert.Equal(t, int64(0), subscription.AmountUsed)
	assert.Equal(t, 1_000, getTokenRemainQuota(t, tokenID))
	var records int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Count(&records).Error)
	assert.Zero(t, records)

	require.NoError(t, info.Billing.Settle(0))
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	assert.Equal(t, int64(0), subscription.AmountUsed)
}

func TestZeroEstimatedSubscriptionChargeSettlesActualCharge(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	const userID, tokenID, subscriptionID = 94, 95, 96
	seedUser(t, userID, 1_000)
	seedToken(t, tokenID, userID, "zero-estimate-settlement-token", 1_000)
	seedBillingSubscription(t, userID, subscriptionID)

	info := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "zero-estimate-settlement-token",
		RequestId: "zero-estimate-settlement",
		UserSetting: dto.UserSetting{
			BillingPreference: "subscription_only",
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 0, info))
	require.NoError(t, info.Billing.Settle(7))

	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	assert.Equal(t, int64(7), subscription.AmountUsed)
	assert.Equal(t, 993, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(7), info.SubscriptionPostDelta)
	assert.Equal(t, 1_000, getUserQuota(t, userID))
}

func TestZeroEstimatedSubscriptionChargeDoesNotChargeWithoutTokenQuota(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	const userID, tokenID, subscriptionID = 97, 98, 99
	seedUser(t, userID, 1_000)
	seedToken(t, tokenID, userID, "zero-estimate-empty-token", 0)
	seedBillingSubscription(t, userID, subscriptionID)

	info := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "zero-estimate-empty-token",
		RequestId: "zero-estimate-empty-token",
		UserSetting: dto.UserSetting{
			BillingPreference: "subscription_only",
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 0, info))
	require.Error(t, info.Billing.Settle(7))

	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	assert.Equal(t, int64(0), subscription.AmountUsed)
	assert.Equal(t, 0, getTokenRemainQuota(t, tokenID))
}

func TestSubscriptionDiscountFallsBackToBaseChargeInsteadOfZero(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	const userID, tokenID, subscriptionID = 101, 102, 103
	seedUser(t, userID, 1_000)
	seedToken(t, tokenID, userID, "nonzero-discount-token", 1_000)
	seedBillingSubscription(t, userID, subscriptionID)

	info := &relaycommon.RelayInfo{
		UserId:                  userID,
		TokenId:                 tokenID,
		TokenKey:                "nonzero-discount-token",
		RequestId:               "nonzero-discount-charge",
		IsPlayground:            true,
		BillingDiscountResolved: true,
		BillingDiscountRatio:    0.01,
		UserSetting: dto.UserSetting{
			BillingPreference: "subscription_only",
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 1, info))
	assert.Equal(t, 1, info.FinalPreConsumedQuota)
	assert.Equal(t, int64(1), info.SubscriptionPreConsumed)
	assert.Equal(t, 1_000, getTokenRemainQuota(t, tokenID))

	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	assert.Equal(t, int64(1), subscription.AmountUsed)
}

func TestBillingSessionRejectsNegativeActualQuota(t *testing.T) {
	session := &BillingSession{}

	require.Error(t, session.Settle(-1))
}
