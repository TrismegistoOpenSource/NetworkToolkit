// SPDX-License-Identifier: MIT

//go:build darwin

package speedtest

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// darwinCounters legge `netstat -ibn`: netstat è un tool di sistema di macOS
// (stesso trattamento già dato a ifconfig/networksetup nel MAC changer), non
// un binario terzo scaricato. È necessario perché golang.org/x/net/route (la
// libreria Go pura per le route su BSD) espone da RTM_IFINFO2 solo tipo e MTU
// dell'interfaccia (route.InterfaceMetrics), non i contatori Ibytes/Obytes
// del kernel — parsare a mano gli offset del messaggio raw del kernel
// sarebbe più fragile di un parsing testuale di un tool stabile da decenni.
//
// Formato di `netstat -ibn` (colonne separate da spazi, riga per
// interfaccia+famiglia indirizzo — <Link#N> è la riga col vero contatore
// hardware, le righe inet/inet6 duplicano nome ma non sempre i contatori):
//
//	Name  Mtu Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
//	en7   1500 <Link#10>   6c:1f:f7:...     16628327  0  22803320320  4205446  0  1029096979  0
type darwinCounters struct {
	iface string
}

func NewOSCounters() (Counters, error) {
	iface, err := activeInterfaceName()
	if err != nil {
		return nil, err
	}
	return &darwinCounters{iface: iface}, nil
}

func (c *darwinCounters) Read() (rx, tx uint64, err error) {
	out, err := exec.Command("netstat", "-ibn").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("netstat -ibn: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[0] != c.iface {
			continue
		}
		// La riga <Link#N> è quella col contatore hardware vero; le righe
		// inet/inet6 della stessa interfaccia hanno "Network" diverso da
		// "<Link#...". Si prende la prima riga col nome giusto la cui
		// colonna Network inizia per "<Link" — è quella affidabile.
		if !strings.HasPrefix(fields[2], "<Link") {
			continue
		}
		rx, err = strconv.ParseUint(fields[6], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		tx, err = strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		return rx, tx, nil
	}
	return 0, 0, fmt.Errorf("netstat -ibn: riga <Link> per %q non trovata", c.iface)
}
