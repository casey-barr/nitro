package residentevm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"os"
	"testing"
)

func TestLogicalRoundTripDeterministic(t *testing.T) {
	in := &ResidentEvmDeltaV1{WireVersion: 1, SchemaVersion: 1, LogicalSequence: 7, Record: &ResidentEvmDeltaV1_MessageCommitted{MessageCommitted: &MessageCommitted{MessageHash: bytes.Repeat([]byte{7}, 32), Mutations: []*Mutation{{Address: bytes.Repeat([]byte{1}, 20), Slot: bytes.Repeat([]byte{2}, 32), Value: bytes.Repeat([]byte{3}, 32)}}}}}
	b, err := MarshalLogical(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ResidentEvmDeltaV1
	if err = UnmarshalLogical(b, &out); err != nil {
		t.Fatal(err)
	}
	got, err := MarshalLogical(&out)
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("not deterministic: %v", err)
	}
}
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
func TestFrameRejectsTrailingAndDuplicateChunks(t *testing.T) {
	f, _ := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, ChunkCount: 1, Payload: []byte{1}})
	if _, err := DecodeFrame(append(f, 0)); err == nil {
		t.Fatal("trailing bytes accepted")
	}
	g, _ := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, ChunkCount: 2, ChunkIndex: 0, Payload: []byte{1}})
	if _, err := ReassembleFrames([][]byte{g, g}); err == nil {
		t.Fatal("duplicate chunk accepted")
	}
	if _, err := ReassembleFrames([][]byte{g}); err == nil {
		t.Fatal("incomplete chunk set accepted")
	}
}

// The exact frame bytes for the golden Gap record, computed independently with
// true CRC32c (Castagnoli). The identical literal is pinned in the Rust
// consumer's tests (rhc-v2 frame.rs GOLDEN_GAP_FRAME_HEX): a checksum or
// layout drift on either side fails a unit test instead of killing the socket.
const goldenGapFrameHex = "52484345564d303100010001000000008c464a1e344c35c0728572a069d890166218e86d8d7e7aa0d330b74bac18280f00000000000000010000000933018efc79bd62706d2677890fefa6acf22fbabac61f450696e2770b8d88b39a7f85fb34080110018201020801"

func TestGoldenFrameBytesArePinnedCrossLanguage(t *testing.T) {
	golden, err := hex.DecodeString(goldenGapFrameHex)
	if err != nil {
		t.Fatal(err)
	}
	payload := golden[96:]
	built, err := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, ChunkCount: 1, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(built, golden) {
		t.Fatalf("encoder diverges from the golden frame: got %x want %x", built, golden)
	}
	decoded, err := DecodeFrame(golden)
	if err != nil || !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("decoder refused the golden frame: %v", err)
	}
	// IEEE CRC32 (the polynomial the round-2 review caught on the Rust side)
	// must be refused.
	ieee := append([]byte(nil), golden...)
	binary.BigEndian.PutUint32(ieee[92:96], 0x4cda0129)
	if _, err := DecodeFrame(ieee); err == nil {
		t.Fatal("IEEE crc32 accepted — polynomial drift undetected")
	}
}

func TestGoldenVectorsRoundTripThroughTheStructs(t *testing.T) {
	raw, err := os.ReadFile("golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Records []struct {
			Name       string `json:"name"`
			PayloadHex string `json:"payload_hex"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Records) != 9 {
		t.Fatalf("golden manifest must pin all 9 records, has %d", len(manifest.Records))
	}
	for _, record := range manifest.Records {
		payload, err := hex.DecodeString(record.PayloadHex)
		if err != nil {
			t.Fatalf("%s: %v", record.Name, err)
		}
		var m ResidentEvmDeltaV1
		if err := UnmarshalLogical(payload, &m); err != nil {
			t.Fatalf("%s: unmarshal: %v", record.Name, err)
		}
		out, err := MarshalLogical(&m)
		if err != nil || !bytes.Equal(out, payload) {
			t.Fatalf("%s: tag drift — re-marshal diverges from the golden bytes", record.Name)
		}
	}
}

// A reused struct must be zeroed, not merged: proto3 omits zero values, so a
// no-op Reset would let message N's non-zero fields survive into message N+1.
func TestUnmarshalZeroesReusedStructs(t *testing.T) {
	first, _ := MarshalLogical(&ResidentEvmDeltaV1{WireVersion: 1, SchemaVersion: 1, LogicalSequence: 7, GapEpoch: 3, Record: &ResidentEvmDeltaV1_Gap{Gap: &Gap{GapEpoch: 3}}})
	second, _ := MarshalLogical(&ResidentEvmDeltaV1{WireVersion: 1, SchemaVersion: 1, Record: &ResidentEvmDeltaV1_Hello{Hello: &Hello{SchemaHash: TypedSchemaHash(), SchemaVersion: 1, NodeInstanceID: bytes.Repeat([]byte{1}, 16), NodeEpoch: 1}}})
	var m ResidentEvmDeltaV1
	if err := UnmarshalLogical(first, &m); err != nil {
		t.Fatal(err)
	}
	if err := UnmarshalLogical(second, &m); err != nil {
		t.Fatal(err)
	}
	if m.LogicalSequence != 0 || m.GapEpoch != 0 {
		t.Fatalf("stale fields merged into reused struct: seq=%d gapEpoch=%d", m.LogicalSequence, m.GapEpoch)
	}
	if _, ok := m.Record.(*ResidentEvmDeltaV1_Hello); !ok {
		t.Fatalf("stale oneof survived: %T", m.Record)
	}
}

func TestDecodeFrameNegativeCases(t *testing.T) {
	valid, _ := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, ChunkCount: 1, Payload: []byte{0xAA}})
	mutate := func(mutator func([]byte)) []byte {
		frame := append([]byte(nil), valid...)
		mutator(frame)
		return frame
	}
	cases := map[string][]byte{
		"wrong magic":      mutate(func(f []byte) { f[0] = 'X' }),
		"wrong schema":     mutate(func(f []byte) { f[16] ^= 1 }),
		"nonzero features": mutate(func(f []byte) { f[15] = 1 }),
		"zero chunk count": mutate(func(f []byte) { binary.BigEndian.PutUint32(f[52:56], 0) }),
		"hostile n":        mutate(func(f []byte) { binary.BigEndian.PutUint32(f[56:60], 0xFFFFFFFF) }),
		"digest corrupt":   mutate(func(f []byte) { f[61] ^= 1 }),
		"truncated":        valid[:95],
	}
	for name, frame := range cases {
		if _, err := DecodeFrame(frame); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if _, err := EncodeFrame(Frame{WireVersion: 1, SchemaVersion: 1, SchemaHash: schemaHash, FeatureBits: 7, ChunkCount: 1, Payload: []byte{1}}); err == nil {
		t.Fatal("nonzero feature bits encoded")
	}
}
