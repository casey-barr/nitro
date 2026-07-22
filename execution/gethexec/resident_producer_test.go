package gethexec

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/offchainlabs/nitro/execution/gethexec/residentevm"
)

func TestProducerNormalizesAbsentAndCarriesWipe(t *testing.T) {
	addr := common.HexToAddress("0x1000")
	recreated := common.HexToAddress("0x2000")
	writes := []state.ResidentMutation{
		{
			// The collector's internal absent value carries BOTH flags; the
			// wire shape must normalize deleted away.
			Key:   state.ResidentMutationKey{Address: addr, Kind: state.ResidentMutationAccount},
			Value: state.ResidentMutationValue{AccountAbsent: true, Deleted: true},
		},
		{
			// Delete-then-recreate: the wipe bit must ride through.
			Key: state.ResidentMutationKey{Address: recreated, Kind: state.ResidentMutationAccount},
			Value: state.ResidentMutationValue{
				Deleted:  true,
				Nonce:    3,
				Balance:  uint256.NewInt(42),
				CodeHash: crypto.Keccak256Hash(nil),
			},
		},
	}
	delta, err := DrainedWritesToCommitted([32]byte{1}, [32]byte{2}, writes)
	if err != nil {
		t.Fatal(err)
	}
	committed := delta.Record.(*residentevm.ResidentEvmDeltaV1_MessageCommitted).MessageCommitted
	if len(committed.Mutations) != 2 {
		t.Fatalf("mutations = %d", len(committed.Mutations))
	}
	absent := committed.Mutations[0]
	if !absent.AccountAbsent || absent.Deleted {
		t.Fatalf("absent not normalized: absent=%v deleted=%v", absent.AccountAbsent, absent.Deleted)
	}
	if len(absent.Balance) != 0 || len(absent.CodeHash) != 0 || absent.Nonce != 0 {
		t.Fatalf("absent carries stray account fields: %+v", absent)
	}
	live := committed.Mutations[1]
	if live.AccountAbsent || !live.Deleted {
		t.Fatalf("wipe bit lost on recreate: %+v", live)
	}
	if live.Nonce != 3 || len(live.Balance) != 32 || live.Balance[31] != 42 || len(live.CodeHash) != 32 {
		t.Fatalf("live account fields wrong: %+v", live)
	}
	// The record must survive the wire (marshal round-trip, schema stamped).
	raw, err := residentevm.MarshalLogical(delta)
	if err != nil {
		t.Fatal(err)
	}
	var back residentevm.ResidentEvmDeltaV1
	if err := residentevm.UnmarshalLogical(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.SchemaHash, residentevm.TypedSchemaHash()) {
		t.Fatal("schema hash not stamped")
	}
}

func TestProducerStorageAndCodeShapes(t *testing.T) {
	addr := common.HexToAddress("0x3000")
	code := []byte{0x60, 0x00}
	writes := []state.ResidentMutation{
		{
			Key:   state.ResidentMutationKey{Address: addr, Slot: common.HexToHash("0x07"), Kind: state.ResidentMutationStorage},
			Value: state.ResidentMutationValue{Value: common.HexToHash("0x2a")},
		},
		{
			Key:   state.ResidentMutationKey{Address: addr, Slot: common.HexToHash("0x08"), Kind: state.ResidentMutationStorage},
			Value: state.ResidentMutationValue{Deleted: true},
		},
		{
			Key:   state.ResidentMutationKey{Address: addr, Kind: state.ResidentMutationCode},
			Value: state.ResidentMutationValue{Code: code, CodeHash: crypto.Keccak256Hash(code)},
		},
	}
	delta, err := DrainedWritesToCommitted([32]byte{1}, [32]byte{2}, writes)
	if err != nil {
		t.Fatal(err)
	}
	committed := delta.Record.(*residentevm.ResidentEvmDeltaV1_MessageCommitted).MessageCommitted
	write := committed.Mutations[0]
	if len(write.Slot) != 32 || len(write.Value) != 32 || write.Value[31] != 0x2a {
		t.Fatalf("storage write wrong: %+v", write)
	}
	deleted := committed.Mutations[1]
	if !deleted.Deleted || len(deleted.Value) != 0 {
		t.Fatalf("storage delete wrong: %+v", deleted)
	}
	codeMutation := committed.Mutations[2]
	if !bytes.Equal(codeMutation.Code, code) || len(codeMutation.CodeHash) != 0 || len(codeMutation.Balance) != 0 {
		t.Fatalf("code mutation must carry only code bytes: %+v", codeMutation)
	}
}

func TestProducerSenderConversionAndWidthRefusals(t *testing.T) {
	record := ResidentPostStartRecord{
		MessageIndex:      9,
		MessageDigest:     common.HexToHash("0x99"),
		ParentBlockNumber: 100,
		ChildBlockNumber:  101,
		Senders: []ResidentSenderSnapshot{
			{Address: common.HexToAddress("0x4000"), Nonce: 7, Balance: big.NewInt(1_000), CodeHash: crypto.Keccak256Hash(nil), Exists: true},
			{Address: common.HexToAddress("0x5000"), Exists: false},
		},
	}
	delta, err := PostStartRecordToDelta(record)
	if err != nil {
		t.Fatal(err)
	}
	post := delta.Record.(*residentevm.ResidentEvmDeltaV1_PostStartBlock).PostStartBlock
	if post.ParentBlock != 100 || post.ChildBlock != 101 || len(post.Sender) != 2 {
		t.Fatalf("post-start envelope wrong: %+v", post)
	}
	if len(post.Sender[0].Balance) != 32 || post.Sender[0].Balance[30] != 0x03 || post.Sender[0].Balance[31] != 0xe8 {
		t.Fatalf("sender balance not 32-byte BE: %x", post.Sender[0].Balance)
	}
	if post.Sender[1].Exists || len(post.Sender[1].Balance) != 0 {
		t.Fatalf("absent sender must carry no account fields: %+v", post.Sender[1])
	}
	// Width refusals fail closed.
	wide := new(big.Int).Lsh(big.NewInt(1), 300)
	record.Senders[0].Balance = wide
	if _, err := PostStartRecordToDelta(record); err == nil {
		t.Fatal("33-byte balance accepted")
	}
	if _, err := DrainedWritesToCommitted([32]byte{}, [32]byte{}, []state.ResidentMutation{{
		Key:   state.ResidentMutationKey{Address: common.HexToAddress("0x6000"), Kind: state.ResidentMutationAccount},
		Value: state.ResidentMutationValue{Balance: nil},
	}}); err == nil {
		t.Fatal("nil balance accepted")
	}
}
