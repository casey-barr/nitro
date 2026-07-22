package residentevm

// The oneof payload messages of ResidentEvmDeltaV1. Field tags mirror
// resident_evm_delta_v1.proto exactly and are cross-checked against the Rust
// consumer's prost types (rhc-v2 crates/rhc-resident-evm-protocol/src/
// generated.rs); the vendored golden vectors pin the encodings. This file was
// missing from the original branch — the package referenced these types but
// never defined them, so it had never compiled.

type Hello struct {
	SchemaHash     []byte `protobuf:"bytes,1,opt,name=schema_hash,json=schemaHash,proto3"`
	SchemaVersion  uint32 `protobuf:"varint,2,opt,name=schema_version,json=schemaVersion,proto3"`
	FeatureBits    uint64 `protobuf:"varint,3,opt,name=feature_bits,json=featureBits,proto3"`
	NodeInstanceID []byte `protobuf:"bytes,4,opt,name=node_instance_id,json=nodeInstanceId,proto3"`
	NodeEpoch      uint64 `protobuf:"varint,5,opt,name=node_epoch,json=nodeEpoch,proto3"`
}

func (*Hello) Reset()         {}
func (*Hello) String() string { return "hello" }
func (*Hello) ProtoMessage()  {}

type PostStartBlockWithSenders struct {
	ParentBlock   uint64    `protobuf:"varint,1,opt,name=parent_block,json=parentBlock,proto3"`
	ChildBlock    uint64    `protobuf:"varint,2,opt,name=child_block,json=childBlock,proto3"`
	MessageDigest []byte    `protobuf:"bytes,3,opt,name=message_digest,json=messageDigest,proto3"`
	Sender        []*Sender `protobuf:"bytes,4,rep,name=sender,proto3"`
}

func (*PostStartBlockWithSenders) Reset()         {}
func (*PostStartBlockWithSenders) String() string { return "post_start_block" }
func (*PostStartBlockWithSenders) ProtoMessage()  {}

type Sender struct {
	Address  []byte `protobuf:"bytes,1,opt,name=address,proto3"`
	Nonce    uint64 `protobuf:"varint,2,opt,name=nonce,proto3"`
	Balance  []byte `protobuf:"bytes,3,opt,name=balance,proto3"`
	CodeHash []byte `protobuf:"bytes,4,opt,name=code_hash,json=codeHash,proto3"`
	Code     []byte `protobuf:"bytes,5,opt,name=code,proto3"`
	Exists   bool   `protobuf:"varint,6,opt,name=exists,proto3"`
}

func (*Sender) Reset()         {}
func (*Sender) String() string { return "sender" }
func (*Sender) ProtoMessage()  {}

type Mutation struct {
	Address       []byte `protobuf:"bytes,1,opt,name=address,proto3"`
	Slot          []byte `protobuf:"bytes,2,opt,name=slot,proto3"`
	Value         []byte `protobuf:"bytes,3,opt,name=value,proto3"`
	Deleted       bool   `protobuf:"varint,4,opt,name=deleted,proto3"`
	Code          []byte `protobuf:"bytes,5,opt,name=code,proto3"`
	AccountAbsent bool   `protobuf:"varint,6,opt,name=account_absent,json=accountAbsent,proto3"`
	BlockNumber   uint64 `protobuf:"varint,7,opt,name=block_number,json=blockNumber,proto3"`
	BlockHash     []byte `protobuf:"bytes,8,opt,name=block_hash,json=blockHash,proto3"`
}

func (*Mutation) Reset()         {}
func (*Mutation) String() string { return "mutation" }
func (*Mutation) ProtoMessage()  {}

type IncludedTransaction struct {
	TxHash    []byte      `protobuf:"bytes,1,opt,name=tx_hash,json=txHash,proto3"`
	Status    uint32      `protobuf:"varint,2,opt,name=status,proto3"`
	GasUsed   uint64      `protobuf:"varint,3,opt,name=gas_used,json=gasUsed,proto3"`
	Mutations []*Mutation `protobuf:"bytes,4,rep,name=mutations,proto3"`
}

func (*IncludedTransaction) Reset()         {}
func (*IncludedTransaction) String() string { return "included_transaction" }
func (*IncludedTransaction) ProtoMessage()  {}

type IncludedGroup struct {
	GroupHash    []byte                 `protobuf:"bytes,1,opt,name=group_hash,json=groupHash,proto3"`
	Transactions []*IncludedTransaction `protobuf:"bytes,2,rep,name=transactions,proto3"`
	Mutations    []*Mutation            `protobuf:"bytes,3,rep,name=mutations,proto3"`
}

func (*IncludedGroup) Reset()         {}
func (*IncludedGroup) String() string { return "included_group" }
func (*IncludedGroup) ProtoMessage()  {}

type MessageCommitted struct {
	MessageHash []byte      `protobuf:"bytes,1,opt,name=message_hash,json=messageHash,proto3"`
	BlockHash   []byte      `protobuf:"bytes,2,opt,name=block_hash,json=blockHash,proto3"`
	Mutations   []*Mutation `protobuf:"bytes,3,rep,name=mutations,proto3"`
}

func (*MessageCommitted) Reset()         {}
func (*MessageCommitted) String() string { return "message_committed" }
func (*MessageCommitted) ProtoMessage()  {}

type BuildAborted struct {
	Reason   string `protobuf:"bytes,1,opt,name=reason,proto3"`
	GapEpoch uint64 `protobuf:"varint,2,opt,name=gap_epoch,json=gapEpoch,proto3"`
}

func (*BuildAborted) Reset()         {}
func (*BuildAborted) String() string { return "build_aborted" }
func (*BuildAborted) ProtoMessage()  {}

type Gap struct {
	GapEpoch          uint64 `protobuf:"varint,1,opt,name=gap_epoch,json=gapEpoch,proto3"`
	FirstLostSequence uint64 `protobuf:"varint,2,opt,name=first_lost_sequence,json=firstLostSequence,proto3"`
}

func (*Gap) Reset()         {}
func (*Gap) String() string { return "gap" }
func (*Gap) ProtoMessage()  {}

type Reorg struct {
	OldHead     []byte `protobuf:"bytes,1,opt,name=old_head,json=oldHead,proto3"`
	NewHead     []byte `protobuf:"bytes,2,opt,name=new_head,json=newHead,proto3"`
	CommonBlock uint64 `protobuf:"varint,3,opt,name=common_block,json=commonBlock,proto3"`
}

func (*Reorg) Reset()         {}
func (*Reorg) String() string { return "reorg" }
func (*Reorg) ProtoMessage()  {}

type EpochReset struct {
	NodeEpoch      uint64 `protobuf:"varint,1,opt,name=node_epoch,json=nodeEpoch,proto3"`
	NodeInstanceID []byte `protobuf:"bytes,2,opt,name=node_instance_id,json=nodeInstanceId,proto3"`
}

func (*EpochReset) Reset()         {}
func (*EpochReset) String() string { return "epoch_reset" }
func (*EpochReset) ProtoMessage()  {}
