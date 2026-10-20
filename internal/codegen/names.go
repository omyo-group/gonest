package codegen

import (
	"fmt"
	"go/token"
	"strings"
	"unicode"
)

// Names holds the spellings of one user supplied name.
type Names struct {
	// Words are the lower-case words of the name.
	Words []string
	// Pascal is UserProfile.
	Pascal string
	// Camel is userProfile.
	Camel string
	// Snake is user_profile (file and package names).
	Snake string
	// Kebab is user-profile (routes, module names).
	Kebab string
}

// ParseName normalises kebab, snake, camel and Pascal case input.
func ParseName(raw string) (Names, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Names{}, fmt.Errorf("name must not be empty")
	}
	for _, r := range raw {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != ' ' {
			return Names{}, fmt.Errorf("invalid character %q in name %q (use letters, digits, '-' or '_')", r, raw)
		}
	}
	words := splitWords(raw)
	if len(words) == 0 {
		return Names{}, fmt.Errorf("name %q has no letters or digits", raw)
	}
	if !unicode.IsLetter([]rune(words[0])[0]) {
		return Names{}, fmt.Errorf("name %q must start with a letter", raw)
	}
	return namesFromWords(words), nil
}

func namesFromWords(words []string) Names {
	n := Names{Words: words}
	var pascal strings.Builder
	for _, w := range words {
		pascal.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	n.Pascal = pascal.String()
	n.Camel = strings.ToLower(n.Pascal[:1]) + n.Pascal[1:]
	n.Snake = strings.Join(words, "_")
	n.Kebab = strings.Join(words, "-")
	return n
}

// splitWords splits on separators and on lower-to-upper case boundaries; an
// acronym followed by a word ("HTTPServer") splits before the last capital.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case r == '-' || r == '_' || r == ' ':
			flush()
			continue
		case unicode.IsUpper(r) && len(cur) > 0:
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

// trimSuffixWord drops a trailing word equal to suffix ("auth-guard" -> auth)
// unless that would leave nothing.
func (n Names) trimSuffixWord(suffix string) Names {
	if len(n.Words) > 1 && n.Words[len(n.Words)-1] == suffix {
		return namesFromWords(n.Words[:len(n.Words)-1])
	}
	return n
}

// validPackage reports whether s can be used as a Go package name.
func validPackage(s string) error {
	if s == "" || !token.IsIdentifier(s) || token.IsKeyword(s) {
		return fmt.Errorf("%q is not a valid Go package name", s)
	}
	return nil
}
