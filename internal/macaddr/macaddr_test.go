// SPDX-License-Identifier: MIT

package macaddr

import (
	"strings"
	"testing"
)

func TestGenerateLocalRandomIsValidAndLocal(t *testing.T) {
	// Molti giri: crypto/rand potrebbe teoricamente produrre un ottetto che
	// viola i vincoli se i bit non venissero forzati. Ne generiamo tanti e
	// controlliamo ogni volta i due invarianti.
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		mac, err := GenerateLocalRandom()
		if err != nil {
			t.Fatalf("GenerateLocalRandom: %v", err)
		}
		hw, err := Parse(mac)
		if err != nil {
			t.Fatalf("MAC generato non valido %q: %v", mac, err)
		}
		if hw[0]&0x01 != 0 {
			t.Fatalf("MAC generato %q è multicast (LSB del primo ottetto = 1)", mac)
		}
		if hw[0]&0x02 == 0 {
			t.Fatalf("MAC generato %q non è locally-administered", mac)
		}
		// Il secondo carattere esadecimale deve stare in {2,6,a,e}.
		second := mac[1]
		if !strings.ContainsRune("26ae", rune(second)) {
			t.Fatalf("MAC generato %q ha secondo nibble %q, atteso uno di 2/6/a/e", mac, string(second))
		}
		seen[mac] = true
	}
	// Non è un test di qualità del CSPRNG, solo un canary contro un generatore
	// che restituisce sempre lo stesso valore (es. rand.Read stub).
	if len(seen) < 1900 {
		t.Fatalf("troppa poca varietà: %d MAC distinti su 2000", len(seen))
	}
}

func TestParseAcceptsSeparators(t *testing.T) {
	forms := []string{
		"02:aa:bb:cc:dd:ee",
		"02-AA-BB-CC-DD-EE",
		"02aa.bbcc.ddee",
		"  02:aa:bb:cc:dd:ee  ",
	}
	for _, f := range forms {
		if got := Normalize(f); got != "02:aa:bb:cc:dd:ee" {
			t.Fatalf("Normalize(%q) = %q, atteso 02:aa:bb:cc:dd:ee", f, got)
		}
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	bad := []string{
		"",
		"non-un-mac",
		"01:aa:bb:cc:dd:ee",       // multicast: LSB primo ottetto = 1
		"ff:ff:ff:ff:ff:ff",       // broadcast → multicast
		"02:aa:bb:cc:dd:ee:ff:00", // EUI-64, non 48 bit
		"02:aa:bb:cc:dd",          // troppo corto
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Fatalf("Parse(%q) doveva fallire, non ha dato errore", s)
		}
	}
}

func TestIsLocallyAdministered(t *testing.T) {
	if !IsLocallyAdministered("02:aa:bb:cc:dd:ee") {
		t.Fatal("02:... deve risultare locally-administered")
	}
	if IsLocallyAdministered("00:aa:bb:cc:dd:ee") {
		t.Fatal("00:... NON deve risultare locally-administered")
	}
	if IsLocallyAdministered("garbage") {
		t.Fatal("un MAC invalido non è locally-administered")
	}
}

func TestHex12(t *testing.T) {
	got, err := Hex12("02:aa:bb:cc:dd:ee")
	if err != nil {
		t.Fatalf("Hex12: %v", err)
	}
	if got != "02AABBCCDDEE" {
		t.Fatalf("Hex12 = %q, atteso 02AABBCCDDEE", got)
	}
	if len(got) != 12 {
		t.Fatalf("Windows scarta un NetworkAddress che non sia lungo 12: %q è %d", got, len(got))
	}
	if _, err := Hex12("multicast:01"); err == nil {
		t.Fatal("Hex12 deve propagare l'errore di Parse")
	}
}
