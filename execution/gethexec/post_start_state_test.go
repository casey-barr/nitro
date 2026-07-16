// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

func testPostStartEntry(parent uint64, child uint64) *postStartStateEntry {
	return &postStartStateEntry{
		identity: PostStartStateIdentity{
			ParentBlockHash:  common.BigToHash(new(big.Int).SetUint64(parent)),
			ChildBlockNumber: hexutil.Uint64(child),
			MessageIndex:     hexutil.Uint64(child),
			NodeEpoch:        1,
		},
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

	request.MessageDigest = common.HexToHash("0x5678")
	if _, err := api.getBatch(request); !errors.Is(err, errPostStartIdentity) {
		t.Fatalf("expected mismatched feed digest refusal, got %v", err)
	}
}
