//go:build !linux

package capture

import "fmt"

func OpenLive(name string) (Live, error) {
	return nil, fmt.Errorf("live capture requires Linux with CAP_NET_RAW; PCAP import is available on this platform")
}
