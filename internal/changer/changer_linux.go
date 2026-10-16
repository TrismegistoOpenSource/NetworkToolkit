// SPDX-License-Identifier: MIT

//go:build linux

package changer

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/vishvananda/netlink"

	"networktoolkit/internal/macaddr"
)

// Caps: su Linux il cambio via netlink è volatile (torna al riavvio), la
// persistenza passa da NetworkManager (cloned-mac-address) se attivo, altrimenti
// da un file .link di systemd-networkd.
func Caps() Capabilities {
	return Capabilities{OS: "linux", CanTemporary: true, CanPermanent: true}
}

const linkDropinDir = "/etc/systemd/network"

func linkDropinPath(id string) string {
	return linkDropinDir + "/10-ntk-" + sanitize(id) + ".link"
}

func platformSet(t Target, persistent bool, log *logbuf) error {
	mac, err := net.ParseMAC(macaddr.Normalize(t.MAC))
	if err != nil {
		return err
	}
	if err := setHardwareAddr(t.ID, mac, log); err != nil {
		return err
	}
	if persistent {
		if err := persistLinux(t, macaddr.Normalize(t.MAC), log); err != nil {
			log.add("  ATTENZIONE: cambio applicato, ma persistenza non installata: %v", err)
		}
	}
	return nil
}

func platformReset(t Target, log *logbuf) error {
	// Rimuove prima la persistenza, così non riscrive il MAC alla prossima
	// riconnessione, poi riporta il MAC di fabbrica.
	unpersistLinux(t, log)

	factory := macaddr.Normalize(t.MAC)
	if factory == "" {
		// Non ci è stato passato: proviamo a leggerlo dall'attributo permanente.
		if link, err := netlink.LinkByName(t.ID); err == nil {
			if p := link.Attrs().PermHWAddr; len(p) == 6 {
				factory = macaddr.Normalize(p.String())
			}
		}
	}
	if factory == "" {
		log.add("  MAC di fabbrica non noto: si ripristina da solo al riavvio")
		return nil
	}
	mac, err := net.ParseMAC(factory)
	if err != nil {
		return err
	}
	return setHardwareAddr(t.ID, mac, log)
}

// setHardwareAddr abbassa l'interfaccia, cambia il MAC e la rialza. Molti
// driver (Wi-Fi in particolare) accettano il cambio solo da interfaccia down e
// non associata.
func setHardwareAddr(id string, mac net.HardwareAddr, log *logbuf) error {
	link, err := netlink.LinkByName(id)
	if err != nil {
		return fmt.Errorf("%w: interfaccia %q non trovata (%v)", ErrNotSupported, id, err)
	}
	wasUp := link.Attrs().Flags&net.FlagUp != 0
	if err := netlink.LinkSetDown(link); err != nil {
		return classifyErr(err)
	}
	if err := netlink.LinkSetHardwareAddr(link, mac); err != nil {
		// errno 95 EOPNOTSUPP → il driver rifiuta (tipico coi MAC non locali).
		_ = netlink.LinkSetUp(link) // non lasciamola giù
		return classifyErr(err)
	}
	if wasUp {
		if err := netlink.LinkSetUp(link); err != nil {
			return classifyErr(err)
		}
	}
	log.add("  netlink: MAC impostato su %s", mac)
	return nil
}

// persistLinux rende permanente il cambio. Con NetworkManager attivo si usa la
// sua proprietà cloned-mac-address sulla connessione della scheda, altrimenti
// NM la sovrascriverebbe alla riconnessione. Senza NM si scrive un file .link.
func persistLinux(t Target, mac string, log *logbuf) error {
	if conn := nmConnectionFor(t.ID); conn != "" {
		prop := "ethernet.cloned-mac-address"
		if t.Kind == "wifi" {
			prop = "wifi.cloned-mac-address"
		}
		if err := run(log, "nmcli", "connection", "modify", conn, prop, mac); err != nil {
			return err
		}
		log.add("  NetworkManager: %s impostato su %s per la connessione %q", prop, mac, conn)
		return nil
	}
	return writeLinkDropin(t.ID, mac, log)
}

func unpersistLinux(t Target, log *logbuf) {
	if conn := nmConnectionFor(t.ID); conn != "" {
		prop := "ethernet.cloned-mac-address"
		if t.Kind == "wifi" {
			prop = "wifi.cloned-mac-address"
		}
		// "permanent" dice a NM di usare il MAC hardware, annullando l'override.
		if err := run(log, "nmcli", "connection", "modify", conn, prop, "permanent"); err == nil {
			log.add("  NetworkManager: ripristinato %s=permanent per %q", prop, conn)
		}
	}
	removeLinkDropin(t.ID, log)
}

// nmConnectionFor restituisce il nome della connessione NetworkManager attiva
// sulla scheda, o "" se NM non è in uso o la scheda non ne ha una attiva.
func nmConnectionFor(id string) string {
	if _, err := exec.LookPath("nmcli"); err != nil {
		return ""
	}
	out, err := exec.Command("nmcli", "-t", "-f", "NAME,DEVICE", "connection", "show", "--active").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		// formato: NAME:DEVICE (i ':' nel nome sono escapati da nmcli come \:)
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		dev := parts[len(parts)-1]
		name := strings.Join(parts[:len(parts)-1], ":")
		if dev == id {
			return strings.ReplaceAll(name, `\:`, ":")
		}
	}
	return ""
}

func writeLinkDropin(id, mac string, log *logbuf) error {
	if err := os.MkdirAll(linkDropinDir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("[Match]\nOriginalName=%s\n\n[Link]\nMACAddress=%s\n", id, mac)
	path := linkDropinPath(id)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	log.add("  systemd-networkd: scritto %s", path)
	_ = run(log, "udevadm", "control", "--reload")
	return nil
}

func removeLinkDropin(id string, log *logbuf) {
	path := linkDropinPath(id)
	if _, err := os.Stat(path); err != nil {
		return
	}
	if err := os.Remove(path); err == nil {
		log.add("  systemd-networkd: rimosso %s", path)
		_ = run(log, "udevadm", "control", "--reload")
	}
}

func classifyErr(err error) error { return classify(err) }
