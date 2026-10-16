// SPDX-License-Identifier: MIT

// Package macaddr valida, normalizza e genera indirizzi MAC a 48 bit.
//
// È volutamente puro Go, senza dipendenze e senza build tag: è la parte più
// facile da sbagliare in silenzio (un bit girato male e il driver rifiuta il
// MAC senza dire nulla) e la più facile da coprire con test veri. Tutta la
// logica OS-specifica sta altrove; qui vivono solo le regole IEEE 802.
package macaddr

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
)

// GenerateLocalRandom crea un MAC casuale valido e assegnabile a una NIC:
//
//   - bit unicast: il bit meno significativo del primo ottetto è 0 (un MAC
//     multicast non è assegnabile a una scheda);
//   - bit "locally administered": il secondo bit meno significativo del primo
//     ottetto è 1, così l'indirizzo dichiara di non essere universale.
//
// Combinando i due vincoli il secondo carattere esadecimale può valere solo
// 2, 6, A o E: è l'unica forma che tutti i driver accettano, Wi-Fi di Windows
// compreso, ed è lo stesso tipo di indirizzo privato che generano iPhone,
// Android e macOS per non farsi tracciare. L'entropia viene da crypto/rand.
func GenerateLocalRandom() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sorgente casuale non disponibile: %w", err)
	}
	b[0] &^= 0x01 // azzera il bit multicast → unicast
	b[0] |= 0x02  // alza il bit locally-administered
	return Format(net.HardwareAddr(b)), nil
}

// Format restituisce la forma canonica minuscola con i due punti
// (aa:bb:cc:dd:ee:ff), la stessa che stampa ifconfig e `ip link`.
func Format(hw net.HardwareAddr) string {
	return hw.String()
}

// Normalize riporta qualunque scrittura accettata (con : , - o . ) alla forma
// canonica di Format, così due MAC si possono confrontare come stringhe.
// Restituisce "" se la stringa non è un MAC a 48 bit valido: comodo per un
// confronto tollerante senza dover gestire l'errore a ogni chiamata.
func Normalize(s string) string {
	hw, err := Parse(s)
	if err != nil {
		return ""
	}
	return Format(hw)
}

// Parse valida un MAC a 48 bit. Accetta i separatori di net.ParseMAC
// (aa:bb:..., aa-bb-..., aabb.ccdd.eeff) e rifiuta esplicitamente:
//   - indirizzi che non siano a 6 byte (EUI-64, Infiniband...);
//   - indirizzi multicast, che una NIC non può assumere.
//
// NON impone il bit locally-administered: un utente che digita a mano un MAC
// vendor (secondo nibble diverso da 2/6/A/E) è libero di provarci; sarà poi il
// driver a rifiutarlo, e la verifica post-applicazione a intercettare il caso.
func Parse(s string) (net.HardwareAddr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("MAC vuoto")
	}
	hw, err := net.ParseMAC(s)
	if err != nil {
		return nil, fmt.Errorf("MAC non valido: %q", s)
	}
	if len(hw) != 6 {
		return nil, fmt.Errorf("sono supportati solo MAC a 48 bit, non %d byte", len(hw))
	}
	if hw[0]&0x01 != 0 {
		return nil, fmt.Errorf("MAC multicast %s non assegnabile a una scheda di rete", hw)
	}
	return hw, nil
}

// IsLocallyAdministered dice se il MAC ha il bit locally-administered alzato.
// Serve alla UI per etichettare un indirizzo come "casuale/privato" anziché
// hardware, e per capire se un MAC digitato a mano rischia il rifiuto del
// driver Wi-Fi.
func IsLocallyAdministered(s string) bool {
	hw, err := Parse(s)
	if err != nil {
		return false
	}
	return hw[0]&0x02 != 0
}

// Hex12 restituisce il MAC come 12 caratteri esadecimali maiuscoli senza
// separatori (AABBCCDDEEFF): è il formato che vuole il valore di registro
// NetworkAddress su Windows, dove 11 o 13 caratteri vengono scartati in
// silenzio.
func Hex12(s string) (string, error) {
	hw, err := Parse(s)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(strings.ReplaceAll(hw.String(), ":", "")), nil
}
