package model

import (
	"time"

	"gorm.io/gorm/clause"
)

// ImageAsset maps an opaque public asset ID to the image resource returned by
// the upstream image middleware.
type ImageAsset struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	AssetID   string `json:"asset_id" gorm:"type:varchar(191);uniqueIndex"`
	ChannelID int    `json:"channel_id" gorm:"index"`
	URL       string `json:"-" gorm:"type:text"`
	CreatedAt int64  `json:"created_at" gorm:"index"`
	UpdatedAt int64  `json:"updated_at"`
}

func UpsertImageAsset(assetID string, channelID int, assetURL string) error {
	if assetID == "" || assetURL == "" {
		return nil
	}
	now := time.Now().Unix()
	asset := &ImageAsset{
		AssetID:   assetID,
		ChannelID: channelID,
		URL:       assetURL,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "asset_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_id",
			"url",
			"updated_at",
		}),
	}).Create(asset).Error
}

func GetImageAssetByAssetID(assetID string) (*ImageAsset, bool, error) {
	if assetID == "" {
		return nil, false, nil
	}
	var asset *ImageAsset
	err := DB.Where("asset_id = ?", assetID).First(&asset).Error
	exists, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	return asset, true, nil
}
