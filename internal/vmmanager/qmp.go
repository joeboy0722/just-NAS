package vmmanager

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func qmp(socket, command string, args any) (json.RawMessage, error) {
	c, e := net.DialTimeout("unix", socket, 2*time.Second)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	d := json.NewDecoder(c)
	enc := json.NewEncoder(c)
	var greeting map[string]json.RawMessage
	if e = d.Decode(&greeting); e != nil {
		return nil, e
	}
	if greeting["QMP"] == nil {
		return nil, fmt.Errorf("QMP 握手無效")
	}
	for _, req := range []map[string]any{{"execute": "qmp_capabilities"}, {"execute": command, "arguments": args}} {
		if args == nil {
			delete(req, "arguments")
		}
		if e = enc.Encode(req); e != nil {
			return nil, e
		}
		for {
			var res struct {
				Return json.RawMessage `json:"return"`
				Error  json.RawMessage `json:"error"`
				Event  string          `json:"event"`
			}
			if e = d.Decode(&res); e != nil {
				return nil, e
			}
			if len(res.Error) > 0 {
				return nil, fmt.Errorf("QMP: %s", res.Error)
			}
			if res.Return != nil {
				if req["execute"] == command {
					return res.Return, nil
				}
				break
			}
		}
	}
	return nil, fmt.Errorf("QMP 沒有回應")
}
