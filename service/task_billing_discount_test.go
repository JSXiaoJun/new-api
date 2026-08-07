package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
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
