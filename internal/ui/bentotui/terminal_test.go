package bentotui

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestReadKeysDecodesNavigationAndInterrupt(t *testing.T) {
	keys := make(chan Key, 8)
	input := make(chan byte, 8)
	for _, value := range []byte{'j', 0x1b, '[', 'A', '\r', 0x03} {
		input <- value
	}
	close(input)
	go decodeKeys(context.Background(), input, keys)
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
}

func TestDrawUsesCarriageReturnNewlinesForRawTerminals(t *testing.T) {
	var out bytes.Buffer
	if err := draw(&out, "one\ntwo"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("one\r\ntwo")) {
		t.Fatalf("draw output %q", out.String())
	}
}
