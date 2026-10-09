package notify

import (
	"strings"
	"testing"
)

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
	hasSystem := SystemCommand() != ""
	ghostty := TerminalInfo{Protocol: OSC777}
	plain := TerminalInfo{}
	cases := []struct {
		mode       string
		t          TerminalInfo
		free, away bool
		term, syst bool
	}{
		// Automatic is the system's, which answers, wherever it has one.
		{Auto, ghostty, true, true, !hasSystem, hasSystem},
		{Auto, ghostty, true, false, false, hasSystem},
		{Auto, ghostty, false, true, false, hasSystem},
		{Auto, plain, true, true, false, hasSystem},
		{Terminal, ghostty, true, false, true, false},
		{Terminal, ghostty, false, true, false, false},
		{System, ghostty, true, true, false, true},
		{Off, ghostty, true, true, false, false},
	}
	for _, c := range cases {
		term, syst := Route(c.mode, c.t, c.free, c.away)
		if term != c.term || syst != c.syst {
			t.Errorf("%q free=%v away=%v: terminal %v system %v", c.mode, c.free, c.away, term, syst)
		}
	}
}

// TestAMultiplexerThatKeepsThemGoesToTheSystem: Zellij and herdr pass no
// notification on, so the system shows them, while the terminal is still
// named to tell it among the applications in front.
func TestAMultiplexerThatKeepsThemGoesToTheSystem(t *testing.T) {
	t.Parallel()
	for _, vars := range []map[string]string{
		{"ZELLIJ": "0", "TERM_PROGRAM": "ghostty"},
		{"HERDR_ENV": "1", "TERM_PROGRAM": "ghostty"},
	} {
		info := Detect(env(vars))
		if info.Muxer == "" || info.Protocol != None || info.Sequence("t", "b") != nil {
			t.Fatalf("%v: %+v", vars, info)
		}
		if useTerminal, useSystem := Route(Auto, info, true, true); useTerminal || !useSystem {
			t.Fatalf("%v: routed to the terminal", vars)
		}
		if app := info.App(env(vars)); app != "Ghostty" {
			t.Fatalf("%v: the terminal's application is %q", vars, app)
		}
	}
}

// TestWhatItIsAboutIsGhosttysTitleLine: Ghostty shows the window's title
// under a notification's heading, so what the news is about goes there for
// the notification and the title is put back after; elsewhere it follows
// the body.
func TestWhatItIsAboutIsGhosttysTitleLine(t *testing.T) {
	ghostty := TerminalInfo{Name: "Ghostty", Protocol: OSC777}
	seq, restore := ghostty.SequenceAbout("Pipeline failed", "acme/api · Rate limits", "Pipeline #8 of !42 failed")
	want := "\x1b]2;acme/api · Rate limits\x1b\\" + "\x1b]777;notify;Pipeline failed;Pipeline #8 of !42 failed\a"
	if string(seq) != want || string(restore) != "\x1b]2;unagit\x1b\\" {
		t.Fatalf("Ghostty got %q then %q", seq, restore)
	}
	iterm := TerminalInfo{Name: "iTerm2", Protocol: OSC9}
	if got, restore := iterm.SequenceAbout("Pipeline failed", "acme/api", "failed"); string(got) != "\x1b]9;Pipeline failed: failed · acme/api\a" || restore != nil {
		t.Fatalf("iTerm2 got %q", got)
	}
	ghostty.Tmux = true
	if got, restore := ghostty.SequenceAbout("Pipeline failed", "acme/api", "failed"); strings.Contains(string(got), "]2;") || restore != nil {
		t.Fatalf("under tmux the title was set: %q", got)
	}
}
