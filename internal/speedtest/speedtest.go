// SPDX-License-Identifier: MIT

// Package speedtest è il motore di misura: headless, non sa nulla di GUI.
//
// Architettura a due livelli, come da richiesta esplicita: qui vive SOLO il
// livello 1 (genera traffico + legge i contatori dell'interfaccia + calcola
// Mbps). Il livello 2 (quadrante, ago, animazioni) sta nel frontend e non
// importa nulla da questo package — riceve solo un numero al secondo.
//
// Metodo di misura: la velocità NON si calcola contando i byte delle
// richieste HTTP (impreciso, l'overhead del protocollo distorce il numero).
// Si leggono invece i contatori RX/TX a basso livello che il sistema
// operativo tiene già per l'interfaccia di rete, prima e dopo un intervallo
// di 1s: il delta è la velocità reale in quel secondo, comprensiva di
// tutto il traffico generato (non solo dei payload che vediamo noi).
package speedtest

import (
	"context"
	"fmt"
	"time"
)

// Sample è una misura di velocità per un secondo di intervallo.
type Sample struct {
	DownMbps float64
	UpMbps   float64
}

// Counters legge i contatori cumulativi RX/TX di un'interfaccia, in byte
// dall'accensione del sistema. Implementato per-OS (counters_<goos>.go).
type Counters interface {
	// Read restituisce (byte ricevuti, byte inviati) sull'interfaccia più
	// attiva del sistema in questo momento.
	Read() (rxBytes, txBytes uint64, err error)
}

// Run genera traffico verso Cloudflare in continuo e scrive un Sample al
// secondo su out, finché ctx non viene annullato. È la funzione che il modo
// headless (main.go, argomento __speedtest) chiama direttamente; la GUI non
// la vede mai, legge solo le righe che il processo headless stampa.
func Run(ctx context.Context, counters Counters, out chan<- Sample) error {
	traffic := NewTraffic()
	defer traffic.Stop()

	traffic.Start(ctx)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	prevRx, prevTx, err := counters.Read()
	if err != nil {
		return fmt.Errorf("lettura contatori di rete fallita: %w", err)
	}
	prevAt := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			rx, tx, err := counters.Read()
			if err != nil {
				// Un fallimento isolato di lettura non deve fermare la
				// misura: si salta il campione, si riprova al giro dopo.
				continue
			}
			elapsed := now.Sub(prevAt).Seconds()
			sample := deltaSample(prevRx, prevTx, rx, tx, elapsed)
			prevRx, prevTx, prevAt = rx, tx, now

			select {
			case out <- sample:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// deltaSample calcola la velocità fra due letture dei contatori.
//
// I contatori del sistema operativo sono cumulativi e normalmente crescono,
// ma possono andare ALL'INDIETRO: interfaccia riattivata (riparte da zero) o
// overflow a 32 bit su certi driver virtuali. Sottraendo alla cieca si
// otterrebbe un numero enorme e privo di senso — in un misuratore di velocità
// significa un picco fasullo di decine di Gbps. In quel caso il campione vale
// zero: meglio un secondo "vuoto" che un numero inventato.
func deltaSample(prevRx, prevTx, rx, tx uint64, elapsed float64) Sample {
	if elapsed <= 0 {
		return Sample{}
	}
	var s Sample
	if rx >= prevRx {
		s.DownMbps = bytesToMbps(rx-prevRx, elapsed)
	}
	if tx >= prevTx {
		s.UpMbps = bytesToMbps(tx-prevTx, elapsed)
	}
	return s
}

// bytesToMbps converte un delta di byte in un intervallo di secondi in
// megabit al secondo (Mbps), l'unità con cui si esprime comunemente la
// velocità di rete (megabit, non megabyte: byte*8/1_000_000).
func bytesToMbps(deltaBytes uint64, seconds float64) float64 {
	return float64(deltaBytes) * 8 / 1_000_000 / seconds
}
