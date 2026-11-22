package i2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var i2pEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)
var i2pBase64 = base64Encoding()

type samReply struct {
	topic  string
	action string
	values map[string]string
}

func base64Encoding() *base64.Encoding {
	return base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-~")
}

func parseReply(line string) samReply {
	reply := samReply{values: make(map[string]string)}
	line = strings.TrimSpace(line)
	fields := strings.Fields(line)
	if len(fields) > 0 {
		reply.topic = fields[0]
	}
	if len(fields) > 1 {
		reply.action = fields[1]
	}
	for _, field := range fields[2:] {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			reply.values[key] = strings.Trim(value, `"`)
		}
	}
	if index := strings.Index(line, `MESSAGE="`); index >= 0 {
		message := line[index+len(`MESSAGE=`):]
		if end := strings.LastIndexByte(message, '"'); end >= 0 {
			reply.values["MESSAGE"] = message[1:end]
		}
	}
	return reply
}

func dialSAM(ctx context.Context, address string, timeout time.Duration) (net.Conn, *bufio.Reader, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, err
	}
	reader := bufio.NewReader(conn)
	reply, err := samCommand(conn, reader, timeout, "HELLO VERSION MIN=3.1 MAX=3.3")
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if reply.topic != "HELLO" || reply.action != "REPLY" || reply.values["RESULT"] != "OK" {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SAM HELLO failed: %s %s", reply.values["RESULT"], reply.values["MESSAGE"])
	}
	return conn, reader, nil
}

func samCommand(conn net.Conn, reader *bufio.Reader, timeout time.Duration, format string, args ...any) (samReply, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return samReply{}, fmt.Errorf("set SAM deadline: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	if _, err := fmt.Fprintf(conn, format+"\n", args...); err != nil {
		return samReply{}, fmt.Errorf("write SAM command: %w", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return samReply{}, fmt.Errorf("read SAM reply: %w", err)
	}
	return parseReply(line), nil
}

func readDestination(priv string) ([]byte, error) {
	decoded, err := i2pBase64.DecodeString(priv)
	if err != nil || len(decoded) < 387 {
		return nil, errors.New("invalid destination key")
	}
	destinationLength := 387 + int(binary.BigEndian.Uint16(decoded[385:387]))
	if len(decoded) < destinationLength {
		return nil, errors.New("truncated destination key")
	}
	return decoded[:destinationLength], nil
}

func addressFor(destination []byte) string {
	digest := sha256.Sum256(destination)
	return strings.ToLower(i2pEncoding.EncodeToString(digest[:])) + ".b32.i2p"
}
