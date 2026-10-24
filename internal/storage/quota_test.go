package storage

import "testing"

func TestQuotaParsing(t *testing.T) {
	for v, want := range map[string]uint64{"none": 0, "10G": 10 << 30, "1GiB": 1 << 30, "500M": 500 << 20} {
		n, e := quotaBytes(v)
		if e != nil || n != want {
			t.Fatalf("%s: %d %v", v, n, e)
		}
	}
	for _, v := range []string{"-1G", "0", "1.5G", "1G;rm", "18446744073709551615T"} {
		if _, e := quotaBytes(v); e == nil {
			t.Fatal("accepted", v)
		}
	}
}
