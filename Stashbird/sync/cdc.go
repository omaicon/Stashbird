package sync

import (
	"encoding/hex"
	"io"
	"os"

	"github.com/zeebo/blake3"
)

// Content-Defined Chunking using BuzzHash (rolling hash).
// Chunk boundaries are content-driven, so inserting bytes at the start of a file
// only invalidates the first chunk — minimizing data transfer on sync.

const (
	cdcMinSize    = 32 * 1024       // 32 KB min
	cdcMaxSize    = 2 * 1024 * 1024 // 2 MB max
	cdcTargetSize = 256 * 1024      // 256 KB target
	cdcMaskBits   = 18              // 2^18 ≈ 256 KB average
	cdcMask       = (1 << cdcMaskBits) - 1
)

// buzzTable maps each byte value to a pseudo-random uint64 for the rolling hash.
// Seeded deterministically with BLAKE3 for reproducibility.
var buzzTable [256]uint64

func init() {
	seed := blake3.Sum256([]byte("SyncthingGO-CDC-BuzzHash-Table"))
	h := blake3.New()
	h.Write(seed[:])

	for i := 0; i < 256; i++ {
		var buf [8]byte
		h.Write([]byte{byte(i)})
		sum := h.Sum(nil)
		copy(buf[:], sum[:8])
		buzzTable[i] = uint64(buf[0]) | uint64(buf[1])<<8 |
			uint64(buf[2])<<16 | uint64(buf[3])<<24 |
			uint64(buf[4])<<32 | uint64(buf[5])<<40 |
			uint64(buf[6])<<48 | uint64(buf[7])<<56
	}
}

// ChunkInfo descreve um chunk produzido pelo CDC.
type ChunkInfo struct {
	Offset int64  `json:"offset"`
	Length int    `json:"length"`
	Hash   string `json:"hash"` // BLAKE3 hex do conteúdo
}

// ChunkFile aplica Content-Defined Chunking a um arquivo.
// Retorna a lista de chunks com offset, tamanho e hash BLAKE3.
func ChunkFile(path string) ([]ChunkInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return chunkReader(f)
}

// ChunkData aplica CDC a um slice de bytes.
func ChunkData(data []byte) []ChunkInfo {
	var chunks []ChunkInfo
	offset := 0

	for offset < len(data) {
		remaining := len(data) - offset
		chunkLen := findBoundary(data[offset:], remaining)

		chunk := data[offset : offset+chunkLen]
		h := blake3.Sum256(chunk)

		chunks = append(chunks, ChunkInfo{
			Offset: int64(offset),
			Length: chunkLen,
			Hash:   hex.EncodeToString(h[:]),
		})
		offset += chunkLen
	}

	return chunks
}

// chunkReader aplica CDC lendo de um io.Reader.
func chunkReader(r io.Reader) ([]ChunkInfo, error) {
	var chunks []ChunkInfo
	// Buffer grande o suficiente para o chunk máximo + lookahead
	buf := make([]byte, cdcMaxSize)
	var offset int64
	carry := 0 // leftover bytes from the previous read

	for {
		n, err := r.Read(buf[carry:])
		total := carry + n
		if total == 0 {
			break
		}

		pos := 0
		for pos < total {
			remaining := total - pos
			chunkLen := findBoundary(buf[pos:pos+remaining], remaining)

			// If the chunk reaches the end of the buffer and there is more data in the file,
			// carry the remaining bytes to the next read.
			// Guard: only carry when remaining bytes fit well within the buffer.
			// When remaining == len(buf), findBoundary already cut at the max and
			// carrying again would loop forever on incompressible data (.rar/.zip/.7z).
			if pos+chunkLen == total && err == nil && (total-pos) < len(buf) {
				copy(buf, buf[pos:total])
				carry = total - pos
				break
			}

			chunk := buf[pos : pos+chunkLen]
			h := blake3.Sum256(chunk)

			chunks = append(chunks, ChunkInfo{
				Offset: offset,
				Length: chunkLen,
				Hash:   hex.EncodeToString(h[:]),
			})
			offset += int64(chunkLen)
			pos += chunkLen
			carry = 0
		}

		if err == io.EOF {
			if carry > 0 {
				chunk := buf[:carry]
				h := blake3.Sum256(chunk)
				chunks = append(chunks, ChunkInfo{
					Offset: offset,
					Length: carry,
					Hash:   hex.EncodeToString(h[:]),
				})
			}
			break
		}
		if err != nil {
			return chunks, err
		}
	}

	return chunks, nil
}

// findBoundary locates the next CDC cut point using BuzzHash, respecting min/max sizes.
func findBoundary(data []byte, available int) int {
	if available <= cdcMinSize {
		return available
	}

	maxLen := cdcMaxSize
	if available < maxLen {
		maxLen = available
	}

	var hash uint64

	// Warm up the rolling hash up to cdcMinSize
	for i := 0; i < cdcMinSize && i < len(data); i++ {
		hash = (hash << 1) | (hash >> 63) // rotate left 1
		hash ^= buzzTable[data[i]]
	}

	// Scan for boundary starting at cdcMinSize
	for i := cdcMinSize; i < maxLen; i++ {
		hash = (hash << 1) | (hash >> 63)
		hash ^= buzzTable[data[i]]

		if hash&cdcMask == 0 {
			return i + 1
		}
	}

	return maxLen
}

// FileChunkHashes retorna apenas os hashes dos chunks de um arquivo,
// sem carregar o conteúdo na memória. Útil para comparação rápida.
func FileChunkHashes(path string) ([]string, error) {
	chunks, err := ChunkFile(path)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(chunks))
	for i, c := range chunks {
		hashes[i] = c.Hash
	}
	return hashes, nil
}

// ReadChunkData lê o conteúdo de um chunk específico de um arquivo.
func ReadChunkData(path string, offset int64, length int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, length)
	_, err = f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}
