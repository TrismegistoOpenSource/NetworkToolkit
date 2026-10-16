// SPDX-License-Identifier: MIT

// Package vpn contiene i toolkit Tailscale e OpenVPN, SOLO per Linux: su macOS
// e Windows i due programmi hanno già le loro interfacce ufficiali, quindi lì
// Service dichiara i toolkit non disponibili e il frontend non li mostra.
//
// Principio: la GUI non reimplementa nulla. Ogni tasto lancia i normali
// comandi da terminale (systemctl, tailscale, curl), e la console mostra la
// riga di comando esatta con il suo output in diretta. Le azioni da
// amministratore passano da pkexec (dialog di polkit), mai da sudo.
//
// Questo file contiene i tipi e i parser, che non dipendono dal sistema
// operativo e si provano ovunque; l'orchestrazione è in service_linux.go.
package vpn

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Nomi dei due toolkit, usati come etichetta delle righe di console.
const (
	ToolkitTailscale = "tailscale"
	ToolkitOpenVPN   = "openvpn"
)

// Stream di una riga di console.
const (
	StreamCmd  = "cmd"  // la riga di comando lanciata
	StreamOut  = "out"  // stdout del comando
	StreamErr  = "err"  // stderr del comando
	StreamInfo = "info" // spiegazioni dell'app (esito, passo successivo)
)

// Line è una riga della console di un toolkit, con l'ora in cui è arrivata.
type Line struct {
	Toolkit string `json:"toolkit"`
	Time    string `json:"time"`
	Stream  string `json:"stream"`
	Text    string `json:"text"`
}

// Emitter riceve le righe di console mentre i comandi girano.
type Emitter func(Line)

// Availability dice al frontend quali toolkit mostrare.
type Availability struct {
	Tailscale bool `json:"tailscale"`
	OpenVPN   bool `json:"openvpn"`
}

// UnitState è lo stato di un'unità systemd come lo riportano
// `systemctl is-active` e `systemctl is-enabled` (stringhe tali e quali:
// active, inactive, failed, activating…; enabled, disabled, static…).
type UnitState struct {
	Unit    string `json:"unit"`
	Active  string `json:"active"`
	Enabled string `json:"enabled"`
}

// Peer è una macchina della rete Tailscale.
type Peer struct {
	Name    string   `json:"name"`
	DNSName string   `json:"dnsName"`
	OS      string   `json:"os"`
	IPs     []string `json:"ips"`
	Online  bool     `json:"online"`
	Self    bool     `json:"self"`
}

// TailscaleState è ciò che il toolkit Tailscale mostra: servizio, connessione
// e macchine della rete.
type TailscaleState struct {
	Supported   bool      `json:"supported"`   // Linux con systemd
	Installed   bool      `json:"installed"`   // tailscale e tailscaled presenti
	HasPkexec   bool      `json:"hasPkexec"`   // serve per le azioni sul servizio
	InstallHint string    `json:"installHint"` // comando per installare ciò che manca
	Message     string    `json:"message"`     // problema da mostrare, se c'è
	Service     UnitState `json:"service"`
	Backend     string    `json:"backend"` // BackendState di tailscale: Running, Stopped, NeedsLogin…
	AuthURL     string    `json:"authUrl"` // pagina di login in attesa, se c'è
	Health      []string  `json:"health"`
	Peers       []Peer    `json:"peers"` // la prima, se c'è, è questa macchina
	UpPending   bool      `json:"upPending"`
}

// Tunnel è una connessione OpenVPN gestita da systemd.
type Tunnel struct {
	Name    string `json:"name"`    // ciò che va dopo la @ dell'unità
	Unit    string `json:"unit"`    // openvpn-client@<nome> oppure openvpn@<nome>
	Config  string `json:"config"`  // file di configurazione, se noto
	Legacy  bool   `json:"legacy"`  // unità openvpn@ con /etc/openvpn/<nome>.conf
	Active  string `json:"active"`  // systemctl is-active
	Enabled string `json:"enabled"` // systemctl is-enabled
}

// OpenVPNState è ciò che il toolkit OpenVPN mostra.
type OpenVPNState struct {
	Supported    bool     `json:"supported"`
	Installed    bool     `json:"installed"`
	HasPkexec    bool     `json:"hasPkexec"`
	InstallHint  string   `json:"installHint"`
	Message      string   `json:"message"`
	Tunnels      []Tunnel `json:"tunnels"`
	Notes        []string `json:"notes"`        // .ovpn da rinominare, nomi non validi…
	Unreadable   bool     `json:"unreadable"`   // una cartella dei tunnel non è leggibile senza root
	SearchedRoot bool     `json:"searchedRoot"` // l'elenco comprende la ricerca con privilegi
}

// ActionResult è l'esito di un tasto. I dettagli sono già passati in console.
type ActionResult struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
	Pending  bool   `json:"pending"` // tailscale up aspetta il login nel browser
	AuthURL  string `json:"authUrl"`
	IPBefore string `json:"ipBefore"`
	IPAfter  string `json:"ipAfter"`
}

// ---------------------------------------------------------------------------
// tailscale status --json

type tsStatusJSON struct {
	BackendState string                `json:"BackendState"`
	AuthURL      string                `json:"AuthURL"`
	Health       []string              `json:"Health"`
	Self         *tsPeerJSON           `json:"Self"`
	Peer         map[string]tsPeerJSON `json:"Peer"`
}

type tsPeerJSON struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	OS           string   `json:"OS"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

// TailscaleStatus è la parte di `tailscale status --json` che serve.
type TailscaleStatus struct {
	Backend string
	AuthURL string
	Health  []string
	Peers   []Peer // questa macchina per prima, poi in linea, poi per nome
}

// ParseTailscaleStatus legge l'output di `tailscale status --json`. Il
// formato è dichiarato "soggetto a cambiare": si leggono solo campi stabili da
// anni e i campi mancanti restano vuoti, invece di far fallire tutto.
func ParseTailscaleStatus(data []byte) (TailscaleStatus, error) {
	var raw tsStatusJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return TailscaleStatus{}, fmt.Errorf("risposta di tailscale illeggibile: %w", err)
	}
	st := TailscaleStatus{
		Backend: raw.BackendState,
		AuthURL: safeURL(raw.AuthURL),
		Health:  raw.Health,
	}
	var others []Peer
	for _, p := range raw.Peer {
		others = append(others, toPeer(p, false))
	}
	sort.Slice(others, func(i, j int) bool {
		if others[i].Online != others[j].Online {
			return others[i].Online
		}
		return strings.ToLower(others[i].Name) < strings.ToLower(others[j].Name)
	})
	// La macchina locale compare solo quando ha un'identità nella rete (dopo
	// il login): prima ha la chiave nulla e nessun IP, e non è una "macchina
	// della rete" da elencare.
	if raw.Self != nil && len(raw.Self.TailscaleIPs) > 0 {
		self := toPeer(*raw.Self, true)
		self.Online = raw.BackendState == "Running"
		st.Peers = append(st.Peers, self)
	}
	st.Peers = append(st.Peers, others...)
	return st, nil
}

func toPeer(p tsPeerJSON, self bool) Peer {
	dns := strings.TrimSuffix(p.DNSName, ".")
	name := p.HostName
	if dns != "" {
		// Il nome breve della rete (MagicDNS) è quello che l'utente usa: è la
		// prima etichetta del DNSName; HostName è il nome che la macchina si dà.
		name = strings.SplitN(dns, ".", 2)[0]
	}
	ips := append([]string(nil), p.TailscaleIPs...)
	// IPv4 prima: è quello che si scrive a mano.
	sort.SliceStable(ips, func(i, j int) bool {
		return !strings.Contains(ips[i], ":") && strings.Contains(ips[j], ":")
	})
	return Peer{Name: name, DNSName: dns, OS: p.OS, IPs: ips, Online: p.Online, Self: self}
}

// ---------------------------------------------------------------------------
// curl https://www.cloudflare.com/cdn-cgi/trace

// TraceURL è l'indirizzo che restituisce l'IP pubblico (riga ip=). È lo stesso
// fornitore dello speedtest: un solo host esterno per tutta l'app.
const TraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

var ipChars = regexp.MustCompile(`^[0-9A-Fa-f.:]{2,45}$`)

// ParseTraceIP estrae l'indirizzo dalla riga ip= della risposta di Cloudflare.
func ParseTraceIP(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "ip="); ok && ipChars.MatchString(v) {
			return v, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// systemctl is-active / is-enabled con più unità

// ParseUnitLines abbina le righe di `systemctl is-active u1 u2…` (una per
// unità, nell'ordine) alle unità chieste. Se il numero di righe non torna, lo
// stato è "sconosciuto" invece che attribuito all'unità sbagliata.
func ParseUnitLines(out string, units []string) map[string]string {
	res := make(map[string]string, len(units))
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	for i, u := range units {
		if len(lines) == len(units) {
			res[u] = lines[i]
		} else {
			res[u] = "unknown"
		}
	}
	return res
}

// ---------------------------------------------------------------------------
// Validazione

// tunnelName: ciò che si può mettere dopo la @ senza escaping di systemd e
// senza che diventi un'opzione di systemctl (niente '-' iniziale). Un file con
// spazi o altri caratteri va rinominato: la console lo dice.
var tunnelName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]{0,99}$`)

// ValidTunnelName dice se un nome di tunnel si può usare così com'è.
func ValidTunnelName(name string) bool { return tunnelName.MatchString(name) }

var urlInText = regexp.MustCompile(`https://[^\s"'<>]+`)

// FindURL restituisce il primo indirizzo https in una riga di output (la
// pagina di login che stampa `tailscale up`).
func FindURL(line string) string {
	return safeURL(urlInText.FindString(line))
}

// SafeURL accetta solo indirizzi https senza spazi né caratteri di
// controllo: è ciò che si apre nel browser con un clic.
func SafeURL(u string) string { return safeURL(u) }

func safeURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), ".,;)")
	if !strings.HasPrefix(u, "https://") || len(u) > 2048 || len(u) <= len("https://") {
		return ""
	}
	for _, r := range u {
		if r <= ' ' || r == 0x7f || r == '"' || r == '\'' || r == '<' || r == '>' || r == '\\' {
			return ""
		}
	}
	return u
}

// QuoteArgs scrive una riga di comando come la si scriverebbe nel terminale:
// è ciò che la console mostra, quindi deve poter essere copiata e incollata.
func QuoteArgs(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
