package model

import (
	"errors"
	"time"
)

// HasUserSignedPurchaseAgreement reports whether the user has completed the
// one-time pre-purchase confirmation. It reads the database directly so the
// gate never depends on a stale user cache entry.
func HasUserSignedPurchaseAgreement(userId int) (bool, error) {
	if userId == 0 {
		return false, errors.New("user id is empty")
	}
	var signedAt int64
	err := DB.Model(&User{}).
		Where("id = ?", userId).
		Select("purchase_agreement_at").
		Scan(&signedAt).Error
	if err != nil {
		return false, err
	}
	return signedAt > 0, nil
}

// ConfirmUserPurchaseAgreement records the pre-purchase confirmation once.
// Repeated calls keep the original timestamp and return it.
func ConfirmUserPurchaseAgreement(userId int) (int64, error) {
	if userId == 0 {
		return 0, errors.New("user id is empty")
	}
	now := time.Now().Unix()
	err := DB.Model(&User{}).
		Where("id = ? AND (purchase_agreement_at IS NULL OR purchase_agreement_at = 0)", userId).
		Update("purchase_agreement_at", now).Error
	if err != nil {
		return 0, err
	}
	var signedAt int64
	err = DB.Model(&User{}).
		Where("id = ?", userId).
		Select("purchase_agreement_at").
		Scan(&signedAt).Error
	if err != nil {
		return 0, err
	}
	if signedAt == 0 {
		return 0, errors.New("user not found")
	}
	return signedAt, nil
}
