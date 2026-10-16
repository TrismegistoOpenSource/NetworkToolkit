// SPDX-License-Identifier: MIT

// Package netif elenca le schede di rete reali e ne legge lo stato (MAC
// corrente, MAC permanente, se è attiva, se è Wi-Fi). È la parte NON
// privilegiata: leggere le interfacce non richiede root su nessun OS, quindi
// la GUI la chiama direttamente. Le modifiche vere passano invece dall'helper
// elevato (package changer).
//
// Ogni OS ha un file dedicato con la propria List(); qui stanno solo i tipi e
// gli helper condivisi.
package netif

const (
	KindWiFi     = "wifi"
	KindEthernet = "ethernet"
)

// Interface descrive una scheda di rete come la vede l'utente.
type Interface struct {
	// ID è l'identificatore stabile con cui l'helper ritrova la scheda:
	// il nome BSD/kernel su macOS e Linux (en0, eth0), il GUID
	// NetCfgInstanceId su Windows. È la chiave di join autorevole, non il
	// nome visualizzato (che può cambiare o ripetersi).
	ID string `json:"id"`
	// Name è il nome breve del sistema (en0, eth0, wlan0). Mostrato come
	// dettaglio tecnico accanto al nome amichevole.
	Name string `json:"name"`
	// DisplayName è il nome leggibile ("Wi-Fi", "Thunderbolt Ethernet").
	DisplayName string `json:"displayName"`
	// Kind è KindWiFi o KindEthernet: decide l'icona e la sequenza di
	// cambio MAC (il Wi-Fi va disassociato prima).
	Kind string `json:"kind"`
	// CurrentMAC è il MAC in uso adesso, forma canonica aa:bb:cc:dd:ee:ff.
	CurrentMAC string `json:"currentMac"`
	// PermanentMAC è il MAC di fabbrica, se noto e diverso dal corrente.
	// Vuoto quando coincide col corrente o non è determinabile.
	PermanentMAC string `json:"permanentMac"`
	// IsUp è vero se l'interfaccia è amministrativamente attiva.
	IsUp bool `json:"isUp"`
	// IsActive è vero se la scheda è quella realmente in uso (carrier
	// presente e un indirizzo instradabile): serve a pre-flaggarla e a
	// distinguerla con il LED.
	IsActive bool `json:"isActive"`
	// Spoofed è vero se il MAC corrente risulta amministrato localmente e
	// diverso dal permanente: un indizio che è già stato cambiato.
	Spoofed bool `json:"spoofed"`
}

// byActiveThenName ordina mettendo prima le schede attive, poi per nome:
// così l'interfaccia in uso è sempre in cima alla lista.
type byActiveThenName []Interface

func (s byActiveThenName) Len() int      { return len(s) }
func (s byActiveThenName) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byActiveThenName) Less(i, j int) bool {
	if s[i].IsActive != s[j].IsActive {
		return s[i].IsActive // le attive prima
	}
	if s[i].Kind != s[j].Kind {
		return s[i].Kind == KindWiFi // il Wi-Fi prima, di solito è quello che interessa
	}
	return s[i].Name < s[j].Name
}
