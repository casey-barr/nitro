package residentevm
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"testing"
)
func TestLogicalRoundTripDeterministic(t *testing.T){in:=&ResidentEvmDeltaV1{WireVersion:1,SchemaVersion:1,LogicalSequence:7,Record:&ResidentEvmDeltaV1_MessageCommitted{MessageCommitted:&MessageCommitted{MessageHash:bytes.Repeat([]byte{7},32),Mutations:[]*Mutation{{Address:bytes.Repeat([]byte{1},20),Slot:bytes.Repeat([]byte{2},32),Value:bytes.Repeat([]byte{3},32)}}}}};b,err:=MarshalLogical(in);if err!=nil{t.Fatal(err)};var out ResidentEvmDeltaV1;if err=UnmarshalLogical(b,&out);err!=nil{t.Fatal(err)};got,err:=MarshalLogical(&out);if err!=nil||!bytes.Equal(got,b){t.Fatalf("not deterministic: %v",err)}}
func TestFrameRoundTripAndChecksum(t *testing.T) {
	payload, _ := MarshalLogical(&ResidentEvmDeltaV1{WireVersion: 1, SchemaVersion: 1, Record: &ResidentEvmDeltaV1_Gap{Gap: &Gap{GapEpoch: 1}}})
	f, err := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, ChunkCount: 1, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFrame(f)
	if err != nil || !bytes.Equal(got.Payload, payload) {
		t.Fatalf("frame: %v", err)
	}
	f[len(f)-1] ^= 1
	if _, err = DecodeFrame(f); err == nil {
		t.Fatal("checksum corruption accepted")
	}
}

// Pins the canonical RHCEVM01 header layout the Rust consumer decodes
// (rhc-v2 crates/rhc-resident-evm-protocol/src/frame.rs): 96-byte header,
// schema_hash at [16..48], digest at [60..92], crc32c at [92..96] covering
// header[0..92] plus the payload.
func TestFrameHeaderLayoutIsCanonical(t *testing.T) {
	payload := []byte{0xAA, 0xBB, 0xCC}
	f, err := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, FeatureBits: 0, SchemaHash: schemaHash, ChunkIndex: 0, ChunkCount: 1, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 96+len(payload) {
		t.Fatalf("frame length %d, want %d", len(f), 96+len(payload))
	}
	if !bytes.Equal(f[0:8], []byte("RHCEVM01")) {
		t.Fatal("magic")
	}
	if binary.BigEndian.Uint16(f[8:10]) != 1 || binary.BigEndian.Uint16(f[10:12]) != 1 || binary.BigEndian.Uint32(f[12:16]) != 0 {
		t.Fatal("version/features field placement")
	}
	if !bytes.Equal(f[16:48], schemaHash[:]) {
		t.Fatal("schema hash must occupy [16..48]")
	}
	if binary.BigEndian.Uint32(f[48:52]) != 0 || binary.BigEndian.Uint32(f[52:56]) != 1 || binary.BigEndian.Uint32(f[56:60]) != uint32(len(payload)) {
		t.Fatal("idx/count/n placement")
	}
	digest := sha256.Sum256(payload)
	if !bytes.Equal(f[60:92], digest[:]) {
		t.Fatal("payload digest must occupy [60..92]")
	}
	crc := crc32.Update(0, castagnoli, f[:92])
	crc = crc32.Update(crc, castagnoli, payload)
	if binary.BigEndian.Uint32(f[92:96]) != crc {
		t.Fatal("crc32c must cover header[0..92] plus payload, at [92..96]")
	}
	if !bytes.Equal(f[96:], payload) {
		t.Fatal("payload must follow the 96-byte header")
	}
}

func TestEncodeFramesChunksAndReassembles(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5A}, (MaxFrameBytes-96)+1234)
	frames, err := EncodeFrames(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("chunks = %d, want 2", len(frames))
	}
	joined, err := ReassembleFrames(frames)
	if err != nil || !bytes.Equal(joined, payload) {
		t.Fatalf("reassembly: %v", err)
	}
}
func TestFrameRejectsTrailingAndMixedMetadata(t *testing.T) {
	f, _ := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, ChunkCount: 1, Payload: []byte{1}})
	if _, err := DecodeFrame(append(f, 0)); err == nil {
		t.Fatal("trailing bytes accepted")
	}
	g, _ := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, FeatureBits: 0, ChunkCount: 2, ChunkIndex: 0, Payload: []byte{1}})
	h, _ := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, FeatureBits: 7, ChunkCount: 2, ChunkIndex: 1, Payload: []byte{2}})
	if _, err := ReassembleFrames([][]byte{g, h}); err == nil {
		t.Fatal("mixed protocol tuple accepted")
	}
}
