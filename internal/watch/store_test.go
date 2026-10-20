package watch

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

func frame(t *testing.T, seq uint32, payloadLen int) []byte {
	t.Helper()
	rtps := make([]byte, 44+payloadLen)
	copy(rtps, []byte("RTPS\x02\x03\x01\x0fabcdefghijkl"))
	rtps[20] = 0x15
	rtps[21] = 5
	binary.LittleEndian.PutUint16(rtps[22:24], uint16(20+payloadLen))
	binary.LittleEndian.PutUint16(rtps[26:28], 16)
	copy(rtps[32:36], []byte{0, 0, 1, 3})
	binary.LittleEndian.PutUint32(rtps[40:44], seq)
	eth := &layers.Ethernet{SrcMAC: []byte{0, 1, 2, 3, 4, 5}, DstMAC: []byte{0, 1, 2, 3, 4, 6}, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: []byte{192, 0, 2, 1}, DstIP: []byte{192, 0, 2, 2}}
	udp := &layers.UDP{SrcPort: 7411, DstPort: 7412}
	udp.SetNetworkLayerForChecksum(ip)
	b := gopacket.NewSerializeBuffer()
	if e := gopacket.SerializeLayers(b, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, udp, gopacket.Payload(rtps)); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func feed(t *testing.T, s *Store, seq uint32, n int, sec int64) {
	b := frame(t, seq, n)
	s.Ingest(b, gopacket.CaptureInfo{Timestamp: time.Unix(sec, 0), CaptureLength: len(b), Length: len(b)}, layers.LinkTypeEthernet)
}
func TestBoundedRetention(t *testing.T) {
	s := NewStore()
	for i := 1; i <= 300; i++ {
		feed(t, s, uint32(i), MaxPayload+1, 100)
	}
	v := s.Snapshot()
	if len(v.Samples) != 256 || v.Evicted != 44 || v.SampleCount != 300 {
		t.Fatalf("invalid retention: %d/%d/%d", len(v.Samples), v.Evicted, v.SampleCount)
	}
	p, ok := s.Sample(300)
	if !ok || !p.Truncated || len(p.Payload) != MaxPayload {
		t.Fatal("preview unbounded")
	}
	if _, ok = s.Sample(1); ok {
		t.Fatal("old sample retained")
	}
}
func TestOutOfOrderBucketDoesNotEraseRecentRate(t *testing.T) {
	s := NewStore()
	s.Reset("test", "file")
	feed(t, s, 1, 4, 100)
	feed(t, s, 2, 4, 95)
	v := s.Snapshot()
	if v.Topics[0].Hz != 0.2 {
		t.Fatalf("rate %f", v.Topics[0].Hz)
	}
}
func TestDomainSeparation(t *testing.T) {
	s := NewStore()
	a := uint32(0)
	b := uint32(1)
	s.participants["aaaaaaaaaaaaaaaaaaaaaaaa"] = &Participant{Domain: &a}
	s.participants["bbbbbbbbbbbbbbbbbbbbbbbb"] = &Participant{Domain: &b}
	for _, prefix := range []string{"aaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbb"} {
		g := prefix + "00000103"
		s.endpoints[g] = &Endpoint{GUID: g, Kind: "writer", Topic: "same", Type: "same"}
	}
	if v := s.Snapshot(); len(v.Topics) != 2 {
		t.Fatal("merged different domains")
	}
}

func TestConcurrentSnapshotAndIngest(t *testing.T) {
	s := NewStore()
	frames := make([][]byte, 100)
	for i := range frames {
		frames[i] = frame(t, uint32(i+1), 4)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, b := range frames {
			s.Ingest(b, gopacket.CaptureInfo{Timestamp: time.Unix(100, 0), CaptureLength: len(b), Length: len(b)}, layers.LinkTypeEthernet)
		}
	}()
	for i := 0; i < 100; i++ {
		s.Snapshot()
		s.Sample(1)
	}
	<-done
	if s.Snapshot().SampleCount != 100 {
		t.Fatal("samples lost during concurrent reads")
	}
}
