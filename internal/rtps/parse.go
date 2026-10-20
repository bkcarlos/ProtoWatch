// Package rtps implements a bounded, passive subset of DDSI-RTPS 2.x.
// It never creates a DDS participant or sends protocol traffic.
package rtps

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
)

type Discovery struct {
	GUID        string  `json:"guid"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name,omitempty"`
	Topic       string  `json:"topic,omitempty"`
	Type        string  `json:"type,omitempty"`
	Domain      *uint32 `json:"domain,omitempty"`
	Reliability string  `json:"reliability,omitempty"`
	Durability  string  `json:"durability,omitempty"`
}

type Data struct {
	Writer    string
	Sequence  string
	Payload   []byte
	Discovery *Discovery
	Builtin   bool
	KeyOnly   bool
}

type Message struct {
	Prefix   string
	Vendor   string
	Version  string
	Data     []Data
	Warnings []string
	Control  uint64
}

func order(little bool) binary.ByteOrder {
	if little {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

func Parse(p []byte) (Message, error) {
	var m Message
	if len(p) < 20 || string(p[:4]) != "RTPS" {
		return m, fmt.Errorf("invalid RTPS header")
	}
	if p[4] != 2 {
		return m, fmt.Errorf("unsupported RTPS major version %d", p[4])
	}
	m.Prefix = hex.EncodeToString(p[8:20])
	prefix := m.Prefix
	m.Vendor = hex.EncodeToString(p[6:8])
	m.Version = fmt.Sprintf("%d.%d", p[4], p[5])
	for pos := 20; pos < len(p); {
		if len(p)-pos < 4 {
			return m, fmt.Errorf("truncated submessage header")
		}
		id, flags := p[pos], p[pos+1]
		bo := order(flags&1 != 0)
		n := int(bo.Uint16(p[pos+2 : pos+4]))
		end := pos + 4 + n
		// INFO_TS with invalid timestamp and PAD may have an empty body.
		if n == 0 && id != 0x01 && !(id == 0x09 && flags&2 != 0) {
			end = len(p)
		}
		if end > len(p) {
			return m, fmt.Errorf("submessage 0x%02x exceeds packet", id)
		}
		b := p[pos+4 : end]
		pos = end
		switch id {
		case 0x0c: // INFO_SRC changes the source for subsequent submessages.
			if len(b) < 20 {
				return m, fmt.Errorf("short INFO_SRC")
			}
			prefix = hex.EncodeToString(b[8:20])
		case 0x15:
			if len(b) < 20 {
				return m, fmt.Errorf("short DATA")
			}
			off := 4 + int(bo.Uint16(b[2:4]))
			if off < 20 || off > len(b) {
				return m, fmt.Errorf("invalid inline QoS offset")
			}
			if flags&2 != 0 {
				_, used, err := parameters(b[off:], bo)
				if err != nil {
					return m, err
				}
				off += used
			}
			if flags&0x0c == 0 {
				m.Warnings = append(m.Warnings, "status_only_data")
				continue
			}
			if flags&0x0c == 0x0c {
				return m, fmt.Errorf("DATA has both data and key flags")
			}
			seq := int64(int32(bo.Uint32(b[12:16])))<<32 | int64(bo.Uint32(b[16:20]))
			d := Data{Writer: prefix + hex.EncodeToString(b[8:12]), Sequence: strconv.FormatInt(seq, 10), Payload: b[off:], Builtin: b[11]&0xc0 == 0xc0, KeyOnly: flags&8 != 0}
			writer := hex.EncodeToString(b[8:12])
			if d.Builtin && !d.KeyOnly && (writer == "000100c2" || writer == "000003c2" || writer == "000004c2") {
				disc, err := discovery(d.Payload, writer)
				if err != nil {
					m.Warnings = append(m.Warnings, "discovery_decode_failed")
				} else {
					d.Discovery = disc
				}
			}
			m.Data = append(m.Data, d)
		case 0x16:
			m.Warnings = append(m.Warnings, "rtps_fragment_unsupported")
		case 0x06, 0x07, 0x08, 0x12, 0x13:
			m.Control++
		case 0x30, 0x31, 0x32, 0x33, 0x34:
			m.Warnings = append(m.Warnings, "protected_rtps")
		case 0x01, 0x09, 0x0d, 0x0e, 0x0f: // Known context messages not needed by this MVP.
		default:
			m.Warnings = append(m.Warnings, "unknown_submessage")
		}
	}
	return m, nil
}

func parameters(b []byte, bo binary.ByteOrder) (map[uint16][]byte, int, error) {
	result := make(map[uint16][]byte)
	for pos, count := 0, 0; pos+4 <= len(b) && count < 1024; count++ {
		id, n := bo.Uint16(b[pos:pos+2]), int(bo.Uint16(b[pos+2:pos+4]))
		pos += 4
		if id == 1 {
			return result, pos, nil
		}
		if n > len(b)-pos || n%4 != 0 {
			return nil, 0, fmt.Errorf("invalid parameter length")
		}
		if id != 0 {
			result[id] = b[pos : pos+n]
		}
		pos += n
	}
	return nil, 0, fmt.Errorf("parameter list missing sentinel or too large")
}

func cdrString(b []byte, bo binary.ByteOrder) (string, error) {
	if len(b) < 4 {
		return "", fmt.Errorf("short CDR string")
	}
	n := int(bo.Uint32(b))
	if n < 1 || n > len(b)-4 || n > 4096 || b[4+n-1] != 0 {
		return "", fmt.Errorf("invalid CDR string")
	}
	return string(b[4 : 4+n-1]), nil
}

func discovery(payload []byte, writer string) (*Discovery, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("missing encapsulation")
	}
	enc := binary.BigEndian.Uint16(payload)
	if enc != 2 && enc != 3 {
		return nil, fmt.Errorf("unsupported discovery encoding")
	}
	bo := order(enc == 3)
	params, _, err := parameters(payload[4:], bo)
	if err != nil {
		return nil, err
	}
	d := &Discovery{Kind: "participant"}
	key := uint16(0x50)
	if writer != "000100c2" {
		key = 0x5a
		d.Kind = "writer"
		if writer == "000004c2" {
			d.Kind = "reader"
		}
	}
	g := params[key]
	if len(g) != 16 {
		return nil, fmt.Errorf("missing entity GUID")
	}
	d.GUID = hex.EncodeToString(g)
	for id, target := range map[uint16]*string{5: &d.Topic, 7: &d.Type, 0x62: &d.Name} {
		if raw, ok := params[id]; ok {
			s, e := cdrString(raw, bo)
			if e != nil {
				return nil, e
			}
			*target = s
		}
	}
	if b := params[0x0f]; len(b) >= 4 {
		v := bo.Uint32(b)
		d.Domain = &v
	}
	if b := params[0x1a]; len(b) >= 4 {
		switch bo.Uint32(b) {
		case 1:
			d.Reliability = "best_effort"
		case 2:
			d.Reliability = "reliable"
		}
	}
	if b := params[0x1d]; len(b) >= 4 {
		v := bo.Uint32(b)
		if v < 4 {
			d.Durability = []string{"volatile", "transient_local", "transient", "persistent"}[v]
		}
	}
	return d, nil
}
