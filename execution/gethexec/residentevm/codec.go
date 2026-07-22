package residentevm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const (
	WireVersion   uint16 = 1
	SchemaVersion uint16 = 1
	// MaxFrameBytes bounds the WHOLE frame (header + payload), matching the
	// Rust consumer's FrameBounds.max_frame_bytes. The largest legal payload
	// per frame is therefore MaxFrameBytes - frameHeaderSize.
	MaxFrameBytes = 1 << 20
	// MaxLogicalBytes bounds the reassembled logical payload, matching the
	// Rust consumer's FrameBounds.max_logical_bytes.
	MaxLogicalBytes        = 4 << 20
	MaxChunkCount   uint32 = 4096
)

var frameMagic = [8]byte{'R', 'H', 'C', 'E', 'V', 'M', '0', '1'}

// Canonical RHCEVM01 header, byte-identical to the Rust consumer
// (rhc-v2 crates/rhc-resident-evm-protocol/src/frame.rs):
//
//	magic[0..8] major[8..10] minor[10..12] features[12..16]
//	schema_hash[16..48] idx[48..52] count[52..56] n[56..60]
//	digest[60..92] crc32c[92..96]
//
// crc32c (Castagnoli) covers header[0..92] followed by the payload; the CRC
// field itself is excluded. digest is sha256 of this frame's payload chunk.
const frameHeaderSize = 96

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

type Frame struct {
	WireVersion   uint16
	SchemaVersion uint16
	FeatureBits   uint32
	SchemaHash    [32]byte
	ChunkIndex    uint32
	ChunkCount    uint32
	Payload       []byte
}

func validateFrameMeta(f Frame, payloadLen int) error {
	if f.WireVersion != WireVersion || f.SchemaVersion != SchemaVersion {
		return errors.New("unsupported frame version")
	}
	if f.SchemaHash != schemaHash {
		return errors.New("unsupported schema hash")
	}
	if f.ChunkCount == 0 || f.ChunkCount > MaxChunkCount || f.ChunkIndex >= f.ChunkCount {
		return errors.New("invalid frame bounds")
	}
	if payloadLen > MaxFrameBytes-frameHeaderSize {
		return errors.New("frame payload too large")
	}
	return nil
}

func EncodeFrame(f Frame) ([]byte, error) {
	if err := validateFrameMeta(f, len(f.Payload)); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(f.Payload)
	out := make([]byte, frameHeaderSize+len(f.Payload))
	copy(out[0:8], frameMagic[:])
	binary.BigEndian.PutUint16(out[8:10], f.WireVersion)
	binary.BigEndian.PutUint16(out[10:12], f.SchemaVersion)
	binary.BigEndian.PutUint32(out[12:16], f.FeatureBits)
	copy(out[16:48], f.SchemaHash[:])
	binary.BigEndian.PutUint32(out[48:52], f.ChunkIndex)
	binary.BigEndian.PutUint32(out[52:56], f.ChunkCount)
	binary.BigEndian.PutUint32(out[56:60], uint32(len(f.Payload)))
	copy(out[60:92], digest[:])
	copy(out[frameHeaderSize:], f.Payload)
	crc := crc32.Update(0, castagnoli, out[:92])
	crc = crc32.Update(crc, castagnoli, f.Payload)
	binary.BigEndian.PutUint32(out[92:96], crc)
	return out, nil
}

func DecodeFrame(data []byte) (Frame, error) {
	if len(data) < frameHeaderSize || !bytes.Equal(data[0:8], frameMagic[:]) {
		return Frame{}, errors.New("invalid frame header")
	}
	if len(data) > MaxFrameBytes {
		return Frame{}, errors.New("frame too large")
	}
	var f Frame
	f.WireVersion = binary.BigEndian.Uint16(data[8:10])
	f.SchemaVersion = binary.BigEndian.Uint16(data[10:12])
	f.FeatureBits = binary.BigEndian.Uint32(data[12:16])
	copy(f.SchemaHash[:], data[16:48])
	f.ChunkIndex = binary.BigEndian.Uint32(data[48:52])
	f.ChunkCount = binary.BigEndian.Uint32(data[52:56])
	n := binary.BigEndian.Uint32(data[56:60])
	// Bounds are checked numerically before any payload-sized work: a hostile
	// n cannot force an allocation.
	if uint64(n) > uint64(MaxFrameBytes-frameHeaderSize) {
		return Frame{}, errors.New("frame payload too large")
	}
	if err := validateFrameMeta(f, int(n)); err != nil {
		return Frame{}, err
	}
	if len(data) != frameHeaderSize+int(n) {
		return Frame{}, errors.New("invalid frame length")
	}
	payload := data[frameHeaderSize:]
	digest := sha256.Sum256(payload)
	if !bytes.Equal(digest[:], data[60:92]) {
		return Frame{}, errors.New("frame digest mismatch")
	}
	crc := crc32.Update(0, castagnoli, data[:92])
	crc = crc32.Update(crc, castagnoli, payload)
	if binary.BigEndian.Uint32(data[92:96]) != crc {
		return Frame{}, errors.New("frame checksum mismatch")
	}
	f.Payload = append([]byte(nil), payload...)
	return f, nil
}

// EncodeFrames chunks one logical payload into wire frames — the producer-side
// mirror of the Rust consumer's FrameDecoder reassembly (and of the Rust
// reference producer encode_frames). An empty payload still produces one
// empty-chunk frame.
func EncodeFrames(payload []byte) ([][]byte, error) {
	if len(payload) > MaxLogicalBytes {
		return nil, errors.New("logical payload too large")
	}
	chunkCapacity := MaxFrameBytes - frameHeaderSize
	chunks := 1
	if len(payload) > 0 {
		chunks = (len(payload) + chunkCapacity - 1) / chunkCapacity
	}
	if uint32(chunks) > MaxChunkCount {
		return nil, errors.New("logical payload needs too many chunks")
	}
	frames := make([][]byte, 0, chunks)
	for i := 0; i < chunks; i++ {
		start := i * chunkCapacity
		end := start + chunkCapacity
		if end > len(payload) {
			end = len(payload)
		}
		frame, err := EncodeFrame(Frame{
			WireVersion:   WireVersion,
			SchemaVersion: SchemaVersion,
			SchemaHash:    schemaHash,
			ChunkIndex:    uint32(i),
			ChunkCount:    uint32(chunks),
			Payload:       payload[start:end],
		})
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func ReassembleFrames(frames [][]byte) ([]byte, error) {
	if len(frames) == 0 || len(frames) > int(MaxChunkCount) {
		return nil, errors.New("invalid frame set")
	}
	decoded := make([]Frame, len(frames))
	total := 0
	for i, b := range frames {
		f, err := DecodeFrame(b)
		if err != nil {
			return nil, err
		}
		decoded[i] = f
		total += len(f.Payload)
		if total > MaxLogicalBytes {
			return nil, errors.New("aggregate payload too large")
		}
	}
	first := decoded[0]
	if first.ChunkCount != uint32(len(frames)) {
		return nil, errors.New("incomplete chunk set")
	}
	out := make([][]byte, len(frames))
	for _, f := range decoded {
		// The whole protocol tuple must match across a chunk set, mirroring
		// the Rust FrameDecoder's per-instance expected tuple.
		if f.ChunkCount != first.ChunkCount ||
			f.WireVersion != first.WireVersion ||
			f.SchemaVersion != first.SchemaVersion ||
			f.FeatureBits != first.FeatureBits ||
			f.SchemaHash != first.SchemaHash {
			return nil, errors.New("chunk identity mismatch")
		}
		if out[f.ChunkIndex] != nil {
			return nil, errors.New("duplicate chunk")
		}
		out[f.ChunkIndex] = f.Payload
	}
	joined := make([]byte, 0, total)
	for _, p := range out {
		if p == nil {
			return nil, errors.New("missing chunk")
		}
		joined = append(joined, p...)
	}
	return joined, nil
}
