package main

import (
	"encoding/json"
	"testing"

	"github.com/fix-gateway/backend/internal/quikexport"
)

func TestFirmsFromRowsKeepsQUIKValues(t *testing.T) {
	rows := []quikexport.Row{
		{Raw: json.RawMessage(`{"Exchange":"KSE","FirmId":"2040  ","FirmName":"Сенти","Status":"Активна","StatusFlag":1}`)},
		{Raw: json.RawMessage(`{"Exchange":null,"FirmId":"9999","FirmName":null,"Status":null,"StatusFlag":"x"}`)},
	}
	got := firmsFromRows(rows)
	if len(got) != 2 || got[0].FirmID != "2040" || got[0].FirmName != "Сенти" || got[0].Exchange != "KSE" || got[0].StatusFlag == nil || *got[0].StatusFlag != 1 {
		t.Fatalf("%+v", got)
	}
	if got[1].FirmName != "" || got[1].StatusFlag != nil {
		t.Fatalf("%+v", got[1])
	}
}
