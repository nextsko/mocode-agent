package browser

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// u32 converts a byte count or an offset into the 32-bit field an AVI header uses.
//
// The values are bounded by the size of one recording, and the frame cap keeps
// them far inside the range; the check makes the conversion honest instead of
// trusting the caller.
func u32(v int) uint32 {
	if v < 0 || int64(v) > math.MaxUint32 {
		return 0
	}
	return uint32(v)
}

// u16 converts a dimension into the 16-bit field an AVI header uses.
func u16(v int) uint16 {
	if v < 0 || v > math.MaxUint16 {
		return 0
	}
	return uint16(v)
}

// VideoFrame is one captured frame and its offset from the start of the take.
type VideoFrame struct {
	Data      []byte
	Timestamp time.Duration
}

// WriteMJPEGAVI writes the frames as an MJPEG video inside an AVI container.
//
// AVI with MJPEG is the one video container that carries JPEG frames
// verbatim: no codec, no encoder, no external binary. Chrome's screencast
// already hands us JPEG frames, so the frames go into the file unchanged and
// the recording stays a real video file rather than a pile of images.
//
// The playback rate is a single frame rate for the whole file, which is what
// classic AVI describes; the rate is derived from the captured timestamps so
// the file plays back at roughly the speed it was recorded.
func WriteMJPEGAVI(path string, frames []VideoFrame, fps int) (err error) {
	if len(frames) == 0 {
		return errors.New("no frames to write")
	}
	if fps <= 0 {
		fps = defaultRecordFPS
	}

	width, height, err := jpegSize(frames[0].Data)
	if err != nil {
		return fmt.Errorf("reading frame size: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	riffSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(riffSize, 0)
	buf.Write(riffSize)
	buf.WriteString("AVI ")

	// hdrl: avih + strl(strh, strf).
	hdrl := buildAviHeader(width, height, len(frames), fps)
	writeList(&buf, "hdrl", hdrl)

	// movi: the frames themselves.
	moviStart := buf.Len()
	buf.WriteString("LIST")
	moviSizePos := buf.Len()
	moviSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(moviSize, 0)
	buf.Write(moviSize)
	moviFourCC := buf.Len()
	buf.WriteString("movi")

	type indexEntry struct {
		offset uint32
		length uint32
	}
	var index []aviIndexEntry
	maxFrame := 0
	for _, frame := range frames {
		if len(frame.Data) > maxFrame {
			maxFrame = len(frame.Data)
		}
		chunkPos := buf.Len()
		buf.WriteString("00dc")
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, u32(len(frame.Data)))
		buf.Write(size)
		buf.Write(frame.Data)
		if len(frame.Data)%2 == 1 {
			buf.WriteByte(0)
		}
		index = append(index, aviIndexEntry{
			offset: u32(chunkPos - moviFourCC),
			length: u32(len(frame.Data)),
		})
	}
	moviEnd := buf.Len()
	binary.LittleEndian.PutUint32(moviSize, u32(moviEnd-moviStart-8))
	copy(buf.Bytes()[moviSizePos:], moviSize)

	// idx1: the index that makes the file seekable.
	idx := buildAviIndex(index)
	buf.WriteString("idx1")
	idxSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(idxSize, u32(len(idx)))
	buf.Write(idxSize)
	buf.Write(idx)

	total := buf.Len()
	binary.LittleEndian.PutUint32(riffSize, u32(total-8))
	copy(buf.Bytes()[4:], riffSize)

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing video: %w", err)
	}
	return nil
}

// writeList writes a RIFF LIST chunk.
func writeList(buf *bytes.Buffer, listType string, payload []byte) {
	buf.WriteString("LIST")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, u32(len(listType)+len(payload)))
	buf.Write(size)
	buf.WriteString(listType)
	buf.Write(payload)
}

// buildAviHeader builds the hdrl payload: avih followed by strl(strh, strf).
func buildAviHeader(width, height, frames, fps int) []byte {
	var out bytes.Buffer

	// avih: MainAVIHeader.
	avih := make([]byte, 56)
	binary.LittleEndian.PutUint32(avih[0:], u32(int(time.Second/time.Duration(fps))))
	binary.LittleEndian.PutUint32(avih[4:], 0)
	binary.LittleEndian.PutUint32(avih[8:], 0)
	binary.LittleEndian.PutUint32(avih[12:], 0x10) // AVIF_HASINDEX
	binary.LittleEndian.PutUint32(avih[16:], u32(frames))
	binary.LittleEndian.PutUint32(avih[20:], 0)
	binary.LittleEndian.PutUint32(avih[24:], 1) // one stream
	binary.LittleEndian.PutUint32(avih[28:], 0)
	binary.LittleEndian.PutUint32(avih[32:], u32(width))
	binary.LittleEndian.PutUint32(avih[36:], u32(height))
	writeChunk(&out, "avih", avih)

	// strh: AVIStreamHeader.
	strh := make([]byte, 56)
	copy(strh[0:], "vids")
	copy(strh[4:], "MJPG")
	binary.LittleEndian.PutUint32(strh[16:], 1)           // dwScale
	binary.LittleEndian.PutUint32(strh[20:], u32(fps))    // dwRate
	binary.LittleEndian.PutUint32(strh[28:], u32(frames)) // dwLength
	binary.LittleEndian.PutUint32(strh[44:], 0xFFFFFFFF)  // dwQuality
	binary.LittleEndian.PutUint16(strh[40:], u16(width))
	binary.LittleEndian.PutUint16(strh[42:], u16(height))

	// strf: BITMAPINFOHEADER.
	strf := make([]byte, 40)
	binary.LittleEndian.PutUint32(strf[0:], 40)
	binary.LittleEndian.PutUint32(strf[4:], u32(width))
	binary.LittleEndian.PutUint32(strf[8:], u32(height))
	binary.LittleEndian.PutUint16(strf[12:], 1)  // biPlanes
	binary.LittleEndian.PutUint16(strf[14:], 24) // biBitCount
	copy(strf[16:], "MJPG")
	binary.LittleEndian.PutUint32(strf[20:], u32(width*height*3))

	var strl bytes.Buffer
	writeChunk(&strl, "strh", strh)
	writeChunk(&strl, "strf", strf)
	writeList(&out, "strl", strl.Bytes())
	return out.Bytes()
}

// writeChunk writes a RIFF chunk with its four-byte size.
func writeChunk(buf *bytes.Buffer, id string, payload []byte) {
	buf.WriteString(id)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, u32(len(payload)))
	buf.Write(size)
	buf.Write(payload)
	if len(payload)%2 == 1 {
		buf.WriteByte(0)
	}
}

// aviIndexEntry is one entry of the idx1 index.
type aviIndexEntry struct {
	offset uint32
	length uint32
}

// buildAviIndex builds the idx1 payload.
func buildAviIndex(entries []aviIndexEntry) []byte {
	var out bytes.Buffer
	for _, e := range entries {
		out.WriteString("00dc")
		flags := make([]byte, 4)
		binary.LittleEndian.PutUint32(flags, 0x10) // AVIIF_KEYFRAME
		out.Write(flags)
		off := make([]byte, 4)
		binary.LittleEndian.PutUint32(off, e.offset)
		out.Write(off)
		length := make([]byte, 4)
		binary.LittleEndian.PutUint32(length, e.length)
		out.Write(length)
	}
	return out.Bytes()
}

// jpegSize reads the pixel dimensions out of a JPEG's frame header.
func jpegSize(data []byte) (int, int, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, 0, errors.New("not a JPEG frame")
	}
	for i := 2; i+9 < len(data); {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if i+4 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(data) {
			break
		}
		// SOF0/SOF1/SOF2 carry the frame size.
		if marker == 0xC0 || marker == 0xC1 || marker == 0xC2 {
			height := int(binary.BigEndian.Uint16(data[i+5 : i+7]))
			width := int(binary.BigEndian.Uint16(data[i+7 : i+9]))
			if width <= 0 || height <= 0 {
				return 0, 0, fmt.Errorf("JPEG frame header reports %dx%d", width, height)
			}
			return width, height, nil
		}
		i += 2 + segLen
	}
	return 0, 0, errors.New("JPEG frame header not found")
}
