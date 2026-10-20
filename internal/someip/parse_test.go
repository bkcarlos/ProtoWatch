package someip

import (
	"encoding/hex"
	"testing"
)

func wire(service, method uint16, kind byte, payload []byte) []byte {
	b := make([]byte, 16+len(payload))
	be.PutUint16(b, service)
	be.PutUint16(b[2:], method)
	be.PutUint32(b[4:], uint32(len(payload)+8))
	be.PutUint16(b[8:], 0x42)
	be.PutUint16(b[10:], 1)
	b[12] = 1
	b[13] = 1
	b[14] = kind
	copy(b[16:], payload)
	return b
}
func offerPayload() []byte {
	// Flags, entry-array length, Offer(0x1234/1, v1.0, TTL=3), options length, IPv4 endpoint.
	b, _ := hex.DecodeString("c000000000000010010000101234000101000003000000000000000c00090400c000021e00117725")
	return b
}
func sdWire(payload []byte) []byte {
	b := wire(0xffff, 0x8100, 2, payload)
	b[8] = 0
	b[9] = 0
	return b
}
func TestParseCoalescedAndDiscovery(t *testing.T) {
	a := wire(0x1234, 1, 0, []byte{1, 2, 3})
	b := wire(0x1234, 0x8001, 2, nil)
	out, err := Parse(append(a, b...))
	if err != nil || len(out) != 2 || out[0].Client != 0x42 || string(out[0].Payload) != "\x01\x02\x03" {
		t.Fatalf("messages: %+v %v", out, err)
	}
	out, err = Parse(sdWire(offerPayload()))
	if err != nil {
		t.Fatal(err)
	}
	e := out[0].SD.Entries[0]
	if e.Kind != "OfferService" || e.TTL != 3 || e.Instance != 1 || len(e.Endpoints) != 1 || e.Endpoints[0].Address != "192.0.2.30" || e.Endpoints[0].Port != 30501 || e.Endpoints[0].Transport != "UDP" {
		t.Fatalf("SD: %+v", e)
	}
}
func TestSDStopAndEventgroupSemantics(t *testing.T) {
	for _, tc := range []struct {
		kind byte
		ttl  byte
		name string
	}{{1, 0, "StopOfferService"}, {0, 3, "FindService"}, {6, 3, "SubscribeEventgroup"}, {6, 0, "StopSubscribeEventgroup"}, {7, 3, "SubscribeEventgroupAck"}, {7, 0, "SubscribeEventgroupNack"}} {
		p := offerPayload()
		p[8] = tc.kind
		p[19] = tc.ttl
		p[21] = 2
		p[23] = 7
		out, e := Parse(sdWire(p))
		if e != nil {
			t.Fatal(e)
		}
		entry := out[0].SD.Entries[0]
		if entry.Kind != tc.name {
			t.Fatal(entry)
		}
		if tc.kind >= 6 && (entry.Eventgroup == nil || *entry.Eventgroup != 7 || entry.Counter != 2) {
			t.Fatal(entry)
		}
	}
}
func TestRejectMalformedAtomically(t *testing.T) {
	valid := sdWire(offerPayload())
	for n := 0; n < len(valid); n++ {
		if _, e := Parse(valid[:n]); e == nil {
			t.Fatalf("accepted truncation at %d", n)
		}
	}
	for _, edit := range []func([]byte){
		func(b []byte) { be.PutUint32(b[4:], 0xffffffff) },
		func(b []byte) { b[12] = 2 },
		func(b []byte) { b[14] = 0xff },
		func(b []byte) { b[27] = 0x20 }, // entry references options that do not exist
		func(b []byte) { b[23] = 15 },   // entry array not multiple of 16
		func(b []byte) { b[43] = 11 },   // inconsistent option array length
		func(b []byte) { b[45] = 8 },    // invalid IPv4 option size
	} {
		b := append([]byte{}, valid...)
		edit(b)
		if _, e := Parse(b); e == nil {
			t.Fatalf("accepted malformed %x", b)
		}
	}
	bad := append(wire(0x1234, 1, 0, nil), []byte{1, 2, 3}...)
	if out, e := Parse(bad); e == nil || out != nil {
		t.Fatal("partial datagram accepted")
	}
}
func FuzzParse(f *testing.F) {
	f.Add(sdWire(offerPayload()))
	f.Add(wire(0x1234, 1, 0, []byte{1, 2, 3}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		out, err := Parse(b)
		if err == nil && (len(out) == 0 || len(out) > MaxMessagesPerPacket) {
			t.Fatal("invalid bounds")
		}
	})
}
