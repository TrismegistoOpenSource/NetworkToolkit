# Componenti di terze parti

NetworkToolkit è distribuito sotto [licenza MIT](LICENSE). Contiene e ridistribuisce
i componenti elencati qui sotto, ciascuno sotto la propria licenza. Tutte le
licenze sono permissive (MIT / BSD / ISC / Apache-2.0), compatibili con la MIT e
ne permettono la redistribuzione.

I testi completi sono nella cartella [`licenses/`](licenses/). Viaggiano anche
dentro l'app distribuita, nella cartella `LICENZE`, insieme a questo file, alla
licenza MIT e ai testi di licenza di **ogni modulo Go** compilato nel
binario (sottocartella `moduli-go/`, elenco completo in `moduli-go/ELENCO.txt`),
raccolti da `scripts/raccogli-licenze.sh` a ogni build:
`Contents/Resources/LICENZE/` su macOS, accanto all'eseguibile nello zip per
Windows, `usr/share/doc/networktoolkit/` dentro l'AppImage per Linux.

> **Nessun runtime proprietario.** L'interfaccia gira nel browser **già
> presente nel sistema operativo** (WebKit su macOS, WebView2 su Windows,
> WebKitGTK su Linux): non viene ridistribuito con l'app. Il cambio del MAC usa
> le API/strumenti nativi dell'OS (`ifconfig`/`networksetup` su macOS, netlink
> su Linux, registro + IP Helper API su Windows): nessun binario esterno viene
> scaricato o impacchettato.

## Runtime

| Componente | Licenza | Uso | Copyright / origine |
|---|---|---|---|
| [Wails v2](https://wails.io) (`github.com/wailsapp/wails/v2`) | MIT ([testo](licenses/MIT-wails.txt)) | shell dell'app: finestra webview, binding Go↔frontend, dialoghi | © Lea Anthony e contributori |
| Libreria standard di Go | BSD-3-Clause ([testo](licenses/BSD-3-Clause-Go.txt)) | rete, crypto/rand, embed | © The Go Authors |
| [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) | BSD-3-Clause ([testo](licenses/BSD-3-Clause-Go.txt)) | registro Windows, IP Helper API, ShellExecute, syscall | © The Go Authors |

## Solo Linux

| Componente | Licenza | Uso | Copyright / origine |
|---|---|---|---|
| [vishvananda/netlink](https://github.com/vishvananda/netlink) | Apache-2.0 ([testo](licenses/Apache-2.0-netlink.txt)) | cambio MAC e lettura schede via netlink (puro Go, nessun cgo) | © Vishvananda Ishaya, Docker, Inc. e contributori |
| [vishvananda/netns](https://github.com/vishvananda/netns) (dipendenza di netlink) | Apache-2.0 ([testo](licenses/Apache-2.0-netlink.txt)) | gestione namespace di rete | © Vishvananda Ishaya e contributori |

Wails tira dentro ulteriori moduli Go sotto MIT/BSD (l'insieme transitivo del
suo `go.mod`, su Windows per esempio go-webview2, go-ole e go-toast); i
testi delle rispettive licenze viaggiano con l'app in `LICENZE/moduli-go/`.

## Codice di altri autori dentro i moduli (licenze annidate)

Alcuni moduli contengono, in una sottocartella, pacchetti di altri autori con
una licenza propria. Finiscono nel binario, quindi i loro testi viaggiano con
l'app in `LICENZE/moduli-go/<modulo>/annidate/` (li raccoglie
`scripts/raccogli-licenze.sh`, e `scripts/verifica-pacchetto.sh` controlla che
ci siano):

| Pacchetto | Licenza | Copyright | Dove finisce |
|---|---|---|---|
| `wails/v2/internal/frontend/desktop/windows/winc` | MIT | © 2019 winc Authors | solo Windows |
| `wails/v2/internal/go-common-file-dialog` | MIT | © 2019 Harry Phillips | solo Windows |
| `go-webview2/webviewloader` | ISC | © 2020 John Chadwick, © 2022 Wails Project Developers | solo Windows |
| `wails/v2/internal/typescriptify` | Apache-2.0 | © 2015 Tomo Krajina | tutti i sistemi |

## Strumenti di sistema invocati (non ridistribuiti)

NetworkToolkit chiama strumenti già presenti nell'OS, senza impacchettarli:

- **macOS**: `ifconfig`, `networksetup`, `launchctl`, `osascript`, `netstat` — parte di macOS.
- **Linux**: `pkexec` (polkit), e opzionalmente `nmcli` (NetworkManager) o
  `udevadm` (systemd) per la persistenza — parte della distribuzione.
- **Windows**: `powershell` (Restart-NetAdapter) — parte di Windows.

Nessuno di questi viene distribuito con l'app.
