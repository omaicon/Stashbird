package network

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"Stashbird/config"
	filesync "Stashbird/sync"
)

// handleStream processa mensagens de um stream yamux individual.
func (ss *SyncServer) handleStream(conn net.Conn) {
	defer conn.Close()
	for {
		msg, err := readMessage(conn)
		if err != nil {
			if err != io.EOF {
				log.Printf("[Server] Erro ao ler stream: %v", err)
			}
			return
		}
		ss.handleMessage(conn, msg)
	}
}

// handleMessage processa uma mensagem recebida
func (ss *SyncServer) handleMessage(conn net.Conn, msg *Message) {
	switch msg.Type {
	case MsgTypeIndex, MsgTypeIndexUpdate:
		ss.handleIndexMessage(conn, msg)
	case MsgTypeRequest:
		ss.handleRequestMessage(conn, msg)
	case MsgTypeData:
		ss.handleDataMessage(msg)
	case MsgTypeChunk:
		ss.handleChunkMessage(msg)
	case MsgTypeDeleteFile:
		ss.handleDeleteMessage(msg)
	case MsgTypePing:
		sendMessage(conn, &Message{Type: MsgTypePong})
	case MsgTypeRelayReq:
		ss.handleRelayRequest(conn, msg)
	case MsgTypeFolderAnnounce:
		ss.handleFolderAnnounce(conn, msg)
	case MsgTypeBlockMap:
		ss.handleBlockMap(conn, msg)
	case MsgTypeBlockNeed:
		ss.handleBlockNeed(conn, msg)
	case MsgTypeBlockData:
		ss.handleBlockData(msg)
	case MsgTypeHello:
		// Hello recebido fora do handshake — ignorar
		log.Printf("[Server] Hello recebido em stream regular (ignorando)")
	case MsgTypePong:
		// Pong — resposta ao ping, apenas ignorar
	}
}

// handleIndexMessage processa um índice recebido
func (ss *SyncServer) handleIndexMessage(conn net.Conn, msg *Message) {
	var indexMsg IndexMessage
	if err := json.Unmarshal(msg.Payload, &indexMsg); err != nil {
		log.Printf("[Server] Erro ao decodificar índice: %v", err)
		return
	}

	log.Printf("[Server] Índice recebido de %s para pasta %s (%d arquivos)",
		indexMsg.DeviceID, indexMsg.FolderID, len(indexMsg.Files))

	// Comparar com nosso índice e solicitar arquivos que faltam
	ss.mu.RLock()
	watcher := ss.findWatcherByRef(indexMsg.FolderID)
	if watcher == nil && indexMsg.FolderLabel != "" {
		watcher = ss.findWatcherByRef(indexMsg.FolderLabel)
	}
	ss.mu.RUnlock()

	if watcher == nil {
		log.Printf("[Server] Pasta não encontrada localmente: %s (label: %s) — criando automaticamente...",
			indexMsg.FolderID, indexMsg.FolderLabel)

		// Auto-criar a pasta localmente (como faz handleFolderAnnounce)
		label := indexMsg.FolderID
		if indexMsg.FolderLabel != "" {
			label = indexMsg.FolderLabel
		}

		// Usar caminho adaptado ao usuário local
		localPath := adaptPathForLocalUser(label)
		// Se o label parece ser um caminho absoluto, usar ele; senão, escolher ~/TailDrive/<label>
		if !filepath.IsAbs(localPath) {
			home := config.UserHome()
			localPath = filepath.Join(home, "TailDrive", label)
		}

		if err := os.MkdirAll(localPath, 0755); err != nil {
			log.Printf("[Server] Erro ao criar pasta '%s': %v", localPath, err)
			return
		}

		folder := config.FolderConfig{
			ID:      label,
			Label:   label,
			Path:    localPath,
			Enabled: true,
		}
		ss.cfg.AddFolder(folder)
		ss.cfg.Save()
		log.Printf("[Server] Pasta '%s' criada automaticamente em: %s", label, localPath)

		// Notificar GUI
		ss.mu.RLock()
		cb := ss.onFolderAdd
		ss.mu.RUnlock()
		if cb != nil {
			cb(folder)
		}

		// Iniciar watcher e tentar novamente
		ss.RefreshWatchers()
		// Pequena espera para o watcher inicializar
		time.Sleep(1 * time.Second)

		ss.mu.RLock()
		watcher = ss.findWatcherByRef(label)
		ss.mu.RUnlock()
		if watcher == nil {
			log.Printf("[Server] Não foi possível criar watcher para pasta '%s'", label)
			return
		}
	}

	localIndex := watcher.GetIndex()
	session := getSession(conn)

	// Se temos sessão yamux, enviar requests em paralelo via streams dedicados
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentTransfers)

	for relPath, remoteFile := range indexMsg.Files {
		localFile, localExists := localIndex[relPath]

		needSync := false
		if !localExists {
			needSync = true
		} else if !remoteFile.IsDir && remoteFile.Hash == localFile.Hash {
			// Hashes iguais — arquivo já sincronizado
			ss.fileStatus.SetStatus(indexMsg.FolderID, relPath, filesync.StatusSynced, localFile.Size)
			continue
		} else if !remoteFile.IsDir && remoteFile.Hash != localFile.Hash {
			// O índice local pode estar defasado (arquivo recebido mas scan ainda
			// não rodou). Verificar o hash real no disco antes de declarar conflito.
			folderPath := ss.findFolderPath(indexMsg.FolderID)
			if folderPath != "" {
				diskPath := filepath.Join(folderPath, filepath.FromSlash(relPath))
				if diskHash, err := filesync.ComputeFileHash(diskPath); err == nil && diskHash == remoteFile.Hash {
					// Arquivo no disco já é idêntico ao remoto — índice defasado, sem conflito
					// Atualizar índice do watcher para evitar repetição
					if watcher != nil {
						info, _ := os.Stat(diskPath)
						if info != nil {
							watcher.UpdateFile(&filesync.FileInfo{
								RelPath: relPath,
								Size:    info.Size(),
								ModTime: info.ModTime(),
								Hash:    diskHash,
							})
						}
					}
					continue
				}
			}

			// Verificar se há conflito (ambos modificaram o arquivo)
			if ss.conflictMgr.DetectConflict(localFile, remoteFile) {
				// Conflito detectado! Resolver de acordo com a estratégia
				folderPath := ss.findFolderPath(indexMsg.FolderID)
				if folderPath != "" {
					shouldAccept, conflict, err := ss.conflictMgr.Resolve(
						folderPath, localFile, remoteFile,
						ss.cfg.DeviceID, indexMsg.DeviceID,
					)
					if err != nil {
						log.Printf("[Conflito] Erro ao resolver conflito de %s: %v", relPath, err)
					}
					if conflict != nil {
						conflict.FolderID = indexMsg.FolderID
						action := fmt.Sprintf("CONFLITO resolvido (%s)", ss.conflictMgr.GetStrategy())
						if ss.onSync != nil {
							ss.onSync(indexMsg.FolderID, relPath, action)
						}
					}
					needSync = shouldAccept
				} else {
					// Fallback: aceitar remoto
					needSync = true
				}
			} else if remoteFile.ModTime.After(localFile.ModTime) {
				// Sem conflito, arquivo remoto é mais recente
				needSync = true
			}
		}

		if needSync && !remoteFile.IsDeleted {
			// Marcar arquivo como pendente
			ss.fileStatus.SetStatus(indexMsg.FolderID, relPath, filesync.StatusPending, remoteFile.Size)

			// Incrementar contador de arquivos pendentes
			atomic.AddInt64(&ss.pendingFiles, 1)
			atomic.StoreInt32(&ss.isSyncing, 1)

			// Verificar se precisa usar relay (arquivo grande)
			useRelay := false
			if remoteFile.Size > int64(ss.cfg.LargeFileThresholdMB)*1024*1024 {
				useRelay = ss.cfg.PeerRelayEnabled && ss.tailscale.IsPeerRelayEnabled()
			}

			if session != nil {
				// Transferência paralela via stream yamux dedicado
				wg.Add(1)
				sem <- struct{}{}
				go func(folderID, rp string, relay bool, sz int64) {
					defer wg.Done()
					defer func() { <-sem }()
					ss.fileStatus.SetStatus(folderID, rp, filesync.StatusSyncing, sz)
					ss.requestFileViaStream(session, folderID, rp, relay)
				}(indexMsg.FolderID, relPath, useRelay, remoteFile.Size)
			} else {
				// Fallback: enviar pela conexão atual (sem yamux)
				reqMsg := FileRequestMessage{
					FolderID: indexMsg.FolderID,
					RelPath:  relPath,
					UseRelay: useRelay,
				}
				payload, _ := json.Marshal(reqMsg)
				sendMessage(conn, &Message{
					Type:     MsgTypeRequest,
					FolderID: indexMsg.FolderID,
					Payload:  payload,
				})
			}
		}
	}

	if session != nil {
		wg.Wait()
	}
}

// handleRequestMessage processa uma solicitação de arquivo.
// Para arquivos não-pequenos, usa CDC Block Map para transferir apenas blocos alterados.
func (ss *SyncServer) handleRequestMessage(conn net.Conn, msg *Message) {
	var reqMsg FileRequestMessage
	if err := json.Unmarshal(msg.Payload, &reqMsg); err != nil {
		log.Printf("[Server] Erro ao decodificar request: %v", err)
		return
	}

	// Encontrar o caminho completo do arquivo
	folderPath := ss.findFolderPath(reqMsg.FolderID)
	if folderPath == "" {
		log.Printf("[Server] Pasta não encontrada: %s", reqMsg.FolderID)
		return
	}

	fullPath := filepath.Join(folderPath, filepath.FromSlash(reqMsg.RelPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		log.Printf("[Server] Arquivo não encontrado: %s", fullPath)
		return
	}

	if info.IsDir() {
		// Enviar diretório
		dataMsg := FileDataMessage{
			FolderID: reqMsg.FolderID,
			RelPath:  reqMsg.RelPath,
			IsDir:    true,
		}
		payload, _ := json.Marshal(dataMsg)
		sendMessage(conn, &Message{
			Type:     MsgTypeData,
			FolderID: reqMsg.FolderID,
			Payload:  payload,
		})
		return
	}

	// Verificar se deve usar chunks (relay) para arquivo grande
	if reqMsg.UseRelay && info.Size() > int64(ss.cfg.LargeFileThresholdMB)*1024*1024 {
		ss.sendFileChunked(conn, reqMsg.FolderID, reqMsg.RelPath, fullPath)
		return
	}

	// ========================================================
	// CDC Block-level Diffing: enviar mapa de blocos em vez do
	// arquivo inteiro, permitindo que o peer peça só o que falta.
	// Para arquivos pequenos (< 64KB) envia direto sem CDC.
	//
	// O ciclo completo é gerenciado aqui de forma self-contained:
	//   ChunkFile (memória) → BlockMap → lê BlockNeed → envia blocos
	// O BoltDB é atualizado assincronamente para dedup futura,
	// eliminando contenção de escrita entre transfers paralelos.
	// ========================================================
	if ss.blockStore != nil && info.Size() >= 64*1024 {
		log.Printf("[Server] Indexando CDC para %s (%d MB)...", reqMsg.RelPath, info.Size()/(1024*1024))

		// Computar chunks em memória (sem lock de BoltDB)
		chunks, err := filesync.ChunkFile(fullPath)
		if err == nil && len(chunks) > 0 {
			// Armazenar no BlockStore de forma assíncrona (cache para dedup futura)
			go ss.blockStore.StoreFileChunks(reqMsg.FolderID, reqMsg.RelPath, chunks)

			blockMap := BlockMapMessage{
				FolderID: reqMsg.FolderID,
				RelPath:  reqMsg.RelPath,
				FileSize: info.Size(),
				Chunks:   chunks,
			}
			payload, _ := json.Marshal(blockMap)
			if err := sendMessage(conn, &Message{
				Type:     MsgTypeBlockMap,
				FolderID: reqMsg.FolderID,
				Payload:  payload,
			}); err != nil {
				log.Printf("[Server] Erro ao enviar BlockMap de %s: %v", reqMsg.RelPath, err)
				return
			}
			log.Printf("[Server] BlockMap enviado para %s (%d blocos CDC, %d KB payload)",
				reqMsg.RelPath, len(chunks), len(payload)/1024)

			// ── Aguardar BlockNeed do peer no mesmo stream ──
			needReply, err := readMessage(conn)
			if err != nil {
				log.Printf("[Server] Erro ao ler BlockNeed de %s: %v", reqMsg.RelPath, err)
				return
			}
			if needReply.Type != MsgTypeBlockNeed {
				log.Printf("[Server] Esperava BlockNeed para %s, recebeu tipo=%d", reqMsg.RelPath, needReply.Type)
				return
			}

			var needMsg BlockNeedMessage
			if err := json.Unmarshal(needReply.Payload, &needMsg); err != nil {
				log.Printf("[Server] Erro ao decodificar BlockNeed de %s: %v", reqMsg.RelPath, err)
				return
			}

			log.Printf("[Server] BlockNeed recebido para %s (%d blocos solicitados)",
				needMsg.RelPath, len(needMsg.NeededHashs))

			if len(needMsg.NeededHashs) == 0 {
				log.Printf("[Server] Peer já tem todos os blocos de %s", needMsg.RelPath)
				atomic.AddInt64(&ss.filesSynced, 1)
				atomic.AddInt64(&ss.completedFiles, 1)
				ss.checkSyncComplete()
				if ss.onSync != nil {
					ss.onSync(reqMsg.FolderID, reqMsg.RelPath, "SINCRONIZADO (0 blocos)")
				}
				return
			}

			// ── Enviar blocos usando chunks em memória (sem DB lookup) ──
			chunkMap := make(map[string]filesync.ChunkInfo, len(chunks))
			for _, c := range chunks {
				chunkMap[c.Hash] = c
			}

			fileHandle, err := os.Open(fullPath)
			if err != nil {
				log.Printf("[Server] Erro ao abrir arquivo para envio CDC: %v", err)
				return
			}
			defer fileHandle.Close()

			sentCount := 0
			totalNeeded := len(needMsg.NeededHashs)
			ss.fileStatus.SetStatus(reqMsg.FolderID, reqMsg.RelPath, filesync.StatusSyncing, 0)
			ss.fileStatus.SetProgress(reqMsg.FolderID, reqMsg.RelPath, 0)

			for _, hash := range needMsg.NeededHashs {
				ci, ok := chunkMap[hash]
				if !ok {
					log.Printf("[Server] Bloco não encontrado no índice em memória: %s", hash)
					continue
				}

				buf := make([]byte, ci.Length)
				if _, err := fileHandle.ReadAt(buf, ci.Offset); err != nil {
					log.Printf("[Server] Erro ao ler bloco %s em offset %d: %v", hash[:8], ci.Offset, err)
					continue
				}

				sentCount++
				isLast := sentCount == totalNeeded

				blockData := BlockDataMessage{
					FolderID: reqMsg.FolderID,
					RelPath:  reqMsg.RelPath,
					Hash:     hash,
					Offset:   ci.Offset,
					Length:   ci.Length,
					Data:     buf,
					IsLast:   isLast,
				}
				bdPayload, _ := json.Marshal(blockData)
				if err := sendMessage(conn, &Message{
					Type:     MsgTypeBlockData,
					FolderID: reqMsg.FolderID,
					Payload:  bdPayload,
				}); err != nil {
					log.Printf("[Server] Erro ao enviar bloco %d/%d de %s: %v",
						sentCount, totalNeeded, reqMsg.RelPath, err)
					return
				}

				atomic.AddInt64(&ss.bytesSent, int64(ci.Length))

				progress := float64(sentCount) / float64(totalNeeded)
				ss.fileStatus.SetProgress(reqMsg.FolderID, reqMsg.RelPath, progress)

				if totalNeeded > 20 && sentCount%(totalNeeded/10+1) == 0 {
					log.Printf("[Server] Progresso envio CDC %s: %d/%d blocos (%d%%)",
						reqMsg.RelPath, sentCount, totalNeeded, sentCount*100/totalNeeded)
				}
			}

			atomic.AddInt64(&ss.filesSynced, 1)
			atomic.AddInt64(&ss.completedFiles, 1)
			ss.checkSyncComplete()
			ss.fileStatus.SetStatus(reqMsg.FolderID, reqMsg.RelPath, filesync.StatusSynced, 0)

			log.Printf("[Server] Envio CDC completo: %s (%d blocos)", reqMsg.RelPath, sentCount)
			if ss.onSync != nil {
				ss.onSync(reqMsg.FolderID, reqMsg.RelPath, fmt.Sprintf("ENVIADO (CDC: %d blocos)", sentCount))
			}
			return
		}
		// Se falhou o CDC, cair no envio completo abaixo
		log.Printf("[Server] CDC falhou para %s, enviando completo: %v", reqMsg.RelPath, err)
	}

	// Fallback para arquivos pequenos ou CDC indisponível.
	// Para arquivos grandes (> 100MB) usar chunked send para não carregar
	// o arquivo inteiro na memória (OOM) nem exceder limite do readMessage.
	if info.Size() > 100*1024*1024 {
		log.Printf("[Server] Arquivo grande sem CDC, usando chunked send: %s (%d MB)",
			reqMsg.RelPath, info.Size()/(1024*1024))
		ss.sendFileChunked(conn, reqMsg.FolderID, reqMsg.RelPath, fullPath)
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		log.Printf("[Server] Erro ao ler arquivo: %v", err)
		return
	}

	// Calcular hash para verificação de integridade no receptor
	fileHash := filesync.ComputeDataHash(data)

	dataMsg := FileDataMessage{
		FolderID: reqMsg.FolderID,
		RelPath:  reqMsg.RelPath,
		Data:     data,
		Size:     info.Size(),
		Hash:     fileHash,
	}
	payload, _ := json.Marshal(dataMsg)
	sendMessage(conn, &Message{
		Type:     MsgTypeData,
		FolderID: reqMsg.FolderID,
		Payload:  payload,
	})

	atomic.AddInt64(&ss.bytesSent, int64(len(data)))
	atomic.AddInt64(&ss.filesSynced, 1)
	atomic.AddInt64(&ss.completedFiles, 1)
	ss.checkSyncComplete()
	ss.fileStatus.SetStatus(reqMsg.FolderID, reqMsg.RelPath, filesync.StatusSynced, info.Size())

	if ss.onSync != nil {
		ss.onSync(reqMsg.FolderID, reqMsg.RelPath, "ENVIADO")
	}
}

// handleDataMessage processa dados de arquivo recebidos
func (ss *SyncServer) handleDataMessage(msg *Message) {
	var dataMsg FileDataMessage
	if err := json.Unmarshal(msg.Payload, &dataMsg); err != nil {
		log.Printf("[Server] Erro ao decodificar dados: %v", err)
		return
	}

	folderPath := ss.findFolderPath(dataMsg.FolderID)
	if folderPath == "" {
		log.Printf("[Server] Pasta não encontrada para dados: %s", dataMsg.FolderID)
		return
	}

	fullPath := filepath.Join(folderPath, filepath.FromSlash(dataMsg.RelPath))

	if dataMsg.IsDir {
		os.MkdirAll(fullPath, 0755)
		log.Printf("[Server] Pasta criada: %s", dataMsg.RelPath)
	} else {
		// Criar diretórios pai se necessário
		os.MkdirAll(filepath.Dir(fullPath), 0755)

		// === Verificação de integridade pré-escrita ===
		if ss.cfg.IntegrityCheckEnabled && dataMsg.Hash != "" {
			result := filesync.VerifyChunkIntegrity(dataMsg.Data, dataMsg.Hash)
			ss.integrityReport.AddResult(result)
			if !result.Valid {
				log.Printf("[Integridade] FALHA pré-escrita em %s: dados corrompidos na transferência (esperado=%s, obtido=%s)",
					dataMsg.RelPath, result.ExpectedHash, result.ActualHash)
				if ss.onSync != nil {
					ss.onSync(dataMsg.FolderID, dataMsg.RelPath, "ERRO: integridade falhou")
				}
				return
			}
		}

		// === Versionamento: salvar versão anterior antes de sobrescrever ===
		if ss.versionMgr.IsEnabled() {
			if _, err := ss.versionMgr.SaveVersion(folderPath, dataMsg.RelPath); err != nil {
				log.Printf("[Versão] Aviso: erro ao versionar %s: %v", dataMsg.RelPath, err)
			}
		}

		// Escrever em arquivo temporário para evitar conflito com processos
		// que mantêm o arquivo de destino aberto.
		tmpPath := tempSyncPath(fullPath)
		if err := os.WriteFile(tmpPath, dataMsg.Data, 0644); err != nil {
			log.Printf("[Server] Erro ao salvar arquivo temp: %v", err)
			return
		}

		// Renomear temp → destino (com retry para Windows file locking)
		if err := atomicRenameWithRetry(tmpPath, fullPath); err != nil {
			log.Printf("[Server] ERRO: rename temp→destino %s: %v", dataMsg.RelPath, err)
			os.Remove(tmpPath)
			return
		}

		// === Verificação de integridade pós-escrita ===
		if ss.cfg.IntegrityCheckEnabled && dataMsg.Hash != "" {
			result := filesync.VerifyFileIntegrity(fullPath, dataMsg.Hash)
			ss.integrityReport.AddResult(result)
			if !result.Valid {
				log.Printf("[Integridade] FALHA pós-escrita em %s: arquivo corrompido no disco (esperado=%s, obtido=%s)",
					dataMsg.RelPath, result.ExpectedHash, result.ActualHash)
				if ss.onSync != nil {
					ss.onSync(dataMsg.FolderID, dataMsg.RelPath, "ERRO: verificação pós-escrita falhou")
				}
				return
			}
			log.Printf("[Integridade] OK: %s verificado com sucesso", dataMsg.RelPath)
		}

		log.Printf("[Server] Arquivo recebido: %s (%d bytes)", dataMsg.RelPath, len(dataMsg.Data))
		atomic.AddInt64(&ss.bytesRecv, int64(len(dataMsg.Data)))
		atomic.AddInt64(&ss.filesSynced, 1)
		atomic.AddInt64(&ss.completedFiles, 1)
		ss.checkSyncComplete()

		// Marcar como sincronizado
		ss.fileStatus.SetStatus(dataMsg.FolderID, dataMsg.RelPath, filesync.StatusSynced, int64(len(dataMsg.Data)))

		// Atualizar índice do watcher imediatamente para evitar
		// que a próxima troca de índices gere falso conflito.
		ss.updateWatcherIndex(dataMsg.FolderID, dataMsg.RelPath, fullPath, dataMsg.Hash)
	}

	if ss.onSync != nil {
		ss.onSync(dataMsg.FolderID, dataMsg.RelPath, "RECEBIDO")
	}
}

// handleDeleteMessage processa exclusão de arquivo
func (ss *SyncServer) handleDeleteMessage(msg *Message) {
	var delMsg DeleteFileMessage
	if err := json.Unmarshal(msg.Payload, &delMsg); err != nil {
		return
	}

	// Verificar se a pasta permite sync de delete
	fc := ss.findFolderConfig(delMsg.FolderID)
	if fc != nil && fc.SyncDelete {
		fullPath := filepath.Join(fc.Path, filepath.FromSlash(delMsg.RelPath))
		os.Remove(fullPath)
		log.Printf("[Server] Arquivo excluído: %s", delMsg.RelPath)

		if ss.onSync != nil {
			ss.onSync(delMsg.FolderID, delMsg.RelPath, "EXCLUÍDO")
		}
	}
}

// handleRelayRequest processa solicitação de relay
func (ss *SyncServer) handleRelayRequest(conn net.Conn, msg *Message) {
	var relayMsg RelayRequestMessage
	if err := json.Unmarshal(msg.Payload, &relayMsg); err != nil {
		return
	}

	log.Printf("[Server] Solicitação de relay de %s: %v", relayMsg.DeviceID, relayMsg.Enable)

	// Responder com ACK
	ack := RelayRequestMessage{
		DeviceID: ss.cfg.DeviceID,
		Enable:   ss.cfg.PeerRelayEnabled && relayMsg.Enable,
	}
	payload, _ := json.Marshal(ack)
	sendMessage(conn, &Message{
		Type:    MsgTypeRelayAck,
		Payload: payload,
	})
}

// handleFolderAnnounce processa anúncio de nova pasta de um peer remoto
func (ss *SyncServer) handleFolderAnnounce(conn net.Conn, msg *Message) {
	var announceMsg FolderAnnounceMessage
	if err := json.Unmarshal(msg.Payload, &announceMsg); err != nil {
		log.Printf("[Server] Erro ao decodificar anúncio de pasta: %v", err)
		return
	}

	log.Printf("[Server] Anúncio de pasta recebido de %s: %s (%s)",
		announceMsg.DeviceID, announceMsg.Label, announceMsg.Path)

	// Verificar se já temos essa pasta configurada (por label)
	existing := ss.findFolderConfig(announceMsg.Label)
	if existing != nil {
		log.Printf("[Server] Pasta '%s' já existe localmente em: %s", announceMsg.Label, existing.Path)
		// Já existe, enviar índice para sincronizar via novo stream
		if session := getSession(conn); session != nil {
			if stream, err := session.Open(); err == nil {
				ss.sendFullIndex(&streamConn{Conn: stream, session: session})
				stream.Close()
			}
		} else {
			ss.sendFullIndex(conn)
		}
		return
	}

	// Adaptar o caminho para o usuário local (cross-platform)
	localPath := adaptPathForLocalUser(announceMsg.Path)
	log.Printf("[Server] Criando pasta '%s' em: %s (original: %s)",
		announceMsg.Label, localPath, announceMsg.Path)

	// Criar a pasta no disco se não existir
	if err := os.MkdirAll(localPath, 0755); err != nil {
		log.Printf("[Server] Erro ao criar pasta '%s': %v", localPath, err)
		return
	}

	// Adicionar à configuração
	folder := config.FolderConfig{
		ID:         announceMsg.Label,
		Label:      announceMsg.Label,
		Path:       localPath,
		Enabled:    true,
		SyncDelete: announceMsg.SyncDelete,
	}
	ss.cfg.AddFolder(folder)
	ss.cfg.Save()

	log.Printf("[Server] Pasta '%s' adicionada automaticamente em: %s", folder.Label, localPath)

	// Notificar GUI via callback
	ss.mu.RLock()
	cb := ss.onFolderAdd
	ss.mu.RUnlock()
	if cb != nil {
		cb(folder)
	}

	// Iniciar watcher para a nova pasta e sincronizar
	go func() {
		ss.RefreshWatchers()
		// Aguardar watcher completar scan inicial antes de enviar índice
		time.Sleep(3 * time.Second)
		log.Printf("[Server] Enviando índice completo após receber pasta '%s'", announceMsg.Label)
		if session := getSession(conn); session != nil {
			if stream, err := session.Open(); err == nil {
				ss.sendFullIndex(&streamConn{Conn: stream, session: session})
				stream.Close()
			}
		} else {
			ss.sendFullIndex(conn)
		}
	}()
}
