package someip

import (
	"fmt"
	"github.com/bkcarlos/ProtoWatch/internal/packet"
	"testing"
	"time"
)

func datagram(b []byte, sec int64) packet.Datagram {
	return packet.Datagram{Source: "192.0.2.30:30501", Destination: "192.0.2.40:40000", SourceIP: "192.0.2.30", DestinationIP: "192.0.2.40", SourcePort: 30501, DestinationPort: 40000, Transport: "UDP", Payload: b, Time: time.Unix(sec, 0), Packet: 1}
}
func offer(s *Store, sec int64) {
	d := datagram(sdWire(offerPayload()), sec)
	d.Source = "192.0.2.30:30490"
	d.SourcePort = 30490
	s.Ingest(d)
}
func TestOfferExpiryStopAndUnknownInstance(t *testing.T) {
	s := NewStore()
	offer(s, 100)
	s.Ingest(datagram(wire(0x1234, 1, 0x80, []byte{1}), 101))
	sample, ok := s.Sample(1)
	if !ok || sample.InstanceID == nil || *sample.InstanceID != 1 {
		t.Fatalf("instance: %+v", sample)
	}
	if v := s.Snapshot(time.Unix(103, 0)); v.Services[0].Status != "expired" {
		t.Fatal(v.Services)
	}
	s.Ingest(datagram(wire(0x1234, 1, 0x80, nil), 104))
	sample, _ = s.Sample(2)
	if sample.InstanceID != nil {
		t.Fatal("expired offer associated")
	}
	p := offerPayload()
	p[19] = 0
	d := datagram(sdWire(p), 105)
	d.SourcePort = 30490
	s.Ingest(d)
	offer(s, 99) // old packet must not revive a stopped service
	for _, v := range s.Snapshot(time.Unix(105, 0)).Services {
		if v.InstanceID != nil && v.Status != "stopped" {
			t.Fatal(v)
		}
	}
}
func TestInstanceIsolationAndAmbiguity(t *testing.T) {
	s := NewStore()
	offer(s, 100)
	p := offerPayload()
	p[15] = 2
	d := datagram(sdWire(p), 100)
	s.Ingest(d)
	s.Ingest(datagram(wire(0x1234, 1, 0x80, nil), 101))
	sample, _ := s.Sample(1)
	if sample.InstanceID != nil || s.state.Warnings["ambiguous_instance"] != 1 {
		t.Fatal(sample)
	}
	if len(s.services) != 3 {
		t.Fatal("instances merged")
	}
}

func TestProviderIsolationWithSameServiceAndInstance(t *testing.T) {
	for _, withSD := range []bool{false, true} {
		s := NewStore()
		for _, host := range []byte{30, 31} {
			ip := fmt.Sprintf("192.0.2.%d", host)
			if withSD {
				p := offerPayload()
				p[35] = host // IPv4 address in the SD endpoint option
				d := datagram(sdWire(p), 100)
				d.SourceIP, d.Source, d.SourcePort = ip, ip+":30490", 30490
				s.Ingest(d)
			}
			for _, kind := range []byte{0, 0x80, 2} {
				d := datagram(wire(0x1234, 1, kind, nil), 101)
				d.SourceIP, d.Source = ip, ip+":30501"
				if kind == 0 {
					d.SourceIP, d.DestinationIP = d.DestinationIP, d.SourceIP
					d.SourcePort, d.DestinationPort = d.DestinationPort, d.SourcePort
					d.Source, d.Destination = d.Destination, d.Source
				}
				s.Ingest(d)
			}
		}
		state := s.Snapshot(time.Unix(101, 0))
		if len(state.Services) != 2 || len(state.Members) != 2 || state.MessageCount != 6 {
			t.Fatalf("with SD %v: providers merged: %+v", withSD, state)
		}
		for _, service := range state.Services {
			if service.Messages != 3 || (service.InstanceID != nil) != withSD {
				t.Fatalf("incorrect provider association: %+v", service)
			}
			for _, sample := range state.Samples {
				if sample.ServiceKey == service.Key {
					endpoint := sample.Source
					if sample.MessageType == "REQUEST" {
						endpoint = sample.Destination
					}
					if endpoint != service.Provider+":30501" {
						t.Fatalf("wrong provider: %+v", sample)
					}
				}
			}
		}
	}
}
func TestBoundedSamplesNoSessionDedupAndTP(t *testing.T) {
	s := NewStore()
	b := wire(0x1234, 1, 0x80, make([]byte, MaxPayload+1))
	for i := 0; i < 300; i++ {
		s.Ingest(datagram(b, 100))
	} // same Session ID is not a global unique sample ID
	v := s.Snapshot(time.Unix(100, 0))
	if v.MessageCount != 300 || len(v.Samples) != 256 || v.Evicted != 44 {
		t.Fatal(v.MessageCount, len(v.Samples), v.Evicted)
	}
	sample, _ := s.Sample(300)
	if !sample.Truncated || len(sample.Payload) != MaxPayload {
		t.Fatal("unbounded preview")
	}
	if _, ok := s.Sample(1); ok {
		t.Fatal("retained evicted sample")
	}
	s.Ingest(datagram(wire(0x1234, 0x8001, 0x22, make([]byte, 4)), 100))
	if s.state.MessageCount != 300 || s.state.Warnings["tp_reassembly_unsupported"] != 1 {
		t.Fatal("TP counted as message")
	}
}
func TestConfigAndEnrichment(t *testing.T) {
	c, err := ParseConfig([]byte(`{"ports":[30501],"services":{"4660":{"name":"Vehicle","methods":{"0x0001":"Get"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	s.Ingest(datagram(wire(0x1234, 1, 0x80, nil), 100))
	s.SetConfig(c)
	sample, _ := s.Sample(1)
	if sample.ServiceName != "Vehicle" || sample.MethodName != "Get" {
		t.Fatal(sample)
	}
	s.Reset()
	if s.Config().Services["0x1234"].Name != "Vehicle" {
		t.Fatal("configuration lost on session reset")
	}
	for _, raw := range []string{`null`, `{"services":{"1":{},"0x1":{}}}`, `{"ports":[65536]}`, `{"ports":[0]}`, `{"unknown":true}`, `{} {}`, `{"services":{"bad":{}}}`} {
		if _, e := ParseConfig([]byte(raw)); e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestKnownPortMalformedAndTCP(t *testing.T) {
	s := NewStore()
	s.SetConfig(Config{Ports: []uint16{30501}})
	d := datagram([]byte{1, 2}, 100)
	s.Ingest(d)
	d.Transport = "TCP"
	s.Ingest(d)
	s.FinishTCP()
	if s.state.Warnings["parse_failed"] != 1 || s.state.Warnings["tcp_incomplete"] != 1 {
		t.Fatal(s.state.Warnings)
	}
}
func TestRateOutOfOrderAndLimits(t *testing.T) {
	s := NewStore()
	s.Ingest(datagram(wire(1, 1, 2, []byte{1}), 100))
	s.Ingest(datagram(wire(1, 1, 2, []byte{1}), 95))
	if s.Snapshot(time.Unix(100, 0)).Services[0].Hz != 0.2 {
		t.Fatal("old bucket overwrote recent rate")
	}
	for i := 2; i < MaxServices+20; i++ {
		s.Ingest(datagram(wire(uint16(i), 1, 2, nil), 100))
	}
	if len(s.services) != MaxServices || s.state.Warnings["service_limit"] == 0 {
		t.Fatal("unbounded services")
	}
}

func TestMemberAndDiscoveryRetentionLimits(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxMembers+10; i++ {
		s.Ingest(datagram(wire(0x1234, uint16(i), 0x80, nil), 100))
	}
	if len(s.members) != MaxMembers || s.state.Warnings["member_limit"] != 10 {
		t.Fatal("member limit", len(s.members), s.state.Warnings)
	}
	for i := 0; i < MaxDiscovery+20; i++ {
		offer(s, int64(101+i))
	}
	if len(s.discovery) != MaxDiscovery || s.state.DiscoveryEvicted != 20 {
		t.Fatal("discovery retention")
	}
}

func TestUnknownMinorAndAckDirection(t *testing.T) {
	s := NewStore()
	s.Ingest(datagram(wire(0x1234, 1, 0x80, nil), 99))
	if s.Snapshot(time.Unix(99, 0)).Services[0].Minor != nil {
		t.Fatal("invented minor version from data header")
	}
	offer(s, 100)
	s.Ingest(datagram(wire(0x1234, 1, 0x40, nil), 101))
	sample, _ := s.Sample(2)
	if sample.InstanceID == nil {
		t.Fatal("request acknowledgement must originate at server endpoint")
	}
}
