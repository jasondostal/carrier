package voice

import (
	"context"
	"hash/fnv"
	"fmt"

	"github.com/jasondostal/carrier/internal/domain"
)

// Mock is an offline Composer: it fabricates a short, persona-flavored body so
// the whole engine loop runs with no inference backend and no spend. Output
// varies by handle so the cast still diverges and seeded runs replay.
type Mock struct{}

func (Mock) Compose(_ context.Context, p *domain.Persona, r Request) (string, error) {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%s|%s", p.Handle, r.Kind, r.To)
	n := int(h.Sum32())
	switch r.Kind {
	case domain.ActMail:
		lines := []string{
			"hey, saw your post. we should team up on the file area. dont tell the sysop.",
			"meet me in node chat later? got something to show you. bring gold.",
			"you were right about CrustyRon. hes all bark. anyway. hi :)",
		}
		return lines[n%len(lines)], nil
	case domain.ActReply:
		lines := []string{
			fmt.Sprintf("lol %s you cant be serious. thats the worst take on this whole board.", r.To),
			fmt.Sprintf("%s hit LORD level 4 today btw. where you at. all talk as usual.", r.To),
			"ratio check. upload something or quit whining about the file area.",
			"back in my day we RTFM'd instead of begging. try it sometime.",
		}
		return lines[n%len(lines)], nil
	default:
		lines := []string{
			"who keeps uploading broken .zip to the file area?? 0/10. name yourself.",
			"just war-dialed 200 numbers, found NOTHING. this scene is dead. step it up.",
			"anyone here NOT a boy arguing about modems? i have a bio test tmrw omg.",
			"got fresh warez. 2 uploads gets you access. no leeches. you know who you are.",
		}
		return lines[n%len(lines)], nil
	}
}
