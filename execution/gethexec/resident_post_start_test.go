package gethexec

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

func TestResidentPostStartObserverDeduplicatesSendersInFirstOccurrenceOrder(t *testing.T) {
	keyA, err := crypto.GenerateKey()
	if err != nil { t.Fatal(err) }
	keyB, err := crypto.GenerateKey()
	if err != nil { t.Fatal(err) }
	signer := types.LatestSigner(params.AllEthashProtocolChanges)
	txA := mustSignedResidentTx(t, signer, keyA, 0)
	txB := mustSignedResidentTx(t, signer, keyB, 0)
	db, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil { t.Fatal(err) }
	fromA := crypto.PubkeyToAddress(keyA.PublicKey)
	fromB := crypto.PubkeyToAddress(keyB.PublicKey)
	db.SetBalance(fromA, uint256.NewInt(11), tracing.BalanceChangeUnspecified)
	db.SetNonce(fromA, 7)
	db.SetBalance(fromB, uint256.NewInt(22), tracing.BalanceChangeUnspecified)
	db.SetNonce(fromB, 8)
	store := NewResidentPostStartStateStore()
	digest := common.HexToHash("0x1234")
	observer, err := store.Observer(17, digest, signer)
	if err != nil { t.Fatal(err) }
	header := &types.Header{Number: big.NewInt(2), ParentHash: common.HexToHash("0x99")}
	observer.StartBlockAppliedWithTransactions(header, db, nil, types.Transactions{txA, txB, txA})
	if observer.Error() != nil { t.Fatal(observer.Error()) }
	if _, ok := store.LatestCanonical(); ok { t.Fatal("pending resident state became visible before canonical append") }
	block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
	store.MarkCanonical(block)
	record, ok := store.LatestCanonical()
	if !ok { t.Fatal("canonical resident state missing") }
	if record.MessageIndex != 17 || record.MessageDigest != digest || record.TransactionCount != 3 || len(record.Senders) != 2 { t.Fatalf("bad record: %+v", record) }
	if record.Senders[0].Address != fromA || record.Senders[1].Address != fromB { t.Fatal("sender order was not first-occurrence order") }
	if record.Senders[0].Nonce != 7 || record.Senders[1].Nonce != 8 { t.Fatal("sender nonce snapshot mismatch") }
}

func TestResidentPostStartObserverFailsClosedOnMissingTransactionPrefix(t *testing.T) {
	store := NewResidentPostStartStateStore()
	observer, err := store.Observer(1, common.HexToHash("0x1"), types.LatestSigner(params.AllEthashProtocolChanges))
	if err != nil { t.Fatal(err) }
	observer.StartBlockAppliedWithTransactions(&types.Header{Number: big.NewInt(1)}, nil, nil, nil)
	if observer.Error() == nil { t.Fatal("missing parsed transaction prefix was accepted") }
	if _, ok := store.LatestCanonical(); ok { t.Fatal("failed observer retained a record") }
}

func TestResidentPostStartObserverRejectsIncompleteIdentity(t *testing.T) {
	store := NewResidentPostStartStateStore()
	if _, err := store.Observer(1, common.Hash{}, types.LatestSigner(params.AllEthashProtocolChanges)); err == nil { t.Fatal("zero message digest accepted") }
	if _, err := store.Observer(1, common.HexToHash("0x1"), nil); err == nil { t.Fatal("nil signer accepted") }
}

func mustSignedResidentTx(t *testing.T, signer types.Signer, key *ecdsa.PrivateKey, nonce uint64) *types.Transaction {
	t.Helper()
	tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: nonce, Gas: 21_000, GasPrice: big.NewInt(1)}), signer, key)
	if err != nil { t.Fatal(err) }
	return tx
}
