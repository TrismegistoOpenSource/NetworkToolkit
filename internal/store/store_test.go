// SPDX-License-Identifier: MIT

package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Tutti i test scrivono SOLO in t.TempDir(): mai il percorso reale dell'utente.
// È la regola assoluta del workspace (un test senza isolamento cancellò 10
// profili di RsyncGUI).

func TestRememberIsFirstWriteWins(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RememberOriginal("en0", "Wi-Fi", "a0:9a:8e:93:d5:0c"); err != nil {
		t.Fatal(err)
	}
	// Un secondo tentativo non deve sovrascrivere il primo valore.
	if err := s.RememberOriginal("en0", "Wi-Fi", "02:11:22:33:44:55"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Original("en0")
	if !ok || got != "a0:9a:8e:93:d5:0c" {
		t.Fatalf("Original(en0) = %q,%v — atteso il PRIMO valore memorizzato", got, ok)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.RememberOriginal("eth0", "Ethernet", "de:ad:be:ef:00:11"); err != nil {
		t.Fatal(err)
	}
	// Riaprendo da zero il valore deve tornare dal disco.
	s2, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Original("eth0")
	if !ok || got != "de:ad:be:ef:00:11" {
		t.Fatalf("dopo riapertura Original(eth0) = %q,%v", got, ok)
	}
}

func TestForget(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.RememberOriginal("en5", "USB", "00:11:22:33:44:55")
	if err := s.Forget("en5"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Original("en5"); ok {
		t.Fatal("dopo Forget la scheda non deve più risultare")
	}
	if err := s.Forget("inesistente"); err != nil {
		t.Fatalf("Forget su id assente non deve fallire: %v", err)
	}
}

func TestBackupWrittenOnOverwrite(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RememberOriginal("a", "A", "02:00:00:00:00:01"); err != nil {
		t.Fatal(err)
	}
	// La seconda scrittura (scheda diversa) deve lasciare un .bak del file
	// precedente: rete di sicurezza minima richiesta dalle regole del workspace.
	if err := s.RememberOriginal("b", "B", "02:00:00:00:00:02"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName+".bak")); err != nil {
		t.Fatalf("atteso un file .bak dopo la seconda scrittura: %v", err)
	}
}

func TestConfigDirRespectsEnv(t *testing.T) {
	want := t.TempDir()
	t.Setenv("NETWORKTOOLKIT_CONFIG_DIR", want)
	got, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ConfigDir = %q, atteso %q dalla variabile d'ambiente", got, want)
	}
}

func TestCorruptFileDoesNotBlockOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{ non-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("un file corrotto non deve impedire l'apertura: %v", err)
	}
	if ids := s.IDs(); len(ids) != 0 {
		t.Fatalf("da un file corrotto si riparte vuoti, trovati %v", ids)
	}
	// Il file corrotto va conservato, non distrutto.
	if _, err := os.Stat(filepath.Join(dir, fileName+".corrupt")); err != nil {
		t.Fatalf("il file corrotto doveva essere conservato in .corrupt: %v", err)
	}
}
