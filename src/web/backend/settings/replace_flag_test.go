package settings

import "testing"

func TestSetReplaceFlag(t *testing.T) {
	cases := []struct {
		in      string
		replace bool
		want    string
	}{
		{"--playlist daily-jams", true, "--playlist daily-jams --replace-playlist=true"},
		{"--playlist daily-jams", false, "--playlist daily-jams --replace-playlist=false"},
		{"--playlist x --replace-playlist=false", true, "--playlist x --replace-playlist=true"},
		{"--playlist x --replace-playlist=true --clean-downloads", false, "--playlist x --clean-downloads --replace-playlist=false"},
		{"--replace-playlist --playlist x", true, "--playlist x --replace-playlist=true"},
		{"", true, "--replace-playlist=true"},
	}
	for _, c := range cases {
		if got := SetReplaceFlag(c.in, c.replace); got != c.want {
			t.Errorf("SetReplaceFlag(%q, %v) = %q, want %q", c.in, c.replace, got, c.want)
		}
	}
}
