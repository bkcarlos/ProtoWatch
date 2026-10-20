// Package someip implements bounded passive SOME/IP over UDP / TCP and SOME/IP-SD decoding.
package someip

import (
	"encoding/binary"
	"fmt"
	"net"
)

const MaxMessagesPerPacket = 1024

var be = binary.BigEndian

type Message struct {
	Service, Method, Client, Session uint16
	Interface, Type, ReturnCode      byte
	TP                               bool
	Payload                          []byte
	SD                               *Discovery
}
type Discovery struct {
	Flags    byte
	Entries  []Entry
	Warnings []string
}
type Endpoint struct {
	Address   string `json:"address"`
	Port      uint16 `json:"port"`
	Transport string `json:"transport"`
	Multicast bool   `json:"multicast"`
}
type Entry struct {
	Kind       string     `json:"kind"`
	Service    uint16     `json:"service_id"`
	Instance   uint16     `json:"instance_id"`
	Major      byte       `json:"major"`
	Minor      uint32     `json:"minor"`
	TTL        uint32     `json:"ttl"`
	Eventgroup *uint16    `json:"eventgroup_id,omitempty"`
	Counter    byte       `json:"counter"`
	Endpoints  []Endpoint `json:"endpoints"`
}

func TypeName(t byte) string {
	switch t &^ 0x20 {
	case 0:
		return "REQUEST"
	case 1:
		return "REQUEST_NO_RETURN"
	case 2:
		return "NOTIFICATION"
	case 0x80:
		return "RESPONSE"
	case 0x81:
		return "ERROR"
	case 0x40:
		return "REQUEST_ACK"
	case 0x41:
		return "REQUEST_NO_RETURN_ACK"
	case 0x42:
		return "NOTIFICATION_ACK"
	case 0xc0:
		return "RESPONSE_ACK"
	case 0xc1:
		return "ERROR_ACK"
	}
	return "UNKNOWN"
}

// Candidate is a heuristic, never proof of protocol identity. SD/configured endpoints
// supply stronger evidence; Parse still validates the entire UDP payload atomically.
func Candidate(b []byte) bool {
	return len(b) >= 16 && b[12] == 1 && be.Uint32(b[4:8]) >= 8 && TypeName(b[14]) != "UNKNOWN"
}
func Parse(b []byte) ([]Message, error) {
	if len(b) == 0 || len(b) > 65507 {
		return nil, fmt.Errorf("empty SOME/IP datagram")
	}
	return parseMessages(b)
}

func parseMessages(b []byte) ([]Message, error) {
	out := []Message{}
	for len(b) > 0 {
		if len(out) >= MaxMessagesPerPacket {
			return nil, fmt.Errorf("too many SOME/IP messages")
		}
		if len(b) < 16 {
			return nil, fmt.Errorf("incomplete SOME/IP header")
		}
		length := uint64(be.Uint32(b[4:8])) + 8
		if length < 16 || length > uint64(len(b)) {
			return nil, fmt.Errorf("invalid SOME/IP length")
		}
		if b[12] != 1 || TypeName(b[14]) == "UNKNOWN" {
			return nil, fmt.Errorf("unsupported SOME/IP header")
		}
		m := Message{Service: be.Uint16(b), Method: be.Uint16(b[2:]), Client: be.Uint16(b[8:]), Session: be.Uint16(b[10:]), Interface: b[13], Type: b[14], ReturnCode: b[15], TP: b[14]&0x20 != 0, Payload: b[16:int(length)]}
		if m.TP && len(m.Payload) < 4 {
			return nil, fmt.Errorf("missing SOME/IP-TP header")
		}
		if m.Service == 0xffff && m.Method == 0x8100 {
			if m.TP || m.Type != 2 || m.Interface != 1 || m.ReturnCode != 0 || m.Client != 0 {
				return nil, fmt.Errorf("invalid SD header")
			}
			var err error
			m.SD, err = parseSD(m.Payload)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, m)
		b = b[int(length):]
	}
	return out, nil
}
func parseSD(b []byte) (*Discovery, error) {
	if len(b) < 12 {
		return nil, fmt.Errorf("short SD payload")
	}
	n := uint64(be.Uint32(b[4:8]))
	if n%16 != 0 || n > uint64(len(b)-12) {
		return nil, fmt.Errorf("invalid SD entries length")
	}
	entries := b[8 : 8+int(n)]
	tail := b[8+int(n):]
	if uint64(be.Uint32(tail)) != uint64(len(tail)-4) {
		return nil, fmt.Errorf("invalid SD options length")
	}
	d := &Discovery{Flags: b[0], Entries: []Entry{}, Warnings: []string{}}
	options := []*Endpoint{}
	for data := tail[4:]; len(data) > 0; {
		if len(data) < 3 {
			return nil, fmt.Errorf("short SD option")
		}
		length := int(be.Uint16(data)) + 3
		if length < 4 || length > len(data) {
			return nil, fmt.Errorf("invalid SD option length")
		}
		o := data[:length]
		var ep *Endpoint
		switch o[2] {
		case 4, 0x14, 6, 0x16:
			addrLen := 4
			if o[2] == 6 || o[2] == 0x16 {
				addrLen = 16
			}
			if length != addrLen+8 {
				return nil, fmt.Errorf("invalid SD IP option length")
			}
			transport := ""
			switch o[5+addrLen] {
			case 17:
				transport = "UDP"
			case 6:
				transport = "TCP"
			default:
				d.Warnings = append(d.Warnings, "sd_transport_unsupported")
			}
			ep = &Endpoint{Address: net.IP(o[4 : 4+addrLen]).String(), Port: be.Uint16(o[6+addrLen:]), Transport: transport, Multicast: o[2] == 0x14 || o[2] == 0x16}
		default:
			d.Warnings = append(d.Warnings, "sd_option_unsupported")
		}
		options = append(options, ep)
		data = data[length:]
	}
	for len(entries) > 0 {
		e := entries[:16]
		entries = entries[16:]
		v := Entry{Service: be.Uint16(e[4:]), Instance: be.Uint16(e[6:]), Major: e[8], TTL: uint32(e[9])<<16 | uint32(e[10])<<8 | uint32(e[11]), Minor: be.Uint32(e[12:]), Endpoints: []Endpoint{}}
		switch e[0] {
		case 0:
			v.Kind = "FindService"
		case 1:
			v.Kind = "OfferService"
			if v.TTL == 0 {
				v.Kind = "StopOfferService"
			}
		case 6, 7:
			group := be.Uint16(e[14:])
			v.Eventgroup = &group
			v.Counter = e[13] & 15
			v.Minor = 0
			if e[0] == 6 {
				v.Kind = "SubscribeEventgroup"
				if v.TTL == 0 {
					v.Kind = "StopSubscribeEventgroup"
				}
			} else {
				v.Kind = "SubscribeEventgroupAck"
				if v.TTL == 0 {
					v.Kind = "SubscribeEventgroupNack"
				}
			}
		default:
			v.Kind = fmt.Sprintf("Unknown(0x%02x)", e[0])
			d.Warnings = append(d.Warnings, "sd_entry_unsupported")
		}
		for _, run := range [][2]int{{int(e[1]), int(e[3] >> 4)}, {int(e[2]), int(e[3] & 15)}} {
			if run[1] == 0 {
				continue
			}
			if run[0]+run[1] > len(options) {
				return nil, fmt.Errorf("SD option reference out of bounds")
			}
			for _, ep := range options[run[0] : run[0]+run[1]] {
				if ep != nil {
					v.Endpoints = append(v.Endpoints, *ep)
				}
			}
		}
		d.Entries = append(d.Entries, v)
	}
	return d, nil
}
