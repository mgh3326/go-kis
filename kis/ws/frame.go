package ws

import (
	"strconv"
	"strings"
)

// KIS transaction IDs handled with typed parsing by this package.
const (
	// TRExecutionLive is the domestic execution notice on the live endpoint.
	TRExecutionLive = "H0STCNI0"
	// TRExecutionVTS is the domestic execution notice on the mock endpoint.
	// Its record layout is identical to TRExecutionLive.
	TRExecutionVTS = "H0STCNI9"
	// TRQuotePrice is the domestic executed-price stream.
	TRQuotePrice = "H0STCNT0"
	// TRQuoteBook is the domestic order-book stream.
	TRQuoteBook = "H0STASP0"
)

// realtimeFrame is one decoded pipe-delimited realtime message:
//
//	<encrypted flag>|<tr_id>|<record count>|<payload>
//
// The payload itself may contain pipes, so only the first three separators
// are structural.
type realtimeFrame struct {
	Encrypted bool
	TR        string
	Count     int
	Payload   string
}

// parseRealtime reports whether raw is a realtime data frame and, if so, its
// parts. A leading '{' marks a system/ACK message instead, and anything with
// fewer than four segments or a non-numeric count is not a realtime frame.
func parseRealtime(raw []byte) (realtimeFrame, bool) {
	text := string(raw)
	if strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), "{") {
		return realtimeFrame{}, false
	}
	parts := strings.SplitN(text, "|", 4)
	if len(parts) < 4 {
		return realtimeFrame{}, false
	}
	if parts[0] != "0" && parts[0] != "1" {
		return realtimeFrame{}, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil || parts[1] == "" {
		return realtimeFrame{}, false
	}
	return realtimeFrame{Encrypted: parts[0] == "1", TR: parts[1], Count: count, Payload: parts[3]}, true
}

// splitFields separates a realtime payload into its fields. KIS separates
// fields with '^'; a few payloads arrive pipe-separated instead, in which case
// empty segments are dropped.
func splitFields(payload string) []string {
	if strings.Contains(payload, "^") {
		return strings.Split(payload, "^")
	}
	fields := make([]string, 0, strings.Count(payload, "|")+1)
	for _, field := range strings.Split(payload, "|") {
		if field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

// field returns fields[index] or "" when the frame is shorter than expected.
// Short frames are data, not programming errors, so they never panic.
func field(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return fields[index]
}
