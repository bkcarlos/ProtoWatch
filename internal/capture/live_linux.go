//go:build linux

package capture

import "github.com/gopacket/gopacket/pcapgo"

type ethernet struct{ *pcapgo.EthernetHandle }

func OpenLive(name string) (Live, error) {
	h, err := pcapgo.NewEthernetHandle(name)
	if err != nil {
		return nil, err
	}
	if err = h.SetCaptureLength(65536); err != nil {
		h.Close()
		return nil, err
	}
	// Packet-socket membership is scoped to this socket, not a permanent link setting.
	if err = h.SetPromiscuous(true); err != nil {
		h.Close()
		return nil, err
	}
	return &ethernet{h}, nil
}
func (h *ethernet) Dropped() (uint64, error) {
	st, err := h.Stats()
	if err != nil {
		return 0, err
	}
	return uint64(st.Drops), nil
}
