package tron

import (
	"fmt"
	"sync"
	"time"
)

type Color string

var Colors = []Color{"blue", "red", "green", "orange", "black", "purple"}

type JoinCmd struct {
	ColorC chan Color
	ArenaC chan Arena
}

type LeaveCmd struct {
	Color Color
}

type Direction string

const (
	DirectionUp    = "u"
	DirectionDown  = "d"
	DirectionLeft  = "l"
	DirectionRight = "r"
)

type MoveCmd struct {
	Color     Color
	Direction Direction
}

type Point struct {
	X float64
	Y float64
}

type Arena struct {
	Snakes map[Color][]Point
}

func NewArena(snakes map[Color][]Point) *Arena {
	a := Arena{
		Snakes: snakes,
	}
	return &a
}

// ChangeInitDirt sets the initial direction by altering the second point of the color's snake.
func (a *Arena) ChangeInitDirt(cmd MoveCmd) bool {
	snake := a.Snakes[cmd.Color]
	prevDirt := computeDirection(snake)
	changed := false
	if cmd.Direction != prevDirt {
		changed = true
		first := snake[0]
		switch cmd.Direction {
		case DirectionUp:
			snake[1] = Point{X: first.X, Y: first.Y + 1}
		case DirectionDown:
			snake[1] = Point{X: first.X, Y: first.Y - 1}
		case DirectionLeft:
			snake[1] = Point{X: first.X - 1, Y: first.Y}
		case DirectionRight:
			snake[1] = Point{X: first.X + 1, Y: first.Y}
		}
	}
	return changed
}

func computeDirection(snake []Point) Direction {
	var prevDirt Direction
	last := snake[len(snake)-1]
	pntm := snake[len(snake)-2]
	if last.X == pntm.X {
		if last.Y > pntm.Y {
			prevDirt = DirectionUp
		} else {
			prevDirt = DirectionDown
		}
	} else {
		if last.X > pntm.X {
			prevDirt = DirectionRight
		} else {
			prevDirt = DirectionLeft
		}
	}
	return prevDirt
}

// Update updates the state of an arena for a timestep.
func (a *Arena) Update(acts map[Color]Direction) {
	for color, snake := range a.Snakes {
		prevDirt := computeDirection(snake)
		dirt := prevDirt
		if act, ok := acts[color]; ok {
			dirt = act
		}

		var p Point
		last := snake[len(snake)-1]
		switch dirt {
		case DirectionUp:
			p = Point{X: last.X, Y: last.Y + 1}
		case DirectionDown:
			p = Point{X: last.X, Y: last.Y - 1}
		case DirectionLeft:
			p = Point{X: last.X - 1, Y: last.Y}
		case DirectionRight:
			p = Point{X: last.X + 1, Y: last.Y}
		}
		if dirt != prevDirt {
			a.Snakes[color] = append(snake, p)
		} else {
			a.Snakes[color][len(snake)-1] = p
		}
	}
}

func (a *Arena) Ended() bool {
	// TODO
	for _, snake := range a.Snakes {
		if len(snake) > 5 {
			return true
		}
	}
	return false
}

type Player struct {
	Arena   chan *Arena
	GameEnd chan struct{}
}

type Room struct {
	sync.RWMutex
	Players    map[*Player]struct{}
	MaxPlayers int
	Game       *Game
}

func NewRoom(maxPlayers int) *Room {
	r := Room{
		Players:    make(map[*Player]struct{}),
		MaxPlayers: maxPlayers,
	}
	return &r
}

func (r *Room) Ready(player *Player) (*Game, Color) {
	r.Lock()
	defer r.Unlock()
	if r.Game == nil {
		r.Game = NewGame(r.MaxPlayers)
	}
	game := r.Game

	var color Color
	for _, c := range Colors {
		if _, ok := game.Players[c]; !ok {
			color = c
			break
		}
	}
	game.Players[color] = player

	if len(game.Players) >= game.MinPlayers {
		go game.Start()
		r.Game = nil
	}
	return game, color
}

type Hall struct {
	sync.RWMutex
	m map[string]*Room
}

func (h *Hall) EnterRoom(name string, player *Player) (*Room, error) {
	h.Lock()
	defer h.Unlock()
	room, ok := h.m[name]
	if !ok {
		room = NewRoom(4)
		h.m[name] = room
	}

	if len(room.Players) >= room.MaxPlayers {
		return nil, fmt.Errorf("max players reached")
	}
	room.Players[player] = struct{}{}

	return room, nil
}

func (h *Hall) LeaveRoom(name string, player *Player) {
	h.Lock()
	defer h.Unlock()
	room, ok := h.m[name]
	if !ok {
		return
	}

	delete(room.Players, player)
	if len(room.Players) == 0 {
		delete(h.m, name)
	}
}

type Game struct {
	Players    map[Color]*Player
	MinPlayers int
	Move       chan MoveCmd
}

func NewGame(minPlayers int) *Game {
	game := Game{
		Players:    make(map[Color]*Player),
		MinPlayers: minPlayers,
		Move:       make(chan MoveCmd),
	}
	return &game
}

func (g *Game) broadcastArena(arena *Arena) {
	for _, p := range g.Players {
		select {
		case p.Arena <- arena:
		default:
		}
	}
}

func (g *Game) broadcastGameEnd() {
	for _, p := range g.Players {
		select {
		case p.GameEnd <- struct{}{}:
		default:
		}
	}
}

var initColors = []Point{
	Point{X: 333, Y: 200},
	Point{X: 666, Y: 200},
	Point{X: 333, Y: 400},
	Point{X: 666, Y: 400},
}

func (g *Game) Start() {
	// Select initial direction
	snakes := make(map[Color][]Point)
	i := 0
	for color, _ := range g.Players {
		s := make([]Point, 2)
		s[0] = initColors[i]
		s[1] = Point{s[0].X + 1, s[0].Y}
		snakes[color] = s
		i += 1
	}
	arena := NewArena(snakes)

	timer := time.After(3 * time.Second)
InitDirt:
	for {
		select {
		case cmd := <-g.Move:
			if ok := arena.ChangeInitDirt(cmd); ok {
				g.broadcastArena(arena)
			}
		case <-timer:
			break InitDirt
		}
	}

	// Game begins!
	for {
		acts := make(map[Color]Direction)
		timer = time.After(50 * time.Millisecond)
	CollectActs:
		for {
			select {
			case cmd := <-g.Move:
				acts[cmd.Color] = cmd.Direction
			case <-timer:
				break CollectActs
			}
		}
		arena.Update(acts)
		g.broadcastArena(arena)

		if arena.Ended() {
			g.broadcastGameEnd()
			return
		}
	}
}
