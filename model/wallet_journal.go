package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrWalletUnavailable = errors.New("wallet state unavailable; reconciliation required")

const walletDirtyKey = "wallet:v1:dirty"

func walletKeys(id int) []string {
	return []string{fmt.Sprintf("wallet:v1:%d", id), fmt.Sprintf("wallet:v1:%d:events", id), walletDirtyKey}
}

type walletEvent struct {
	Sequence int64       `json:"sequence"`
	Delta    int64       `json:"delta"`
	Credit   QuotaCredit `json:"credit"`
}

// withWalletTransaction serializes SQL writers using the user row, while a
// non-expiring Redis barrier stops reservations during commit/acknowledgment.
// A subsequent flush can recover a crashed writer under that same SQL row lock.
func withWalletTransaction(id int, work func(*gorm.DB) error) error {
	if !common.RedisEnabled {
		return DB.Transaction(func(tx *gorm.DB) error {
			var user User
			if err := lockForUpdate(tx).Select("id", "wallet_epoch").First(&user, id).Error; err != nil {
				return err
			}
			if user.WalletEpoch != "" {
				return ErrWalletUnavailable
			}
			if work != nil {
				if err := work(tx); err != nil {
					return err
				}
			}
			if err := tx.Select("quota").First(&user, id).Error; err != nil {
				return err
			}
			if user.Quota < common.MinQuota || user.Quota > common.MaxQuota {
				return errors.New("wallet quota limit exceeded")
			}
			return nil
		})
	}
	ctx := context.Background()
	keys := walletKeys(id)
	token := common.GetUUID()
	var finalQuota int
	var finalSequence int64
	var epoch string
	err := DB.Transaction(func(tx *gorm.DB) error {
		// SQLite has no row locks. Acquire its writer lock before reading the
		// checkpoint so two deferred transactions cannot both freeze Redis.
		if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			if err := tx.Model(&User{}).Where("id = ?", id).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
				return err
			}
		}
		var user User
		if err := lockForUpdate(tx).First(&user, id).Error; err != nil {
			return err
		}
		epoch = user.WalletEpoch
		if epoch == "" {
			epoch = common.GetUUID()
			if err := tx.Model(&User{}).Where("id = ?", id).Update("wallet_epoch", epoch).Error; err != nil {
				return err
			}
		}
		// An existing SQL epoch must never be reinitialized from an old SQL
		// balance after Redis data loss. Only first enrollment may initialize.
		const freeze = `
local stateType = redis.call('TYPE', KEYS[1]).ok
local eventsType = redis.call('TYPE', KEYS[2]).ok
local dirtyType = redis.call('TYPE', KEYS[3]).ok
if (stateType ~= 'none' and stateType ~= 'hash') or (eventsType ~= 'none' and eventsType ~= 'list') or (dirtyType ~= 'none' and dirtyType ~= 'set') then return 0 end
if ARGV[1] == '1' and redis.call('EXISTS', KEYS[1]) == 1 then
  if redis.call('HEXISTS', KEYS[1], 'busy') == 0 or redis.call('LLEN', KEYS[2]) ~= 0 then return 0 end
  redis.call('DEL', KEYS[1])
end
if redis.call('EXISTS', KEYS[1]) == 0 then
  if ARGV[1] ~= '1' then return 0 end
  redis.call('HSET', KEYS[1], 'epoch', ARGV[2], 'quota', ARGV[3], 'sequence', ARGV[4], 'acked', ARGV[4])
end
if redis.call('HGET', KEYS[1], 'epoch') ~= ARGV[2] then return 0 end
redis.call('HSET', KEYS[1], 'busy', ARGV[5])
redis.call('SADD', KEYS[3], ARGV[6])
return 1`
		initial := "0"
		if user.WalletEpoch == "" {
			initial = "1"
		}
		ok, err := common.RDB.Eval(ctx, freeze, keys, initial, epoch, user.Quota, user.WalletSequence, token, id).Int()
		if err != nil {
			return err
		}
		if ok != 1 {
			return ErrWalletUnavailable
		}
		raw, err := common.RDB.LRange(ctx, keys[1], 0, -1).Result()
		if err != nil {
			return err
		}
		state, err := common.RDB.HGetAll(ctx, keys[0]).Result()
		if err != nil {
			return err
		}
		sequence, err := strconv.ParseInt(state["sequence"], 10, 64)
		if err != nil || sequence < user.WalletSequence {
			return ErrWalletUnavailable
		}
		var delta int64
		credits := make([]QuotaCredit, 0)
		applied := user.WalletSequence
		for _, value := range raw {
			var event walletEvent
			if err := common.UnmarshalJsonStr(value, &event); err != nil {
				return err
			}
			if event.Sequence <= user.WalletSequence {
				continue
			}
			if event.Sequence != applied+1 || event.Delta > common.MaxQuota || event.Delta < -common.MaxQuota {
				return ErrWalletUnavailable
			}
			applied = event.Sequence
			delta += event.Delta
			if event.Delta > 0 {
				if event.Credit.UserId != id || event.Credit.Delta != event.Delta {
					return ErrWalletUnavailable
				}
				event.Credit.Id = 0
				credits = append(credits, event.Credit)
			}
		}
		if applied != sequence || int64(user.Quota)+delta < common.MinQuota || int64(user.Quota)+delta > common.MaxQuota {
			return ErrWalletUnavailable
		}
		if applied != user.WalletSequence {
			if err := tx.Model(&User{}).Where("id = ?", id).Updates(map[string]interface{}{
				"quota": gorm.Expr("quota + ?", delta), "wallet_sequence": applied,
			}).Error; err != nil {
				return err
			}
			if len(credits) > 0 {
				if err := tx.CreateInBatches(&credits, 80).Error; err != nil {
					return err
				}
			}
		}
		if work != nil {
			if err := work(tx); err != nil {
				return err
			}
		}
		var updated User
		if err := tx.Select("quota", "wallet_sequence").First(&updated, id).Error; err != nil {
			return err
		}
		finalQuota, finalSequence = updated.Quota, updated.WalletSequence
		if finalQuota < common.MinQuota || finalQuota > common.MaxQuota {
			return errors.New("wallet quota limit exceeded")
		}
		return nil
	})
	if err != nil {
		// Keep the barrier on any uncertain result. A later checkpoint-only
		// flush resolves commit ambiguity without repeating the business action.
		return err
	}
	const acknowledge = `
if redis.call('HGET', KEYS[1], 'epoch') ~= ARGV[1] or redis.call('HGET', KEYS[1], 'busy') ~= ARGV[2] then return 0 end
local dirtyType = redis.call('TYPE', KEYS[3]).ok
if dirtyType ~= 'none' and dirtyType ~= 'set' then return redis.error_reply('invalid wallet dirty key') end
redis.call('HSET', KEYS[1], 'quota', ARGV[3], 'acked', ARGV[4])
redis.call('HDEL', KEYS[1], 'busy')
redis.call('DEL', KEYS[2])
redis.call('SREM', KEYS[3], ARGV[5])
return 1`
	if _, err := common.RDB.Eval(ctx, acknowledge, keys, epoch, token, finalQuota, finalSequence, id).Result(); err != nil {
		// SQL committed: reporting business failure could cause a second credit.
		common.SysError(fmt.Sprintf("wallet %d committed but Redis acknowledgment failed: %v", id, err))
	}
	return nil
}

func getWalletQuota(id int) (int, error) {
	if !common.RedisEnabled {
		var user User
		if err := DB.Select("quota", "wallet_epoch").First(&user, id).Error; err != nil {
			return 0, err
		}
		if user.WalletEpoch != "" {
			return 0, ErrWalletUnavailable
		}
		return user.Quota, nil
	}
	const read = `
if redis.call('EXISTS', KEYS[1]) == 0 then return 'recover' end
if redis.call('HEXISTS', KEYS[1], 'busy') == 1 then return 'recover' end
local seq = tonumber(redis.call('HGET', KEYS[1], 'sequence'))
local acked = tonumber(redis.call('HGET', KEYS[1], 'acked'))
if not seq or not acked or redis.call('LLEN', KEYS[2]) ~= seq - acked or not redis.call('HGET', KEYS[1], 'epoch') then return 'invalid' end
return redis.call('HGET', KEYS[1], 'quota') or 'invalid'`
	for attempt := 0; attempt < 2; attempt++ {
		value, err := common.RDB.Eval(context.Background(), read, walletKeys(id)).Text()
		if err != nil {
			return 0, err
		}
		if value == "recover" && attempt == 0 {
			if err := withWalletTransaction(id, nil); err != nil {
				return 0, err
			}
			continue
		}
		quota, err := strconv.Atoi(value)
		if err != nil || quota < common.MinQuota || quota > common.MaxQuota {
			return 0, ErrWalletUnavailable
		}
		return quota, nil
	}
	return 0, ErrWalletUnavailable
}

func journalWalletDelta(id, delta int, reserve bool, meta QuotaCreditMeta) (bool, error) {
	if _, err := getWalletQuota(id); err != nil {
		return false, err
	}
	event := walletEvent{Delta: int64(delta)}
	if delta > 0 {
		if meta.Source == "" {
			meta.Source = "wallet_credit"
		}
		event.Credit = QuotaCredit{UserId: id, Delta: int64(delta), CreatedAt: common.GetTimestamp(),
			Source: meta.Source, Reference: meta.Reference, RequestId: meta.RequestId, OperatorId: meta.OperatorId, Ip: meta.Ip}
	}
	payload, err := common.Marshal(event)
	if err != nil {
		return false, err
	}
	keys := append(walletKeys(id), "wallet:v1:op:"+common.GetUUID())
	const apply = `
local previous = redis.call('GET', KEYS[4])
if previous then return tonumber(previous) end
local dirtyType = redis.call('TYPE', KEYS[3]).ok
if dirtyType ~= 'none' and dirtyType ~= 'set' then return -1 end
if redis.call('HEXISTS', KEYS[1], 'busy') == 1 then return -2 end
local quota = tonumber(redis.call('HGET', KEYS[1], 'quota'))
local seq = tonumber(redis.call('HGET', KEYS[1], 'sequence'))
local acked = tonumber(redis.call('HGET', KEYS[1], 'acked'))
if not quota or not seq or not acked or seq >= 1000000000000 or redis.call('LLEN', KEYS[2]) ~= seq - acked then return -1 end
if seq - acked >= 4096 then return -2 end
local delta = tonumber(ARGV[1])
if ARGV[2] == '1' and quota + delta < 0 then return 0 end
if quota + delta < tonumber(ARGV[4]) or quota + delta > tonumber(ARGV[5]) then return -1 end
local event = cjson.decode(ARGV[3])
event.sequence = seq + 1
local encoded = cjson.encode(event)
redis.call('RPUSH', KEYS[2], encoded)
redis.call('HSET', KEYS[1], 'quota', quota + delta, 'sequence', seq + 1)
redis.call('SADD', KEYS[3], ARGV[6])
redis.call('SET', KEYS[4], '1', 'EX', 600)
return 1`
	reserveArg := "0"
	if reserve {
		reserveArg = "1"
	}
	result, err := common.RDB.Eval(context.Background(), apply, keys, delta, reserveArg, string(payload), common.MinQuota, common.MaxQuota, id).Int()
	if err != nil {
		return false, err
	}
	if result == -2 {
		// A flush can start between the read and the Lua mutation. Serialize
		// behind that writer instead of dropping a completed request's charge.
		var applied bool
		err := withWalletTransaction(id, func(tx *gorm.DB) error {
			query := tx.Model(&User{}).Where("id = ?", id).
				Where("quota >= ? AND quota <= ?", int64(common.MinQuota)-int64(delta), int64(common.MaxQuota)-int64(delta))
			if reserve {
				query = query.Where("quota >= ?", -delta)
			}
			result := query.Update("quota", gorm.Expr("quota + ?", delta))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				if reserve {
					return nil
				}
				return errors.New("wallet quota limit exceeded")
			}
			applied = true
			return RecordQuotaCredit(tx, event.Credit)
		})
		return applied && err == nil, err
	}
	if result < 0 {
		return false, ErrWalletUnavailable
	}
	if result == 0 {
		return false, nil
	}
	if !common.BatchUpdateEnabled {
		if err := withWalletTransaction(id, nil); err != nil {
			common.SysError(fmt.Sprintf("wallet %d accepted mutation awaiting flush: %v", id, err))
		}
	}
	return true, nil
}

// FlushWalletJournals is also run without accounting batching: it recovers a
// process that stopped between a SQL commit and Redis acknowledgment.
func FlushWalletJournals() {
	if !common.RedisEnabled {
		return
	}
	var cursor uint64
	for {
		ids, next, err := common.RDB.SScan(context.Background(), walletDirtyKey, cursor, "*", 100).Result()
		if err != nil {
			common.SysError("wallet journal scan failed: " + err.Error())
			return
		}
		for _, value := range ids {
			id, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			if err := withWalletTransaction(id, nil); err != nil {
				common.SysError(fmt.Sprintf("wallet %d journal flush failed: %v", id, err))
			}
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}
