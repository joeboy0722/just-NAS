// SPDX-License-Identifier: MIT
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"just-nas/internal/extensions"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: nas-package-check public-key-file bundle-or-glob...")
		os.Exit(2)
	}
	raw, e := os.ReadFile(os.Args[1])
	if e != nil {
		panic(e)
	}
	key, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if e != nil || len(key) != ed25519.PublicKeySize {
		panic("invalid public key")
	}
	count := 0
	for _, pattern := range os.Args[2:] {
		files, e := filepath.Glob(pattern)
		if e != nil {
			panic(e)
		}
		for _, p := range files {
			v, e := extensions.Verify(p, []ed25519.PublicKey{ed25519.PublicKey(key)}, "")
			if e != nil {
				fmt.Fprintln(os.Stderr, p, e)
				os.Exit(1)
			}
			fmt.Printf("PASS %s %s official=%v unique_payload=%d\n", v.Manifest.ID, v.Manifest.Version, v.Official, v.Manifest.InstalledBytes)
			count++
		}
	}
	if count == 0 {
		panic("no bundle files matched")
	}
}
