// SPDX-License-Identifier: MIT

//go:build !linux

package main

import "os/exec"

// Su macOS e Windows non esiste un equivalente di Pdeathsig: la garanzia che
// il figlio non sopravviva al padre è affidata interamente al guinzaglio su
// stdin (vedi StartSpeedtest e cmd_speedtest.go), che però funziona
// identico su tutti e tre i sistemi.
func configureSpeedtestChild(_ *exec.Cmd) {}
