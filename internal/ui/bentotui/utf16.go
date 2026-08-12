package bentotui

import "unicode/utf16"

type utf16StreamDecoder struct {
	high uint16
}

func (d *utf16StreamDecoder) push(unit uint16) []rune {
	if unit >= 0xd800 && unit <= 0xdbff {
		if d.high != 0 {
			d.high = unit
			return []rune{'\uFFFD'}
		}
		d.high = unit
		return nil
	}
	if unit >= 0xdc00 && unit <= 0xdfff {
		if d.high == 0 {
			return []rune{'\uFFFD'}
		}
		value := utf16.DecodeRune(rune(d.high), rune(unit))
		d.high = 0
		return []rune{value}
	}
	if d.high != 0 {
		d.high = 0
		return []rune{'\uFFFD', rune(unit)}
	}
	return []rune{rune(unit)}
}

func (d *utf16StreamDecoder) flush() []rune {
	if d.high == 0 {
		return nil
	}
	d.high = 0
	return []rune{'\uFFFD'}
}
