package network

import (
	"time"

	filesync "Stashbird/sync"
)

// Constantes do protocolo
const (
	MsgTypeIndex          byte = 0x01 // Envio de índice de arquivos
	MsgTypeIndexUpdate    byte = 0x02 // Atualização parcial de índice
	MsgTypeRequest        byte = 0x03 // Solicitar arquivo
	MsgTypeData           byte = 0x04 // Dados de arquivo
	MsgTypeClose          byte = 0x05 // Fechar conexão
	MsgTypePing           byte = 0x06 // Ping
	MsgTypePong           byte = 0x07 // Pong
	MsgTypeRelayReq       byte = 0x08 // Solicitar relay (arquivos grandes)
	MsgTypeRelayAck       byte = 0x09 // Confirmação de relay
	MsgTypeDeleteFile     byte = 0x0A // Excluir arquivo
	MsgTypeChunk          byte = 0x0B // Chunk de arquivo grande
	MsgTypeChunkAck       byte = 0x0C // Confirmação de chunk
	MsgTypeFolderAnnounce byte = 0x0D // Anunciar nova pasta para peers

	// Handshake
	MsgTypeHello byte = 0x0E // Identificação do peer na conexão

	// CDC Block-level Diffing
	MsgTypeBlockMap  byte = 0x10 // Enviar mapa de blocos CDC de um arquivo
	MsgTypeBlockNeed byte = 0x11 // Responder quais blocos são necessários
	MsgTypeBlockData byte = 0x12 // Enviar dados de um bloco específico
	MsgTypeBlockAck  byte = 0x13 // Confirmação de recebimento de bloco
)

// Message é a mensagem base do protocolo
type Message struct {
	Type     byte   `json:"type"`
	FolderID string `json:"folder_id"`
	Payload  []byte `json:"payload"`
}

// IndexMessage contém o índice de arquivos
type IndexMessage struct {
	DeviceID    string                        `json:"device_id"`
	FolderID    string                        `json:"folder_id"`
	FolderLabel string                        `json:"folder_label"`
	Files       map[string]*filesync.FileInfo `json:"files"`
}

// FileRequestMessage solicita um arquivo
type FileRequestMessage struct {
	FolderID string `json:"folder_id"`
	RelPath  string `json:"rel_path"`
	UseRelay bool   `json:"use_relay"` // usar relay para arquivo grande
}

// FileDataMessage contém dados de um arquivo
type FileDataMessage struct {
	FolderID string `json:"folder_id"`
	RelPath  string `json:"rel_path"`
	Data     []byte `json:"data"`
	Size     int64  `json:"size"`
	Hash     string `json:"hash"`
	IsDir    bool   `json:"is_dir"`
}

// ChunkMessage contém um chunk de arquivo grande
type ChunkMessage struct {
	FolderID    string `json:"folder_id"`
	RelPath     string `json:"rel_path"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
	Hash        string `json:"hash"` // hash do chunk
}

// DeleteFileMessage solicita exclusão de arquivo
type DeleteFileMessage struct {
	FolderID string `json:"folder_id"`
	RelPath  string `json:"rel_path"`
}

// RelayRequestMessage solicita ativação de relay
type RelayRequestMessage struct {
	DeviceID string `json:"device_id"`
	Enable   bool   `json:"enable"`
}

// FolderAnnounceMessage anuncia uma nova pasta para peers remotos
type FolderAnnounceMessage struct {
	DeviceID   string `json:"device_id"`
	Label      string `json:"label"`
	Path       string `json:"path"`
	SyncDelete bool   `json:"sync_delete"`
}

// HelloMessage é enviada no início da conexão para identificar o peer.
type HelloMessage struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
}

// BlockMapMessage envia o mapa de blocos CDC de um arquivo para o peer.
// O peer responde com BlockNeedMessage indicando quais blocos precisa.
type BlockMapMessage struct {
	FolderID string               `json:"folder_id"`
	RelPath  string               `json:"rel_path"`
	FileSize int64                `json:"file_size"`
	FileHash string               `json:"file_hash"`
	Chunks   []filesync.ChunkInfo `json:"chunks"`
}

// BlockNeedMessage responde quais blocos o peer precisa receber.
type BlockNeedMessage struct {
	FolderID    string   `json:"folder_id"`
	RelPath     string   `json:"rel_path"`
	NeededHashs []string `json:"needed_hashs"` // hashes dos blocos que faltam
}

// BlockDataMessage envia os dados de um bloco CDC específico.
type BlockDataMessage struct {
	FolderID string `json:"folder_id"`
	RelPath  string `json:"rel_path"`
	Hash     string `json:"hash"`
	Offset   int64  `json:"offset"`
	Length   int    `json:"length"`
	Data     []byte `json:"data"`
	IsLast   bool   `json:"is_last"` // último bloco do arquivo
}

// SyncStats contém estatísticas de sincronização
type SyncStats struct {
	BytesSent      int64
	BytesReceived  int64
	FilesSynced    int64
	LastSyncTime   time.Time
	ConnectedPeers int
	TotalPeers     int
	IsRunning      bool
	PendingFiles   int64
	CompletedFiles int64
	SyncProgress   float64 // 0.0 a 1.0
	IsSyncing      bool
}
