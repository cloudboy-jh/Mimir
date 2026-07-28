package bentotui

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestReadKeysDecodesNavigationAndInterrupt(t *testing.T) {
	keys := make(chan Key, 8)
	errs := make(chan error, 1)
	go readKeys(bytes.NewBuffer([]byte{'j', 0x1b, '[', 'A', '\r', 0x03}), keys, errs)
	want := []Key{{Kind: KeyRune, Rune: 'j'}, {Kind: KeyUp}, {Kind: KeyEnter}, {Kind: KeyInterrupt}}
	for index, expected := range want {
		select {
		case got := <-keys:
			if got != expected {
				t.Fatalf("key %d: got %#v want %#v", index, got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for key %d", index)
		}
	}
	select {
	case err := <-errs:
		if err != io.EOF {
			t.Fatalf("read error %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not report EOF")
	}
}
