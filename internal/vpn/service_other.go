// SPDX-License-Identifier: MIT

//go:build !linux

package vpn

// Su macOS e Windows Tailscale e OpenVPN hanno le loro interfacce ufficiali:
// i toolkit non esistono. Service ha gli stessi metodi della versione Linux
// solo perché i binding verso il frontend sono gli stessi su tutti i sistemi;
// Availability dice che non ci sono, e il frontend non li mostra.

const notOnThisOS = "disponibile solo su Linux"

// Service è vuoto fuori da Linux.
type Service struct{}

// New crea il servizio (vuoto).
func New(_ Emitter) *Service { return &Service{} }

// Shutdown non ha niente da fermare.
func (s *Service) Shutdown() {}

// Availability: nessun toolkit.
func (s *Service) Availability() Availability { return Availability{} }

// TailscaleState: non supportato.
func (s *Service) TailscaleState() TailscaleState {
	return TailscaleState{Message: notOnThisOS}
}

// TailscaleService: non supportato.
func (s *Service) TailscaleService(string) ActionResult { return ActionResult{Message: notOnThisOS} }

// TailscaleConnection: non supportato.
func (s *Service) TailscaleConnection(string) ActionResult { return ActionResult{Message: notOnThisOS} }

// OpenVPNState: non supportato.
func (s *Service) OpenVPNState() OpenVPNState { return OpenVPNState{Message: notOnThisOS} }

// OpenVPNTunnel: non supportato.
func (s *Service) OpenVPNTunnel(string, string) ActionResult {
	return ActionResult{Message: notOnThisOS}
}

// OpenVPNSearchPrivileged: non supportato.
func (s *Service) OpenVPNSearchPrivileged() ActionResult { return ActionResult{Message: notOnThisOS} }

// PublicIP: non supportato.
func (s *Service) PublicIP(string) ActionResult { return ActionResult{Message: notOnThisOS} }
