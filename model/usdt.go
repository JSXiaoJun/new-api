package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	PaymentMethodUSDT   = "usdt"
	PaymentProviderUSDT = "usdt"

	UsdtDepositCredited    = "credited"
	UsdtDepositBelowMin    = "below_min"
	UsdtDepositSourceScan  = "scanner"
	UsdtDepositSourceClaim = "claim"
	UsdtDepositSourceAdmin = "admin"
)

// UsdtDepositAddress is a user's permanent deposit wallet on one network. The
// address is the CREATE2 wallet of the network's DepositFactory for Salt, and
// it is never reassigned: identifying the payer by address is what makes a
// deposit impossible to credit to another user.
type UsdtDepositAddress struct {
	Id     int    `json:"id"`
	UserId int    `json:"user_id" gorm:"uniqueIndex:idx_usdt_addr_user_chain,priority:1"`
	Chain  string `json:"chain" gorm:"type:varchar(16);uniqueIndex:idx_usdt_addr_user_chain,priority:2;uniqueIndex:idx_usdt_addr_chain_address,priority:1"`
	// Address is 0x-prefixed lowercase hex on every network, TRON included.
	Address   string `json:"address" gorm:"type:varchar(64);uniqueIndex:idx_usdt_addr_chain_address,priority:2"`
	Salt      string `json:"-" gorm:"type:varchar(66)"`
	CreatedAt int64  `json:"created_at"`
}

// UsdtDeposit records one verified USDT Transfer log. (chain, tx_hash,
// log_index) is unique, so a transfer is credited at most once no matter how
// many times the scanner, the user or an administrator submits it.
type UsdtDeposit struct {
	Id          int    `json:"id"`
	Chain       string `json:"chain" gorm:"type:varchar(16);uniqueIndex:idx_usdt_deposit_log,priority:1"`
	TxHash      string `json:"tx_hash" gorm:"type:varchar(80);uniqueIndex:idx_usdt_deposit_log,priority:2"`
	LogIndex    int    `json:"log_index" gorm:"uniqueIndex:idx_usdt_deposit_log,priority:3"`
	UserId      int    `json:"user_id" gorm:"index"`
	Address     string `json:"address" gorm:"type:varchar(64)"`
	FromAddress string `json:"from_address" gorm:"type:varchar(64)"`
	RawAmount   string `json:"raw_amount" gorm:"type:varchar(80)"`
	Amount      string `json:"amount" gorm:"type:varchar(40)"`
	Rate        string `json:"rate" gorm:"type:varchar(32)"`
	Quota       int64  `json:"quota" gorm:"type:bigint;default:0"`
	BlockNumber int64  `json:"block_number" gorm:"type:bigint"`
	Status      string `json:"status" gorm:"type:varchar(16);index"`
	Source      string `json:"source" gorm:"type:varchar(16)"`
	TradeNo     string `json:"trade_no" gorm:"type:varchar(128)"`
	CreatedAt   int64  `json:"created_at" gorm:"index"`
	CreditedAt  int64  `json:"credited_at"`
}

func GetUsdtDepositAddress(userId int, chain string) (*UsdtDepositAddress, error) {
	var row UsdtDepositAddress
	err := DB.Where("user_id = ? AND chain = ?", userId, chain).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func ListUserUsdtDepositAddresses(userId int) ([]UsdtDepositAddress, error) {
	var rows []UsdtDepositAddress
	err := DB.Where("user_id = ?", userId).Find(&rows).Error
	return rows, err
}

// CreateUsdtDepositAddress inserts the row, or returns the row a concurrent
// request created first.
func CreateUsdtDepositAddress(row *UsdtDepositAddress) (*UsdtDepositAddress, error) {
	row.CreatedAt = common.GetTimestamp()
	if err := DB.Create(row).Error; err != nil {
		existing, getErr := GetUsdtDepositAddress(row.UserId, row.Chain)
		if getErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	return row, nil
}

func FindUsdtDepositAddress(chain, address string) (*UsdtDepositAddress, error) {
	var row UsdtDepositAddress
	err := DB.Where("chain = ? AND address = ?", chain, strings.ToLower(address)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListUsdtDepositAddressesSince pages the scanner's watch list by id.
func ListUsdtDepositAddressesSince(sinceId int, limit int) ([]UsdtDepositAddress, error) {
	var rows []UsdtDepositAddress
	err := DB.Where("id > ?", sinceId).Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func CountUsdtDepositAddresses() (map[string]int64, error) {
	var rows []struct {
		Chain string
		Count int64
	}
	if err := DB.Model(&UsdtDepositAddress{}).Select("chain, count(*) as count").Group("chain").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Chain] = r.Count
	}
	return out, nil
}

func UsdtDepositAddressCount() (int64, error) {
	var count int64
	err := DB.Model(&UsdtDepositAddress{}).Count(&count).Error
	return count, err
}

// UsdtCreditInput is a transfer new-api has already verified on chain.
type UsdtCreditInput struct {
	Chain       string
	TxHash      string
	LogIndex    int
	UserId      int
	Address     string
	FromAddress string
	RawAmount   string
	Amount      decimal.Decimal // USDT
	Rate        decimal.Decimal // USD quota per USDT
	MinAmount   decimal.Decimal
	BlockNumber int64
	Source      string
	CallerIp    string
	// Force credits a deposit below the network minimum (administrator action).
	Force bool
}

type UsdtCreditResult struct {
	Status        string
	AlreadyDone   bool
	CreditedQuota int
	Deposit       UsdtDeposit
}

func usdtTradeNo(chain, txHash string, logIndex int) string {
	return fmt.Sprintf("USDT-%s-%s-%d", chain, strings.TrimPrefix(txHash, "0x"), logIndex)
}

// CreditUsdtDeposit records a verified transfer and, when it meets the network
// minimum, credits the owner's wallet, writes a successful top-up order and
// pays the inviter reward, all in one wallet transaction.
func CreditUsdtDeposit(in UsdtCreditInput) (UsdtCreditResult, error) {
	var result UsdtCreditResult
	if in.UserId <= 0 || in.Amount.Sign() <= 0 || in.Rate.Sign() <= 0 {
		return result, errors.New("invalid USDT deposit")
	}
	tradeNo := usdtTradeNo(in.Chain, in.TxHash, in.LogIndex)
	var quotaToAdd int
	var bonusNote *common.Message
	var inviterReward inviterTopUpReward
	var topUp *TopUp

	err := withWalletTransaction(in.UserId, func(tx *gorm.DB) error {
		var existing UsdtDeposit
		err := lockForUpdate(tx).Where("chain = ? AND tx_hash = ? AND log_index = ?", in.Chain, in.TxHash, in.LogIndex).Take(&existing).Error
		found := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if found && (existing.Status == UsdtDepositCredited || !in.Force) {
			result.AlreadyDone = true
			result.Status = existing.Status
			result.Deposit = existing
			return nil
		}
		if found && existing.UserId != in.UserId {
			return errors.New("USDT deposit belongs to another user")
		}

		deposit := existing
		if !found {
			deposit = UsdtDeposit{
				Chain: in.Chain, TxHash: in.TxHash, LogIndex: in.LogIndex, UserId: in.UserId,
				Address: strings.ToLower(in.Address), FromAddress: strings.ToLower(in.FromAddress),
				RawAmount: in.RawAmount, Amount: in.Amount.String(), BlockNumber: in.BlockNumber,
				Source: in.Source, CreatedAt: common.GetTimestamp(),
			}
		}
		if !in.Force && in.Amount.LessThan(in.MinAmount) {
			deposit.Status = UsdtDepositBelowMin
			result.Status = deposit.Status
			result.Deposit = deposit
			return tx.Create(&deposit).Error
		}

		usd := in.Amount.Mul(in.Rate)
		paidQuota, err := common.WalletQuotaFromDecimalStrict(usd.Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Truncate(0))
		if err != nil || paidQuota <= 0 {
			return ErrInvalidTopUpQuota
		}
		var bonusQuota int64
		if percent := operation_setting.TopUpBonusPercent(usd.IntPart()); percent > 0 {
			bonusQuota = decimal.NewFromInt(int64(paidQuota)).Mul(decimal.NewFromFloat(percent)).Div(decimal.NewFromInt(100)).IntPart()
		}
		now := common.GetTimestamp()
		topUp = &TopUp{
			UserId:          in.UserId,
			Amount:          usd.IntPart(),
			Money:           in.Amount.InexactFloat64(),
			TradeNo:         tradeNo,
			PaymentMethod:   PaymentMethodUSDT,
			PaymentProvider: PaymentProviderUSDT,
			CreateTime:      now,
			CompleteTime:    now,
			Status:          common.TopUpStatusSuccess,
			BonusQuota:      bonusQuota,
		}
		if quotaToAdd, err = withTopUpBonus(topUp, paidQuota); err != nil {
			return err
		}
		bonusNote = topUpBonusLogPart(quotaToAdd, paidQuota)
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		if err := creditTopUpQuota(tx, topUp, quotaToAdd, nil, QuotaCreditMeta{Ip: in.CallerIp}); err != nil {
			return err
		}
		if inviterReward, err = creditInviterTopUpReward(tx, topUp, paidQuota); err != nil {
			return err
		}

		deposit.Status = UsdtDepositCredited
		deposit.Rate = in.Rate.String()
		deposit.Quota = int64(quotaToAdd)
		deposit.TradeNo = tradeNo
		deposit.CreditedAt = now
		if found {
			deposit.Source = in.Source
			err = tx.Save(&deposit).Error
		} else {
			err = tx.Create(&deposit).Error
		}
		result.Status = deposit.Status
		result.CreditedQuota = quotaToAdd
		result.Deposit = deposit
		return err
	})
	if err != nil {
		return UsdtCreditResult{}, err
	}
	if topUp != nil && quotaToAdd > 0 {
		common.SysLog(fmt.Sprintf("USDT top-up credited trade_no=%s user_id=%d quota=%d amount=%s", tradeNo, in.UserId, quotaToAdd, in.Amount.String()))
		RecordTopupLog(in.UserId, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{
			"provider": "USDT", "quota": logger.FormatQuota(quotaToAdd), "amount": in.Amount.String() + " USDT",
		}), in.CallerIp, PaymentMethodUSDT, PaymentProviderUSDT, bonusNote)
		inviterReward.record(in.UserId)
	}
	return result, nil
}

func ListUserUsdtDeposits(userId int, offset, limit int) ([]UsdtDeposit, int64, error) {
	var rows []UsdtDeposit
	var total int64
	query := DB.Model(&UsdtDeposit{}).Where("user_id = ?", userId)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

type UsdtDepositFilter struct {
	UserId int
	Chain  string
	Status string
	TxHash string
}

func ListUsdtDeposits(filter UsdtDepositFilter, offset, limit int) ([]UsdtDeposit, int64, error) {
	var rows []UsdtDeposit
	var total int64
	query := DB.Model(&UsdtDeposit{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.Chain != "" {
		query = query.Where("chain = ?", filter.Chain)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.TxHash != "" {
		query = query.Where("tx_hash = ?", strings.ToLower(filter.TxHash))
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func GetUsdtDepositById(id int) (*UsdtDeposit, error) {
	var row UsdtDeposit
	if err := DB.Where("id = ?", id).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
