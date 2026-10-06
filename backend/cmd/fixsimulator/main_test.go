package main

import (
	"bufio"
	"net"
	"testing"
	"time"
)

func simulatorClient(t *testing.T) (net.Conn, *bufio.Reader) {
	t.Helper()
	server, client := net.Pipe()
	go handleConn(server, "test")
	t.Cleanup(func() { client.Close() })
	client.SetDeadline(time.Now().Add(3 * time.Second))
	return client, bufio.NewReader(client)
}

func sendSim(t *testing.T, conn net.Conn, kind string, seq *int32, fields map[string]string) {
	t.Helper()
	if _, err := conn.Write([]byte(buildMsg(kind, "CLIENT", "SIM", seq, fields))); err != nil {
		t.Fatal(err)
	}
}

func readSim(t *testing.T, r *bufio.Reader) map[string]string {
	t.Helper()
	msg, err := readMessage(r)
	if err != nil {
		t.Fatal(err)
	}
	return parseFIX(msg)
}

func TestSimulatorSecurityIDRejectionAndSequence(t *testing.T) {
	conn, r := simulatorClient(t)
	var seq int32
	sendSim(t, conn, "A", &seq, map[string]string{"98": "0", "108": "30"})
	if msg := readSim(t, r); msg["34"] != "1" || msg["35"] != "A" {
		t.Fatalf("logon: %#v", msg)
	}
	sendSim(t, conn, "D", &seq, map[string]string{"11": "reject", "48": "REJECT", "54": "1", "38": "1"})
	if msg := readSim(t, r); msg["34"] != "2" || msg["39"] != "8" {
		t.Fatalf("reject: %#v", msg)
	}
}

func TestSimulatorDisconnectThenLateConfirmation(t *testing.T) {
	t.Setenv("SIM_FAULT_SYMBOL", "FAULT_RECOVER")
	conn, r := simulatorClient(t)
	var seq int32
	sendSim(t, conn, "A", &seq, nil)
	readSim(t, r)
	sendSim(t, conn, "D", &seq, map[string]string{"11": "fault-test", "48": "FAULT_RECOVER", "54": "1", "38": "2"})
	if _, err := readMessage(r); err == nil {
		t.Fatal("fault did not disconnect before ACK")
	}
	reconnected, reports := simulatorClient(t)
	seq = 0
	sendSim(t, reconnected, "A", &seq, nil)
	readSim(t, reports)
	ack := readSim(t, reports)
	if ack["35"] != "8" || ack["11"] != "fault-test" || ack["39"] != "0" || ack["38"] != "2" {
		t.Fatalf("late confirmation: %+v", ack)
	}
	sendSim(t, reconnected, "1", &seq, map[string]string{"112": "after-recovery"})
	if heartbeat := readSim(t, reports); heartbeat["35"] != "0" {
		t.Fatalf("confirmation repeated: %+v", heartbeat)
	}
}

func TestSimulatorCancelPreventsFill(t *testing.T) {
	t.Setenv("SIM_FILL_DELAY", "100ms")
	conn, r := simulatorClient(t)
	var seq int32
	sendSim(t, conn, "A", &seq, nil)
	readSim(t, r)
	sendSim(t, conn, "D", &seq, map[string]string{"11": "order", "48": "SBER", "54": "1", "38": "2"})
	ack := readSim(t, r)
	if ack["39"] != "0" {
		t.Fatalf("ack: %#v", ack)
	}
	sendSim(t, conn, "F", &seq, map[string]string{"11": "cancel", "41": "order"})
	if msg := readSim(t, r); msg["39"] != "4" || msg["37"] != ack["37"] || msg["34"] != "3" {
		t.Fatalf("cancel: %#v", msg)
	}
	time.Sleep(150 * time.Millisecond)
	sendSim(t, conn, "1", &seq, map[string]string{"112": "probe"})
	if msg := readSim(t, r); msg["35"] != "0" || msg["112"] != "probe" || msg["34"] != "4" {
		t.Fatalf("unexpected post-cancel report: %#v", msg)
	}
}

func TestSimulatorFillThenCancelReject(t *testing.T) {
	t.Setenv("SIM_FILL_DELAY", "1ms")
	conn, r := simulatorClient(t)
	var seq int32
	sendSim(t, conn, "A", &seq, nil)
	readSim(t, r)
	sendSim(t, conn, "D", &seq, map[string]string{"11": "order", "48": "SBER", "54": "1", "38": "2", "44": "100"})
	readSim(t, r)
	if msg := readSim(t, r); msg["39"] != "2" || msg["32"] != "2" || msg["34"] != "3" {
		t.Fatalf("fill: %#v", msg)
	}
	sendSim(t, conn, "F", &seq, map[string]string{"11": "cancel", "41": "order"})
	if msg := readSim(t, r); msg["35"] != "9" || msg["39"] != "2" || msg["34"] != "4" {
		t.Fatalf("cancel reject: %#v", msg)
	}
}
