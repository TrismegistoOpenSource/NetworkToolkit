// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"networktoolkit/internal/speedtest"
)

// SpeedtestHelperArg mette il binario in modo headless: nessuna finestra,
// nessun privilegio (a differenza del modo __apply del MAC changer, leggere i
// contatori di rete e fare richieste HTTP non richiede root/admin). Stampa
// una riga al secondo su stdout, che la GUI legge in streaming lanciando
// questo stesso binario come processo figlio (internal/speedtest è il
// "livello 1", questo file è solo il collante da riga di comando).
const SpeedtestHelperArg = "__speedtest"

// IsSpeedtestInvocation dice se il processo è stato lanciato in modo headless.
func IsSpeedtestInvocation(args []string) bool {
	return len(args) >= 2 && args[1] == SpeedtestHelperArg
}

// RunSpeedtestHeadless genera traffico verso Cloudflare e stampa una riga al
// secondo del tipo "DOWN 123.45 UP 6.78" (Mbps, due decimali) finché il
// processo non viene terminato (chiusura della finestra → la GUI uccide
// questo processo, altrimenti continuerebbe a generare traffico a vuoto).
func RunSpeedtestHeadless() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Guinzaglio: la GUI ci passa uno stdin che non scrive mai. Finché il padre
	// è vivo questa lettura resta bloccata; nell'istante in cui muore — anche
	// ucciso di forza — il sistema operativo chiude la pipe, la lettura
	// restituisce EOF e usciamo. È ciò che impedisce a un processo che genera
	// traffico di sopravvivere alla finestra che lo ha avviato.
	// (Lanciato a mano da terminale, l'attesa è sul tty: si esce con Ctrl-D.)
	ctx, parentGone := context.WithCancel(ctx)
	defer parentGone()
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		parentGone()
	}()

	counters, err := speedtest.NewOSCounters()
	if err != nil {
		fmt.Fprintln(os.Stderr, "contatori di rete non disponibili:", err)
		return 1
	}

	out := make(chan speedtest.Sample)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	go func() {
		for s := range out {
			fmt.Fprintf(w, "DOWN %.2f UP %.2f\n", s.DownMbps, s.UpMbps)
			w.Flush() // la GUI legge riga per riga in streaming: niente buffering nascosto
		}
	}()

	if err := speedtest.Run(ctx, counters, out); err != nil {
		close(out)
		fmt.Fprintln(os.Stderr, "motore di misura interrotto:", err)
		return 1
	}
	close(out)
	return 0
}
