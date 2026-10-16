// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"networktoolkit/internal/vpn"
)

// EventVPNConsole porta al frontend le righe di console dei toolkit Tailscale
// e OpenVPN (riga di comando, stdout, stderr), mentre i comandi girano.
const EventVPNConsole = "vpn:console"

// I toolkit Tailscale e OpenVPN esistono solo su Linux (internal/vpn). Questi
// metodi ci sono su tutti i sistemi perché i binding del frontend sono gli
// stessi; fuori da Linux VPNToolkits dice che non ci sono e il frontend non
// mostra i due menu.

func (a *App) emitVPN(l vpn.Line) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, EventVPNConsole, l)
	}
}

// VPNToolkits dice quali toolkit mostrare.
func (a *App) VPNToolkits() vpn.Availability { return a.vpn.Availability() }

// TailscaleState legge lo stato del servizio, della connessione e della rete.
func (a *App) TailscaleState() vpn.TailscaleState { return a.vpn.TailscaleState() }

// TailscaleService: start, stop, enable, disable di tailscaled (con pkexec).
func (a *App) TailscaleService(action string) vpn.ActionResult {
	return a.vpn.TailscaleService(action)
}

// TailscaleConnection: up o down.
func (a *App) TailscaleConnection(action string) vpn.ActionResult {
	return a.vpn.TailscaleConnection(action)
}

// OpenVPNState elenca i tunnel con il loro stato.
func (a *App) OpenVPNState() vpn.OpenVPNState { return a.vpn.OpenVPNState() }

// OpenVPNTunnel: start, stop, enable, disable del tunnel (unità systemd).
func (a *App) OpenVPNTunnel(unit, action string) vpn.ActionResult {
	return a.vpn.OpenVPNTunnel(unit, action)
}

// OpenVPNSearchPrivileged elenca con pkexec le cartelle dei tunnel chiuse.
func (a *App) OpenVPNSearchPrivileged() vpn.ActionResult { return a.vpn.OpenVPNSearchPrivileged() }

// PublicIP legge l'IP pubblico con curl, mostrandolo nella console del toolkit.
func (a *App) PublicIP(toolkit string) vpn.ActionResult { return a.vpn.PublicIP(toolkit) }

// OpenURL apre nel browser la pagina di login di Tailscale. Solo https: il
// frontend non può far aprire altro.
func (a *App) OpenURL(url string) error {
	u := vpn.SafeURL(url)
	if u == "" {
		return fmt.Errorf("indirizzo non valido")
	}
	wruntime.BrowserOpenURL(a.ctx, u)
	return nil
}
