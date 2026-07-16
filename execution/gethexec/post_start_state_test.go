// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func testPostStartEntry(parent uint64, child uint64) *postStartStateEntry {
	return &postStartStateEntry{
		identity: PostStartStateIdentity{
			ParentBlockHash:   common.BigToHash(new(big.Int).SetUint64(parent)),
			ParentBlockNumber: hexutil.Uint64(parent),
			ChildBlockNumber:  hexutil.Uint64(child),
			MessageIndex:      hexutil.Uint64(child),
			NodeEpoch:         1,
		},
	}
}

func TestPostStartStateStoreDiscoversByExactMessageIdentity(t *testing.T) {
	store := NewPostStartStateStore(nil, 3)
	first := testPostStartEntry(1, 2)
	first.identity.MessageDigest = common.HexToHash("0x1234")
	second := testPostStartEntry(2, 3)
	second.identity.MessageDigest = common.HexToHash("0x5678")
	sameIndexDifferentDigest := testPostStartEntry(3, 2)
	sameIndexDifferentDigest.identity.MessageDigest = common.HexToHash("0x9abc")
	store.retain(first)
	store.retain(second)
	store.retain(sameIndexDifferentDigest)

	found, err := store.getByMessage(2, first.identity.MessageDigest)
	if err != nil || found != first {
		t.Fatalf("expected exact feed identity to discover first retained frame, got %p %v", found, err)
	}
	if _, err := store.getByMessage(2, second.identity.MessageDigest); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("expected cross-frame digest to fail closed, got %v", err)
	}
	found, err = store.getByMessage(2, sameIndexDifferentDigest.identity.MessageDigest)
	if err != nil || found != sameIndexDifferentDigest {
		t.Fatalf("expected same-index distinct digest to select only its exact frame, got %p %v", found, err)
	}
	if _, err := store.getByMessage(2, common.Hash{}); !errors.Is(err, errPostStartIdentity) {
		t.Fatalf("expected zero discovery digest to fail closed, got %v", err)
	}
}

func TestPostStartMessageDigestCrossLanguageVectorIncludesKindByte(t *testing.T) {
	// Raw L2 messages include the kind byte. This fixed vector is suitable for
	// consumers in other languages: keccak256(hex"04deadbeef").
	rawMessage := []byte{0x04, 0xde, 0xad, 0xbe, 0xef}
	want := common.HexToHash("0x8254d34fb3df9e9bd61801f5da0ed83b736448d8653ddeacbbe1cab334e4ded7")
	if got := crypto.Keccak256Hash(rawMessage); got != want {
		t.Fatalf("message digest vector mismatch: got %s want %s", got, want)
	}
	if withoutKind := crypto.Keccak256Hash(rawMessage[1:]); withoutKind == want {
		t.Fatal("digest must commit to the L2 message kind byte")
	}
}

func TestPostStartStateStoreDiscoversCanonicalExactMessageAcrossReorg(t *testing.T) {
	store := NewPostStartStateStore(nil, 3)
	digest := common.HexToHash("0x1234")
	canonicalParent := common.HexToHash("0xaaaa")
	staleParent := common.HexToHash("0xbbbb")
	canonical := testPostStartEntry(1, 2)
	canonical.identity.ParentBlockHash = canonicalParent
	canonical.identity.MessageDigest = digest
	staleNewest := testPostStartEntry(1, 2)
	staleNewest.identity.ParentBlockHash = staleParent
	staleNewest.identity.MessageDigest = digest
	store.retain(canonical)
	store.retain(staleNewest)

	currentParent := canonicalParent
	store.canonicalHash = func(number uint64) common.Hash {
		if number == 1 {
			return currentParent
		}
		return common.Hash{}
	}
	found, err := store.getByMessage(2, digest)
	if err != nil || found != canonical {
		t.Fatalf("newest stale fork must not shadow older canonical match, got %p %v", found, err)
	}

	currentParent = staleParent
	found, err = store.getByMessage(2, digest)
	if err != nil || found != staleNewest {
		t.Fatalf("expected reorged canonical match, got %p %v", found, err)
	}

	currentParent = common.HexToHash("0xcccc")
	if _, err := store.getByMessage(2, digest); !errors.Is(err, errPostStartNotCanonical) {
		t.Fatalf("expected exact stale-only matches to refuse as non-canonical, got %v", err)
	}
}

func TestPostStartStateStoreRetentionAndReplacement(t *testing.T) {
	store := NewPostStartStateStore(nil, 2)
	first := testPostStartEntry(1, 2)
	replacement := testPostStartEntry(1, 2)
	second := testPostStartEntry(2, 3)
	third := testPostStartEntry(3, 4)

	store.retain(first)
	store.retain(replacement)
	if len(store.entries) != 1 || store.entries[0] != replacement {
		t.Fatal("same parent and child must replace the retained snapshot")
	}
	store.retain(second)
	store.retain(third)
	if len(store.entries) != 2 || store.entries[0] != second || store.entries[1] != third {
		t.Fatal("retention must evict only the oldest snapshot")
	}
	if _, err := store.get(first.identity.ParentBlockHash, 2); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("expected evicted snapshot to be unavailable, got %v", err)
	}
}

func TestPostStartStateStoreClear(t *testing.T) {
	store := NewPostStartStateStore(nil, 2)
	entry := testPostStartEntry(1, 2)
	store.retain(entry)
	store.Clear()
	if _, err := store.get(entry.identity.ParentBlockHash, 2); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("expected cleared snapshot to be unavailable, got %v", err)
	}
	store.retain(entry)
	if _, err := store.get(entry.identity.ParentBlockHash, 2); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("expected pre-reorg epoch snapshot to be refused, got %v", err)
	}
}

func TestPostStartStateStoreDuplicateReplacementPreservesCurrentEpoch(t *testing.T) {
	store := NewPostStartStateStore(nil, 2)
	first := testPostStartEntry(1, 2)
	first.identity.MessageDigest = common.HexToHash("0x1111")
	replacement := testPostStartEntry(1, 2)
	replacement.identity.MessageDigest = common.HexToHash("0x2222")
	store.retain(first)
	store.retain(replacement)
	if len(store.entries) != 1 || store.entries[0] != replacement {
		t.Fatal("duplicate parent/child capture must retain only the newest immutable snapshot")
	}
	if _, err := store.getByMessage(2, first.identity.MessageDigest); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("replaced digest must not remain discoverable, got %v", err)
	}
	if found, err := store.getByMessage(2, replacement.identity.MessageDigest); err != nil || found != replacement {
		t.Fatalf("replacement must remain discoverable in the current epoch, got %p %v", found, err)
	}
}

func TestPostStartStateStoreCanonicalEnrichment(t *testing.T) {
	store := NewPostStartStateStore(nil, 1)
	entry := testPostStartEntry(1, 2)
	entry.identity.MessageDigest = common.HexToHash("0x1234")
	store.retain(entry)
	block := types.NewBlockWithHeader(&types.Header{
		ParentHash: entry.identity.ParentBlockHash,
		Number:     new(big.Int).SetUint64(2),
	})
	store.MarkCanonical(block)
	if entry.identity.ChildBlockHash != block.Hash() {
		t.Fatal("expected retained ephemeral identity to gain the canonical child hash")
	}
}

func TestPostStartStateStoreCanonicalEnrichmentDoesNotBlockEpochReads(t *testing.T) {
	store := NewPostStartStateStore(nil, 1)
	entry := testPostStartEntry(1, 2)
	store.retain(entry)
	block := types.NewBlockWithHeader(&types.Header{
		ParentHash: entry.identity.ParentBlockHash,
		Number:     new(big.Int).SetUint64(2),
	})

	entry.mu.Lock()
	store.mu.Lock()
	started := make(chan struct{})
	marked := make(chan struct{})
	go func() {
		close(started)
		store.MarkCanonical(block)
		close(marked)
	}()
	<-started
	store.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for {
		if store.mu.TryLock() {
			store.mu.Unlock()
			break
		}
		if time.Now().After(deadline) {
			entry.mu.Unlock()
			t.Fatal("canonical enrichment held the store lock while waiting for an entry")
		}
		time.Sleep(time.Millisecond)
	}
	entry.mu.Unlock()

	select {
	case <-marked:
	case <-time.After(time.Second):
		t.Fatal("canonical enrichment did not complete")
	}
}

func TestPostStartStateAPIMaterializationDoesNotBlockCanonicalEnrichment(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostStartStateStore(nil, 1)
	entry := testPostStartEntry(1, 2)
	entry.identity.MessageDigest = common.HexToHash("0x1234")
	entry.state = statedb
	store.retain(entry)
	store.canonicalHash = func(number uint64) common.Hash {
		if number == 1 {
			return entry.identity.ParentBlockHash
		}
		return common.Hash{}
	}
	materializing := make(chan struct{})
	releaseMaterialization := make(chan struct{})
	api := NewPostStartStateAPI(store)
	api.afterEntrySnapshot = func() {
		close(materializing)
		<-releaseMaterialization
	}
	request := PostStartStateRequest{
		MessageIndex:  entry.identity.MessageIndex,
		MessageDigest: entry.identity.MessageDigest,
	}
	served := make(chan error, 1)
	go func() {
		_, err := api.getBatch(request)
		served <- err
	}()
	<-materializing

	block := types.NewBlockWithHeader(&types.Header{
		ParentHash: entry.identity.ParentBlockHash,
		Number:     new(big.Int).SetUint64(2),
	})
	marked := make(chan struct{})
	go func() {
		store.MarkCanonical(block)
		close(marked)
	}()
	select {
	case <-marked:
	case <-time.After(time.Second):
		close(releaseMaterialization)
		t.Fatal("canonical enrichment blocked behind account/root materialization")
	}
	close(releaseMaterialization)
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("batch did not complete after materialization barrier released")
	}
}

func TestPostStartStateAPIRejectsNonIPC(t *testing.T) {
	api := NewPostStartStateAPI(NewPostStartStateStore(nil, 1))
	_, err := api.GetBatch(context.Background(), PostStartStateRequest{})
	if !errors.Is(err, errPostStartIPCOnly) {
		t.Fatalf("expected IPC-only refusal, got %v", err)
	}
}

func TestPostStartStateAPIBoundsReads(t *testing.T) {
	store := NewPostStartStateStore(nil, 1)
	entry := testPostStartEntry(1, 2)
	entry.identity.MessageDigest = common.HexToHash("0x1234")
	store.retain(entry)
	api := NewPostStartStateAPI(store)
	request := PostStartStateRequest{
		ParentBlockHash:  entry.identity.ParentBlockHash,
		ChildBlockNumber: entry.identity.ChildBlockNumber,
		MessageIndex:     entry.identity.MessageIndex,
		MessageDigest:    entry.identity.MessageDigest,
		Accounts:         make([]PostStartAccountRequest, 4097),
	}
	if _, err := api.getBatch(request); err == nil {
		t.Fatal("expected oversized account batch to be rejected")
	}
}

func TestPostStartStateAPIServesExactEphemeralBinding(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostStartStateStore(nil, 1)
	entry := testPostStartEntry(1, 2)
	entry.identity.MessageDigest = common.HexToHash("0x1234")
	entry.environment = PostStartExecutionEnvironment{
		L1BlockNumber: 7,
		Timestamp:     100,
		GasLimit:      30_000_000,
		BaseFeePerGas: (*hexutil.Big)(big.NewInt(100_000_000)),
		Beneficiary:   common.HexToAddress("0x42"),
		Difficulty:    (*hexutil.Big)(new(big.Int)),
		PrevRandao:    common.HexToHash("0x43"),
		ExcessBlobGas: 123,
		BlobBaseFee:   (*hexutil.Big)(big.NewInt(456)),
	}
	entry.state = statedb
	store.retain(entry)
	api := NewPostStartStateAPI(store)
	request := PostStartStateRequest{
		ParentBlockHash:  entry.identity.ParentBlockHash,
		ChildBlockNumber: entry.identity.ChildBlockNumber,
		MessageIndex:     entry.identity.MessageIndex,
		MessageDigest:    entry.identity.MessageDigest,
	}
	result, err := api.getBatch(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Identity.ChildBlockHash != (common.Hash{}) {
		t.Fatal("ephemeral result must not claim a canonical child hash")
	}
	if result.Identity.PostStartStateRoot == (common.Hash{}) {
		t.Fatal("ephemeral result must include the exact post-StartBlock state root")
	}
	if uint64(result.Environment.L1BlockNumber) != 7 || uint64(result.Environment.Timestamp) != 100 ||
		uint64(result.Environment.GasLimit) != 30_000_000 ||
		(*big.Int)(result.Environment.BaseFeePerGas).Uint64() != 100_000_000 ||
		result.Environment.Beneficiary != common.HexToAddress("0x42") ||
		(*big.Int)(result.Environment.Difficulty).Sign() != 0 ||
		result.Environment.PrevRandao != common.HexToHash("0x43") ||
		uint64(result.Environment.ExcessBlobGas) != 123 ||
		(*big.Int)(result.Environment.BlobBaseFee).Uint64() != 456 {
		t.Fatalf("expected retained execution environment, got %+v", result.Environment)
	}

	request.ParentBlockHash = common.HexToHash("0x9999")
	if _, err := api.getBatch(request); !errors.Is(err, errPostStartIdentity) {
		t.Fatalf("expected optional parent expectation mismatch refusal, got %v", err)
	}
	request.ParentBlockHash = entry.identity.ParentBlockHash

	request.MessageDigest = common.HexToHash("0x5678")
	if _, err := api.getBatch(request); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("expected mismatched feed digest refusal, got %v", err)
	}
}
