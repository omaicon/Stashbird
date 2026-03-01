package network

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	filesync "Stashbird/sync"

	"github.com/hashicorp/yamux"
)

// requestFileViaStream abre um stream yamux dedicado para solicitar um arquivo,
// receber a resposta (Data, BlockMap+BlockData, ou Chunks) e fechar o stream.
// Cada arquivo é transferido no seu próprio stream, permitindo paralelismo real.
func (ss *SyncServer) requestFileViaStream(session *yamux.Session, folderID, relPath string, useRelay bool) {
	stream, err := session.Open()
	if err != nil {
		log.Printf("[Yamux] Erro ao abrir stream para %s: %v", relPath, err)
		return
	}
	defer stream.Close()

	reqMsg := FileRequestMessage{
		FolderID: folderID,
		RelPath:  relPath,
		UseRelay: useRelay,
	}
	payload, _ := json.Marshal(reqMsg)
	if err := sendMessage(stream, &Message{
		Type:     MsgTypeRequest,
		FolderID: folderID,
		Payload:  payload,
	}); err != nil {
		log.Printf("[Yamux] Erro ao enviar request %s: %v", relPath, err)
		return
	}

	// Ler respostas até completar a transferência do arquivo
	for {
		msg, err := readMessage(stream)
		if err != nil {
			if err != io.EOF {
				log.Printf("[Yamux] Erro ao ler resposta para %s: %v", relPath, err)
			}
			return
		}

		switch msg.Type {
		case MsgTypeData:
			ss.handleDataMessage(msg)
			return // arquivo completo

		case MsgTypeBlockMap:
			// Processar mapa de blocos CDC e enviar BlockNeed
			var blockMap BlockMapMessage
			if err := json.Unmarshal(msg.Payload, &blockMap); err != nil {
				return
			}
			neededHashes := ss.processBlockMap(&blockMap)

			// Preparar arquivo: versionar, truncar, pré-escrever blocos existentes
			// nas posições corretas.  Blocos que falharam na leitura local são
			// adicionados à lista de necessários.
			extraNeeded := ss.prepareFileForCDC(&blockMap, neededHashes)
			if len(extraNeeded) > 0 {
				neededHashes = append(neededHashes, extraNeeded...)
			}

			// Enviar BlockNeed (uma única vez)
			needMsg := BlockNeedMessage{
				FolderID:    blockMap.FolderID,
				RelPath:     blockMap.RelPath,
				NeededHashs: neededHashes,
			}
			p, _ := json.Marshal(needMsg)
			if err := sendMessage(stream, &Message{
				Type:     MsgTypeBlockNeed,
				FolderID: blockMap.FolderID,
				Payload:  p,
			}); err != nil {
				log.Printf("[Yamux] Erro ao enviar BlockNeed para %s: %v", relPath, err)
				return
			}
			log.Printf("[Yamux] BlockNeed enviado para %s (%d blocos solicitados)", relPath, len(neededHashes))

			if len(neededHashes) == 0 {
				// Arquivo montado inteiramente com blocos locais no temp.
				// Renomear temp → destino.
				folderPath := ss.findFolderPath(blockMap.FolderID)
				if folderPath != "" {
					fp := filepath.Join(folderPath, filepath.FromSlash(blockMap.RelPath))
					tmpFp := tempSyncPath(fp)
					if err := atomicRenameWithRetry(tmpFp, fp); err != nil {
						log.Printf("[Server] ERRO: rename temp→destino %s: %v", blockMap.RelPath, err)
					}
					ss.updateWatcherIndex(blockMap.FolderID, blockMap.RelPath, fp, "")
				}
				atomic.AddInt64(&ss.filesSynced, 1)
				atomic.AddInt64(&ss.completedFiles, 1)
				ss.checkSyncComplete()
				ss.fileStatus.SetStatus(blockMap.FolderID, blockMap.RelPath, filesync.StatusSynced, blockMap.FileSize)
				if ss.onSync != nil {
					ss.onSync(folderID, relPath, "SINCRONIZADO (0 blocos)")
				}
				return
			}
			// Continuar lendo BlockData...

		case MsgTypeBlockData:
			if ss.processBlockData(msg) {
				return // arquivo completo via CDC
			}

		case MsgTypeChunk:
			ss.handleChunkMessage(msg)
			var peek struct {
				ChunkIndex  int `json:"chunk_index"`
				TotalChunks int `json:"total_chunks"`
			}
			json.Unmarshal(msg.Payload, &peek)
			if peek.ChunkIndex >= peek.TotalChunks-1 {
				return
			}

		default:
			ss.handleMessage(stream, msg)
		}
	}
}

// sendFileChunked envia arquivo em chunks (para arquivos grandes via relay)
func (ss *SyncServer) sendFileChunked(conn net.Conn, folderID, relPath, fullPath string) {
	f, err := os.Open(fullPath)
	if err != nil {
		log.Printf("[Server] Erro ao abrir arquivo para chunking: %v", err)
		return
	}
	defer f.Close()

	info, _ := f.Stat()
	chunkSize := int64(ss.cfg.RelayChunkSizeMB) * 1024 * 1024
	totalChunks := int(info.Size()/chunkSize) + 1

	log.Printf("[Server] Enviando %s em %d chunks (%d MB)", relPath, totalChunks, info.Size()/(1024*1024))

	buf := make([]byte, chunkSize)
	for i := 0; i < totalChunks; i++ {
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			log.Printf("[Server] Erro ao ler chunk %d: %v", i, err)
			return
		}
		if n == 0 {
			break
		}

		chunkMsg := ChunkMessage{
			FolderID:    folderID,
			RelPath:     relPath,
			ChunkIndex:  i,
			TotalChunks: totalChunks,
			Data:        buf[:n],
			Hash:        filesync.ComputeDataHash(buf[:n]),
		}
		payload, _ := json.Marshal(chunkMsg)
		if err := sendMessage(conn, &Message{
			Type:     MsgTypeChunk,
			FolderID: folderID,
			Payload:  payload,
		}); err != nil {
			log.Printf("[Server] Erro ao enviar chunk %d/%d de %s: %v", i+1, totalChunks, relPath, err)
			return
		}

		// Log de progresso a cada 10%
		if totalChunks > 10 && (i+1)%(totalChunks/10+1) == 0 {
			log.Printf("[Server] Progresso envio chunks %s: %d/%d (%d%%)",
				relPath, i+1, totalChunks, (i+1)*100/totalChunks)
		}

		// Pequeno delay entre chunks para não sobrecarregar
		time.Sleep(10 * time.Millisecond)
	}

	if ss.onSync != nil {
		ss.onSync(folderID, relPath, "ENVIADO (relay)")
	}
}

// handleChunkMessage processa chunk de arquivo grande
func (ss *SyncServer) handleChunkMessage(msg *Message) {
	var chunkMsg ChunkMessage
	if err := json.Unmarshal(msg.Payload, &chunkMsg); err != nil {
		log.Printf("[Server] Erro ao decodificar chunk: %v", err)
		return
	}

	// === Verificação de integridade do chunk ===
	if ss.cfg.IntegrityCheckEnabled && chunkMsg.Hash != "" {
		result := filesync.VerifyChunkIntegrity(chunkMsg.Data, chunkMsg.Hash)
		ss.integrityReport.AddResult(result)
		if !result.Valid {
			log.Printf("[Integridade] FALHA no chunk %d/%d de %s: esperado=%s, obtido=%s",
				chunkMsg.ChunkIndex+1, chunkMsg.TotalChunks, chunkMsg.RelPath,
				result.ExpectedHash, result.ActualHash)
			if ss.onSync != nil {
				ss.onSync(chunkMsg.FolderID, chunkMsg.RelPath,
					fmt.Sprintf("ERRO: chunk %d/%d falhou integridade", chunkMsg.ChunkIndex+1, chunkMsg.TotalChunks))
			}
			return
		}
	}

	folderPath := ss.findFolderPath(chunkMsg.FolderID)
	if folderPath == "" {
		return
	}

	fullPath := filepath.Join(folderPath, filepath.FromSlash(chunkMsg.RelPath))

	// Criar diretórios pai
	os.MkdirAll(filepath.Dir(fullPath), 0755)

	// === Versionamento: salvar versão antes do primeiro chunk ===
	if chunkMsg.ChunkIndex == 0 && ss.versionMgr.IsEnabled() {
		if _, err := ss.versionMgr.SaveVersion(folderPath, chunkMsg.RelPath); err != nil {
			log.Printf("[Versão] Aviso: erro ao versionar %s: %v", chunkMsg.RelPath, err)
		}
	}

	// Abrir/criar arquivo TEMP para escrita (evitar lock no arquivo real)
	tmpPath := tempSyncPath(fullPath)
	var flag int
	if chunkMsg.ChunkIndex == 0 {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	} else {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	f, err := os.OpenFile(tmpPath, flag, 0644)
	if err != nil {
		log.Printf("[Server] Erro ao abrir temp para chunk: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(chunkMsg.Data); err != nil {
		log.Printf("[Server] Erro ao escrever chunk: %v", err)
		return
	}

	log.Printf("[Server] Chunk %d/%d recebido para %s",
		chunkMsg.ChunkIndex+1, chunkMsg.TotalChunks, chunkMsg.RelPath)

	if chunkMsg.ChunkIndex == chunkMsg.TotalChunks-1 {
		// Fechar antes de renomear
		f.Close()

		// Renomear temp → destino
		if err := atomicRenameWithRetry(tmpPath, fullPath); err != nil {
			log.Printf("[Server] ERRO: rename temp→destino chunk %s: %v", chunkMsg.RelPath, err)
			return
		}

		atomic.AddInt64(&ss.completedFiles, 1)
		ss.checkSyncComplete()

		// Atualizar índice do watcher imediatamente
		ss.updateWatcherIndex(chunkMsg.FolderID, chunkMsg.RelPath, fullPath, "")

		log.Printf("[Server] Arquivo reassemblado via chunks: %s", chunkMsg.RelPath)
		if ss.onSync != nil {
			ss.onSync(chunkMsg.FolderID, chunkMsg.RelPath, "RECEBIDO (relay)")
		}
		// Marcar como sincronizado
		ss.fileStatus.SetStatus(chunkMsg.FolderID, chunkMsg.RelPath, filesync.StatusSynced, 0)
	}
}
