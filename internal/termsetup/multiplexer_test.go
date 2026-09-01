package termsetup

import (
	"strings"
	"testing"
)

func TestDetectMultiplexer(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want Multiplexer
		ok   bool
	}{
		{"tmux", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}, Tmux, true},
		{"screen", map[string]string{"STY": "1234.pts-0.host"}, Screen, true},
		{"neither", map[string]string{}, NoMultiplexer, false},
		{"tmux wins when both are set somehow", map[string]string{"TMUX": "x", "STY": "y"}, Tmux, true},
		{"cmux, by its panel id", map[string]string{"CMUX_PANEL_ID": "0186FB26"}, Cmux, true},
		{"cmux, by another of its family", map[string]string{"CMUX_SOCKET_PATH": "/x/cmux.sock"}, Cmux, true},
		{"an empty cmux var is not cmux", map[string]string{"CMUX_SOCKET": ""}, NoMultiplexer, false},
		{"tmux wins over cmux, being the passthrough layer", map[string]string{"TMUX": "x", "CMUX_PANEL_ID": "y"}, Tmux, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DetectMultiplexer(tc.env)
			if got != tc.want || ok != tc.ok {
				t.Errorf("DetectMultiplexer(%v) = (%v, %v), want (%v, %v)", tc.env, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestMultiplexerWarningNamesTheMultiplexer(t *testing.T) {
	if got := MultiplexerWarning(Tmux); !strings.Contains(got, "tmux") {
		t.Errorf("the tmux warning does not say tmux: %q", got)
	}
	if got := MultiplexerWarning(Screen); !strings.Contains(got, "screen") {
		t.Errorf("the screen warning does not say screen: %q", got)
	}
}

// cmux is not a passthrough layer like tmux: it embeds Ghostty and reads the
// same ~/.config/ghostty/config this package writes, but only loads a change
// to it on `cmux reload-config`. So its warning must send the operator to that
// command, not to tmux-style extended-key passthrough.
func TestCmuxWarningSendsYouToReloadConfig(t *testing.T) {
	got := MultiplexerWarning(Cmux)
	if !strings.Contains(got, "cmux") {
		t.Errorf("the cmux warning does not say cmux: %q", got)
	}
	if !strings.Contains(got, "cmux reload-config") {
		t.Errorf("the cmux warning does not name `cmux reload-config`, the one thing that loads the change: %q", got)
	}
}
