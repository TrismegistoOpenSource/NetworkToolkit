// SPDX-License-Identifier: MIT

//go:build linux

package netif

import (
	"net"
	"os"
	"sort"

	"github.com/vishvananda/netlink"

	"networktoolkit/internal/macaddr"
)

// List elenca le schede reali su Linux via netlink (puro Go, nessun binario
// esterno, nessun privilegio per la sola lettura).
//
// "Scheda reale" = interfaccia che ha una directory device sotto
// /sys/class/net/<nome>/device: esclude loopback, bridge, veth di Docker,
// tunnel wireguard e simili, che non hanno un dispositivo fisico dietro.
// netlink fornisce direttamente il MAC corrente (HardwareAddr) e quello
// permanente (PermHWAddr, dall'attributo IFLA_PERM_ADDRESS), senza dover
// ricorrere a ethtool.
func List() ([]Interface, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	var out []Interface
	for _, l := range links {
		a := l.Attrs()
		if a.HardwareAddr == nil || len(a.HardwareAddr) != 6 {
			continue // niente MAC a 48 bit: non è una scheda che cambiamo
		}
		if !hasDevice(a.Name) {
			continue // interfaccia virtuale (bridge/veth/tun...)
		}

		it := Interface{
			ID:         a.Name,
			Name:       a.Name,
			CurrentMAC: macaddr.Normalize(a.HardwareAddr.String()),
			Kind:       KindEthernet,
			IsUp:       a.Flags&net.FlagUp != 0,
			IsActive:   a.OperState == netlink.OperUp,
		}
		it.DisplayName = friendlyName(a.Name)
		if isWireless(a.Name) {
			it.Kind = KindWiFi
		}
		if len(a.PermHWAddr) == 6 {
			pm := macaddr.Normalize(a.PermHWAddr.String())
			if pm != "" && pm != it.CurrentMAC {
				it.PermanentMAC = pm
			}
		}
		it.Spoofed = it.PermanentMAC != "" && macaddr.IsLocallyAdministered(it.CurrentMAC)
		out = append(out, it)
	}

	sort.Stable(byActiveThenName(out))
	return out, nil
}

func hasDevice(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name + "/device")
	return err == nil
}

// isWireless: la presenza di phy80211 sotto la scheda è il marcatore standard
// del kernel per un dispositivo 802.11.
func isWireless(name string) bool {
	if _, err := os.Stat("/sys/class/net/" + name + "/phy80211"); err == nil {
		return true
	}
	_, err := os.Stat("/sys/class/net/" + name + "/wireless")
	return err == nil
}

func friendlyName(name string) string {
	if isWireless(name) {
		return "Wi-Fi (" + name + ")"
	}
	return "Ethernet (" + name + ")"
}
