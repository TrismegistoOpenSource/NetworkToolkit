// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"testing"
)

func TestWindowsOptionsFollowConfigDir(t *testing.T) {
	t.Setenv("NETWORKTOOLKIT_CONFIG_DIR", "")
	if o := windowsOptions(); o != nil {
		t.Errorf("senza redirezione: %+v", o)
	}
	d := t.TempDir()
	t.Setenv("NETWORKTOOLKIT_CONFIG_DIR", d)
	o := windowsOptions()
	if o == nil || o.WebviewUserDataPath != filepath.Join(d, "webview") {
		t.Errorf("con redirezione: %+v", o)
	}
}
