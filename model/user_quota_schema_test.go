package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestEnsureUserQuotaColumnsUpgradesLegacySchema covers upgrading a wallet
// created by a 32-bit build: worker nodes refuse to start, the master widens
// every quota column losslessly, and the upgraded column holds 64-bit balances.
func TestEnsureUserQuotaColumnsUpgradesLegacySchema(t *testing.T) {
	for _, dialect := range []struct {
		name   string
		env    string
		dbType common.DatabaseType
		alter  string
	}{
		{"mysql", "TEST_MYSQL_DSN", common.DatabaseTypeMySQL, "ALTER TABLE users MODIFY COLUMN %s int DEFAULT 0"},
		{"postgres", "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL, "ALTER TABLE users ALTER COLUMN %s TYPE integer"},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dsn == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			t.Setenv("QUOTA_SCHEMA_TEST_DSN", dsn)
			db, dbType, err := chooseDB("QUOTA_SCHEMA_TEST_DSN", false)
			require.NoError(t, err)
			require.Equal(t, dialect.dbType, dbType)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			require.NoError(t, db.Migrator().DropTable(&User{}))
			t.Cleanup(func() { _ = db.Migrator().DropTable(&User{}) })
			require.NoError(t, db.AutoMigrate(&User{}))
			for _, column := range userQuotaColumns {
				require.NoError(t, db.Exec(strings.Replace(dialect.alter, "%s", column, 1)).Error)
			}
			legacy := User{Username: "legacy-wallet", Password: "placeholder", AffCode: "legacy-aff", Quota: common.MaxQuota, UsedQuota: 12, AffQuota: 34, AffHistoryQuota: 56}
			require.NoError(t, db.Create(&legacy).Error)

			previousMaster := common.IsMasterNode
			t.Cleanup(func() { common.IsMasterNode = previousMaster })

			common.IsMasterNode = false
			err = ensureUserQuotaColumns(db, dbType)
			require.Error(t, err, "worker nodes must not start on a 32-bit wallet")
			assert.Contains(t, err.Error(), "32-bit is not supported")

			common.IsMasterNode = true
			// Every legacy column is widened by a single table rewrite.
			alters := 0
			require.NoError(t, db.Callback().Raw().After("gorm:raw").Register("test:count_quota_alters", func(tx *gorm.DB) {
				if strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "ALTER TABLE") {
					alters++
				}
			}))
			require.NoError(t, ensureUserQuotaColumns(db, dbType))
			require.NoError(t, db.Callback().Raw().Remove("test:count_quota_alters"))
			assert.Equal(t, 1, alters)
			columnTypes, err := db.Migrator().ColumnTypes(&User{})
			require.NoError(t, err)
			for _, column := range userQuotaColumns {
				found := false
				for _, actual := range columnTypes {
					if strings.EqualFold(actual.Name(), column) {
						found = true
						assert.True(t, is64BitIntegerType(dbType, actual.DatabaseTypeName()), "users.%s is %s", column, actual.DatabaseTypeName())
					}
				}
				assert.True(t, found, "users.%s missing", column)
			}

			var upgraded User
			require.NoError(t, db.First(&upgraded, legacy.Id).Error)
			assert.Equal(t, common.MaxQuota, upgraded.Quota)
			assert.Equal(t, 12, upgraded.UsedQuota)
			assert.Equal(t, 34, upgraded.AffQuota)
			assert.Equal(t, 56, upgraded.AffHistoryQuota)
			require.NoError(t, db.Model(&User{}).Where("id = ?", legacy.Id).Update("quota", common.MaxWalletQuota).Error)
			require.NoError(t, db.First(&upgraded, legacy.Id).Error)
			assert.Equal(t, common.MaxWalletQuota, upgraded.Quota)

			// A second start is a no-op, including on worker nodes.
			common.IsMasterNode = false
			require.NoError(t, ensureUserQuotaColumns(db, dbType))
			require.NoError(t, db.AutoMigrate(&User{}))
		})
	}
}
