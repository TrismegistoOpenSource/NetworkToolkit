// SPDX-License-Identifier: MIT

//go:build linux

package vpn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// I toolkit si provano senza servizi veri: finti systemctl, tailscale, curl e
// pkexec scritti in una cartella temporanea, che diventa l'UNICO elemento del
// PATH (così non si arriva mai ai programmi veri). I finti tengono lo stato
// in file di testo e scrivono ogni invocazione in un registro.
//
// Il finto pkexec esegue il comando con FAKE_ROOT=1; i finti systemctl e
// tailscale rifiutano le azioni senza root come quelli veri.

const fakeSystemctl = `#!/bin/sh
S="$FAKE_DIR/stato"
echo "systemctl $* root=${FAKE_ROOT:-0}" >> "$FAKE_DIR/registro"
verb="$1"; shift
case "$verb" in
  is-active|is-enabled)
    for u in "$@"; do
      f="$S/$u.$verb"
      if [ -f "$f" ]; then /bin/cat "$f"; else echo "not-found"; fi
    done ;;
  start|stop|enable|disable)
    if [ "${FAKE_ROOT:-0}" != 1 ]; then echo "Access denied" >&2; exit 4; fi
    u="$1"
    if [ -f "$S/$u.fallisce" ]; then
      echo "Job for $u.service failed because the control process exited with error code." >&2
      exit 1
    fi
    case "$verb" in
      start) echo active > "$S/$u.is-active"; [ -f "$S/$u.ip" ] && /bin/cp "$S/$u.ip" "$S/ip" ;;
      stop) echo inactive > "$S/$u.is-active"; /bin/cp "$S/ip.originale" "$S/ip" ;;
      enable) echo enabled > "$S/$u.is-enabled"; echo "Created symlink $u." >&2 ;;
      disable) echo disabled > "$S/$u.is-enabled"; echo "Removed $u." >&2 ;;
    esac ;;
  status) echo "x $1.service - finto"; echo "   Active: failed"; exit 3 ;;
  list-units) [ -f "$S/list-units" ] && /bin/cat "$S/list-units" ;;
esac
exit 0
`

const fakeTailscale = `#!/bin/sh
echo "tailscale $* root=${FAKE_ROOT:-0}" >> "$FAKE_DIR/registro"
case "$1" in
  status) /bin/cat "$FAKE_DIR/stato/status.json" ;;
  up|down)
    if [ "${FAKE_ROOT:-0}" != 1 ] && [ ! -f "$FAKE_DIR/stato/operatore" ]; then
      echo "Access denied: checkprefs access denied" >&2
      echo "" >&2
      echo "Use 'sudo tailscale $1'." >&2
      exit 1
    fi
    if [ "$1" = up ] && [ -f "$FAKE_DIR/stato/avviso" ]; then
      echo "Warning: see https://tailscale.com/s/avviso for details" >&2
    fi
    if [ "$1" = up ] && [ -f "$FAKE_DIR/stato/login" ]; then
      echo "" >&2
      echo "To authenticate, visit:" >&2
      echo "" >&2
      printf '\thttps://login.tailscale.com/a/abc123\n' >&2
      # il "login" in un processo figlio, come farebbe un programma vero che
      # ne lancia altri: alla chiusura deve morire anche lui
      /bin/sleep "$(/bin/cat "$FAKE_DIR/stato/login")" &
      echo $! > "$FAKE_DIR/stato/figlio.pid"
      wait
      echo "Success." >&2
    fi ;;
esac
exit 0
`

const fakeCurl = `#!/bin/sh
echo "curl $*" >> "$FAKE_DIR/registro"
echo "fl=1"
echo "h=www.cloudflare.com"
printf 'ip=%s\n' "$(/bin/cat "$FAKE_DIR/stato/ip")"
echo "warp=off"
`

const fakePkexec = `#!/bin/sh
echo "pkexec $*" >> "$FAKE_DIR/registro"
if [ -f "$FAKE_DIR/stato/pkexec-esce" ]; then exit "$(/bin/cat "$FAKE_DIR/stato/pkexec-esce")"; fi
FAKE_ROOT=1 exec "$@"
`

type fakeEnv struct {
	dir   string
	state string
	mu    sync.Mutex
	lines []Line
}

func newFake(t *testing.T, withPkexec bool) *fakeEnv {
	t.Helper()
	if !systemdBooted() {
		t.Skip("serve un sistema con systemd")
	}
	f := &fakeEnv{dir: t.TempDir()}
	f.state = filepath.Join(f.dir, "stato")
	bin := filepath.Join(f.dir, "bin")
	for _, d := range []string{f.state, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	progs := map[string]string{"systemctl": fakeSystemctl, "tailscale": fakeTailscale, "curl": fakeCurl, "openvpn": "#!/bin/sh\n"}
	if withPkexec {
		progs["pkexec"] = fakePkexec
	}
	for name, body := range progs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.set(t, "ip", "198.51.100.1")
	f.set(t, "ip.originale", "198.51.100.1")
	t.Setenv("PATH", bin)
	t.Setenv("FAKE_DIR", f.dir)
	return f
}

func (f *fakeEnv) set(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.state, name), []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeEnv) get(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.state, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func (f *fakeEnv) registro(t *testing.T) string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "registro"))
	return string(b)
}

func (f *fakeEnv) emit(l Line) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, l)
}

// console restituisce le righe come "stream: testo".
func (f *fakeEnv) console() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, l := range f.lines {
		b.WriteString(l.Stream + ": " + l.Text + "\n")
	}
	return b.String()
}

func (f *fakeEnv) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var c []string
	for _, l := range f.lines {
		if l.Stream == StreamCmd {
			c = append(c, l.Text)
		}
	}
	return c
}

func (f *fakeEnv) service(t *testing.T, dirs ScanDirs) *Service {
	s := New(f.emit)
	s.dirs = dirs
	t.Cleanup(s.Shutdown)
	return s
}

func copyTestdata(t *testing.T, f *fakeEnv, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	f.set(t, "status.json", string(b))
}

// ---------------------------------------------------------------------------

func TestTailscaleStateCompleto(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "tailscaled.is-active", "active")
	f.set(t, "tailscaled.is-enabled", "enabled")
	copyTestdata(t, f, "tailscale-status-running.json")
	s := f.service(t, ScanDirs{})

	st := s.TailscaleState()
	if !st.Installed || !st.HasPkexec || st.Service.Active != "active" || st.Service.Enabled != "enabled" {
		t.Fatalf("stato: %+v", st)
	}
	if st.Backend != "Running" || len(st.Peers) != 4 || !st.Peers[0].Self {
		t.Errorf("rete: backend %q, %d macchine", st.Backend, len(st.Peers))
	}
	if c := f.console(); c != "" {
		t.Errorf("la lettura dello stato non deve scrivere in console:\n%s", c)
	}
}

func TestTailscaleStateServizioFermo(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "tailscaled.is-active", "inactive")
	f.set(t, "tailscaled.is-enabled", "disabled")
	s := f.service(t, ScanDirs{})
	st := s.TailscaleState()
	if st.Backend != "Stopped" || st.Service.Active != "inactive" {
		t.Errorf("%+v", st)
	}
	if strings.Contains(f.registro(t), "tailscale status") {
		t.Error("con il servizio fermo tailscale status non va chiamato")
	}
}

func TestTailscaleNonInstallato(t *testing.T) {
	f := newFake(t, false)
	os.Remove(filepath.Join(f.dir, "bin", "tailscale"))
	s := f.service(t, ScanDirs{})
	st := s.TailscaleState()
	if st.Installed || !strings.Contains(st.InstallHint, "tailscale.com/install.sh") {
		t.Errorf("non installato: %+v", st)
	}
}

func TestTailscaleServizioConPkexec(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "tailscaled.is-enabled", "disabled")
	s := f.service(t, ScanDirs{})

	if res := s.TailscaleService("enable"); !res.OK {
		t.Fatalf("enable: %+v\n%s", res, f.console())
	}
	if got := f.get(t, "tailscaled.is-enabled"); got != "enabled" {
		t.Errorf("is-enabled dopo enable = %q", got)
	}
	cmds := f.commands()
	if len(cmds) != 1 || cmds[0] != "pkexec systemctl enable tailscaled" {
		t.Errorf("la console deve mostrare il comando esatto: %q", cmds)
	}
	// l'output del comando (stderr di systemctl) arriva in console, con l'ora
	if !strings.Contains(f.console(), "err: Created symlink tailscaled.") {
		t.Errorf("output mancante:\n%s", f.console())
	}
	for _, l := range f.lines {
		if _, err := time.Parse("15:04:05", l.Time); err != nil {
			t.Errorf("ora non valida %q", l.Time)
		}
	}

	if res := s.TailscaleService("rm -rf"); res.OK || !strings.Contains(res.Message, "sconosciuta") {
		t.Errorf("azione non prevista accettata: %+v", res)
	}
}

func TestTailscaleServizioAnnullato(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "pkexec-esce", "126")
	f.set(t, "tailscaled.is-active", "inactive")
	s := f.service(t, ScanDirs{})
	res := s.TailscaleService("start")
	if res.OK || res.Message != "annullato" {
		t.Errorf("%+v", res)
	}
	if f.get(t, "tailscaled.is-active") != "inactive" {
		t.Error("con la password annullata il servizio non deve partire")
	}
	if !strings.Contains(f.console(), "finestra della password chiusa") {
		t.Errorf("console:\n%s", f.console())
	}
}

func TestTailscaleServizioSenzaPkexec(t *testing.T) {
	f := newFake(t, false)
	s := f.service(t, ScanDirs{})
	if res := s.TailscaleService("start"); res.OK || res.Message != "manca pkexec" {
		t.Errorf("%+v", res)
	}
	if strings.Contains(f.registro(t), "systemctl start") {
		t.Error("senza pkexec non si lancia niente")
	}
}

func TestTailscaleDownNonOperatore(t *testing.T) {
	f := newFake(t, true)
	s := f.service(t, ScanDirs{})
	if res := s.TailscaleConnection("down"); !res.OK {
		t.Fatalf("%+v\n%s", res, f.console())
	}
	want := []string{"tailscale down", "pkexec tailscale down"}
	if strings.Join(f.commands(), "|") != strings.Join(want, "|") {
		t.Errorf("prima senza privilegi, poi con pkexec: %q", f.commands())
	}
	if !strings.Contains(f.console(), "--operator=$USER") {
		t.Errorf("manca la spiegazione dell'operatore:\n%s", f.console())
	}
}

func TestTailscaleDownOperatore(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "operatore", "1")
	s := f.service(t, ScanDirs{})
	if res := s.TailscaleConnection("down"); !res.OK {
		t.Fatalf("%+v", res)
	}
	if cmds := f.commands(); len(cmds) != 1 || cmds[0] != "tailscale down" {
		t.Errorf("da operatore niente pkexec: %q", cmds)
	}
}

func TestTailscaleUpConLogin(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "login", "1") // il finto aspetta 1 s il "login" dopo l'indirizzo
	f.set(t, "tailscaled.is-active", "inactive")
	s := f.service(t, ScanDirs{})

	t0 := time.Now()
	res := s.TailscaleConnection("up")
	if !res.OK || !res.Pending || res.AuthURL != "https://login.tailscale.com/a/abc123" {
		t.Fatalf("%+v\n%s", res, f.console())
	}
	if time.Since(t0) > 900*time.Millisecond {
		t.Error("il tasto deve tornare appena compare l'indirizzo, non a login finito")
	}
	if cmds := f.commands(); len(cmds) != 2 || cmds[1] != "pkexec tailscale up --timeout=10m" {
		t.Errorf("comandi: %q", cmds)
	}
	if st := s.TailscaleState(); !st.UpPending {
		t.Error("lo stato deve dire che il login è in attesa")
	}
	// un secondo clic non lancia un altro up
	if res2 := s.TailscaleConnection("up"); !res2.Pending || len(f.commands()) != 2 {
		t.Errorf("secondo up: %+v %q", res2, f.commands())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if p, _ := s.pendingUp(); !p {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("il login in attesa non si chiude")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(f.console(), "Login completato") {
		t.Errorf("console:\n%s", f.console())
	}
}

func TestTailscaleUpAvvisoNonELogin(t *testing.T) {
	// Un indirizzo di documentazione in un avviso non è la pagina di login.
	f := newFake(t, true)
	f.set(t, "operatore", "1")
	f.set(t, "avviso", "1")
	s := f.service(t, ScanDirs{})
	res := s.TailscaleConnection("up")
	if !res.OK || res.Pending || res.AuthURL != "" {
		t.Errorf("%+v", res)
	}
}

func scanFixture(t *testing.T) ScanDirs {
	root := t.TempDir()
	client := filepath.Join(root, "client")
	if err := os.MkdirAll(client, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"ufficio.conf", "casa.conf", "telefono.ovpn"} {
		if err := os.WriteFile(filepath.Join(client, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ScanDirs{Client: client, Legacy: filepath.Join(root, "assente")}
}

func TestOpenVPNStato(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "openvpn-client@ufficio.is-enabled", "enabled")
	f.set(t, "openvpn-client@ufficio.is-active", "active")
	f.set(t, "openvpn-client@casa.is-enabled", "disabled")
	f.set(t, "openvpn-client@casa.is-active", "inactive")
	s := f.service(t, scanFixture(t))

	st := s.OpenVPNState()
	if !st.Installed || len(st.Tunnels) != 2 {
		t.Fatalf("%+v", st)
	}
	casa, uff := st.Tunnels[0], st.Tunnels[1]
	if casa.Name != "casa" || casa.Enabled != "disabled" || uff.Enabled != "enabled" || uff.Active != "active" {
		t.Errorf("tunnel: %+v", st.Tunnels)
	}
	if len(st.Notes) != 1 || !strings.Contains(st.Notes[0], "telefono.conf") {
		t.Errorf("note: %q", st.Notes)
	}
	if c := f.console(); c != "" {
		t.Errorf("lo stato non scrive in console:\n%s", c)
	}
}

func TestOpenVPNAccendiConIP(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "openvpn-client@casa.ip", "203.0.113.50") // IP del server VPN
	s := f.service(t, scanFixture(t))

	res := s.OpenVPNTunnel("openvpn-client@casa", "start")
	if !res.OK || res.IPBefore != "198.51.100.1" || res.IPAfter != "203.0.113.50" {
		t.Fatalf("%+v\n%s", res, f.console())
	}
	want := []string{
		"curl -sS --max-time 8 https://www.cloudflare.com/cdn-cgi/trace",
		"pkexec systemctl start openvpn-client@casa",
		"curl -sS --max-time 8 https://www.cloudflare.com/cdn-cgi/trace",
	}
	if strings.Join(f.commands(), "|") != strings.Join(want, "|") {
		t.Errorf("comandi:\n%q", f.commands())
	}
	if !strings.Contains(f.console(), "prima 198.51.100.1, dopo 203.0.113.50") {
		t.Errorf("console:\n%s", f.console())
	}

	// spegnendo, l'IP torna quello di prima
	res = s.OpenVPNTunnel("openvpn-client@casa", "stop")
	if !res.OK || res.IPAfter != "198.51.100.1" {
		t.Errorf("stop: %+v", res)
	}
}

func TestOpenVPNIPInvariato(t *testing.T) {
	f := newFake(t, true)
	s := f.service(t, scanFixture(t))
	if res := s.OpenVPNTunnel("openvpn-client@ufficio", "start"); !res.OK {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(f.console(), "non è cambiato") {
		t.Errorf("va detto che il traffico non passa dal tunnel:\n%s", f.console())
	}
}

func TestOpenVPNAccensioneFallita(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "openvpn-client@casa.fallisce", "1")
	s := f.service(t, scanFixture(t))
	res := s.OpenVPNTunnel("openvpn-client@casa", "start")
	if res.OK {
		t.Fatal("accensione fallita dichiarata riuscita")
	}
	cmds := f.commands()
	if cmds[len(cmds)-1] != "systemctl status --no-pager --lines=15 openvpn-client@casa" {
		t.Errorf("dopo il fallimento serve systemctl status, senza privilegi: %q", cmds)
	}
}

func TestOpenVPNSoloTunnelRilevati(t *testing.T) {
	f := newFake(t, true)
	s := f.service(t, scanFixture(t))
	for _, unit := range []string{"openvpn-client@altro", "sshd", "openvpn-client@telefono", "--now"} {
		res := s.OpenVPNTunnel(unit, "start")
		if res.OK {
			t.Errorf("%q accettato", unit)
		}
	}
	if strings.Contains(f.registro(t), "systemctl start") {
		t.Errorf("systemctl lanciato su un'unità non rilevata:\n%s", f.registro(t))
	}
	if res := s.OpenVPNTunnel("openvpn-client@casa", "restart; reboot"); res.OK {
		t.Error("azione non prevista accettata")
	}
}

func TestOpenVPNAbilitaAllAvvio(t *testing.T) {
	f := newFake(t, true)
	f.set(t, "openvpn-client@casa.is-enabled", "disabled")
	s := f.service(t, scanFixture(t))
	if res := s.OpenVPNTunnel("openvpn-client@casa", "enable"); !res.OK {
		t.Fatalf("%+v", res)
	}
	if f.get(t, "openvpn-client@casa.is-enabled") != "enabled" {
		t.Error("non abilitato")
	}
	if cmds := f.commands(); len(cmds) != 1 || cmds[0] != "pkexec systemctl enable openvpn-client@casa" {
		t.Errorf("abilitare non richiede curl: %q", cmds)
	}
}

func TestOpenVPNCercaConPrivilegi(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("da root ogni cartella è leggibile")
	}
	f := newFake(t, true)
	// il finto ls risponde come la cartella chiusa vista da root
	ls := "#!/bin/sh\necho \"ls $*\" >> \"$FAKE_DIR/registro\"\nprintf 'segreto.conf\\naltro.ovpn\\n'\n"
	if err := os.WriteFile(filepath.Join(f.dir, "bin", "ls"), []byte(ls), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := scanFixture(t)
	if err := os.Chmod(dirs.Client, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dirs.Client, 0o755) })
	s := f.service(t, dirs)

	if st := s.OpenVPNState(); !st.Unreadable || len(st.Tunnels) != 0 {
		t.Fatalf("prima della ricerca: %+v", st)
	}
	if res := s.OpenVPNSearchPrivileged(); !res.OK {
		t.Fatalf("%+v\n%s", res, f.console())
	}
	if cmds := f.commands(); len(cmds) != 1 || cmds[0] != "pkexec ls -1 -- "+dirs.Client {
		t.Errorf("%q", cmds)
	}
	st := s.OpenVPNState()
	if len(st.Tunnels) != 1 || st.Tunnels[0].Unit != "openvpn-client@segreto" || !st.SearchedRoot {
		t.Errorf("dopo la ricerca: %+v", st)
	}
}

func TestPublicIP(t *testing.T) {
	f := newFake(t, false)
	s := f.service(t, ScanDirs{})
	res := s.PublicIP(ToolkitTailscale)
	if !res.OK || res.IPAfter != "198.51.100.1" {
		t.Errorf("%+v", res)
	}
	// l'output del curl va in console per intero, poi l'IP letto
	c := f.console()
	if !strings.Contains(c, "out: ip=198.51.100.1") || !strings.Contains(c, "info: IP pubblico: 198.51.100.1") {
		t.Errorf("console:\n%s", c)
	}
}

func TestShutdownFermaIComandi(t *testing.T) {
	// Un comando senza privilegi ancora in corso alla chiusura viene fermato.
	f := newFake(t, false)
	f.set(t, "operatore", "1")
	f.set(t, "login", "30")
	s := New(f.emit)
	res := s.TailscaleConnection("up")
	if !res.Pending {
		t.Fatalf("%+v", res)
	}
	// l'indirizzo arriva prima che il finto abbia scritto il pid del figlio
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for {
		b, _ := os.ReadFile(filepath.Join(f.state, "figlio.pid"))
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			pid = n
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("il finto non ha lanciato il figlio")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Shutdown()
	for {
		if p, _ := s.pendingUp(); !p {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tailscale up sopravvive alla chiusura")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// anche il processo lanciato dal comando deve essere morto
	for {
		if processGone(pid) {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("il processo figlio %d sopravvive alla chiusura", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// processGone: il processo non esiste più o è uno zombie in attesa di init.
func processGone(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	f := strings.Fields(string(b))
	return len(f) > 2 && f[2] == "Z"
}

func TestRunnerRigaSenzaACapoEdErrore(t *testing.T) {
	f := newFake(t, false)
	sh := "#!/bin/sh\nprintf 'uno\\ndue senza a capo'\nprintf 'errore\\n' >&2\nexit 3\n"
	if err := os.WriteFile(filepath.Join(f.dir, "bin", "prova"), []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Toolkit: "x", Emit: f.emit}
	o := r.Run(context.Background(), false, "prova", "a b")
	if o.Exit != 3 || o.OK() {
		t.Errorf("uscita: %+v", o)
	}
	c := f.console()
	for _, want := range []string{"cmd: prova 'a b'", "out: uno", "out: due senza a capo", "err: errore", "info: [uscita 3]"} {
		if !strings.Contains(c, want) {
			t.Errorf("manca %q in:\n%s", want, c)
		}
	}
	o = r.Run(context.Background(), false, "nonesiste")
	if o.Err == nil || !strings.Contains(o.Err.Error(), "non è installato") {
		t.Errorf("programma assente: %+v", o)
	}
}
