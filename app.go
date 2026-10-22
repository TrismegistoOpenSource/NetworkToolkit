// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"networktoolkit/internal/changer"
	"networktoolkit/internal/elevate"
	"networktoolkit/internal/macaddr"
	"networktoolkit/internal/netif"
	"networktoolkit/internal/store"
	"networktoolkit/internal/vpn"
)

// Version è la versione dell'app. Va tenuta allineata a wails.json e al tag git.
const Version = "0.3.0"

// App è la struttura legata al frontend da Wails: ogni metodo esportato diventa
// una funzione chiamabile dalla UI.
type App struct {
	ctx context.Context
	st  *store.Store
	// esito dell'autoverifica all'avvio (integrity.go)
	integrity integrityState

	// mu serializza le operazioni privilegiate: due switch in parallelo
	// aprirebbero due dialog di password e si pesterebbero i piedi.
	mu sync.Mutex

	// Stato del toolkit speedtest (processo figlio headless). Separato dal
	// resto: non è privilegiato e non deve condividere il lock di sopra, che
	// resta bloccato per tutta la durata di un dialog di password.
	speedMu   sync.Mutex
	speedSess *speedtestSession
	speedGen  uint64

	// Toolkit Tailscale e OpenVPN: solo Linux (vedi vpn_app.go).
	vpn *vpn.Service
}

// NewApp crea l'app aprendo lo store del MAC di fabbrica. Se lo store non si
// apre (permessi, disco), l'app parte comunque: il ripristino userà solo il MAC
// permanente esposto dall'OS, quando c'è.
func NewApp() *App {
	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store non disponibile:", err)
	}
	a := &App{st: st}
	a.vpn = vpn.New(a.emitVPN)
	return a
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// shutdown: alla chiusura della finestra il motore di misura va spento, o
// resterebbe a generare traffico a vuoto; e si fermano i comandi dei toolkit
// VPN ancora in corso.
func (a *App) shutdown(_ context.Context) {
	a.shutdownSpeedtest()
	a.vpn.Shutdown()
}

// GetVersion espone la versione alla UI (badge in barra laterale).
func (a *App) GetVersion() string { return Version }

// GetCapabilities dice al frontend quali pulsanti abilitare per questo OS.
func (a *App) GetCapabilities() changer.Capabilities { return changer.Caps() }

// UIInterface è ciò che vede il frontend: la scheda più il flag "ripristinabile".
type UIInterface struct {
	netif.Interface
	CanReset bool `json:"canReset"`
}

// OpResult è l'esito di uno switch/ripristino: il log da mostrare in console,
// il dettaglio per scheda e l'elenco aggiornato (così i campi MAC si
// aggiornano in un solo giro).
type OpResult struct {
	OK         bool             `json:"ok"`
	Log        []string         `json:"log"`
	Results    []changer.Result `json:"results"`
	Interfaces []UIInterface    `json:"interfaces"`
}

// ListInterfaces elenca le schede reali con MAC corrente e permanente.
func (a *App) ListInterfaces() ([]UIInterface, error) {
	return a.listUI()
}

// SwitchTemporary genera un nuovo MAC casuale valido per ciascuna scheda scelta
// e lo applica in modo volatile (torna al riavvio). Un click = un MAC nuovo.
func (a *App) SwitchTemporary(ids []string) OpResult {
	return a.switchRandom(ids, false)
}

// SwitchPermanent fa lo stesso, ma in modo che sopravviva al riavvio.
func (a *App) SwitchPermanent(ids []string) OpResult {
	return a.switchRandom(ids, true)
}

// Reset riporta le schede scelte al MAC di fabbrica e rimuove la persistenza.
func (a *App) Reset(ids []string) OpResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	targets, errMsg := a.targetsFor(ids, func(it UIInterface) (string, error) {
		return a.resolvePermanent(it.Interface), nil
	})
	if errMsg != "" {
		return a.opError(errMsg)
	}
	return a.apply(changer.Request{Action: changer.ActionReset, Targets: targets})
}

// targetsFor costruisce i bersagli per le schede scelte; macFor decide il MAC
// che ognuna riceve. Va chiamata con a.mu preso. Restituisce un messaggio
// d'errore non vuoto se non si può procedere.
func (a *App) targetsFor(ids []string, macFor func(UIInterface) (string, error)) ([]changer.Target, string) {
	current, err := a.listUI()
	if err != nil {
		return nil, "elenco schede fallito: " + err.Error()
	}
	byID := indexByID(current)

	var targets []changer.Target
	for _, id := range ids {
		it, ok := byID[id]
		if !ok {
			continue
		}
		mac, err := macFor(it)
		if err != nil {
			return nil, err.Error()
		}
		targets = append(targets, changer.Target{
			ID: it.ID, Name: it.DisplayName, Kind: it.Kind, MAC: mac,
		})
	}
	if len(targets) == 0 {
		return nil, "nessuna scheda valida selezionata"
	}
	return targets, ""
}

func (a *App) switchRandom(ids []string, persistent bool) OpResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	targets, errMsg := a.targetsFor(ids, func(it UIInterface) (string, error) {
		// Fissa il MAC di fabbrica PRIMA di cambiarlo, se non già noto: è
		// l'ancora del ripristino.
		a.snapshotOriginal(it.Interface)

		mac, err := macaddr.GenerateLocalRandom()
		if err != nil {
			return "", fmt.Errorf("generazione MAC fallita: %w", err)
		}
		return mac, nil
	})
	if errMsg != "" {
		return a.opError(errMsg)
	}
	return a.apply(changer.Request{Action: changer.ActionSet, Persistent: persistent, Targets: targets})
}

// apply lancia l'helper elevato e confeziona l'OpResult con l'elenco aggiornato.
func (a *App) apply(req changer.Request) OpResult {
	exe, err := os.Executable()
	if err != nil {
		return a.opError("percorso eseguibile non determinabile: " + err.Error())
	}
	resp, err := elevate.Run(exe, req)
	if err != nil {
		// Annullo della password o elevazione fallita: non è un errore di rete.
		return a.opError(err.Error())
	}

	refreshed, _ := a.listUI()
	ok := true
	for _, r := range resp.Results {
		if !r.OK {
			ok = false
		}
	}
	return OpResult{OK: ok, Log: resp.Log, Results: resp.Results, Interfaces: refreshed}
}

func (a *App) opError(msg string) OpResult {
	refreshed, _ := a.listUI()
	return OpResult{OK: false, Log: []string{"ERRORE: " + msg}, Interfaces: refreshed}
}

// listUI costruisce la lista per il frontend, fondendo il MAC permanente noto
// all'OS con quello ricordato nello store.
func (a *App) listUI() ([]UIInterface, error) {
	list, err := netif.List()
	if err != nil {
		return nil, err
	}
	out := make([]UIInterface, 0, len(list))
	for i := range list {
		it := list[i]
		a.snapshotOriginal(it)

		perm := a.resolvePermanent(it)
		// Mostra il permanente solo se davvero diverso dal corrente.
		if perm != "" && !macEqual(perm, it.CurrentMAC) {
			it.PermanentMAC = perm
		} else {
			it.PermanentMAC = ""
		}

		out = append(out, UIInterface{
			Interface: it,
			CanReset:  perm != "" && !macEqual(perm, it.CurrentMAC),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsActive != out[j].IsActive {
			return out[i].IsActive
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// snapshotOriginal ricorda il MAC corrente come "di fabbrica" quando la scheda
// non è mai stata toccata (MAC non locally-administered) e non ne conosciamo
// già uno. Il primo valore visto è quello buono.
func (a *App) snapshotOriginal(it netif.Interface) {
	if a.st == nil {
		return
	}
	if _, known := a.st.Original(it.ID); known {
		return
	}
	if it.PermanentMAC != "" {
		return // l'OS espone già il permanente: nessun bisogno di indovinare
	}
	if macaddr.IsLocallyAdministered(it.CurrentMAC) {
		return // già alterato: non sappiamo il vero originale
	}
	_ = a.st.RememberOriginal(it.ID, it.DisplayName, it.CurrentMAC)
}

// resolvePermanent restituisce il miglior candidato per il MAC di fabbrica:
// quello esposto dall'OS se c'è, altrimenti quello ricordato nello store.
func (a *App) resolvePermanent(it netif.Interface) string {
	if it.PermanentMAC != "" {
		return it.PermanentMAC
	}
	if a.st != nil {
		if orig, ok := a.st.Original(it.ID); ok {
			return orig
		}
	}
	return ""
}

func indexByID(list []UIInterface) map[string]UIInterface {
	m := make(map[string]UIInterface, len(list))
	for _, it := range list {
		m[it.ID] = it
	}
	return m
}

func macEqual(a, b string) bool {
	na, nb := macaddr.Normalize(a), macaddr.Normalize(b)
	return na != "" && na == nb
}
