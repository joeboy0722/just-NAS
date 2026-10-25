package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"just-nas/internal/config"
	"path/filepath"
	"testing"
)

func nativeManifest() Manifest {
	payload := []byte("#!/bin/sh\n")
	h := sha256.Sum256(payload)
	return Manifest{Format: 2, ABI: NativeABI, ID: "vm-manager", Name: "VM", Kind: "app", Arch: "x86_64", Version: "1.0.0-rc1", MinNASVersion: "2.1.0-rc4", License: "MIT", Native: &NativeSpec{Service: "vm-manager"}, Dependencies: map[string]string{"qemu-runtime": "11.0.3-r0"}, Files: map[string]File{"bin/vm-manager": {SHA256: hex.EncodeToString(h[:]), Size: int64(len(payload)), Mode: 0755}}, InstalledBytes: int64(len(payload))}
}
func TestNativeProfileIsNotContainerOrArbitraryService(t *testing.T) {
	if e := nativeManifest().Validate(); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Manifest){func(m *Manifest) { m.ID = "shell" }, func(m *Manifest) { m.Format = 1; m.ABI = NASABI }, func(m *Manifest) { m.MinNASVersion = "2.1.0-rc3" }, func(m *Manifest) { m.Native.Service = "sshd" }, func(m *Manifest) { m.Dependencies = nil }, func(m *Manifest) { m.App = &AppSpec{} }} {
		m := nativeManifest()
		mutate(&m)
		if m.Validate() == nil {
			t.Fatal("unsafe native declaration accepted", m)
		}
	}
	if NativeUID < 65534 {
		t.Fatal("native identity overlaps family account allocation")
	}
}
func TestNativePoolReferencesAndSettingsAreRemovedTogether(t *testing.T) {
	oldDir, oldSave := config.Dir, Save
	config.Dir = t.TempDir()
	Save = func() error { return nil }
	defer func() { config.Dir = oldDir; Save = oldSave }()
	m := nativeManifest()
	s := State{Version: 1, Dataset: "homepool/.just-nas-extensions", Installed: map[string]Installation{}}
	a := Deployment{ID: m.ID, Package: Verified{Manifest: m, Official: true, Hash: hex.EncodeToString(make([]byte, 32))}, Dataset: s.Dataset + "/apps/" + m.ID}
	config.WriteJSON(StateFile, s)
	config.WriteJSON("apps.json", AppState{Version: 1, Apps: map[string]Deployment{a.ID: a}})
	raw, _ := json.Marshal(map[string]any{"version": 1, "data_references": []string{"vmtest/.just-nas-vms"}, "state": map[string]int{"version": 1}})
	if e := SaveNativeState(a.ID, raw); e != nil {
		t.Fatal(e)
	}
	if !ApplicationReferences(s, a, "vmtest") {
		t.Fatal("missing VM storage reference")
	}
	next, kept, e := MakePoolState("vmtest")
	if e != nil {
		t.Fatal(e)
	}
	if len(kept) != 1 || kept[0] != a.Dataset {
		t.Fatal("app data on other pool must be retained", kept)
	}
	var apps AppState
	json.Unmarshal(next["apps.json"], &apps)
	if len(apps.Apps) != 0 {
		t.Fatal("deployment retained")
	}
	var states map[string]NativeStateRecord
	json.Unmarshal(next[NativeStatesFile], &states)
	if len(states) != 0 {
		t.Fatal("native metadata retained")
	}
	if filepath.Base(NativeSocket("vm-manager")) != "service.sock" {
		t.Fatal("unexpected socket")
	}
	_ = context.Background()
}
