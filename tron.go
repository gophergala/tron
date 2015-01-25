package tron

import (
  "encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"

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

type wsData struct{
  Type string
  Body json.RawMessage
}

func Join(ws *websocket.Conn) {
  data := struct{
    Room string
  }{}
  if err := websocket.JSON.Receive(ws, &data); err != nil {
    return
  }
  me := &Player{
    Arena: make(chan *Arena),
    GameEnd: make(chan struct{}),
  }
  room, err := hall.EnterRoom(data.Room, me)
  if err != nil {
    return
  }
  defer hall.LeaveRoom(data.Room, me)
  game, color := room.Ready(me)
  if err := websocket.JSON.Send(ws, struct{Color Color}{Color: color}); err != nil {
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
        resp := struct{
          Type string
          Color Color
        }{
          Type: "Ready",
          Color: color,
        }
        if err := websocket.JSON.Send(ws, resp); err != nil {
          return
        }
      case "Move":
        body := struct{
          Direction Direction
        }{}
        if err := json.Unmarshal(data.Body, &body); err != nil {
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
      if err := websocket.JSON.Send(ws, arena); err != nil {
        return
      }
    case ge := <-me.GameEnd:
      if err := websocket.JSON.Send(ws, ge); err != nil {
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
	}{}
	if AppID == "" {
		page.IP = "127.0.0.1"
	} else {
		page.IP, _ = aws.PublicIPv4()
	}
	chatTmpl.Execute(w, page)
}

var rootTmpl = template.Must(template.ParseFiles(fmt.Sprintf("%s/tmpl/index.html", assetsPath)))

func root(w http.ResponseWriter, r *http.Request) {
	page := struct{}{}
	rootTmpl.Execute(w, page)
}
