// SPDX-License-Identifier: MIT

//go:build darwin || linux

package elevate

import (
	"fmt"
	"strings"
)

// errWithOutput allega alla causa la coda dell'output del comando di
// elevazione, dove finisce il motivo vero del fallimento.
func errWithOutput(err error, out string) error {
	out = strings.TrimSpace(out)
	if out == "" {
		return err
	}
	if len(out) > 800 {
		out = out[len(out)-800:]
	}
	return fmt.Errorf("%v — %s", err, out)
}
