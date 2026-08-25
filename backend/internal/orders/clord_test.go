package orders

import "testing"

func TestClOrdLookupKeysFromCancelID(t *testing.T) {
	keys := clOrdLookupKeys("G1492c075-b29a-4e59-b4a1-76b40702-C", "")
	want := "G1492c075-b29a-4e59-b4a1-76b40702"
	found := false
	for _, k := range keys {
		if k == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected original clOrdID %s in %v", want, keys)
	}
}
