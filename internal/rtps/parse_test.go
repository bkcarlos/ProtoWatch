package rtps

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func wire(t *testing.T, s string) []byte {
	t.Helper()
	v, e := hex.DecodeString(s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestDataWireEndianness(t *testing.T) {
	// Hand-authored RTPS DATA with sequence (1 << 32) + 7, beyond a uint32.
	for _, tc := range []struct{ name, wire string }{
		{"little", "525450530203010f0102030405060708090a0b0c15051800000010000000000000001003010000000700000000010000"},
		{"big", "525450530203010f0102030405060708090a0b0c15040018000000100000000000001003000000010000000700000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, e := Parse(wire(t, tc.wire))
			if e != nil {
				t.Fatal(e)
			}
			if len(m.Data) != 1 || m.Data[0].Sequence != "4294967303" || m.Data[0].Writer != "0102030405060708090a0b0c00001003" {
				t.Fatalf("unexpected DATA: %+v", m)
			}
			if len(m.Data[0].Payload) != 4 {
				t.Fatal("encapsulation lost")
			}
		})
	}
}

func TestInlineQoSAndMalformedLengths(t *testing.T) {
	packet := wire(t, "525450530203010f0102030405060708090a0b0c15072000000010000000000000001003000000000100000071000400000000000100000000010000")
	// Inline QoS is status-info (8 bytes) plus sentinel (4), so DATA length=36.
	binary.LittleEndian.PutUint16(packet[22:24], 36)
	m, e := Parse(packet)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Data[0].Payload) != 4 {
		t.Fatal("inline QoS leaked into payload")
	}
	for i := 0; i < len(packet); i++ {
		_, _ = Parse(packet[:i])
	} // Every truncation must be safe.
	bad := append([]byte(nil), packet...)
	bad[26] = 0xff
	bad[27] = 0xff
	if _, e = Parse(bad); e == nil {
		t.Fatal("accepted invalid QoS offset")
	}
	bad = append([]byte(nil), packet...)
	bad[22] = 0xff
	bad[23] = 0xff
	if _, e = Parse(bad); e == nil {
		t.Fatal("accepted submessage overrun")
	}
}

func TestFragmentAndSecurityAreNotSamples(t *testing.T) {
	for _, id := range []byte{0x16, 0x31} {
		p := wire(t, "525450530203010f0102030405060708090a0b0c0001040000000000")
		p[20] = id
		m, e := Parse(p)
		if e != nil {
			t.Fatal(e)
		}
		if len(m.Data) != 0 || len(m.Warnings) != 1 {
			t.Fatalf("not labelled unsupported: %+v", m)
		}
	}
}
func FuzzParse(f *testing.F) {
	f.Add([]byte("RTPS\x02\x03\x01\x0fabcdefghijkl"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			return
		}
		_, _ = Parse(b)
	})
}
