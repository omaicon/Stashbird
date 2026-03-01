package network

import (
	"encoding/json"
	"log"
	"time"

	filesync "Stashbird/sync"
)

// startWatchers inicia watchers para todas as pastas configuradas
func (ss *SyncServer) startWatchers() {
	folders := ss.cfg.GetFolders()
	interval := time.Duration(ss.cfg.ScanIntervalSec) * time.Second

	started := 0
	for _, folder := range folders {
		if !folder.Enabled {
			log.Printf("[Watcher] Pasta '%s' desabilitada, ignorando", folder.Label)
			continue
		}

		watcher := filesync.NewFolderWatcher(folder, interval, func(changes []filesync.FileChange) {
			ss.onChangesDetected(changes)
		})
		watcher.Start()
		ss.watchers[folder.ID] = watcher
		started++
	}
	log.Printf("[Watcher] %d watchers iniciados de %d pastas configuradas", started, len(folders))
}

// onChangesDetected é chamado quando mudanças são detectadas em uma pasta
func (ss *SyncServer) onChangesDetected(changes []filesync.FileChange) {
	if len(changes) == 0 {
		return
	}

	// Contar tipos de mudança para logging
	var created, modified, deleted int
	for _, c := range changes {
		switch c.Type {
		case filesync.ChangeCreated:
			created++
		case filesync.ChangeModified:
			modified++
		case filesync.ChangeDeleted:
			deleted++
		}
	}
	folderID := changes[0].FolderID
	log.Printf("[Watcher] Mudanças detectadas em '%s': %d novos, %d modificados, %d excluídos",
		ss.getFolderLabel(folderID), created, modified, deleted)

	// Atualizar status dos arquivos detectados
	for _, change := range changes {
		switch change.Type {
		case filesync.ChangeCreated, filesync.ChangeModified:
			ss.fileStatus.SetStatus(change.FolderID, change.FileInfo.RelPath, filesync.StatusLocal, change.FileInfo.Size)
		case filesync.ChangeDeleted:
			ss.fileStatus.Remove(change.FolderID, change.FileInfo.RelPath)
		}
	}

	ss.mu.RLock()
	defer ss.mu.RUnlock()

	// Contar peers conectados
	var connectedPeers int
	for _, pc := range ss.peers {
		pc.mu.Lock()
		if pc.alive && pc.session != nil {
			connectedPeers++
		}
		pc.mu.Unlock()
	}
	if connectedPeers == 0 {
		log.Printf("[Watcher] Nenhum peer conectado — mudanças serão sincronizadas quando um peer conectar")
		return
	}

	var sentCount int
	for _, pc := range ss.peers {
		pc.mu.Lock()
		alive := pc.alive
		session := pc.session
		pc.mu.Unlock()

		if !alive || session == nil {
			continue
		}

		for _, change := range changes {
			label := ss.getFolderLabel(change.FolderID)
			stream, err := session.Open()
			if err != nil {
				log.Printf("[Watcher] Erro ao abrir stream para peer: %v", err)
				break
			}
			switch change.Type {
			case filesync.ChangeCreated, filesync.ChangeModified:
				// Enviar atualização de índice
				indexMsg := IndexMessage{
					DeviceID:    ss.cfg.DeviceID,
					FolderID:    label,
					FolderLabel: label,
					Files: map[string]*filesync.FileInfo{
						change.FileInfo.RelPath: change.FileInfo,
					},
				}
				payload, _ := json.Marshal(indexMsg)
				sendMessage(stream, &Message{
					Type:     MsgTypeIndexUpdate,
					FolderID: label,
					Payload:  payload,
				})

			case filesync.ChangeDeleted:
				delMsg := DeleteFileMessage{
					FolderID: label,
					RelPath:  change.FileInfo.RelPath,
				}
				payload, _ := json.Marshal(delMsg)
				sendMessage(stream, &Message{
					Type:     MsgTypeDeleteFile,
					FolderID: label,
					Payload:  payload,
				})
			}
			stream.Close()
			sentCount++
		}
	}
	log.Printf("[Watcher] %d atualizações de índice enviadas para %d peers", sentCount, connectedPeers)
}

// RefreshWatchers reinicia watchers (útil após mudar configuração)
func (ss *SyncServer) RefreshWatchers() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Parar watchers existentes
	for _, w := range ss.watchers {
		w.Stop()
	}
	ss.watchers = make(map[string]*filesync.FolderWatcher)

	// Reiniciar
	ss.startWatchers()
}
