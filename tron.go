package tron

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/websocket"

	"github.com/gophergala/tron/aws"
)

var (
	assetsPath = os.Getenv("ASSETS_PATH")

	hall = &Hall{m: make(map[string]*Room)}
)

func init() {
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(assetsPath+"/static"))))

	http.HandleFunc("/chat", chat)
	http.Handle("/chatWS", websocket.Handler(chatWS))

	http.Handle("/Join", websocket.Handler(Join))
	http.HandleFunc("/", root)
}

type wsData struct {
	Type string
	Body json.RawMessage
}

type WSReady struct {
	Type  string
	Color Color
}

func NewWSReady(color Color) WSReady {
	return WSReady{Type: "Ready", Color: color}
}

type WSRefreshMap struct {
	Type  string
	State map[Color][]Point
}

func NewWSRefreshMap(arena *Arena) WSRefreshMap {
	canvas := make(map[Color][]Point)
	for color, snake := range arena.Snakes {
		canvas[color] = make([]Point, len(snake))
		for i := 0; i < len(canvas[color]); i++ {
			canvas[color][i].X = snake[i].X
			canvas[color][i].Y = arena.Size.Y - snake[i].Y
		}
	}
	return WSRefreshMap{Type: "RefreshMap", State: canvas}
}

type WSGameEnd struct {
	Type   string
	Winner Color
}

func NewWSGameEnd(winner Color) WSGameEnd {
	return WSGameEnd{Type: "GameEnd", Winner: winner}
}

type WSError struct {
	Type string
	Msg  string
}

func NewWSError(msg string) WSError {
	return WSError{Type: "Error", Msg: msg}
}

func Join(ws *websocket.Conn) {
	data := struct {
		Body struct {
			Room string
		}
	}{}
	if err := websocket.JSON.Receive(ws, &data); err != nil {
		return
	}
	me := &Player{
		Arena:   make(chan *Arena),
		GameEnd: make(chan Color),
	}
	room, err := hall.EnterRoom(data.Body.Room, me)
	if err != nil {
		websocket.JSON.Send(ws, NewWSError(err.Error()))
		return
	}
	defer hall.LeaveRoom(data.Body.Room, me)
	game, color := room.Ready(me)
	if err := websocket.JSON.Send(ws, NewWSReady(color)); err != nil {
		return
	}

	readStopped := make(chan struct{})
	go func() {
		defer close(readStopped)
		for {
			data := wsData{}
			if err := websocket.JSON.Receive(ws, &data); err != nil {
				return
			}
			switch data.Type {
			case "Leave":
				return
			case "Ready":
				game, color = room.Ready(me)
				if err := websocket.JSON.Send(ws, NewWSReady(color)); err != nil {
					return
				}
			case "Move":
				body := struct {
					Direction Direction
				}{}
				if err := json.Unmarshal(data.Body, &body); err != nil {
					glog.Errorf("%v", err)
					break
				}
				select {
				case game.Move <- MoveCmd{Color: color, Direction: body.Direction}:
				default:
				}
			}
		}
	}()

	for {
		select {
		case arena := <-me.Arena:
			if err := websocket.JSON.Send(ws, NewWSRefreshMap(arena)); err != nil {
				return
			}
		case ge := <-me.GameEnd:
			if err := websocket.JSON.Send(ws, NewWSGameEnd(ge)); err != nil {
				return
			}
		case <-readStopped:
			return
		}
	}
}

var chats = struct {
	sync.RWMutex
	m map[int64]chan []byte
}{m: make(map[int64]chan []byte)}

func chatWS(ws *websocket.Conn) {
	chats.Lock()
	key := time.Now().UnixNano()
	c := make(chan []byte, 256)
	chats.m[key] = c
	chats.Unlock()
	defer func() {
		chats.Lock()
		delete(chats.m, key)
		close(c)
		chats.Unlock()
	}()

	errC := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			n, err := ws.Read(b)
			if err != nil {
				errC <- err
				return
			}
			msg := b[0:n]
			chats.RLock()
			for _, c := range chats.m {
				select {
				case c <- msg:
				default:
				}
			}
			chats.RUnlock()
		}
	}()

L:
	for {
		select {
		case msg := <-c:
			if _, err := ws.Write(msg); err != nil {
				break L
			}
		case <-errC:
			break L
		}
	}
}

var chatTmpl = template.Must(template.ParseFiles(fmt.Sprintf("%s/tmpl/chat.html", assetsPath)))

func chat(w http.ResponseWriter, r *http.Request) {
	page := struct {
		IP string
	}{
		IP: PublicIPv4(),
	}
	chatTmpl.Execute(w, page)
}

var rootTmpl = template.Must(template.ParseFiles(fmt.Sprintf("%s/tmpl/index.html", assetsPath)))

func root(w http.ResponseWriter, r *http.Request) {
	page := struct {
		IP string
		}{
			IP: PublicIPv4(),
		}
	rootTmpl.Execute(w, page)
}

func PublicIPv4() string {
	if AppID == "" {
		return "web1.tunnlr.com:11630"
	} else {
		ip, _ := aws.PublicIPv4()
		return ip
	}
}
