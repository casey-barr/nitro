package residentevm

import (
	"bytes"
	"crypto/sha256"
	"errors"
	oldproto "github.com/golang/protobuf/proto"
)

type ResidentEvmDeltaV1 struct {
	WireVersion       uint32                      `protobuf:"varint,1,opt,name=wire_version,json=wireVersion,proto3"`
	SchemaVersion     uint32                      `protobuf:"varint,2,opt,name=schema_version,json=schemaVersion,proto3"`
	LogicalSequence   uint64                      `protobuf:"varint,3,opt,name=logical_sequence,json=logicalSequence,proto3"`
	TransportSequence uint64                      `protobuf:"varint,4,opt,name=transport_sequence,json=transportSequence,proto3"`
	GapEpoch          uint64                      `protobuf:"varint,5,opt,name=gap_epoch,json=gapEpoch,proto3"`
	SchemaHash        []byte                      `protobuf:"bytes,6,opt,name=schema_hash,json=schemaHash,proto3"`
	NodeInstanceID    []byte                      `protobuf:"bytes,7,opt,name=node_instance_id,json=nodeInstanceId,proto3"`
	NodeEpoch         uint64                      `protobuf:"varint,8,opt,name=node_epoch,json=nodeEpoch,proto3"`
	FeatureBits       uint64                      `protobuf:"varint,9,opt,name=feature_bits,json=featureBits,proto3"`
	Record            isResidentEvmDeltaV1_Record `protobuf_oneof:"record"`
}
type isResidentEvmDeltaV1_Record interface{ isResidentEvmDeltaV1_Record() }
type ResidentEvmDeltaV1_Hello struct {
	Hello *Hello `protobuf:"bytes,10,opt,name=hello,proto3,oneof"`
}
type ResidentEvmDeltaV1_PostStartBlock struct {
	PostStartBlock *PostStartBlockWithSenders `protobuf:"bytes,11,opt,name=post_start_block,json=postStartBlock,proto3,oneof"`
}
type ResidentEvmDeltaV1_IncludedTransaction struct {
	IncludedTransaction *IncludedTransaction `protobuf:"bytes,12,opt,name=included_transaction,json=includedTransaction,proto3,oneof"`
}
type ResidentEvmDeltaV1_IncludedGroup struct {
	IncludedGroup *IncludedGroup `protobuf:"bytes,13,opt,name=included_group,json=includedGroup,proto3,oneof"`
}
type ResidentEvmDeltaV1_MessageCommitted struct {
	MessageCommitted *MessageCommitted `protobuf:"bytes,14,opt,name=message_committed,json=messageCommitted,proto3,oneof"`
}
type ResidentEvmDeltaV1_BuildAborted struct {
	BuildAborted *BuildAborted `protobuf:"bytes,15,opt,name=build_aborted,json=buildAborted,proto3,oneof"`
}
type ResidentEvmDeltaV1_Gap struct {
	Gap *Gap `protobuf:"bytes,16,opt,name=gap,proto3,oneof"`
}
type ResidentEvmDeltaV1_Reorg struct {
	Reorg *Reorg `protobuf:"bytes,17,opt,name=reorg,proto3,oneof"`
}
type ResidentEvmDeltaV1_EpochReset struct {
	EpochReset *EpochReset `protobuf:"bytes,18,opt,name=epoch_reset,json=epochReset,proto3,oneof"`
}

func (*ResidentEvmDeltaV1_Hello) isResidentEvmDeltaV1_Record()               {}
func (*ResidentEvmDeltaV1_PostStartBlock) isResidentEvmDeltaV1_Record()      {}
func (*ResidentEvmDeltaV1_IncludedTransaction) isResidentEvmDeltaV1_Record() {}
func (*ResidentEvmDeltaV1_IncludedGroup) isResidentEvmDeltaV1_Record()       {}
func (*ResidentEvmDeltaV1_MessageCommitted) isResidentEvmDeltaV1_Record()    {}
func (*ResidentEvmDeltaV1_BuildAborted) isResidentEvmDeltaV1_Record()        {}
func (*ResidentEvmDeltaV1_Gap) isResidentEvmDeltaV1_Record()                 {}
func (*ResidentEvmDeltaV1_Reorg) isResidentEvmDeltaV1_Record()               {}
func (*ResidentEvmDeltaV1_EpochReset) isResidentEvmDeltaV1_Record()          {}
func (*ResidentEvmDeltaV1) Reset()                                           {}
func (*ResidentEvmDeltaV1) String() string                                   { return "resident_evm_delta_v1" }
func (*ResidentEvmDeltaV1) ProtoMessage()                                    {}
func (*ResidentEvmDeltaV1) XXX_OneofWrappers() []interface{} {
	return []interface{}{(*ResidentEvmDeltaV1_Hello)(nil), (*ResidentEvmDeltaV1_PostStartBlock)(nil), (*ResidentEvmDeltaV1_IncludedTransaction)(nil), (*ResidentEvmDeltaV1_IncludedGroup)(nil), (*ResidentEvmDeltaV1_MessageCommitted)(nil), (*ResidentEvmDeltaV1_BuildAborted)(nil), (*ResidentEvmDeltaV1_Gap)(nil), (*ResidentEvmDeltaV1_Reorg)(nil), (*ResidentEvmDeltaV1_EpochReset)(nil)}
}

var schemaHash = sha256.Sum256([]byte("rhc-resident-evm-delta-v1|oneof-records-v1"))

func TypedSchemaHash() []byte { return append([]byte(nil), schemaHash[:]...) }
func validateRecord(m *ResidentEvmDeltaV1) error {
	if m == nil || m.Record == nil {
		return errors.New("missing record")
	}
	if m.WireVersion != 0 && m.WireVersion != uint32(WireVersion) || m.SchemaVersion != 0 && m.SchemaVersion != uint32(SchemaVersion) {
		return errors.New("unsupported version")
	}
	if len(m.SchemaHash) != 0 && !bytes.Equal(m.SchemaHash, schemaHash[:]) {
		return errors.New("schema hash mismatch")
	}
	if len(m.NodeInstanceID) != 0 && len(m.NodeInstanceID) != 16 {
		return errors.New("node instance id width")
	}
	return nil
}
func MarshalLogical(m *ResidentEvmDeltaV1) ([]byte, error) {
	if err := validateRecord(m); err != nil {
		return nil, err
	}
	b := oldproto.NewBuffer(nil)
	b.SetDeterministic(true)
	if err := b.Marshal(m); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// UnmarshalLogical decodes one logical record into m. m is zeroed first:
// golang/protobuf's Unmarshal contract is reset-then-merge and these hand
// generated types carry no-op Reset() methods, so without the explicit zero a
// reused struct would silently MERGE stale non-zero fields (sequence numbers,
// schema hash, the previous oneof record) under proto3 zero-omission.
func UnmarshalLogical(data []byte, m *ResidentEvmDeltaV1) error {
	if len(data) == 0 || m == nil {
		return errors.New("empty logical record")
	}
	*m = ResidentEvmDeltaV1{}
	if err := oldproto.Unmarshal(data, m); err != nil {
		return err
	}
	return validateRecord(m)
}
