package perch

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFingerprintProfileMatchesRecordedJA3(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	captured := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			captured <- "accept: " + err.Error()
			return
		}
		defer func() { _ = conn.Close() }()
		ja3, err := readJA3(conn)
		if err != nil {
			captured <- "read: " + err.Error()
			return
		}
		captured <- ja3
	}()

	dialer := FingerprintDialer(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialer.DialTLSContext(ctx, "tcp", listener.Addr().String())
	if err == nil && conn != nil {
		_ = conn.Close()
	}

	select {
	case ja3 := <-captured:
		if strings.Contains(ja3, ":") {
			t.Fatalf("capture failed: %s", ja3)
		}
		if ja3 != PerchJA3 {
			t.Fatalf("JA3 = %s, want %s (raw: %s)", ja3, PerchJA3, lastJA3Raw)
		}
		if lastJA3Raw != PerchJA3Raw {
			t.Fatalf("JA3 parameters = %s, want %s", lastJA3Raw, PerchJA3Raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no client hello captured")
	}
}

func readJA3(conn net.Conn) (string, error) {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	if header[0] != 0x16 {
		return "", fmt.Errorf("not a handshake record")
	}
	body := make([]byte, int(binary.BigEndian.Uint16(header[3:5])))
	if _, err := io.ReadFull(conn, body); err != nil {
		return "", err
	}
	if len(body) < 38 || body[0] != 0x01 {
		return "", fmt.Errorf("not a client hello")
	}
	data := body[4:]
	version := fmt.Sprintf("%d", int(data[0])<<8|int(data[1]))
	pos := 34
	sessionLen := int(data[pos])
	pos += 1 + sessionLen
	cipherLen := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	ciphers := make([]string, 0, cipherLen/2)
	for i := 0; i < cipherLen; i += 2 {
		ciphers = append(ciphers, fmt.Sprintf("%04x", binary.BigEndian.Uint16(data[pos:])))
		pos += 2
	}
	compressionLen := int(data[pos])
	pos += 1 + compressionLen
	if pos+2 > len(data) {
		return ja3Hash(version, ciphers, nil, nil, nil), nil
	}
	extLen := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	end := pos + extLen
	var extensions, curves, formats []string
	for pos+4 <= end && pos+4 <= len(data) {
		extType := int(binary.BigEndian.Uint16(data[pos:]))
		extDataLen := int(binary.BigEndian.Uint16(data[pos+2:]))
		pos += 4
		extData := data[pos : pos+extDataLen]
		pos += extDataLen
		extensions = append(extensions, fmt.Sprintf("%d", extType))
		switch extType {
		case 10:
			if len(extData) >= 2 {
				n := int(binary.BigEndian.Uint16(extData))
				for i := 0; i+1 < n && 2+i+1 < len(extData); i += 2 {
					curves = append(curves, fmt.Sprintf("%d", binary.BigEndian.Uint16(extData[2+i:])))
				}
			}
		case 11:
			if len(extData) >= 1 {
				n := int(extData[0])
				for i := 0; i < n && 1+i < len(extData); i++ {
					formats = append(formats, fmt.Sprintf("%d", extData[1+i]))
				}
			}
		}
	}
	return ja3Hash(version, ciphers, extensions, curves, formats), nil
}

var lastJA3Raw string

func ja3Hash(version string, ciphers, extensions, curves, formats []string) string {
	raw := strings.Join([]string{
		version,
		strings.Join(ciphers, "-"),
		strings.Join(extensions, "-"),
		strings.Join(curves, "-"),
		strings.Join(formats, "-"),
	}, ",")
	lastJA3Raw = raw
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestFingerprintTransportSetsDialer(t *testing.T) {
	transport := FingerprintTransport(nil)
	if transport.DialTLSContext == nil {
		t.Fatalf("fingerprint transport must own the TLS dialer")
	}
	if transport.ForceAttemptHTTP2 {
		t.Fatalf("perch traffic is HTTP/1.1, so HTTP/2 must stay off")
	}
}
