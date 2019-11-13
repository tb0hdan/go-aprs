// An APRS gateway.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tb0hdan/go-aprs"
	"github.com/tb0hdan/go-aprs/aprsis"
	"github.com/tb0hdan/go-aprs/ax25"
	"github.com/dustin/go-broadcast"
	"github.com/dustin/go-rs232"
)

var (
	server     = flag.String("server", "second.aprs.net:14580", "APRS-IS upstream")
	httpAddr   = flag.String("http", ":7373", "HTTP bind address")
	portString = flag.String("port", "", "Serial port KISS thing")
	call       = flag.String("call", "", "Your callsign (for APRS-IS)")
	pass       = flag.String("pass", "", "Your call pass (for APRS-IS)")
	filter     = flag.String("filter", "", "Optional filter for APRS-IS server")
	rawlog     = flag.String("rawlog", "", "Path to raw log messages")
	wdTime     = flag.Duration("watchdog_time", 5*time.Minute, "Close connection if a message hasn't been heard in this long")
)

var (
	logWriter = io.Writer(ioutil.Discard)
	radio     io.ReadWriteCloser
)

func reporter(b broadcast.Broadcaster) {
	ch := make(chan interface{})
	b.Register(ch)
	defer b.Unregister(ch)

	for msgi := range ch {
		msg := msgi.(aprs.Frame)
		pos, err := msg.Body.Position()
		if err == nil {
			log.Printf("%s sent a ``%v'' to %s:  ``%s'' at %v",
				msg.Source, msg.Body.Type(), msg.Dest, msg.Body, pos)
		} else {
			log.Printf("%s sent a ``%v'' to %s:  ``%s'' -- err=%v", msg.Source,
				msg.Body.Type(), msg.Dest, msg.Body, err)
		}
	}
}

type loggingInfoHandler struct{}

var annoyinglog sync.Once

func (*loggingInfoHandler) Info(msg string) {
	// Ignore this annoying repetitive message
	if strings.HasPrefix(msg, "# aprsc") {
		annoyinglog.Do(func() {
			log.Printf("info: %s", msg)
		})
		return
	}
	log.Printf("info: %s", msg)

}

func netClient(b broadcast.Broadcaster) error {
	is, err := aprsis.Dial("tcp", *server)
	if err != nil {
		return err
	}

	if err := is.Auth(*call, *pass, *filter); err != nil {
		return err
	}

	if *rawlog != "" {
		logWriter, err := os.OpenFile(*rawlog,
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
		is.SetRawLog(logWriter)
	}

	is.SetInfoHandler(&loggingInfoHandler{})

	wd := time.AfterFunc(*wdTime, func() { _ = is.Close() })
	for {
		msg, err := is.Next()
		if err != nil {
			return err
		}
		b.Submit(msg)
		wd.Reset(*wdTime)
	}
}

func readNet(b broadcast.Broadcaster) {
	if *call == "" {
		fmt.Fprintf(os.Stderr, "Your callsign is required.\n")
		flag.Usage()
		os.Exit(1)
	}
	if *pass == "" {
		fmt.Fprintf(os.Stderr, "Your call pass is required.\n")
		flag.Usage()
		os.Exit(1)
	}

	for {
		err := netClient(b)
		log.Printf("*** Error reading from net:  %v (restarting)", err)
		time.Sleep(time.Second)
	}
}

func readSerial(b broadcast.Broadcaster) {
	var err error
	radio, err = rs232.OpenPort(*portString, 57600, rs232.S_8N1)
	if err != nil {
		log.Fatalf("Error opening port: %s", err)
	}

	d := ax25.NewDecoder(radio)
	for {
		msg, err := d.Next()
		if err != nil {
			log.Fatalf("Error retrieving APRS message via KISS: %v", err)
		}
		b.Submit(msg)
	}
}

func main() {
	var serverNet, serverAddr string
	flag.StringVar(&serverNet, "is-net", "tcp", "Network for APRS-IS server")
	flag.StringVar(&serverAddr, "is-addr", ":10152", "Bind address for APRS-IS server")
	useSyslog := flag.Bool("syslog", false, "Log to syslog")
	flag.Parse()

	if *useSyslog {
		sl, err := syslog.New(syslog.LOG_INFO, "aprs-gate")
		if err != nil {
			log.Fatalf("Error initializing syslog")
		}
		log.SetOutput(sl)
		log.SetFlags(0)
	}

	broadcaster := broadcast.NewBroadcaster(100)

	// go reporter(broadcaster)
	go notify(broadcaster)

	if *server != "" {
		go readNet(broadcaster)
	}

	if *portString != "" {
		go readSerial(broadcaster)
	}

	go startIS(serverNet, serverAddr, broadcaster)

	log.Fatal(http.ListenAndServe(*httpAddr, nil))
}
