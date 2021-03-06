package gotds

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os" // For hostname

	utf16c "github.com/Grovespaz/go-tds/utf16"
)

func (c *Conn) login() ([]byte, error) {
	loginPacket, err := c.makeLoginPacket()
	if err != nil {
		return nil, err
	}

	if c.cfg.verboseLog {
		errLog.Printf("Trying to login with username: %v, password: %v and default DB: %v", c.cfg.user, c.cfg.password, c.cfg.dbname)
		//errLog.Printf("Request: % X\n", loginPacket)
	}

	loginResult, sqlerr, err := c.sendMessage(ptyLogin, loginPacket)

	if err != nil {
		return nil, err
	}

	if len(*sqlerr) > 0 {
		// For now:
		return nil, (*sqlerr)[0]
	}

	if len(*loginResult) == 0 {
		return nil, errors.New("No Login response")
	}

	if len(*loginResult) > 1 {
		return nil, errors.New("More than 1 result in the Login response")
	}

	loginResultData := (*loginResult)[0] //[8:]

	if c.cfg.verboseLog {
		errLog.Printf("Request: % x\n", loginPacket)
		errLog.Printf("Response: % x\n", loginResultData)
	}

	return loginResultData, nil
}

func (c *Conn) makeLoginPacket() ([]byte, error) {
	b := new(bytes.Buffer)
	b.Grow(0) // TODO: Fill in the least needed amount here

	b.Write([]byte{0, 0, 0, 0}) // Length bytes, to be set later

	binary.Write(b, binary.BigEndian, c.tdsVersion)
	binary.Write(b, binary.LittleEndian, c.cfg.maxPacketSize)
	binary.Write(b, binary.LittleEndian, driverVersion)
	binary.Write(b, binary.LittleEndian, c.clientPID)
	binary.Write(b, binary.LittleEndian, c.connectionID)

	optionFlags1 := makeByteFromBits(c.byteOrder,
		c.charType,
		false, //Floattype: 2 bits, 00 = IEEE_754
		false,
		c.dumpLoad,
		c.useDBWarnings,
		c.cfg.failIfNoDB,
		c.setLang)
	b.WriteByte(optionFlags1)

	// I cheat here a little bit because we know the user is always going to be a regular user. Hence, the hardcoded false-flags you see here are specifying the usertype regular user.
	optionFlags2 := makeByteFromBits(c.cfg.failIfNoLanguage,
		c.odbc,
		false, //transboundary?
		false, //cacheConnect?
		false,
		false,
		false,
		c.cfg.integratedSecurity)
	b.WriteByte(optionFlags2)

	if (c.tdsVersion < TDS72) && (c.useOLEDB) {
		panic("Cannot set useOLEDB when TDSVersion < 7.2")
	}

	// Readonly but can still be sent < TDS7.4 even if it was only introduced in 7.4

	typeFlags := makeByteFromBits(c.cfg.sqlType,
		false, //SQLType is documented to be 4-bits, only 1 is used.
		false,
		false,
		c.useOLEDB,
		c.cfg.readOnly,
		false,
		false)

	b.WriteByte(typeFlags)

	if c.tdsVersion < TDS72 {
		b.WriteByte(0) // Was reserved < TDS7.2
	} else {
		optionFlags3 := makeByteFromBits(c.cfg.changePass,
			false, //for now, Determines if Yukon binary xml is sent when sending XML
			c.cfg.userInstance,
			false, //unknown collation handling pre 7.3
			false, //Do we use the extension-section introduced in 7.4? No we don't cause we don't offer connection resuming.
			false, //Unused from here
			false,
			false)
		b.WriteByte(optionFlags3)
	}

	binary.Write(b, binary.LittleEndian, c.cfg.timezone)
	binary.Write(b, binary.LittleEndian, c.cfg.lcid)

	hostname, err := os.Hostname()
	if err != nil {
		// Not strictly necessary, we can send a nil value but meh.
		hostname = "Unknown-go-tds-client"
	}

	var appname string
	if c.cfg.appname != "" {
		appname = c.cfg.appname
	} else {
		appname = "go-tds" //os.Args[0] // Should be executable name, at least in *nix
	}

	var servername string
	var clientID []byte // 6-byte, apparently created using MAC (NIC) address. No idea how though, so for now:
	clientID = []byte{0xfa, 0xca, 0xde, 0xfa, 0xca, 0xde}

	// Variable portion:
	varBlock := []varData{
		varData{strData: hostname},
		//According to MS specs this should be: varData{strData: ensureBrackets(c.cfg.user)},
		// But in reality they do:
		varData{strData: c.cfg.user},
		varData{data: encodePassword(c.cfg.password), halfLength: true}, //strData or data?
		varData{strData: appname},
		varData{strData: servername},
		varData{}, // Extension block which we do not use at the moment
		varData{strData: driverName},
		varData{data: []byte(c.cfg.preferredLanguage)},
		// Again, according to MS specs this should be: varData{strData: ensureBrackets(c.cfg.dbname)},
		// But in reality they do:
		varData{strData: c.cfg.dbname},
		varData{data: clientID, raw: true},
		varData{}, // SSPI data, we'll look at this later...
		varData{strData: c.cfg.attachDB},
		varData{data: []byte(c.cfg.newPass), halfLength: true}, //strData or data?
		varData{data: []byte{0, 0, 0, 0}, raw: true},           //SSPI long length.
	}

	b.Write(makeVariableDataPortion(varBlock, b.Len()))

	// Have to write length as first 4 bytes:
	// Even though we'll never exceed 2 bytes...
	result := b.Bytes()
	length := len(result)
	result[0] = byte(length % 256)
	result[1] = byte(length / 256)

	return result, nil
}

// The second part of the LOGIN message contains all data of variable length (mostly strings)
// The result consists of two parts, a header indicating all offsets and lengths, and the actual data following that.
// For some reason I can't fathom, smack in the middle of the header lies a 6(!)-byte field for the ClientID, which completely breaks any sleek generic function one would want to write for this. At the end of the header is another field in case the SSPI-length was larger than uint16. This field is a uint32 and can be used as a replacement length.
// Because of this, I introduce this struct:
type varData struct {
	data       []byte // The data to include OR:
	strData    string // The string to include
	raw        bool   // Whether to do it properly or to just smack the raw data in the header...
	halfLength bool   // Whether to divide the length in half when building the packet (for raw string data)
}

//...which we loop through a couple of times here
func makeVariableDataPortion(data []varData, startingOffset int) []byte {
	totalLength := 0
	for _, part := range data {
		var dataLength int
		if part.data == nil {
			//TODO(gv): This can't be right, make it better...
			dataLength = len(part.strData)
		} else {
			dataLength = len(part.data)
		}

		if part.raw {
			startingOffset += dataLength
			totalLength += 4 + dataLength
		} else {
			startingOffset += 4 //Two bytes offset, two bytes length
			totalLength += 4 + dataLength
		}
	}

	offset := startingOffset
	buf := bytes.NewBuffer(make([]byte, 0, totalLength))

	for _, part := range data {
		if part.raw {
			buf.Write(part.data)
		} else {
			var dataLength int
			if part.data == nil {
				//TODO(gv): This can't be right, make it better...
				dataLength = len(part.strData)
			} else {
				dataLength = len(part.data)
			}

			binary.Write(buf, binary.LittleEndian, uint16(offset))
			if part.halfLength {
				binary.Write(buf, binary.LittleEndian, uint16(dataLength/2))
			} else {
				binary.Write(buf, binary.LittleEndian, uint16(dataLength))
			}
			if part.data == nil {
				offset += dataLength * 2
			} else {
				offset += dataLength
			}
		}
	}

	for _, part := range data {
		if !part.raw {
			if part.data == nil {
				writeUTF16String(buf, part.strData)
			} else {
				buf.Write(part.data)
			}
		}
	}

	//return []byte{0x5E, 0x00, 0x08, 0x00, 0x6E, 0x00, 0x02, 0x00, 0x72, 0x00, 0x00, 0x00, 0x72, 0x00, 0x07, 0x00, 0x80, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x80, 0x00, 0x04, 0x00, 0x88, 0x00, 0x00, 0x00, 0x88, 0x00, 0x00, 0x00, 0x00, 0x50, 0x8B, 0xE2, 0xB7, 0x8F, 0x88, 0x00, 0x00, 0x00, 0x88, 0x00, 0x00, 0x00, 0x88, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x73, 0x00, 0x6B, 0x00, 0x6F, 0x00, 0x73, 0x00, 0x74, 0x00, 0x6F, 0x00, 0x76, 0x00, 0x31, 0x00, 0x73, 0x00, 0x61, 0x00, 0x4F, 0x00, 0x53, 0x00, 0x51, 0x00, 0x4C, 0x00, 0x2D, 0x00, 0x33, 0x00, 0x32, 0x00, 0x4F, 0x00, 0x44, 0x00, 0x42, 0x00, 0x43, 0x00}
	return buf.Bytes()
}

func encodePassword(password string) []byte {
	b := utf16c.Encode(password)
	for i := 0; i < len(b); i++ {
		b[i] = (b[i] >> 4) | (b[i] << 4)
		b[i] = b[i] ^ 0xA5 //10100101
	}

	return b
}

// ensureBrackets ensures that a value is enclosed in square brackets like so: [value]
// Later on this could be changed into a more full-fledged validator for object identifiers (for values such as [dbo].[value]
func ensureBrackets(value string) string {
	if len(value) == 0 {
		return value
	}

	if (value[0] == '[') && (value[len(value)-1] == ']') {
		return value
	}

	if (value[0] != '[') && (value[len(value)-1] != ']') {
		return "[" + value + "]"
	}

	panic("Incorrect format specified for value: " + value)
}
