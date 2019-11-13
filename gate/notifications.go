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

	"github.com/tb0hdan/go-aprs"
	"github.com/dustin/go-broadcast"
	"github.com/dustin/go-nma"
	"github.com/pmylund/go-cache"
	"github.com/rem7/goprowl"
)

const maxRetries = 10

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

func notifyMyAndroid(n notifier, note notification) error {
	notifier := nma.New(n.Config["apikey"])

	i, err := strconv.Atoi(n.Config["priority"])
	if err != nil {
		return err
	}

	msg := nma.Notification{
		Application: n.Config["application"],
		Description: note.Msg,
		Event:       note.Event,
		Priority:    nma.PriorityLevel(i),
	}

	return notifier.Notify(&msg)
}

func notifyProwl(n notifier, note notification) error {
	p := goprowl.Goprowl{}
	if err := p.RegisterKey(n.Config["apikey"]); err != nil {
		return err
	}

	msg := goprowl.Notification{
		Application: n.Config["application"],
		Description: note.Msg,
		Event:       note.Event,
		Priority:    n.Config["priority"],
	}

	return p.Push(&msg)
}

func notifyWebhook(n notifier, note notification) error {
	data, err := json.Marshal(note)
	if err != nil {
		return err
	}

	r, err := http.Post(n.Config["url"], "application/json",
		strings.NewReader(string(data)))
	if err == nil {
		defer r.Body.Close()
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			err = errors.New(r.Status)
		}
	}
	return err
}

func (n notifier) notify(note notification) {
	log.Printf("Sending notification:  %v", note)
	for i := 0; i < maxRetries; i++ {
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

func notify(b broadcast.Broadcaster) {
	notifiers, err := loadNotifiers("notify.json")
	if err != nil {
		notifiers = []notifier{}
		log.Printf("No notifiers loaded because %v", err)
	}

	ch := make(chan interface{})
	b.Register(ch)
	defer b.Unregister(ch)

	c := cache.New(time.Hour, time.Minute)

	for msgi := range ch {
		msg := msgi.(aprs.Frame)
		sender := msg.Source
		for msg.Body.Type().IsThirdParty() && len(msg.Body) > 1 {
			msg = aprs.ParseFrame(string(msg.Body[1:]))
		}
		k := fmt.Sprintf("%v %v %v", msg.Dest, msg.Source, msg.Body)

		_, found := c.Get(k)
		if found {
			// Already processed this one.
			continue
		}

		c.Set(k, "hi", 0)

		note := notification{msg.Body.Type().String(), fmt.Sprintf("%s: %s", sender, msg.Body)}
		m := msg.Message()
		if m.Parsed {
			note.Msg = fmt.Sprintf("%s: %s", sender, m.Body)
		}
		for _, n := range notifiers {
			if n.To == msg.Dest.Call || (m.Parsed && m.Recipient.Call == n.To && !m.IsACK()) {
				go n.notify(note)
			} else if m.IsBulletin() && n.To == "BLN" {
				note.Msg = fmt.Sprintf("BLN: %s", msg.Body)
				go n.notify(note)
			}
		}
	}
}
