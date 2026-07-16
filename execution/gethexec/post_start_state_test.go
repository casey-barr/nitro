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
	store := NewPostStartStateStore(nil, 2)
	first := testPostStartEntry(1, 2)
	first.identity.MessageDigest = common.HexToHash("0x1234")
	second := testPostStartEntry(2, 3)
	second.identity.MessageDigest = common.HexToHash("0x5678")
	store.retain(first)
	store.retain(second)

	found, err := store.getByMessage(2, first.identity.MessageDigest)
	if err != nil || found != first {
		t.Fatalf("expected exact feed identity to discover first retained frame, got %p %v", found, err)
	}
	if _, err := store.getByMessage(2, second.identity.MessageDigest); !errors.Is(err, errPostStartStateNotFound) {
		t.Fatalf("expected cross-frame digest to fail closed, got %v", err)
	}
	if _, err := store.getByMessage(2, common.Hash{}); !errors.Is(err, errPostStartIdentity) {
		t.Fatalf("expected zero discovery digest to fail closed, got %v", err)
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
		uint64(result.Environment.ExcessBlobGas) != 123 || (*big.Int)(result.Environment.BlobBaseFee).Uint64() != 456 {
		t.Fatalf("expected retained execution environment, got %+v", result.Environment)
	}

	request.ParentBlockHash = common.HexToHash("0x9999")
	if _, err := api.getBatch(request); !errors.Is(err, errPostStartIdentity) {
		t.Fatalf("expected optional parent expectation mismatch refusal, got %v", err)
	}
	request.ParentBlockHash = entry.identity.ParentBlockHash

	request.MessageDigest = common.HexToHash("0x5678")
	if _, err := api.getBatch(request); !errors.Is(err, errPostStartIdentity) {
		t.Fatalf("expected mismatched feed digest refusal, got %v", err)
	}
}
