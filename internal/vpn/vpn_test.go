// SPDX-License-Identifier: MIT

package vpn

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseTailscaleStatusNeedsLogin(t *testing.T) {
	// Uscita vera di tailscale 1.102.5 senza login (testdata/LEGGIMI.txt).
	data, err := os.ReadFile("testdata/tailscale-status-needslogin.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := ParseTailscaleStatus(data)
	if err != nil {
		t.Fatal(err)
	}
	if st.Backend != "NeedsLogin" {
		t.Errorf("Backend = %q, atteso NeedsLogin", st.Backend)
	}
	if len(st.Peers) != 0 {
		t.Errorf("senza login non ci sono macchine da elencare, trovate %d: %+v", len(st.Peers), st.Peers)
	}
	if len(st.Health) == 0 {
		t.Errorf("gli avvisi di salute vanno letti")
	}
}

func TestParseTailscaleStatusRunning(t *testing.T) {
	data, err := os.ReadFile("testdata/tailscale-status-running.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := ParseTailscaleStatus(data)
	if err != nil {
		t.Fatal(err)
	}
	if st.Backend != "Running" {
		t.Fatalf("Backend = %q", st.Backend)
	}
	var got []string
	for _, p := range st.Peers {
		on := "off"
		if p.Online {
			on = "on"
		}
		got = append(got, p.Name+"/"+p.IPs[0]+"/"+on)
	}
	// questa macchina prima, poi le macchine in linea per nome, poi le altre;
	// IPv4 per primo anche se il JSON mette prima l'IPv6
	want := "host-a/100.101.102.10/on host-b/100.101.102.40/on host-c/100.101.102.20/on host-d/100.101.102.30/off"
	if strings.Join(got, " ") != want {
		t.Errorf("macchine:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if !st.Peers[0].Self || st.Peers[1].Self {
		t.Errorf("solo la prima è questa macchina: %+v", st.Peers[:2])
	}
	if st.Peers[1].DNSName != "host-b.tail0000.ts.net" {
		t.Errorf("DNSName senza il punto finale: %q", st.Peers[1].DNSName)
	}
}

func TestParseTailscaleStatusErrori(t *testing.T) {
	if _, err := ParseTailscaleStatus([]byte("failed to connect to local tailscaled")); err == nil {
		t.Error("testo non JSON accettato")
	}
	// campi mancanti: niente errore, campi vuoti
	st, err := ParseTailscaleStatus([]byte(`{"BackendState":"Stopped","Peer":{"k":{"HostName":"x"}}}`))
	if err != nil || st.Backend != "Stopped" || len(st.Peers) != 1 || st.Peers[0].Name != "x" {
		t.Errorf("JSON ridotto: %+v %v", st, err)
	}
	// un AuthURL che non è https non si apre con un clic
	st, _ = ParseTailscaleStatus([]byte(`{"AuthURL":"javascript:alert(1)"}`))
	if st.AuthURL != "" {
		t.Errorf("AuthURL non sicuro accettato: %q", st.AuthURL)
	}
}

func TestParseTraceIP(t *testing.T) {
	out := "fl=126f112\nh=www.cloudflare.com\nip=203.0.113.7\nts=1791396200.000\nvisit_scheme=https\n"
	if ip, ok := ParseTraceIP(out); !ok || ip != "203.0.113.7" {
		t.Errorf("IPv4: %q %v", ip, ok)
	}
	if ip, ok := ParseTraceIP("h=x\r\nip=2001:db8::1\r\n"); !ok || ip != "2001:db8::1" {
		t.Errorf("IPv6 con CRLF: %q %v", ip, ok)
	}
	for _, bad := range []string{"", "h=x\n", "ip=<script>\n", "ip=\n", "visit_ip=1.2.3.4\n"} {
		if ip, ok := ParseTraceIP(bad); ok {
			t.Errorf("%q: accettato %q", bad, ip)
		}
	}
}

func TestParseUnitLines(t *testing.T) {
	units := []string{"a", "b", "c"}
	m := ParseUnitLines("active\ninactive\nfailed\n", units)
	if m["a"] != "active" || m["b"] != "inactive" || m["c"] != "failed" {
		t.Errorf("%v", m)
	}
	// righe che non tornano: nessuno stato attribuito all'unità sbagliata
	m = ParseUnitLines("active\n", units)
	for _, u := range units {
		if m[u] != "unknown" {
			t.Errorf("%s = %q, atteso unknown", u, m[u])
		}
	}
}

func TestValidTunnelName(t *testing.T) {
	for _, ok := range []string{"ufficio", "casa-2", "vpn.example.com", "a_b", "X1:udp"} {
		if !ValidTunnelName(ok) {
			t.Errorf("%q rifiutato", ok)
		}
	}
	for _, bad := range []string{"", "-h", "--now", "con spazio", "caffè", "a/b", "../x", "a\nb", "a;rm", "$(x)", ".nascosto", strings.Repeat("a", 101)} {
		if ValidTunnelName(bad) {
			t.Errorf("%q accettato", bad)
		}
	}
}

func TestFindURL(t *testing.T) {
	if u := FindURL("\thttps://login.tailscale.com/a/1a2b3c4d"); u != "https://login.tailscale.com/a/1a2b3c4d" {
		t.Errorf("%q", u)
	}
	if u := FindURL("vedi (https://tailscale.com/s/dns)."); u != "https://tailscale.com/s/dns" {
		t.Errorf("punteggiatura finale: %q", u)
	}
	for _, bad := range []string{"http://login.tailscale.com/a/x", "nessun indirizzo", "https://"} {
		if u := FindURL(bad); u != "" {
			t.Errorf("%q: %q", bad, u)
		}
	}
	if SafeURL("https://a.b/x\"onclick=") != "" || SafeURL("https://a b") != "" {
		t.Error("SafeURL accetta caratteri pericolosi")
	}
}

func TestQuoteArgs(t *testing.T) {
	got := QuoteArgs([]string{"pkexec", "systemctl", "start", "openvpn-client@casa"})
	if got != "pkexec systemctl start openvpn-client@casa" {
		t.Errorf("%q", got)
	}
	if got := QuoteArgs([]string{"mv", "/x/a b.ovpn", "it's"}); got != `mv '/x/a b.ovpn' 'it'\''s'` {
		t.Errorf("%q", got)
	}
}

func TestScanTunnels(t *testing.T) {
	root := t.TempDir()
	client := filepath.Join(root, "client")
	legacy := root
	wants := filepath.Join(root, "multi-user.target.wants")
	for _, d := range []string{client, wants} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"client/ufficio.conf", "client/casa.conf", "client/arch.ovpn",
		"client/con spazio.conf", "client/note.txt",
		"vecchio.conf", "update-resolv-conf",
	} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("client\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// un tunnel abilitato all'avvio e uno di un'altra cartella
	for _, l := range []string{"openvpn-client@ufficio.service", "openvpn-client@altrove.service", "cups.service"} {
		// sul sistema sono link a /lib/systemd/...: conta solo il nome (file
		// vuoti e non symlink, che su Windows richiedono privilegi)
		if err := os.WriteFile(filepath.Join(wants, l), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := ScanTunnels(ScanDirs{Client: client, Legacy: legacy, Wants: []string{wants}})
	var units []string
	for _, tn := range r.Tunnels {
		units = append(units, tn.Unit)
	}
	want := "openvpn-client@altrove openvpn-client@casa openvpn-client@ufficio openvpn@vecchio"
	if strings.Join(units, " ") != want {
		t.Errorf("tunnel:\n got %s\nwant %s", strings.Join(units, " "), want)
	}
	notes := strings.Join(r.Notes, "\n")
	if !strings.Contains(notes, "arch.ovpn") || !strings.Contains(notes, "arch.conf") {
		t.Errorf("manca la nota sul .ovpn da rinominare:\n%s", notes)
	}
	if !strings.Contains(notes, "con spazio.conf") {
		t.Errorf("manca la nota sul nome con spazio:\n%s", notes)
	}
	if r.Unreadable {
		t.Error("cartelle leggibili segnate come chiuse")
	}

	// cartella assente: nessun errore, nessuna nota
	r = ScanTunnels(ScanDirs{Client: filepath.Join(root, "manca"), Legacy: filepath.Join(root, "manca2")})
	if len(r.Tunnels) != 0 || len(r.Notes) != 0 || r.Unreadable {
		t.Errorf("cartelle assenti: %+v", r)
	}
}

func TestScanTunnelsCartellaChiusa(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("servono i permessi Unix e un utente non root")
	}
	root := t.TempDir()
	client := filepath.Join(root, "client")
	wants := filepath.Join(root, "w")
	if err := os.MkdirAll(client, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wants, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client, "segreto.conf"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wants, "openvpn-client@segreto.service"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(client, 0o700&^0o400); err != nil { // come Arch: non leggibile
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(client, 0o755) })

	r := ScanTunnels(ScanDirs{Client: client, Wants: []string{wants}})
	if !r.Unreadable {
		t.Fatal("cartella non leggibile non segnalata")
	}
	if len(r.Tunnels) != 1 || r.Tunnels[0].Unit != "openvpn-client@segreto" {
		t.Errorf("il tunnel abilitato all'avvio va trovato lo stesso: %+v", r.Tunnels)
	}
}

func TestTunnelFromUnitEListe(t *testing.T) {
	if tn, ok := TunnelFromUnit("openvpn@x.service"); !ok || !tn.Legacy || tn.Name != "x" {
		t.Errorf("%+v %v", tn, ok)
	}
	for _, bad := range []string{"openvpn-client@.service", "openvpn-server@x.service", "openvpn.service", "openvpn-client@-x"} {
		if _, ok := TunnelFromUnit(bad); ok {
			t.Errorf("%q accettato", bad)
		}
	}
	lu := ParseListUnits("openvpn-client@a.service loaded inactive dead OpenVPN tunnel for a\nopenvpn@b.service loaded active running x\n\n")
	if len(lu) != 2 || lu[0].Unit != "openvpn-client@a" || lu[1].Unit != "openvpn@b" {
		t.Errorf("list-units: %+v", lu)
	}
	rl := ParseRootListing("a.conf\nb.ovpn\nc d.conf\n-x.conf\n", "/etc/openvpn/client", "openvpn-client@", false)
	if len(rl) != 1 || rl[0].Unit != "openvpn-client@a" {
		t.Errorf("elenco con privilegi: %+v", rl)
	}
}
