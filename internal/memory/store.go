// Package memory is the file-based persona mind: a character card
// (persona.yaml), an append-only episodic stream (memory.jsonl), and a
// relationships file (relationships.yaml). No database on purpose — the colony's
// minds are openable, greppable, diffable, and part of the demo.
package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jasondostal/carrier/internal/domain"
	"gopkg.in/yaml.v3"
)

type personaFile struct {
	Handle   string   `yaml:"handle"`
	Name     string   `yaml:"name"`
	Model    string   `yaml:"model"`
	Bio      string   `yaml:"bio"`
	Style    string   `yaml:"style"`
	Goals    []string `yaml:"goals"`
	CallUrge float64  `yaml:"call_urge"`
}

// Store manages one persona's on-disk mind.
type Store struct{ dir string }

// Bank maps persona id -> Store.
type Bank map[string]*Store

type entry struct {
	Tick int    `json:"tick"`
	Text string `json:"text"`
}

// Load reads every personas/<id>/persona.yaml under root and returns the
// personas plus a memory Bank keyed by id. A directory without a card is
// skipped rather than fatal.
func Load(root string) ([]*domain.Persona, Bank, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	var personas []*domain.Persona
	bank := Bank{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		dir := filepath.Join(root, id)
		raw, err := os.ReadFile(filepath.Join(dir, "persona.yaml"))
		if err != nil {
			continue
		}
		var pf personaFile
		if err := yaml.Unmarshal(raw, &pf); err != nil {
			return nil, nil, err
		}
		personas = append(personas, &domain.Persona{
			ID: id, Handle: pf.Handle, Name: pf.Name, Model: pf.Model,
			Bio: pf.Bio, Style: pf.Style, Goals: pf.Goals, CallUrge: pf.CallUrge,
		})
		bank[id] = &Store{dir: dir}
	}
	sort.Slice(personas, func(i, j int) bool { return personas[i].ID < personas[j].ID })
	return personas, bank, nil
}

// Recent returns up to n most-recent memory lines, oldest first.
func (s *Store) Recent(n int) []string {
	f, err := os.Open(filepath.Join(s.dir, "memory.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var en entry
		if json.Unmarshal([]byte(line), &en) == nil {
			all = append(all, en.Text)
		}
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// Append adds one episodic memory line.
func (s *Store) Append(tick int, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "memory.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(entry{Tick: tick, Text: text})
	_, err = f.Write(append(b, '\n'))
	return err
}

// Relationships returns the raw relationships.yaml text, injected into the
// prompt verbatim so the model reads a caller's grudges and crushes as-is.
func (s *Store) Relationships() string {
	raw, err := os.ReadFile(filepath.Join(s.dir, "relationships.yaml"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
