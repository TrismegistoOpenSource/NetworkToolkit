// SPDX-License-Identifier: MIT

//go:build windows

package elevate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"networktoolkit/internal/changer"
)

// Il binario dei test fa anche da helper, come l'exe vero (main.go).
func TestMain(m *testing.M) {
	if IsHelperInvocation(os.Args) {
		os.Exit(RunHelper(os.Args))
	}
	os.Exit(m.Run())
}

// Tutto il giro GUI → helper di Run (ShellExecuteEx, argomenti, file di
// risposta in %TEMP%), con l'exe e %TEMP% in cartelle con spazi e accenti,
// come per un utente "Mario Rossi" o "Nicolò". Solo il verbo è "open" invece
// di "runas", per non avere il prompt UAC. La richiesta è malformata apposta:
// l'helper la rifiuta senza toccare la rete.
func TestRunConSpaziEAccenti(t *testing.T) {
	base := filepath.Join(t.TempDir(), "Nicolò con spazi")
	exe := filepath.Join(base, "programmi è", "NetworkToolkit prova.exe")
	tmp := filepath.Join(base, "temp à")
	for _, d := range []string{filepath.Dir(exe), tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(t, os.Args[0], exe)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	elevateVerb = "open"
	t.Cleanup(func() { elevateVerb = "runas" })

	req := changer.Request{Action: changer.ActionSet, Targets: []changer.Target{
		{ID: "id con spazi", Name: "prova", Kind: "ethernet", MAC: "02:00:00:00:00:01"},
	}}
	resp, err := Run(exe, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].OK ||
		!strings.Contains(resp.Results[0].Message, "richiesta non valida") {
		t.Fatalf("risposta inattesa dall'helper: %+v", resp)
	}
	// il file di risposta in %TEMP% va tolto
	if left, _ := filepath.Glob(filepath.Join(tmp, "ntk-result-*")); len(left) > 0 {
		t.Fatalf("file di risposta rimasti: %v", left)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
