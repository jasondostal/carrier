// Package llm is the OpenRouter (OpenAI-compatible) client. Each persona names
// its own model, so the model *is* the personality. Mock mode returns canned
// actions so the whole orchestration loop runs offline without spending.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"time"
)

const endpoint = "https://openrouter.ai/api/v1/chat/completions"

// Msg is one chat message.
type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to OpenRouter. The API key is read from OPENROUTER_API_KEY at
// runtime and never stored on disk or in the repo.
type Client struct {
	key  string
	http *http.Client
	mock bool
}

// New builds a client. Mock mode needs no key.
func New(mock bool) *Client {
	return &Client{
		key:  os.Getenv("OPENROUTER_API_KEY"),
		http: &http.Client{Timeout: 90 * time.Second},
		mock: mock,
	}
}

// Mock reports whether the client is running canned/offline.
func (c *Client) Mock() bool { return c.mock }

type chatReq struct {
	Model       string  `json:"model"`
	Messages    []Msg   `json:"messages"`
	Temperature float64 `json:"temperature"`
}

type chatResp struct {
	Choices []struct {
		Message Msg `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends messages to a model and returns the assistant text.
func (c *Client) Chat(ctx context.Context, model string, msgs []Msg) (string, error) {
	if c.mock {
		return mockAction(model, msgs), nil
	}
	if c.key == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY not set (or run with --mock)")
	}
	body, _ := json.Marshal(chatReq{Model: model, Messages: msgs, Temperature: 0.9})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("HTTP-Referer", "https://github.com/jasondostal/carrier")
	req.Header.Set("X-Title", "carrier")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openrouter %d: %s", resp.StatusCode, string(raw))
	}
	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", err
	}
	if cr.Error != nil {
		return "", fmt.Errorf("openrouter: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("openrouter: empty response")
	}
	return cr.Choices[0].Message.Content, nil
}

// mockAction fabricates a valid Action JSON so the loop can be exercised
// offline. It varies by a cheap hash of model + last message, so different
// personas diverge and a given seed replays identically.
func mockAction(model string, msgs []Msg) string {
	h := fnv.New32a()
	h.Write([]byte(model))
	if len(msgs) > 0 {
		h.Write([]byte(msgs[len(msgs)-1].Content))
	}
	n := int(h.Sum32())
	switch []string{"post", "post", "reply", "door", "mail", "post"}[n%6] {
	case "door":
		mv := []string{"forest", "inn"}[n%2]
		return fmt.Sprintf(`{"action":"door","door_move":%q,"memory":"went to the %s in Red Dragon"}`, mv, mv)
	case "mail":
		return `{"action":"mail","to":"CrustyRon","secret":true,"body":"meet me in node chat at midnight ;)","memory":"shot my shot with a private mail"}`
	case "reply":
		bodies := []string{
			"lol you call THAT elite? my little sister codes better",
			"back in my day we didn't whine, we RTFM'd. try it sometime.",
			"ratio check. upload or get off my file area.",
		}
		return fmt.Sprintf(`{"action":"reply","board":"General","reply_to":1,"subject":"re:","body":%q,"memory":"clapped back at someone"}`, bodies[n%len(bodies)])
	default:
		subj := []string{"this board SUX", "the old days", "trade offer", "im the best h4x0r here", "netiquette lesson #47"}
		body := []string{
			"just war-dialed 200 numbers and found NOTHING good. this scene is dead. step it up.",
			"gather round children, let me tell you about REAL modems. 300 baud. uphill. both ways.",
			"got fresh warez. 2 uploads gets you access. no leeches. you know who you are.",
			"nobody on this board can touch my sk1llz. prove me wrong. i'll wait.",
			"reminder: TOP-POSTING is a bannable offense in a civilized board. act like it.",
		}
		i := n % len(subj)
		return fmt.Sprintf(`{"action":"post","board":"General","subject":%q,"body":%q,"memory":"posted my thoughts, as one does"}`, subj[i], body[i])
	}
}
