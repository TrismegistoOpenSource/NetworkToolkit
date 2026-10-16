// SPDX-License-Identifier: MIT

package speedtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Endpoint pubblici di Cloudflare che alimentano speed.cloudflare.com: nessun
// token da procurarsi (a differenza di fast.com, che lo nasconde nella pagina
// e lo cambia) e nessun blocco geografico — Cloudflare risponde dal nodo edge
// più vicino automaticamente (verificato: dall'Italia risponde un nodo a
// Fiumicino). __down?bytes=N restituisce esattamente N byte; __up accetta
// qualunque corpo in POST.
const (
	downloadURL   = "https://speed.cloudflare.com/__down?bytes=%d"
	uploadURL     = "https://speed.cloudflare.com/__up"
	downloadChunk = 20_000_000 // 20 MB per richiesta: abbastanza per non far dominare la latenza di apertura connessione
	uploadChunk   = 8_000_000  // 8 MB per richiesta
	parallelism   = 4          // richieste in parallelo, per saturare connessioni che scalano su più stream
)

// Traffic genera traffico continuo di download+upload finché non viene
// fermato. Non misura nulla lui stesso: la misura vera arriva da Counters,
// che legge l'interfaccia indipendentemente da questo generatore — Traffic
// esiste solo per tenere occupata la rete.
type Traffic struct {
	client *http.Client
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewTraffic() *Traffic {
	return &Traffic{
		client: &http.Client{
			// Nessun timeout complessivo: le richieste vengono interrotte dal
			// context passato a Start, non da una scadenza fissa che
			// troncherebbe un trasferimento in corso a metà.
			Timeout: 0,
		},
	}
}

// Start lancia parallelism goroutine di download e parallelism di upload, che
// ripetono le richieste finché ctx non viene annullato.
func (t *Traffic) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	for i := 0; i < parallelism; i++ {
		t.wg.Add(1)
		go t.downloadLoop(ctx)
	}
	for i := 0; i < parallelism; i++ {
		t.wg.Add(1)
		go t.uploadLoop(ctx)
	}
}

// Stop interrompe tutte le richieste in volo e aspetta che le goroutine
// finiscano, così il processo headless può uscire pulito.
func (t *Traffic) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
}

func (t *Traffic) downloadLoop(ctx context.Context) {
	defer t.wg.Done()
	url := fmt.Sprintf(downloadURL, downloadChunk)
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		resp, err := t.client.Do(req)
		if err != nil {
			// Rete assente/interrotta: non ha senso martellare di richieste
			// che falliranno tutte. Piccola pausa prima di riprovare.
			sleepOrDone(ctx, 500*time.Millisecond)
			continue
		}
		// Il corpo va scaricato per intero e scartato: è il traffico stesso
		// che ci serve, il contenuto non ha alcun significato.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func (t *Traffic) uploadLoop(ctx context.Context) {
	defer t.wg.Done()
	payload := make([]byte, uploadChunk)
	_, _ = rand.Read(payload) // dati casuali: non comprimibili, il traffico reale non lo è quasi mai
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := t.client.Do(req)
		if err != nil {
			sleepOrDone(ctx, 500*time.Millisecond)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
