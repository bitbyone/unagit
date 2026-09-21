package fuzzy

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, text string
		want          bool
	}{
		{"", "anything", true},
		{"api", "backend/api-gateway", true},
		{"bapi", "backend/api-gateway", true},
		{"gateway api", "backend/api-gateway", true},
		{"xyz", "backend/api-gateway", false},
		{"api web", "backend/api-gateway", false},
		{"API", "backend/api-gateway", true},
	}
	for _, c := range cases {
		if _, ok := Match(c.pattern, c.text); ok != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.text, ok, c.want)
		}
	}
}

func TestSubstringBeatsSubsequence(t *testing.T) {
	exact, _ := Match("auth", "platform/auth")
	scattered, _ := Match("auth", "a-very-useful-thing")
	if exact <= scattered {
		t.Errorf("exact substring scored %d, scattered scored %d", exact, scattered)
	}
}

func TestWordBoundaryBonus(t *testing.T) {
	boundary, _ := Match("gate", "backend/gateway")
	inside, _ := Match("gate", "backendgatexway")
	if boundary <= inside {
		t.Errorf("boundary %d should beat inside %d", boundary, inside)
	}
}
