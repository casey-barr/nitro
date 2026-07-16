// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	errPostStartStateNotFound = errors.New("post-StartBlock state not retained")
	errPostStartNotCanonical  = errors.New("post-StartBlock state is not canonical")
	errPostStartIdentity      = errors.New("post-StartBlock state identity mismatch")
	errPostStartIPCOnly       = errors.New("post-StartBlock state API is IPC-only")
)

type PostStartStateIdentity struct {
	ChainID        *hexutil.Big   `json:"chainId"`
	NodeInstanceID common.Hash    `json:"nodeInstanceId"`
	NodeEpoch      hexutil.Uint64 `json:"nodeEpoch"`
	MessageIndex   hexutil.Uint64 `json:"messageIndex"`
	// MessageDigest is keccak256 of the raw L2 message payload. It is zero for
	// locally sequenced blocks until canonical child-hash enrichment.
	MessageDigest         common.Hash    `json:"messageDigest"`
	ParentBlockHash       common.Hash    `json:"parentBlockHash"`
	ParentStateRoot       common.Hash    `json:"parentStateRoot"`
	ChildBlockNumber      hexutil.Uint64 `json:"childBlockNumber"`
	ChildBlockHash        common.Hash    `json:"childBlockHash"`
	StartBlockTxHash      common.Hash    `json:"startBlockTxHash"`
	StartBlockInputDigest common.Hash    `json:"startBlockInputDigest"`
	StartBlockTxType      hexutil.Uint64 `json:"startBlockTxType"`
	StartBlockTxIndex     hexutil.Uint64 `json:"startBlockTxIndex"`
	ArbOSVersion          hexutil.Uint64 `json:"arbosVersion"`
	PostStartStateRoot    common.Hash    `json:"postStartStateRoot"`
	CaptureLatencyNanos   hexutil.Uint64 `json:"captureLatencyNanos"`
}

type postStartStateEntry struct {
	mu       sync.Mutex
	rootOnce sync.Once
	rootErr  error
	identity PostStartStateIdentity
	state    *state.StateDB
}

// PostStartStateStore retains a small, in-memory window of exact StateDB copies
// captured after StartBlock and before the first user transaction.
type PostStartStateStore struct {
	mu       sync.RWMutex
	chain    *core.BlockChain
	capacity int
	instance common.Hash
	epoch    uint64
	entries  []*postStartStateEntry
}

func NewPostStartStateStore(chain *core.BlockChain, capacity int) *PostStartStateStore {
	if capacity < 1 {
		capacity = 1
	}
	instanceSeed := fmt.Sprintf("%p:%d", chain, time.Now().UnixNano())
	return &PostStartStateStore{
		chain:    chain,
		capacity: capacity,
		instance: crypto.Keccak256Hash([]byte(instanceSeed)),
		epoch:    1,
	}
}

type postStartStateObserver struct {
	store         *PostStartStateStore
	nodeEpoch     uint64
	messageIndex  uint64
	messageDigest common.Hash
}

func (s *PostStartStateStore) Observer(messageIndex uint64, messageDigest common.Hash) *postStartStateObserver {
	s.mu.RLock()
	epoch := s.epoch
	s.mu.RUnlock()
	return &postStartStateObserver{
		store:         s,
		nodeEpoch:     epoch,
		messageIndex:  messageIndex,
		messageDigest: messageDigest,
	}
}

func (o *postStartStateObserver) StartBlockApplied(header *types.Header, statedb *state.StateDB, tx *types.Transaction) {
	o.store.capture(o.nodeEpoch, o.messageIndex, o.messageDigest, header, statedb, tx)
}

func (s *PostStartStateStore) capture(nodeEpoch uint64, messageIndex uint64, messageDigest common.Hash, header *types.Header, statedb *state.StateDB, tx *types.Transaction) {
	start := time.Now()
	snapshot := statedb.Copy()

	parent := s.chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return
	}
	genesis := s.chain.Config().ArbitrumChainParams.GenesisBlockNum
	if header.Number.Uint64() < genesis {
		return
	}
	if messageIndex != header.Number.Uint64()-genesis {
		return
	}
	chainID := (*hexutil.Big)(new(big.Int).Set(s.chain.Config().ChainID))
	entry := &postStartStateEntry{
		identity: PostStartStateIdentity{
			ChainID:               chainID,
			NodeInstanceID:        s.instance,
			NodeEpoch:             hexutil.Uint64(nodeEpoch),
			MessageIndex:          hexutil.Uint64(messageIndex),
			MessageDigest:         messageDigest,
			ParentBlockHash:       header.ParentHash,
			ParentStateRoot:       parent.Root,
			ChildBlockNumber:      hexutil.Uint64(header.Number.Uint64()),
			StartBlockTxHash:      tx.Hash(),
			StartBlockInputDigest: crypto.Keccak256Hash(tx.Data()),
			StartBlockTxType:      hexutil.Uint64(tx.Type()),
			StartBlockTxIndex:     0,
			ArbOSVersion:          hexutil.Uint64(types.DeserializeHeaderExtraInformation(header).ArbOSFormatVersion),
			CaptureLatencyNanos:   hexutil.Uint64(time.Since(start).Nanoseconds()),
		},
		state: snapshot,
	}

	s.retain(entry)
}

func (s *PostStartStateStore) retain(entry *postStartStateEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if uint64(entry.identity.NodeEpoch) != s.epoch {
		return
	}
	for i, retained := range s.entries {
		if retained.identity.ChildBlockNumber == entry.identity.ChildBlockNumber &&
			retained.identity.ParentBlockHash == entry.identity.ParentBlockHash {
			s.entries[i] = entry
			return
		}
	}
	s.entries = append(s.entries, entry)
	if len(s.entries) > s.capacity {
		s.entries = s.entries[len(s.entries)-s.capacity:]
	}
}

func (s *PostStartStateStore) MarkCanonical(block *types.Block) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if uint64(entry.identity.ChildBlockNumber) == block.NumberU64() &&
			entry.identity.ParentBlockHash == block.ParentHash() {
			entry.mu.Lock()
			entry.identity.ChildBlockHash = block.Hash()
			entry.mu.Unlock()
			return
		}
	}
}

func (s *PostStartStateStore) Clear() {
	s.mu.Lock()
	s.entries = nil
	s.epoch++
	s.mu.Unlock()
}

func (s *PostStartStateStore) isCurrent(nodeEpoch uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.epoch == nodeEpoch
}

func (s *PostStartStateStore) get(parentHash common.Hash, childNumber uint64) (*postStartStateEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		if entry.identity.ParentBlockHash == parentHash &&
			uint64(entry.identity.ChildBlockNumber) == childNumber {
			return entry, nil
		}
	}
	return nil, errPostStartStateNotFound
}

type PostStartAccountRequest struct {
	Address     common.Address `json:"address"`
	StorageKeys []common.Hash  `json:"storageKeys"`
	IncludeCode bool           `json:"includeCode"`
}

type PostStartStateRequest struct {
	ParentBlockHash               common.Hash               `json:"parentBlockHash"`
	ChildBlockNumber              hexutil.Uint64            `json:"childBlockNumber"`
	MessageIndex                  hexutil.Uint64            `json:"messageIndex"`
	MessageDigest                 common.Hash               `json:"messageDigest"`
	ExpectedNodeInstanceID        common.Hash               `json:"expectedNodeInstanceId"`
	ExpectedNodeEpoch             hexutil.Uint64            `json:"expectedNodeEpoch"`
	ExpectedStartBlockTxHash      common.Hash               `json:"expectedStartBlockTxHash"`
	ExpectedStartBlockInputDigest common.Hash               `json:"expectedStartBlockInputDigest"`
	ChildBlockHash                common.Hash               `json:"childBlockHash"`
	ExpectedPostStartStateRoot    common.Hash               `json:"expectedPostStartStateRoot"`
	Accounts                      []PostStartAccountRequest `json:"accounts"`
	BlockNumbers                  []hexutil.Uint64          `json:"blockNumbers"`
}

type PostStartStorageValue struct {
	Key   common.Hash `json:"key"`
	Value common.Hash `json:"value"`
}

type PostStartAccountResult struct {
	Address       common.Address          `json:"address"`
	Exists        bool                    `json:"exists"`
	Balance       *hexutil.Big            `json:"balance"`
	Nonce         hexutil.Uint64          `json:"nonce"`
	CodeHash      common.Hash             `json:"codeHash"`
	Code          hexutil.Bytes           `json:"code,omitempty"`
	StorageValues []PostStartStorageValue `json:"storageValues"`
}

type PostStartBlockHashResult struct {
	Number hexutil.Uint64 `json:"number"`
	Hash   common.Hash    `json:"hash"`
}

type PostStartStateResult struct {
	Identity    PostStartStateIdentity     `json:"identity"`
	Accounts    []PostStartAccountResult   `json:"accounts"`
	BlockHashes []PostStartBlockHashResult `json:"blockHashes"`
}

type PostStartStateAPI struct {
	store *PostStartStateStore
}

func NewPostStartStateAPI(store *PostStartStateStore) *PostStartStateAPI {
	return &PostStartStateAPI{store: store}
}

// GetBatch returns account, code, storage, and BLOCKHASH inputs from one exact
// retained post-StartBlock snapshot. It is intentionally unavailable over HTTP
// and WebSocket even if the namespace is accidentally enabled there.
func (api *PostStartStateAPI) GetBatch(ctx context.Context, request PostStartStateRequest) (PostStartStateResult, error) {
	if rpc.PeerInfoFromContext(ctx).Transport != "ipc" {
		return PostStartStateResult{}, errPostStartIPCOnly
	}
	return api.getBatch(request)
}

func (api *PostStartStateAPI) getBatch(request PostStartStateRequest) (PostStartStateResult, error) {
	entry, err := api.store.get(request.ParentBlockHash, uint64(request.ChildBlockNumber))
	if err != nil {
		return PostStartStateResult{}, err
	}
	if len(request.Accounts) > 4096 {
		return PostStartStateResult{}, fmt.Errorf("too many account reads: %d", len(request.Accounts))
	}
	totalStorage := 0
	for _, account := range request.Accounts {
		totalStorage += len(account.StorageKeys)
	}
	if totalStorage > 16384 {
		return PostStartStateResult{}, fmt.Errorf("too many storage reads: %d", totalStorage)
	}
	if len(request.BlockNumbers) > 256 {
		return PostStartStateResult{}, fmt.Errorf("too many block-hash reads: %d", len(request.BlockNumbers))
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	childNumber := uint64(entry.identity.ChildBlockNumber)
	if uint64(request.MessageIndex) != uint64(entry.identity.MessageIndex) {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if entry.identity.MessageDigest != (common.Hash{}) {
		if request.MessageDigest != entry.identity.MessageDigest {
			return PostStartStateResult{}, errPostStartIdentity
		}
	} else if request.ChildBlockHash == (common.Hash{}) {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedNodeInstanceID != (common.Hash{}) &&
		request.ExpectedNodeInstanceID != entry.identity.NodeInstanceID {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedNodeEpoch != 0 &&
		request.ExpectedNodeEpoch != entry.identity.NodeEpoch {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedStartBlockTxHash != (common.Hash{}) &&
		request.ExpectedStartBlockTxHash != entry.identity.StartBlockTxHash {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedStartBlockInputDigest != (common.Hash{}) &&
		request.ExpectedStartBlockInputDigest != entry.identity.StartBlockInputDigest {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ChildBlockHash != (common.Hash{}) {
		if entry.identity.ChildBlockHash == (common.Hash{}) ||
			request.ChildBlockHash != entry.identity.ChildBlockHash ||
			api.store.chain.GetCanonicalHash(childNumber) != entry.identity.ChildBlockHash ||
			api.store.chain.GetCanonicalHash(childNumber-1) != entry.identity.ParentBlockHash {
			return PostStartStateResult{}, errPostStartNotCanonical
		}
	}
	entry.rootOnce.Do(func() {
		rootState := entry.state.Copy()
		entry.identity.PostStartStateRoot = rootState.IntermediateRoot(true)
		entry.rootErr = rootState.Error()
	})
	if entry.rootErr != nil {
		return PostStartStateResult{}, fmt.Errorf("compute post-StartBlock state root: %w", entry.rootErr)
	}
	if request.ExpectedPostStartStateRoot != (common.Hash{}) &&
		request.ExpectedPostStartStateRoot != entry.identity.PostStartStateRoot {
		return PostStartStateResult{}, errPostStartIdentity
	}
	result := PostStartStateResult{Identity: entry.identity}
	for _, requested := range request.Accounts {
		balance := entry.state.GetBalance(requested.Address).ToBig()
		account := PostStartAccountResult{
			Address:  requested.Address,
			Exists:   entry.state.Exist(requested.Address),
			Balance:  (*hexutil.Big)(balance),
			Nonce:    hexutil.Uint64(entry.state.GetNonce(requested.Address)),
			CodeHash: entry.state.GetCodeHash(requested.Address),
		}
		if requested.IncludeCode {
			account.Code = hexutil.Bytes(entry.state.GetCode(requested.Address))
		}
		for _, key := range requested.StorageKeys {
			account.StorageValues = append(account.StorageValues, PostStartStorageValue{
				Key:   key,
				Value: entry.state.GetState(requested.Address, key),
			})
		}
		result.Accounts = append(result.Accounts, account)
	}
	for _, number := range request.BlockNumbers {
		if uint64(number) >= uint64(entry.identity.ChildBlockNumber) {
			return PostStartStateResult{}, fmt.Errorf("BLOCKHASH number %d is not before child block %d", number, entry.identity.ChildBlockNumber)
		}
		result.BlockHashes = append(result.BlockHashes, PostStartBlockHashResult{
			Number: number,
			Hash:   api.store.chain.GetCanonicalHash(uint64(number)),
		})
	}
	if err := entry.state.Error(); err != nil {
		return PostStartStateResult{}, fmt.Errorf("read post-StartBlock state: %w", err)
	}
	if !api.store.isCurrent(uint64(entry.identity.NodeEpoch)) {
		return PostStartStateResult{}, errPostStartIdentity
	}
	return result, nil
}
