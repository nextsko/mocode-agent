package browser

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jpegBytes encodes a small JPEG of the given size.
func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func TestJpegSize_ReadsTheFrameHeader(t *testing.T) {
	t.Parallel()

	width, height, err := jpegSize(jpegBytes(t, 32, 16))
	require.NoError(t, err)
	assert.Equal(t, 32, width)
	assert.Equal(t, 16, height)
}

func TestJpegSize_RejectsDataThatIsNotAJPEG(t *testing.T) {
	t.Parallel()

	_, _, err := jpegSize([]byte("not a jpeg"))
	assert.Error(t, err)
}

func TestWriteMJPEGAVI_ProducesAParsableVideoFile(t *testing.T) {
	t.Parallel()

	first := jpegBytes(t, 8, 4)
	second := jpegBytes(t, 8, 4)
	frames := []VideoFrame{
		{Data: first, Timestamp: 0},
		{Data: second, Timestamp: 100 * time.Millisecond},
	}

	path := filepath.Join(t.TempDir(), "take.avi")
	require.NoError(t, WriteMJPEGAVI(path, frames, 10))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	require.Greater(t, len(data), 12)
	assert.Equal(t, "RIFF", string(data[0:4]))
	assert.Equal(t, "AVI ", string(data[8:12]))
	assert.Equal(t, uint32(len(data)-8), binary.LittleEndian.Uint32(data[4:8]),
		"the RIFF size must cover the whole file")

	// Walk the top level chunks: hdrl, movi and idx1 in that order.
	chunks := readRiffChunks(t, data[12:])
	require.Len(t, chunks, 3)
	assert.Equal(t, "LIST", chunks[0].id)
	assert.Equal(t, "LIST", chunks[1].id)
	assert.Equal(t, "idx1", chunks[2].id)
	assert.Equal(t, "hdrl", listType(t, chunks[0]))
	assert.Equal(t, "movi", listType(t, chunks[1]))

	// The header must describe the captured size.
	avih := findChunk(t, listPayload(t, chunks[0]), "avih")
	require.NotNil(t, avih)
	assert.Equal(t, uint32(8), binary.LittleEndian.Uint32(avih[32:36]), "width")
	assert.Equal(t, uint32(4), binary.LittleEndian.Uint32(avih[36:40]), "height")
	assert.Equal(t, uint32(2), binary.LittleEndian.Uint32(avih[16:20]), "frame count")

	// Both frames must be stored verbatim.
	frames2 := readRiffChunks(t, listPayload(t, chunks[1]))
	require.Len(t, frames2, 2)
	assert.Equal(t, "00dc", frames2[0].id)
	assert.Equal(t, first, frames2[0].payload)
	assert.Equal(t, second, frames2[1].payload)

	// The index must point at both frames.
	idx := chunks[2].payload
	require.Len(t, idx, 32, "two index entries of sixteen bytes")
	assert.Equal(t, "00dc", string(idx[0:4]))
	assert.Equal(t, uint32(len(first)), binary.LittleEndian.Uint32(idx[12:16]))
	assert.Equal(t, "00dc", string(idx[16:20]))
}

func TestWriteMJPEGAVI_RejectsAnEmptyTake(t *testing.T) {
	t.Parallel()

	err := WriteMJPEGAVI(filepath.Join(t.TempDir(), "empty.avi"), nil, 10)
	assert.Error(t, err)
}

// riffChunk is one chunk of a RIFF stream.
type riffChunk struct {
	id      string
	payload []byte
}

// readRiffChunks splits a RIFF payload into its chunks.
func readRiffChunks(t *testing.T, data []byte) []riffChunk {
	t.Helper()
	var out []riffChunk
	for len(data) >= 8 {
		id := string(data[0:4])
		size := int(binary.LittleEndian.Uint32(data[4:8]))
		require.LessOrEqual(t, size, len(data)-8, "chunk %q claims a size past the end", id)
		out = append(out, riffChunk{id: id, payload: data[8 : 8+size]})
		data = data[8+size:]
		if size%2 == 1 && len(data) > 0 {
			data = data[1:]
		}
	}
	return out
}

// findChunk returns the payload of the first chunk with the given id.
func findChunk(t *testing.T, data []byte, id string) []byte {
	t.Helper()
	for _, chunk := range readRiffChunks(t, data) {
		if chunk.id == id {
			return chunk.payload
		}
	}
	return nil
}

// listType returns the four-byte type of a LIST chunk.
func listType(t *testing.T, chunk riffChunk) string {
	t.Helper()
	require.GreaterOrEqual(t, len(chunk.payload), 4, "a LIST chunk carries a type")
	return string(chunk.payload[0:4])
}

// listPayload returns the contents of a LIST chunk, without its type.
func listPayload(t *testing.T, chunk riffChunk) []byte {
	t.Helper()
	require.GreaterOrEqual(t, len(chunk.payload), 4, "a LIST chunk carries a type")
	return chunk.payload[4:]
}
