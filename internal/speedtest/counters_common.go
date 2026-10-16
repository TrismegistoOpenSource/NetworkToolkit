// SPDX-License-Identifier: MIT

package speedtest

import (
	"fmt"

	"networktoolkit/internal/netif"
)

// activeInterfaceName riusa l'elenco schede già scritto per il MAC changer:
// stessa nozione di "scheda attiva" in tutto il toolkit, condivisa fra i tre
// lettori di contatori per-OS.
func activeInterfaceName() (string, error) {
	list, err := netif.List()
	if err != nil {
		return "", err
	}
	for _, it := range list {
		if it.IsActive {
			return it.Name, nil
		}
	}
	if len(list) > 0 {
		return list[0].Name, nil // nessuna "attiva" rilevata: la prima è un fallback ragionevole
	}
	return "", fmt.Errorf("nessuna interfaccia di rete trovata")
}
