// SPDX-License-Identifier: MIT

//go:build linux

package elevate

import (
	"fmt"
	"os/exec"
)

// platformElevate usa pkexec, che apre il dialog grafico di polkit. È la via
// giusta da una GUI: sudo/doas vogliono un terminale e non propagano
// $DISPLAY/$XAUTHORITY. pkexec passa gli argomenti direttamente al programma,
// quindi non serve alcun quoting per la shell.
func platformElevate(exe, b64, outPath string) error {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec non trovato: installa polkit, oppure esegui l'helper da terminale con sudo (%w)", err)
	}
	c := exec.Command("pkexec", exe, HelperArg, b64, outPath)
	out, err := c.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			switch ee.ExitCode() {
			case 126:
				return ErrCanceled // autorizzazione negata o dialog chiuso
			case 127:
				return fmt.Errorf("autenticazione pkexec fallita")
			}
		}
		return errWithOutput(err, string(out))
	}
	return nil
}
