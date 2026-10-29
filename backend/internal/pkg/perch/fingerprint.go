package perch

import (
	"context"
	"net"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

var perchCipherSuites = []uint16{
	0x1302, 0x1303, 0x1301, 0xc02f, 0xc02b, 0xc030, 0xc02c, 0x009e,
	0xc027, 0x0067, 0xc028, 0x006b, 0x00a3, 0x009f, 0xcca9, 0xcca8,
	0xccaa, 0xc0ad, 0xc09f, 0xc05d, 0xc061, 0xc057, 0xc053, 0x00a2,
	0xc0ac, 0xc09e, 0xc05c, 0xc060, 0xc056, 0xc052, 0xc024, 0x006a,
	0xc023, 0x0040, 0xc00a, 0xc014, 0x0039, 0x0038, 0xc009, 0xc013,
	0x0033, 0x0032, 0x009d, 0xc09d, 0xc051, 0x009c, 0xc09c, 0xc050,
	0x003d, 0x003c, 0x0035, 0x002f,
}

var perchCurves = []uint16{4588, 29, 23, 30, 24, 25, 256, 257}

var perchPointFormats = []uint16{0, 1, 2}

var perchSignatureAlgorithms = []uint16{2309, 2310, 2308, 1027, 1283, 1539, 2055, 2056, 2074, 2075, 2076, 2057, 2058, 2059, 2052, 2053, 2054, 1025, 1281, 1537, 771, 769, 770, 1026, 1282, 1538}

var perchExtensions = []uint16{65281, 0, 11, 10, 35, 16, 22, 23, 13, 43, 45, 51}

const PerchJA3 = "fe1a22af39881f8613c4834b2a568a00"

const PerchJA3Raw = "771,1302-1303-1301-c02f-c02b-c030-c02c-009e-c027-0067-c028-006b-00a3-009f-cca9-cca8-ccaa-c0ad-c09f-c05d-c061-c057-c053-00a2-c0ac-c09e-c05c-c060-c056-c052-c024-006a-c023-0040-c00a-c014-0039-0038-c009-c013-0033-0032-009d-c09d-c051-009c-c09c-c050-003d-003c-0035-002f,65281-11-10-35-16-22-23-13-43-45-51,4588-29-23-30-24-25-256-257,0-1-2"

func FingerprintProfile() *tlsfingerprint.Profile {
	return &tlsfingerprint.Profile{
		Name:                "perch-cli-2.4.120",
		CipherSuites:        append([]uint16(nil), perchCipherSuites...),
		Curves:              append([]uint16(nil), perchCurves...),
		PointFormats:        append([]uint16(nil), perchPointFormats...),
		Extensions:          append([]uint16(nil), perchExtensions...),
		SignatureAlgorithms: append([]uint16(nil), perchSignatureAlgorithms...),
		ALPNProtocols:       []string{"http/1.1"},
		SupportedVersions:   []uint16{0x0304, 0x0303},
		KeyShareGroups:      []uint16{4588, 29},
		PSKModes:            []uint16{1},
	}
}

type BaseDialer func(ctx context.Context, network, addr string) (net.Conn, error)

func FingerprintDialer(base BaseDialer) *tlsfingerprint.Dialer {
	if base == nil {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		base = dialer.DialContext
	}
	return tlsfingerprint.NewDialer(FingerprintProfile(), base)
}
