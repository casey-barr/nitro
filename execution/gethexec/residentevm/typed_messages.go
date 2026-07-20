package residentevm
type Hello struct { SchemaHash []byte; SchemaVersion uint32; FeatureBits uint64; NodeInstanceID []byte; NodeEpoch uint64 }
type Sender struct { Address []byte; Nonce uint64; Balance []byte; CodeHash []byte; Code []byte; Exists bool }
type PostStartBlockWithSenders struct { ParentBlock uint64; ChildBlock uint64; MessageDigest []byte; Senders []*Sender }
type IncludedTransaction struct { TxHash []byte; Status uint32; GasUsed uint64; Mutations []*Mutation }
type IncludedGroup struct { GroupHash []byte; Transactions []*IncludedTransaction; Mutations []*Mutation }
type MessageCommitted struct { MessageHash []byte; BlockHash []byte; Mutations []*Mutation }
type BuildAborted struct { Reason string; GapEpoch uint64 }
type Gap struct { GapEpoch uint64; FirstLostSequence uint64 }
type Reorg struct { OldHead []byte; NewHead []byte; CommonBlock uint64 }
type EpochReset struct { NodeEpoch uint64; NodeInstanceID []byte }
func (*Hello) Reset(){};func (*Hello) String()string{return "hello"};func (*Hello) ProtoMessage(){}
func (*Sender) Reset(){};func (*Sender) String()string{return "sender"};func (*Sender) ProtoMessage(){}
func (*PostStartBlockWithSenders) Reset(){};func (*PostStartBlockWithSenders) String()string{return "post_start"};func (*PostStartBlockWithSenders) ProtoMessage(){}
func (*IncludedTransaction) Reset(){};func (*IncludedTransaction) String()string{return "tx"};func (*IncludedTransaction) ProtoMessage(){}
func (*IncludedGroup) Reset(){};func (*IncludedGroup) String()string{return "group"};func (*IncludedGroup) ProtoMessage(){}
func (*MessageCommitted) Reset(){};func (*MessageCommitted) String()string{return "committed"};func (*MessageCommitted) ProtoMessage(){}
func (*BuildAborted) Reset(){};func (*BuildAborted) String()string{return "aborted"};func (*BuildAborted) ProtoMessage(){}
func (*Gap) Reset(){};func (*Gap) String()string{return "gap"};func (*Gap) ProtoMessage(){}
func (*Reorg) Reset(){};func (*Reorg) String()string{return "reorg"};func (*Reorg) ProtoMessage(){}
func (*EpochReset) Reset(){};func (*EpochReset) String()string{return "epoch_reset"};func (*EpochReset) ProtoMessage(){}
