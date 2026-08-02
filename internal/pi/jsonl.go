package pi

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

const maxJSONLRecord = 8 << 20

// readJSONL reads LF-delimited records without imposing a maximum record size.
// A trailing CR is stripped to tolerate producers that emit CRLF.
func readJSONL(r io.Reader, yield func([]byte) error) error {
	reader := bufio.NewReader(r)
	var record bytes.Buffer

	for {
		fragment, err := reader.ReadSlice('\n')
		record.Write(fragment)
		if record.Len() > maxJSONLRecord {
			return fmt.Errorf("Pi RPC record exceeds %d bytes", maxJSONLRecord)
		}

		switch {
		case err == nil:
			line := trimRecord(record.Bytes())
			if len(line) > 0 {
				if err := yield(line); err != nil {
					return err
				}
			}
			record.Reset()
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			line := trimRecord(record.Bytes())
			if len(line) > 0 {
				if err := yield(line); err != nil {
					return err
				}
			}
			return nil
		default:
			return err
		}
	}
}

func trimRecord(record []byte) []byte {
	record = bytes.TrimSuffix(record, []byte{'\n'})
	return bytes.TrimSuffix(record, []byte{'\r'})
}
