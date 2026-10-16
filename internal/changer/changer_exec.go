// SPDX-License-Identifier: MIT

//go:build darwin || linux

package changer

import (
	"fmt"
	"os/exec"
	"strings"
)

// run esegue un comando esterno, ne registra l'esito nel log e mappa
// l'eventuale errore includendo l'output (dove i comandi di rete scrivono la
// causa vera). Vale per macOS e Linux, che usano strumenti da riga di comando
// per la parte di persistenza; Windows non lo usa.
func run(log *logbuf, name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed != "" {
			return fmt.Errorf("%s %s: %v — %s", name, strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return nil
}

// classify traduce un errore di basso livello in uno degli errori tipizzati,
// guardando le frasi che i tool/di kernel usano quando negano il permesso o
// rifiutano il MAC.
func classify(err error) error {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "operation not permitted"),
		strings.Contains(s, "permission denied"),
		strings.Contains(s, "not permitted"):
		return fmt.Errorf("%w (%v)", ErrPermission, err)
	case strings.Contains(s, "not supported"),
		strings.Contains(s, "operation not supported"): // EOPNOTSUPP: driver Wi-Fi che rifiuta MAC non locali
		return fmt.Errorf("%w (%v)", ErrDriverRejected, err)
	case strings.Contains(s, "can't assign"),
		strings.Contains(s, "invalid argument"):
		return fmt.Errorf("%w (%v)", ErrDriverRejected, err)
	default:
		return fmt.Errorf("%w: %v", ErrDriverRejected, err)
	}
}

// sanitize riduce un id a lettere/numeri/punti/trattini per usarlo in un nome
// file o in un label senza rompere script o path.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
