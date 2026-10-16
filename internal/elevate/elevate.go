// SPDX-License-Identifier: MIT

// Package elevate esegue l'helper privilegiato: lo stesso binario, rilanciato
// in "modo helper" con privilegi di amministratore. La GUI resta non
// privilegiata (obbligatorio su macOS, buona norma ovunque) e chiede la
// password una sola volta per ogni switch, tramite il dialog nativo dell'OS.
//
// Protocollo, identico sui tre sistemi:
//
//	<exe> __apply <richiesta-base64> <file-di-uscita>
//
// L'helper decodifica la richiesta, applica i cambi e scrive la Response JSON
// nel file di uscita (0644, così la GUI non privilegiata può rileggerla). Si
// usa un file invece dello stdout perché su Windows l'elevazione via
// ShellExecute non lo cattura: un unico meccanismo per tutti evita divergenze.
package elevate

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"networktoolkit/internal/changer"
)

// HelperArg è il primo argomento che mette il binario in modo helper.
const HelperArg = "__apply"

// IsHelperInvocation dice se questo processo è stato lanciato come helper.
func IsHelperInvocation(args []string) bool {
	return len(args) >= 2 && args[1] == HelperArg
}

// RunHelper esegue il modo helper: decodifica la richiesta dall'argomento
// base64, applica i cambi e scrive la Response nel file indicato. Restituisce
// il codice di uscita del processo.
func RunHelper(args []string) int {
	if len(args) < 4 {
		fmt.Fprintln(os.Stderr, "uso: <exe> __apply <richiesta-base64> <file-uscita>")
		return 2
	}
	raw, err := base64.StdEncoding.DecodeString(args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "richiesta non decodificabile:", err)
		return 2
	}
	var req changer.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		fmt.Fprintln(os.Stderr, "richiesta non valida:", err)
		return 2
	}

	resp := changer.Apply(req)

	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "serializzazione risposta fallita:", err)
		return 1
	}
	if err := os.WriteFile(args[3], data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "scrittura risposta fallita:", err)
		return 1
	}
	return 0
}

// ErrCanceled indica che l'utente ha annullato il dialog della password.
var ErrCanceled = fmt.Errorf("operazione annullata")

// Run costruisce la richiesta, lancia l'helper elevato e restituisce la
// Response. Un fallimento di elevazione (password annullata, pkexec assente)
// torna come errore; i fallimenti per singola scheda stanno dentro la Response.
func Run(exe string, req changer.Request) (changer.Response, error) {
	var zero changer.Response

	payload, err := json.Marshal(req)
	if err != nil {
		return zero, err
	}
	b64 := base64.StdEncoding.EncodeToString(payload)

	out, err := os.CreateTemp("", "ntk-result-*.json")
	if err != nil {
		return zero, err
	}
	outPath := out.Name()
	out.Close()
	defer os.Remove(outPath)

	if err := platformElevate(exe, b64, outPath); err != nil {
		return zero, err
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return zero, fmt.Errorf("helper terminato senza produrre un risultato: %w", err)
	}
	var resp changer.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return zero, fmt.Errorf("risposta dell'helper illeggibile: %w", err)
	}
	return resp, nil
}
