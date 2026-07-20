package residentevm
import ( "bytes"; "crypto/sha256"; "errors"; oldproto "github.com/golang/protobuf/proto" )
type TypedRecordV1 struct { WireVersion uint32; SchemaVersion uint32; LogicalSequence uint64; TransportSequence uint64; GapEpoch uint64; SchemaHash []byte; NodeInstanceID []byte; NodeEpoch uint64; FeatureBits uint64; Record isTypedRecordV1_Record `protobuf_oneof:"record"` }
type isTypedRecordV1_Record interface { isTypedRecordV1_Record() }
type TypedRecordV1_Hello struct { Hello *Hello `protobuf:"bytes,10,opt,name=hello,proto3,oneof"` }
type TypedRecordV1_PostStartBlock struct { PostStartBlock *PostStartBlockWithSenders `protobuf:"bytes,11,opt,name=post_start_block,proto3,oneof"` }
type TypedRecordV1_IncludedTransaction struct { IncludedTransaction *IncludedTransaction `protobuf:"bytes,12,opt,name=included_transaction,proto3,oneof"` }
type TypedRecordV1_IncludedGroup struct { IncludedGroup *IncludedGroup `protobuf:"bytes,13,opt,name=included_group,proto3,oneof"` }
type TypedRecordV1_MessageCommitted struct { MessageCommitted *MessageCommitted `protobuf:"bytes,14,opt,name=message_committed,proto3,oneof"` }
type TypedRecordV1_BuildAborted struct { BuildAborted *BuildAborted `protobuf:"bytes,15,opt,name=build_aborted,proto3,oneof"` }
type TypedRecordV1_Gap struct { Gap *Gap `protobuf:"bytes,16,opt,name=gap,proto3,oneof"` }
type TypedRecordV1_Reorg struct { Reorg *Reorg `protobuf:"bytes,17,opt,name=reorg,proto3,oneof"` }
type TypedRecordV1_EpochReset struct { EpochReset *EpochReset `protobuf:"bytes,18,opt,name=epoch_reset,proto3,oneof"` }
func (*TypedRecordV1_Hello) isTypedRecordV1_Record(){}; func (*TypedRecordV1_PostStartBlock) isTypedRecordV1_Record(){}; func (*TypedRecordV1_IncludedTransaction) isTypedRecordV1_Record(){}; func (*TypedRecordV1_IncludedGroup) isTypedRecordV1_Record(){}; func (*TypedRecordV1_MessageCommitted) isTypedRecordV1_Record(){}; func (*TypedRecordV1_BuildAborted) isTypedRecordV1_Record(){}; func (*TypedRecordV1_Gap) isTypedRecordV1_Record(){}; func (*TypedRecordV1_Reorg) isTypedRecordV1_Record(){}; func (*TypedRecordV1_EpochReset) isTypedRecordV1_Record(){}
func (*TypedRecordV1) Reset(){}; func (*TypedRecordV1) String() string{return "typed_record_v1"}; func (*TypedRecordV1) ProtoMessage(){}
func (*TypedRecordV1) XXX_OneofWrappers() []interface{} { return []interface{}{(*TypedRecordV1_Hello)(nil),(*TypedRecordV1_PostStartBlock)(nil),(*TypedRecordV1_IncludedTransaction)(nil),(*TypedRecordV1_IncludedGroup)(nil),(*TypedRecordV1_MessageCommitted)(nil),(*TypedRecordV1_BuildAborted)(nil),(*TypedRecordV1_Gap)(nil),(*TypedRecordV1_Reorg)(nil),(*TypedRecordV1_EpochReset)(nil)} }
var typedSchemaHash = sha256.Sum256([]byte("rhc-resident-evm-delta-v1|oneof-records-v1"))
func TypedSchemaHash() []byte { return append([]byte(nil), typedSchemaHash[:]...) }
func MarshalTypedRecord(m *TypedRecordV1) ([]byte,error) { if m==nil||m.Record==nil{return nil,errors.New("missing typed record")}; if len(m.SchemaHash)!=0&&!bytes.Equal(m.SchemaHash,typedSchemaHash[:]){return nil,errors.New("schema hash mismatch")}; var b oldproto.Buffer; b.SetDeterministic(true); if err:=b.Marshal(m);err!=nil{return nil,err};return b.Bytes(),nil }
func UnmarshalTypedRecord(data []byte,m *TypedRecordV1) error {if len(data)==0||m==nil{return errors.New("empty typed record")};if err:=oldproto.Unmarshal(data,m);err!=nil{return err};_,err:=MarshalTypedRecord(m);return err}
