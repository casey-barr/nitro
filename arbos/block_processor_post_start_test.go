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
	observeStartBlock(observer, nil, nil, tx, 0, nil)

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
	observeStartBlock(observer, nil, nil, tx, 0, nil)

	if observer.calls != 0 {
		t.Fatal("non-StartBlock internal transaction was captured")
	}
}


type recordingRichStartBlockObserver struct {
	recordingStartBlockObserver
	txs types.Transactions
}

func (observer *recordingRichStartBlockObserver) StartBlockAppliedWithTransactions(
	header *types.Header,
	statedb *state.StateDB,
	tx *types.Transaction,
	txs types.Transactions,
) {
	observer.StartBlockApplied(header, statedb, tx)
	observer.txs = txs
}

func TestObserveStartBlockPassesExactParsedTransactionsToRichObserver(t *testing.T) {
	originalSelector := InternalTxStartBlockMethodID
	t.Cleanup(func() { InternalTxStartBlockMethodID = originalSelector })
	InternalTxStartBlockMethodID = [4]byte{0x01, 0x02, 0x03, 0x04}
	start := internalTransaction(InternalTxStartBlockMethodID)
	user := types.NewTx(&types.LegacyTx{Nonce: 1})
	observer := &recordingRichStartBlockObserver{}
	parsed := types.Transactions{user}
	observeStartBlock(observer, nil, nil, start, 0, parsed)
	if len(observer.txs) != 1 || observer.txs[0] != user {
		t.Fatal("rich observer did not receive the exact parsed transaction slice")
	}
}

func TestObserveStartBlockRichObserverReceivesNilOnLegacyCall(t *testing.T) {
	originalSelector := InternalTxStartBlockMethodID
	t.Cleanup(func() { InternalTxStartBlockMethodID = originalSelector })
	InternalTxStartBlockMethodID = [4]byte{0x01, 0x02, 0x03, 0x04}
	observer := &recordingRichStartBlockObserver{}
	observeStartBlock(observer, nil, nil, internalTransaction(InternalTxStartBlockMethodID), 0, nil)
	if observer.txs != nil {
		t.Fatal("legacy observer call unexpectedly supplied a transaction prefix")
	}
}
