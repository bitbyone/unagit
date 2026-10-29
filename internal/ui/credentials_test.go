package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/credential"
	"github.com/tobola/unagit/internal/testenv"
	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

// credentialApp is the Sandboxes tab with a socket path short enough for
// macOS, which a test's own directory is not.
func credentialApp(t *testing.T, w, h int) (*App, tcell.SimulationScreen, *uniboxtest.Fake) {
	t.Helper()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"secret set * --command * --json": uniboxtest.OK("sbx asks unagit for the token"),
	})
	dir, err := os.MkdirTemp("/tmp", "uc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	a, sc, _ := newTestAppSrv(t, func(a *App) {
		prepare(a)
		a.credSocket = credential.SocketPath(dir)
	})
	resizeApp(a, sc, w, h)
	openSandboxes(t, a, sc)
	return a, sc, fake
}

// calledWith is the first command line the stand-in ran that begins so.
func calledWith(t *testing.T, a *App, fake *uniboxtest.Fake, prefix string) string {
	t.Helper()
	var line string
	waitEditorState(t, a, func() bool {
		for _, c := range fake.Calls() {
			if strings.HasPrefix(c, prefix) {
				line = c
				return true
			}
		}
		return false
	})
	return line
}

// TestATokenIsKeptInUnagit: t on a token of an env's takes it into the
// vault, has sbx keep unagit credential get as its source, and from then
// on the socket hands it out - and says so in Activity - while a name it
// does not keep is refused.
func TestATokenIsKeptInUnagit(t *testing.T) {
	t.Parallel()
	a, sc, fake := credentialApp(t, 140, 40)
	selectSandboxRow(t, a, "e:web")
	typeRunes(sc, "K")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "t")
	waitFor(t, a, sc, "claude setup-token")
	typeRunes(sc, "sk-ant-oat01-secret")
	pressButton(t, a, sc, frontForm(a), "Keep")

	line := calledWith(t, a, fake, "secret set anthropic -e web --command ")
	if !strings.Contains(line, " credential get anthropic --socket "+a.credentialSocket()) {
		t.Fatalf("sbx is not told unagit's command: %q", line)
	}
	if strings.Contains(strings.Join(fake.Calls(), "\n"), "sk-ant") {
		t.Fatal("the token reached unibox's command line")
	}
	token, err := credential.Get(a.credentialSocket(), "anthropic")
	if err != nil || token != "sk-ant-oat01-secret" {
		t.Fatalf("handed out %q, %v", token, err)
	}
	if _, err := credential.Get(a.credentialSocket(), "work"); err == nil || !strings.Contains(err.Error(), "no token work") {
		t.Fatalf("a forge token's name was answered: %v", err)
	}
	store := onLoop(a, func() bool { return a.watchStore != nil })
	if !store {
		t.Fatal("no store to write the handouts to")
	}
	events := a.watchStore.History("credential\x00anthropic")
	if len(events) != 1 || events[0].Heading != "Token handed out" {
		t.Fatalf("the handout is not in Activity: %+v", events)
	}
	if events := a.watchStore.History("credential\x00work"); len(events) != 1 || events[0].Heading != "Token refused" {
		t.Fatalf("the refusal is not in Activity: %+v", events)
	}

	// T lists it without its value; d takes it out, and the socket goes.
	typeRunes(sc, "T")
	waitFor(t, a, sc, "Tokens in unagit")
	waitFor(t, a, sc, "service")
	if strings.Contains(a.screenText(sc), "sk-ant") {
		t.Fatal("a token is shown")
	}
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Remove token anthropic")
	typeRunes(sc, "y")
	waitEditorState(t, a, func() bool { return a.credServer == nil })
	if _, err := credential.Get(a.credentialSocket(), "anthropic"); err == nil || !strings.Contains(err.Error(), "start it and unlock it") {
		t.Fatalf("the socket outlived the last token: %v", err)
	}
}

// TestATokenMadeOnAPageOpensItFirst: a token the env says is made on a
// page opens the page, then asks for it; sbx is given the command alone.
func TestATokenMadeOnAPageOpensItFirst(t *testing.T) {
	opened := make(chan string, 1)
	saved := openBrowser
	openBrowser = func(url string) error { opened <- url; return nil }
	t.Cleanup(func() { openBrowser = saved })
	a, sc, fake := credentialApp(t, 140, 40)
	selectSandboxRow(t, a, "e:web")
	typeRunes(sc, "K")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "j")
	typeRunes(sc, "t")
	waitFor(t, a, sc, "Token of artifactory · web")
	if url := <-opened; url != "https://artifactory.example.com/ui/user_profile" {
		t.Fatalf("opened %q", url)
	}
	typeRunes(sc, "narrow")
	pressButton(t, a, sc, frontForm(a), "Keep")
	line := calledWith(t, a, fake, "secret set artifactory -e web --command ")
	if !strings.Contains(line, "credential get artifactory") || strings.Contains(line, "narrow") {
		t.Fatalf("unibox was told %q", line)
	}
	if token, err := credential.Get(a.credentialSocket(), "artifactory"); err != nil || token != "narrow" {
		t.Fatalf("handed out %q, %v", token, err)
	}
}

// TestTheTokenFormsFitTheirFrames: the agent's token and the service's
// form at several sizes.
func TestTheTokenFormsFitTheirFrames(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{120, 34}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := credentialApp(t, size.w, size.h)
			selectSandboxRow(t, a, "e:web")
			typeRunes(sc, "K")
			waitFor(t, a, sc, "Tokens of web")
			assertLegible(t, a, sc, "the tokens of an env")
			waitFor(t, a, sc, "NORMAL")
			typeRunes(sc, "t")
			waitFor(t, a, sc, "claude setup-token")
			assertFormFits(t, a, sc, frontForm(a), "Token", "Keep", "Cancel")
			assertLegible(t, a, sc, "a token kept in unagit")
		})
	}
}

// TestKeepingClaudesTokenRunsSetupTokenFirst: t on Claude's token, not
// kept yet, runs claude setup-token in this terminal - its browser login -
// and then asks for the token it printed.
func TestKeepingClaudesTokenRunsSetupTokenFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	claude := filepath.Join(dir, "claude")
	if err := testenv.WriteExecutable(claude, []byte("#!/bin/sh\necho \"$@\" > "+ran+"\n")); err != nil {
		t.Fatal(err)
	}
	a, sc, _ := credentialApp(t, 140, 40)
	onLoop(a, func() bool {
		find := a.findExecutable
		a.findExecutable = func(name string) (string, error) {
			if name == "claude" {
				return claude, nil
			}
			return find(name)
		}
		return true
	})
	selectSandboxRow(t, a, "e:web")
	typeRunes(sc, "K")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "t")
	waitFor(t, a, sc, "claude setup-token printed")
	if got, _ := os.ReadFile(ran); strings.TrimSpace(string(got)) != "setup-token" {
		t.Fatalf("claude ran with %q", got)
	}
}
