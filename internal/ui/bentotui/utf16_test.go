package bentotui

import (
	"reflect"
	"testing"
)

func TestUTF16StreamDecoder(t *testing.T) {
	tests := []struct {
		name  string
		units []uint16
		want  []rune
	}{
		{name: "latin", units: []uint16{'é'}, want: []rune{'é'}},
		{name: "bmp", units: []uint16{'界'}, want: []rune{'界'}},
		{name: "surrogate pair", units: []uint16{0xd83d, 0xde00}, want: []rune{'😀'}},
		{name: "lone low", units: []uint16{0xde00}, want: []rune{'\uFFFD'}},
		{name: "interrupted high", units: []uint16{0xd83d, 'A'}, want: []rune{'\uFFFD', 'A'}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var decoder utf16StreamDecoder
			var got []rune
			for _, unit := range test.units {
				got = append(got, decoder.push(unit)...)
			}
			got = append(got, decoder.flush()...)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %U want %U", got, test.want)
			}
		})
	}
}

func TestUTF16StreamDecoderFlushesPendingHighSurrogate(t *testing.T) {
	var decoder utf16StreamDecoder
	if got := decoder.push(0xd83d); len(got) != 0 {
		t.Fatalf("push = %U", got)
	}
	if got := decoder.flush(); !reflect.DeepEqual(got, []rune{'\uFFFD'}) {
		t.Fatalf("flush = %U", got)
	}
}
