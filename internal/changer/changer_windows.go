// SPDX-License-Identifier: MIT

//go:build windows

package changer

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"networktoolkit/internal/macaddr"
)

// Caps: su Windows il MAC si cambia scrivendo il valore di registro
// NetworkAddress, che è di per sé PERMANENTE (sopravvive al riavvio). Non
// esiste un equivalente "solo per questa sessione", quindi il cambio temporaneo
// non è offerto.
func Caps() Capabilities {
	return Capabilities{OS: "windows", CanTemporary: false, CanPermanent: true}
}

// La classe {4d36e972-...} raccoglie tutte le schede di rete. Ogni scheda è una
// sottochiave 0000, 0001, ... il cui valore NetCfgInstanceId è il GUID
// dell'interfaccia: è la chiave di join autorevole con l'ID che arriva dalla
// GUI (AdapterName di GetAdaptersAddresses).
const classPath = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`

func platformSet(t Target, _ bool, log *logbuf) error {
	// Il flag persistent è ignorato di proposito: su Windows la scrittura del
	// registro è comunque permanente (vedi Caps).
	hex, err := macaddr.Hex12(t.MAC)
	if err != nil {
		return err
	}
	if macaddr.IsLocallyAdministered(t.MAC) {
		log.add("  MAC locally-administered: accettato dai driver Wi-Fi/Intel (secondo nibble 2/6/A/E)")
	} else {
		log.add("  NOTA: MAC non locally-administered — alcune schede Wi-Fi lo ignorano in silenzio")
	}

	subkey, err := findSubkeyByGUID(t.ID)
	if err != nil {
		return err
	}
	if err := writeNetworkAddress(subkey, hex, log); err != nil {
		return err
	}
	return restartAdapter(t.ID, log)
}

func platformReset(t Target, log *logbuf) error {
	subkey, err := findSubkeyByGUID(t.ID)
	if err != nil {
		return err
	}
	if err := deleteNetworkAddress(subkey, log); err != nil {
		return err
	}
	return restartAdapter(t.ID, log)
}

func findSubkeyByGUID(guid string) (string, error) {
	base, err := registry.OpenKey(registry.LOCAL_MACHINE, classPath, registry.READ)
	if err != nil {
		return "", fmt.Errorf("%w: apertura chiave di classe (%v)", ErrPermission, err)
	}
	defer base.Close()
	names, err := base.ReadSubKeyNames(-1)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE, classPath+`\`+n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		id, _, _ := sk.GetStringValue("NetCfgInstanceId")
		sk.Close()
		if strings.EqualFold(id, guid) {
			return n, nil
		}
	}
	return "", fmt.Errorf("%w: nessuna sottochiave di registro per la scheda %s", ErrNotSupported, guid)
}

func writeNetworkAddress(subkey, hex12 string, log *logbuf) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, classPath+`\`+subkey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("%w: scrittura NetworkAddress (%v)", ErrPermission, err)
	}
	defer k.Close()
	if err := k.SetStringValue("NetworkAddress", hex12); err != nil {
		return err
	}
	log.add(`  registro: NetworkAddress = %s (sottochiave %s)`, hex12, subkey)
	return nil
}

func deleteNetworkAddress(subkey string, log *logbuf) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, classPath+`\`+subkey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("%w: accesso NetworkAddress (%v)", ErrPermission, err)
	}
	defer k.Close()
	if err := k.DeleteValue("NetworkAddress"); err != nil && err != registry.ErrNotExist {
		return err
	}
	log.add("  registro: NetworkAddress rimosso (sottochiave %s) → torna il MAC di fabbrica", subkey)
	return nil
}

// restartAdapter riavvia la scheda perché Windows rilegga NetworkAddress.
// Disable-NetAdapter è asincrona: Restart-NetAdapter fa da sé disable+enable
// aspettando il completamento. Si individua la scheda per InterfaceGuid, che
// coincide con l'ID che usiamo ovunque.
func restartAdapter(guid string, log *logbuf) error {
	ps := fmt.Sprintf(
		`$a = Get-NetAdapter -IncludeHidden | Where-Object { $_.InterfaceGuid -eq '%s' }; `+
			`if (-not $a) { Write-Error 'scheda non trovata'; exit 1 }; `+
			`Restart-NetAdapter -Name $a.Name -Confirm:$false`, guid)
	out, err := powershell(ps).CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("%w: riavvio scheda fallito (%v) %s", ErrDriverRejected, err, trimmed)
	}
	log.add("  scheda riavviata (Restart-NetAdapter)")
	return nil
}

// powershell prepara il comando senza finestra di console. L'helper è l'exe
// di Wails, un programma GUI senza console: senza CREATE_NO_WINDOW powershell
// se ne aprirebbe una, una finestra nera per tutto il riavvio della scheda.
func powershell(script string) *exec.Cmd {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}
