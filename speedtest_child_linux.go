// SPDX-License-Identifier: MIT

//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// configureSpeedtestChild aggiunge, su Linux, la rete di sicurezza più forte
// possibile: Pdeathsig fa uccidere il figlio DAL KERNEL nel momento in cui il
// padre muore, qualunque sia la causa. È un rinforzo del guinzaglio su stdin
// (che funziona su tutti e tre gli OS), non un sostituto.
func configureSpeedtestChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
