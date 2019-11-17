package aprsis

import (
	"encoding/json"
	"fmt"
	"log"
)

type APRSParsed struct {
	Raw  string   `json:"raw"`
	From string   `json:"from"`
	To   string   `json:"to"`
	Path []string `json:"path"`
	Via  string   `json:"via"`
	// Position + moving stations
	Latitude          float32 `json:"latitude,omitempty"`
	Longitude         float32 `json:"longitude,omitempty"`
	PositionAmbiguity int32   `json:"posambiguity,omitempty"`
	Speed             float32 `json:"speed,omitempty"`
	Course            float32 `json:"course,omitempty"`
	Symbol            string  `json:"symbol,omitempty"`
	SymbolTable       string  `json:"symbol_table,omitempty"`
	// Telemetry
}

// python aprslib compatibility
func APRSROConsumer(callback func(packet string)) {
	client, err := NewROAPRS()

	if err != nil {
		log.Fatal("login", err)
	}

	defer client.Close()

	go client.ManageConnection()

	for frame := range client.GetIncomingMessages() {
		from := frame.Source.Call
		if frame.Source.SSID != "" {
			from += "-" + frame.Source.SSID
		}
		to := frame.Dest.Call
		if frame.Dest.SSID != "" {
			to += "-" + frame.Dest.SSID
		}

		path := make([]string, 0)
		for _, p := range frame.Path {
			path = append(path, p.String())
		}

		via := path[len(path)-1]
		pos, err := frame.Body.Position()
		if err != nil {
			fmt.Println("Not a position packet!", frame.Original)
			continue
		}

		//  'object_name': '224.900IN', 'alive': True, 'raw_timestamp': '161802z', 'timestamp': 1573927320, 'format': 'object', 'posambiguity': 0, 'symbol': 'r', 'symbol_table': '/', 'latitude': 41.630833333333335, 'longitude': -85.9605, 'comment': '131.8 K9DEW/R', 'object_format': 'uncompressed'}
		// 'messagecapable': False, 'format': 'uncompressed', 'posambiguity': 0, 'symbol': '&', 'symbol_table': 'D', 'latitude': 50.879, 'longitude': 0.5931666666666667, 'rng': 64.37376, 'comment': '440 Voice 439.5375  -9.00 MHz'}
		p := &APRSParsed{
			Raw:               frame.Original,
			From:              from,
			To:                to,
			Path:              path,
			Via:               via,
			PositionAmbiguity: int32(pos.Ambiguity),
			Latitude:          float32(pos.Lat),
			Longitude:         float32(pos.Lon),
			Speed:             float32(pos.Velocity.Speed),
			Course:            float32(pos.Velocity.Course),
			Symbol:            string(pos.Symbol.Symbol), // <-- FIXME: use proper symbols according to table
			SymbolTable:       string(pos.Symbol.Table),
		}
		out, err := json.Marshal(p)
		if err != nil {
			break
		}
		callback(string(out))
	}
}
