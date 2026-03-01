package network

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"

	filesync "Stashbird/sync"
)

// ============================================================
// CDC BLOCK-LEVEL DIFFING HANDLERS
// ============================================================

// processBlockMap analisa um BlockMapMessage e retorna os hashes dos blocos necessários.
// NÃO armazena metadados — blocos só são registrados no blockStore quando os
// dados são efetivamente recebidos e gravados (em processBlockData).
func (ss *SyncServer) processBlockMap(blockMap *BlockMapMessage) []string {
	allHashes := make([]string, len(blockMap.Chunks))
	for i, c := range blockMap.Chunks {
		allHashes[i] = c.Hash
	}

	var neededHashes []string
	if ss.blockStore != nil {
		have := ss.blockStore.HasBlocks(allHashes)
		for _, h := range allHashes {
			if !have[h] {
				neededHashes = append(neededHashes, h)
			}
		}
	} else {
		neededHashes = allHashes
	}

	// NÃO chamar PutBlock aqui! Registrar blocos prematuramente fazia
	// com que uma segunda troca de índices (scan a cada 30s) encontrasse
	// todos os hashes no blockStore e declarasse o arquivo sincronizado
	// com "0 blocos necessários" — mesmo sem os dados terem sido gravados.
	// O PutBlock correto acontece em processBlockData após a escrita real.

	saved := len(allHashes) - len(neededHashes)
	log.Printf("[Server] BlockMap %s: %d/%d blocos já existem localmente (economia: %d blocos)",
		blockMap.RelPath, saved, len(allHashes), saved)

	return neededHashes
}

// handleBlockMap é chamado quando recebemos o mapa de blocos CDC de um arquivo.
// Verificamos quais blocos já temos no BlockStore local e pedimos apenas os faltantes.
func (ss *SyncServer) handleBlockMap(conn net.Conn, msg *Message) {
	var blockMap BlockMapMessage
	if err := json.Unmarshal(msg.Payload, &blockMap); err != nil {
		log.Printf("[Server] Erro ao decodificar BlockMap: %v", err)
		return
	}

	log.Printf("[Server] BlockMap recebido para %s (%d blocos, %d bytes)",
		blockMap.RelPath, len(blockMap.Chunks), blockMap.FileSize)

	neededHashes := ss.processBlockMap(&blockMap)

	// Preparar arquivo: versionar, truncar, pré-escrever blocos existentes
	extraNeeded := ss.prepareFileForCDC(&blockMap, neededHashes)
	if len(extraNeeded) > 0 {
		neededHashes = append(neededHashes, extraNeeded...)
	}

	needMsg := BlockNeedMessage{
		FolderID:    blockMap.FolderID,
		RelPath:     blockMap.RelPath,
		NeededHashs: neededHashes,
	}
	payload, _ := json.Marshal(needMsg)
	sendMessage(conn, &Message{
		Type:     MsgTypeBlockNeed,
		FolderID: blockMap.FolderID,
		Payload:  payload,
	})
}

// handleBlockNeed é chamado quando o peer nos diz quais blocos precisa.
// Enviamos apenas os blocos solicitados.
func (ss *SyncServer) handleBlockNeed(conn net.Conn, msg *Message) {
	var needMsg BlockNeedMessage
	if err := json.Unmarshal(msg.Payload, &needMsg); err != nil {
		log.Printf("[Server] Erro ao decodificar BlockNeed: %v", err)
		return
	}

	log.Printf("[Server] BlockNeed recebido para %s (%d blocos solicitados)",
		needMsg.RelPath, len(needMsg.NeededHashs))

	// Se não precisa de nenhum bloco, o arquivo já está sincronizado
	if len(needMsg.NeededHashs) == 0 {
		log.Printf("[Server] Peer já tem todos os blocos de %s", needMsg.RelPath)
		atomic.AddInt64(&ss.filesSynced, 1)
		atomic.AddInt64(&ss.completedFiles, 1)
		ss.checkSyncComplete()
		if ss.onSync != nil {
			ss.onSync(needMsg.FolderID, needMsg.RelPath, "SINCRONIZADO (0 blocos)")
		}
		return
	}

	// Encontrar o arquivo local
	folderPath := ss.findFolderPath(needMsg.FolderID)
	if folderPath == "" {
		return
	}
	fullPath := filepath.Join(folderPath, filepath.FromSlash(needMsg.RelPath))

	// Index do arquivo para obter offset/length de cada chunk
	var chunks []filesync.ChunkInfo
	if ss.blockStore != nil {
		chunks, _ = ss.blockStore.GetFileChunks(needMsg.FolderID, needMsg.RelPath)
	}
	if len(chunks) == 0 {
		// Reindexar se não temos os chunks
		if ss.blockStore != nil {
			chunks, _ = ss.blockStore.IndexFile(needMsg.FolderID, needMsg.RelPath, fullPath)
		}
	}

	// Criar mapa hash -> ChunkInfo para lookup rápido
	chunkMap := make(map[string]filesync.ChunkInfo, len(chunks))
	for _, c := range chunks {
		chunkMap[c.Hash] = c
	}

	// Abrir arquivo UMA VEZ (em vez de abrir/fechar para cada bloco)
	fileHandle, err := os.Open(fullPath)
	if err != nil {
		log.Printf("[Server] Erro ao abrir arquivo para envio CDC: %v", err)
		return
	}
	defer fileHandle.Close()

	sentCount := 0
	totalNeeded := len(needMsg.NeededHashs)

	// Marcar arquivo como enviando no tracker de status
	ss.fileStatus.SetStatus(needMsg.FolderID, needMsg.RelPath, filesync.StatusSyncing, 0)
	ss.fileStatus.SetProgress(needMsg.FolderID, needMsg.RelPath, 0)
	for _, hash := range needMsg.NeededHashs {
		ci, ok := chunkMap[hash]
		if !ok {
			log.Printf("[Server] Bloco não encontrado no índice: %s", hash)
			continue
		}

		// Ler bloco diretamente do handle aberto (sem abrir/fechar por bloco)
		buf := make([]byte, ci.Length)
		if _, err := fileHandle.ReadAt(buf, ci.Offset); err != nil {
			log.Printf("[Server] Erro ao ler bloco %s em offset %d: %v", hash[:8], ci.Offset, err)
			continue
		}

		sentCount++
		isLast := sentCount == totalNeeded

		blockData := BlockDataMessage{
			FolderID: needMsg.FolderID,
			RelPath:  needMsg.RelPath,
			Hash:     hash,
			Offset:   ci.Offset,
			Length:   ci.Length,
			Data:     buf,
			IsLast:   isLast,
		}
		payload, _ := json.Marshal(blockData)
		if err := sendMessage(conn, &Message{
			Type:     MsgTypeBlockData,
			FolderID: needMsg.FolderID,
			Payload:  payload,
		}); err != nil {
			log.Printf("[Server] Erro ao enviar bloco %d/%d de %s: %v", sentCount, totalNeeded, needMsg.RelPath, err)
			return // Conexão perdida, parar envio
		}

		atomic.AddInt64(&ss.bytesSent, int64(ci.Length))

		// Atualizar progresso no tracker de status (cada bloco)
		progress := float64(sentCount) / float64(totalNeeded)
		ss.fileStatus.SetProgress(needMsg.FolderID, needMsg.RelPath, progress)

		// Log de progresso a cada 10% para arquivos grandes
		if totalNeeded > 20 && sentCount%(totalNeeded/10+1) == 0 {
			log.Printf("[Server] Progresso envio CDC %s: %d/%d blocos (%d%%)",
				needMsg.RelPath, sentCount, totalNeeded, sentCount*100/totalNeeded)
		}
	}

	atomic.AddInt64(&ss.filesSynced, 1)
	atomic.AddInt64(&ss.completedFiles, 1)
	ss.checkSyncComplete()
	ss.fileStatus.SetStatus(needMsg.FolderID, needMsg.RelPath, filesync.StatusSynced, 0)

	log.Printf("[Server] Envio CDC completo: %s (%d blocos)", needMsg.RelPath, sentCount)
	if ss.onSync != nil {
		ss.onSync(needMsg.FolderID, needMsg.RelPath, fmt.Sprintf("ENVIADO (CDC: %d blocos)", sentCount))
	}
}

// processBlockData processa dados de um bloco CDC recebido:
// escreve no arquivo, armazena no block store e retorna true se for o último bloco.
func (ss *SyncServer) processBlockData(msg *Message) bool {
	var blockData BlockDataMessage
	if err := json.Unmarshal(msg.Payload, &blockData); err != nil {
		log.Printf("[Server] Erro ao decodificar BlockData: %v", err)
		return false
	}

	// === Verificação de integridade do bloco CDC ===
	if ss.cfg.IntegrityCheckEnabled && blockData.Hash != "" {
		result := filesync.VerifyChunkIntegrity(blockData.Data, blockData.Hash)
		ss.integrityReport.AddResult(result)
		if !result.Valid {
			log.Printf("[Integridade] FALHA no bloco CDC %s de %s: esperado=%s, obtido=%s",
				blockData.Hash[:8], blockData.RelPath, result.ExpectedHash, result.ActualHash)
			if ss.onSync != nil {
				ss.onSync(blockData.FolderID, blockData.RelPath, "ERRO: bloco CDC falhou integridade")
			}
			return false
		}
	}

	folderPath := ss.findFolderPath(blockData.FolderID)
	if folderPath == "" {
		return false
	}

	fullPath := filepath.Join(folderPath, filepath.FromSlash(blockData.RelPath))

	// Garantir diretórios pai
	os.MkdirAll(filepath.Dir(fullPath), 0755)

	// NOTA: versionamento e truncamento são feitos por prepareFileForCDC
	// antes de qualquer bloco ser recebido. Aqui só escrevemos no TEMP.

	tmpPath := tempSyncPath(fullPath)

	// Abrir arquivo TEMP para escrita (já criado por prepareFileForCDC)
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[Server] Erro ao abrir temp para bloco CDC: %v", err)
		return false
	}
	defer f.Close()

	// Escrever no offset correto
	if _, err := f.WriteAt(blockData.Data, blockData.Offset); err != nil {
		log.Printf("[Server] Erro ao escrever bloco CDC em offset %d: %v", blockData.Offset, err)
		return false
	}

	// Armazenar no block store local
	if ss.blockStore != nil {
		ss.blockStore.PutBlock(filesync.BlockMeta{
			Hash:     blockData.Hash,
			FolderID: blockData.FolderID,
			RelPath:  blockData.RelPath,
			Offset:   blockData.Offset,
			Length:   blockData.Length,
		})
	}

	atomic.AddInt64(&ss.bytesRecv, int64(len(blockData.Data)))

	if blockData.IsLast {
		// Fechar o handle antes de renomear
		f.Close()

		// === Renomear temp → destino com retry (Windows file locking) ===
		if err := atomicRenameWithRetry(tmpPath, fullPath); err != nil {
			log.Printf("[Server] ERRO: não foi possível mover temp para destino %s: %v", blockData.RelPath, err)
			return false
		}

		// === Verificação de integridade pós-reassembly do arquivo completo ===
		if ss.cfg.IntegrityCheckEnabled {
			// Recalcular hash do arquivo completo
			fileHash, err := filesync.ComputeFileHash(fullPath)
			if err == nil && fileHash != "" {
				log.Printf("[Integridade] Arquivo CDC reassemblado %s: hash=%s", blockData.RelPath, fileHash[:16])
			}
		}

		atomic.AddInt64(&ss.filesSynced, 1)
		atomic.AddInt64(&ss.completedFiles, 1)
		ss.checkSyncComplete()
		log.Printf("[Server] Arquivo reassemblado via CDC: %s", blockData.RelPath)

		// Marcar como sincronizado
		ss.fileStatus.SetStatus(blockData.FolderID, blockData.RelPath, filesync.StatusSynced, 0)

		// Atualizar índice do watcher imediatamente para evitar
		// que a próxima troca de índices gere falso conflito.
		ss.updateWatcherIndex(blockData.FolderID, blockData.RelPath, fullPath, "")

		if ss.onSync != nil {
			ss.onSync(blockData.FolderID, blockData.RelPath, "RECEBIDO (CDC)")
		}
	}

	return blockData.IsLast
}

// handleBlockData é o handler padrão para blocos CDC recebidos no message loop.
func (ss *SyncServer) handleBlockData(msg *Message) {
	ss.processBlockData(msg)
}

// prepareFileForCDC prepara o arquivo TEMPORÁRIO antes da montagem CDC:
//  1. Versiona o arquivo existente (se aplicável)
//  2. Cria/trunca o arquivo TEMP com o tamanho esperado
//  3. Pré-escreve blocos que já existem localmente nas posições corretas
//
// Os dados são escritos em .syncthing.<nome>.tmp para evitar conflitos
// com processos que bloqueiam o arquivo real (e.g. .exe no Windows).
// O rename temp → destino é feito por processBlockData (IsLast) ou
// pelo chamador quando neededHashes == 0.
//
// Retorna hashes de blocos que deveriam estar localmente mas não puderam
// ser lidos (devem ser adicionados ao BlockNeed).
func (ss *SyncServer) prepareFileForCDC(blockMap *BlockMapMessage, neededHashes []string) []string {
	folderPath := ss.findFolderPath(blockMap.FolderID)
	if folderPath == "" {
		return nil
	}

	fullPath := filepath.Join(folderPath, filepath.FromSlash(blockMap.RelPath))
	tmpPath := tempSyncPath(fullPath)
	os.MkdirAll(filepath.Dir(fullPath), 0755)

	// Versionamento: salvar versão antes de sobrescrever
	if ss.versionMgr.IsEnabled() {
		if _, err := ss.versionMgr.SaveVersion(folderPath, blockMap.RelPath); err != nil {
			log.Printf("[Versão] Aviso: erro ao versionar %s: %v", blockMap.RelPath, err)
		}
	}

	// Criar/truncar arquivo TEMPORÁRIO com tamanho correto.
	// Escrevemos no .tmp para não conflitar com o arquivo real
	// que pode estar em uso por outro processo.
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("[CDC] Erro ao preparar temp %s: %v", blockMap.RelPath, err)
		return nil
	}
	defer f.Close()

	if blockMap.FileSize > 0 {
		if err := f.Truncate(blockMap.FileSize); err != nil {
			log.Printf("[CDC] Erro ao definir tamanho de %s para %d: %v",
				blockMap.RelPath, blockMap.FileSize, err)
		}
	}

	// Identificar blocos que não serão recebidos (já "temos" no blockStore)
	neededSet := make(map[string]bool, len(neededHashes))
	for _, h := range neededHashes {
		neededSet[h] = true
	}

	// Para cada bloco que "já temos", ler do disco e escrever na posição
	// correta do arquivo destino. Se a leitura falhar, adicionar à lista
	// de blocos que precisam ser solicitados ao remetente.
	var extraNeeded []string
	if ss.blockStore != nil {
		preWritten := 0
		for _, chunk := range blockMap.Chunks {
			if neededSet[chunk.Hash] {
				continue // será recebido via BlockData
			}

			meta, err := ss.blockStore.GetBlock(chunk.Hash)
			if err != nil || meta == nil {
				extraNeeded = append(extraNeeded, chunk.Hash)
				continue
			}

			srcFolderPath := ss.findFolderPath(meta.FolderID)
			if srcFolderPath == "" {
				extraNeeded = append(extraNeeded, chunk.Hash)
				continue
			}
			srcFullPath := filepath.Join(srcFolderPath, filepath.FromSlash(meta.RelPath))
			data, err := filesync.ReadChunkData(srcFullPath, meta.Offset, meta.Length)
			if err != nil {
				extraNeeded = append(extraNeeded, chunk.Hash)
				continue
			}

			// Verificar integridade dos dados lidos
			if filesync.ComputeDataHash(data) != chunk.Hash {
				extraNeeded = append(extraNeeded, chunk.Hash)
				continue
			}

			if _, err := f.WriteAt(data, chunk.Offset); err != nil {
				extraNeeded = append(extraNeeded, chunk.Hash)
				continue
			}
			preWritten++
		}

		if preWritten > 0 {
			log.Printf("[CDC] %s: %d blocos existentes pré-escritos no arquivo temp",
				blockMap.RelPath, preWritten)
		}
	}

	return extraNeeded
}
