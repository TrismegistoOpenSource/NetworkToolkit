// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"sync"
	"time"

	"networktoolkit/internal/autoverifica"
	"networktoolkit/internal/store"
)

// releaseRepo è il repository delle release pubbliche, da cui l'autoverifica
// scarica la firma (internal/autoverifica).
const releaseRepo = "TrismegistoOpenSource/NetworkToolkit"

// integrityState tiene l'esito dell'autoverifica all'avvio.
type integrityState struct {
	once  sync.Once
	state autoverifica.Stato
}

// VerifyIntegrity controlla che i file del programma siano integri e, se la
// release è pubblica, firmati da chi la pubblica. Il frontend la chiama dopo il
// primo disegno, quindi non ritarda l'avvio; gira una volta sola e le chiamate
// successive restituiscono lo stesso esito. L'ultimo esito resta scritto nella
// cartella di configurazione (autoverifica-stato.json).
func (a *App) VerifyIntegrity() autoverifica.Stato {
	a.integrity.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		dir, _ := store.ConfigDir()
		write := func(s autoverifica.Stato) {
			if dir != "" {
				_ = autoverifica.ScriviStato(dir, s)
			}
		}
		a.integrity.state = autoverifica.Verifica(ctx, autoverifica.Configurazione{
			Versione:     Version,
			Repo:         releaseRepo,
			CartellaDati: dir,
			Avanzamento:  write, // l'esito locale è su disco subito, prima del controllo in rete
		})
		write(a.integrity.state)
	})
	return a.integrity.state
}
