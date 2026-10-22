// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"networktoolkit/internal/autoverifica"
)

func TestVersionMatchesWails(t *testing.T) {
	b, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`"productVersion":\s*"([^"]+)"`).FindSubmatch(b)
	if m == nil || string(m[1]) != Version {
		t.Fatalf("Version %q, wails.json %q", Version, m)
	}
}

// Fuori da un pacchetto confezionato (qui, il binario dei test) non c'è
// INTEGRITA.txt: l'esito è «non verificabile», senza rete, scritto nella
// cartella di configurazione (di prova, non la vera).
func TestVerifyIntegrityOutsidePackage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NETWORKTOOLKIT_CONFIG_DIR", dir)
	a := &App{}
	s := a.VerifyIntegrity()
	if s.Esito != autoverifica.EsitoNonVerificabile {
		t.Fatalf("esito %q", s.Esito)
	}
	if again := a.VerifyIntegrity(); again.Esito != s.Esito {
		t.Error("la seconda chiamata dà un altro esito")
	}
	if _, err := os.Stat(filepath.Join(dir, autoverifica.NomeFileStato)); err != nil {
		t.Errorf("stato non scritto: %v", err)
	}
}
