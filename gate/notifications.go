package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-aprs"
	"github.com/dustin/nma.go"
	"github.com/pmylund/go-cache"
	"github.com/rem7/goprowl"
)

const max_retries = 10

type notifier struct {
	Name     string
	Driver   string
	To       string
	Disabled bool
	Config   map[string]string
}

type notification struct {
	Event string `json:"event"`
	Msg   string `json:"msg"`
}

type notifyFun func(n notifier, note notification) error

var notifyFuns = map[string]notifyFun{
	"prowl":   notifyProwl,
	"webhook": notifyWebhook,
	"nma":     notifyMyAndroid,
}

func notifyMyAndroid(n notifier, note notification) (err error) {
	notifier := nma.New(n.Config["apikey"])

	i, err := strconv.Atoi(n.Config["priority"])
	if err != nil {
		return err
	}

	msg := nma.Notification{
		Application: n.Config["application"],
		Description: note.Msg,
		Event:       note.Event,
		Priority:    i,
	}

	return notifier.Notify(&msg)
}

func notifyProwl(n notifier, note notification) (err error) {
	p := goprowl.Goprowl{}
	p.RegisterKey(n.Config["apikey"])

	msg := goprowl.Notification{
		Application: n.Config["application"],
		Description: note.Msg,
		Event:       note.Event,
		Priority:    n.Config["priority"],
	}

	return p.Push(&msg)
}

func notifyWebhook(n notifier, note notification) (err error) {
	data, err := json.Marshal(note)
	if err != nil {
		return
	}

	r, err := http.Post(n.Config["url"], "application/json",
		strings.NewReader(string(data)))
	if err == nil {
		defer r.Body.Close()
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			err = errors.New(r.Status)
		}
	}
	return
}

func (n notifier) notify(note notification) {
	log.Printf("Sending notification:  %v", note)
	for i := 0; i < max_retries; i++ {
		if err := notifyFuns[n.Driver](n, note); err == nil {
			break
		} else {
			time.Sleep(1 * time.Second)
			log.Printf("Retrying notification %s due to %v", n.Name, err)
		}
	}
}

func loadNotifiers(path string) ([]notifier, error) {
	notifiers := []notifier{}

	f, err := os.Open(path)
	if err != nil {
		return notifiers, err
	}
	defer f.Close()

	d := json.NewDecoder(f)
	if err = d.Decode(&notifiers); err != nil {
		return notifiers, err
	}

	for _, v := range notifiers {
		if _, ok := notifyFuns[v.Driver]; !ok {
			log.Fatalf("Unknown driver '%s' in '%s'", v.Driver, v.Name)
		}
	}

	return notifiers, nil
}

func notify(b *broadcaster) {
	notifiers, err := loadNotifiers("notify.json")
	if err != nil {
		notifiers = []notifier{}
		log.Printf("No notifiers loaded because %v", err)
	}

	ch := make(chan aprs.APRSMessage)
	b.Register(ch)
	defer b.Unregister(ch)

	c := cache.New(10*time.Minute, time.Minute)

	for msg := range ch {
		k := fmt.Sprintf("%v %v %v", msg.Dest, msg.Source, msg.Body)

		_, found := c.Get(k)
		if found {
			log.Printf("Skipping duplicate message: %v", k)
			continue
		}

		c.Set(k, "hi", 0)

		note := notification{msg.Body.Type().String(), string(msg.Body)}
		for _, n := range notifiers {
			if n.To == msg.Dest.Call {
				go n.notify(note)
			} else if msg.Body.Type().IsMessage() &&
				msg.Body.Recipient().Call == n.To &&
				!strings.HasPrefix(msg.Body.Message(), "ack") {
				note.Msg = msg.Body.Message()
				go n.notify(note)
			}
		}
	}
}