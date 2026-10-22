// SPDX-License-Identifier: MIT

// NetworkToolkit — cassetta degli attrezzi di rete. Primo strumento: cambio del
// MAC address della scheda di rete. Layout Aurora: menù dei toolkit a sinistra,
// interfaccia dello strumento a destra.
package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"networktoolkit/internal/elevate"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Modo helper: lo stesso binario, rilanciato con privilegi di
	// amministratore per applicare i cambi di MAC (la GUI resta non
	// privilegiata). Va intercettato PRIMA di avviare Wails, così l'helper non
	// apre alcuna finestra.
	if elevate.IsHelperInvocation(os.Args) {
		os.Exit(elevate.RunHelper(os.Args))
	}
	// Modo speedtest headless: nessun privilegio, va intercettato allo stesso
	// modo prima di avviare Wails (vedi cmd_speedtest.go).
	if IsSpeedtestInvocation(os.Args) {
		os.Exit(RunSpeedtestHeadless())
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "NetworkToolkit",
		Width:     1040,
		Height:    720,
		MinWidth:  920,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 16, B: 22, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: windowsOptions(),
		Linux: &linux.Options{
			Icon: windowIcon(appIcon), // ridotta: vedi icon_linux.go
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "NetworkToolkit " + Version,
				Message: "Cassetta degli attrezzi di rete.\n© 2026 Trismegisto Open Source — MIT",
			},
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
