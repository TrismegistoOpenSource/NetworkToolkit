// SPDX-License-Identifier: MIT

//go:build linux

package speedtest

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// linuxCounters legge /proc/net/dev: puro Go, nessun sottoprocesso, nessun
// privilegio richiesto. Formato di ogni riga (dopo le due di intestazione):
//
//	face |bytes    packets ... | bytes    packets ...
//	   eth0: 123456   100  0 ...    654321   90  0 ...
//
// Il primo numero dopo il nome è RX bytes, l'ottavo è TX bytes.
type linuxCounters struct {
	iface string // nome scelto una volta all'avvio (la scheda attiva)
}

func NewOSCounters() (Counters, error) {
	iface, err := activeInterfaceName()
	if err != nil {
		return nil, err
	}
	return &linuxCounters{iface: iface}, nil
}

func (c *linuxCounters) Read() (rx, tx uint64, err error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue // riga di intestazione
		}
		name := strings.TrimSpace(line[:i])
		if name != c.iface {
			continue
		}
		fields := strings.Fields(line[i+1:])
		if len(fields) < 9 {
			return 0, 0, fmt.Errorf("/proc/net/dev: riga di %q malformata", name)
		}
		rx, err = strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		tx, err = strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		return rx, tx, nil
	}
	return 0, 0, fmt.Errorf("/proc/net/dev: interfaccia %q non trovata", c.iface)
}
