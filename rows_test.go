package gotds

import (
	"testing"
  "fmt"
)

func TestParseSimpleSelectResult(t *testing.T) {
  c := Conn{tdsVersion: TDS72}
	// Original query: "SELECT 1, 2, 3"
	raw := []byte{0x81, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x38, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x38, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x38, 0x00, 0xd1, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0xfd, 0x10, 0x00, 0xc1, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
  result, err := c.parseResult(raw[1:])
  if err != nil {
   t.Fatal(err) 
  }
  fmt.Printf("%+v", result)
}
