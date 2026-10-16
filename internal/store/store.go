// SPDX-License-Identifier: MIT

// Package store ricorda il MAC di fabbrica di ogni scheda, così il tasto
// "Ripristina" funziona anche dove l'OS non espone il MAC permanente (macOS su
// interfacce virtuali, Windows). È l'unico stato che l'app scrive su disco.
//
// La cartella è redirigibile via NETWORKTOOLKIT_CONFIG_DIR: i test devono
// sempre puntarla a t.TempDir(), MAI al percorso reale dell'utente. Prima di
// ogni riscrittura si tiene una copia .bak, e il salvataggio è atomico
// (scrittura su file temporaneo + rename), per non lasciare mai un file di
// configurazione mezzo scritto.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Entry è ciò che ricordiamo per una scheda.
type Entry struct {
	OriginalMAC string    `json:"originalMac"`
	Name        string    `json:"name"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Store è la mappa id-scheda → Entry, protetta da mutex e persistita su file.
type Store struct {
	dir  string
	path string
	mu   sync.Mutex
	data map[string]Entry
}

const fileName = "originals.json"

// ConfigDir restituisce la cartella di configurazione, rispettando
// l'override NETWORKTOOLKIT_CONFIG_DIR.
func ConfigDir() (string, error) {
	if d := os.Getenv("NETWORKTOOLKIT_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "NetworkToolkit"), nil
}

// Open apre (o crea) lo store nella cartella di configurazione.
func Open() (*Store, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return OpenAt(dir)
}

// OpenAt apre lo store in una cartella specifica. È il punto d'ingresso dei
// test, che passano t.TempDir().
func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:  dir,
		path: filepath.Join(dir, fileName),
		data: map[string]Entry{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // primo avvio: mappa vuota
		}
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	m := map[string]Entry{}
	if err := json.Unmarshal(raw, &m); err != nil {
		// Un file corrotto non deve impedire l'avvio né sovrascrivere subito i
		// dati: si riparte da vuoto ma si conserva il file com'è in .corrupt.
		_ = os.Rename(s.path, s.path+".corrupt")
		return nil
	}
	s.data = m
	return nil
}

// Original restituisce il MAC di fabbrica ricordato per la scheda.
func (s *Store) Original(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[id]
	if !ok {
		return "", false
	}
	return e.OriginalMAC, e.OriginalMAC != ""
}

// RememberOriginal registra il MAC di fabbrica di una scheda, solo se non ne
// abbiamo già uno: il primo valore visto (prima di qualunque nostra modifica) è
// quello buono, i successivi potrebbero essere già alterati.
func (s *Store) RememberOriginal(id, name, mac string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.data[id]; ok && e.OriginalMAC != "" {
		return nil
	}
	s.data[id] = Entry{OriginalMAC: mac, Name: name, UpdatedAt: time.Now()}
	return s.save()
}

// Forget dimentica una scheda (usato dopo un ripristino andato a buon fine, se
// si vuole ripartire pulito). Innocuo se la scheda non c'è.
func (s *Store) Forget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return nil
	}
	delete(s.data, id)
	return s.save()
}

// save scrive la mappa in modo atomico, con backup .bak del file precedente.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stat(s.path); err == nil {
		// Copia di sicurezza prima di toccare il file buono.
		if prev, rerr := os.ReadFile(s.path); rerr == nil {
			_ = os.WriteFile(s.path+".bak", prev, 0o600)
		}
	}
	tmp, err := os.CreateTemp(s.dir, fileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("salvataggio store fallito: %w", err)
	}
	return nil
}

// IDs elenca gli id ricordati (ordinati), utile a diagnostica e test.
func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.data))
	for id := range s.data {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
