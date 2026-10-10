package ui

// Dialogs are a stack: whatever is cancelled goes back to the dialog it was
// opened from, and that one to its own, all the way down. A picker closes
// before what it opens - a form, a question, another picker - so it leaves
// behind how to open it again (returnTo), with where it goes back to in
// turn; the next dialog takes that as its own way back. Esc and Cancel on
// a form, Cancel on a question, Esc on a picker, closing a message over a
// bare screen, and giving up a wait all go there.
//
// A picker takes the way back for itself; a form, a question or a message
// only reads it, so that the list they came from, opened again after them,
// still finds where it goes back to. A key pressed on a screen - a main
// one, the worktree view, the Changes dialog - forgets it: whatever is
// opened from there starts a stack of its own.

// dialogReturn is how to go back to a picker that closed to open something.
type dialogReturn struct {
	title  string
	reopen func()
	// back is where the picker itself went back to.
	back func()
}

// leaveReturn notes how to open a picker again, as it closes to run what
// was chosen in it.
func (a *App) leaveReturn(title string, reopen, back func()) {
	a.returnTo = &dialogReturn{title: title, reopen: reopen, back: back}
}

// claimBack is where a picker titled title goes back to, taken: the picker
// it was opened from, or - for the same list opened again after something
// done from it - where that one went back to.
func (a *App) claimBack(title string) func() {
	r := a.returnTo
	a.returnTo = nil
	switch {
	case r == nil:
		return nil
	case r.title == title:
		return r.back
	}
	return r.reopen
}

// peekBack is where a form, a question or a message goes back to when it
// is cancelled: the picker it was opened from, or nil.
func (a *App) peekBack() func() {
	if a.returnTo == nil {
		return nil
	}
	return a.returnTo.reopen
}

// forgetReturn drops the way back, for a key pressed on a screen.
func (a *App) forgetReturn() {
	name, _ := a.pages.GetFrontPage()
	if !isModalPage(name) || name == pageWorktree || name == pageChanges {
		a.returnTo = nil
	}
}
