// SPDX-License-Identifier: MIT

package speedtest

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestBytesToMbps(t *testing.T) {
	// 1 MB in un secondo = 8 Mbps (megabit, non megabyte).
	if got := bytesToMbps(1_000_000, 1); !almost(got, 8) {
		t.Fatalf("1 MB/s = %v Mbps, atteso 8", got)
	}
	// Lo stesso trasferimento in due secondi vale metà.
	if got := bytesToMbps(1_000_000, 2); !almost(got, 4) {
		t.Fatalf("1 MB in 2s = %v Mbps, atteso 4", got)
	}
}

func TestDeltaSampleValoreReale(t *testing.T) {
	// Numeri presi da una misura vera fatta con netstat su questa macchina:
	// 9.347.513 byte in un secondo erano ~74,78 Mbps.
	const prev = 23_073_177_797
	const now = 23_082_525_310
	s := deltaSample(prev, 0, now, 0, 1)
	if !almost(s.DownMbps, 74.78) {
		t.Fatalf("DownMbps = %v, atteso ~74.78", s.DownMbps)
	}
}

func TestDeltaSampleContatoreAllIndietro(t *testing.T) {
	// Interfaccia riattivata: il contatore riparte da zero. Sottraendo alla
	// cieca verrebbe fuori un picco fasullo enorme; il campione deve valere 0.
	s := deltaSample(5_000_000, 5_000_000, 1_000, 2_000, 1)
	if s.DownMbps != 0 {
		t.Fatalf("contatore RX all'indietro: DownMbps = %v, atteso 0", s.DownMbps)
	}
	if s.UpMbps != 0 {
		t.Fatalf("contatore TX all'indietro: UpMbps = %v, atteso 0", s.UpMbps)
	}
}

func TestDeltaSampleUnaDirezioneSola(t *testing.T) {
	// Solo RX torna indietro: TX deve restare valido, non azzerarsi per
	// solidarietà.
	s := deltaSample(5_000_000, 1_000_000, 1_000, 2_000_000, 1)
	if s.DownMbps != 0 {
		t.Fatalf("DownMbps = %v, atteso 0", s.DownMbps)
	}
	if !almost(s.UpMbps, 8) {
		t.Fatalf("UpMbps = %v, atteso 8", s.UpMbps)
	}
}

func TestDeltaSampleIntervalloNullo(t *testing.T) {
	// Un intervallo nullo o negativo (orologio che salta all'indietro) non
	// deve produrre né infiniti né NaN.
	for _, elapsed := range []float64{0, -1} {
		s := deltaSample(0, 0, 1_000_000, 1_000_000, elapsed)
		if s.DownMbps != 0 || s.UpMbps != 0 {
			t.Fatalf("elapsed=%v ha prodotto %+v, atteso tutto zero", elapsed, s)
		}
		if math.IsInf(s.DownMbps, 0) || math.IsNaN(s.DownMbps) {
			t.Fatalf("elapsed=%v ha prodotto un valore non finito", elapsed)
		}
	}
}
