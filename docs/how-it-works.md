# Come funziona NetworkToolkit

Documento tecnico per chi lavora sul codice. Riassume le scelte e le trappole
per OS, così non si riscoprono da capo.

## Architettura: GUI non privilegiata + helper elevato

Lo stesso binario ha due modi:

1. **GUI** (default): elenca le schede (operazione **non** privilegiata) e,
   allo switch, costruisce una richiesta e lancia sé stesso in modo helper.
2. **Helper** (`<exe> __apply <richiesta-base64> <file-uscita>`): gira con
   privilegi di amministratore, applica i cambi (`internal/changer`), scrive la
   risposta JSON nel file di uscita ed esce.

`main.go` intercetta il modo helper **prima** di avviare Wails, così l'helper
non apre finestre. La GUI non gira mai da root — obbligatorio su macOS, buona
norma ovunque.

L'elevazione (`internal/elevate`) è **un prompt per switch**:

- **macOS**: `osascript … with administrator privileges` (dialog nativo).
- **Linux**: `pkexec` (dialog polkit). `sudo`/`doas` vogliono un terminale, non
  adatti a una GUI.
- **Windows**: `ShellExecuteExW` con verbo `runas` (prompt UAC), aspettando la
  fine del processo elevato con `WaitForSingleObject`.

La risposta passa da un **file temporaneo** (0644) invece che dallo stdout,
perché su Windows l'elevazione via ShellExecute non cattura lo stdout: un solo
meccanismo per tutti e tre gli OS.

## Generazione del MAC (`internal/macaddr`)

Regole IEEE 802 su cui l'app non transige (puro Go, con test):

- **unicast**: bit meno significativo del primo ottetto = 0;
- **locally administered**: secondo bit meno significativo = 1.

Combinati, il **secondo carattere esadecimale** può valere solo `2/6/A/E`.
È l'unica forma che tutti i driver accettano — Wi-Fi di Windows compreso — ed è
lo stesso tipo di indirizzo privato di iPhone/Android/macOS. Entropia da
`crypto/rand`.

## Elenco schede (`internal/netif`, non privilegiato)

- **macOS**: `networksetup -listallhardwareports` è la fonte autorevole di
  "cos'è una scheda vera" (esclude awdl0, llw0, utunN, bridge0…). La sua
  *Ethernet Address* è il MAC **di fabbrica** letto dalla IORegistry: resta tale
  anche dopo un `ifconfig ether`, quindi è il MAC permanente e il bersaglio del
  ripristino. Le interfacce `feth` (schede virtuali di test) si aggiungono a
  parte.
- **Linux**: `netlink.LinkList()` (puro Go). "Scheda reale" = ha
  `/sys/class/net/<nome>/device`. Il MAC permanente arriva da `PermHWAddr`
  (attributo `IFLA_PERM_ADDRESS`); il Wi-Fi si riconosce da `phy80211`.
- **Windows**: `GetAdaptersAddresses` (IP Helper API). Si tengono solo Ethernet
  (`IF_TYPE_ETHERNET_CSMACD`) e Wi-Fi (`IF_TYPE_IEEE80211`). L'ID è
  l'`AdapterName` (GUID = `NetCfgInstanceId`), la chiave di join col registro.

"Attiva" = interfaccia su + carrier + un indirizzo IP instradabile. È
l'euristica "in uso" che non richiede di leggere la tabella di routing.

## Applicazione dei cambi (`internal/changer`, privilegiato)

- **macOS**: `ifconfig <if> ether <mac>`. Per il Wi-Fi si spegne/riaccende la
  radio attorno al cambio (`networksetup -setairportpower`; `airport -z` è stato
  ritirato in Sonoma 14.4). Persistenza: un **LaunchDaemon** che riapplica al
  boot.
- **Linux**: netlink `LinkSetDown`/`LinkSetHardwareAddr`/`LinkSetUp`. Persistenza
  via **NetworkManager** (`nmcli … cloned-mac-address`) se attivo — altrimenti
  NM sovrascriverebbe alla riconnessione — o un file `.link` di
  **systemd-networkd**.
- **Windows**: si scrive il valore di registro `NetworkAddress` (12 hex, senza
  separatori: con 11 o 13 caratteri Windows lo scarta in silenzio) nella
  sottochiave la cui `NetCfgInstanceId` combacia, poi `Restart-NetAdapter`.
  È **sempre permanente**: non esiste un temporaneo.

## Verifica obbligatoria

Dopo ogni operazione si **rilegge** il MAC (ripassando da `netif`, con qualche
tentativo: dopo un restart/down-up la scheda può sparire per un istante) e lo si
confronta con quello richiesto. Un cambio che non risulta applicato è un
**fallimento dichiarato**, mai un falso successo — è la difesa contro i cambi
ignorati in silenzio (Wi-Fi macOS ancora associato, driver Windows che scarta il
valore).

## Memoria del MAC di fabbrica (`internal/store`)

Serve al ripristino dove l'OS non espone il MAC permanente (feth su macOS,
Windows). Si ricorda **il primo MAC visto** prima di qualunque nostra modifica.
La cartella è redirigibile via `NETWORKTOOLKIT_CONFIG_DIR` (i test puntano
sempre a `t.TempDir()`), con `.bak` prima di ogni riscrittura e salvataggio
atomico — nessun file di configurazione va mai lasciato mezzo scritto.

## Test empirico senza toccare la rete reale

Su macOS si prova su una scheda **virtuale** `feth`: la si crea
(`ifconfig feth create`), le si applica lo switch e il ripristino tramite
l'helper vero, si verifica e la si distrugge. Le schede reali (`en0`, Wi-Fi,
Ethernet in uso) **non vengono mai toccate**. Su Linux l'equivalente è una
scheda `dummy`.

---

# Toolkit velocità

Due livelli **completamente disaccoppiati**: il motore di misura non sa che
esiste una finestra, e la finestra non sa che esiste una rete.

## Livello 1 — motore di misura (headless)

`internal/speedtest/`, nessun privilegio richiesto.

- **Sorgente del traffico** (`traffic.go`): gli endpoint pubblici di
  Cloudflare, `https://speed.cloudflare.com/__down?bytes=N` (GET, restituisce
  esattamente N byte) e `/__up` (POST). Quattro richieste di download e quattro
  di upload in parallelo, ripetute finché il contesto non viene annullato; il
  corpo si scarica e si butta — è il traffico in sé che serve, non il
  contenuto. Il payload di upload è casuale e generato **una volta sola** fuori
  dal ciclo: rigenerarlo a ogni richiesta brucerebbe CPU per nulla.
  - *Perché Cloudflare e non fast.com*: fast.com richiede un token nascosto
    nella pagina, che cambia e va scovato ogni volta, ed è geo-bloccato fuori
    dagli USA in alcune zone. Cloudflare non vuole token, non ha blocchi, e
    l'anycast risponde dal nodo più vicino senza bisogno di una chiamata di
    scoperta separata. È l'unico host contattato.
- **Misura** (`counters_*.go`): la velocità **non** si ottiene contando i byte
  delle richieste HTTP — l'overhead del protocollo falserebbe il numero. Si
  leggono i contatori cumulativi RX/TX che il sistema operativo tiene già per
  la scheda, prima e dopo un intervallo di un secondo:
  - **Linux**: `/proc/net/dev` (puro Go, nessun sottoprocesso).
  - **Windows**: `GetIfEntry2Ex` (IP Helper API), stessa famiglia già usata da
    `internal/netif`.
  - **macOS**: `netstat -ibn`. Non è una scorciatoia: `golang.org/x/net/route`
    espone da `RTM_IFINFO2` solo tipo e MTU (`route.InterfaceMetrics`), **non**
    `Ibytes`/`Obytes`, e parsare a mano gli offset del messaggio grezzo del
    kernel sarebbe più fragile del parsing di un tool stabile da decenni.
  - Conseguenza onesta del metodo: il numero comprende **tutto** il traffico
    che passa dalla scheda, non solo quello del test.
- **Delta** (`speedtest.go`, `deltaSample`): i contatori possono andare
  all'indietro (interfaccia riattivata, overflow a 32 bit su driver virtuali).
  Sottrarre alla cieca darebbe picchi fasulli di decine di Gbps: in quel caso
  il campione vale zero. Coperto da test.

Modo headless: `<exe> __speedtest` stampa una riga al secondo,
`DOWN 123.45 UP 6.78`. Nessuna elevazione, a differenza di `__apply`.

## Livello 2 — quadranti

Il frontend riceve un numero al secondo via evento Wails e lo trasforma in
movimento: arco aperto di 270° disegnato con path SVG, fondo-scala a gradini
che si allarga quando il valore lo supera (e **non** si restringe durante la
misura, o l'ago cambierebbe significato a metà), ago e arco animati dal CSS,
"power-on" scenico all'apertura.

Due dettagli imparati sbagliando:

- Il valore numerico sta nell'**apertura in basso** del quadrante, non al
  centro: al centro l'ago e il mozzo ci passano sopra e tagliano le cifre.
- Il tween del numero ha una rete di sicurezza a `setTimeout`:
  `requestAnimationFrame` viene strozzato o sospeso (finestra in secondo piano,
  cattura schermo) e il valore finale non deve dipendere dal fatto che
  l'animazione arrivi in fondo.

## Il guinzaglio: il motore non sopravvive alla finestra

La GUI lancia il processo figlio passandogli uno **stdin che non scrive mai**.
Finché la GUI è viva quella lettura resta bloccata; nell'istante in cui muore —
anche uccisa di forza, senza poter eseguire alcuna callback — il sistema
operativo chiude la pipe, il figlio legge EOF ed esce. Su Linux si aggiunge
`Pdeathsig` come rinforzo.

Serve perché un motore orfano continuerebbe a generare traffico per sempre. È
la stessa lezione del §13 di `PROBLEMI-NOTI.md`: la correttezza non deve
dipendere da un callback che forse non viene chiamato. **Verificato con
`kill -9` sul processo padre: il figlio muore.**

Per lo stesso motivo, uscendo dal toolkit velocità la misura si ferma: un test
che continua a macinare traffico mentre si guarda un altro strumento sarebbe
traffico nascosto, e quest'app esiste per mostrare cosa passa dalla rete.

---

# Toolkit Tailscale e OpenVPN (solo Linux)

`internal/vpn/`. Su macOS e Windows `service_other.go` dichiara i toolkit non
disponibili (`VPNToolkits` → tutto falso) e il frontend non mostra i menu; i
metodi esistono lo stesso perché i binding Wails sono unici. Le build macOS e
Windows non cambiano comportamento.

## Comandi veri, mostrati in console

`Runner` (runner.go) lancia ogni comando, scrive in console la riga esatta
(quotata come in un terminale, quindi copiabile) e poi stdout e stderr una
riga alla volta mentre arrivano, ognuna con l'ora; alla fine il codice di
uscita. Gli eventi vanno al frontend come `vpn:console`. Le azioni da root
passano da `pkexec <comando>` (126 = finestra chiusa, 127 = autenticazione
fallita: si dicono in chiaro). Le letture di stato per i LED (`systemctl
is-active|is-enabled`, `tailscale status --json`) usano invece `quiet`:
nessuna riga in console, altrimenti ogni 4 secondi la riempirebbero.

Il frontend non può far lanciare comandi arbitrari: le azioni sono una lista
chiusa (`start`, `stop`, `enable`, `disable`, `up`, `down`), e l'unità di un
tunnel deve essere fra quelle rilevate. I nomi dei tunnel accettati sono
quelli che systemd usa senza escaping e che non iniziano con `-`.

## Tailscale

- Servizio: `pkexec systemctl start|stop|enable|disable tailscaled`.
- Connessione: `tailscale up|down` prima **senza** privilegi (basta se
  l'utente è operatore); se tailscaled risponde `Access denied`, lo stesso
  comando con pkexec, spiegando in console come diventare operatore.
- `tailscale up` resta in attesa finché il login nel browser non è finito.
  Il toolkit torna libero appena compare l'indirizzo di login (quello dopo la
  riga "To authenticate", non un qualsiasi indirizzo di documentazione), e il
  comando continua in background. Ha `--timeout=10m` perché quando gira come
  root la GUI non lo può fermare; allo scadere il login non si annulla, lo
  completa tailscaled.
- Stato: `tailscale status --json` (BackendState, AuthURL, Health, Self,
  Peer). Esempi in `internal/vpn/testdata`.

## OpenVPN

- Rilevamento (scan.go): `.conf` in `/etc/openvpn/client` →
  `openvpn-client@`; `.conf` in `/etc/openvpn` → `openvpn@`; collegamenti nelle
  cartelle `*.wants` di systemd (tunnel abilitati all'avvio, leggibili anche
  quando `/etc/openvpn/client` è chiusa, come su Arch). Se una cartella non è
  leggibile si aggiungono le unità che systemd conosce (`systemctl list-units
  --all`) e si offre «Cerca con privilegi» (`pkexec ls -1 -- <cartella>`).
- Accendi/Spegni: curl dell'IP pubblico, `pkexec systemctl start|stop`, curl
  di nuovo. Se l'accensione fallisce, `systemctl status` senza privilegi
  mostra il motivo.

## Guinzaglio dei comandi

Ogni comando senza privilegi ha `Pdeathsig` e un gruppo di processi proprio:
alla chiusura della finestra (`Shutdown`) si uccide il gruppo intero, e se la
GUI muore di colpo il kernel uccide il comando. `WaitDelay` impedisce che un
processo nipote che tiene aperto l'output lasci appesa l'attesa. Provato con
un finto `tailscale` che lancia un figlio: senza il gruppo il figlio
sopravviveva.

## Test

`service_linux_test.go` mette nel PATH (come unico elemento) finti
`systemctl`, `tailscale`, `curl` e `pkexec` scritti al volo in una cartella
temporanea, con lo stato in file di testo: si provano comandi esatti, uscite
annullate, operatore sì/no, login, IP prima e dopo, tunnel non rilevati. La
prova vera con Tailscale e OpenVPN configurati si fa a mano.

# Icona della finestra su Linux

Wails su Linux disegna l'icona della finestra solo da `options.Linux.Icon`.
L'icona di `assets/` è 1024×1024: in `_NET_WM_ICON` sarebbero 4 MB, oltre il
limite di una richiesta X, e la proprietà non viene scritta (icona generica
nella barra). `icon_linux.go` la riduce in memoria a 256 px.
