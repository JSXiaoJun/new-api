package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// useAbnormalChargeDatabase points DB and LOG_DB at a fresh database of the
// given engine with the tables this feature touches, migrated twice to prove
// the migration is idempotent.
func useAbnormalChargeDatabase(t *testing.T, dialect common.DatabaseType) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case common.DatabaseTypeSQLite:
		// Same pragmas as production (common.SQLitePath).
		driver = sqlite.Open(filepath.Join(t.TempDir(), "abnormal.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	case common.DatabaseTypeMySQL:
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case common.DatabaseTypePostgreSQL:
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	tables := []any{&User{}, &QuotaCredit{}, &Log{}, &AbnormalCharge{}, &Option{}}
	require.NoError(t, db.Migrator().DropTable(tables...))
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.AutoMigrate(tables...))
	var version string
	if dialect == common.DatabaseTypeSQLite {
		require.NoError(t, db.Raw("select sqlite_version()").Scan(&version).Error)
	} else {
		require.NoError(t, db.Raw("select version()").Scan(&version).Error)
	}
	t.Logf("%s version: %s", dialect, version)

	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(dialect, dialect)
	t.Cleanup(func() {
		if dialect != common.DatabaseTypeSQLite {
			require.NoError(t, db.Migrator().DropTable(tables...))
		}
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func abnormalChargeDialects() []common.DatabaseType {
	return []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL}
}

func seedAbnormalChargeUser(t *testing.T, db *gorm.DB, id, role, quota int) {
	t.Helper()
	require.NoError(t, db.Create(&User{
		Id: id, Username: fmt.Sprintf("abnormal_%d", id), Password: "x", Role: role,
		Status: common.UserStatusEnabled, Quota: quota, AffCode: fmt.Sprintf("abn%d", id), AuthVersion: 1,
	}).Error)
}

func seedAbnormalCharges(t *testing.T, userId int, createdAt int64, count int, record AbnormalCharge) []int64 {
	t.Helper()
	ids := make([]int64, 0, count)
	for range count {
		entry := record
		entry.UserId = userId
		entry.CreatedAt = createdAt
		require.NoError(t, RecordAbnormalCharge(&entry))
		ids = append(ids, entry.Id)
	}
	return ids
}

func userQuota(t *testing.T, db *gorm.DB, id int) int {
	t.Helper()
	var user User
	require.NoError(t, db.Select("quota").First(&user, id).Error)
	return user.Quota
}

func TestAbnormalChargeSummariesFlagRepeatedUserCausedRequests(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			hour := (common.GetTimestamp()/3600 - 1) * 3600
			for _, id := range []int{11, 12, 13} {
				seedAbnormalChargeUser(t, db, id, common.RoleCommonUser, 1_000)
			}
			userCaused := AbnormalCharge{UserCaused: true, Case: "client_before_response", Outcome: AbnormalChargeOutcomeWaived, ElapsedMs: 20_000}
			// User 11: 8 user-caused requests in one hour, 2 of which received
			// output (weighted 10), and 80,000 uncollected: count + amount.
			seedAbnormalCharges(t, 11, hour+10, 6, AbnormalCharge{UserCaused: true, Case: "client_before_response", Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 10_000, ElapsedMs: 20_000})
			seedAbnormalCharges(t, 11, hour+20, 2, AbnormalCharge{UserCaused: true, Case: "client_during_response", Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 10_000, ElapsedMs: 20_100, ClientReceivedData: true})
			// Upstream faults and records outside the period never count.
			seedAbnormalCharges(t, 11, hour+30, 3, AbnormalCharge{UserCaused: false, Case: "upstream_during_response", Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 50_000})
			seedAbnormalCharges(t, 11, hour-3*86400, 4, AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 50_000})
			// User 12: a single small waiver.
			userCaused.ForgoneQuota = 500
			seedAbnormalCharges(t, 12, hour+40, 1, userCaused)
			// User 13: 10 of 12 requests in the hour ended abnormally: count +
			// ratio. A waived request still writes its zero consume log, so the
			// hour has 12 consume logs.
			userCaused.ForgoneQuota = 100
			seedAbnormalCharges(t, 13, hour+50, 10, userCaused)
			for range 12 {
				require.NoError(t, db.Create(&Log{UserId: 13, Type: LogTypeConsume, CreatedAt: hour + 60}).Error)
			}

			thresholds := AbnormalChargeThresholds{HourlyCount: 10, RatioPercent: 80, MinRequests: 10, ForgoneQuota: 60_000}
			summaries, total, err := ListAbnormalChargeUsers(hour-86400, "", common.RoleRootUser, thresholds, 0, 10)
			require.NoError(t, err)
			require.EqualValues(t, 3, total)
			require.Len(t, summaries, 3)

			byUser := map[int]*AbnormalChargeUserSummary{}
			for _, summary := range summaries {
				byUser[summary.UserId] = summary
			}
			first := byUser[11]
			assert.EqualValues(t, 8, first.Count)
			assert.EqualValues(t, 2, first.ReceivedCount)
			assert.EqualValues(t, 80_000, first.ForgoneQuota)
			assert.EqualValues(t, 80_000, first.PendingQuota)
			assert.EqualValues(t, 10, first.PeakHourWeighted)
			assert.True(t, first.Flagged)
			assert.ElementsMatch(t, []string{"hourly_count", "forgone_quota"}, first.FlagReasons)
			assert.True(t, first.FixedTiming, "disconnects at nearly the same elapsed time look scripted")
			assert.Equal(t, "abnormal_11", first.Username)

			assert.False(t, byUser[12].Flagged)
			third := byUser[13]
			assert.True(t, third.PeakHourRatioKnown)
			assert.EqualValues(t, 12, third.PeakHourRequests)
			assert.InDelta(t, 83.3, third.PeakHourRatioPct, 0.1)
			assert.ElementsMatch(t, []string{"hourly_count", "ratio"}, third.FlagReasons)
			assert.True(t, third.Flagged)

			assert.False(t, summaries[len(summaries)-1].Flagged, "flagged users sort first")
			filtered, filteredTotal, err := ListAbnormalChargeUsers(hour-86400, "abnormal_12", common.RoleRootUser, thresholds, 0, 10)
			require.NoError(t, err)
			require.EqualValues(t, 1, filteredTotal)
			assert.Equal(t, 12, filtered[0].UserId)
		})
	}
}

func TestRecoverAbnormalChargesChargesEachRecordOnce(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			now := common.GetTimestamp()
			seedAbnormalChargeUser(t, db, 21, common.RoleCommonUser, 1_000)
			record := AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 10_000}
			ids := seedAbnormalCharges(t, 21, now-60, 8, record)
			record.UserCaused, record.ForgoneQuota = false, 99_000
			upstream := seedAbnormalCharges(t, 21, now-60, 1, record)

			recovery, err := RecoverAbnormalCharges(21, 1, common.RoleRootUser, ids[:2], 0, 0)
			require.NoError(t, err)
			assert.EqualValues(t, 2, recovery.Count)
			assert.EqualValues(t, 20_000, recovery.Quota)
			assert.Equal(t, 1_000, recovery.Before)
			assert.Equal(t, -19_000, recovery.After, "a recovery may leave a negative balance")
			assert.Equal(t, -19_000, userQuota(t, db, 21))

			_, err = RecoverAbnormalCharges(21, 1, common.RoleRootUser, ids[:2], 0, 0)
			require.ErrorIs(t, err, ErrNoAbnormalChargeToRecover)
			_, err = RecoverAbnormalCharges(21, 1, common.RoleRootUser, upstream, 0, 0)
			require.ErrorIs(t, err, ErrNoAbnormalChargeToRecover, "upstream faults are never recovered from the user")
			assert.Equal(t, -19_000, userQuota(t, db, 21))

			recovery, err = RecoverAbnormalCharges(21, 1, common.RoleRootUser, nil, now-3600, 0)
			require.NoError(t, err)
			assert.EqualValues(t, 6, recovery.Count)
			assert.EqualValues(t, 60_000, recovery.Quota)
			assert.Equal(t, -79_000, userQuota(t, db, 21))

			pending, pendingTotal, err := ListAbnormalCharges(21, now-3600, "pending", 0, 20)
			require.NoError(t, err)
			require.EqualValues(t, 1, pendingTotal)
			assert.Equal(t, upstream[0], pending[0].Id)
			recovered, recoveredTotal, err := ListAbnormalCharges(21, now-3600, "recovered", 0, 3)
			require.NoError(t, err)
			assert.EqualValues(t, 8, recoveredTotal)
			assert.Len(t, recovered, 3)
			assert.NotEmpty(t, recovered[0].RecoveryId)
			assert.Equal(t, 1, recovered[0].RecoveredBy)
		})
	}
}

func TestRecoverAbnormalChargesConcurrentCallsChargeOnce(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			now := common.GetTimestamp()
			seedAbnormalChargeUser(t, db, 31, common.RoleCommonUser, 100_000)
			seedAbnormalCharges(t, 31, now-60, 5, AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeRefunded, ForgoneQuota: 3_000})

			var wg sync.WaitGroup
			results := make(chan error, 4)
			for range 4 {
				wg.Go(func() {
					_, err := RecoverAbnormalCharges(31, 1, common.RoleRootUser, nil, now-3600, 0)
					results <- err
				})
			}
			wg.Wait()
			close(results)
			succeeded := 0
			for err := range results {
				if err == nil {
					succeeded++
					continue
				}
				require.True(t, errors.Is(err, ErrNoAbnormalChargeToRecover), "unexpected error: %v", err)
			}
			assert.Equal(t, 1, succeeded)
			assert.Equal(t, 85_000, userQuota(t, db, 31))
		})
	}
}

func TestRecoverAbnormalChargesRequiresAHigherRole(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			now := common.GetTimestamp()
			seedAbnormalChargeUser(t, db, 41, common.RoleAdminUser, 5_000)
			seedAbnormalCharges(t, 41, now-60, 1, AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 1_000})

			_, err := RecoverAbnormalCharges(41, 2, common.RoleAdminUser, nil, now-3600, 0)
			require.ErrorIs(t, err, ErrAbnormalChargePermission)
			assert.Equal(t, 5_000, userQuota(t, db, 41))
			pending, total, err := ListAbnormalCharges(41, now-3600, "pending", 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total, "a rejected recovery must not claim records")
			assert.Empty(t, pending[0].RecoveryId)

			_, err = RecoverAbnormalCharges(41, 1, common.RoleRootUser, nil, now-3600, 0)
			require.NoError(t, err)
			assert.Equal(t, 4_000, userQuota(t, db, 41))
		})
	}
}

func TestRecordAbnormalChargeSkipsRecordsWithoutUncollectedQuota(t *testing.T) {
	useAbnormalChargeDatabase(t, common.DatabaseTypeSQLite)
	require.NoError(t, RecordAbnormalCharge(&AbnormalCharge{UserId: 51, ForgoneQuota: 0}))
	require.NoError(t, RecordAbnormalCharge(&AbnormalCharge{UserId: 0, ForgoneQuota: 10}))
	var count int64
	require.NoError(t, DB.Model(&AbnormalCharge{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestAbnormalChargeUsersHideUsersTheOperatorCannotManage(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			now := common.GetTimestamp()
			seedAbnormalChargeUser(t, db, 61, common.RoleCommonUser, 1_000)
			seedAbnormalChargeUser(t, db, 62, common.RoleAdminUser, 1_000)
			seedAbnormalChargeUser(t, db, 63, common.RoleRootUser, 1_000)
			for _, id := range []int{61, 62, 63} {
				seedAbnormalCharges(t, id, now-60, 1, AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 1_000})
			}
			userIds := func(summaries []*AbnormalChargeUserSummary) []int {
				ids := make([]int, 0, len(summaries))
				for _, summary := range summaries {
					ids = append(ids, summary.UserId)
				}
				slices.Sort(ids)
				return ids
			}

			summaries, total, err := ListAbnormalChargeUsers(now-3600, "", common.RoleAdminUser, AbnormalChargeThresholds{}, 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			assert.Equal(t, []int{61}, userIds(summaries))

			summaries, total, err = ListAbnormalChargeUsers(now-3600, "abnormal_6", common.RoleAdminUser, AbnormalChargeThresholds{}, 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total, "a keyword never widens what the operator sees")
			assert.Equal(t, []int{61}, userIds(summaries))

			summaries, total, err = ListAbnormalChargeUsers(now-3600, "", common.RoleRootUser, AbnormalChargeThresholds{}, 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 3, total)
			assert.Equal(t, []int{61, 62, 63}, userIds(summaries))
		})
	}
}

// Recovering a whole user charges only the records the admin reviewed, not
// ones that arrived before the confirmation.
func TestRecoverAbnormalChargesStopsAtTheReviewedRecords(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			now := common.GetTimestamp()
			seedAbnormalChargeUser(t, db, 71, common.RoleCommonUser, 10_000)
			seedAbnormalCharges(t, 71, now-120, 2, AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 1_000})
			summaries, _, err := ListAbnormalChargeUsers(now-3600, "", common.RoleRootUser, AbnormalChargeThresholds{}, 0, 10)
			require.NoError(t, err)
			require.Len(t, summaries, 1)
			reviewed := summaries[0]
			seedAbnormalCharges(t, 71, now-60, 1, AbnormalCharge{UserCaused: true, Outcome: AbnormalChargeOutcomeWaived, ForgoneQuota: 5_000})

			recovery, err := RecoverAbnormalCharges(71, 1, common.RoleRootUser, nil, now-3600, reviewed.MaxPendingId)
			require.NoError(t, err)
			assert.EqualValues(t, reviewed.PendingCount, recovery.Count)
			assert.Equal(t, reviewed.PendingQuota, recovery.Quota)
			assert.Equal(t, 8_000, userQuota(t, db, 71))
			_, pending, err := ListAbnormalCharges(71, now-3600, "pending", 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, pending, "the newer record stays pending")
		})
	}
}

// A strict billing change merges into the stored list, even when this
// node's copy is stale because another node changed it.
func TestSetStrictBillingUserKeepsChangesFromOtherNodes(t *testing.T) {
	for _, dialect := range abnormalChargeDialects() {
		t.Run(string(dialect), func(t *testing.T) {
			db := useAbnormalChargeDatabase(t, dialect)
			if common.OptionMap == nil {
				common.OptionMap = map[string]string{}
			}
			key := billing_setting.StrictBillingUsersOptionKey
			t.Cleanup(func() { require.NoError(t, billing_setting.LoadStrictBillingUsers("[]")) })

			changed, err := SetStrictBillingUser(5, true)
			require.NoError(t, err)
			assert.True(t, changed)
			// Another node adds user 9; this node's copy still holds [5].
			require.NoError(t, db.Model(&Option{}).Where(Option{Key: key}).Update("value", "[5,9]").Error)

			changed, err = SetStrictBillingUser(7, true)
			require.NoError(t, err)
			assert.True(t, changed)
			changed, err = SetStrictBillingUser(9, true)
			require.NoError(t, err)
			assert.False(t, changed, "already on in the stored list")
			changed, err = SetStrictBillingUser(5, false)
			require.NoError(t, err)
			assert.True(t, changed)

			var option Option
			require.NoError(t, db.Where(Option{Key: key}).First(&option).Error)
			assert.Equal(t, "[7,9]", option.Value)
			assert.Equal(t, []int{7, 9}, billing_setting.StrictBillingUsers())
		})
	}
}
