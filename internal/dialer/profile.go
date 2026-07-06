package dialer

import (
	"fmt"
	"regexp"
	"sort"
	"time"
)

// registry maps a board name to the adapter that builds its Profile. This is the
// plug point for host-agnosticism: register a constructor here (or from another
// file / package init) and it becomes selectable by name. Think of it as the DI
// container wiring a named IBbsAdapter implementation.
var registry = map[string]func() *Profile{
	"tresbbs": BuiltinTresBBS,
}

// Register adds (or overrides) a board adapter under name. Call from an init()
// to contribute a new BBS without touching existing code.
func Register(name string, build func() *Profile) { registry[name] = build }

// Get returns the adapter's Profile for a registered board name.
func Get(name string) (*Profile, error) {
	build, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("no adapter registered for board %q (known: %v)", name, Known())
	}
	return build(), nil
}

// Known lists the registered board names (sorted) for help text and errors.
func Known() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Profile describes how to drive ONE kind of BBS: the regexes that recognize its
// prompts and the keystrokes that navigate its menus. The session driver is
// generic and reads everything host-specific from here, so supporting a new
// board is a matter of writing a new Profile — never touching the engine.
//
// This is what makes carrier host-agnostic: BuiltinTresBBS() below is one target;
// a Renegade, Mystic, or Synchronet profile would be a sibling constructor.
type Profile struct {
	Name string

	ConnectWait time.Duration // how long to wait for the first login prompt
	ActWait     time.Duration // idle window for a normal menu/read response

	// --- login / registration ---
	UserPrompt *regexp.Regexp // "User Name:"
	PassPrompt *regexp.Regexp // "Password:"
	NewUserAsk *regexp.Regexp // "New user? (y/N)"
	NewUserYes string         // key/line to accept new-user signup
	BadLogin   *regexp.Regexp // "Incorrect password" — bail
	Register   []RegStep      // ordered prompt->field signup script
	Welcome    *regexp.Regexp // post-login/registration banner ("Welcome back"/"Welcome to")

	// --- shared anchors ---
	MainMenu *regexp.Regexp // main-menu command prompt
	Pause    *regexp.Regexp // "Press Enter to continue"
	PauseKey string         // what dismisses a pause (usually "\r")
	Busy     *regexp.Regexp // "All nodes busy" banner

	// --- main-menu navigation keys (single keystrokes) ---
	ToMsgArea string // enter the message subsystem
	Logoff    string // hang up cleanly

	// --- message subsystem ---
	MsgMenu       *regexp.Regexp // message-menu command prompt
	PostKey       string         // "enter a message"
	ReadKey       string         // "read messages"
	MsgMenuToMain string         // key to return to main menu

	// post sub-flow
	SubjectPrompt *regexp.Regexp
	BodyPrompt    *regexp.Regexp // "Enter message (empty line to finish)"
	PostedOK      *regexp.Regexp

	// reader + threaded reply sub-flow
	ReaderPrompt    *regexp.Regexp // "Read#  <R>eply ... 0=exit"
	MsgLine         *regexp.Regexp // parses a listing row -> (id, author, subject)
	ReplyKey        string
	ReplyIDPrompt   *regexp.Regexp // "Message # to reply to:"
	ReplySubjPrompt *regexp.Regexp // "Subject [Re: ...]:"
	ReplyBodyPrompt *regexp.Regexp
}

// RegStep is one prompt in a signup script: wait for Prompt, then send the
// value named by Field ("alias","name","city","phone","street","state","zip",
// "email","password", or "" for a free-form question answered generically).
type RegStep struct {
	Prompt *regexp.Regexp
	Field  string
}

// BuiltinTresBBS returns the profile for tresbbs (github.com/jasondostal/tresbbs),
// derived from its live prompts and NEWUSER.QUE signup flow.
func BuiltinTresBBS() *Profile {
	re := regexp.MustCompile
	return &Profile{
		Name:        "tresbbs",
		ConnectWait: 8 * time.Second,
		ActWait:     4 * time.Second,

		UserPrompt: re(`User Name:\s*$`),
		PassPrompt: re(`Password:\s*$`),
		NewUserAsk: re(`New user\?`),
		NewUserYes: "Y",
		BadLogin:   re(`Incorrect password|locked|denied access`),
		Welcome:    re(`Welcome back|Welcome to`),
		Register: []RegStep{
			{re(`Choose an alias:`), "alias"},
			{re(`real name:`), "name"},
			{re(`Your city:`), "city"},
			{re(`Phone number:`), "phone"},
			{re(`Street address`), "street"},
			{re(`State \(optional\):`), "state"},
			{re(`ZIP code`), "zip"},
			{re(`Email address`), "email"},
			{re(`Choose a password:`), "password"},
			{re(`first computer\?`), ""},
			{re(`hear about this BBS\?`), ""},
		},

		MainMenu: re(`Enter Selection - \[`),
		Pause:    re(`Press Enter to continue`),
		PauseKey: "\r",
		Busy:     re(`(?i)all nodes busy|no free node|try again later`),

		ToMsgArea: "M",
		Logoff:    "G",

		MsgMenu:       re(`Enter Selection - \[C E R`),
		PostKey:       "E",
		ReadKey:       "R",
		MsgMenuToMain: "M",

		SubjectPrompt: re(`Subject:\s*$`),
		BodyPrompt:    re(`empty line to finish`),
		PostedOK:      re(`Message posted|Reply posted`),

		ReaderPrompt:    re(`0=exit:`),
		// A listing row: "  13 - 07/06/26 15:04 author          [RE] subject"
		// (no `$` anchor — tresbbs lines end in \r\n and \r would defeat it).
		MsgLine:         re(`(?m)^\s*(\d+)\s*-\s*\S+ \S+\s+(\S.*?)\s{2,}(?:\[RE\] )?([^\r\n]+)`),
		ReplyKey:        "R",
		ReplyIDPrompt:   re(`Message # to reply to:`),
		ReplySubjPrompt: re(`Subject \[`),
		ReplyBodyPrompt: re(`empty line to finish`),
	}
}
