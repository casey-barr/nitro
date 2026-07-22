package gethexec

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/offchainlabs/nitro/execution/gethexec/residentevm"
)

// Resident producer conversion: turns the two capture surfaces — the observer's
// post-StartBlock sender snapshots and the geth collector's coalesced drained
// writes — into wire-shape [`residentevm.ResidentEvmDeltaV1`] records at the
// canonical-append boundary. Pure conversion, no transport, no sequencing
// state: the (future) exporter owns envelope sequence numbers, node identity,
// and the socket. Everything here is dormant until that exporter exists.
//
// Contract rules enforced here (the Rust consumer refuses violations):
//   - An absent account normalizes to {account_absent:true, deleted:false} on
//     the wire — the collector's internal value carries both flags, but the
//     consumer's strict absent shape refuses `deleted`.
//   - A live account record carries nonce, a 32-byte big-endian balance, and a
//     32-byte code hash; `deleted:true` rides through as wipe-then-apply.
//   - A code record carries only the code bytes (the consumer derives and
//     verifies the hash itself).
//   - Storage records carry a 32-byte slot and value; a deleted slot carries
//     no value.
//   - The drain's deterministic order (address, then kind Account < Storage <
//     Code) is preserved: an account wipe lands before the slots and code
//     that follow it.

var (
	errResidentProducerBalanceWidth = errors.New("resident producer: balance exceeds 32 bytes")
	errResidentProducerNilBalance   = errors.New("resident producer: nil balance on live account")
	errResidentProducerHashWidth    = errors.New("resident producer: hash is not 32 bytes")
)

// residentDelta stamps the envelope fields the conversion layer owns (schema
// identity and versions). Sequence numbers and node identity stay zero here;
// the exporter assigns them at emission time.
func residentDelta(record interface{}) *residentevm.ResidentEvmDeltaV1 {
	delta := &residentevm.ResidentEvmDeltaV1{
		WireVersion:   uint32(residentevm.WireVersion),
		SchemaVersion: uint32(residentevm.SchemaVersion),
		SchemaHash:    residentevm.TypedSchemaHash(),
	}
	switch typed := record.(type) {
	case *residentevm.PostStartBlockWithSenders:
		delta.Record = &residentevm.ResidentEvmDeltaV1_PostStartBlock{PostStartBlock: typed}
	case *residentevm.MessageCommitted:
		delta.Record = &residentevm.ResidentEvmDeltaV1_MessageCommitted{MessageCommitted: typed}
	}
	return delta
}

// PostStartRecordToDelta converts one retained observer record into the wire
// PostStartBlock record. Balances are big-endian 32-byte; nil balance on an
// existing sender or anything wider than 32 bytes fails closed.
func PostStartRecordToDelta(record ResidentPostStartRecord) (*residentevm.ResidentEvmDeltaV1, error) {
	senders := make([]*residentevm.Sender, 0, len(record.Senders))
	for _, sender := range record.Senders {
		wire := &residentevm.Sender{
			Address: sender.Address.Bytes(),
			Nonce:   sender.Nonce,
			Exists:  sender.Exists,
		}
		if sender.Exists {
			if sender.Balance == nil {
				return nil, errResidentProducerNilBalance
			}
			balance, err := balance32(sender.Balance.Bytes())
			if err != nil {
				return nil, err
			}
			wire.Balance = balance
			wire.CodeHash = sender.CodeHash.Bytes()
			wire.Code = append([]byte(nil), sender.Code...)
		}
		senders = append(senders, wire)
	}
	return residentDelta(&residentevm.PostStartBlockWithSenders{
		ParentBlock:   record.ParentBlockNumber,
		ChildBlock:    record.ChildBlockNumber,
		MessageDigest: record.MessageDigest.Bytes(),
		Sender:        senders,
	}), nil
}

// DrainedWritesToCommitted converts one boundary's coalesced drained writes
// into the wire MessageCommitted record, applying the normalization rules
// above. messageHash and blockHash identify the committed boundary.
func DrainedWritesToCommitted(messageHash, blockHash [32]byte, writes []state.ResidentMutation) (*residentevm.ResidentEvmDeltaV1, error) {
	mutations := make([]*residentevm.Mutation, 0, len(writes))
	for _, write := range writes {
		mutation, err := drainedWriteToMutation(write)
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, mutation)
	}
	return residentDelta(&residentevm.MessageCommitted{
		MessageHash: messageHash[:],
		BlockHash:   blockHash[:],
		Mutations:   mutations,
	}), nil
}

func drainedWriteToMutation(write state.ResidentMutation) (*residentevm.Mutation, error) {
	switch write.Key.Kind {
	case state.ResidentMutationAccount:
		if write.Value.AccountAbsent {
			// Normalize: the consumer's strict absent shape refuses `deleted`.
			return &residentevm.Mutation{
				Address:       write.Key.Address.Bytes(),
				AccountAbsent: true,
			}, nil
		}
		if write.Value.Balance == nil {
			return nil, errResidentProducerNilBalance
		}
		balance, err := balance32(write.Value.Balance.Bytes())
		if err != nil {
			return nil, err
		}
		codeHash := write.Value.CodeHash.Bytes()
		if len(codeHash) != 32 {
			return nil, errResidentProducerHashWidth
		}
		return &residentevm.Mutation{
			Address:  write.Key.Address.Bytes(),
			Nonce:    write.Value.Nonce,
			Balance:  balance,
			CodeHash: codeHash,
			// Wipe-then-apply rides through: delete-then-recreate within the
			// boundary must clear the prior incarnation's storage first.
			Deleted: write.Value.Deleted,
		}, nil
	case state.ResidentMutationStorage:
		mutation := &residentevm.Mutation{
			Address: write.Key.Address.Bytes(),
			Slot:    write.Key.Slot.Bytes(),
			Deleted: write.Value.Deleted,
		}
		if !write.Value.Deleted {
			mutation.Value = write.Value.Value.Bytes()
		}
		return mutation, nil
	case state.ResidentMutationCode:
		return &residentevm.Mutation{
			Address: write.Key.Address.Bytes(),
			Code:    append([]byte(nil), write.Value.Code...),
		}, nil
	default:
		return nil, fmt.Errorf("resident producer: unknown mutation kind %d", write.Key.Kind)
	}
}

// balance32 left-pads a big-endian balance to exactly 32 bytes, refusing
// anything wider.
func balance32(raw []byte) ([]byte, error) {
	if len(raw) > 32 {
		return nil, errResidentProducerBalanceWidth
	}
	out := make([]byte, 32)
	copy(out[32-len(raw):], raw)
	return out, nil
}
