// SPDX-License-Identifier: MIT

//go:build windows

package elevate

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformElevate rilancia l'helper con il verbo "runas": Windows mostra il
// prompt UAC. Con SEE_MASK_NOCLOSEPROCESS otteniamo l'handle del processo
// elevato e ne aspettiamo la fine, così alla lettura il file di uscita è già
// stato scritto.
//
// ShellExecuteExW non è esposto da x/sys/windows: lo si dichiara qui su
// shell32. Il binario gira nel sottosistema GUI, ma in modo helper non apre
// finestre (main.go dirotta prima di avviare la UI), e nShow=SW_HIDE evita
// qualunque lampo di finestra.
func platformElevate(exe, b64, outPath string) error {
	const (
		seeMaskNoCloseProcess = 0x00000040
		swHide                = 0
	)

	verb, _ := windows.UTF16PtrFromString(elevateVerb)
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params := HelperArg + " " + b64 + ` "` + outPath + `"`
	paramsPtr, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return err
	}

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: paramsPtr,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	if err := shellExecuteEx(&info); err != nil {
		if err == windows.ERROR_CANCELLED {
			return ErrCanceled
		}
		return fmt.Errorf("elevazione UAC fallita: %w", err)
	}
	if info.hProcess == 0 {
		return fmt.Errorf("processo elevato non avviato")
	}
	defer windows.CloseHandle(info.hProcess)

	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return err
	}
	return nil
}

// elevateVerb è "runas" (prompt UAC). Solo i test lo cambiano in "open", per
// provare tutto il resto del giro senza il prompt.
var elevateVerb = "runas"

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle // union hIcon/hMonitor
	hProcess     windows.Handle
}

var (
	modshell32       = windows.NewLazySystemDLL("shell32.dll")
	procShellExecute = modshell32.NewProc("ShellExecuteExW")
)

func shellExecuteEx(info *shellExecuteInfo) error {
	r, _, e := procShellExecute.Call(uintptr(unsafe.Pointer(info)))
	if r == 0 {
		// ShellExecuteExW ritorna FALSE: e porta il vero GetLastError.
		if errno, ok := e.(windows.Errno); ok && errno != 0 {
			return errno
		}
		return fmt.Errorf("ShellExecuteExW ha restituito FALSE")
	}
	return nil
}
