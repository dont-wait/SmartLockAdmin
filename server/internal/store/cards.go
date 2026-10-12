package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"smartlockadmin/server/internal/model"
)

type Cards struct {
	mu    sync.RWMutex
	path  string
	cards []model.Card
}

func OpenCards(path string) (*Cards, error) {
	s := &Cards{path: path, cards: []model.Card{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		var card model.Card
		if err := json.Unmarshal(entry, &card); err == nil {
			s.cards = append(s.cards, card)
			continue
		}
		var uid string
		if err := json.Unmarshal(entry, &uid); err != nil {
			return nil, err
		}
		s.cards = append(s.cards, model.Card{Uid: uid, Email: ""})
	}
	if s.cards == nil {
		s.cards = []model.Card{}
	}
	return s, nil
}

func (s *Cards) List() []model.Card {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.Card(nil), s.cards...)
}

func (s *Cards) Replace(cards []model.Card) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cards == nil {
		cards = []model.Card{}
	}
	if err := s.persist(cards); err != nil {
		return err
	}
	s.cards = append([]model.Card(nil), cards...)
	return nil
}

func (s *Cards) Enroll(uid string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, card := range s.cards {
		if card.Uid == uid {
			return false, nil
		}
	}
	cards := append(append([]model.Card(nil), s.cards...), model.Card{Uid: uid, Email: ""})
	if err := s.persist(cards); err != nil {
		return false, err
	}
	s.cards = cards
	return true, nil
}

func (s *Cards) Find(uid string) (model.Card, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, card := range s.cards {
		if card.Uid == uid {
			return card, true
		}
	}
	return model.Card{}, false
}

func (s *Cards) persist(cards []model.Card) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cards, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
