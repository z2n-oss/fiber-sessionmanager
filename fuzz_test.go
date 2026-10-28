package sessionmanager

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

func FuzzDecodeRecord(f *testing.F) {
	for _, value := range []record{
		{Version: 1, Values: map[string][]byte{}},
		{Version: 1, Values: map[string][]byte{"nil": nil, "empty": {}, "value": {1}}},
		{Version: 1, ExpiresAt: -1, Values: map[string][]byte{}},
		{Version: 2, Values: map[string][]byte{}},
	} {
		raw, err := cbor.Marshal(value)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte("not a record"))
	n := normalize(Config{Namespace: "fuzz"})
	f.Fuzz(func(t *testing.T, raw []byte) {
		// Bound the fuzz harness's workload, not the production decoder's input.
		if len(raw) > 1<<20 {
			t.Skip()
		}
		original := bytes.Clone(raw)
		r, err := decodeRecord(&n, raw)
		if !bytes.Equal(raw, original) {
			t.Fatal("decoding mutated borrowed storage bytes")
		}
		if err != nil {
			return
		}
		encoded, err := cbor.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		again, err := decodeRecord(&n, encoded)
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatalf("accepted record did not round-trip: %v", err)
		}
	})
}
