// SPDX-License-Identifier: MIT

//go:build darwin

package changer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"networktoolkit/internal/macaddr"
)

// Caps: su macOS il cambio via `ifconfig ether` è volatile (torna al riavvio),
// e la persistenza si ottiene con un LaunchDaemon che lo riapplica al boot.
func Caps() Capabilities {
	return Capabilities{OS: "darwin", CanTemporary: true, CanPermanent: true}
}

const launchDaemonDir = "/Library/LaunchDaemons"

func daemonLabel(id string) string {
	return "org.trismegisto.networktoolkit." + sanitize(id)
}
func daemonPath(id string) string {
	return filepath.Join(launchDaemonDir, daemonLabel(id)+".plist")
}

func platformSet(t Target, persistent bool, log *logbuf) error {
	mac := macaddr.Normalize(t.MAC)

	if t.Kind == "wifi" {
		// Il MAC del Wi-Fi non si cambia mentre l'interfaccia è associata a
		// una rete: si spegne la radio, si scrive il MAC da disassociati, poi
		// si riaccende. `airport -z` è stato ritirato in Sonoma 14.4, quindi
		// si usa networksetup.
		log.add("  Wi-Fi: spengo la radio per disassociarmi")
		_ = run(log, "networksetup", "-setairportpower", t.ID, "off")
	}

	if err := run(log, "ifconfig", t.ID, "ether", mac); err != nil {
		return classify(err)
	}

	if t.Kind == "wifi" {
		log.add("  Wi-Fi: riaccendo la radio")
		_ = run(log, "networksetup", "-setairportpower", t.ID, "on")
	}

	if persistent {
		if err := installDaemon(t, mac, log); err != nil {
			// Il MAC è comunque cambiato adesso: la persistenza è un extra, non
			// un fallimento del cambio. Lo segnaliamo senza far fallire tutto.
			log.add("  ATTENZIONE: cambio applicato, ma persistenza non installata: %v", err)
		}
	}
	return nil
}

func platformReset(t Target, log *logbuf) error {
	removeDaemon(t.ID, log) // se non c'è, nessun problema

	if t.Kind == "wifi" {
		_ = run(log, "networksetup", "-setairportpower", t.ID, "off")
	}

	factory := macaddr.Normalize(t.MAC)
	if factory == "" {
		log.add("  MAC di fabbrica non noto: si ripristina da solo al riavvio")
		if t.Kind == "wifi" {
			_ = run(log, "networksetup", "-setairportpower", t.ID, "on")
		}
		return nil
	}
	err := run(log, "ifconfig", t.ID, "ether", factory)
	if t.Kind == "wifi" {
		_ = run(log, "networksetup", "-setairportpower", t.ID, "on")
	}
	if err != nil {
		return classify(err)
	}
	return nil
}

// installDaemon scrive un LaunchDaemon che riapplica il MAC a ogni avvio.
// Per l'Ethernet è affidabile; per il Wi-Fi è best-effort (al boot la radio
// potrebbe essersi già associata).
func installDaemon(t Target, mac string, log *logbuf) error {
	label := daemonLabel(t.ID)
	path := daemonPath(t.ID)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>RunAtLoad</key><true/>
  <key>ProgramArguments</key>
  <array>
    <string>/sbin/ifconfig</string>
    <string>%s</string>
    <string>ether</string>
    <string>%s</string>
  </array>
</dict>
</plist>
`, label, t.ID, mac)

	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	log.add("  persistenza: scritto %s", path)
	// bootout/bootstrap ricaricano il daemon; su bootout di uno assente esce
	// non-zero, quindi lo si ignora.
	_ = exec.Command("launchctl", "bootout", "system", path).Run()
	if err := exec.Command("launchctl", "bootstrap", "system", path).Run(); err != nil {
		log.add("  nota: launchctl bootstrap ha segnalato %v (il MAC è comunque impostato)", err)
	}
	return nil
}

func removeDaemon(id string, log *logbuf) {
	path := daemonPath(id)
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = exec.Command("launchctl", "bootout", "system", path).Run()
	if err := os.Remove(path); err == nil {
		log.add("  persistenza rimossa: %s", path)
	}
}
