package someip

import (
	"bytes"
	"github.com/bkcarlos/ProtoWatch/internal/packet"
	"testing"
	"time"
)

func tcpPacket(seq uint32, payload []byte) packet.Datagram {
	d := datagram(payload, 101)
	d.Transport = "TCP"
	d.TCPSeq = seq
	d.TCPACK = true
	return d
}
func syn(s *Store, seq uint32) {
	d := tcpPacket(seq, nil)
	d.TCPSYN = true
	d.TCPACK = false
	s.Ingest(d)
}
func tcpStore() *Store { s := NewStore(); s.SetConfig(Config{Ports: []uint16{30501}}); return s }
func TestTCPFramingRetransmissionAndReordering(t *testing.T) {
	s := tcpStore()
	syn(s, 99)
	a := wire(0x1234, 1, 0x80, []byte("response"))
	b := wire(0x1234, 0x8001, 2, []byte("event"))
	stream := append(append([]byte{}, a...), b...)
	s.Ingest(tcpPacket(108, stream[8:20])) // reordered, including a split header
	s.Ingest(tcpPacket(108, stream[8:20])) // queued duplicate
	s.Ingest(tcpPacket(100, stream[:12]))  // overlap with queued bytes
	if s.state.MessageCount != 0 {
		t.Fatal("partial message emitted")
	}
	s.Ingest(tcpPacket(120, stream[20:])) // finishes a and contains b
	s.Ingest(tcpPacket(100, stream))      // complete retransmission
	if s.state.MessageCount != 2 || s.state.TCPMessages != 2 || s.state.Responses != 1 || s.state.Notifications != 1 {
		t.Fatal(s.state)
	}
	first, _ := s.Sample(1)
	second, _ := s.Sample(2)
	if string(first.Payload) != "response" || string(second.Payload) != "event" || first.Transport != "TCP" {
		t.Fatal(first, second)
	}
	if s.tcpBytes != 0 {
		t.Fatal("buffer leak", s.tcpBytes)
	}
	s.FinishTCP()
	if len(s.state.Warnings) != 0 {
		t.Fatal(s.state.Warnings)
	}
}
func TestTCPWrapBidirectionalAndTransportIsolation(t *testing.T) {
	s := tcpStore()
	syn(s, 0xfffffff8)
	response := wire(0x1234, 1, 0x80, []byte{1, 2, 3})
	s.Ingest(tcpPacket(0xfffffff9, response[:10]))
	s.Ingest(tcpPacket(3, response[10:]))
	req := tcpPacket(800, wire(0x1234, 1, 0, []byte{4}))
	req.Source, req.Destination = req.Destination, req.Source
	req.SourceIP, req.DestinationIP = req.DestinationIP, req.SourceIP
	req.SourcePort, req.DestinationPort = req.DestinationPort, req.SourcePort
	s.Ingest(req)
	s.Ingest(datagram(response, 101))
	if s.state.TCPMessages != 2 || s.state.UDPMessages != 1 || len(s.services) != 2 {
		t.Fatal(s.state, len(s.services))
	}
	for _, v := range s.services {
		if v.Provider != "192.0.2.30" {
			t.Fatal(v)
		}
	}
}
func TestTCPDiscoveryAndLargeMessage(t *testing.T) {
	s := tcpStore()
	p := offerPayload()
	p[37] = 6
	s.Ingest(datagram(sdWire(p), 100))
	syn(s, 10)
	payload := bytes.Repeat([]byte{0x5a}, 100000)
	msg := wire(0x1234, 1, 0x80, payload)
	for offset := 0; offset < len(msg); offset += 1200 {
		s.Ingest(tcpPacket(uint32(11+offset), msg[offset:min(offset+1200, len(msg))]))
	}
	sample, ok := s.Sample(1)
	if !ok || sample.InstanceID == nil || *sample.InstanceID != 1 || sample.Size != 100000 || !sample.Truncated || len(sample.Payload) != MaxPayload {
		t.Fatal(sample)
	}
}
func TestTCPGapsConflictsAndLimits(t *testing.T) {
	t.Run("gap and EOF", func(t *testing.T) {
		s := tcpStore()
		syn(s, 99)
		m := wire(1, 1, 0x80, []byte("abcd"))
		s.Ingest(tcpPacket(100, m[:8]))
		s.Ingest(tcpPacket(110, m[10:]))
		s.FinishTCP()
		if s.state.MessageCount != 0 || s.state.Warnings["tcp_incomplete"] != 1 || s.tcpBytes != 0 {
			t.Fatal(s.state)
		}
	})
	t.Run("conflicting queued overlap", func(t *testing.T) {
		s := tcpStore()
		syn(s, 99)
		m := wire(1, 1, 0x80, nil)
		s.Ingest(tcpPacket(108, m[8:]))
		bad := append([]byte{}, m...)
		bad[10] ^= 1
		s.Ingest(tcpPacket(100, bad))
		if s.state.MessageCount != 0 || s.state.Warnings["tcp_overlap_conflict"] != 1 || s.tcpBytes != 0 {
			t.Fatal(s.state)
		}
	})
	t.Run("length bomb", func(t *testing.T) {
		s := tcpStore()
		syn(s, 99)
		m := wire(1, 1, 0x80, nil)
		be.PutUint32(m[4:], 0xffffffff)
		s.Ingest(tcpPacket(100, m))
		if s.state.Warnings["tcp_message_limit"] != 1 || s.tcpBytes != 0 {
			t.Fatal(s.state)
		}
	})
	t.Run("far ahead", func(t *testing.T) {
		s := tcpStore()
		syn(s, 99)
		s.Ingest(tcpPacket(100+MaxTCPReorderBytes, []byte{1}))
		if s.state.Warnings["tcp_gap_limit"] != 1 {
			t.Fatal(s.state)
		}
	})
	t.Run("segment bound", func(t *testing.T) {
		s := tcpStore()
		syn(s, 99)
		for i := 0; i <= MaxTCPSegments; i++ {
			s.Ingest(tcpPacket(uint32(102+i*2), []byte{1}))
		}
		if s.state.Warnings["tcp_reorder_limit"] != 1 || s.tcpBytes != 0 {
			t.Fatal(s.state)
		}
	})
}
func TestTCPFinRstReuseAndIdle(t *testing.T) {
	s := tcpStore()
	syn(s, 99)
	msg := wire(1, 1, 0x80, nil)
	d := tcpPacket(100, msg)
	d.TCPFIN = true
	s.Ingest(d)
	s.Ingest(d)
	if s.state.MessageCount != 1 {
		t.Fatal("FIN retransmitted message")
	}
	syn(s, 99)
	s.Ingest(tcpPacket(100, msg)) // same tuple and ISN, closed generation
	if s.state.MessageCount != 2 {
		t.Fatal("new connection was suppressed")
	}
	s.Ingest(tcpPacket(116, msg[:5]))
	d = tcpPacket(121, nil)
	d.TCPRST = true
	s.Ingest(d)
	if s.state.Warnings["tcp_incomplete"] != 1 || s.tcpBytes != 0 {
		t.Fatal(s.state)
	}
	syn(s, 500)
	s.Ingest(tcpPacket(501, msg[:5]))
	s.ExpireTCP(time.Unix(162, 0))
	if s.state.Warnings["tcp_incomplete"] != 2 || len(s.tcp) != 0 || s.tcpBytes != 0 {
		t.Fatal(s.state)
	}
}
func TestTCPUnknownSplitHeaderCookiesAndInvalidTraffic(t *testing.T) {
	s := NewStore()
	msg := wire(1, 1, 0x80, []byte("ok"))
	s.Ingest(tcpPacket(100, msg[:5]))
	s.Ingest(tcpPacket(105, msg[5:]))
	sample, _ := s.Sample(1)
	if sample.Detection != "header_heuristic" || s.state.Warnings["tcp_midstream"] != 1 {
		t.Fatal(sample, s.state)
	}
	s = tcpStore()
	syn(s, 99)
	cookie := []byte{0xff, 0xff, 0, 0, 0, 0, 0, 8, 0xde, 0xad, 0xbe, 0xef, 1, 1, 1, 0}
	s.Ingest(tcpPacket(100, append(cookie, msg...)))
	if s.state.MessageCount != 1 || s.state.Warnings["tcp_magic_cookie"] != 1 {
		t.Fatal(s.state)
	}
	s = NewStore()
	s.Ingest(tcpPacket(100, []byte("GET / HTTP/1.1\r\n\r\n")))
	s.FinishTCP()
	if s.state.MessageCount != 0 || s.state.Packets != 0 || len(s.state.Warnings) != 0 {
		t.Fatal(s.state)
	}
}
func TestTCPGlobalAndConnectionBounds(t *testing.T) {
	s := tcpStore()
	for i := 0; i < MaxTCPConnections+1; i++ {
		d := tcpPacket(0, nil)
		d.SourcePort = uint16(1000 + i)
		d.Source = "client:" + Hex(uint16(i))
		d.TCPSYN = true
		d.TCPACK = false
		s.Ingest(d)
	}
	if len(s.tcp) != MaxTCPConnections || s.state.Warnings["tcp_connection_limit"] != 1 {
		t.Fatal(len(s.tcp), s.state)
	}
	s = tcpStore()
	// Force aggregate content over 32 MiB using several incomplete, valid large messages.
	m := wire(1, 1, 0x80, bytes.Repeat([]byte{1}, 60000))
	be.PutUint32(m[4:], MaxTCPMessage-8)
	for i := 0; i < 12; i++ {
		d := tcpPacket(100, m)
		d.SourcePort = uint16(30501 + i)
		d.Source = "server:" + Hex(uint16(i))
		s.Ingest(d)
		for j := 0; j < 60; j++ {
			d.TCPSeq += uint32(len(d.Payload))
			d.Payload = m[16:]
			s.Ingest(d)
		}
	}
	if s.tcpBytes > MaxTCPBuffered || s.state.Warnings["tcp_buffer_limit"] == 0 {
		t.Fatal(s.tcpBytes, s.state.Warnings)
	}
	s.FinishTCP()
	if s.tcpBytes != 0 {
		t.Fatal("leak")
	}
}
func FuzzTCPStream(f *testing.F) {
	f.Add(wire(1, 1, 0x80, []byte("hello")), uint32(100), byte(5))
	f.Fuzz(func(t *testing.T, b []byte, seq uint32, cut byte) {
		if len(b) > 100000 {
			return
		}
		s := tcpStore()
		syn(s, seq-1)
		n := min(int(cut), len(b))
		s.Ingest(tcpPacket(seq+uint32(n), b[n:]))
		s.Ingest(tcpPacket(seq, b[:n]))
		s.Ingest(tcpPacket(seq, b))
		s.FinishTCP()
		if s.tcpBytes != 0 {
			t.Fatal("buffer leak")
		}
	})
}
