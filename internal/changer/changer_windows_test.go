// SPDX-License-Identifier: MIT

//go:build windows

package changer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// L'helper è l'exe di Wails, un programma GUI senza console: un powershell
// lanciato senza CREATE_NO_WINDOW si apre una console sua, e a ogni cambio di
// MAC compariva una finestra nera per tutto il Restart-NetAdapter.
//
// Per riprodurlo il test si rilancia SENZA console (DETACHED_PROCESS), come
// l'helper; da lì lancia powershell, che dice se ha una finestra di console.
// (Lanciato da un processo che ha già una console, powershell la erediterebbe
// e il test non distinguerebbe niente.)
func TestPowershellSenzaFinestra(t *testing.T) {
	const detachedProcess = 0x00000008
	cmd := exec.Command(os.Args[0], "-test.run=^TestPowershellSenzaFinestraFiglio$")
	cmd.Env = append(os.Environ(), "NTK_TEST_FIGLIO_SENZA_CONSOLE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("processo senza console: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "console=0\n") {
		t.Fatalf("powershell ha aperto una finestra di console: deve partire con CREATE_NO_WINDOW\n%s", out)
	}
}

// TestPowershellSenzaFinestraFiglio gira solo dentro TestPowershellSenzaFinestra.
func TestPowershellSenzaFinestraFiglio(t *testing.T) {
	if os.Getenv("NTK_TEST_FIGLIO_SENZA_CONSOLE") != "1" {
		t.Skip("solo come processo figlio")
	}
	probe := `Add-Type -Name W -Namespace N -MemberDefinition ` +
		`'[DllImport("kernel32.dll")] public static extern IntPtr GetConsoleWindow();'; ` +
		`"console=" + [N.W]::GetConsoleWindow()`
	out, err := powershell(probe).CombinedOutput()
	if err != nil {
		t.Fatalf("powershell: %v\n%s", err, out)
	}
	fmt.Printf("%s\n", strings.TrimSpace(string(out)))
}
