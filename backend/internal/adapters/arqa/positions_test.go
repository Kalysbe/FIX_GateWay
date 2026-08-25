package arqa

import "testing"

func TestParsePosBlocksFromRaw_ML_DL(t *testing.T) {
	raw := "8=FIX.4.2\x019=100\x0135=AP\x01710=POS1\x01715=20260812\x01" +
		"448=MC0031400000\x01447=D\x01452=13\x01" +
		"448=5253\x01447=D\x01452=3\x01" +
		"702=2\x01" +
		"703=ML\x015024=0\x01" +
		"5025=3\x015026=CUR\x015027=SUR\x015026=TAG\x015027=RUB\x015026=KIND\x015027=T0\x01" +
		"5028=4\x015029=OPEN_BAL\x015030=1000\x015029=CUR_BAL\x015030=950.5\x015029=CUR_LIM\x015030=2000\x015029=LOCKED\x015030=50\x01" +
		"703=DL\x015024=0\x01" +
		"5025=2\x015026=SEC\x015027=SBER\x015026=ACC\x015027=L01+00000F00\x01" +
		"5028=3\x015029=OPEN_BAL\x015030=10\x015029=CUR_BAL\x015030=12\x015029=AVG_POS_PRICE\x015030=250.5\x01" +
		"5110=ES\x01"

	blocks := parsePosBlocksFromRaw(raw)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].posType != "ML" || blocks[0].keys["CUR"] != "SUR" || blocks[0].items["CUR_BAL"] != "950.5" {
		t.Fatalf("ML block: %+v", blocks[0])
	}
	if blocks[1].posType != "DL" || blocks[1].keys["SEC"] != "SBER" || blocks[1].items["AVG_POS_PRICE"] != "250.5" {
		t.Fatalf("DL block: %+v", blocks[1])
	}

	firm, client, _ := parsePartiesFromRaw(raw)
	if firm != "MC0031400000" || client != "5253" {
		t.Fatalf("parties firm=%s client=%s", firm, client)
	}
}

func TestKindFromPosType(t *testing.T) {
	if kindFromPosType("ML") != "CASH" {
		t.Fatal("ML")
	}
	if kindFromPosType("DL") != "INSTRUMENT" {
		t.Fatal("DL")
	}
	if kindFromPosType("FP") != "FUTURES" {
		t.Fatal("FP")
	}
}
