// SPDX-License-Identifier: MIT

//go:build linux

package vpn

import (
	"os/exec"
	"syscall"
)

// configureChild lega i comandi alla GUI:
//   - Pdeathsig: se la GUI muore, anche uccisa di forza, il kernel uccide il
//     comando (come per il motore dello speedtest);
//   - gruppo di processi proprio: fermandolo (chiusura dell'app) si fermano
//     anche i processi che il comando ha lanciato a sua volta.
//
// Niente di questo vale per pkexec: il segnale si azzera all'exec di un
// programma setuid, e un processo di root non si può fermare da utente. Per
// questo i comandi elevati che potrebbero aspettare a lungo hanno un loro
// limite di tempo (tailscale up --timeout).
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// -pid: tutto il gruppo. Un rifiuto (processo di root) si ignora.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
}
