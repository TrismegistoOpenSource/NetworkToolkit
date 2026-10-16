// SPDX-License-Identifier: MIT

//go:build windows

package netif

import (
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"

	"networktoolkit/internal/macaddr"
)

// List elenca le schede reali su Windows via GetAdaptersAddresses (IP Helper
// API), senza privilegi. Tiene solo Ethernet (IF_TYPE_ETHERNET_CSMACD) e
// Wi-Fi (IF_TYPE_IEEE80211), scartando loopback, tunnel e adattatori software.
//
// L'ID è AdapterName, cioè il GUID {....} dell'interfaccia: è lo stesso
// NetCfgInstanceId con cui l'helper ritrova la sottochiave di registro
// NetworkAddress da scrivere. Il MAC permanente non è esposto qui (la
// PhysicalAddress è quella corrente); lo fornisce il MAC salvato alla prima
// modifica, sovrapposto a monte da chi chiama.
func List() ([]Interface, error) {
	const flags = windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER

	var size uint32
	// Prima chiamata a vuoto per sapere quanti byte servono.
	err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, nil, &size)
	if err != nil && err != windows.ERROR_BUFFER_OVERFLOW {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	addr := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, addr, &size); err != nil {
		return nil, err
	}

	var out []Interface
	for a := addr; a != nil; a = a.Next {
		if a.IfType != windows.IF_TYPE_ETHERNET_CSMACD && a.IfType != windows.IF_TYPE_IEEE80211 {
			continue
		}
		if a.PhysicalAddressLength != 6 {
			continue // niente MAC a 48 bit
		}
		hw := make([]byte, 6)
		copy(hw, a.PhysicalAddress[:6])
		cur := macaddr.Normalize(net6(hw))
		if cur == "" {
			continue
		}

		it := Interface{
			ID:          windows.BytePtrToString(a.AdapterName),
			Name:        windows.BytePtrToString(a.AdapterName),
			DisplayName: windows.UTF16PtrToString(a.FriendlyName),
			CurrentMAC:  cur,
			Kind:        KindEthernet,
			IsUp:        a.OperStatus == windows.IfOperStatusUp,
			IsActive:    a.OperStatus == windows.IfOperStatusUp,
		}
		if a.IfType == windows.IF_TYPE_IEEE80211 {
			it.Kind = KindWiFi
		}
		if it.DisplayName == "" {
			it.DisplayName = windows.UTF16PtrToString(a.Description)
		}
		it.Spoofed = macaddr.IsLocallyAdministered(it.CurrentMAC)
		out = append(out, it)
	}

	sort.Stable(byActiveThenName(out))
	return out, nil
}

// net6 formatta 6 byte come aa:bb:cc:dd:ee:ff senza tirare dentro il pacchetto
// net solo per HardwareAddr.String().
func net6(b []byte) string {
	const hexdig = "0123456789abcdef"
	s := make([]byte, 0, 17)
	for i, x := range b {
		if i > 0 {
			s = append(s, ':')
		}
		s = append(s, hexdig[x>>4], hexdig[x&0x0f])
	}
	return string(s)
}
