// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// windowsOptions separa il profilo di WebView2 (cache, cookie, localStorage)
// da quello vero dell'utente quando la configurazione è rediretta con
// NETWORKTOOLKIT_CONFIG_DIR, cioè nelle prove: una prova non deve sporcare né
// leggere il profilo del programma installato. Con la configurazione normale
// resta il comportamento di Wails. Fuori da Windows l'opzione è ignorata.
func windowsOptions() *windows.Options {
	d := os.Getenv("NETWORKTOOLKIT_CONFIG_DIR")
	if d == "" {
		return nil
	}
	return &windows.Options{WebviewUserDataPath: filepath.Join(d, "webview")}
}
