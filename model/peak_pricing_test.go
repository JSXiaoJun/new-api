package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidPeakPricingDoesNotOverwritePersistedOption(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	key := billing_setting.PeakPricingOptionKey
	var old Option
	oldErr := DB.Where(&Option{Key: key}).First(&old).Error
	common.OptionMapRWMutex.Lock()
	oldValue, hadValue := common.OptionMap[key]
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		if oldErr == nil {
			require.NoError(t, UpdateOption(key, old.Value))
		} else {
			require.NoError(t, DB.Where(&Option{Key: key}).Delete(&Option{}).Error)
			require.NoError(t, billing_setting.LoadPeakPricing(`{}`))
		}
		common.OptionMapRWMutex.Lock()
		if hadValue {
			common.OptionMap[key] = oldValue
		} else {
			delete(common.OptionMap, key)
		}
		common.OptionMapRWMutex.Unlock()
	})
	valid := `{"db-peak":{"timezone":"UTC","default":{"mode":"per_request","price":0},"periods":[{"start":"09:00","end":"18:00","tariff":{"mode":"per_second","price":0.1}}]}}`
	require.NoError(t, UpdateOption(key, valid))
	require.Error(t, UpdateOption(key, `{"db-peak":{"timezone":"UTC"}}`))
	var stored Option
	require.NoError(t, DB.Where(&Option{Key: key}).First(&stored).Error)
	assert.JSONEq(t, valid, stored.Value)
	schedule, ok := billing_setting.GetPeakPricing("db-peak")
	require.True(t, ok)
	assert.Equal(t, 0.0, *schedule.Default.Price)
}
