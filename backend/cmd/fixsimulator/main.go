package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Minimal FIX.4.4 acceptor for local e2e testing (two ports).
// Not a full FIX engine — enough for Logon / NewOrderSingle / Cancel + ExecutionReports.

func main() {
	ports := []string{"5001", "5002"}
	if v := os.Getenv("SIM_PORTS"); v != "" {
		ports = strings.Split(v, ",")
	}
	var wg sync.WaitGroup
	for _, p := range ports {
		wg.Add(1)
		go func(port string) {
			defer wg.Done()
			if err := listen(port); err != nil {
				log.Printf("port %s error: %v", port, err)
			}
		}(strings.TrimSpace(p))
	}
	wg.Wait()
}

func listen(port string) error {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	log.Printf("fixsimulator listening on %s", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, port)
	}
}

func handleConn(conn net.Conn, port string) {
	defer conn.Close()
	log.Printf("[%s] connection from %s", port, conn.RemoteAddr())
	r := bufio.NewReader(conn)
	var seqOut int32 = 1
	var sender, target string
	loggedOn := false

	for {
		msg, err := readMessage(r)
		if err != nil {
			if err != io.EOF {
				log.Printf("[%s] read error: %v", port, err)
			}
			return
		}
		fields := parseFIX(msg)
		msgType := fields["35"]
		if sender == "" {
			sender = fields["56"] // our SenderCompID = their TargetCompID
			target = fields["49"]
		}
		switch msgType {
		case "A":
			loggedOn = true
			resp := buildMsg("A", sender, target, &seqOut, map[string]string{"98": "0", "108": "30"})
			_, _ = conn.Write([]byte(resp))
			log.Printf("[%s] logon from %s", port, target)
		case "0":
			resp := buildMsg("0", sender, target, &seqOut, map[string]string{"112": fields["112"]})
			_, _ = conn.Write([]byte(resp))
		case "5":
			resp := buildMsg("5", sender, target, &seqOut, nil)
			_, _ = conn.Write([]byte(resp))
			return
		case "D":
			if !loggedOn {
				continue
			}
			handleNewOrder(conn, sender, target, &seqOut, fields)
		case "F":
			if !loggedOn {
				continue
			}
			handleCancel(conn, sender, target, &seqOut, fields)
		default:
			log.Printf("[%s] ignore msg type %s", port, msgType)
		}
	}
}

func handleNewOrder(conn net.Conn, sender, target string, seq *int32, f map[string]string) {
	clOrdID := f["11"]
	symbol := f["55"]
	side := f["54"]
	qty := f["38"]
	if qty == "" {
		qty = "1"
	}
	orderID := fmt.Sprintf("Q%d", time.Now().UnixNano()%1_000_000_000)
	execID := fmt.Sprintf("E%d", time.Now().UnixNano()%1_000_000_000)

	if symbol == "REJECT" {
		er := map[string]string{
			"11": clOrdID, "37": orderID, "17": execID, "150": "8", "39": "8",
			"55": symbol, "54": side, "38": qty, "14": "0", "151": qty, "6": "0", "58": "symbol rejected",
		}
		_, _ = conn.Write([]byte(buildMsg("8", sender, target, seq, er)))
		return
	}

	// ACK
	ack := map[string]string{
		"11": clOrdID, "37": orderID, "17": execID, "150": "0", "39": "0",
		"55": symbol, "54": side, "38": qty, "14": "0", "151": qty, "6": "0",
	}
	_, _ = conn.Write([]byte(buildMsg("8", sender, target, seq, ack)))

	// Auto-fill after short delay
	time.AfterFunc(50*time.Millisecond, func() {
		fillExec := fmt.Sprintf("E%d", time.Now().UnixNano()%1_000_000_000)
		px := f["44"]
		if px == "" {
			px = "100"
		}
		fill := map[string]string{
			"11": clOrdID, "37": orderID, "17": fillExec, "150": "2", "39": "2",
			"55": symbol, "54": side, "38": qty, "14": qty, "151": "0", "6": px,
			"32": qty, "31": px,
		}
		_, _ = conn.Write([]byte(buildMsg("8", sender, target, seq, fill)))
	})
}

func handleCancel(conn net.Conn, sender, target string, seq *int32, f map[string]string) {
	clOrdID := f["11"]
	orig := f["41"]
	orderID := f["37"]
	if orderID == "" {
		orderID = "Q0"
	}
	execID := fmt.Sprintf("E%d", time.Now().UnixNano()%1_000_000_000)
	er := map[string]string{
		"11": clOrdID, "41": orig, "37": orderID, "17": execID, "150": "4", "39": "4",
		"55": f["55"], "54": f["54"], "14": "0", "151": "0", "6": "0",
	}
	_, _ = conn.Write([]byte(buildMsg("8", sender, target, seq, er)))
}

func readMessage(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		chunk, err := r.ReadString('\x01')
		if err != nil {
			return "", err
		}
		b.WriteString(chunk)
		if strings.Contains(chunk, "10=") || strings.HasPrefix(chunk, "10=") {
			// checksum field ends message
			if strings.Contains(b.String(), "\x0110=") || strings.HasPrefix(b.String(), "8=") {
				s := b.String()
				if idx := strings.LastIndex(s, "\x0110="); idx >= 0 {
					// include checksum value + SOH
					rest := s[idx+1:]
					if cidx := strings.IndexByte(rest, '\x01'); cidx >= 0 {
						return s[:idx+1+cidx+1], nil
					}
				}
			}
		}
		if len(b.String()) > 1<<20 {
			return "", fmt.Errorf("message too large")
		}
	}
}

func parseFIX(msg string) map[string]string {
	out := map[string]string{}
	parts := strings.Split(msg, "\x01")
	for _, p := range parts {
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

func buildMsg(msgType, sender, target string, seq *int32, body map[string]string) string {
	n := atomic.AddInt32(seq, 1)
	var bodyStr strings.Builder
	bodyStr.WriteString("35=" + msgType + "\x01")
	bodyStr.WriteString("49=" + sender + "\x01")
	bodyStr.WriteString("56=" + target + "\x01")
	bodyStr.WriteString("34=" + strconv.Itoa(int(n)) + "\x01")
	bodyStr.WriteString("52=" + time.Now().UTC().Format("20060102-15:04:05.000") + "\x01")
	for k, v := range body {
		if v == "" && k != "112" {
			continue
		}
		bodyStr.WriteString(k + "=" + v + "\x01")
	}
	bodyLen := bodyStr.Len()
	head := fmt.Sprintf("8=FIX.4.4\x019=%d\x01", bodyLen)
	msg := head + bodyStr.String()
	sum := 0
	for i := 0; i < len(msg); i++ {
		sum += int(msg[i])
	}
	return msg + fmt.Sprintf("10=%03d\x01", sum%256)
}
