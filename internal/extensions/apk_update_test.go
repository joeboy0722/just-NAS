package extensions

import "testing"

func TestAPKVirtualUpdateKeepsExistingRuntimeDependencies(t *testing.T) {
	installed := map[string]string{"qemu-system-x86_64": "11.0.3-r0", "musl": "1.2.6-r0"}
	protected := map[string]bool{"musl": true}
	for _, tc := range []struct {
		name, version string
		include, fail bool
	}{
		{"qemu-system-x86_64", "11.0.3-r0", true, false},
		{"qemu-hw-display-virtio-vga", "11.0.3-r0", true, false},
		{"musl", "1.2.6-r0", false, false},
		{"musl", "1.2.7-r0", false, true},
	} {
		include, e := includeExtensionAPK(tc.name, tc.version, installed, protected)
		if include != tc.include || (e != nil) != tc.fail {
			t.Fatalf("%s: include=%v error=%v", tc.name, include, e)
		}
	}
}
