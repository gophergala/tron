package tron

import (
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
)

func init() {
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(assetsPath+"/static"))))

	http.HandleFunc("/chat", chat)
	http.Handle("/chatWS", websocket.Handler(chatWS))

	http.HandleFunc("/", root)
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
