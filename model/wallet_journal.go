package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrWalletUnavailable = errors.New("wallet state unavailable; reconciliation required")

// ErrWalletOutcomeUnknown means Redis received a wallet mutation but its reply
// was lost, so the mutation may already be applied. Only a retry with the same
// QuotaCreditMeta.OperationId can resolve it without applying it twice.
var ErrWalletOutcomeUnknown = errors.New("wallet mutation outcome unknown")

const walletDirtyKey = "wallet:v1:dirty"

// walletQuotaLimit bounds wallet balances to the JavaScript-safe 64-bit
// domain shared with top-ups. Journal scripts only compare balances in Lua;
// every write is an exact integer (Go-formatted argument or HINCRBY), because
// Lua number formatting would turn large balances into exponent notation.
const walletQuotaLimit = common.MaxWalletQuota

func walletKeys(id int) []string {
	return []string{fmt.Sprintf("wallet:v1:%d", id), fmt.Sprintf("wallet:v1:%d:events", id), walletDirtyKey}
}

type walletEvent struct {
	Sequence int64       `json:"sequence"`
	Delta    int64       `json:"delta"`
	Credit   QuotaCredit `json:"credit"`
}

// ErrWalletNotApplied wraps a wallet mutation error when the mutation certainly
// changed nothing: it failed before reaching storage, or its SQL transaction
// body failed and rolled back. A failed commit is never wrapped, because the
// mutation may have been applied. Retrying a not-applied mutation is safe.
var ErrWalletNotApplied = errors.New("wallet mutation not applied")

// walletMutationError tags a wallet error with how certain its outcome is while
// keeping the original message and identity for every existing caller.
type walletMutationError struct {
	outcome error
	err     error
}

func (e *walletMutationError) Error() string   { return e.err.Error() }
func (e *walletMutationError) Unwrap() []error { return []error{e.outcome, e.err} }

func tagWalletOutcome(outcome, err error) error {
	if err == nil || errors.Is(err, ErrWalletNotApplied) || errors.Is(err, ErrWalletOutcomeUnknown) {
		return err
	}
	return &walletMutationError{outcome: outcome, err: err}
}

func walletNotApplied(err error) error { return tagWalletOutcome(ErrWalletNotApplied, err) }

// walletSQLTransaction reports rolledBack when the error came from the
// transaction body or from opening it, so nothing was committed.
func walletSQLTransaction(body func(*gorm.DB) error) (rolledBack bool, err error) {
	ran := false
	var bodyErr error
	err = DB.Transaction(func(tx *gorm.DB) error {
		ran = true
		bodyErr = body(tx)
		return bodyErr
	})
	return err != nil && (!ran || bodyErr != nil), err
}

// withWalletTransaction serializes SQL writers using the user row, while a
// non-expiring Redis barrier stops reservations during commit/acknowledgment.
// A subsequent flush can recover a crashed writer under that same SQL row lock.
func withWalletTransaction(id int, work func(*gorm.DB) error) error {
	_, err := walletTransaction(id, work)
	return err
}

// walletTransaction is withWalletTransaction that also reports whether a
// failure certainly left the wallet unchanged (see walletSQLTransaction).
func walletTransaction(id int, work func(*gorm.DB) error) (rolledBack bool, err error) {
	return walletTransactionRecording(id, work, nil)
}

// walletTransactionRecording also records, in Redis mode, the operation key
// that operationKey returns after a successful commit (empty: none).
func walletTransactionRecording(id int, work func(*gorm.DB) error, operationKey func() string) (rolledBack bool, err error) {
	if !common.RedisEnabled {
		return walletSQLTransaction(func(tx *gorm.DB) error {
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
			if user.Quota < -walletQuotaLimit || user.Quota > walletQuotaLimit {
				return ErrWalletQuotaLimitExceeded
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
	rolledBack, err = walletSQLTransaction(func(tx *gorm.DB) error {
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
		if applied != sequence || int64(user.Quota)+delta < -walletQuotaLimit || int64(user.Quota)+delta > walletQuotaLimit {
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
		if finalQuota < -walletQuotaLimit || finalQuota > walletQuotaLimit {
			return ErrWalletQuotaLimitExceeded
		}
		return nil
	})
	if err != nil {
		// Keep the barrier on any uncertain result. A later checkpoint-only
		// flush resolves commit ambiguity without repeating the business action.
		return rolledBack, err
	}
	// An applied operation is recorded before the barrier is lifted, so a
	// delayed copy of it can never run in between and apply it again.
	const acknowledge = `
if redis.call('HGET', KEYS[1], 'epoch') ~= ARGV[1] or redis.call('HGET', KEYS[1], 'busy') ~= ARGV[2] then return 0 end
local dirtyType = redis.call('TYPE', KEYS[3]).ok
if dirtyType ~= 'none' and dirtyType ~= 'set' then return redis.error_reply('invalid wallet dirty key') end
if KEYS[4] then redis.call('SET', KEYS[4], '1', 'EX', 600) end
redis.call('HSET', KEYS[1], 'quota', ARGV[3], 'acked', ARGV[4])
redis.call('HDEL', KEYS[1], 'busy')
redis.call('DEL', KEYS[2])
redis.call('SREM', KEYS[3], ARGV[5])
return 1`
	ackKeys := keys
	if operationKey != nil {
		if key := operationKey(); key != "" {
			ackKeys = append(append([]string{}, keys...), key)
		}
	}
	if _, err := common.RDB.Eval(ctx, acknowledge, ackKeys, epoch, token, finalQuota, finalSequence, id).Result(); err != nil {
		// SQL committed: reporting business failure could cause a second credit.
		common.SysError(fmt.Sprintf("wallet %d committed but Redis acknowledgment failed: %v", id, err))
	}
	return false, nil
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
		if err != nil || quota < -walletQuotaLimit || quota > walletQuotaLimit {
			return 0, ErrWalletUnavailable
		}
		return quota, nil
	}
	return 0, ErrWalletUnavailable
}

func journalWalletDelta(id, delta int, reserve bool, meta QuotaCreditMeta) (bool, error) {
	if _, err := getWalletQuota(id); err != nil {
		return false, walletNotApplied(err)
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
		return false, walletNotApplied(err)
	}
	operationId := meta.OperationId
	if operationId == "" {
		operationId = common.GetUUID()
	}
	keys := append(walletKeys(id), "wallet:v1:op:"+operationId)
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
if quota + delta < tonumber(ARGV[4]) or quota + delta > tonumber(ARGV[5]) then return -3 end
local event = cjson.decode(ARGV[3])
event.sequence = seq + 1
local encoded = cjson.encode(event)
redis.call('RPUSH', KEYS[2], encoded)
redis.call('HINCRBY', KEYS[1], 'quota', ARGV[1])
redis.call('HSET', KEYS[1], 'sequence', seq + 1)
redis.call('SADD', KEYS[3], ARGV[6])
redis.call('SET', KEYS[4], '1', 'EX', 600)
return 1`
	reserveArg := "0"
	if reserve {
		reserveArg = "1"
	}
	result, err := common.RDB.Eval(context.Background(), apply, keys, delta, reserveArg, string(payload), -walletQuotaLimit, walletQuotaLimit, id).Int()
	if err != nil {
		return false, tagWalletOutcome(ErrWalletOutcomeUnknown, err)
	}
	if result == -2 {
		// A flush can start between the read and the Lua mutation. Serialize
		// behind that writer instead of dropping a completed request's charge.
		var applied bool
		recordApplied := func() string {
			if applied {
				return keys[3]
			}
			return ""
		}
		rolledBack, err := walletTransactionRecording(id, func(tx *gorm.DB) error {
			query := tx.Model(&User{}).Where("id = ?", id).
				Where("quota >= ? AND quota <= ?", int64(-walletQuotaLimit)-int64(delta), int64(walletQuotaLimit)-int64(delta))
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
				return ErrWalletQuotaLimitExceeded
			}
			applied = true
			return RecordQuotaCredit(tx, event.Credit)
		}, recordApplied)
		if err != nil {
			if rolledBack {
				return false, walletNotApplied(err)
			}
			return false, err
		}
		if applied {
			// Also set here in case the acknowledgment (which records it before
			// lifting the barrier) did not run.
			if setErr := common.RDB.Set(context.Background(), keys[3], "1", 600*time.Second).Err(); setErr != nil {
				common.SysError(fmt.Sprintf("wallet %d applied fallback mutation but could not record operation: %v", id, setErr))
			}
		}
		return applied, nil
	}
	if result == -3 {
		return false, walletNotApplied(ErrWalletQuotaLimitExceeded)
	}
	if result < 0 {
		return false, walletNotApplied(ErrWalletUnavailable)
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
