package referee

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/safe"
	"github.com/Agame7k/freezy-sim/internal/world"
)

// HeadRef is the AI head referee. The line referees (the world's rules engine) call fouls as they happen; the head
// referee keeps the event-wide record a real head referee keeps (VERBAL WARNINGS, YELLOW CARDS that carry between
// matches), decides the CARDS presented after each match (Game Manual 6.6.1, G211, G408), and runs the post-match
// field signals (purple: safe for field staff, then green: safe for all).
type HeadRef struct {
	hub  *bus.Hub
	path string

	mu       sync.Mutex
	ledger   map[int]*teamRecord // by team number, for the current tournament phase
	phase    int                 // FMS match type the ledger belongs to (cards and warnings clear between phases)
	pending  map[int]string      // cards to present after this match
	log      []Ruling            // recent rulings (event-wide)
	awardL3  map[string]bool     // G420: stations awarded LEVEL 3 this match
	shown    []PresentedCard     // cards presented after the last match (the 3D head referee holds them up)
	matchFor string
}

// PresentedCard is a card the head referee held up after a match.
type PresentedCard struct {
	Team int    `json:"team"`
	Card string `json:"card"`
	At   int64  `json:"at"` // unix ms
}

type teamRecord struct {
	Warnings  int  `json:"warnings"`
	Yellow    bool `json:"yellow"` // carrying a YELLOW CARD
	Red       int  `json:"red"`
	Strategic int  `json:"strategic"` // strategic G408 violations
	Fouls     int  `json:"fouls"`
}

// Ruling is one decision shown in the officials panel.
type Ruling struct {
	Time  string `json:"time"`
	Match string `json:"match"`
	Team  int    `json:"team,omitempty"`
	Text  string `json:"text"`
}

// HeadRefState is published on officials.headRef.
type HeadRefState struct {
	Rulings   []Ruling              `json:"rulings"`
	Ledger    map[string]teamRecord `json:"ledger"`
	Pending   map[string]string     `json:"pending"`
	Presented []PresentedCard       `json:"presented"`
}

func newHeadRef(hub *bus.Hub, dir string) *HeadRef {
	head := &HeadRef{hub: hub, path: filepath.Join(dir, "head-referee.json"), ledger: map[int]*teamRecord{},
		pending: map[int]string{}, awardL3: map[string]bool{}}
	head.load()
	return head
}

type headRefFile struct {
	Phase  int                 `json:"phase"`
	Ledger map[int]*teamRecord `json:"ledger"`
	Log    []Ruling            `json:"log"`
}

func (head *HeadRef) load() {
	data, err := os.ReadFile(head.path)
	if err != nil {
		return
	}
	var file headRefFile
	if json.Unmarshal(data, &file) == nil && file.Ledger != nil {
		head.phase, head.ledger, head.log = file.Phase, file.Ledger, file.Log
	}
}

func (head *HeadRef) saveLocked() {
	data, _ := json.MarshalIndent(headRefFile{Phase: head.phase, Ledger: head.ledger, Log: head.log}, "", "  ")
	_ = os.MkdirAll(filepath.Dir(head.path), 0o755)
	if err := safe.WriteFile(head.path, data, 0o644); err != nil {
		head.hub.Warnf("referee", "Could not save the head referee ledger: %v", err)
	}
}

func (head *HeadRef) record(team int) *teamRecord {
	if head.ledger[team] == nil {
		head.ledger[team] = &teamRecord{}
	}
	return head.ledger[team]
}

func (head *HeadRef) ruleLocked(team int, text string) {
	ruling := Ruling{Time: time.Now().Format("15:04:05"), Match: head.matchFor, Team: team, Text: text}
	head.log = append(head.log, ruling)
	if len(head.log) > 200 {
		head.log = head.log[len(head.log)-200:]
	}
	head.hub.Logf("head-ref", "%s", text)
}

// StartMatch begins a match; matchType is the FMS type (practice, qualification, playoff): VERBAL WARNINGS and
// YELLOW CARDS clear between tournament phases.
func (head *HeadRef) StartMatch(name string, matchType int) {
	head.mu.Lock()
	if matchType != head.phase {
		if len(head.ledger) > 0 {
			head.ruleLocked(0, "New tournament phase: VERBAL WARNINGS and YELLOW CARDS cleared")
		}
		head.ledger = map[int]*teamRecord{}
		head.phase = matchType
	}
	head.pending = map[int]string{}
	head.awardL3 = map[string]bool{}
	head.matchFor = name
	head.saveLocked()
	head.mu.Unlock()
	head.publish()
}

// Review looks at a line referee's call and decides any escalation.
func (head *HeadRef) Review(call world.Infraction) {
	head.mu.Lock()
	defer head.mu.Unlock()
	if call.AwardLevel3 != "" {
		head.awardL3[call.AwardLevel3] = true
	}
	if call.Team == 0 {
		return
	}
	record := head.record(call.Team)
	record.Fouls++
	switch {
	case call.Egregious:
		head.cardLocked(call.Team, fmt.Sprintf("%s (%s)", call.Reason, call.Rule))
	case call.Strategic && call.Rule == "G408":
		record.Strategic++
		if record.Strategic == 1 {
			record.Warnings++
			head.ruleLocked(call.Team, fmt.Sprintf("VERBAL WARNING to %d: strategic catching under the HUB (G408)", call.Team))
		} else {
			head.cardLocked(call.Team, fmt.Sprintf("repeated strategic catching under the HUB (G408, %d times this phase)",
				record.Strategic))
		}
	}
	head.saveLocked()
}

// cardLocked queues a CARD for after the match: a YELLOW, or a RED for a team already carrying a YELLOW.
func (head *HeadRef) cardLocked(team int, reason string) {
	record := head.record(team)
	card := "yellow"
	if record.Yellow || head.pending[team] == "yellow" {
		card = "red"
	}
	head.pending[team] = card
	head.ruleLocked(team, fmt.Sprintf("%s CARD for %d after the match: %s", map[string]string{"yellow": "YELLOW",
		"red": "RED"}[card], team, reason))
}

// Awarded reports whether a station was awarded LEVEL 3 by a G420 call this match.
func (head *HeadRef) Awarded(station string) bool {
	head.mu.Lock()
	defer head.mu.Unlock()
	return head.awardL3[station]
}

// PresentCards returns the cards to enter for this match and updates the ledger (a YELLOW carries forward).
func (head *HeadRef) PresentCards() map[int]string {
	head.mu.Lock()
	defer head.mu.Unlock()
	cards := head.pending
	head.shown = nil
	for team, card := range cards {
		head.shown = append(head.shown, PresentedCard{Team: team, Card: card, At: time.Now().UnixMilli()})
		record := head.record(team)
		if card == "yellow" {
			record.Yellow = true
		} else {
			record.Red++
			record.Yellow = true // a team that has received a RED CARD carries a YELLOW into later matches
		}
	}
	head.pending = map[int]string{}
	head.saveLocked()
	return cards
}

// CarryingYellow lists teams in this match that carry a YELLOW CARD from earlier matches (shown, not re-entered).
func (head *HeadRef) CarryingYellow(teams [6]int) []int {
	head.mu.Lock()
	defer head.mu.Unlock()
	var carrying []int
	for _, team := range teams {
		if record := head.ledger[team]; team > 0 && record != nil && record.Yellow {
			carrying = append(carrying, team)
		}
	}
	return carrying
}

func (head *HeadRef) publish() {
	head.mu.Lock()
	state := HeadRefState{Rulings: append([]Ruling(nil), head.log...), Ledger: map[string]teamRecord{},
		Pending: map[string]string{}, Presented: append([]PresentedCard(nil), head.shown...)}
	if len(state.Rulings) > 40 {
		state.Rulings = state.Rulings[len(state.Rulings)-40:]
	}
	for team, record := range head.ledger {
		state.Ledger[strconv.Itoa(team)] = *record
	}
	for team, card := range head.pending {
		state.Pending[strconv.Itoa(team)] = card
	}
	head.mu.Unlock()
	head.hub.Publish("officials.headRef", state)
}

// Note records a ruling from outside a foul review (field signals, assessments).
func (head *HeadRef) Note(text string) {
	head.mu.Lock()
	head.ruleLocked(0, text)
	head.saveLocked()
	head.mu.Unlock()
	head.publish()
}
