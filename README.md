<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go&logoColor=white" />
  <img src="https://img.shields.io/badge/Tailscale-Mesh_VPN-000000?style=for-the-badge&logo=tailscale&logoColor=white" />
  <img src="https://img.shields.io/badge/Plataforma-Windows_|_Linux_|_macOS-blue?style=for-the-badge" />
  <img src="https://img.shields.io/badge/Licença-MIT-green?style=for-the-badge" />
</p>

<h1 align="center">🐦 Stashbird</h1>

<p align="center">
  <strong>Syncthing + Obsidian + Notion tiveram um filho em Go.</strong><br>
  Sync de arquivos P2P + Editor Markdown um binário, zero desculpas.
</p>

<p align="center">
  Seus arquivos vão direto de um PC pro outro, sem passar pela nuvem de ninguém.<br>
 
</p>

---

## Tá, mas o que é isso?

Imagine se o Syncthing, o Obsidian e o Google Drive entrassem num bar e decidissem fundir numa coisa só. Esse é o Stashbird.

- **Sincroniza arquivos** direto entre seus dispositivos (sem servidor, sem nuvem, sem drama)
- **Edita Markdown** com wikilinks `[[nota]]`, backlinks, grafo de notas tipo Obsidian, mas embutido
- **Roda num binário só** compilou, executou, pronto.
> 💡 O nome vem do [Clark's Nutcracker](https://en.wikipedia.org/wiki/Clark%27s_nutcracker), um pássaro que esconde **98.000 sementes** e lembra onde guardou cada uma. Basicamente o oposto de você tentando achar aquele PDF.

---

## O que faz

**📁 Sync de Arquivos**
- Peer-to-peer via Tailscale criptografado, atravessa NAT, zero configuração de rede
- Delta sync com CDC, só manda os pedaços que mudaram, não o arquivo inteiro
- 8 transferências paralelas, verificação BLAKE3, escrita atômica
- Versionamento automático + 3 estratégias de conflito (renomear, mais novo, mais velho)

**✏️ Editor Markdown**
- CodeMirror 6 com preview em tempo real (split/source/preview)
- Wikilinks `[[nota]]` com autocompletar, backlinks e grafo interativo (D3.js)
- Toolbar, callouts, upload de imagens e tudo que você espera e mais um pouco

**🌐 Rede**
- Tailscale CLI ou tsnet embutido (funciona sem instalar o Tailscale!)
- Descoberta automática de peers na sua tailnet
- Protocolo binário customizado com multiplexação Yamux

**🖥️ Interface**
- Web UI estilo Google Drive, tema claro/escuro, lista/grade, busca global
- Preview de imagens, vídeos, PDF, código direto no browser
- 100% responsivo, o acesso funciona até no celular do seu primo

---

## Quero testar AGORA

### Como Compilar

**Requisitos:** Go 1.25+ e um computador (Windows, Linux ou macOS).

```bash
# Baixar dependências
go mod tidy

# Básico
go build -o Stashbird.exe

# Sem janelinha do CMD (recomendado)
go build -ldflags "-H=windowsgui" -o Stashbird.exe

# Binário menor pra distribuir pros amigos
go build -ldflags "-H=windowsgui -s -w" -o Stashbird.exe
```

```bash
# Linux / macOS
GOOS=linux GOARCH=amd64 go build -o Stashbird
GOOS=darwin GOARCH=arm64 go build -o Stashbird   # Apple Silicon
```

## Como usar (em 60 segundos)

1. **Execute o binário** o browser padrão abre sozinho
2. **Adicione pastas** na barra lateral, são as pastas que vão editar/sincronizar
3. **Configure o Tailscale** na aba **Ajustes** (via Auth Key) ou use o modo CLI se já tiver instalado
4. **Busque dispositivos** o Stashbird acha outros peers automaticamente
5. **Pronto** seus arquivos sincronizam sozinhos. Clique num `.md` pra editar.

É isso. Não tem passo 6.

---

## Feito com

Go, Tailscale/tsnet, BoltDB, BLAKE3, Yamux, Goldmark, CodeMirror 6, D3.js, e uma quantidade questionável de café.

---

## 📄 Licença

MIT, faça o que quiser, só não diga que foi você que inventou.

---

<p align="center">
  Feito com ☕ e Go<br>
  <em>Seus arquivos. Seus dispositivos. Sem nuvem.</em><br>
  <strong>Stashbird</strong>  como o Clark's Nutcracker, nunca esquece onde guardou.
</p>
