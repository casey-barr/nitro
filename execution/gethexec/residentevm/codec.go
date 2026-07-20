package residentevm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	oldproto "github.com/golang/protobuf/proto"
)

type DeltaKind uint32
const (
	DeltaKindInvalid DeltaKind = iota
	DeltaKindPostStartBlock
	DeltaKindIncludedTransaction
	DeltaKindIncludedGroup
	DeltaKindMessageCommitted
	DeltaKindBuildAborted
	DeltaKindGap
	DeltaKindReorg
	DeltaKindEpochReset
)

type Mutation struct { Address []byte `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`; Slot []byte `protobuf:"bytes,2,opt,name=slot,proto3" json:"slot,omitempty"`; Value []byte `protobuf:"bytes,3,opt,name=value,proto3" json:"value,omitempty"`; Deleted bool `protobuf:"varint,4,opt,name=deleted,proto3" json:"deleted,omitempty"`; Code []byte `protobuf:"bytes,5,opt,name=code,proto3" json:"code,omitempty"`; AccountAbsent bool `protobuf:"varint,6,opt,name=account_absent,json=accountAbsent,proto3" json:"account_absent,omitempty"`; BlockNumber uint64 `protobuf:"varint,7,opt,name=block_number,json=blockNumber,proto3" json:"block_number,omitempty"`; BlockHash []byte `protobuf:"bytes,8,opt,name=block_hash,json=blockHash,proto3" json:"blockHash,omitempty"` }
type ResidentEvmDeltaV1 struct { Kind DeltaKind `protobuf:"varint,1,opt,name=kind,proto3" json:"kind,omitempty"`; LogicalSequence uint64 `protobuf:"varint,2,opt,name=logical_sequence,json=logicalSequence,proto3" json:"logical_sequence,omitempty"`; Mutations []*Mutation `protobuf:"bytes,3,rep,name=mutations,proto3" json:"mutations,omitempty"`; WireVersion uint32 `protobuf:"varint,4,opt,name=wire_version,json=wireVersion,proto3" json:"wire_version,omitempty"`; SchemaHash []byte `protobuf:"bytes,5,opt,name=schema_hash,json=schemaHash,proto3" json:"schema_hash,omitempty"`; NodeInstanceID []byte `protobuf:"bytes,6,opt,name=node_instance_id,json=nodeInstanceId,proto3" json:"node_instance_id,omitempty"`; NodeEpoch uint64 `protobuf:"varint,7,opt,name=node_epoch,json=nodeEpoch,proto3" json:"node_epoch,omitempty"`; TransportSequence uint64 `protobuf:"varint,8,opt,name=transport_sequence,json=transportSequence,proto3" json:"transport_sequence,omitempty"`; GapEpoch uint64 `protobuf:"varint,9,opt,name=gap_epoch,json=gapEpoch,proto3" json:"gap_epoch,omitempty"`; SchemaVersion uint32 `protobuf:"varint,10,opt,name=schema_version,json=schemaVersion,proto3" json:"schema_version,omitempty"`; FeatureBits uint64 `protobuf:"varint,11,opt,name=feature_bits,json=featureBits,proto3" json:"feature_bits,omitempty"` }
func (*Mutation) Reset() {} ; func (*Mutation) String() string { return "mutation" }; func (*Mutation) ProtoMessage() {}
func (*ResidentEvmDeltaV1) Reset() {} ; func (*ResidentEvmDeltaV1) String() string { return "resident_evm_delta_v1" }; func (*ResidentEvmDeltaV1) ProtoMessage() {}

var schemaHash = sha256.Sum256([]byte("rhc-resident-evm-delta-v1"))
func validate(m *ResidentEvmDeltaV1) error {
 if m==nil || m.Kind==DeltaKindInvalid || m.Kind>DeltaKindEpochReset{return errors.New("invalid delta kind")}
 if m.WireVersion!=0 && uint16(m.WireVersion)!=WireVersion || m.SchemaVersion!=0 && uint16(m.SchemaVersion)!=SchemaVersion{return errors.New("unsupported version")}
 if len(m.NodeInstanceID)!=0 && len(m.NodeInstanceID)!=16{return errors.New("node instance id width")}
 if len(m.SchemaHash)!=0 && !bytes.Equal(m.SchemaHash,schemaHash[:]){return errors.New("schema hash mismatch")}
 for _,x:=range m.Mutations {if x==nil||len(x.Address)!=20||(len(x.Slot)!=0&&len(x.Slot)!=32)||(len(x.Value)!=0&&len(x.Value)!=32)||(x.Deleted&&len(x.Value)!=0)||(x.AccountAbsent&&len(x.Slot)!=0){return errors.New("invalid mutation")};if len(x.BlockHash)!=0&&len(x.BlockHash)!=32{return errors.New("block hash width")}}
 return nil
}
func MarshalLogical(m *ResidentEvmDeltaV1)([]byte,error){if err:=validate(m);err!=nil{return nil,err};var b oldproto.Buffer;b.SetDeterministic(true);if err:=b.Marshal(m);err!=nil{return nil,err};return b.Bytes(),nil}
func UnmarshalLogical(data []byte,m *ResidentEvmDeltaV1)error{if len(data)==0||m==nil{return errors.New("empty logical record")};if err:=oldproto.Unmarshal(data,m);err!=nil{return err};return validate(m)}

var frameMagic = [8]byte{'R','H','C','E','V','M','0','1'}
type Frame struct { WireVersion uint16; SchemaVersion uint16; FeatureBits uint32; Flags uint32; LogicalSequence uint64; TransportSequence uint64; ChunkIndex uint32; ChunkCount uint32; GapEpoch uint64; Payload []byte }
const frameHeaderSize = 8 + 2 + 2 + 4 + 4 + 8*3 + 4*3 + 32 + 4
func EncodeFrame(f Frame) ([]byte, error) {
	if len(f.Payload) > 4<<20 || f.ChunkCount == 0 || f.ChunkIndex >= f.ChunkCount { return nil, errors.New("invalid frame bounds") }
	h := sha256.Sum256(f.Payload); out := make([]byte, frameHeaderSize+len(f.Payload)); copy(out[:8], frameMagic[:]); p := 8; binary.BigEndian.PutUint16(out[p:], f.WireVersion); p += 2; binary.BigEndian.PutUint16(out[p:], f.SchemaVersion); p += 2; binary.BigEndian.PutUint32(out[p:], f.FeatureBits); p += 4; binary.BigEndian.PutUint32(out[p:], f.Flags); p += 4
	binary.BigEndian.PutUint64(out[p:], f.LogicalSequence); p += 8; binary.BigEndian.PutUint64(out[p:], f.TransportSequence); p += 8; binary.BigEndian.PutUint64(out[p:], f.GapEpoch); p += 8
	binary.BigEndian.PutUint32(out[p:], f.ChunkIndex); p += 4; binary.BigEndian.PutUint32(out[p:], f.ChunkCount); p += 4; binary.BigEndian.PutUint32(out[p:], uint32(len(f.Payload))); p += 4; copy(out[p:p+32], h[:]); p += 32
	copy(out[p:], f.Payload); crc := crc32c(out[:p+len(f.Payload)]); binary.BigEndian.PutUint32(out[p+len(f.Payload):], crc); return out, nil
}
func DecodeFrame(data []byte) (Frame, error) {
	if len(data) < frameHeaderSize || !bytes.Equal(data[:8], frameMagic[:]) { return Frame{}, errors.New("invalid frame header") }; p := 8; f := Frame{}; f.WireVersion = binary.BigEndian.Uint16(data[p:]); p += 2; f.SchemaVersion = binary.BigEndian.Uint16(data[p:]); p += 2; f.FeatureBits = binary.BigEndian.Uint32(data[p:]); p += 4; f.Flags = binary.BigEndian.Uint32(data[p:]); p += 4
	f.LogicalSequence = binary.BigEndian.Uint64(data[p:]); p += 8; f.TransportSequence = binary.BigEndian.Uint64(data[p:]); p += 8; f.GapEpoch = binary.BigEndian.Uint64(data[p:]); p += 8; f.ChunkIndex = binary.BigEndian.Uint32(data[p:]); p += 4; f.ChunkCount = binary.BigEndian.Uint32(data[p:]); p += 4; n := binary.BigEndian.Uint32(data[p:]); p += 4
	if f.ChunkCount == 0 || f.ChunkIndex >= f.ChunkCount || uint64(n) > uint64(len(data)-p-36) { return Frame{}, errors.New("invalid frame bounds") }; want := data[p:p+32]; p += 32; f.Payload = append([]byte(nil), data[p:p+int(n)]...); got := sha256.Sum256(f.Payload); if !bytes.Equal(want, got[:]) || binary.BigEndian.Uint32(data[p+int(n):]) != crc32c(data[:p+int(n)]) { return Frame{}, errors.New("frame checksum mismatch") }; return f, nil
}
func crc32c(data []byte) uint32 { var crc uint32 = 0xffffffff; for _, b := range data { crc ^= uint32(b); for i:=0;i<8;i++ { mask := uint32(0) - (crc & 1); crc = (crc >> 1) ^ (0x82f63b78 & mask) } }; return ^crc }



func ReassembleFrames(frames [][]byte) ([]byte, error) {
 if len(frames)==0 { return nil, errors.New("empty frame set") }
 decoded:=make([]Frame,len(frames)); for i,b:=range frames { f,e:=DecodeFrame(b); if e!=nil{return nil,e}; decoded[i]=f }
 first:=decoded[0]; if first.ChunkCount!=uint32(len(frames)){return nil,errors.New("incomplete chunk set")}
 out:=make([][]byte,len(frames)); for _,f:=range decoded { if f.ChunkCount!=first.ChunkCount||f.LogicalSequence!=first.LogicalSequence||f.TransportSequence!=first.TransportSequence||f.GapEpoch!=first.GapEpoch||f.ChunkIndex>=first.ChunkCount{return nil,errors.New("chunk identity mismatch")}; if out[f.ChunkIndex]!=nil{return nil,errors.New("duplicate chunk")}; out[f.ChunkIndex]=f.Payload }
 var joined []byte; for i,p:=range out {if p==nil{return nil,errors.New("missing chunk")}; if i==0 {joined=append(joined,p...)} else {joined=append(joined,p...)} }; return joined,nil
}
