package notify

import "testing"

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestEachTerminalGetsItsOwnSequence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		vars map[string]string
		name string
		want string
	}{
		{map[string]string{"TERM_PROGRAM": "ghostty"}, "Ghostty", "\x1b]777;notify;api !42;pipeline failed\a"},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, "WezTerm", "\x1b]777;notify;api !42;pipeline failed\a"},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iTerm2", "\x1b]9;api !42: pipeline failed\a"},
		{map[string]string{"TERM": "xterm-kitty"}, "kitty", "\x1b]99;i=1:d=0;api !42\x1b\\\x1b]99;i=1:p=body;pipeline failed\x1b\\"},
		{map[string]string{"TERM": "foot-extra"}, "foot", "\x1b]777;notify;api !42;pipeline failed\a"},
		{map[string]string{"TERM": "xterm-256color"}, "", ""},
	}
	for _, c := range cases {
		info := Detect(env(c.vars))
		if info.Name != c.name {
			t.Errorf("%v: detected %q, want %q", c.vars, info.Name, c.name)
		}
		if got := string(info.Sequence("api !42", "pipeline failed")); got != c.want {
			t.Errorf("%s: sequence %q, want %q", c.name, got, c.want)
		}
	}
}

func TestInsideTmuxTheSequenceIsWrapped(t *testing.T) {
	t.Parallel()
	info := Detect(env(map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "TERM_PROGRAM": "tmux", "GHOSTTY_RESOURCES_DIR": "/Applications/Ghostty.app"}))
	if info.Name != "Ghostty" || !info.Tmux {
		t.Fatalf("detected %+v", info)
	}
	want := "\x1bPtmux;\x1b\x1b]777;notify;t;b\a\x1b\\"
	if got := string(info.Sequence("t", "b")); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNothingInASequenceCanEndItEarly(t *testing.T) {
	t.Parallel()
	info := TerminalInfo{Protocol: OSC777}
	got := string(info.Sequence("a;b\x1b]", "line\x07two"))
	if got != "\x1b]777;notify;a,b ];line two\a" {
		t.Fatalf("got %q", got)
	}
}

func TestRoute(t *testing.T) {
	t.Parallel()
	ghostty := TerminalInfo{Protocol: OSC777}
	plain := TerminalInfo{}
	cases := []struct {
		mode       string
		t          TerminalInfo
		free       bool
		term, syst bool
	}{
		{Auto, ghostty, true, true, false},
		{Auto, ghostty, false, false, true},
		{Auto, plain, true, false, true},
		{Terminal, ghostty, false, false, false},
		{System, ghostty, true, false, true},
		{Off, ghostty, true, false, false},
	}
	for _, c := range cases {
		term, syst := Route(c.mode, c.t, c.free)
		if term != c.term || syst != c.syst {
			t.Errorf("%q free=%v: terminal %v system %v", c.mode, c.free, term, syst)
		}
	}
}
