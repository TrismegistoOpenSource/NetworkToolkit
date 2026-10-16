// SPDX-License-Identifier: MIT

//go:build !linux

package vpn

import "os/exec"

// Su macOS e Windows i toolkit non esistono: nessun processo figlio da legare.
func configureChild(_ *exec.Cmd) {}
