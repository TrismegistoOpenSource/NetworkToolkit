// SPDX-License-Identifier: MIT

//go:build linux

package vpn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// upTimeout limita l'attesa di `tailscale up` mentre l'utente fa il login nel
// browser. Serve soprattutto quando il comando gira come root (pkexec): un
// processo di root non si può fermare dalla GUI, e senza limite resterebbe in
// attesa per sempre anche dopo la chiusura della finestra. Allo scadere il
// login non si annulla: lo porta a termine tailscaled, e il LED se ne accorge.
const upTimeout = "10m"

// Service esegue i due toolkit. Un'azione alla volta per toolkit: due
// finestre di password aperte insieme si pesterebbero i piedi.
type Service struct {
	emit   Emitter
	ctx    context.Context
	cancel context.CancelFunc

	tsMu sync.Mutex
	ovMu sync.Mutex

	upMu  sync.Mutex
	upJob *Job   // `tailscale up` in attesa del login, se c'è
	upURL string // la sua pagina di login

	rootMu      sync.Mutex
	rootTunnels []Tunnel // trovati con «Cerca con privilegi»

	dirs ScanDirs
}

// New crea il servizio. emit riceve le righe di console.
func New(emit Emitter) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{emit: emit, ctx: ctx, cancel: cancel, dirs: DefaultScanDirs()}
}

// Shutdown ferma i comandi ancora in corso lanciati senza privilegi (quelli
// con pkexec hanno il loro limite di tempo). Va chiamato alla chiusura.
func (s *Service) Shutdown() { s.cancel() }

// Availability: su Linux i due toolkit ci sono sempre. Se manca il programma
// o systemd, il toolkit compare lo stesso, con il LED grigio e il comando per
// installare ciò che manca.
func (s *Service) Availability() Availability { return Availability{Tailscale: true, OpenVPN: true} }

func (s *Service) runner(toolkit string) *Runner { return &Runner{Toolkit: toolkit, Emit: s.emit} }

// systemdBooted è il controllo di sd_booted(3).
func systemdBooted() bool {
	fi, err := os.Stat("/run/systemd/system")
	return err == nil && fi.IsDir()
}

func have(prog string) bool { _, err := exec.LookPath(prog); return err == nil }

// ---------------------------------------------------------------------------
// Tailscale

const tailscaleUnit = "tailscaled"

// TailscaleState legge lo stato con comandi leggeri e senza console.
func (s *Service) TailscaleState() TailscaleState {
	st := TailscaleState{Supported: systemdBooted(), HasPkexec: have("pkexec")}
	st.Service = UnitState{Unit: tailscaleUnit}
	st.UpPending, _ = s.pendingUp()
	if !st.Supported {
		st.Message = "Questo sistema non usa systemd: il toolkit gestisce il servizio con systemctl."
		return st
	}
	st.Service.Active = unitProperty("is-active", tailscaleUnit)
	st.Service.Enabled = unitProperty("is-enabled", tailscaleUnit)
	st.Installed = have("tailscale") && st.Service.Enabled != "not-found"
	if !st.Installed {
		st.InstallHint = "curl -fsSL https://tailscale.com/install.sh | sh"
		st.Message = "Tailscale non è installato. Lo script ufficiale lo installa e attiva il servizio:"
		return st
	}
	if !st.HasPkexec {
		st.InstallHint = pkexecHint()
		st.Message = "Manca pkexec, che apre la finestra della password per avviare e fermare il servizio:"
	}
	if st.Service.Active != "active" {
		st.Backend = "Stopped"
		return st
	}
	out, code, err := quiet(s.ctx, "tailscale", "status", "--json")
	if err != nil || code != 0 {
		// Succede nei secondi in cui tailscaled sta partendo.
		st.Backend = "NoState"
		return st
	}
	ts, err := ParseTailscaleStatus([]byte(out))
	if err != nil {
		st.Message = err.Error()
		return st
	}
	st.Backend = ts.Backend
	st.AuthURL = ts.AuthURL
	st.Health = ts.Health
	st.Peers = ts.Peers
	if st.UpPending && st.AuthURL == "" {
		_, st.AuthURL = s.pendingUp()
	}
	return st
}

// TailscaleService avvia, ferma, abilita o disabilita tailscaled.
func (s *Service) TailscaleService(action string) ActionResult {
	verb, ok := map[string]string{"start": "start", "stop": "stop", "enable": "enable", "disable": "disable"}[action]
	if !ok {
		return ActionResult{Message: "azione sconosciuta: " + action}
	}
	s.tsMu.Lock()
	defer s.tsMu.Unlock()
	r := s.runner(ToolkitTailscale)
	if !have("pkexec") {
		r.Info("manca pkexec: %s", pkexecHint())
		return ActionResult{Message: "manca pkexec"}
	}
	return result(r.Run(s.ctx, true, "systemctl", verb, tailscaleUnit))
}

// TailscaleConnection fa `tailscale up` o `tailscale down`. Prima senza
// privilegi, che basta se l'utente è operatore di Tailscale
// (`tailscale set --operator=$USER`); se tailscaled risponde "Access denied",
// lo stesso comando si ripete con pkexec.
func (s *Service) TailscaleConnection(action string) ActionResult {
	if action != "up" && action != "down" {
		return ActionResult{Message: "azione sconosciuta: " + action}
	}
	s.tsMu.Lock()
	defer s.tsMu.Unlock()
	r := s.runner(ToolkitTailscale)
	if !have("tailscale") {
		r.Info("tailscale non è installato")
		return ActionResult{Message: "tailscale non è installato"}
	}
	if action == "up" {
		if pending, url := s.pendingUp(); pending {
			r.Info("Un «tailscale up» è già in attesa del login: apri la pagina indicata qui sopra.")
			return ActionResult{OK: true, Pending: true, AuthURL: url, Message: "login in attesa"}
		}
		return s.tailscaleUp(r)
	}
	o := r.Run(s.ctx, false, "tailscale", "down")
	if accessDenied(o) {
		explainElevation(r)
		if !have("pkexec") {
			r.Info("manca pkexec: %s", pkexecHint())
			return ActionResult{Message: "manca pkexec"}
		}
		o = r.Run(s.ctx, true, "tailscale", "down")
	}
	return result(o)
}

func (s *Service) tailscaleUp(r *Runner) ActionResult {
	args := []string{"up", "--timeout=" + upTimeout}
	for _, elevated := range []bool{false, true} {
		urlCh := make(chan string, 1)
		// stdout e stderr arrivano da due goroutine: il segnale è atomico.
		var sawAuth atomic.Bool
		onLine := func(_, text string) {
			// L'indirizzo di login segue la riga "To authenticate, visit:".
			// Altri indirizzi (pagine di documentazione negli avvisi) non
			// vanno presi per la pagina di login.
			if strings.Contains(strings.ToLower(text), "authenticate") {
				sawAuth.Store(true)
			}
			if u := FindURL(text); u != "" && sawAuth.Load() {
				select {
				case urlCh <- u:
				default:
				}
			}
		}
		job := r.Start(s.ctx, elevated, onLine, "tailscale", args...)
		select {
		case <-job.Done():
			o := job.Outcome()
			if !elevated && accessDenied(o) {
				explainElevation(r)
				if !have("pkexec") {
					r.Info("manca pkexec: %s", pkexecHint())
					return ActionResult{Message: "manca pkexec"}
				}
				continue
			}
			return result(o)
		case u := <-urlCh:
			s.setPendingUp(job, u, r)
			r.Info("Serve il login: apri l'indirizzo qui sopra (clic). Il comando aspetta fino a %s; il LED della connessione diventa verde appena il login è completo.", upTimeout)
			return ActionResult{OK: true, Pending: true, AuthURL: u, Message: "login richiesto"}
		}
	}
	return ActionResult{Message: "tailscale up non riuscito"}
}

func (s *Service) pendingUp() (bool, string) {
	s.upMu.Lock()
	defer s.upMu.Unlock()
	return s.upJob != nil, s.upURL
}

// setPendingUp tiene traccia del `tailscale up` lasciato in attesa del login:
// quando finisce, l'esito va in console e il toolkit torna libero.
func (s *Service) setPendingUp(job *Job, url string, r *Runner) {
	s.upMu.Lock()
	s.upJob, s.upURL = job, url
	s.upMu.Unlock()
	go func() {
		o := job.Outcome()
		s.upMu.Lock()
		s.upJob, s.upURL = nil, ""
		s.upMu.Unlock()
		if o.OK() {
			r.Info("Login completato: Tailscale è connesso.")
		} else if s.ctx.Err() == nil {
			r.Info("«tailscale up» ha smesso di aspettare. Se il login nel browser si completa dopo, la connessione parte lo stesso: guarda il LED.")
		}
	}()
}

// accessDenied riconosce il rifiuto di tailscaled a un utente che non è
// operatore ("Access denied: checkprefs access denied", "prefs write access
// denied").
func accessDenied(o Outcome) bool {
	return o.Err == nil && o.Exit != 0 && strings.Contains(o.Stderr+o.Stdout, "Access denied")
}

func explainElevation(r *Runner) {
	r.Info("tailscaled accetta il comando solo da root o dall'operatore: lo ripeto con pkexec. (Per non dover dare la password ogni volta, una volta sola nel terminale: sudo tailscale set --operator=$USER)")
}

// ---------------------------------------------------------------------------
// OpenVPN

// OpenVPNState elenca i tunnel con il loro stato.
func (s *Service) OpenVPNState() OpenVPNState {
	st := OpenVPNState{Supported: systemdBooted(), Installed: have("openvpn"), HasPkexec: have("pkexec")}
	if !st.Supported {
		st.Message = "Questo sistema non usa systemd: i tunnel si gestiscono come unità openvpn-client@<nome>."
		return st
	}
	if !st.Installed {
		st.InstallHint = openvpnHint()
		st.Message = "OpenVPN non è installato:"
		return st
	}
	if !st.HasPkexec {
		st.InstallHint = pkexecHint()
		st.Message = "Manca pkexec, che apre la finestra della password per accendere e spegnere i tunnel:"
	}
	tunnels, notes, unreadable := s.tunnels()
	st.Notes = notes
	st.Unreadable = unreadable
	s.rootMu.Lock()
	st.SearchedRoot = s.rootTunnels != nil
	s.rootMu.Unlock()

	if len(tunnels) > 0 {
		units := make([]string, len(tunnels))
		for i, t := range tunnels {
			units[i] = t.Unit
		}
		active := unitProperties("is-active", units)
		enabled := unitProperties("is-enabled", units)
		for i := range tunnels {
			tunnels[i].Active = active[tunnels[i].Unit]
			tunnels[i].Enabled = enabled[tunnels[i].Unit]
		}
	}
	st.Tunnels = tunnels
	return st
}

// tunnels unisce ciò che si legge dalle cartelle, ciò che systemd conosce già
// e ciò che ha trovato la ricerca con privilegi.
func (s *Service) tunnels() ([]Tunnel, []string, bool) {
	sc := ScanTunnels(s.dirs)
	seen := map[string]bool{}
	for _, t := range sc.Tunnels {
		seen[t.Unit] = true
	}
	extra := func(list []Tunnel) {
		for _, t := range list {
			if !seen[t.Unit] {
				seen[t.Unit] = true
				sc.Tunnels = append(sc.Tunnels, t)
			}
		}
	}
	if sc.Unreadable {
		// Solo se una cartella è chiusa: altrimenti un'unità rimasta caricata
		// dopo aver cancellato il suo .conf comparirebbe come tunnel valido.
		if out, code, err := quiet(s.ctx, "systemctl", "list-units", "--all", "--plain", "--no-legend",
			"--type=service", "openvpn-client@*", "openvpn@*"); err == nil && code == 0 {
			extra(ParseListUnits(out))
		}
	}
	s.rootMu.Lock()
	extra(s.rootTunnels)
	s.rootMu.Unlock()
	return sc.Tunnels, sc.Notes, sc.Unreadable
}

// OpenVPNTunnel accende, spegne, abilita o disabilita il tunnel indicato.
// L'unità deve essere fra quelle rilevate: il frontend non può far lanciare
// systemctl su un'unità qualsiasi.
func (s *Service) OpenVPNTunnel(unit, action string) ActionResult {
	verb, ok := map[string]string{"start": "start", "stop": "stop", "enable": "enable", "disable": "disable"}[action]
	if !ok {
		return ActionResult{Message: "azione sconosciuta: " + action}
	}
	s.ovMu.Lock()
	defer s.ovMu.Unlock()
	r := s.runner(ToolkitOpenVPN)

	tunnels, _, _ := s.tunnels()
	var t *Tunnel
	for i := range tunnels {
		if tunnels[i].Unit == unit {
			t = &tunnels[i]
		}
	}
	if t == nil {
		r.Info("tunnel %q non trovato: premi Aggiorna", unit)
		return ActionResult{Message: "tunnel non trovato"}
	}
	if !have("pkexec") {
		r.Info("manca pkexec: %s", pkexecHint())
		return ActionResult{Message: "manca pkexec"}
	}

	if verb == "enable" || verb == "disable" {
		return result(r.Run(s.ctx, true, "systemctl", verb, t.Unit))
	}

	// Accendi/Spegni: IP pubblico prima e dopo, per vedere se il traffico
	// passa davvero dal tunnel.
	before, _ := s.publicIP(r)
	o := r.Run(s.ctx, true, "systemctl", verb, t.Unit)
	res := result(o)
	res.IPBefore = before
	if !o.OK() {
		if !o.Canceled && !o.AuthFailed && o.Err == nil {
			// systemctl dice solo "Job failed": il motivo vero è nel registro
			// dell'unità, che `systemctl status` mostra senza privilegi (le
			// righe del journal solo se l'utente è nel gruppo adm o
			// systemd-journal).
			r.Run(s.ctx, false, "systemctl", "status", "--no-pager", "--lines=15", t.Unit)
		}
		return res
	}
	after, _ := s.publicIP(r)
	res.IPAfter = after
	switch {
	case before == "" || after == "":
	case verb == "start" && before == after:
		r.Info("ATTENZIONE: l'IP pubblico non è cambiato (%s). Il tunnel è acceso ma il traffico verso Internet non passa da lì: il server potrebbe non inviare redirect-gateway, e va bene se usi il tunnel solo per una rete interna.", after)
	case before != after:
		r.Info("IP pubblico: prima %s, dopo %s.", before, after)
	default:
		r.Info("IP pubblico invariato: %s.", after)
	}
	return res
}

// OpenVPNSearchPrivileged elenca con pkexec le cartelle che l'utente non può
// leggere. Il risultato resta in memoria fino alla chiusura dell'app.
func (s *Service) OpenVPNSearchPrivileged() ActionResult {
	s.ovMu.Lock()
	defer s.ovMu.Unlock()
	r := s.runner(ToolkitOpenVPN)
	if !have("pkexec") {
		r.Info("manca pkexec: %s", pkexecHint())
		return ActionResult{Message: "manca pkexec"}
	}
	var found []Tunnel
	for _, d := range []struct {
		dir, prefix string
		legacy      bool
	}{
		{s.dirs.Client, "openvpn-client@", false},
		{s.dirs.Legacy, "openvpn@", true},
	} {
		if _, err := os.ReadDir(d.dir); err == nil || os.IsNotExist(err) {
			continue // leggibile (già elencata) o assente
		}
		o := r.Run(s.ctx, true, "ls", "-1", "--", d.dir)
		if !o.OK() {
			return result(o)
		}
		found = append(found, ParseRootListing(o.Stdout, d.dir, d.prefix, d.legacy)...)
	}
	s.rootMu.Lock()
	s.rootTunnels = append([]Tunnel{}, found...)
	s.rootMu.Unlock()
	r.Info("Tunnel trovati con privilegi: %d.", len(found))
	return ActionResult{OK: true, Message: fmt.Sprintf("%d tunnel trovati", len(found))}
}

// ---------------------------------------------------------------------------
// IP pubblico

// PublicIP mostra in console l'IP pubblico, con il curl che lo legge.
func (s *Service) PublicIP(toolkit string) ActionResult {
	if toolkit != ToolkitTailscale && toolkit != ToolkitOpenVPN {
		toolkit = ToolkitOpenVPN
	}
	ip, err := s.publicIP(s.runner(toolkit))
	if err != nil {
		return ActionResult{Message: err.Error()}
	}
	return ActionResult{OK: true, IPAfter: ip, Message: ip}
}

func (s *Service) publicIP(r *Runner) (string, error) {
	o := r.Run(s.ctx, false, "curl", "-sS", "--max-time", "8", TraceURL)
	if !o.OK() {
		return "", fmt.Errorf("IP pubblico non letto")
	}
	ip, ok := ParseTraceIP(o.Stdout)
	if !ok {
		r.Info("risposta senza la riga ip=")
		return "", fmt.Errorf("risposta senza IP")
	}
	r.Info("IP pubblico: %s", ip)
	return ip, nil
}

// ---------------------------------------------------------------------------
// systemctl e suggerimenti di installazione

// unitProperty restituisce la risposta di `systemctl is-active|is-enabled`.
// Il codice di uscita non serve: la parola stampata dice già tutto.
func unitProperty(verb, unit string) string {
	return unitProperties(verb, []string{unit})[unit]
}

func unitProperties(verb string, units []string) map[string]string {
	args := append([]string{verb}, units...)
	out, _, err := quiet(context.Background(), "systemctl", args...)
	if err != nil {
		res := map[string]string{}
		for _, u := range units {
			res[u] = "unknown"
		}
		return res
	}
	return ParseUnitLines(out, units)
}

// osFamily legge ID e ID_LIKE da /etc/os-release.
func osFamily() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	var id, like string
	for _, l := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(l, "ID="); ok {
			id = strings.Trim(v, `"`)
		}
		if v, ok := strings.CutPrefix(l, "ID_LIKE="); ok {
			like = strings.Trim(v, `"`)
		}
	}
	return id + " " + like
}

func installCmd(debian, arch, fedora, suse string) string {
	fam := " " + osFamily() + " "
	switch {
	case strings.Contains(fam, " debian ") || strings.Contains(fam, " ubuntu "):
		return debian
	case strings.Contains(fam, " arch "):
		return arch
	case strings.Contains(fam, " fedora ") || strings.Contains(fam, " rhel "):
		return fedora
	case strings.Contains(fam, " suse ") || strings.Contains(fam, "opensuse"):
		return suse
	}
	return debian
}

func openvpnHint() string {
	return installCmd("sudo apt install openvpn", "sudo pacman -S openvpn",
		"sudo dnf install openvpn", "sudo zypper install openvpn")
}

func pkexecHint() string {
	return installCmd("sudo apt install pkexec", "sudo pacman -S polkit",
		"sudo dnf install polkit", "sudo zypper install polkit")
}

// result traduce l'esito di un comando nel messaggio breve del tasto.
func result(o Outcome) ActionResult {
	switch {
	case o.OK():
		return ActionResult{OK: true, Message: "fatto"}
	case o.Canceled:
		return ActionResult{Message: "annullato"}
	case o.AuthFailed:
		return ActionResult{Message: "autenticazione fallita"}
	case o.Err != nil:
		return ActionResult{Message: o.Err.Error()}
	default:
		return ActionResult{Message: fmt.Sprintf("il comando è uscito con codice %d: vedi la console", o.Exit)}
	}
}

// quiet lancia un comando di sola lettura SENZA console: è ciò che fanno i LED
// ogni pochi secondi, e riempire la console con quelle righe la renderebbe
// inutile. Restituisce stdout, il codice di uscita e l'errore di avvio.
func quiet(ctx context.Context, name string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	configureChild(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), ee.ExitCode(), nil
		}
		return string(out), -1, startError(name, err)
	}
	return string(out), 0, nil
}
