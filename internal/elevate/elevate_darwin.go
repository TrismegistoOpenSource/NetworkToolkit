// SPDX-License-Identifier: MIT

//go:build darwin

package elevate

import (
	"os/exec"
	"strings"
)

// platformElevate usa il dialog nativo di macOS via osascript. La GUI non gira
// mai da root (non è supportato da Apple): eleva solo il piccolo helper.
//
// Il comando shell viene passato come primo argomento allo script (item 1 of
// argv) invece di essere interpolato nel sorgente AppleScript, così non serve
// escaparlo per AppleScript; le parti con possibili spazi (percorso eseguibile
// e file di uscita) sono quotate per la shell, il base64 non ne ha bisogno.
func platformElevate(exe, b64, outPath string) error {
	cmd := shellQuote(exe) + " " + HelperArg + " " + b64 + " " + shellQuote(outPath)
	prompt := "NetworkToolkit deve cambiare l'indirizzo MAC e richiede privilegi di amministratore."

	c := exec.Command("osascript",
		"-e", "on run argv",
		"-e", "do shell script (item 1 of argv) with administrator privileges with prompt (item 2 of argv)",
		"-e", "end run",
		cmd, prompt,
	)
	out, err := c.CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "User canceled") || strings.Contains(s, "-128") {
			return ErrCanceled
		}
		return errWithOutput(err, s)
	}
	return nil
}

func shellQuote(s string) string {
	// Quoting POSIX robusto: racchiude tra apici singoli ed escapa gli apici
	// singoli interni come '\''.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
