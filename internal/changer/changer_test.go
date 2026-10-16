// SPDX-License-Identifier: MIT

package changer

import (
	"errors"
	"strings"
	"testing"
)

// L'helper gira come root: una richiesta malformata va rifiutata PRIMA di
// toccare il sistema. Con queste richieste Apply non deve arrivare a
// platformSet/platformReset (che fallirebbero comunque senza root, ma il
// rifiuto deve essere il primo controllo, non un effetto collaterale).
func TestApplyRifiutaRichiesteMalformate(t *testing.T) {
	cases := []struct {
		name string
		req  Request
	}{
		{"a capo nell'ID (iniezione nel file .link)", Request{Action: ActionSet, Targets: []Target{
			{ID: "eth0\n[Match]\nOriginalName=*", Kind: "ethernet", MAC: "02:00:00:00:00:01"}}}},
		{"barra nell'ID (percorso)", Request{Action: ActionReset, Targets: []Target{
			{ID: "../../etc/x", Kind: "ethernet"}}}},
		{"ID vuoto", Request{Action: ActionSet, Targets: []Target{{ID: "", Kind: "wifi", MAC: "02:00:00:00:00:01"}}}},
		{"ID che inizia con un trattino", Request{Action: ActionSet, Targets: []Target{{ID: "-x", Kind: "wifi", MAC: "02:00:00:00:00:01"}}}},
		{"tipo sconosciuto", Request{Action: ActionSet, Targets: []Target{{ID: "eth0", Kind: "bluetooth", MAC: "02:00:00:00:00:01"}}}},
		{"MAC non valido", Request{Action: ActionSet, Targets: []Target{{ID: "eth0", Kind: "ethernet", MAC: "zz"}}}},
		{"MAC multicast", Request{Action: ActionSet, Targets: []Target{{ID: "eth0", Kind: "ethernet", MAC: "01:00:00:00:00:01"}}}},
		{"MAC di fabbrica non valido", Request{Action: ActionReset, Targets: []Target{{ID: "eth0", Kind: "ethernet", MAC: "x;y"}}}},
		{"azione sconosciuta", Request{Action: "rm", Targets: []Target{{ID: "eth0", Kind: "ethernet"}}}},
	}
	for _, c := range cases {
		resp := Apply(c.req)
		if len(resp.Results) != 1 || resp.Results[0].OK {
			t.Errorf("%s: accettata: %+v", c.name, resp.Results)
			continue
		}
		if strings.Contains(strings.Join(resp.Log, "\n"), "applico") ||
			strings.Contains(strings.Join(resp.Log, "\n"), "ripristino") {
			t.Errorf("%s: si è arrivati all'applicazione:\n%s", c.name, strings.Join(resp.Log, "\n"))
		}
		for _, l := range resp.Log {
			if strings.ContainsAny(l, "\n\r") {
				t.Errorf("%s: caratteri di controllo nel log: %q", c.name, l)
			}
		}
	}
}

func TestValidateTargetAccettaIDVeri(t *testing.T) {
	for _, id := range []string{"en0", "feth3", "eth0", "wlp0s20f3", "enx00e04c680001", "enp0s3",
		"{4D36E972-E325-11CE-BFC1-08002BE10318}", "eth0.100", "macvlan@eth0"} {
		if err := validateTarget(ActionSet, Target{ID: id, Kind: "ethernet", MAC: "02:00:00:00:00:01"}); err != nil {
			t.Errorf("%q rifiutato: %v", id, err)
		}
	}
	if err := validateTarget(ActionReset, Target{ID: "en0", Kind: "wifi"}); err != nil {
		t.Errorf("ripristino senza MAC: %v", err)
	}
	if err := validateTarget(ActionSet, Target{ID: "a b", Kind: "wifi", MAC: "02:00:00:00:00:01"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("errore tipizzato atteso, avuto %v", err)
	}
}
