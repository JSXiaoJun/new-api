package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaCredit is a committed wallet increase, kept in the main database rather
// than the independently configured and purgeable business log database.
type QuotaCredit struct {
	Id         int64  `json:"id" gorm:"primaryKey"`
	UserId     int    `json:"user_id" gorm:"index:idx_quota_credit_user_id,priority:1"`
	CreatedAt  int64  `json:"created_at" gorm:"index:idx_quota_credit_user_id,priority:2"`
	Delta      int64  `json:"delta"`
	Source     string `json:"source" gorm:"type:varchar(64)"`
	Reference  string `json:"reference" gorm:"type:varchar(255)"`
	RequestId  string `json:"request_id" gorm:"type:varchar(255)"`
	OperatorId int    `json:"operator_id"`
	Ip         string `json:"ip" gorm:"type:varchar(64)"`
}

type QuotaCreditMeta struct {
	Source     string
	Reference  string
	RequestId  string
	OperatorId int
	Ip         string
}

// RecordQuotaCredit must share the transaction that actually adds wallet quota.
// Never call this for subscription allowance or untransferred invitation quota.
func RecordQuotaCredit(tx *gorm.DB, credit QuotaCredit) error {
	if credit.Delta <= 0 {
		return nil
	}
	if credit.UserId <= 0 {
		return errors.New("invalid quota credit user")
	}
	credit.Id = 0
	credit.CreatedAt = common.GetTimestamp()
	if credit.Source == "" {
		credit.Source = "wallet_credit"
	}
	return tx.Create(&credit).Error
}

func GetUserQuotaCredits(userId, startIdx, num int) ([]*QuotaCredit, int64, error) {
	credits := make([]*QuotaCredit, 0)
	var total int64
	query := DB.Model(&QuotaCredit{}).Where("user_id = ?", userId)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC, id DESC").Offset(startIdx).Limit(num).Find(&credits).Error
	return credits, total, err
}

// OverrideUserQuota observes the actual database value under a row lock so a
// concurrent administrator update cannot produce a fabricated credit delta.
func OverrideUserQuota(id, quota int, meta QuotaCreditMeta) (int, error) {
	if quota < 0 || quota > common.MaxQuota {
		return 0, errors.New("invalid quota")
	}
	var previous int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "quota").First(&user, id).Error; err != nil {
			return err
		}
		previous = user.Quota
		if err := tx.Model(&User{}).Where("id = ?", id).Update("quota", quota).Error; err != nil {
			return err
		}
		return RecordQuotaCredit(tx, QuotaCredit{
			UserId: id, Delta: int64(quota) - int64(previous), Source: "admin_override",
			OperatorId: meta.OperatorId, RequestId: meta.RequestId, Ip: meta.Ip,
		})
	})
	if err != nil {
		return 0, err
	}
	if err := cacheIncrUserQuota(id, int64(quota)-int64(previous)); err != nil {
		common.SysLog("failed to sync quota override to user cache: " + err.Error())
	}
	return previous, nil
}
