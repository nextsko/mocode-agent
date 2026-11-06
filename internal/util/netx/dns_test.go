package netx

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// DNS constants used by the tiny fake server below.
const (
	dnsHeaderLen = 12
	dnsTypeA     = 1
	dnsClassINET = 1
)

// TestConfigureDNSUnset proves that an empty environment leaves the process
// resolver untouched.
func TestConfigureDNSUnset(t *testing.T) {
	t.Setenv(DNSServerEnv, "")
	require.False(t, ConfigureDNS())
}

// TestConfigureDNSLookup starts a UDP DNS server on loopback, points
// MOCODE_DNS_SERVER at it, and proves that the default resolver goes through
// it.
func TestConfigureDNSLookup(t *testing.T) {
	addr := startFakeDNSServer(t)
	t.Setenv(DNSServerEnv, addr)

	require.True(t, ConfigureDNS())
	require.Equal(t, []string{addr}, Servers())

	hosts, err := net.DefaultResolver.LookupHost(t.Context(), "example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1"}, hosts)
}

// startFakeDNSServer listens on a random loopback UDP port and answers every
// A query for example.com with 127.0.0.1. It returns the listen address.
func startFakeDNSServer(t *testing.T) string {
	t.Helper()

	var lc net.ListenConfig
	conn, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, readErr := conn.ReadFrom(buf)
			if readErr != nil {
				return
			}
			resp, ok := buildResponse(buf[:n])
			if !ok {
				continue
			}
			if _, writeErr := conn.WriteTo(resp, from); writeErr != nil {
				return
			}
		}
	}()

	return conn.LocalAddr().String()
}

// buildResponse hand-crafts a minimal DNS reply for one query. A queries get
// an A record for 127.0.0.1; every other query type gets an empty NOERROR
// answer so the resolver treats it as "no records of this type".
func buildResponse(query []byte) ([]byte, bool) {
	if len(query) < dnsHeaderLen {
		return nil, false
	}

	// Find the end of the question name; the query is not compressed.
	end := dnsHeaderLen
	for {
		if end >= len(query) {
			return nil, false
		}
		labelLen := int(query[end])
		if labelLen == 0 {
			end++
			break
		}
		if labelLen&0xC0 != 0 {
			return nil, false // Compression pointers are not expected here.
		}
		end += labelLen + 1
	}
	if end+4 > len(query) {
		return nil, false
	}
	qType := binary.BigEndian.Uint16(query[end : end+2])
	questionEnd := end + 4

	answerCount := uint16(0)
	if qType == dnsTypeA {
		answerCount = 1
	}

	resp := make([]byte, 0, questionEnd+16)
	resp = append(resp, query[0], query[1]) // Transaction ID.
	resp = append(resp,
		0x81, 0x80, // QR=1, RD=1, RA=1, RCODE=0.
		0x00, 0x01, // QDCOUNT
		0x00, byte(answerCount), // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
	)
	resp = append(resp, query[dnsHeaderLen:questionEnd]...)

	if answerCount == 1 {
		resp = append(resp,
			0xC0, 0x0C, // Name: pointer to the question name.
			0x00, dnsTypeA,
			0x00, dnsClassINET,
			0x00, 0x00, 0x00, 0x3C, // TTL: 60s.
			0x00, 0x04, // RDLENGTH
			127, 0, 0, 1, // RDATA
		)
	}

	return resp, true
}
