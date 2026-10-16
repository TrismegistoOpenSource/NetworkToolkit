// SPDX-License-Identifier: MIT

package vpn

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dove le distribuzioni mettono i tunnel OpenVPN gestiti da systemd.
//
//   - /etc/openvpn/client/<nome>.conf → openvpn-client@<nome> (Debian, Ubuntu,
//     Fedora, Arch: l'unità fa `openvpn --config <nome>.conf` in quella
//     cartella, quindi legge SOLO i .conf);
//   - /etc/openvpn/<nome>.conf → openvpn@<nome>, l'unità di prima (Debian la
//     conserva ancora).
//
// Un .ovpn è lo stesso formato con un'altra estensione: systemd non lo vede
// finché non lo si rinomina in .conf, e il toolkit lo dice invece di ignorarlo.
type ScanDirs struct {
	Client string   // /etc/openvpn/client
	Legacy string   // /etc/openvpn
	Wants  []string // cartelle *.wants di systemd: i tunnel abilitati all'avvio
}

// DefaultScanDirs sono i percorsi veri del sistema.
func DefaultScanDirs() ScanDirs {
	wants, _ := filepath.Glob("/etc/systemd/system/*.wants")
	return ScanDirs{Client: "/etc/openvpn/client", Legacy: "/etc/openvpn", Wants: wants}
}

// ScanResult è l'elenco dei tunnel trovati leggendo le cartelle.
type ScanResult struct {
	Tunnels    []Tunnel
	Notes      []string
	Unreadable bool
}

// ScanTunnels cerca i tunnel nelle cartelle di OpenVPN. Non serve root, a
// meno che la distribuzione non chiuda la cartella (Arch: 750 openvpn:network):
// in quel caso lo segnala, e i tunnel abilitati all'avvio si trovano lo
// stesso dai collegamenti di systemd, che sono leggibili da tutti.
func ScanTunnels(d ScanDirs) ScanResult {
	var r ScanResult
	seen := map[string]bool{}
	add := func(t Tunnel) {
		if seen[t.Unit] {
			return
		}
		seen[t.Unit] = true
		r.Tunnels = append(r.Tunnels, t)
	}

	scanDir := func(dir, unitPrefix string, legacy bool) {
		if dir == "" {
			return
		}
		entries, err := os.ReadDir(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return
		case errors.Is(err, fs.ErrPermission):
			r.Unreadable = true
			r.Notes = append(r.Notes, fmt.Sprintf(
				"La cartella %s non è leggibile senza privilegi: qui sotto compaiono solo i tunnel abilitati all'avvio o già noti a systemd. Per vederli tutti usa «Cerca con privilegi».", dir))
			return
		case err != nil:
			r.Notes = append(r.Notes, fmt.Sprintf("Lettura di %s fallita: %v", dir, err))
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			base := strings.TrimSuffix(name, filepath.Ext(name))
			switch ext {
			case ".conf":
				if !ValidTunnelName(base) {
					r.Notes = append(r.Notes, fmt.Sprintf(
						"%s: il nome contiene caratteri che systemd non accetta senza escaping (spazi, accenti…). Rinominalo con sole lettere, numeri, trattini e punti.",
						filepath.Join(dir, name)))
					continue
				}
				add(Tunnel{Name: base, Unit: unitPrefix + base, Config: filepath.Join(dir, name), Legacy: legacy})
			case ".ovpn":
				target := base + ".conf"
				if !ValidTunnelName(base) {
					target = "<nome-senza-spazi>.conf"
				}
				r.Notes = append(r.Notes, fmt.Sprintf(
					"%s: systemd legge solo i file .conf. Per usarlo: sudo mv %s %s",
					filepath.Join(dir, name), shellQuote(filepath.Join(dir, name)), shellQuote(filepath.Join(dir, target))))
			}
		}
	}

	scanDir(d.Client, "openvpn-client@", false)
	scanDir(d.Legacy, "openvpn@", true)

	// Tunnel abilitati all'avvio: i collegamenti nelle cartelle *.wants si
	// leggono anche quando /etc/openvpn/client è chiusa.
	for _, w := range d.Wants {
		entries, err := os.ReadDir(w)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if t, ok := TunnelFromUnit(e.Name()); ok {
				add(t)
			}
		}
	}

	sort.Slice(r.Tunnels, func(i, j int) bool {
		if r.Tunnels[i].Name != r.Tunnels[j].Name {
			return r.Tunnels[i].Name < r.Tunnels[j].Name
		}
		return !r.Tunnels[i].Legacy // a parità di nome, prima l'unità moderna
	})
	return r
}

// TunnelFromUnit riconosce un'unità OpenVPN (openvpn-client@x.service o
// openvpn@x.service, con o senza suffisso).
func TunnelFromUnit(unit string) (Tunnel, bool) {
	u := strings.TrimSuffix(strings.TrimSpace(unit), ".service")
	for _, p := range []struct {
		prefix string
		legacy bool
		dir    string
	}{
		{"openvpn-client@", false, "/etc/openvpn/client"},
		{"openvpn@", true, "/etc/openvpn"},
	} {
		if name, ok := strings.CutPrefix(u, p.prefix); ok && ValidTunnelName(name) {
			return Tunnel{Name: name, Unit: p.prefix + name, Config: p.dir + "/" + name + ".conf", Legacy: p.legacy}, true
		}
	}
	return Tunnel{}, false
}

// ParseListUnits legge `systemctl list-units --all --plain --no-legend …`:
// la prima colonna di ogni riga è il nome dell'unità.
func ParseListUnits(out string) []Tunnel {
	var res []Tunnel
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if t, ok := TunnelFromUnit(f[0]); ok {
			res = append(res, t)
		}
	}
	return res
}

// ParseRootListing legge l'uscita di `ls -1 <cartella>` fatta con privilegi:
// un nome di file per riga.
func ParseRootListing(out, dir, unitPrefix string, legacy bool) []Tunnel {
	var res []Tunnel
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasSuffix(name, ".conf") {
			continue
		}
		base := strings.TrimSuffix(name, ".conf")
		if ValidTunnelName(base) {
			res = append(res, Tunnel{Name: base, Unit: unitPrefix + base, Config: dir + "/" + name, Legacy: legacy})
		}
	}
	return res
}
