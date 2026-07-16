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
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
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
	ParentBlockNumber     hexutil.Uint64 `json:"parentBlockNumber"`
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

type PostStartExecutionEnvironment struct {
	L1BlockNumber hexutil.Uint64 `json:"l1BlockNumber"`
	Timestamp     hexutil.Uint64 `json:"timestamp"`
	GasLimit      hexutil.Uint64 `json:"gasLimit"`
	BaseFeePerGas *hexutil.Big   `json:"baseFeePerGas"`
	Beneficiary   common.Address `json:"beneficiary"`
	Difficulty    *hexutil.Big   `json:"difficulty"`
	PrevRandao    common.Hash    `json:"prevrandao"`
	ExcessBlobGas hexutil.Uint64 `json:"excessBlobGas"`
	BlobBaseFee   *hexutil.Big   `json:"blobBaseFee"`
}

type postStartStateEntry struct {
	mu          sync.Mutex
	rootOnce    sync.Once
	root        common.Hash
	rootErr     error
	identity    PostStartStateIdentity
	environment PostStartExecutionEnvironment
	state       *state.StateDB
}

// PostStartStateStore retains a small, in-memory window of exact StateDB copies
// captured after StartBlock and before the first user transaction.
type PostStartStateStore struct {
	mu            sync.RWMutex
	chain         *core.BlockChain
	capacity      int
	instance      common.Hash
	epoch         uint64
	entries       []*postStartStateEntry
	canonicalHash func(uint64) common.Hash
}

func NewPostStartStateStore(chain *core.BlockChain, capacity int) *PostStartStateStore {
	if capacity < 1 {
		capacity = 1
	}
	instanceSeed := fmt.Sprintf("%p:%d", chain, time.Now().UnixNano())
	store := &PostStartStateStore{
		chain:    chain,
		capacity: capacity,
		instance: crypto.Keccak256Hash([]byte(instanceSeed)),
		epoch:    1,
	}
	if chain != nil {
		store.canonicalHash = chain.GetCanonicalHash
	}
	return store
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
	headerExtra := types.DeserializeHeaderExtraInformation(header)
	baseFee := new(big.Int)
	if header.BaseFee != nil {
		baseFee.Set(header.BaseFee)
	}
	difficulty := new(big.Int)
	if header.Difficulty != nil {
		difficulty.Set(header.Difficulty)
	}
	excessBlobGas := uint64(0)
	if header.ExcessBlobGas != nil {
		excessBlobGas = *header.ExcessBlobGas
	}
	blobBaseFee := eip4844.CalcBlobFee(s.chain.Config(), header)
	entry := &postStartStateEntry{
		identity: PostStartStateIdentity{
			ChainID:               chainID,
			NodeInstanceID:        s.instance,
			NodeEpoch:             hexutil.Uint64(nodeEpoch),
			MessageIndex:          hexutil.Uint64(messageIndex),
			MessageDigest:         messageDigest,
			ParentBlockHash:       header.ParentHash,
			ParentBlockNumber:     hexutil.Uint64(parent.Number.Uint64()),
			ParentStateRoot:       parent.Root,
			ChildBlockNumber:      hexutil.Uint64(header.Number.Uint64()),
			StartBlockTxHash:      tx.Hash(),
			StartBlockInputDigest: crypto.Keccak256Hash(tx.Data()),
			StartBlockTxType:      hexutil.Uint64(tx.Type()),
			StartBlockTxIndex:     0,
			ArbOSVersion:          hexutil.Uint64(headerExtra.ArbOSFormatVersion),
			CaptureLatencyNanos:   hexutil.Uint64(time.Since(start).Nanoseconds()),
		},
		environment: PostStartExecutionEnvironment{
			L1BlockNumber: hexutil.Uint64(headerExtra.L1BlockNumber),
			Timestamp:     hexutil.Uint64(header.Time),
			GasLimit:      hexutil.Uint64(header.GasLimit),
			BaseFeePerGas: (*hexutil.Big)(baseFee),
			Beneficiary:   header.Coinbase,
			Difficulty:    (*hexutil.Big)(difficulty),
			PrevRandao:    header.MixDigest,
			ExcessBlobGas: hexutil.Uint64(excessBlobGas),
			BlobBaseFee:   (*hexutil.Big)(blobBaseFee),
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
	s.mu.RLock()
	var matching *postStartStateEntry
	for _, entry := range s.entries {
		if uint64(entry.identity.ChildBlockNumber) == block.NumberU64() &&
			entry.identity.ParentBlockHash == block.ParentHash() {
			matching = entry
			break
		}
	}
	s.mu.RUnlock()
	if matching == nil {
		return
	}

	matching.mu.Lock()
	defer matching.mu.Unlock()
	if !s.isCurrent(uint64(matching.identity.NodeEpoch)) {
		return
	}
	matching.identity.ChildBlockHash = block.Hash()
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

func (s *PostStartStateStore) getByMessage(messageIndex uint64, messageDigest common.Hash) (*postStartStateEntry, error) {
	if messageDigest == (common.Hash{}) {
		return nil, errPostStartIdentity
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := false
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		if uint64(entry.identity.MessageIndex) == messageIndex &&
			entry.identity.MessageDigest == messageDigest {
			matched = true
			if s.canonicalHash == nil ||
				s.canonicalHash(uint64(entry.identity.ParentBlockNumber)) == entry.identity.ParentBlockHash {
				return entry, nil
			}
		}
	}
	if matched {
		return nil, errPostStartNotCanonical
	}
	return nil, errPostStartStateNotFound
}

func (e *postStartStateEntry) snapshot() (PostStartStateIdentity, PostStartExecutionEnvironment, *state.StateDB) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.identity, e.environment, e.state
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
	Identity    PostStartStateIdentity        `json:"identity"`
	Environment PostStartExecutionEnvironment `json:"environment"`
	Accounts    []PostStartAccountResult      `json:"accounts"`
	BlockHashes []PostStartBlockHashResult    `json:"blockHashes"`
}

type PostStartStateAPI struct {
	store              *PostStartStateStore
	afterEntrySnapshot func()
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
	entry, err := api.store.getByMessage(uint64(request.MessageIndex), request.MessageDigest)
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

	identity, environment, retainedState := entry.snapshot()
	if api.afterEntrySnapshot != nil {
		api.afterEntrySnapshot()
	}
	snapshot := retainedState.Copy()
	childNumber := uint64(identity.ChildBlockNumber)
	parentNumber := uint64(identity.ParentBlockNumber)
	if request.ParentBlockHash != (common.Hash{}) && request.ParentBlockHash != identity.ParentBlockHash {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ChildBlockNumber != 0 && request.ChildBlockNumber != identity.ChildBlockNumber {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if uint64(request.MessageIndex) != uint64(identity.MessageIndex) {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if identity.MessageDigest != (common.Hash{}) {
		if request.MessageDigest != identity.MessageDigest {
			return PostStartStateResult{}, errPostStartIdentity
		}
	} else if request.ChildBlockHash == (common.Hash{}) {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedNodeInstanceID != (common.Hash{}) &&
		request.ExpectedNodeInstanceID != identity.NodeInstanceID {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedNodeEpoch != 0 &&
		request.ExpectedNodeEpoch != identity.NodeEpoch {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedStartBlockTxHash != (common.Hash{}) &&
		request.ExpectedStartBlockTxHash != identity.StartBlockTxHash {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ExpectedStartBlockInputDigest != (common.Hash{}) &&
		request.ExpectedStartBlockInputDigest != identity.StartBlockInputDigest {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if request.ChildBlockHash != (common.Hash{}) {
		if identity.ChildBlockHash == (common.Hash{}) ||
			request.ChildBlockHash != identity.ChildBlockHash ||
			api.store.canonicalHash == nil ||
			api.store.canonicalHash(childNumber) != identity.ChildBlockHash ||
			api.store.canonicalHash(childNumber-1) != identity.ParentBlockHash {
			return PostStartStateResult{}, errPostStartNotCanonical
		}
	}
	if api.store.canonicalHash != nil && api.store.canonicalHash(parentNumber) != identity.ParentBlockHash {
		return PostStartStateResult{}, errPostStartNotCanonical
	}
	entry.rootOnce.Do(func() {
		rootState := snapshot.Copy()
		entry.root = rootState.IntermediateRoot(true)
		entry.rootErr = rootState.Error()
	})
	if entry.rootErr != nil {
		return PostStartStateResult{}, fmt.Errorf("compute post-StartBlock state root: %w", entry.rootErr)
	}
	identity.PostStartStateRoot = entry.root
	if request.ExpectedPostStartStateRoot != (common.Hash{}) &&
		request.ExpectedPostStartStateRoot != identity.PostStartStateRoot {
		return PostStartStateResult{}, errPostStartIdentity
	}
	result := PostStartStateResult{Identity: identity, Environment: environment}
	for _, requested := range request.Accounts {
		balance := snapshot.GetBalance(requested.Address).ToBig()
		account := PostStartAccountResult{
			Address:  requested.Address,
			Exists:   snapshot.Exist(requested.Address),
			Balance:  (*hexutil.Big)(balance),
			Nonce:    hexutil.Uint64(snapshot.GetNonce(requested.Address)),
			CodeHash: snapshot.GetCodeHash(requested.Address),
		}
		if requested.IncludeCode {
			account.Code = hexutil.Bytes(snapshot.GetCode(requested.Address))
		}
		for _, key := range requested.StorageKeys {
			account.StorageValues = append(account.StorageValues, PostStartStorageValue{
				Key:   key,
				Value: snapshot.GetState(requested.Address, key),
			})
		}
		result.Accounts = append(result.Accounts, account)
	}
	for _, number := range request.BlockNumbers {
		if uint64(number) >= uint64(identity.ChildBlockNumber) {
			return PostStartStateResult{}, fmt.Errorf("BLOCKHASH number %d is not before child block %d", number, identity.ChildBlockNumber)
		}
		result.BlockHashes = append(result.BlockHashes, PostStartBlockHashResult{
			Number: number,
			Hash:   api.store.canonicalHash(uint64(number)),
		})
	}
	if err := snapshot.Error(); err != nil {
		return PostStartStateResult{}, fmt.Errorf("read post-StartBlock state: %w", err)
	}
	if !api.store.isCurrent(uint64(identity.NodeEpoch)) {
		return PostStartStateResult{}, errPostStartIdentity
	}
	if api.store.canonicalHash != nil && api.store.canonicalHash(parentNumber) != identity.ParentBlockHash {
		return PostStartStateResult{}, errPostStartNotCanonical
	}
	return result, nil
}
