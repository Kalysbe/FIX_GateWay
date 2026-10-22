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

// Opt-in test fault: retain an accepted order across a socket disconnect and
// publish its delayed ACK on the next logon. This ledger is process-local.
var recoveryFaults = struct {
	sync.Mutex
	pending map[string][]map[string]string
}{pending: make(map[string][]map[string]string)}

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
	session := &simSession{orders: make(map[string]*simOrder), fillDelay: 50 * time.Millisecond}
	if delay := os.Getenv("SIM_FILL_DELAY"); delay != "" {
		if parsed, err := time.ParseDuration(delay); err == nil && parsed >= 0 {
			session.fillDelay = parsed
		}
	}
	defer func() {
		session.mu.Lock()
		session.closed = true
		for _, order := range session.orders {
			if order.timer != nil {
				order.timer.Stop()
			}
		}
		conn.Close()
		session.mu.Unlock()
	}()
	log.Printf("[%s] connection from %s", port, conn.RemoteAddr())
	r := bufio.NewReader(conn)
	var seqOut int32
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
		session.mu.Lock()
		switch msgType {
		case "A":
			loggedOn = true
			resp := buildMsg("A", sender, target, &seqOut, map[string]string{"98": "0", "108": "30"})
			_, _ = conn.Write([]byte(resp))
			log.Printf("[%s] logon from %s", port, target)
			recoveryFaults.Lock()
			scope := port + ":" + target
			reports := recoveryFaults.pending[scope]
			delete(recoveryFaults.pending, scope)
			recoveryFaults.Unlock()
			for _, report := range reports {
				_, _ = conn.Write([]byte(buildMsg("8", sender, target, &seqOut, report)))
			}
		case "0":
			resp := buildMsg("0", sender, target, &seqOut, map[string]string{"112": fields["112"]})
			_, _ = conn.Write([]byte(resp))
		case "5":
			resp := buildMsg("5", sender, target, &seqOut, nil)
			_, _ = conn.Write([]byte(resp))
			session.mu.Unlock()
			return
		case "1":
			_, _ = conn.Write([]byte(buildMsg("0", sender, target, &seqOut, map[string]string{"112": fields["112"]})))
		case "D":
			if !loggedOn {
				session.mu.Unlock()
				continue
			}
			if faultSymbol := os.Getenv("SIM_FAULT_SYMBOL"); faultSymbol != "" && firstNonEmptySim(fields["48"], fields["55"]) == faultSymbol {
				report := map[string]string{"11": fields["11"], "37": fmt.Sprintf("Q%d", time.Now().UnixNano()), "17": fmt.Sprintf("E%d", time.Now().UnixNano()), "150": "0", "39": "0", "55": faultSymbol, "54": fields["54"], "38": fields["38"], "14": "0", "151": fields["38"], "6": "0"}
				recoveryFaults.Lock()
				scope := port + ":" + target
				recoveryFaults.pending[scope] = append(recoveryFaults.pending[scope], report)
				recoveryFaults.Unlock()
				log.Printf("[%s] FAULT_RECEIVED %s; disconnecting before ACK", port, fields["11"])
				session.mu.Unlock()
				return
			}
			handleNewOrder(session, conn, sender, target, &seqOut, fields)
		case "F":
			if !loggedOn {
				session.mu.Unlock()
				continue
			}
			handleCancel(session, conn, sender, target, &seqOut, fields)
		case "AL":
			if !loggedOn {
				session.mu.Unlock()
				continue
			}
			handlePosMaint(conn, sender, target, &seqOut, fields)
		case "AN":
			if !loggedOn {
				session.mu.Unlock()
				continue
			}
			handlePosRequest(conn, sender, target, &seqOut, fields)
		default:
			log.Printf("[%s] ignore msg type %s", port, msgType)
		}
		session.mu.Unlock()
	}
}

type simSession struct {
	mu        sync.Mutex
	orders    map[string]*simOrder
	fillDelay time.Duration
	closed    bool
}

type simOrder struct {
	id, symbol, side, qty string
	status                string
	terminal              bool
	timer                 *time.Timer
}

func handleNewOrder(session *simSession, conn net.Conn, sender, target string, seq *int32, f map[string]string) {
	clOrdID := f["11"]
	symbol := firstNonEmptySim(f["48"], f["55"])
	side := f["54"]
	qty := f["38"]
	if qty == "" {
		qty = "1"
	}
	board := firstNonEmptySim(f["100"], f["207"], "TQBR")
	orderNumber := strconv.FormatInt(time.Now().UnixNano(), 10)
	orderID := time.Now().UTC().Format("060102") + "-" + board + "-" + orderNumber
	execID := fmt.Sprintf("E%d", time.Now().UnixNano())
	order := &simOrder{id: orderID, symbol: symbol, side: side, qty: qty}
	session.orders[clOrdID] = order

	if symbol == "REJECT" {
		order.terminal = true
		order.status = "8"
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
	order.timer = time.AfterFunc(session.fillDelay, func() {
		session.mu.Lock()
		defer session.mu.Unlock()
		if session.closed || order.terminal {
			return
		}
		order.terminal = true
		order.status = "2"
		fillExec := fmt.Sprintf("E%d", time.Now().UnixNano())
		px := f["44"]
		if px == "" {
			px = "100"
		}
		fill := map[string]string{
			"11": clOrdID, "37": orderID, "17": fillExec, "150": "2", "39": "2",
			"55": symbol, "54": side, "38": qty, "14": qty, "151": "0", "6": px,
			"32": qty, "31": px,
		}
		// The disposable simulator supplies exchange identity too, so recovery
		// tests exercise the same durable ledger path as ARQA reports.
		tradeNumber := strconv.FormatInt(time.Now().UnixNano(), 10)
		fill["880"] = time.Now().UTC().Format("060102") + "-" + board + "-" + tradeNumber
		fill["5001"], fill["5002"] = tradeNumber, orderNumber
		fill["75"], fill["100"], fill["48"] = time.Now().UTC().Format("20060102"), board, symbol
		fill["5018"], fill["109"], fill["1"] = f["5018"], f["109"], f["1"]
		_, _ = conn.Write([]byte(buildMsg("8", sender, target, seq, fill)))
	})
}

func handleCancel(session *simSession, conn net.Conn, sender, target string, seq *int32, f map[string]string) {
	clOrdID := f["11"]
	orig := f["41"]
	order, ok := session.orders[orig]
	if !ok || order.terminal {
		reason, status := "1", "8"
		if ok {
			reason, status = "0", order.status
		}
		_, _ = conn.Write([]byte(buildMsg("9", sender, target, seq, map[string]string{
			"11": clOrdID, "41": orig, "37": firstNonEmptySim(f["37"], "NONE"), "39": status,
			"102": reason, "434": "1", "58": "order is not cancellable",
		})))
		return
	}
	order.terminal = true
	order.status = "4"
	if order.timer != nil {
		order.timer.Stop()
	}
	orderID := order.id
	execID := fmt.Sprintf("E%d", time.Now().UnixNano())
	er := map[string]string{
		"11": clOrdID, "41": orig, "37": orderID, "17": execID, "150": "4", "39": "4",
		"55": order.symbol, "54": order.side, "38": order.qty, "14": "0", "151": "0", "6": "0",
	}
	_, _ = conn.Write([]byte(buildMsg("8", sender, target, seq, er)))
}

func handlePosMaint(conn net.Conn, sender, target string, seq *int32, f map[string]string) {
	reqID := f["710"]
	am := map[string]string{
		"710": reqID,
		"721": fmt.Sprintf("PmR%d", time.Now().UnixNano()%1_000_000),
		"709": firstNonEmptySim(f["709"], "3"),
		"712": firstNonEmptySim(f["712"], "2"),
		"722": "1", // Completed
		"723": "0",
	}
	_, _ = conn.Write([]byte(buildMsg("AM", sender, target, seq, am)))
}

func handlePosRequest(conn net.Conn, sender, target string, seq *int32, f map[string]string) {
	reqID := f["710"]
	ao := map[string]string{
		"710": reqID,
		"721": fmt.Sprintf("RfPA%d", time.Now().UnixNano()%1_000_000),
		"724": "0",
		"728": "0",
		"729": "0",
		"727": "0",
	}
	_, _ = conn.Write([]byte(buildMsg("AO", sender, target, seq, ao)))
}

func firstNonEmptySim(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
