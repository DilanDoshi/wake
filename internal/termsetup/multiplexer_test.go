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
