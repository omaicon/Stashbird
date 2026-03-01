package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"Stashbird/config"

	"github.com/hashicorp/yamux"
)

// acceptLoop aceita conexões entrantes
func (ss *SyncServer) acceptLoop() {
	for {
		conn, err := ss.listener.Accept()
		if err != nil {
			select {
			case <-ss.stopChan:
				return
			default:
				log.Printf("[Server] Erro ao aceitar conexão: %v", err)
				continue
			}
		}

		go ss.handleConnection(conn)
	}
}

// handleConnection lida com uma conexão entrante, estabelecendo uma sessão yamux,
// recebendo o Hello do peer remoto, registrando-o e trocando índices.
func (ss *SyncServer) handleConnection(rawConn net.Conn) {
	remoteAddr := rawConn.RemoteAddr().String()
	log.Printf("[Server] Nova conexão de: %s", remoteAddr)

	session, err := yamux.Server(rawConn, yamuxConfig())
	if err != nil {
		log.Printf("[Server] Erro ao criar sessão yamux: %v", err)
		rawConn.Close()
		return
	}

	log.Printf("[Server] Sessão yamux estabelecida com: %s", remoteAddr)

	// Esperar o primeiro stream que deve conter o Hello do peer
	firstStream, err := session.Accept()
	if err != nil {
		log.Printf("[Server] Erro ao aceitar primeiro stream (hello): %v", err)
		session.Close()
		rawConn.Close()
		return
	}

	// Ler o Hello do peer
	helloMsg, err := readMessage(firstStream)
	if err != nil || helloMsg.Type != MsgTypeHello {
		log.Printf("[Server] Peer %s não enviou Hello válido (err=%v, tipo=%d)", remoteAddr, err, 0)
		firstStream.Close()
		session.Close()
		rawConn.Close()
		return
	}

	var hello HelloMessage
	if err := json.Unmarshal(helloMsg.Payload, &hello); err != nil {
		log.Printf("[Server] Erro ao decodificar Hello de %s: %v", remoteAddr, err)
		firstStream.Close()
		session.Close()
		rawConn.Close()
		return
	}

	log.Printf("[Server] Hello recebido de: %s (ID=%s)", hello.DeviceName, hello.DeviceID)

	// Enviar nosso Hello de volta pelo mesmo stream
	myHello := HelloMessage{
		DeviceID:   ss.cfg.DeviceID,
		DeviceName: ss.cfg.DeviceName,
	}
	helloPayload, _ := json.Marshal(myHello)
	sendMessage(firstStream, &Message{Type: MsgTypeHello, Payload: helloPayload})
	firstStream.Close()

	// Procurar o peer na configuração pelo DeviceID, IP ou nome
	// e atualizar o ID do peer se encontrar por IP/nome mas ID difere
	var matchedPeer *config.PeerConfig
	var remoteIP string
	if tcpAddr, ok := rawConn.RemoteAddr().(*net.TCPAddr); ok {
		remoteIP = tcpAddr.IP.String()
	} else {
		// Extrair IP de string "ip:port"
		addrStr := rawConn.RemoteAddr().String()
		if host, _, err := net.SplitHostPort(addrStr); err == nil {
			remoteIP = host
		}
	}
	log.Printf("[Server] Buscando peer: DeviceID=%s, IP=%s, Nome=%s", hello.DeviceID, remoteIP, hello.DeviceName)
	for _, p := range ss.cfg.GetPeers() {
		if p.TailscaleIP == remoteIP || p.Name == hello.DeviceName || p.ID == hello.DeviceID {
			matched := p
			matchedPeer = &matched
			log.Printf("[Server] Peer encontrado na config: %s (ID=%s, IP=%s)", p.Name, p.ID, p.TailscaleIP)
			// Se o peer foi encontrado por IP/nome mas o ID real é diferente, atualizar
			if hello.DeviceID != "" && matchedPeer.ID != hello.DeviceID {
				oldID := matchedPeer.ID
				log.Printf("[Server] Atualizando ID do peer %s na config: %s → %s", matchedPeer.Name, oldID, hello.DeviceID)
				// Remapear no mapa de peers
				ss.mu.Lock()
				if existing, ok := ss.peers[oldID]; ok {
					delete(ss.peers, oldID)
					ss.peers[hello.DeviceID] = existing
				}
				ss.mu.Unlock()
				if ss.cfg.UpdatePeerID(oldID, hello.DeviceID) {
					ss.cfg.Save()
				}
				matchedPeer.ID = hello.DeviceID
			}
			break
		}
	}

	// Se não encontrou por config, criar um PeerConfig virtual
	if matchedPeer == nil {
		log.Printf("[Server] Peer %s (%s) não encontrado na config — registrando dinamicamente", hello.DeviceName, hello.DeviceID)
		matchedPeer = &config.PeerConfig{
			ID:          hello.DeviceID,
			Name:        hello.DeviceName,
			TailscaleIP: remoteIP,
			Port:        ss.cfg.ListenPort,
			Enabled:     true,
		}
	}

	// Verificar se já temos uma conexão ativa com este peer (via connectToPeer)
	ss.mu.RLock()
	existingPC, exists := ss.peers[matchedPeer.ID]
	ss.mu.RUnlock()

	if exists {
		existingPC.mu.Lock()
		existingAlive := existingPC.alive
		existingPC.mu.Unlock()

		if existingAlive {
			// Já temos conexão ativa — usar resolução determinística:
			// o peer com DeviceID "maior" (lexicográfico) mantém a conexão de saída.
			// O outro aceita a conexão de entrada.
			if ss.cfg.DeviceID > hello.DeviceID {
				// Nós temos prioridade — rejeitar esta conexão entrante
				log.Printf("[Server] Já conectado a %s via outbound — rejeitando conexão entrante",
					hello.DeviceName)
				session.Close()
				rawConn.Close()
				return
			}
			// O peer remoto tem prioridade — substituir nossa conexão de saída
			log.Printf("[Server] Substituindo conexão outbound para %s pela conexão inbound",
				hello.DeviceName)
			existingPC.mu.Lock()
			existingPC.alive = false
			if existingPC.session != nil {
				existingPC.session.Close()
			}
			existingPC.mu.Unlock()
		}
	}

	// Registrar o peer no mapa de conexões
	pc := &PeerConnection{
		conn:     rawConn,
		session:  session,
		peer:     *matchedPeer,
		alive:    true,
		lastPing: time.Now(),
	}

	ss.mu.Lock()
	ss.peers[matchedPeer.ID] = pc
	ss.mu.Unlock()

	log.Printf("[Server] Peer registrado via conexão entrante: %s (ID=%s, IP=%s)",
		matchedPeer.Name, matchedPeer.ID, remoteIP)

	if ss.onStatus != nil {
		ss.onStatus(matchedPeer.ID, true)
	}

	// Anunciar todas as pastas para o peer (para criar pastas que não existem no remoto)
	ss.sendFolderAnnouncementsToSession(session)

	// Enviar nosso índice completo para o peer via um novo stream
	if stream, err := session.Open(); err == nil {
		sc := &streamConn{Conn: stream, session: session}
		ss.sendFullIndex(sc)
		stream.Close()
	}

	// Aceitar streams subsequentes até a sessão encerrar
	for {
		stream, err := session.Accept()
		if err != nil {
			if err != io.EOF {
				select {
				case <-ss.stopChan:
				default:
					log.Printf("[Server] Erro ao aceitar stream yamux de %s: %v", hello.DeviceName, err)
				}
			}
			break
		}
		go ss.handleStream(&streamConn{Conn: stream, session: session})
	}

	// Peer desconectou
	pc.mu.Lock()
	pc.alive = false
	pc.mu.Unlock()

	if ss.onStatus != nil {
		ss.onStatus(matchedPeer.ID, false)
	}

	log.Printf("[Server] Peer desconectou: %s (ID=%s)", matchedPeer.Name, matchedPeer.ID)
	session.Close()
	rawConn.Close()
}

// acceptPeerStreams aceita streams iniciados pelo peer remoto.
// Bloqueia até a sessão yamux encerrar.
func (ss *SyncServer) acceptPeerStreams(pc *PeerConnection) {
	pc.mu.Lock()
	session := pc.session
	pc.mu.Unlock()
	if session == nil {
		return
	}
	for {
		stream, err := session.Accept()
		if err != nil {
			return
		}
		go ss.handleStream(&streamConn{Conn: stream, session: session})
	}
}

// connectToPeers conecta a todos os peers configurados
func (ss *SyncServer) connectToPeers() {
	for _, peer := range ss.cfg.GetPeers() {
		if !peer.Enabled {
			continue
		}
		go ss.connectToPeer(peer)
	}
}

// connectToPeer conecta a um peer específico
func (ss *SyncServer) connectToPeer(peer config.PeerConfig) {
	addr := fmt.Sprintf("%s:%d", peer.TailscaleIP, peer.Port)
	for {
		select {
		case <-ss.stopChan:
			return
		default:
		}

		var conn net.Conn
		var err error

		// Se estamos no modo tsnet e conectado, usar dial do tsnet
		if ss.tailscale != nil && ss.tailscale.GetMode() == ModeTsnet {
			connected, _ := ss.tailscale.GetStatus()
			if connected {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				conn, err = ss.tailscale.Dial(ctx, "tcp", addr)
				cancel()
			} else {
				conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
			}
		} else {
			conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
		}

		if err != nil {
			log.Printf("[Server] Erro ao conectar a %s (%s): %v", peer.Name, addr, err)
			time.Sleep(10 * time.Second)
			continue
		}

		// Estabelecer sessão yamux sobre a conexão TCP
		session, err := yamux.Client(conn, yamuxConfig())
		if err != nil {
			log.Printf("[Server] Erro ao criar sessão yamux com %s: %v", peer.Name, err)
			conn.Close()
			time.Sleep(10 * time.Second)
			continue
		}

		log.Printf("[Server] Conectado ao peer: %s (%s) [yamux]", peer.Name, addr)

		// === HANDSHAKE: enviar Hello para o servidor ===
		helloStream, err := session.Open()
		if err != nil {
			log.Printf("[Server] Erro ao abrir stream para Hello com %s: %v", peer.Name, err)
			session.Close()
			conn.Close()
			time.Sleep(10 * time.Second)
			continue
		}

		myHello := HelloMessage{
			DeviceID:   ss.cfg.DeviceID,
			DeviceName: ss.cfg.DeviceName,
		}
		helloPayload, _ := json.Marshal(myHello)
		if err := sendMessage(helloStream, &Message{Type: MsgTypeHello, Payload: helloPayload}); err != nil {
			log.Printf("[Server] Erro ao enviar Hello para %s: %v", peer.Name, err)
			helloStream.Close()
			session.Close()
			conn.Close()
			time.Sleep(10 * time.Second)
			continue
		}

		// Aguardar Hello de volta do servidor
		replyMsg, err := readMessage(helloStream)
		if err != nil {
			log.Printf("[Server] Erro ao receber Hello de volta de %s: %v", peer.Name, err)
			helloStream.Close()
			session.Close()
			conn.Close()
			time.Sleep(10 * time.Second)
			continue
		}
		helloStream.Close()

		if replyMsg.Type == MsgTypeHello {
			var serverHello HelloMessage
			if err := json.Unmarshal(replyMsg.Payload, &serverHello); err == nil {
				log.Printf("[Server] Hello recebido de %s (ID=%s)", serverHello.DeviceName, serverHello.DeviceID)
				// Atualizar o ID do peer se o DeviceID real difere do config
				if serverHello.DeviceID != "" && serverHello.DeviceID != peer.ID {
					oldID := peer.ID
					log.Printf("[Server] Atualizando ID do peer %s: %s → %s", peer.Name, oldID, serverHello.DeviceID)
					// Remapear no mapa de peers (se já registrado)
					ss.mu.Lock()
					if existing, ok := ss.peers[oldID]; ok {
						delete(ss.peers, oldID)
						ss.peers[serverHello.DeviceID] = existing
					}
					ss.mu.Unlock()
					// Atualizar na config e persistir
					if ss.cfg.UpdatePeerID(oldID, serverHello.DeviceID) {
						ss.cfg.Save()
					}
					peer.ID = serverHello.DeviceID
				}
			}
		}

		// Verificar se o servidor já registrou esta conexão como inbound
		// (resolução de conexão duplicada) — se a sessão fechou, reconectar
		if session.IsClosed() {
			log.Printf("[Server] Sessão com %s foi fechada pelo servidor (conexão duplicada resolvida)", peer.Name)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		pc := &PeerConnection{
			conn:     conn,
			session:  session,
			peer:     peer,
			alive:    true,
			lastPing: time.Now(),
		}

		ss.mu.Lock()
		ss.peers[peer.ID] = pc
		ss.mu.Unlock()

		if ss.onStatus != nil {
			ss.onStatus(peer.ID, true)
		}

		// Anunciar todas as pastas para o peer (para criar pastas que não existem no remoto)
		ss.sendFolderAnnouncementsToSession(session)

		// Enviar nosso índice via stream dedicado
		if stream, err := session.Open(); err == nil {
			sc := &streamConn{Conn: stream, session: session}
			ss.sendFullIndex(sc)
			stream.Close()
		}

		// Aceitar streams do servidor até a sessão encerrar
		ss.acceptPeerStreams(pc)

		pc.mu.Lock()
		pc.alive = false
		pc.mu.Unlock()

		session.Close()

		if ss.onStatus != nil {
			ss.onStatus(peer.ID, false)
		}

		// Reconectar após delay — mas apenas se não temos uma conexão inbound ativa
		time.Sleep(10 * time.Second)

		// Verificar se há conexão inbound ativa para este peer
		ss.mu.RLock()
		if existPC, ok := ss.peers[peer.ID]; ok {
			existPC.mu.Lock()
			isAlive := existPC.alive
			existPC.mu.Unlock()
			if isAlive {
				ss.mu.RUnlock()
				log.Printf("[Server] Peer %s já conectado via inbound — parando tentativas outbound", peer.Name)
				// Aguardar até a conexão inbound cair, depois retomar
				for {
					time.Sleep(5 * time.Second)
					select {
					case <-ss.stopChan:
						return
					default:
					}
					ss.mu.RLock()
					if existPC2, ok2 := ss.peers[peer.ID]; ok2 {
						existPC2.mu.Lock()
						stillAlive := existPC2.alive
						existPC2.mu.Unlock()
						ss.mu.RUnlock()
						if !stillAlive {
							log.Printf("[Server] Conexão inbound com %s caiu — retomando tentativas outbound", peer.Name)
							break
						}
					} else {
						ss.mu.RUnlock()
						break
					}
				}
				continue
			}
		}
		ss.mu.RUnlock()
	}
}

// pingLoop envia pings periódicos para manter conexões vivas
func (ss *SyncServer) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ss.mu.RLock()
			for _, pc := range ss.peers {
				pc.mu.Lock()
				alive := pc.alive
				session := pc.session
				pc.mu.Unlock()

				if alive && session != nil {
					if stream, err := session.Open(); err == nil {
						sendMessage(stream, &Message{Type: MsgTypePing})
						stream.Close()
					}
				}
			}
			ss.mu.RUnlock()
		case <-ss.stopChan:
			return
		}
	}
}
