// Package llm is a multi-provider, OpenAI-compatible chat client. Each persona
// names its brain as "provider:model" (e.g. "deepseek:deepseek-v4-flash",
// "xiaomi:mimo-v2.5-pro-ultraspeed", "openrouter:openai/gpt-oss-120b:free"). A
// bare model with no known provider prefix defaults to OpenRouter. The model IS
// the personality — so casting across providers is how behavior diverges.
// Mock mode returns canned actions so the whole loop runs offline with no spend.
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
	"strings"
	"time"
)

// provider is one OpenAI-compatible endpoint. Any provider's base URL can be
// overridden at runtime with CARRIER_<PROVIDER>_BASE_URL (e.g.
// CARRIER_LMSTUDIO_BASE_URL=http://192.168.1.57:1234/v1) so the local voice
// model can live on another box without a code change.
type provider struct {
	name    string
	baseURL string
	keyEnv  string
	noAuth  bool // local endpoints (LM Studio) take no API key
}

// providers mirrors the wiring already proven in the pi harness (models.json),
// plus lmstudio: a local, OpenAI-compatible endpoint for the fine-tuned voice
// model. The model IS the personality for the cast; the voice model is the
// *shared* period voice the engine reaches for when it needs prose.
var providers = map[string]provider{
	"openrouter": {name: "openrouter", baseURL: "https://openrouter.ai/api/v1", keyEnv: "OPENROUTER_API_KEY"},
	"deepseek":   {name: "deepseek", baseURL: "https://api.deepseek.com/v1", keyEnv: "DEEPSEEK_API_KEY"},
	"xiaomi":     {name: "xiaomi", baseURL: "https://api.xiaomimimo.com/v1", keyEnv: "XIAOMI_MIMO_API_KEY"},
	"lmstudio":   {name: "lmstudio", baseURL: "http://localhost:1234/v1", noAuth: true},
}

// route splits "provider:model" on the FIRST colon (so model ids keeping their
// own ":free" suffix survive). Unknown/absent prefix → OpenRouter.
func route(model string) (provider, string) {
	if i := strings.Index(model, ":"); i > 0 {
		if p, ok := providers[model[:i]]; ok {
			return p, model[i+1:]
		}
	}
	return providers["openrouter"], model
}

// Msg is one chat message.
type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client dispatches to whichever provider a model names. Keys are read from the
// environment at call time and never stored on disk or in the repo.
type Client struct {
	http *http.Client
	mock bool
}

// New builds a client. Mock mode needs no keys.
func New(mock bool) *Client {
	return &Client{http: &http.Client{Timeout: 180 * time.Second}, mock: mock}
}

// Mock reports whether the client is running canned/offline.
func (c *Client) Mock() bool { return c.mock }

type chatReq struct {
	Model            string  `json:"model"`
	Messages         []Msg   `json:"messages"`
	Temperature      float64 `json:"temperature"`
	MaxTokens        int     `json:"max_tokens"`
	FrequencyPenalty float64 `json:"frequency_penalty,omitempty"`
}

// Opts tunes one call. Zero values mean "use the sensible default" (see Chat).
type Opts struct {
	Temperature      float64
	MaxTokens        int
	FrequencyPenalty float64 // >0 discourages the token loops small models fall into
}

type chatResp struct {
	Choices []struct {
		Message Msg `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends messages to a model (routed to its provider) and returns the text,
// with defaults tuned for the decision/roleplay brains.
//
// 6000, not 1500: reasoning models (mimo-ultraspeed, deepseek) spend their
// budget in reasoning_content first — a cap they hit mid-think returns EMPTY
// content, which parse() silently turns into idle. MiMo burns 400–2400
// reasoning tokens on a routine turn before emitting the action JSON.
func (c *Client) Chat(ctx context.Context, model string, msgs []Msg) (string, error) {
	return c.ChatWith(ctx, model, msgs, Opts{Temperature: 0.8, MaxTokens: 6000})
}

// ChatWith is Chat with explicit sampling — the voice model uses it for short,
// repetition-penalized generations.
func (c *Client) ChatWith(ctx context.Context, model string, msgs []Msg, o Opts) (string, error) {
	if c.mock {
		return mockAction(model, msgs), nil
	}
	p, id := route(model)
	if env := os.Getenv("CARRIER_" + strings.ToUpper(p.name) + "_BASE_URL"); env != "" {
		p.baseURL = env
	}
	var key string
	if !p.noAuth {
		key = os.Getenv(p.keyEnv)
		if key == "" {
			return "", fmt.Errorf("%s not set for provider %q (or run with --mock)", p.keyEnv, p.name)
		}
	}
	if o.Temperature == 0 {
		o.Temperature = 0.8
	}
	if o.MaxTokens == 0 {
		o.MaxTokens = 6000
	}
	body, _ := json.Marshal(chatReq{Model: id, Messages: msgs, Temperature: o.Temperature, MaxTokens: o.MaxTokens, FrequencyPenalty: o.FrequencyPenalty})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if p.name == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/jasondostal/carrier")
		req.Header.Set("X-Title", "carrier")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s %d: %s", p.name, resp.StatusCode, snippet(raw))
	}
	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", err
	}
	if cr.Error != nil {
		return "", fmt.Errorf("%s: %s", p.name, cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("%s: empty response", p.name)
	}
	return cr.Choices[0].Message.Content, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}

// mockAction fabricates a valid Action JSON so the loop can be exercised
// offline, varying by a cheap hash so personas diverge and seeds replay.
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
