// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

type recordingStartBlockObserver struct {
	calls int
	tx    *types.Transaction
}

func (observer *recordingStartBlockObserver) StartBlockApplied(
	_ *types.Header,
	_ *state.StateDB,
	tx *types.Transaction,
) {
	observer.calls++
	observer.tx = tx
}

func internalTransaction(selector [4]byte) *types.Transaction {
	return types.NewTx(&types.ArbitrumInternalTx{
		ChainId: big.NewInt(1),
		Data: append(selector[:], 0xaa),
	})
}

func TestObserveStartBlockCapturesExactStartBlockSelector(t *testing.T) {
	originalSelector := InternalTxStartBlockMethodID
	t.Cleanup(func() {
		InternalTxStartBlockMethodID = originalSelector
	})
	InternalTxStartBlockMethodID = [4]byte{0x01, 0x02, 0x03, 0x04}

	tx := internalTransaction(InternalTxStartBlockMethodID)
	observer := &recordingStartBlockObserver{}
	observeStartBlock(observer, nil, nil, tx, 0)

	if observer.calls != 1 || observer.tx != tx {
		t.Fatal("exact StartBlock internal transaction was not captured")
	}
}

func TestObserveStartBlockRejectsDifferentInternalMethod(t *testing.T) {
	originalSelector := InternalTxStartBlockMethodID
	t.Cleanup(func() {
		InternalTxStartBlockMethodID = originalSelector
	})
	InternalTxStartBlockMethodID = [4]byte{0x01, 0x02, 0x03, 0x04}

	tx := internalTransaction([4]byte{0x05, 0x06, 0x07, 0x08})
	observer := &recordingStartBlockObserver{}
	observeStartBlock(observer, nil, nil, tx, 0)

	if observer.calls != 0 {
		t.Fatal("non-StartBlock internal transaction was captured")
	}
}
