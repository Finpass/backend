package domain

import (
	"strings"
	"unicode"
)

type Command string

const (
	CmdMore     Command = "MORE"
	CmdStop     Command = "STOP"
	CmdResume   Command = "RESUME"
	CmdProfile  Command = "PROFILE"
	CmdUpdate   Command = "UPDATE"
	CmdHelp     Command = "HELP"
	CmdQuestion Command = "QUESTION" // routed to the chatbot (Feature 2, not built yet)
	CmdUnknown  Command = "UNKNOWN"
)

var aliases = map[string]Command{
	"MORE": CmdMore, "LAGI": CmdMore, "STOP": CmdStop, "BERHENTI": CmdStop, "UNSUBSCRIBE": CmdStop,
	"RESUME": CmdResume, "LANJUT": CmdResume, "START": CmdResume, "PROFILE": CmdProfile, "PROFIL": CmdProfile,
	"UPDATE": CmdUpdate, "HELP": CmdHelp, "BANTUAN": CmdHelp,
}

// ParseCommand reads the reply text. A command is the whole message (ignoring case and punctuation); a longer text with
// a question mark or question word is a question, anything else is unknown (answered with HELP).
func ParseCommand(text string) Command {
	t := strings.TrimFunc(strings.TrimSpace(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '?' })
	if c, ok := aliases[strings.ToUpper(t)]; ok {
		return c
	}
	if t == "" {
		return CmdUnknown
	}
	l := strings.ToLower(t)
	if strings.Contains(l, "?") {
		return CmdQuestion
	}
	for _, w := range []string{"apa ", "bagaimana ", "kenapa ", "mengapa ", "kapan ", "berapa ", "apakah ", "what ", "how ", "why ", "when ", "should "} {
		if strings.HasPrefix(l, w) {
			return CmdQuestion
		}
	}
	return CmdUnknown
}
