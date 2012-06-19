package aprs

import (
	"bytes"
	"strings"
)

type MsgBody string

type APRSMessage struct {
	Original string
	Source   string
	Dest     string
	Path     []string
	Body     MsgBody
}

func (a APRSMessage) Type() PacketType {
	return PacketType(a.Body[0])
}

func ParseAPRSMessage(i string) APRSMessage {
	parts := strings.SplitN(i, ":", 2)

	srcparts := strings.SplitN(parts[0], ">", 2)
	pathparts := strings.Split(srcparts[1], ",")

	return APRSMessage{Original: i,
		Source: srcparts[0],
		Dest:   pathparts[0], Path: pathparts[1:],
		Body: MsgBody(parts[1])}
}

func (m *APRSMessage) String() string {
	b := bytes.NewBufferString(m.Source)
	b.WriteByte('>')
	b.WriteString(m.Dest)
	if len(m.Path) > 0 {
		b.WriteByte(',')
		b.WriteString(strings.Join(m.Path, ","))
	}
	b.WriteByte(':')
	b.WriteString(string(m.Body))
	return b.String()
}
