package gethexec

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbos"
)

var (
	errResidentMessageIdentity = errors.New("resident post-StartBlock message identity is incomplete")
	errResidentSenderRead      = errors.New("resident post-StartBlock sender read failed")
	errResidentSenderEncoding  = errors.New("resident post-StartBlock sender encoding failed")
)

// ResidentSenderSnapshot is the exact account view needed to seed sender
// recovery after StartBlock. Code and balance are copied so the snapshot is
// independent of the mutable StateDB passed to the observer.
type ResidentSenderSnapshot struct {
	Address  common.Address
	Nonce    uint64
	Balance  *big.Int
	CodeHash common.Hash
	Code     []byte
	Exists   bool
}

// ResidentPostStartRecord is retained only after the corresponding message
// has been appended as canonical. It is intentionally independent of the
// transport/protobuf layer; the producer can later encode it at the commit
// boundary without re-reading StateDB.
type ResidentPostStartRecord struct {
	MessageIndex      uint64
	MessageDigest     common.Hash
	TransactionCount  uint64
	ParentBlockNumber uint64
	ChildBlockNumber  uint64
	Senders           []ResidentSenderSnapshot
}

type residentPostStartContext struct {
	messageIndex  uint64
	messageDigest common.Hash
	signer        types.Signer
}

type residentPostStartEntry struct {
	parentHash common.Hash
	childHash  common.Hash
	record     ResidentPostStartRecord
	canonical  bool
}

// ResidentPostStartStateStore owns the small set of resident records waiting
// for appendBlock to make their message canonical. It does not emit or apply
// any state delta yet.
type ResidentPostStartStateStore struct {
	mu      sync.RWMutex
	entries []*residentPostStartEntry
	// failures counts messages whose observer latched an error and therefore
	// retained no record. The export layer sees those messages as gaps; the
	// counter makes the discontinuity observable without ever influencing
	// block construction.
	failures uint64
}

func NewResidentPostStartStateStore() *ResidentPostStartStateStore {
	return &ResidentPostStartStateStore{}
}

func (s *ResidentPostStartStateStore) Observer(messageIndex uint64, messageDigest common.Hash, signer types.Signer) (*residentPostStartObserver, error) {
	if messageDigest == (common.Hash{}) || signer == nil {
		return nil, errResidentMessageIdentity
	}
	return &residentPostStartObserver{
		store: s,
		context: residentPostStartContext{
			messageIndex:  messageIndex,
			messageDigest: messageDigest,
			signer:        signer,
		},
	}, nil
}

func (s *ResidentPostStartStateStore) MarkCanonical(block *types.Block) {
	if block == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if entry.parentHash == block.ParentHash() && entry.record.ChildBlockNumber == block.NumberU64() {
			entry.canonical = true
		}
	}
	// A resident record is useful only while its block is the current child;
	// retain the most recent bounded window until the consumer is wired.
	if len(s.entries) > 16 {
		s.entries = append([]*residentPostStartEntry(nil), s.entries[len(s.entries)-16:]...)
	}
}

// Clear drops every retained record and counts nothing. Called on reorg:
// records keyed to orphaned blocks must never serve sender snapshots.
func (s *ResidentPostStartStateStore) Clear() {
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
}

func (s *ResidentPostStartStateStore) noteFailure() {
	s.mu.Lock()
	s.failures++
	s.mu.Unlock()
}

// FailureCount reports how many messages latched an observer error and were
// therefore retained as gaps rather than records.
func (s *ResidentPostStartStateStore) FailureCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failures
}

// noteResidentObserverFailure logs and counts a latched resident observer
// failure without ever propagating it into block construction: a resident
// authority observer must fail itself, never the chain executor
// (docs/revm-ship-readiness-assessment-2026-07-21.md). The failed message
// simply retains no record, so the export layer sees a gap for it.
func noteResidentObserverFailure(observers []*residentPostStartObserver) {
	if len(observers) == 0 || observers[0] == nil {
		return
	}
	if err := observers[0].Error(); err != nil {
		observers[0].store.noteFailure()
		log.Warn("resident post-start observer failed; block construction unaffected", "err", err)
	}
}

func (s *ResidentPostStartStateStore) LatestCanonical() (ResidentPostStartRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].canonical {
			return cloneResidentRecord(s.entries[i].record), true
		}
	}
	return ResidentPostStartRecord{}, false
}

type combinedStartBlockObserver struct {
	retained arbos.StartBlockObserver
	resident *residentPostStartObserver
}

func (o *combinedStartBlockObserver) StartBlockApplied(header *types.Header, statedb *state.StateDB, tx *types.Transaction) {
	if o.retained != nil {
		o.retained.StartBlockApplied(header, statedb, tx)
	}
	if o.resident != nil {
		o.resident.StartBlockApplied(header, statedb, tx)
	}
}

func (o *combinedStartBlockObserver) StartBlockAppliedWithTransactions(header *types.Header, statedb *state.StateDB, tx *types.Transaction, txes types.Transactions, authoritative bool) {
	if o.retained != nil {
		o.retained.StartBlockApplied(header, statedb, tx)
	}
	if o.resident != nil {
		o.resident.StartBlockAppliedWithTransactions(header, statedb, tx, txes, authoritative)
	}
}

type residentPostStartObserver struct {
	store   *ResidentPostStartStateStore
	context residentPostStartContext
	errMu   sync.Mutex
	err     error
}

// StartBlockApplied preserves the base observer interface. The richer method
// is selected by arbos when ProduceBlock has the exact parsed tx prefix.
func (o *residentPostStartObserver) StartBlockApplied(header *types.Header, statedb *state.StateDB, tx *types.Transaction) {
	o.StartBlockAppliedWithTransactions(header, statedb, tx, nil, false)
}

func (o *residentPostStartObserver) StartBlockAppliedWithTransactions(header *types.Header, statedb *state.StateDB, _ *types.Transaction, txes types.Transactions, authoritative bool) {
	// A resident observer must fail itself, never the chain executor: this
	// method runs inside block production, so any panic in capture latches an
	// error instead of unwinding into the state transition.
	defer func() {
		if r := recover(); r != nil {
			o.setError(fmt.Errorf("resident post-start observer panic: %v", r))
		}
	}()
	if header == nil || header.Number == nil || statedb == nil || !authoritative {
		o.setError(errResidentSenderRead)
		return
	}
	addresses, err := uniqueSenders(txes, o.context.signer)
	if err != nil {
		o.setError(err)
		return
	}
	// Snapshot from a COPY: geth state getters latch read errors (missing
	// trie/code nodes) into the StateDB they run on, and a latched error on
	// the live instance fails the canonical block at commit. The copy
	// absorbs any such poison and is discarded.
	reader := statedb.Copy()
	snapshots := make([]ResidentSenderSnapshot, 0, len(addresses))
	for _, address := range addresses {
		snapshot, err := snapshotSender(reader, address)
		if err != nil {
			o.setError(err)
			return
		}
		snapshots = append(snapshots, snapshot)
	}
	childNumber := header.Number.Uint64()
	parentNumber := uint64(0)
	if childNumber > 0 {
		parentNumber = childNumber - 1
	}
	o.store.mu.Lock()
	o.store.entries = append(o.store.entries, &residentPostStartEntry{
		parentHash: header.ParentHash,
		childHash:  header.Hash(),
		record: ResidentPostStartRecord{
			MessageIndex:      o.context.messageIndex,
			MessageDigest:     o.context.messageDigest,
			TransactionCount:  uint64(len(txes)),
			ParentBlockNumber: parentNumber,
			ChildBlockNumber:  childNumber,
			Senders:           snapshots,
		},
	})
	o.store.mu.Unlock()
}

func (o *residentPostStartObserver) setError(err error) {
	o.errMu.Lock()
	if o.err == nil {
		o.err = err
	}
	o.errMu.Unlock()
}

func (o *residentPostStartObserver) Error() error {
	o.errMu.Lock()
	defer o.errMu.Unlock()
	return o.err
}
func uniqueSenders(txes types.Transactions, signer types.Signer) ([]common.Address, error) {
	if signer == nil {
		return nil, errResidentSenderEncoding
	}
	seen := make(map[common.Address]struct{}, len(txes))
	addresses := make([]common.Address, 0, len(txes))
	for _, tx := range txes {
		if tx == nil {
			return nil, errResidentSenderEncoding
		}
		address, err := types.Sender(signer, tx)
		if err != nil {
			return nil, errResidentSenderEncoding
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

func snapshotSender(statedb *state.StateDB, address common.Address) (ResidentSenderSnapshot, error) {
	if statedb.Error() != nil {
		return ResidentSenderSnapshot{}, errResidentSenderRead
	}
	balance := statedb.GetBalance(address)
	if balance == nil {
		return ResidentSenderSnapshot{}, errResidentSenderRead
	}
	code := statedb.GetCode(address)
	if statedb.Error() != nil {
		return ResidentSenderSnapshot{}, errResidentSenderRead
	}
	return ResidentSenderSnapshot{
		Address:  address,
		Nonce:    statedb.GetNonce(address),
		Balance:  balance.ToBig(),
		CodeHash: statedb.GetCodeHash(address),
		Code:     append([]byte(nil), code...),
		Exists:   statedb.Exist(address),
	}, nil
}

func cloneResidentRecord(record ResidentPostStartRecord) ResidentPostStartRecord {
	clone := record
	clone.Senders = make([]ResidentSenderSnapshot, len(record.Senders))
	for i, sender := range record.Senders {
		clone.Senders[i] = sender
		clone.Senders[i].Balance = new(big.Int).Set(sender.Balance)
		clone.Senders[i].Code = append([]byte(nil), sender.Code...)
	}
	return clone
}
