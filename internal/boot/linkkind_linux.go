package boot

import (
	"encoding/binary"
	"strings"
	"syscall"
)

// Read kernel link kinds directly; the core does not require iproute2 or Docker CLI.
func linkKinds() map[string]string {
	out := map[string]string{}
	raw, e := syscall.NetlinkRIB(syscall.RTM_GETLINK, syscall.AF_UNSPEC)
	if e != nil {
		return out
	}
	messages, e := syscall.ParseNetlinkMessage(raw)
	if e != nil {
		return out
	}
	for _, m := range messages {
		if m.Header.Type != syscall.RTM_NEWLINK {
			continue
		}
		attrs, e := syscall.ParseNetlinkRouteAttr(&m)
		if e != nil {
			continue
		}
		name, kind := "", ""
		for _, a := range attrs {
			switch a.Attr.Type & 0x3fff {
			case 3:
				name = strings.TrimRight(string(a.Value), "\x00")
			case 18:
				b := a.Value
				for len(b) >= 4 {
					n := int(binary.NativeEndian.Uint16(b[:2]))
					typ := binary.NativeEndian.Uint16(b[2:4]) & 0x3fff
					if n < 4 || n > len(b) {
						break
					}
					if typ == 1 {
						kind = strings.TrimRight(string(b[4:n]), "\x00")
					}
					step := (n + 3) &^ 3
					if step > len(b) {
						break
					}
					b = b[step:]
				}
			}
		}
		out[name] = kind
	}
	return out
}
