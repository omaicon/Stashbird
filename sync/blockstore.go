package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

// BlockStore is a local chunk index backed by BoltDB.
// It answers "do I have block X?" without re-reading files from disk.
//
// Buckets:
//   - "blocks"      : hash -> BlockMeta (JSON)
//   - "file_chunks" : folderID/relPath -> []ChunkInfo (JSON)

var (
	bucketBlocks     = []byte("blocks")
	bucketFileChunks = []byte("file_chunks")
)

// BlockMeta armazena metadados de um bloco no store.
type BlockMeta struct {
	Hash     string `json:"hash"`
	FolderID string `json:"folder_id"`
	RelPath  string `json:"rel_path"`
	Offset   int64  `json:"offset"`
	Length   int    `json:"length"`
}

// BlockStore é o índice local de blocos baseado em BoltDB.
type BlockStore struct {
	db *bolt.DB
}

// NewBlockStore abre (ou cria) o banco de blocos no diretório dado.
func NewBlockStore(dataDir string) (*BlockStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório do blockstore: %w", err)
	}

	dbPath := filepath.Join(dataDir, "blocks.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{
		NoSync:         true, // cache is reconstructible; fsync not required
		NoFreelistSync: true,
	})
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir blockstore: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketBlocks); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketFileChunks); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &BlockStore{db: db}, nil
}

// Close fecha o banco de dados.
func (bs *BlockStore) Close() error {
	if bs.db != nil {
		return bs.db.Close()
	}
	return nil
}

// HasBlock verifica se um bloco com o hash dado existe localmente.
func (bs *BlockStore) HasBlock(hash string) bool {
	var found bool
	bs.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketBlocks)
		found = b.Get([]byte(hash)) != nil
		return nil
	})
	return found
}

// HasBlocks checks multiple hashes at once. Returns a map of hash -> exists.
func (bs *BlockStore) HasBlocks(hashes []string) map[string]bool {
	result := make(map[string]bool, len(hashes))
	bs.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketBlocks)
		for _, h := range hashes {
			result[h] = b.Get([]byte(h)) != nil
		}
		return nil
	})
	return result
}

// PutBlock stores block metadata. Uses db.Batch to coalesce concurrent writes.
func (bs *BlockStore) PutBlock(meta BlockMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return bs.db.Batch(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlocks).Put([]byte(meta.Hash), data)
	})
}

// PutBlocks stores multiple blocks in a single transaction.
func (bs *BlockStore) PutBlocks(metas []BlockMeta) error {
	return bs.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketBlocks)
		for _, meta := range metas {
			data, err := json.Marshal(meta)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(meta.Hash), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetBlock retorna os metadados de um bloco pelo hash.
func (bs *BlockStore) GetBlock(hash string) (*BlockMeta, error) {
	var meta BlockMeta
	err := bs.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketBlocks).Get([]byte(hash))
		if data == nil {
			return fmt.Errorf("bloco não encontrado: %s", hash)
		}
		return json.Unmarshal(data, &meta)
	})
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

// StoreFileChunks armazena chunks pré-computados no BlockStore.
// Usa db.Batch() para melhor concorrência quando múltiplos arquivos
// são indexados simultaneamente (evita serialização de db.Update).
func (bs *BlockStore) StoreFileChunks(folderID, relPath string, chunks []ChunkInfo) error {
	metas := make([]BlockMeta, len(chunks))
	for i, c := range chunks {
		metas[i] = BlockMeta{
			Hash:     c.Hash,
			FolderID: folderID,
			RelPath:  relPath,
			Offset:   c.Offset,
			Length:   c.Length,
		}
	}

	fileKey := []byte(folderID + "/" + relPath)
	chunksJSON, _ := json.Marshal(chunks)

	return bs.db.Batch(func(tx *bolt.Tx) error {
		bb := tx.Bucket(bucketBlocks)
		for _, meta := range metas {
			data, _ := json.Marshal(meta)
			if err := bb.Put([]byte(meta.Hash), data); err != nil {
				return err
			}
		}
		return tx.Bucket(bucketFileChunks).Put(fileKey, chunksJSON)
	})
}

// IndexFile aplica CDC a um arquivo e armazena todos os chunks no store.
// Retorna a lista de ChunkInfo produzida.
func (bs *BlockStore) IndexFile(folderID, relPath, fullPath string) ([]ChunkInfo, error) {
	chunks, err := ChunkFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("erro ao chunkar arquivo %s: %w", relPath, err)
	}

	if err := bs.StoreFileChunks(folderID, relPath, chunks); err != nil {
		return nil, err
	}

	return chunks, nil
}

// GetFileChunks retorna os chunks armazenados para um arquivo.
func (bs *BlockStore) GetFileChunks(folderID, relPath string) ([]ChunkInfo, error) {
	fileKey := []byte(folderID + "/" + relPath)
	var chunks []ChunkInfo

	err := bs.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketFileChunks).Get(fileKey)
		if data == nil {
			return fmt.Errorf("arquivo não indexado: %s/%s", folderID, relPath)
		}
		return json.Unmarshal(data, &chunks)
	})
	if err != nil {
		return nil, err
	}
	return chunks, nil
}

// RemoveFile remove o índice de um arquivo e seus blocos exclusivos.
func (bs *BlockStore) RemoveFile(folderID, relPath string) error {
	fileKey := []byte(folderID + "/" + relPath)
	return bs.db.Update(func(tx *bolt.Tx) error {
		fc := tx.Bucket(bucketFileChunks)
		data := fc.Get(fileKey)
		if data != nil {
			var chunks []ChunkInfo
			if err := json.Unmarshal(data, &chunks); err == nil {
				bb := tx.Bucket(bucketBlocks)
				for _, c := range chunks {
					// Block may be shared, but it will be re-indexed on next scan.
					bb.Delete([]byte(c.Hash))
				}
			}
		}
		return fc.Delete(fileKey)
	})
}

// Stats retorna estatísticas do block store.
type BlockStoreStats struct {
	TotalBlocks int `json:"total_blocks"`
	TotalFiles  int `json:"total_files"`
}

func (bs *BlockStore) Stats() BlockStoreStats {
	var stats BlockStoreStats
	bs.db.View(func(tx *bolt.Tx) error {
		stats.TotalBlocks = tx.Bucket(bucketBlocks).Stats().KeyN
		stats.TotalFiles = tx.Bucket(bucketFileChunks).Stats().KeyN
		return nil
	})
	return stats
}

// DefaultBlockStorePath retorna o caminho padrão para o block store (cross-platform).
func DefaultBlockStorePath() string {
	return filepath.Join(configAppDataDir(), "blockstore")
}

// configAppDataDir mirrors config.AppDataDir() without importing that package (avoids import cycle).
func configAppDataDir() string {
	switch {
	case os.Getenv("APPDATA") != "":
		return filepath.Join(os.Getenv("APPDATA"), "SyncthingGO")
	default:
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "SyncthingGO")
		}
		return filepath.Join(".", "SyncthingGO")
	}
}
