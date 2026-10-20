package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/credential"
	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

// credentialApp is the Sandboxes tab with a socket path short enough for
// macOS, which a test's own directory is not.
func credentialApp(t *testing.T, w, h int) (*App, tcell.SimulationScreen, *uniboxtest.Fake) {
	t.Helper()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"agent login claude -p web --command * --json": uniboxtest.OK("profile web signs its agents in"),
		"service add * --json":                         uniboxtest.OK("service gitlab-2n added"),
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

// TestAnAgentSignsInWithATokenInUnagit: A on a profile lists its agents;
// Enter takes a token into the vault, has unibox name unagit credential
// get as its source, and from then on the socket hands it out - and says
// so in Activity - while a name it does not keep is refused.
func TestAnAgentSignsInWithATokenInUnagit(t *testing.T) {
	t.Parallel()
	a, sc, fake := credentialApp(t, 140, 40)
	selectSandboxRow(t, a, "p:web")
	typeRunes(sc, "A")
	waitFor(t, a, sc, "Sign in · web")
	waitFor(t, a, sc, "Copilot")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "claude setup-token")
	typeRunes(sc, "sk-ant-oat01-secret")
	pressButton(t, a, sc, frontForm(a), "Sign In")

	line := calledWith(t, a, fake, "agent login claude -p web --command ")
	if !strings.Contains(line, " credential get claude --socket "+a.credentialSocket()) {
		t.Fatalf("the profile is not told unagit's command: %q", line)
	}
	if strings.Contains(strings.Join(fake.Calls(), "\n"), "secret") && strings.Contains(strings.Join(fake.Calls(), "\n"), "sk-ant") {
		t.Fatal("the token reached unibox's command line")
	}
	token, err := credential.Get(a.credentialSocket(), "claude")
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
	events := a.watchStore.History("credential\x00claude")
	if len(events) != 1 || events[0].Heading != "Token handed out" {
		t.Fatalf("the handout is not in Activity: %+v", events)
	}
	if events := a.watchStore.History("credential\x00work"); len(events) != 1 || events[0].Heading != "Token refused" {
		t.Fatalf("the refusal is not in Activity: %+v", events)
	}

	// T lists it without its value; d takes it out, and the socket goes.
	typeRunes(sc, "T")
	waitFor(t, a, sc, "Tokens in unagit")
	waitFor(t, a, sc, "agent")
	if strings.Contains(a.screenText(sc), "sk-ant") {
		t.Fatal("a token is shown")
	}
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Remove token claude")
	typeRunes(sc, "y")
	waitEditorState(t, a, func() bool { return a.credServer == nil })
	if _, err := credential.Get(a.credentialSocket(), "claude"); err == nil || !strings.Contains(err.Error(), "start it and unlock it") {
		t.Fatalf("the socket outlived the last token: %v", err)
	}
}

// TestAServiceIsAddedWithATokenInUnagit: t in a profile's services adds
// one whose token unagit keeps; unibox is given the hosts and the command.
func TestAServiceIsAddedWithATokenInUnagit(t *testing.T) {
	t.Parallel()
	a, sc, fake := credentialApp(t, 140, 40)
	selectSandboxRow(t, a, "p:web")
	typeRunes(sc, "v")
	waitFor(t, a, sc, "Services of web")
	typeRunes(sc, "t")
	waitFor(t, a, sc, "Add a service · web")
	form := frontForm(a)
	setField(t, a, form, 0, "gitlab-2n")
	setField(t, a, form, 2, "gitlab.2n.cz")
	setField(t, a, form, 3, "glpat-narrow")
	pressButton(t, a, sc, form, "Add")
	line := calledWith(t, a, fake, "service add gitlab-2n -p web --kind gitlab --command ")
	if !strings.Contains(line, "credential get gitlab-2n") || !strings.Contains(line, "--host gitlab.2n.cz") || strings.Contains(line, "glpat") {
		t.Fatalf("unibox was told %q", line)
	}
	if token, err := credential.Get(a.credentialSocket(), "gitlab-2n"); err != nil || token != "glpat-narrow" {
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
			selectSandboxRow(t, a, "p:web")
			typeRunes(sc, "A")
			waitFor(t, a, sc, "Sign in · web")
			assertLegible(t, a, sc, "the agents to sign in")
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			waitFor(t, a, sc, "claude setup-token")
			assertFormFits(t, a, sc, frontForm(a), "Token", "Sign In", "Cancel")
			assertLegible(t, a, sc, "an agent's token")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitGone(t, a, sc, "claude setup-token")

			selectSandboxRow(t, a, "p:web")
			typeRunes(sc, "v")
			waitFor(t, a, sc, "Services of web")
			typeRunes(sc, "t")
			waitFor(t, a, sc, "Add a service · web")
			assertFormFits(t, a, sc, frontForm(a), "Service", "Kind", "Hosts", "Token", "Add", "Cancel")
			assertLegible(t, a, sc, "a service with a token")
		})
	}
}
