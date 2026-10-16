// SPDX-License-Identifier: MIT

// Package changer applica i cambi di MAC. È la parte PRIVILEGIATA: gira dentro
// l'helper elevato (un solo prompt di password per clic), mai nella GUI, che
// su macOS non può e non deve girare da root.
//
// L'orchestrazione (ciclo sui target, verifica post-applicazione, raccolta del
// log) è qui e condivisa; ogni OS fornisce solo platformSet e platformReset.
// La rilettura del MAC per la verifica è cross-platform e passa da
// internal/netif, così non si duplica codice di lettura per sistema.
package changer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"networktoolkit/internal/macaddr"
	"networktoolkit/internal/netif"
)

// Errori tipizzati: distinguono le cause di fallimento perché la GUI mostri un
// messaggio utile invece di un generico "non ha funzionato".
var (
	ErrNotSupported   = errors.New("operazione non supportata da questa scheda o driver")
	ErrPermission     = errors.New("permessi insufficienti (servono privilegi di amministratore)")
	ErrDriverRejected = errors.New("il driver ha rifiutato il MAC")
	ErrVerifyFailed   = errors.New("il MAC non risulta cambiato dopo l'applicazione")
)

const (
	ActionSet   = "set"   // applica Target.MAC
	ActionReset = "reset" // torna al MAC di fabbrica e rimuove la persistenza
)

// Target è una scheda da modificare.
type Target struct {
	ID   string `json:"id"`   // identificatore stabile (nome BSD/kernel o GUID)
	Name string `json:"name"` // nome leggibile, solo per i messaggi di log
	Kind string `json:"kind"` // netif.KindWiFi o netif.KindEthernet
	// MAC è il nuovo indirizzo per ActionSet; per ActionReset è il MAC di
	// fabbrica atteso (per la verifica e, dove serve, per riscriverlo).
	MAC string `json:"mac"`
}

// Request è ciò che la GUI passa all'helper (serializzato in JSON base64).
type Request struct {
	Action     string   `json:"action"`
	Persistent bool     `json:"persistent"` // solo per ActionSet
	Targets    []Target `json:"targets"`
}

// Result è l'esito per una singola scheda.
type Result struct {
	ID         string `json:"id"`
	OK         bool   `json:"ok"`
	CurrentMAC string `json:"currentMac"` // sempre rilette, così la GUI aggiorna il campo
	Message    string `json:"message"`
}

// Response è ciò che l'helper stampa su stdout.
type Response struct {
	Results []Result `json:"results"`
	Log     []string `json:"log"`
}

// Capabilities dice alla GUI quali pulsanti abilitare per l'OS corrente.
type Capabilities struct {
	OS           string `json:"os"`
	CanTemporary bool   `json:"canTemporary"` // cambio volatile, perso al riavvio
	CanPermanent bool   `json:"canPermanent"` // cambio che sopravvive al riavvio
}

type logbuf struct{ lines []string }

func (l *logbuf) add(format string, a ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, a...))
}

// Apply esegue la richiesta e restituisce l'esito per ogni scheda.
func Apply(req Request) Response {
	log := &logbuf{}
	var resp Response

	for _, t := range req.Targets {
		// L'helper gira come root e la richiesta arriva da riga di comando:
		// si valida tutto prima di toccare il sistema. Un ID con un a capo
		// finirebbe, per esempio, dentro il file .link di systemd-networkd.
		if err := validateTarget(req.Action, t); err != nil {
			log.add("[%s] RIFIUTATO: %v", sanitizeForLog(t.Name), err)
			resp.Results = append(resp.Results, Result{ID: t.ID, OK: false, Message: err.Error()})
			continue
		}
		var opErr error
		switch req.Action {
		case ActionSet:
			log.add("[%s] applico %s%s", t.Name, t.MAC, persistSuffix(req.Persistent))
			opErr = platformSet(t, req.Persistent, log)
		case ActionReset:
			log.add("[%s] ripristino il MAC di fabbrica", t.Name)
			opErr = platformReset(t, log)
		default:
			opErr = fmt.Errorf("azione sconosciuta %q", req.Action)
		}

		res := Result{ID: t.ID}
		cur, readErr := readCurrentMAC(t.ID)
		res.CurrentMAC = cur

		switch {
		case opErr != nil:
			res.OK = false
			res.Message = opErr.Error()
			log.add("[%s] ERRORE: %v", t.Name, opErr)
		case readErr != nil:
			res.OK = false
			res.Message = "cambio inviato ma rilettura del MAC fallita: " + readErr.Error()
			log.add("[%s] %s", t.Name, res.Message)
		case req.Action == ActionSet && !macEqual(cur, t.MAC):
			res.OK = false
			res.Message = fmt.Sprintf("%v: atteso %s, letto %s", ErrVerifyFailed, t.MAC, cur)
			log.add("[%s] VERIFICA FALLITA: %s", t.Name, res.Message)
		default:
			res.OK = true
			res.Message = "ok"
			log.add("[%s] OK — MAC attuale %s", t.Name, cur)
		}
		resp.Results = append(resp.Results, res)
	}

	resp.Log = log.lines
	return resp
}

// idPattern: nomi di interfaccia (en0, wlp0s20f3, enx00e04c68…) e GUID di
// Windows ({4D36E972-…}). Niente spazi, a capo, barre o caratteri di shell.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9{}][A-Za-z0-9{}._:@-]{0,63}$`)

// ErrInvalidRequest: la richiesta all'helper non è ben formata.
var ErrInvalidRequest = errors.New("richiesta non valida")

func validateTarget(action string, t Target) error {
	if action != ActionSet && action != ActionReset {
		return fmt.Errorf("%w: azione sconosciuta %q", ErrInvalidRequest, action)
	}
	if !idPattern.MatchString(t.ID) {
		return fmt.Errorf("%w: identificatore di scheda %q", ErrInvalidRequest, t.ID)
	}
	if t.Kind != netif.KindWiFi && t.Kind != netif.KindEthernet {
		return fmt.Errorf("%w: tipo di scheda %q", ErrInvalidRequest, t.Kind)
	}
	// Per il ripristino il MAC può mancare (si legge quello permanente), ma
	// se c'è deve essere un MAC.
	switch {
	case action == ActionSet:
		if _, err := macaddr.Parse(t.MAC); err != nil {
			return err
		}
	case t.MAC != "" && macaddr.Normalize(t.MAC) == "":
		return fmt.Errorf("%w: MAC di fabbrica %q", ErrInvalidRequest, t.MAC)
	}
	return nil
}

// sanitizeForLog toglie dal nome i caratteri di controllo prima di scriverlo.
func sanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func persistSuffix(p bool) string {
	if p {
		return " (permanente)"
	}
	return " (temporaneo)"
}

func macEqual(a, b string) bool {
	na, nb := macaddr.Normalize(a), macaddr.Normalize(b)
	return na != "" && na == nb
}

// readCurrentMAC rilegge il MAC corrente di una scheda ripassando da netif, con
// qualche tentativo: subito dopo un restart dell'adattatore (Windows) o un
// down/up (Linux) la scheda può sparire per un istante dalla lista.
func readCurrentMAC(id string) (string, error) {
	var lastErr error
	for i := 0; i < 10; i++ {
		list, err := netif.List()
		if err != nil {
			lastErr = err
		} else {
			for _, it := range list {
				if strings.EqualFold(it.ID, id) {
					return it.CurrentMAC, nil
				}
			}
			lastErr = fmt.Errorf("scheda %q non più presente nell'elenco", id)
		}
		time.Sleep(400 * time.Millisecond)
	}
	return "", lastErr
}
