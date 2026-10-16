# NetworkToolkit

Cassetta degli attrezzi di rete (**Go + Wails v2**), interfaccia pulita con
**menù dei toolkit a sinistra** e **strumento a destra** (stesso layout di
Aurora Studio), tema **scuro/chiaro** commutabile.

[![Build](https://github.com/TrismegistoOpenSource/NetworkToolkit/actions/workflows/build.yml/badge.svg)](https://github.com/TrismegistoOpenSource/NetworkToolkit/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> **Versione 0.2** — quattro strumenti: **cambio del MAC address** e **misura
> della velocità** della connessione su tutti i sistemi, più i toolkit
> **Tailscale** e **OpenVPN** solo su Linux (su macOS e Windows i due servizi
> hanno già le loro interfacce ufficiali).

## Cosa fa il toolkit "MAC Address"

- **Rileva da solo le schede di rete reali** su macOS, Linux e Windows, e mostra
  già **selezionata** quella attiva (distinta da un **LED verde**). Se ce ne
  sono più d'una le mostra tutte: puoi sceglierne una o più.
- Mostra il **MAC corrente** e, quando l'OS lo espone, quello **di fabbrica**.
- **Switcha** il MAC con un clic: genera un indirizzo **casuale valido** (lo
  stesso tipo "amministrato localmente" che usano iPhone/Android per la privacy)
  e lo applica. Ogni clic produce un MAC nuovo.
  - **Temporaneo** — si perde al riavvio (macOS, Linux).
  - **Permanente** — sopravvive al riavvio (Windows sempre; Linux via
    NetworkManager/systemd; macOS via LaunchDaemon).
- **Ripristina** il MAC di fabbrica e rimuove la persistenza.
- Una **console** mostra il log di ogni operazione (successi ed errori).
- Ogni cambio è **verificato**: se il MAC non è davvero cambiato, l'app lo dice
  invece di dichiarare un falso successo.

L'app **non gira mai come amministratore**: chiede la password solo al momento
dello switch, tramite il dialog nativo del sistema (uno per clic).

## Cosa fa il toolkit "Velocità"

Misura la banda reale della connessione su due quadranti (download e upload),
aggiornati una volta al secondo.

- **La sorgente del traffico** sono gli endpoint pubblici di
  `speed.cloudflare.com` (gli stessi del test di Cloudflare): nessun token da
  procurarsi e nessun blocco geografico, e la rete anycast risponde dal nodo
  più vicino.
- **La misura non conta i byte delle richieste HTTP** (l'overhead del
  protocollo falserebbe il numero): legge i contatori RX/TX che il sistema
  operativo tiene già per la scheda di rete, prima e dopo un intervallo di un
  secondo. Di conseguenza il numero comprende **tutto** il traffico che passa
  dalla scheda, anche quello di fondo del computer — è la banda davvero in
  uso, non solo quella del test.
- **Motore e interfaccia sono separati**: la misura gira in un processo
  headless a parte che stampa una riga al secondo; la finestra la legge e
  anima i quadranti. Chiudere la finestra — o ucciderla di forza — ferma il
  motore, che altrimenti continuerebbe a generare traffico a vuoto.

Non servono privilegi di amministratore: né per leggere i contatori, né per
generare il traffico.

## Toolkit "Tailscale" e "OpenVPN" (solo Linux)

Su macOS e Windows i due programmi hanno già le loro interfacce ufficiali: lì
questi toolkit non compaiono. Su Linux la GUI **non reimplementa nulla**: ogni
tasto lancia i normali comandi da terminale (`systemctl`, `tailscale`,
`curl`), e una **console** mostra la riga di comando esatta e il suo output in
diretta (stdout e stderr, con l'ora), da copiare o svuotare. Le azioni da
amministratore chiedono la password con il dialog di polkit (`pkexec`).

**Tailscale**
- **Servizio** (`tailscaled`): Avvia, Ferma e l'interruttore **All'avvio del
  sistema** (`systemctl enable`/`disable`), con il LED e lo stato vero letto
  da `systemctl is-active` e `is-enabled`.
- **Connessione**: Connetti (`tailscale up`) e Disconnetti
  (`tailscale down`). Senza password se sei operatore di Tailscale
  (`sudo tailscale set --operator=$USER`, una volta sola), altrimenti il
  comando si ripete con pkexec. Se serve il login, l'indirizzo compare in
  console e si apre con un clic.
- **Macchine della rete**: nome, IP Tailscale e se sono in linea, da
  `tailscale status --json`.

**OpenVPN**
- Rileva da solo i tunnel: `/etc/openvpn/client/<nome>.conf` →
  `openvpn-client@<nome>`, più le unità di prima `openvpn@<nome>`
  (`/etc/openvpn/<nome>.conf`). Un `.ovpn` va rinominato in `.conf` (systemd
  legge solo quelli): il toolkit lo dice con il comando. Più tunnel → menu a
  tendina (con il segno di quelli che partono all'avvio), uno solo → il nome,
  nessuno → come aggiungerne uno.
- Ogni tunnel è un servizio systemd: **Accendi/Spegni**
  (`systemctl start`/`stop`) e **All'avvio del sistema** (`enable`/`disable`).
- **IP pubblico prima e dopo** ogni accensione e spegnimento, con un `curl` a
  `https://www.cloudflare.com/cdn-cgi/trace`: se non cambia, il traffico verso
  Internet non passa dal tunnel.

I LED si aggiornano ogni 4 secondi **solo mentre il toolkit è visibile**. Se
manca il programma, systemd o pkexec, il LED resta grigio e il toolkit mostra
il comando per installare ciò che manca.

## Download

Le build compilate sono nella pagina
**[Release](https://github.com/TrismegistoOpenSource/NetworkToolkit/releases)**.

| File | Piattaforma |
|---|---|
| `NetworkToolkit-AppleSilicon.zip` | macOS Apple Silicon (M1 e successivi) |
| `NetworkToolkit-Intel.zip` | macOS Intel |
| `NetworkToolkit-Windows.zip` | Windows x64 (cartella con l'exe e le licenze) |
| `NetworkToolkit-Linux-x86_64.AppImage` | Linux x64 |

### macOS: sbloccare l'app al primo avvio

L'app **non è firmata con un Apple Developer ID**, quindi al primo avvio macOS la
mette in quarantena e dice che è "danneggiata". Non lo è: è la quarantena.
Trascina l'app in **Applicazioni**, poi nel Terminale:

```bash
xattr -dr com.apple.quarantine /Applications/NetworkToolkit-AppleSilicon.app
```

(oppure clic destro sull'app → **Apri** → di nuovo **Apri**). Va fatto una volta
sola. Questa sezione sparirà il giorno in cui ci sarà una firma Apple
riconosciuta.

### Windows

Al primo avvio compare l'avviso SmartScreen (manca un certificato di
code-signing): **Ulteriori informazioni → Esegui comunque**. Lo switch chiede
conferma tramite il prompt **UAC**.

### Linux

L'AppImage è autoconsistente. Per il dialog della password serve **polkit**
(`pkexec`), presente su quasi tutti i desktop. La persistenza usa
**NetworkManager** se attivo, altrimenti un file `.link` di systemd-networkd.

## Note per piattaforma

- **Wi-Fi**: cambiare il MAC richiede di disassociarsi prima. L'app spegne e
  riaccende la radio attorno al cambio. Alcuni driver Wi-Fi (soprattutto su
  Windows) accettano **solo** MAC "amministrati localmente" — quelli che l'app
  genera per costruzione.
- **Windows**: il cambio passa dal valore di registro `NetworkAddress` ed è
  **sempre permanente** (non esiste un temporaneo); l'app riavvia la scheda per
  applicarlo.
- **macOS/Linux**: il cambio temporaneo torna indietro al riavvio; quello
  permanente resta finché non premi **Ripristina**.

## Compilare dai sorgenti

Prerequisiti: **Go 1.25+**, il **CLI di Wails v2** e **Node.js/npm** (frontend
TypeScript+Vite).

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
bash scripts/build.sh        # compila per il sistema su cui gira
bash scripts/pacchetto.sh    # zip (macOS, Windows) o AppImage (Linux)
```

Su macOS produce **due app separate ad architettura pura** (non un universal
binary), con nome e bundle id distinti. Gli artefatti finiscono in
`../build/`, fuori dal sorgente.

Su Linux servono `libgtk-3-dev` e `libwebkit2gtk-4.1-dev` e il tag
`-tags webkit2_41` (già gestito da `scripts/build.sh`).

Su Windows gli script si lanciano da **Git Bash**; l'exe si compila senza cgo,
ma i test con `-race` di `scripts/ci-locale.sh` vogliono **gcc** (per esempio
WinLibs, `winget install BrechtSanders.WinLibs.POSIX.UCRT`).

Prova completa del proprio sistema (vet, test con `-race`, build, pacchetto,
verifica e avvio), come la fanno le tre macchine prima di ogni versione:
`bash scripts/ci-locale.sh`. GitHub fa solo build e release, sui tag.

## Struttura

```
main.go                  avvio finestra + deviazione nei modi headless
app.go                   metodi esposti al frontend (lista, switch, reset)
speedtest_app.go         avvio/arresto del motore di misura e inoltro dei campioni
cmd_speedtest.go         modo headless del motore (una riga al secondo su stdout)
internal/macaddr/        validazione e generazione MAC (puro Go, con test)
internal/netif/          elenco schede + stato, un file per OS (non privilegiato)
internal/changer/        applicazione dei cambi, un file per OS (privilegiato)
internal/elevate/        rilancio dell'helper con privilegi (osascript/pkexec/UAC)
internal/store/          memoria del MAC di fabbrica (redirigibile, con test)
internal/speedtest/      traffico + lettura contatori di rete, un file per OS
internal/vpn/            toolkit Tailscale e OpenVPN (solo Linux; stub altrove)
vpn_app.go               metodi dei toolkit VPN esposti al frontend
frontend/                interfaccia (HTML/CSS/TypeScript), layout Aurora + LED RsyncGUI
```

## Sicurezza

- La GUI non gira mai da root; solo il piccolo helper viene elevato, per la
  durata di una singola operazione.
- Nessuna password, chiave o segreto nel codice.
- Nessun binario esterno scaricato o impacchettato: solo strumenti già presenti
  nell'OS.

## Licenza

[MIT](LICENSE) © 2026 Trismegisto Open Source: software libero, si può usare, copiare,
modificare e ridistribuire. I componenti di terze parti e le loro licenze (tutte
permissive) sono in [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md).

Contributi e segnalazioni: [CONTRIBUTING.md](CONTRIBUTING.md). Problemi di
sicurezza: [SECURITY.md](SECURITY.md).
