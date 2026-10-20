package capture

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/bkcarlos/ProtoWatch/internal/watch"
)

func demo(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("../server/web/demo.pcap")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestImportSyntheticNetwork(t *testing.T) {
	r, e := Reader(demo(t))
	if e != nil {
		t.Fatal(e)
	}
	s := watch.NewStore()
	s.Reset("synthetic", "file")
	if e = Import(context.Background(), r, s); e != nil {
		t.Fatal(e)
	}
	v := s.Snapshot()
	if v.Packets != 128 || v.SampleCount != 120 || len(v.Topics) != 3 || len(v.Endpoints) != 6 || len(v.Participants) != 2 || v.DiscoveryCount != 8 {
		t.Fatalf("unexpected network: %+v", v)
	}
	for _, topic := range v.Topics {
		if topic.Samples != 40 || topic.Writers != 1 || topic.Readers != 1 || topic.Domain != "0" || topic.Hz != 8 {
			t.Fatalf("unexpected topic: %+v", topic)
		}
	}
	if len(v.Warnings) != 0 {
		t.Fatalf("warnings: %v", v.Warnings)
	}
	// Reprocessing identical traffic must not double-count user samples.
	r, _ = Reader(demo(t))
	if e = Import(context.Background(), r, s); e != nil {
		t.Fatal(e)
	}
	v = s.Snapshot()
	if v.SampleCount != 120 || v.DuplicateCount != 120 {
		t.Fatalf("dedup failed: %d / %d", v.SampleCount, v.DuplicateCount)
	}
}
func TestImportBoundsAndTypes(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func([]byte)
	}{
		{"snaplen", func(b []byte) { binary.LittleEndian.PutUint32(b[16:20], 0xffffffff) }},
		{"unknown link", func(b []byte) { binary.LittleEndian.PutUint32(b[20:24], 257) }},
		{"pcapng", func(b []byte) { copy(b, []byte{0x0a, 0x0d, 0x0d, 0x0a}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := demo(t)
			test.edit(b)
			if _, e := Reader(b); e == nil {
				t.Fatal("accepted unsupported header")
			}
		})
	}
	b := demo(t)
	binary.LittleEndian.PutUint32(b[32:36], 0x7fffffff)
	r, e := Reader(b)
	if e != nil {
		t.Fatal(e)
	}
	if e = Import(context.Background(), r, watch.NewStore()); e == nil {
		t.Fatal("accepted huge packet allocation")
	}
}
func TestCancellation(t *testing.T) {
	r, _ := Reader(demo(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := Import(ctx, r, watch.NewStore()); e != context.Canceled {
		t.Fatal(e)
	}
}

func TestMixedProtocolsAndNameMapping(t *testing.T) {
	data, err := os.ReadFile("../server/web/mixed-demo.pcap")
	if err != nil {
		t.Fatal(err)
	}
	s := watch.NewStore()
	s.Reset("mixed", "file")
	r, err := Reader(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = Import(context.Background(), r, s); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot()
	if v.Packets != 230 || v.RTPSPackets != 128 || v.SampleCount != 120 || len(v.Topics) != 3 {
		t.Fatalf("DDS regression: %d/%d/%d", v.Packets, v.RTPSPackets, v.SampleCount)
	}
	a := v.SomeIP
	if a.Packets != 102 || a.MessageCount != 93 || a.DiscoveryCount != 9 || a.Requests != 31 || a.Responses != 30 || a.Notifications != 32 || a.Errors != 1 || len(a.Services) != 4 || len(a.Members) != 3 {
		t.Fatalf("SOME/IP counters: %+v", a)
	}
	statuses := map[uint16]string{}
	for _, service := range a.Services {
		statuses[service.ServiceID] = service.Status
	}
	if statuses[0x1234] != "offered" || statuses[0x2000] != "stopped" || statuses[0x3000] != "expired" || statuses[0x4000] != "observed" {
		t.Fatal(statuses)
	}
	if a.Warnings["tp_reassembly_unsupported"] != 1 {
		t.Fatal(a.Warnings)
	}
	if a.Samples[0].InstanceID != nil {
		t.Fatal("unknown instance guessed")
	}
	cfg, err := os.ReadFile("../../docs/examples/someip-names.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetSomeIPConfig(cfg); err != nil {
		t.Fatal(err)
	}
	sample, ok := s.SomeIPSample(1)
	if !ok || sample.ServiceName != "VehicleStatus" || sample.MethodName != "GetTemperature" || sample.InstanceID == nil || *sample.InstanceID != 1 {
		t.Fatal(sample)
	}
	s.Reset("new", "file")
	if s.Snapshot().SomeIP.MessageCount != 0 || len(s.SomeIPConfig().Services) != 4 {
		t.Fatal("reset must clear observations but keep mapping")
	}
}

func TestConcurrentMixedIngestAndMapping(t *testing.T) {
	data, err := os.ReadFile("../server/web/mixed-demo.pcap")
	if err != nil {
		t.Fatal(err)
	}
	s := watch.NewStore()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 10; i++ {
			r, e := Reader(data)
			if e != nil {
				done <- e
				return
			}
			if e = Import(context.Background(), r, s); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 40; i++ {
		s.Snapshot()
		s.SomeIPSample(1)
		if e := s.SetSomeIPConfig([]byte(`{"services":{"0x1234":{"name":"Concurrent"}}}`)); e != nil {
			t.Fatal(e)
		}
		s.SomeIPConfig()
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().SomeIP.MessageCount != 930 {
		t.Fatal("lost SOME/IP messages")
	}
}

func TestTCPPCAPEndToEnd(t *testing.T) {
	data, err := os.ReadFile("../../frontend/public/tcp-demo.pcap")
	if err != nil {
		t.Fatal(err)
	}
	s := watch.NewStore()
	s.Reset("tcp-demo", "file")
	r, err := Reader(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = Import(context.Background(), r, s); err != nil {
		t.Fatal(err)
	}
	s.Status("complete", nil)
	v := s.Snapshot()
	a := v.SomeIP
	if v.Packets != 86 || a.MessageCount != 5 || a.TCPMessages != 4 || a.UDPMessages != 1 || a.DiscoveryCount != 1 || a.Requests != 1 || a.Responses != 1 || a.Notifications != 3 || len(a.Services) != 1 || len(a.Members) != 4 || a.TCPBuffered != 0 || a.TCPConnections != 0 {
		t.Fatalf("unexpected TCP fixture: %+v", a)
	}
	if len(a.Warnings) != 1 || a.Warnings["payload_preview_truncated"] != 1 || len(v.Warnings) != 0 {
		t.Fatal(a.Warnings, v.Warnings)
	}
	for _, sample := range a.Samples {
		if sample.InstanceID == nil || *sample.InstanceID != 1 {
			t.Fatal("SD association lost", sample)
		}
	}
	large, ok := s.SomeIPSample(4)
	if !ok || large.Size != 102400 || !large.Truncated || len(large.Payload) != 16384 || large.Transport != "TCP" {
		t.Fatal("large message reassembly/retention", large)
	}
	for i, b := range large.Payload {
		if b != byte(i) {
			t.Fatalf("corrupt reassembled byte %d: %d", i, b)
		}
	}
	// End the file on a complete capture record, but in the middle of a TCP message.
	end := 24
	for i := 0; i < 10; i++ {
		end += 16 + int(binary.LittleEndian.Uint32(data[end+8:end+12]))
	}
	s.Reset("partial TCP", "file")
	r, err = Reader(data[:end])
	if err != nil {
		t.Fatal(err)
	}
	if err = Import(context.Background(), r, s); err != nil {
		t.Fatal(err)
	}
	s.Status("complete", nil)
	a = s.Snapshot().SomeIP
	if a.MessageCount != 3 || a.Warnings["tcp_incomplete"] != 1 || a.TCPBuffered != 0 {
		t.Fatal("EOF must discard partial TCP message with a warning", a)
	}
}
