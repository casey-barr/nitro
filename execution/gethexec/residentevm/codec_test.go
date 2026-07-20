package residentevm

import (
	"bytes"
	"testing"
)

func TestLogicalRoundTripDeterministic(t *testing.T) {
	in := ResidentEvmDeltaV1{Kind: DeltaKindMessageCommitted, LogicalSequence: 7, Mutations: []*Mutation{{Address: bytes.Repeat([]byte{1}, 20), Slot: bytes.Repeat([]byte{2}, 32), Value: bytes.Repeat([]byte{3}, 32)}}}
	b, err := MarshalLogical(&in)
	if err != nil { t.Fatal(err) }
	var out ResidentEvmDeltaV1
	if err := UnmarshalLogical(b, &out); err != nil { t.Fatal(err) }
	if got, err := MarshalLogical(&out); err != nil || !bytes.Equal(got, b) { t.Fatalf("not deterministic: %v", err) }
}

func TestFrameRoundTripAndChecksum(t *testing.T) {
	payload, err := MarshalLogical(&ResidentEvmDeltaV1{Kind: DeltaKindGap})
	if err != nil { t.Fatal(err) }
	f, err := EncodeFrame(Frame{LogicalSequence: 4, Payload: payload})
	if err != nil { t.Fatal(err) }
	got, err := DecodeFrame(f)
	if err != nil || !bytes.Equal(got.Payload, payload) { t.Fatalf("frame: %v", err) }
	f[len(f)-1] ^= 1
	if _, err := DecodeFrame(f); err == nil { t.Fatal("checksum corruption accepted") }
}
