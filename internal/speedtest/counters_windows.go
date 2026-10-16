// SPDX-License-Identifier: MIT

//go:build windows

package speedtest

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// windowsCounters legge MIB_IF_ROW2 via GetIfEntry2Ex (IP Helper API), lo
// stesso sottosistema già usato in internal/netif per elencare le schede:
// nessun sottoprocesso, nessun privilegio richiesto per la sola lettura.
type windowsCounters struct {
	guid windows.GUID
}

func NewOSCounters() (Counters, error) {
	name, err := activeInterfaceName() // su Windows netif.Interface.Name è il GUID ("{....}")
	if err != nil {
		return nil, err
	}
	guid, err := windows.GUIDFromString(name)
	if err != nil {
		return nil, fmt.Errorf("GUID interfaccia %q non valido: %w", name, err)
	}
	return &windowsCounters{guid: guid}, nil
}

func (c *windowsCounters) Read() (rx, tx uint64, err error) {
	row := windows.MibIfRow2{InterfaceGuid: c.guid}
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &row); err != nil {
		return 0, 0, fmt.Errorf("GetIfEntry2Ex: %w", err)
	}
	return row.InOctets, row.OutOctets, nil
}
