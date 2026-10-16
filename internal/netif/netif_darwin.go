// SPDX-License-Identifier: MIT

//go:build darwin

package netif

import (
	"net"
	"os/exec"
	"sort"
	"strings"

	"networktoolkit/internal/macaddr"
)

// List elenca le schede reali su macOS.
//
// La fonte autorevole di "cos'è una scheda vera" è
// `networksetup -listallhardwareports`: elenca solo le porte hardware
// (Wi-Fi, Ethernet, Thunderbolt...), non le decine di interfacce di sistema
// (awdl0, llw0, utunN, bridge0, gif0...) che confonderebbero l'utente. In più
// la sua "Ethernet Address" è il MAC **di fabbrica** letto dalla IORegistry:
// resta invariato anche dopo un `ifconfig ether`, quindi è il nostro MAC
// permanente e il bersaglio del ripristino, senza doverlo salvare da nessuna
// parte.
//
// Alle porte hardware si aggiungono le eventuali interfacce `feth` (schede
// virtuali usate per provare l'app senza toccare la rete vera): non compaiono
// in networksetup, ma vanno mostrate quando ci sono.
func List() ([]Interface, error) {
	perm := parseHardwarePorts() // bsd name -> hardwarePort
	sysIfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var out []Interface
	for _, si := range sysIfaces {
		hp, isHardware := perm[si.Name]
		isFeth := strings.HasPrefix(si.Name, "feth")
		if !isHardware && !isFeth {
			continue // interfaccia di sistema, non una scheda che l'utente cambierebbe
		}

		cur := si.HardwareAddr.String()
		if cur == "" {
			continue // niente MAC (es. interfaccia punto-punto): non pertinente
		}

		it := Interface{
			ID:         si.Name,
			Name:       si.Name,
			CurrentMAC: macaddr.Normalize(cur),
			Kind:       KindEthernet,
			IsUp:       si.Flags&net.FlagUp != 0,
			IsActive:   isActive(si),
		}

		if isHardware {
			it.DisplayName = hp.port
			if strings.Contains(strings.ToLower(hp.port), "wi-fi") {
				it.Kind = KindWiFi
			}
			pm := macaddr.Normalize(hp.mac)
			if pm != "" && pm != it.CurrentMAC {
				it.PermanentMAC = pm
			}
		} else {
			it.DisplayName = "Scheda virtuale di test (" + si.Name + ")"
		}

		it.Spoofed = it.PermanentMAC != "" && macaddr.IsLocallyAdministered(it.CurrentMAC)
		out = append(out, it)
	}

	sort.Stable(byActiveThenName(out))
	return out, nil
}

// isActive: la scheda ha carrier (RUNNING) ed espone un indirizzo IP
// instradabile. È l'euristica "in uso" che non richiede di leggere la tabella
// di routing (che avrebbe bisogno di privilegi/parsing fragile).
func isActive(si net.Interface) bool {
	if si.Flags&net.FlagUp == 0 || si.Flags&net.FlagRunning == 0 {
		return false
	}
	addrs, err := si.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ip, ok := a.(*net.IPNet); ok && ip.IP.IsGlobalUnicast() && !ip.IP.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

type hardwarePort struct {
	port string
	mac  string
}

// parseHardwarePorts legge l'output di networksetup a triplette:
//
//	Hardware Port: Wi-Fi
//	Device: en0
//	Ethernet Address: a0:9a:8e:93:d5:0c
func parseHardwarePorts() map[string]hardwarePort {
	res := map[string]hardwarePort{}
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return res // niente networksetup (improbabile su macOS): resta la sola feth
	}
	var curPort, curDev, curMAC string
	flush := func() {
		if curDev != "" {
			res[curDev] = hardwarePort{port: curPort, mac: curMAC}
		}
		curPort, curDev, curMAC = "", "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Hardware Port:"):
			flush()
			curPort = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
		case strings.HasPrefix(line, "Device:"):
			curDev = strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
		case strings.HasPrefix(line, "Ethernet Address:"):
			curMAC = strings.TrimSpace(strings.TrimPrefix(line, "Ethernet Address:"))
		}
	}
	flush()
	return res
}
