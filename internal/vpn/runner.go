// SPDX-License-Identifier: MIT

package vpn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Codici di uscita di pkexec che non vengono dal comando: la finestra di
// polkit chiusa o l'autorizzazione negata (126), l'autenticazione fallita
// (127). Vedi pkexec(1).
const (
	pkexecDismissed  = 126
	pkexecAuthFailed = 127
)

// Outcome è l'esito di un comando lanciato con la console.
type Outcome struct {
	Exit       int    // codice di uscita; -1 se il comando non è partito
	Stdout     string // tutto lo stdout, per chi deve leggerlo dopo
	Stderr     string
	Canceled   bool  // pkexec: finestra chiusa o autorizzazione negata
	AuthFailed bool  // pkexec: password sbagliata
	Err        error // il comando non è partito o è stato interrotto
}

// OK dice se il comando è partito ed è uscito con 0.
func (o Outcome) OK() bool { return o.Err == nil && o.Exit == 0 }

// Runner lancia i comandi mostrandoli in console: prima la riga di comando
// esatta, poi stdout e stderr riga per riga mentre arrivano, ognuna con l'ora.
type Runner struct {
	Toolkit string
	Emit    Emitter
	Now     func() time.Time
}

func (r *Runner) emit(stream, text string) {
	if r.Emit == nil {
		return
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	r.Emit(Line{Toolkit: r.Toolkit, Time: now().Format("15:04:05"), Stream: stream, Text: text})
}

// Info scrive in console una riga dell'app (non di un comando).
func (r *Runner) Info(format string, a ...any) { r.emit(StreamInfo, fmt.Sprintf(format, a...)) }

// Job è un comando in corso.
type Job struct {
	done    chan struct{}
	outcome Outcome
}

// Done si chiude quando il comando è finito e il suo output è tutto in console.
func (j *Job) Done() <-chan struct{} { return j.done }

// Outcome va letto dopo Done.
func (j *Job) Outcome() Outcome { <-j.done; return j.outcome }

// Run lancia il comando e ne aspetta la fine. Con elevated passa da pkexec,
// che apre la finestra della password di polkit.
func (r *Runner) Run(ctx context.Context, elevated bool, name string, args ...string) Outcome {
	return r.Start(ctx, elevated, nil, name, args...).Outcome()
}

// Start lancia il comando senza aspettarlo. onLine, se c'è, vede ogni riga di
// output (serve a `tailscale up` per accorgersi della pagina di login senza
// aspettare la fine del comando).
func (r *Runner) Start(ctx context.Context, elevated bool, onLine func(stream, text string), name string, args ...string) *Job {
	argv := append([]string{name}, args...)
	if elevated {
		argv = append([]string{"pkexec"}, argv...)
	}
	job := &Job{done: make(chan struct{})}
	r.emit(StreamCmd, QuoteArgs(argv))

	finish := func(o Outcome) {
		job.outcome = o
		switch {
		case o.Err != nil:
			r.emit(StreamInfo, "non eseguito: "+o.Err.Error())
		case elevated && o.Exit == pkexecDismissed:
			r.emit(StreamInfo, "annullato: finestra della password chiusa o autorizzazione negata")
		case elevated && o.Exit == pkexecAuthFailed:
			r.emit(StreamInfo, "autenticazione fallita")
		default:
			r.emit(StreamInfo, fmt.Sprintf("[uscita %d]", o.Exit))
		}
		close(job.done)
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = nil // /dev/null: nessun comando deve aspettare la tastiera
	configureChild(cmd)
	// Se il comando lascia un figlio che tiene aperto l'output (o non muore
	// quando lo si ferma), Wait non resta appeso: dopo questo tempo chiude le
	// pipe e torna.
	cmd.WaitDelay = 3 * time.Second

	var outBuf, errBuf strings.Builder
	line := func(stream string, buf *strings.Builder) *lineWriter {
		return &lineWriter{fn: func(s string) {
			buf.WriteString(s)
			text := strings.TrimRight(s, "\r\n")
			r.emit(stream, text)
			if onLine != nil {
				onLine(stream, text)
			}
		}}
	}
	outW, errW := line(StreamOut, &outBuf), line(StreamErr, &errBuf)
	cmd.Stdout, cmd.Stderr = outW, errW
	if err := cmd.Start(); err != nil {
		finish(Outcome{Exit: -1, Err: startError(argv[0], err)})
		return job
	}

	go func() {
		werr := cmd.Wait()
		// L'ultima riga può non avere l'a capo finale: non va persa.
		outW.flush()
		errW.flush()
		o := Outcome{Exit: 0, Stdout: outBuf.String(), Stderr: errBuf.String()}
		var ee *exec.ExitError
		switch {
		case werr == nil:
		case errors.As(werr, &ee):
			o.Exit = ee.ExitCode()
			if ctx.Err() != nil {
				o.Err = fmt.Errorf("interrotto alla chiusura")
			}
		case ctx.Err() != nil:
			o.Exit = -1
			o.Err = fmt.Errorf("interrotto alla chiusura")
		default:
			o.Exit = -1
			o.Err = werr
		}
		if elevated {
			o.Canceled = o.Exit == pkexecDismissed
			o.AuthFailed = o.Exit == pkexecAuthFailed
		}
		finish(o)
	}()
	return job
}

// lineWriter riceve l'output di un comando e lo consegna una riga alla
// volta, appena la riga è completa. exec chiama Write da una sola goroutine
// per writer, e flush solo dopo Wait: nessun lock necessario.
type lineWriter struct {
	fn  func(string)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.fn(string(w.buf[:i+1]))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.fn(string(w.buf))
		w.buf = nil
	}
}

func startError(prog string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s non è installato", prog)
	}
	return err
}
