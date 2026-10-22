package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 退款日志记录的是正数额度，语义是「这笔消耗已经退还」。
// 用量统计与数据看板都必须把它回减，否则失败任务的预扣额度会被永久计为消耗。

func seedStatLog(t *testing.T, logType int, quota int, createdAt int64) {
	t.Helper()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		CreatedAt: createdAt,
		Type:      logType,
		ModelName: "seedance-2.5-720p",
		TokenName: "official",
		Quota:     quota,
		ChannelId: 152,
		Group:     "video",
	}).Error)
}

func TestSumUsedQuotaSubtractsRefundedQuota(t *testing.T) {
	truncateTables(t)

	// 三次消耗全部退款，净用量应当为 0（对应界面上 3 条消耗 + 3 条退款）。
	for i := 0; i < 3; i++ {
		seedStatLog(t, LogTypeConsume, 22, 1000)
		seedStatLog(t, LogTypeRefund, 22, 1001)
	}

	stat, err := SumUsedQuota(0, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, 0, stat.Quota)
}

func TestSumUsedQuotaKeepsPartialRefundNetAmount(t *testing.T) {
	truncateTables(t)

	// 差额结算：预扣 100，实际 30，退款 70，净用量 30。
	seedStatLog(t, LogTypeConsume, 100, 1000)
	seedStatLog(t, LogTypeRefund, 70, 1001)
	// 与计费无关的日志类型不得进入用量统计。
	seedStatLog(t, LogTypeTopup, 500, 1002)
	seedStatLog(t, LogTypeManage, 900, 1003)

	stat, err := SumUsedQuota(0, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, 30, stat.Quota)
}

func TestSumUsedQuotaAppliesFiltersToRefundLogs(t *testing.T) {
	truncateTables(t)

	seedStatLog(t, LogTypeConsume, 22, 1000)
	seedStatLog(t, LogTypeRefund, 22, 1001)

	// 退款发生在时间窗之外时不参与回减，窗口内仍是完整消耗。
	stat, err := SumUsedQuota(0, 900, 1000, "", "", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, 22, stat.Quota)

	// 其它筛选条件（渠道）同样必须同时作用于消耗与退款。
	stat, err = SumUsedQuota(0, 0, 0, "", "", "", 999, "")
	require.NoError(t, err)
	require.Equal(t, 0, stat.Quota)
}

func TestRecordTaskBillingLogRefundReversesDashboardQuota(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice"}).Error)

	originalExport := common.DataExportEnabled
	common.DataExportEnabled = true
	t.Cleanup(func() { common.DataExportEnabled = originalExport })

	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	params := RecordTaskBillingLogParams{
		UserId:    1,
		ChannelId: 152,
		ModelName: "seedance-2.5-720p",
		Quota:     22,
		Group:     "video",
		NodeName:  "node-a",
	}

	consume := params
	consume.LogType = LogTypeConsume
	RecordTaskBillingLog(consume)

	refund := params
	refund.LogType = LogTypeRefund
	RecordTaskBillingLog(refund)

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	// 额度被完整回减，但请求次数保留：这次调用确实发生过。
	require.Equal(t, 0, rows[0].Quota)
	require.Equal(t, 1, rows[0].Count)
}

func TestRecordTaskBillingLogSkipsDashboardWhenConsumeLogDisabled(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice"}).Error)

	originalExport := common.DataExportEnabled
	originalConsume := common.LogConsumeEnabled
	common.DataExportEnabled = true
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.DataExportEnabled = originalExport
		common.LogConsumeEnabled = originalConsume
	})

	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	// 消耗侧被禁用时从未写入看板，退款也不能单独回减出负数。
	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:    1,
		LogType:   LogTypeRefund,
		ChannelId: 152,
		ModelName: "seedance-2.5-720p",
		Quota:     22,
		Group:     "video",
	})

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Find(&rows).Error)
	require.Empty(t, rows)
}

func TestGetChannelQuotaRankNetsRefundsPerChannelAndSortsByNetQuota(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "west"}).Error)

	seed := func(channelId int, logType int, quota int, createdAt int64, prompt int, completion int) {
		require.NoError(t, LOG_DB.Create(&Log{
			UserId:           1,
			Username:         "alice",
			CreatedAt:        createdAt,
			Type:             logType,
			ModelName:        "gpt-a",
			Quota:            quota,
			PromptTokens:     prompt,
			CompletionTokens: completion,
			ChannelId:        channelId,
		}).Error)
	}
	// east: 两次消耗 100+50，退款 30，净额 120。
	seed(1, LogTypeConsume, 100, 1000, 10, 5)
	seed(1, LogTypeConsume, 50, 1500, 4, 2)
	seed(1, LogTypeRefund, 30, 1600, 0, 0)
	// west: 单次消耗 200，净额 200，应排第一。
	seed(2, LogTypeConsume, 200, 1200, 20, 8)
	// 已删除的渠道仍要出现，名称留空。
	seed(9, LogTypeConsume, 120, 1300, 1, 1)
	// 窗口为 [1000, 2000)：结束时刻与窗口外的记录、非计费类型都不计入。
	seed(2, LogTypeConsume, 999, 2000, 0, 0)
	seed(2, LogTypeConsume, 999, 999, 0, 0)
	seed(1, LogTypeError, 999, 1100, 0, 0)
	seed(1, LogTypeTopup, 999, 1100, 0, 0)

	rows, err := GetChannelQuotaRank(1000, 2000)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	assert.Equal(t, ChannelQuotaRank{ChannelId: 2, ChannelName: "west", RequestCount: 1, PromptTokens: 20, CompletionTokens: 8, ConsumedQuota: 200, NetQuota: 200}, *rows[0])
	// east 与已删除渠道 9 净额同为 120，按渠道 ID 升序。
	assert.Equal(t, ChannelQuotaRank{ChannelId: 1, ChannelName: "east", RequestCount: 2, PromptTokens: 14, CompletionTokens: 7, ConsumedQuota: 150, RefundedQuota: 30, NetQuota: 120}, *rows[1])
	assert.Equal(t, ChannelQuotaRank{ChannelId: 9, RequestCount: 1, PromptTokens: 1, CompletionTokens: 1, ConsumedQuota: 120, NetQuota: 120}, *rows[2])

	empty, err := GetChannelQuotaRank(5000, 6000)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
