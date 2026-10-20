package someip

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const MaxConfigBytes = 1024 * 1024

type ServiceNames struct {
	Name        string            `json:"name"`
	Methods     map[string]string `json:"methods,omitempty"`
	Eventgroups map[string]string `json:"eventgroups,omitempty"`
}
type Config struct {
	Ports    []uint16                `json:"ports"`
	Services map[string]ServiceNames `json:"services"`
}

func EmptyConfig() Config { return Config{Ports: []uint16{}, Services: map[string]ServiceNames{}} }
func Hex(n uint16) string { return fmt.Sprintf("0x%04x", n) }
func parseID(s string) (uint16, error) {
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		base = 16
		s = s[2:]
	}
	n, e := strconv.ParseUint(s, base, 16)
	return uint16(n), e
}
func ParseConfig(data []byte) (Config, error) {
	out := EmptyConfig()
	if len(data) > MaxConfigBytes {
		return out, fmt.Errorf("name mapping exceeds 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw Config
	if err := dec.Decode(&raw); err != nil {
		return out, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return out, fmt.Errorf("expected one JSON object")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return out, fmt.Errorf("expected a JSON object")
	}
	if len(raw.Services) > 1024 || len(raw.Ports) > 256 {
		return out, fmt.Errorf("mapping entry limit exceeded")
	}
	ports := map[uint16]bool{}
	for _, p := range raw.Ports {
		if p == 0 {
			return out, fmt.Errorf("port must be nonzero")
		}
		if !ports[p] {
			out.Ports = append(out.Ports, p)
			ports[p] = true
		}
	}
	total := 0
	normalize := func(in map[string]string) (map[string]string, error) {
		result := map[string]string{}
		for k, v := range in {
			total++
			if total > 16384 || len(v) > 256 {
				return nil, fmt.Errorf("too many or oversized names")
			}
			id, err := parseID(k)
			if err != nil {
				return nil, fmt.Errorf("invalid ID %q", k)
			}
			key := Hex(id)
			if _, ok := result[key]; ok {
				return nil, fmt.Errorf("duplicate ID %q", k)
			}
			result[key] = v
		}
		return result, nil
	}
	for k, v := range raw.Services {
		id, err := parseID(k)
		if err != nil {
			return out, fmt.Errorf("invalid service ID %q", k)
		}
		key := Hex(id)
		if _, ok := out.Services[key]; ok {
			return out, fmt.Errorf("duplicate service ID %q", k)
		}
		if len(v.Name) > 256 {
			return out, fmt.Errorf("name exceeds 256 bytes")
		}
		if v.Methods, err = normalize(v.Methods); err != nil {
			return out, err
		}
		if v.Eventgroups, err = normalize(v.Eventgroups); err != nil {
			return out, err
		}
		out.Services[key] = v
	}
	return out, nil
}
