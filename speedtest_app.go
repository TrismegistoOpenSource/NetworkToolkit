// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Eventi verso il frontend. Il livello 2 (quadranti) non sa nulla di rete:
// riceve solo numeri già pronti.
const (
	EventSpeedtestSample  = "speedtest:sample"
	EventSpeedtestStopped = "speedtest:stopped"
	EventSpeedtestError   = "speedtest:error"
)

// SpeedtestSample è una misura al secondo, come la riceve il frontend.
type SpeedtestSample struct {
	DownMbps float64 `json:"downMbps"`
	UpMbps   float64 `json:"upMbps"`
}

// speedtestSession tiene il processo figlio headless e il suo guinzaglio.
type speedtestSession struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser // mai scritto: serve solo a far morire il figlio col padre
	gen   uint64         // generazione, per ignorare i campioni di una sessione sostituita
}

// StartSpeedtest lancia il motore di misura come processo figlio e comincia a
// inoltrare i campioni al frontend. Ritorna subito: i numeri arrivano via
// evento, uno al secondo.
func (a *App) StartSpeedtest() error {
	a.speedMu.Lock()
	defer a.speedMu.Unlock()

	if a.speedSess != nil {
		return nil // già in corso: un secondo avvio è un no-op, non un errore
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("percorso eseguibile non determinabile: %w", err)
	}

	cmd := exec.Command(exe, SpeedtestHelperArg)

	// Il guinzaglio: lo stdin del figlio resta aperto e non ci scriviamo mai.
	// Se questo processo muore — anche ucciso di forza, senza poter eseguire
	// alcuna callback di chiusura — il sistema operativo chiude la pipe, il
	// figlio legge EOF ed esce da solo. Senza questo, una GUI terminata male
	// lascerebbe un processo che continua a scaricare a vuoto per sempre.
	// (La correttezza non deve dipendere dall'esecuzione di una callback:
	// PROBLEMI-NOTI.md §13.)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	configureSpeedtestChild(cmd) // rinforzo per-OS (Pdeathsig su Linux)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("avvio del motore di misura fallito: %w", err)
	}

	a.speedGen++
	sess := &speedtestSession{cmd: cmd, stdin: stdin, gen: a.speedGen}
	a.speedSess = sess

	go a.pumpSpeedtest(sess, stdout, stderr)
	return nil
}

// StopSpeedtest ferma il motore.
func (a *App) StopSpeedtest() {
	a.speedMu.Lock()
	sess := a.speedSess
	a.speedSess = nil
	a.speedMu.Unlock()

	stopSpeedtestSession(sess)
}

func stopSpeedtestSession(sess *speedtestSession) {
	if sess == nil {
		return
	}
	// Chiudere lo stdin fa uscire il figlio spontaneamente (legge EOF); il Kill
	// subito dopo è la garanzia che smetta *adesso*: non ha nulla da salvare,
	// e un processo che genera traffico non va lasciato a discrezione dei tempi
	// di uscita.
	_ = sess.stdin.Close()
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
	_ = sess.cmd.Wait()
}

// pumpSpeedtest legge le righe del figlio e le trasforma in eventi. Una riga
// ha la forma "DOWN 123.45 UP 6.78".
func (a *App) pumpSpeedtest(sess *speedtestSession, stdout, stderr io.ReadCloser) {
	// Lo stderr del figlio viene raccolto a parte: se muore, il motivo va
	// mostrato in chiaro invece di lasciare i quadranti fermi senza spiegazione.
	var errBuf strings.Builder
	var errMu sync.Mutex
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			errMu.Lock()
			errBuf.WriteString(sc.Text())
			errBuf.WriteString("\n")
			errMu.Unlock()
		}
	}()

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		var down, up float64
		if _, err := fmt.Sscanf(sc.Text(), "DOWN %f UP %f", &down, &up); err != nil {
			continue // riga inattesa: si ignora, non si inventa un numero
		}
		if !a.speedtestIsCurrent(sess) {
			return // sessione già sostituita: i suoi campioni non valgono più
		}
		wruntime.EventsEmit(a.ctx, EventSpeedtestSample, SpeedtestSample{DownMbps: down, UpMbps: up})
	}

	// Lo stdout si è chiuso: il processo è finito (per Stop, o perché è morto).
	_ = sess.cmd.Wait()

	if !a.speedtestIsCurrent(sess) {
		return // fermato da noi o rimpiazzato: nessun errore da segnalare
	}

	a.speedMu.Lock()
	a.speedSess = nil
	a.speedMu.Unlock()

	errMu.Lock()
	reason := strings.TrimSpace(errBuf.String())
	errMu.Unlock()
	if reason == "" {
		reason = "il motore di misura si è chiuso inaspettatamente"
	}
	wruntime.EventsEmit(a.ctx, EventSpeedtestError, reason)
	wruntime.EventsEmit(a.ctx, EventSpeedtestStopped, nil)
}

// speedtestIsCurrent dice se la sessione è ancora quella attiva: protegge dal
// caso in cui l'utente fermi e riavvii in fretta e i campioni della sessione
// vecchia arrivino in ritardo, sovrascrivendo quelli nuovi.
func (a *App) speedtestIsCurrent(sess *speedtestSession) bool {
	a.speedMu.Lock()
	defer a.speedMu.Unlock()
	return a.speedSess != nil && a.speedSess.gen == sess.gen
}

// shutdownSpeedtest è chiamata alla chiusura della finestra: il processo di
// misura non deve mai sopravvivere alla GUI.
func (a *App) shutdownSpeedtest() {
	a.StopSpeedtest()
}
