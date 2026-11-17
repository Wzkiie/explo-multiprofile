package settings

import (
	"strings"
	"testing"
)

func TestExtractSpDC(t *testing.T) {
	val := "AQDfakefakefakefakefakefakefakefakefake-_1234"
	cases := map[string]string{
		"bare value":       val,
		"with name":        "sp_dc=" + val,
		"netscape":         "# Netscape HTTP Cookie File\n.spotify.com\tTRUE\t/\tTRUE\t1893456000\tsp_t\tabcdef\n.spotify.com\tTRUE\t/\tTRUE\t1893456000\tsp_dc\t" + val + "\n",
		"httponly prefix":  "#HttpOnly_.spotify.com\tTRUE\t/\tTRUE\t1893456000\tsp_dc\t" + val + "\n",
		"windows newlines": strings.ReplaceAll("# Netscape\n.spotify.com\tTRUE\t/\tTRUE\t1\tsp_dc\t"+val+"\n", "\n", "\r\n"),
	}
	for name, in := range cases {
		got, ok := extractSpDC([]byte(in))
		if !ok || got != val {
			t.Errorf("%s: got %q %v", name, got, ok)
		}
	}
	for name, in := range map[string]string{
		"empty":        "",
		"too short":    "abc",
		"other site":   ".example.com\tTRUE\t/\tTRUE\t1\tsp_dc\t" + val + "\n",
		"other cookie": ".spotify.com\tTRUE\t/\tTRUE\t1\tsp_t\t" + val + "\n",
		"spaces":       "not a cookie value at all, just text",
	} {
		if v, ok := extractSpDC([]byte(in)); ok {
			t.Errorf("%s: must be rejected, got %q", name, v)
		}
	}
}
