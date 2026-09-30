package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidUserQuotaAdjustment = errors.New("invalid user quota adjustment")
	ErrUserQuotaPermission        = errors.New("cannot adjust quota for this user role")
)

// UserQuotaAdjustment is the immutable database snapshot of a committed manual
// adjustment. Pending relay deductions in the quota cache are not part of it.
type UserQuotaAdjustment struct {
	UserID   int
	Username string
	Before   int
	After    int
}

// AdjustUserQuota runs inside the wallet journal transaction: pending Redis
// wallet events are folded into SQL first, and the committed balance is then
// acknowledged back to Redis, so manual edits never race cached reservations.
// Balance increases are recorded as wallet credits for the audit trail.
func AdjustUserQuota(userID, operatorRole int, mode string, value int, details ...QuotaCreditMeta) (*UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != "override" && value <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if value > walletQuotaLimit || value < -walletQuotaLimit {
		return nil, ErrWalletQuotaLimitExceeded
	}
	meta := QuotaCreditMeta{}
	if len(details) > 0 {
		meta = details[0]
	}
	meta.Source = "admin_add"
	if mode == "override" {
		meta.Source = "admin_override"
	}

	var adjustment UserQuotaAdjustment
	err := withWalletTransaction(userID, func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		if user.Quota > walletQuotaLimit || user.Quota < -walletQuotaLimit {
			return ErrWalletQuotaLimitExceeded
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil || after > walletQuotaLimit || after < -walletQuotaLimit {
			return ErrWalletQuotaLimitExceeded
		}
		// An unchanged override is a successful operation, including on MySQL
		// configurations that count only changed rows in RowsAffected.
		if after != user.Quota {
			result := tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: after}
		return RecordQuotaCredit(tx, QuotaCredit{
			UserId: userID, Delta: int64(after) - int64(user.Quota), Source: meta.Source,
			RequestId: meta.RequestId, OperatorId: meta.OperatorId, Ip: meta.Ip,
		})
	})
	if err != nil {
		return nil, err
	}
	return &adjustment, nil
}
